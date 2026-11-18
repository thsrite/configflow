package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/health"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
)

// Err captures the health owner's last pre-leaf check, then simulates preemption
// before jsonstore is entered. The leaf must recheck the real Runtime lifetime.
// Both save and update currently check entry, lock acquisition and publication.
// No production collaborator, clock, timeout or writer is replaced here.
type publicationContext struct {
	context.Context
	checks int
	at     int
	pause  func()
}

func (c *publicationContext) Err() error {
	err := c.Context.Err()
	c.checks++
	if c.checks == c.at {
		c.pause()
	}
	return err
}

func TestDiscoveryJSONStoreOwnerLifetime(t *testing.T) {
	for _, operation := range []string{"probe", "region"} {
		for _, phase := range []string{"already-stopped", "shutdown-before-leaf", "disconnect-before-leaf"} {
			t.Run(operation+"/"+phase, func(t *testing.T) {
				r := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
				r.Start(context.Background())
				t.Cleanup(func() { r.Shutdown(context.Background()) })
				repository, err := repo.Open(t.TempDir(), repo.Options{})
				if err != nil {
					t.Fatal(err)
				}
				request, disconnect := context.WithCancel(r.BindRequest(repo.WithRepository(context.Background(), repository)))
				t.Cleanup(disconnect)
				owner := lifecycle.Detach(request)
				probe, err := health.GetProbeStore(owner)
				if err != nil {
					t.Fatal(err)
				}
				region, err := health.GetRegionStore(owner)
				if err != nil {
					t.Fatal(err)
				}
				path := probe.Path("synthetic")
				before := `{"hosts":{},"opaque":"keep"}`
				if operation == "region" {
					path = region.Path("synthetic")
					before = `{"services":null,"domains":{},"websites":{},"opaque":"keep"}`
				}
				// Stores resolve their own production directories; create only the selected parent.
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(before), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path+".bak", []byte(before), 0600); err != nil {
					t.Fatal(err)
				}
				paused := false
				ctx := &publicationContext{Context: owner, at: 3, pause: func() {
					paused = true
					if phase == "disconnect-before-leaf" {
						disconnect()
					} else {
						r.Shutdown(context.Background())
					}
				}}
				if phase == "already-stopped" {
					r.Shutdown(context.Background())
				}
				if operation == "probe" {
					err = saveProbes(ctx, "synthetic", map[string]Probe{"example.com": {Host: "example.com", CheckedAt: timestamp(), Verdict: "usable"}})
				} else {
					err = editRegion(ctx, "synthetic", func(data *regionRecord) error { data.Services = json.RawMessage(`{"new":true}`); return nil })
				}
				if phase != "already-stopped" && !paused {
					t.Error("health publication boundary was not reached through production adapter")
				}
				shutdown := phase != "disconnect-before-leaf"
				if shutdown && !errors.Is(err, context.Canceled) {
					t.Errorf("shutdown returned %v", err)
				}
				if !shutdown && err != nil {
					t.Fatal(err)
				}
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if shutdown && string(raw) != before {
					t.Errorf("late publication: %s", raw)
				}
				if !shutdown {
					if string(raw) == before {
						t.Error("disconnect prevented publication")
					}
					var stored map[string]json.RawMessage
					if err := json.Unmarshal(raw, &stored); err != nil {
						t.Fatal(err)
					}
					if string(stored["opaque"]) != `"keep"` {
						t.Errorf("lost unknown sibling: %s", raw)
					}
					if operation == "probe" {
						got := probe.LoadEntries("synthetic", now())
						if _, ok := got.Get("example.com"); !ok {
							t.Error("probe was not persisted")
						}
					} else if string(region.LoadData("synthetic").Services) != `{"new":true}` {
						t.Error("region was not persisted")
					}
				}
				if got, err := os.ReadFile(path + ".bak"); err != nil || string(got) != before {
					t.Errorf("backup changed: %s %v", got, err)
				}
				if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("temp remains: %v", err)
				}
			})
		}
	}
}
