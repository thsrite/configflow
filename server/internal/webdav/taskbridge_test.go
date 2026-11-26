package webdav

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

func TestBackupTaskUsesStoredConfigurationAndRuntimeAdmission(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect completes", true: "shutdown rejects"}[shutdown], func(t *testing.T) {
			rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: time.Millisecond})
			rt.Start(context.Background())
			t.Cleanup(func() { rt.Shutdown(context.Background()) })
			f, server := newDAV(t)
			r, err := repo.Open(t.TempDir(), repo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			svc := NewService(server.Client())
			svc.Repo = r
			tk := tasks.NewRegistry(r, rt)
			t.Cleanup(svc.registerTasks(tk))
			ctx := repo.WithScope(context.Background())
			settings, err := json.Marshal(fixtureSettings(server))
			if err != nil {
				t.Fatal(err)
			}
			err = r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
				return system.SetJSON("backup", settings)
			})
			if err != nil {
				t.Fatal(err)
			}
			request, cancel := context.WithCancel(rt.BindRequest(ctx))
			defer cancel()
			if shutdown {
				rt.Shutdown(context.Background())
			} else {
				cancel()
			}
			err = tk.RunNow(request, tasks.BackupTaskKey)
			if shutdown {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(5 * time.Second)
				for tasks.IsBackground(tasks.BackupTaskKey) && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if tasks.IsBackground(tasks.BackupTaskKey) {
					t.Fatal("backup never completed")
				}
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			statuses, err := taskstatus.New(r.DataDir()).Read()
			if err != nil {
				t.Fatal(err)
			}
			if shutdown {
				if f.uploads != 0 || len(statuses) != 0 {
					t.Fatal("stopped generation uploaded or recorded a result")
				}
			} else {
				entry := statuses.Entry(tasks.BackupTaskKey)
				if f.uploads != 1 || entry == nil || entry.LastStatus == nil || *entry.LastStatus != "success" {
					t.Fatal("backup/receipt missing", f.uploads, statuses)
				}
				for _, data := range f.files {
					var document struct {
						System struct {
							Backup Settings `json:"backup"`
						} `json:"system"`
					}
					if err := json.Unmarshal(data, &document); err != nil || document.System.Backup.Username != "fixture-user" {
						t.Fatal("not a full stored backup", err)
					}
				}
			}
		})
	}
}
