package webdav

// Repository snapshots and commit observers share the native document model.
import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/taskstatus"
)

var service atomic.Pointer[Service]

func init() { service.Store(NewService(nil)) }

// Default returns the process's backup service.
func Default() *Service { return service.Load() }

// SetDefault installs a service and returns a restore function for isolated tests.
func SetDefault(next *Service) func() {
	if next == nil {
		next = NewService(nil)
	}
	previous := service.Swap(next)
	return func() { service.Store(previous) }
}

// Snapshot is a fresh complete document and its settings. The private repository
// association lets background receipts/notifications use their originating store;
// no request context is retained.
type Snapshot struct {
	Document   json.RawMessage
	Settings   Settings
	DataDir    string
	repository *repo.Repository
}

type repositorySource struct{ repository *repo.Repository }

func (s repositorySource) Snapshot(ctx context.Context) (Snapshot, error) {
	snapshot, document, err := captureDocument(ctx, s.repository)
	if err != nil {
		return Snapshot{}, err
	}
	var settings Settings
	backup, err := json.Marshal(document.System.Backup)
	if err != nil {
		return Snapshot{}, err
	}
	if err := json.Unmarshal(backup, &settings); err != nil {
		return Snapshot{}, err
	}
	snapshot.Settings = settings
	return snapshot, nil
}

func captureDocument(ctx context.Context, r *repo.Repository) (Snapshot, *model.Document, error) {
	document, err := r.ExportAllJSON(lifecycle.Detach(ctx), false)
	if err != nil {
		return Snapshot{}, nil, err
	}
	var native model.Document
	if err := json.Unmarshal(document, &native); err != nil {
		return Snapshot{}, nil, err
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return Snapshot{}, nil, err
	}
	return Snapshot{Document: data, DataDir: r.DataDir(), repository: r}, &native, nil
}

// Capture exports the complete configuration for a manual backup. The caller
// supplies request settings; unrelated malformed saved settings cannot block it.
func Capture(ctx context.Context) (Snapshot, error) {
	ctx = lifecycle.Detach(ctx)
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot, _, err := captureDocument(ctx, r)
	return snapshot, err
}

// Run records one manual or automatic upload in the shared task receipt store.
func Run(ctx context.Context, snapshot Snapshot, automatic bool) (Result, error) {
	ctx = lifecycle.Detach(ctx)
	if snapshot.repository != nil {
		ctx = repo.WithRepository(ctx, snapshot.repository)
	}
	s := Default()
	started := s.Now()
	clock := time.Now()
	trigger := "manual"
	if automatic {
		trigger = "event"
	}
	result, err := s.Upload(ctx, snapshot.Document, snapshot.Settings, automatic)
	detail := ""
	if err != nil {
		detail = DescribeError(err)
		var directory *DirectoryError
		if errors.Is(err, ErrIncompleteSettings) || errors.As(err, &directory) {
			detail = err.Error()
		}
	}
	store := notifications.TaskStatus(ctx, snapshot.DataDir)
	store.Now = s.Now
	_, recordErr := store.SaveResult(ctx, "webdav_backup", taskstatus.Result{OK: err == nil, StartedAt: started.UTC().Format("2006-01-02T15:04:05Z"), Trigger: trigger, DurationMs: float64(time.Since(clock).Milliseconds()), Error: detail})
	if err != nil {
		return Result{}, err
	}
	return result, recordErr
}

func newAutomaticWorker(upload UploadFunc) *Worker { return NewWorker(time.Second, upload) }

var automaticWorker = newAutomaticWorker(func(ctx context.Context, snapshot Snapshot) error { _, err := Run(ctx, snapshot, true); return err })

// Wire installs the post-commit hook and bounded shutdown handling.
func Wire() {
	wireTasks()
	registerHook(automaticWorker)
	lifecycle.RegisterStartup(workerStartup(automaticWorker, lifecycle.DefaultSpawner{}, lifecycle.RegisterShutdown))
}

func workerStartup(worker *Worker, spawner lockx.Spawner, register func(lifecycle.ShutdownHook)) lifecycle.Startup {
	return lifecycle.Startup{Name: "webdav-worker-init", Run: func(ctx context.Context) error {
		g := worker.activate(workerOptions{Clock: lifecycle.ClockFrom(ctx), Spawner: spawner})
		if g != nil {
			// Bind the hook to this activation. A late old hook cannot stop a
			// new generation after the old Runtime's grace period elapsed.
			register(lifecycle.ShutdownHook{Name: "webdav-auto-backup", Phase: lifecycle.PhaseAfterDrain,
				Run: func(ctx context.Context) { _ = worker.stopGeneration(ctx, g) }})
		}
		return nil
	}}
}

func registerHook(worker *Worker) {
	repo.RegisterHooks(repo.Hooks{RecordHooks: repo.RecordHooks{ConfigurationCommittedRecords: func(r *repo.Repository, previous, current *model.Document) {
		// A failed scheduling/serialization step must never change commit success.
		defer func() {
			if recover() != nil {
				logger.Warning(msgScheduleFailed)
			}
		}()
		if autoBackupOff(current) {
			return
		}
		before, err := json.Marshal(previous)
		if err != nil {
			logger.Warning(msgScheduleFailed)
			return
		}
		after, err := json.Marshal(current)
		if err != nil {
			logger.Warning(msgScheduleFailed)
			return
		}
		changed, err := configurationChanged(before, after)
		if err != nil {
			logger.Warning(msgScheduleFailed)
			return
		}
		if changed {
			worker.Schedule(repositorySource{r})
		}
	}}})
}

// autoBackupOff reports a committed generation whose automatic backup switch is
// stored as false. Like Python, such a commit schedules nothing, so the two
// complete documents are not encoded and compared. A missing, null or
// mistyped switch keeps the comparison; the worker reads the settings again.
func autoBackupOff(current *model.Document) bool {
	return current != nil && current.System.Backup.AutoBackup != nil && !*current.System.Backup.AutoBackup
}

// WaitIdle waits for automatic backups and their receipt/notification callbacks.
// Cancelling ctx stops only this wait, never a committed backup's I/O.
func WaitIdle(ctx context.Context) error { return automaticWorker.WaitIdle(ctx) }
