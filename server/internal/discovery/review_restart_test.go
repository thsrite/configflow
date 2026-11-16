package discovery

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"configflow/internal/repo"
)

func TestReviewRestartDropsPollingJobsButKeepsProbeStore(t *testing.T) {
	if dir := os.Getenv("CONFIGFLOW_C37_RESTART_DIR"); dir != "" {
		r, err := repo.OpenDefault(dir)
		if err != nil {
			t.Fatal(err)
		}
		ctx := repo.WithRepository(context.Background(), r)
		if runningJob("") != nil || GetJob(os.Getenv("CONFIGFLOW_C37_RESTART_JOB"), "default") != nil {
			t.Fatal("new process restored a phantom running job")
		}
		saved, err := probes(ctx, []Agent{{ID: "a1"}})
		if err != nil || saved["example.com"].Host != "example.com" {
			t.Fatalf("restart lost persisted probe: %v", err)
		}
		return
	}
	ctx := jobFixture(t)
	r, _ := repo.GetRepository(ctx)
	started, release := make(chan struct{}), make(chan struct{})
	j, _, err := startJob(ctx, "default", "probe", false, func(worker context.Context, _ func(int, int)) (outcome, error) {
		err := saveProbes(worker, "a1", map[string]Probe{"example.com": {Host: "example.com", CheckedAt: timestamp()}})
		close(started)
		<-release
		return outcome{}, err
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	defer func() { close(release); awaitJob(t, j.ID, "default") }()
	child, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(child, os.Args[0], "-test.run=^TestReviewRestartDropsPollingJobsButKeepsProbeStore$")
	cmd.Env = append(os.Environ(), "CONFIGFLOW_C37_RESTART_DIR="+r.DataDir(), "CONFIGFLOW_C37_RESTART_JOB="+j.ID)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fresh process: %v\n%s", err, output)
	}
}
