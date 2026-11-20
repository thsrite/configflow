package health

// Regression tests of the adversarial review of C16 (PROGRESS-config.md,
// "Review C16").

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

func TestReviewStoreDeleteOfADirectoryFailsLikeOSRemove(t *testing.T) {
	t.Parallel()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	for _, c := range []struct {
		dir   string
		del   func(context.Context, string) error
		empty bool
	}{
		{"probes", deleteProbeStore, true},
		{"probes", deleteProbeStore, false},
		{"regions", deleteRegionStore, true},
		{"regions", deleteRegionStore, false},
	} {
		path := filepath.Join(r.DataDir(), c.dir, "a_b.json")
		_ = os.RemoveAll(path)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if !c.empty {
			_ = os.WriteFile(filepath.Join(path, "x"), []byte("x"), 0o644)
		}
		err := c.del(ctx, "a/b")
		if !errors.Is(err, syscall.EISDIR) {
			t.Errorf("%s empty=%v: got %v, want EISDIR", c.dir, c.empty, err)
		}
		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("%s empty=%v: directory removed", c.dir, c.empty)
		}
		_ = os.RemoveAll(path)
		if err := c.del(ctx, "a/b"); err != nil {
			t.Errorf("missing file: %v", err)
		}
	}
}

func TestReviewCancelledRequestFinishesTheLatencyTest(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	base := repo.WithRepository(context.Background(), r)
	if err := r.UpdateShared(base, func(_ context.Context, shared *model.Shared) error {
		shared.Nodes = []model.Node{testjson.Decode[model.Node](t, map[string]any{
			"id": "n1", "name": "A", "type": "ss", "server": "a.test", "port": 1,
			"cipher": "aes-128-gcm", "password": "p",
		})}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "probe.sh")
	_ = os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\nsleep 0.3\nprintf '%s' '{\"version\": 4, \"results\": [{\"name\": \"[\\\"node\\\",\\\"n1\\\"]\", \"latency\": 42}]}'\n"), 0o755)
	t.Cleanup(SetProbeEnv(&ProbeEnv{
		LookupEnv:     func(string) (string, bool) { return "", false },
		Environ:       os.Environ,
		Runner:        ExecRunner{},
		Now:           time.Now,
		ProbeBinary:   func() (string, error) { return script, nil },
		Collaborators: defaultCollaborators(nil),
	}))
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithCancel(base)
		go func() { time.Sleep(50 * time.Millisecond); cancel() }()
		outcome, err := RunLatencyTests(ctx, nil, nil)
		if err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		outcomeRaw, err := json.Marshal(outcome)
		if err != nil {
			t.Fatal(err)
		}
		parsedOutcome, err := testjson.Parse(outcomeRaw)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := testjson.Path(parsedOutcome, "results", `["node","n1"]`, "latency"); !testjson.Equal(got, 42) {
			t.Fatalf("round %d: %s", i, outcomeRaw)
		}
		storedRaw, err := json.Marshal(LoadLatencyResults(base))
		if err != nil {
			t.Fatal(err)
		}
		parsedStored, err := testjson.Parse(storedRaw)
		if err != nil {
			t.Fatal(err)
		}
		hist, _ := testjson.Path(parsedStored, `["node","n1"]`, "history")
		if list, _ := hist.([]any); len(list) != i+1 {
			t.Fatalf("round %d: stored %s", i, storedRaw)
		}
	}
}
