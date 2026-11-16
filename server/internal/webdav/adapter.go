package webdav

// This file adapts the repository's legacy documents; the transport, worker and
// retention logic use only Go-native types.
import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
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
	document, err := s.repository.ExportAll(context.WithoutCancel(ctx), false)
	if err != nil {
		return Snapshot{}, err
	}
	data, err := jsonx.Indent2(document)
	if err != nil {
		return Snapshot{}, err
	}
	var settings Settings
	backup, err := jsonx.Compact(document.GetObject("system").Value("backup"))
	if err != nil {
		return Snapshot{}, err
	}
	if err := json.Unmarshal(backup, &settings); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Document: data, Settings: settings, DataDir: s.repository.DataDir(), repository: s.repository}, nil
}

// Capture exports the complete configuration for a manual backup.
func Capture(ctx context.Context) (Snapshot, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	return repositorySource{r}.Snapshot(ctx)
}

// Run records one manual or automatic upload in the shared task receipt store.
func Run(ctx context.Context, snapshot Snapshot, automatic bool) (Result, error) {
	ctx = context.WithoutCancel(ctx)
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
	_, recordErr := store.Record("webdav_backup", taskstatus.Result{OK: err == nil, StartedAt: started.UTC().Format("2006-01-02T15:04:05Z"), Trigger: trigger, DurationMs: float64(time.Since(clock).Milliseconds()), Error: detail})
	if err != nil {
		return Result{}, err
	}
	return result, recordErr
}

var automaticWorker = NewWorker(time.Second, func(ctx context.Context, snapshot Snapshot) error { _, err := Run(ctx, snapshot, true); return err })
var wireOnce sync.Once

// Wire installs the post-commit hook and bounded shutdown handling.
func Wire() {
	registerHook(automaticWorker)
	wireOnce.Do(func() {
		lifecycle.OnShutdown("webdav-auto-backup", func(ctx context.Context) { _ = automaticWorker.Close(ctx) })
	})
}
func registerHook(worker *Worker) {
	repo.RegisterHooks(repo.Hooks{ConfigurationCommitted: func(r *repo.Repository, previous, current *jsonx.Object) {
		// A failed scheduling/serialization step must never change commit success.
		defer func() {
			if recover() != nil {
				logger.Warning(msgScheduleFailed)
			}
		}()
		before, err := jsonx.Compact(previous)
		if err != nil {
			logger.Warning(msgScheduleFailed)
			return
		}
		after, err := jsonx.Compact(current)
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
	}})
}

// WaitIdle waits for automatic backups and their receipt/notification callbacks.
// Cancelling ctx stops only this wait, never a committed backup's I/O.
func WaitIdle(ctx context.Context) error { return automaticWorker.WaitIdle(ctx) }
