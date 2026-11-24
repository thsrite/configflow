package monitor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

func TestNativeStatisticsDisconnectFinishesAndShutdownDoesNotWrite(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown-during-snapshot"}[shutdown], func(t *testing.T) {
			e := newPubEnv(t)
			saved := readFile(t, e.store.Path("one"))
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(context.Background())
			defer runtime.Shutdown(context.Background())
			bound := runtime.BindRequest(context.Background())
			request, cancel := context.WithCancel(bound)
			cancel()
			if shutdown {
				e.f.hook = func(_ *model.Agent, endpoint string, _ query) {
					if endpoint == "overview" {
						runtime.Shutdown(context.Background())
					}
				}
			}
			_, err := ResetStatistics(request, e.m, "one", []byte(`{}`))
			after := readFile(t, e.store.Path("one"))
			if shutdown {
				if !errors.Is(err, context.Canceled) || !bytes.Equal(saved, after) {
					t.Fatalf("shutdown changed statistics: %v", err)
				}
				calls := len(e.f.endpoints())
				if _, err := ReadOverview(bound, e.m, "one", nil); !errors.Is(err, context.Canceled) || len(e.f.endpoints()) != calls {
					t.Fatal("stopped generation collected another snapshot", err)
				}
			} else if err != nil || bytes.Equal(saved, after) {
				t.Fatalf("disconnect canceled reset: %v", err)
			}
		})
	}
}

func TestNativeRankingSQLRollsBackAtShutdown(t *testing.T) {
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runtime.Start(context.Background())
	defer runtime.Shutdown(context.Background())
	bound := lifecycle.Detach(runtime.BindRequest(context.Background()))
	store := NewRankingStore(t.TempDir())
	slot := &Slot{Records: 1, Rows: map[string]*Counts{"clients": {Keys: []string{"192.0.2.1"}, Values: map[string]int64{"192.0.2.1": 1}}}}
	request, cancel := context.WithCancel(bound)
	cancel()
	if added, err := store.StoreSlot(lifecycle.Detach(request), "dns", "r", 0, slot); err != nil || !added {
		t.Fatalf("disconnect canceled slot: %v", err)
	}
	err := store.with(bound, func(c *rankConn) error {
		if _, err := c.write("INSERT INTO ranking_slots (agent_id,recorder,start) VALUES (?, ?, ?)", "dns", "r", HistorySlot); err != nil {
			return err
		}
		runtime.Shutdown(context.Background())
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown commit returned %v", err)
	}
	fetched, err := store.Fetched(t.Context(), "dns", "r", []int64{0, HistorySlot})
	if err != nil || !fetched[0] || fetched[HistorySlot] {
		t.Fatalf("shutdown SQL committed: %v %v", fetched, err)
	}
	empty := NewRankingStore(t.TempDir())
	if _, err := empty.StoreSlot(bound, "dns", "r", 0, slot); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(empty.Path); !os.IsNotExist(err) {
		t.Fatal("shutdown created ranking database", err)
	}
	state := NewStateStore(t.TempDir())
	if _, err := state.ReadState(bound, "dns"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(state.Directory); !os.IsNotExist(err) {
		t.Fatal("shutdown created statistics directory", err)
	}
}

func TestCoreVersionCacheDoesNotPublishAfterShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown"}[shutdown], func(t *testing.T) {
			installLatest(t)
			m, client := panelManager(t, panelAgentRecord("version", "1.8.60-go"))
			client.add(jsonResponse(map[string]any{"version": "v1.19.1"}, 200))
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(context.Background())
			defer runtime.Shutdown(context.Background())
			bound := runtime.BindRequest(context.Background())
			request, cancel := context.WithCancel(bound)
			cancel()
			previous := latestCoreVersion
			t.Cleanup(func() { latestCoreVersion = previous })
			latestCoreVersion = func(ctx context.Context, _ string, _ *model.SystemConfig) (string, error) {
				if ctx.Err() != nil {
					t.Fatal("fetch retained browser cancellation")
				}
				if shutdown {
					runtime.Shutdown(context.Background())
				}
				return "v1.19.2", nil
			}
			_, err := CoreUpdateCheck(request, m, "mihomo", false)
			if shutdown {
				if !errors.Is(err, context.Canceled) || coreCachedOf("release") != nil {
					t.Fatalf("shutdown published cached version: %v", err)
				}
			} else if err != nil || coreCachedOf("release") == nil {
				t.Fatal("disconnect canceled shared version check", err)
			}
		})
	}
}
