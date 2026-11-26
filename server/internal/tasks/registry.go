package tasks

import (
	"context"
	"errors"
	"strings"
	"sync"

	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/repo"
	"configflow/internal/taskstatus"
)

// Registry owns task row builders, runners, and interval editors for one
// server instance.
type Registry struct {
	mu        sync.Mutex
	rows      map[string]BuildRows
	runners   map[string]RunFunc
	intervals map[string]IntervalEditor
	repo      *repo.Repository
	spawner   lockx.Spawner
}

// NewRegistry creates a task Registry pre-populated with the built-in WebDAV
// backup row builder.
func NewRegistry(r *repo.Repository, spawner lockx.Spawner) *Registry {
	reg := &Registry{
		rows:      map[string]BuildRows{},
		runners:   map[string]RunFunc{},
		intervals: map[string]IntervalEditor{},
		repo:      r,
		spawner:   spawner,
	}
	reg.rows[SlotBackup] = reg.backupRows
	return reg
}

func (g *Registry) repository(_ context.Context) (*repo.Repository, error) {
	if g.repo != nil {
		return g.repo, nil
	}
	return nil, errors.New("tasks: repository is nil")
}

func (g *Registry) taskSpawner(ctx context.Context) lockx.Spawner {
	if g.spawner != nil {
		return g.spawner
	}
	return lifecycle.SpawnerFrom(ctx)
}

// SetRowBuilder installs the row builder of a slot and returns its restoration.
func (g *Registry) SetRowBuilder(slot string, f BuildRows) func() {
	g.mu.Lock()
	previous := g.rows[slot]
	g.rows[slot] = f
	g.mu.Unlock()
	return func() { g.mu.Lock(); g.rows[slot] = previous; g.mu.Unlock() }
}

// SetRunner installs the "run now" of a category (or of the exact key
// "metrics_history_flush") and returns its restoration.
func (g *Registry) SetRunner(category string, f RunFunc) func() {
	g.mu.Lock()
	previous := g.runners[category]
	g.runners[category] = f
	g.mu.Unlock()
	return func() { g.mu.Lock(); g.runners[category] = previous; g.mu.Unlock() }
}

// SetIntervalEditor installs an exact-key editor and returns its restoration.
func (g *Registry) SetIntervalEditor(key string, f IntervalEditor) func() {
	g.mu.Lock()
	previous := g.intervals[key]
	g.intervals[key] = f
	g.mu.Unlock()
	return func() { g.mu.Lock(); g.intervals[key] = previous; g.mu.Unlock() }
}

// HasRows reports whether a real provider is installed for a slot.
func (g *Registry) HasRows(slot string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.rows[slot] != nil
}

// Missing lists the slots nobody provides yet.
func (g *Registry) Missing() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var missing []string
	for _, slot := range slotOrder {
		if g.rows[slot] == nil {
			missing = append(missing, slot)
		}
	}
	return missing
}

// Complete reports whether every slot has rows.
func (g *Registry) Complete() bool { return len(g.Missing()) == 0 }

// Validate checks that every required task slot is registered.
func (g *Registry) Validate() error {
	if missing := g.Missing(); len(missing) > 0 {
		return &Unavailable{Slots: missing}
	}
	return nil
}

// Runner returns the run function of a category.
func (g *Registry) Runner(category string) RunFunc {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.runners[category]
}

// IntervalEditorFor returns the interval edit of a key.
func (g *Registry) IntervalEditorFor(key string) IntervalEditor {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.intervals[key]
}

// StartBackground runs work once in the background using g's spawner.
func (g *Registry) StartBackground(ctx context.Context, key string, work func(ctx context.Context) error) error {
	return startBackground(ctx, key, work, g.taskSpawner(ctx))
}

// List is list_tasks().
func (g *Registry) List(ctx context.Context) ([]*Task, error) {
	if !g.Complete() {
		return nil, ErrIncomplete
	}
	return g.Available(ctx)
}

// Available projects receipts only for registered task owners.
func (g *Registry) Available(ctx context.Context) ([]*Task, error) {
	return g.availableRows(ctx, "")
}

// ForSlot returns only the selected provided slot, never a fake empty provider.
func (g *Registry) ForSlot(ctx context.Context, slot string) ([]*Task, error) {
	if !g.HasRows(slot) {
		return nil, &Unavailable{Slots: []string{slot}}
	}
	return g.availableRows(ctx, slot)
}

func (g *Registry) availableRows(ctx context.Context, selected string) ([]*Task, error) {
	repository, err := g.repository(ctx)
	if err != nil {
		return nil, err
	}
	statuses, err := taskstatus.New(repository.DataDir()).Read()
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	builders := make([]BuildRows, 0, len(slotOrder))
	for _, slot := range slotOrder {
		if selected != "" && slot != selected {
			continue
		}
		if build := g.rows[slot]; build != nil {
			builders = append(builders, build)
		}
	}
	g.mu.Unlock()
	result := []*Task{}
	for _, build := range builders {
		part, err := build(ctx, statuses)
		if err != nil {
			return nil, err
		}
		result = append(result, part...)
	}
	return result, nil
}

// RunNow owns task admission; missing known capabilities never become empty
// production providers, and a stopped generation cannot start a new job.
func (g *Registry) RunNow(ctx context.Context, key string) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	category, resource, _ := strings.Cut(key, ":")
	if key == MetricsTaskKey {
		category = key
	}
	slot, known := SlotFor(category)
	if !known || (category == MetricsTaskKey && key != MetricsTaskKey) {
		return ErrUnknownTask
	}
	run := g.Runner(category)
	if !g.HasRows(slot) || run == nil {
		return &Unavailable{Slots: []string{slot}}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return run(ctx, key, resource)
}

func (g *Registry) backupRows(ctx context.Context, statuses taskstatus.Snapshot) ([]*Task, error) {
	repository, err := g.repository(ctx)
	if err != nil {
		return nil, err
	}
	backup, err := repository.Backup(ctx)
	if err != nil {
		return nil, err
	}
	configured := backup.WebDAVURL != "" && backup.WebDAVUsername != "" && backup.WebDAVPassword != ""
	if _, present := statuses[BackupTaskKey]; !configured && !present {
		return nil, nil
	}
	history, err := ProjectHistory(statuses.Entry(BackupTaskKey))
	if err != nil {
		return nil, err
	}
	automatic := backup.AutoBackup != nil && *backup.AutoBackup
	hint := ""
	if !configured {
		hint = msgBackupConfigure
	}
	manual := history.LastTrigger != nil && *history.LastTrigger == "manual"
	return []*Task{{Key: BackupTaskKey, Category: SlotBackup, Name: msgBackupName, Enabled: automatic,
		NotificationsEnabled: new(automatic || (configured && manual)), ScheduleText: msgBackupSchedule,
		CanRun: configured, RunHint: hint, Running: IsBackground(BackupTaskKey), Link: new("/system-settings"), HistoryData: history}}, nil
}
