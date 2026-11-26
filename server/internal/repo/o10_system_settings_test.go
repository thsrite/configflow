package repo

import (
	"context"
	"runtime"
	"testing"

	"configflow/internal/model"
)

// o10StoreSystem stores a system section whose Agent carries a deployment
// history of the given size (synthetic values only).
func o10StoreSystem(t *testing.T, deployments int) *Repository {
	t.Helper()
	r := newRepo(t)
	source := reviewN811System(t, deployments)
	noError(t, r.UpdateSystem(bg, func(_ context.Context, s *model.System) error {
		s.SystemConfig = source.SystemConfig
		s.Agents = source.Agents
		s.Agents[0].ProfileID = "default"
		s.Backup.WebDAVURL = "https://dav.example.com/backup"
		s.Backup.WebDAVUsername = "synthetic-user"
		return nil
	}))
	return r
}

func o10ReadBytes(t *testing.T, read func() error) uint64 {
	t.Helper()
	noError(t, read()) // warm the snapshot cache
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range 20 {
		noError(t, read())
	}
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / 20
}

// TestO10SettingsReadsMatchSystemAndSkipAgents: SystemConfig and Backup return
// the same records as System, owned by the caller, and their cost does not grow
// with the Agents' deployment history (System copies all of it).
func TestO10SettingsReadsMatchSystemAndSkipAgents(t *testing.T) {
	small, large := o10StoreSystem(t, 1), o10StoreSystem(t, 500)
	system := must(large.System(bg))(t)
	settings := must(large.SystemConfig(bg))(t)
	backup := must(large.Backup(bg))(t)
	tests := []struct {
		name      string
		got, want any
	}{
		{"system_config", settings, &system.SystemConfig},
		{"backup", backup, &system.Backup},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, want := must(model.Marshal(tt.got))(t), must(model.Marshal(tt.want))(t)
			if string(got) != string(want) {
				t.Fatalf("got %s, want %s", got, want)
			}
		})
	}
	if settings.RuleProxyToken != "synthetic-rule-token" || backup.WebDAVUsername != "synthetic-user" {
		t.Fatalf("unexpected values: %q %q", settings.RuleProxyToken, backup.WebDAVUsername)
	}

	settings.RuleProxyToken = "changed"
	backup.WebDAVUsername = "changed"
	if again := must(large.SystemConfig(bg))(t); again.RuleProxyToken != "synthetic-rule-token" {
		t.Fatal("a caller's change to SystemConfig reached the repository")
	}
	if again := must(large.Backup(bg))(t); again.WebDAVUsername != "synthetic-user" {
		t.Fatal("a caller's change to Backup reached the repository")
	}

	// The test-only View guard encodes the whole document around every View;
	// measure the reads themselves.
	small.SetViewGuard(nil)
	large.SetViewGuard(nil)
	reads := []struct {
		name string
		read func(*Repository) func() error
	}{
		{"SystemConfig", func(r *Repository) func() error {
			return func() error { _, err := r.SystemConfig(bg); return err }
		}},
		{"Backup", func(r *Repository) func() error {
			return func() error { _, err := r.Backup(bg); return err }
		}},
	}
	for _, tt := range reads {
		t.Run(tt.name+"/history", func(t *testing.T) {
			smallBytes, largeBytes := o10ReadBytes(t, tt.read(small)), o10ReadBytes(t, tt.read(large))
			// The history is ~0.5 MB; copying it even once per call exceeds this bound.
			if largeBytes > smallBytes+64*1024 {
				t.Fatalf("%s allocates %d bytes with a large history (%d with a small one)", tt.name, largeBytes, smallBytes)
			}
		})
	}
	// Negative control: System does copy the history, so the bound above is meaningful.
	systemSmall := o10ReadBytes(t, func() error { _, err := small.System(bg); return err })
	systemLarge := o10ReadBytes(t, func() error { _, err := large.System(bg); return err })
	if systemLarge < systemSmall+512*1024 {
		t.Fatalf("control: System allocates %d bytes with a large history (%d small); the bound would not detect a full copy", systemLarge, systemSmall)
	}
}
