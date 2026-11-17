package health

// Regression tests of the adversarial review of C16 (PROGRESS-config.md,
// "Review C16").

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// os.remove is unlink(2): a directory where an Agent's store file should be
// (empty or not) fails with IsADirectoryError, and the DELETE route answers
// 500 with str(exc). Go's os.Remove removed an empty directory and reported
// ENOTEMPTY in Go's wording for a full one. Python (3.11, Linux):
// "[Errno 21] Is a directory: '<path>'" for both.
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
	}{{"probes", deleteProbeStore, true}, {"probes", deleteProbeStore, false}, {"regions", deleteRegionStore, true},
		{"regions", deleteRegionStore, false}} {
		path := filepath.Join(r.DataDir(), c.dir, "a_b.json")
		os.RemoveAll(path)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if !c.empty {
			os.WriteFile(filepath.Join(path, "x"), []byte("x"), 0o644)
		}
		err := c.del(ctx, "a/b")
		if want := "[Errno 21] Is a directory: " + jsonx.Repr(path); err == nil || err.Error() != want {
			t.Errorf("%s empty=%v: %v, want %s", c.dir, c.empty, err, want)
		}
		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("%s empty=%v: directory removed", c.dir, c.empty)
		}
		os.RemoveAll(path)
		if err := c.del(ctx, "a/b"); err != nil {
			t.Errorf("missing file: %v", err)
		}
	}
}

// A test started by a request finishes (worker not killed, results
// stored) when the request context is cancelled; Python's request thread is
// never interrupted.
func TestReviewCancelledRequestFinishesTheLatencyTest(t *testing.T) {
	// Sequential: installs the process-wide probe Env for all three runs.
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	base := repo.WithRepository(context.Background(), r)
	if _, err := r.UpdateSharedTransaction(base, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("nodes", []any{jsonx.Obj("id", "n1", "name", "A", "type", "ss", "server", "a.test", "port", jsonx.Int(1),
			"cipher", "aes-128-gcm", "password", "p")})
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "probe.sh")
	os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\nsleep 0.3\nprintf '%s' '{\"version\": 4, \"results\": [{\"name\": \"[\\\"node\\\",\\\"n1\\\"]\", \"latency\": 42}]}'\n"), 0o755)
	t.Cleanup(SetEnv(&Env{LookupEnv: func(string) (string, bool) { return "", false }, Environ: os.Environ,
		Runner: ExecRunner{}, Now: time.Now, ProbeBinary: func() (string, error) { return script, nil }}))
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithCancel(base)
		go func() { time.Sleep(50 * time.Millisecond); cancel() }()
		outcome, err := RunTests(ctx, nil, nil)
		if err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		if got := outcome.GetObject("results").GetObject(`["node","n1"]`).Value("latency"); !jsonx.Equal(got, jsonx.Int(42)) {
			t.Fatalf("round %d: %s", i, jsonx.Repr(outcome))
		}
		if stored := LoadResults(base).GetObject(`["node","n1"]`); len(stored.GetList("history")) != i+1 {
			t.Fatalf("round %d: stored %s", i, jsonx.Repr(stored))
		}
	}
}
