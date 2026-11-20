package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"configflow/internal/deploy"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

func TestBaselineWorkerIsolation(t *testing.T) {
	if data := os.Getenv("A14_BASELINE_TEST_COPY"); data != "" {
		os.Args = []string{"baseline", data, os.Getenv("A14_BASELINE_TEST_PROOF")}
		if err := run(); err != nil {
			t.Fatal(err)
		}
		return
	}
	root := filepath.Join(t.TempDir(), "run-synthetic")
	data := filepath.Join(root, "copies-synthetic", "go")
	proofDir := filepath.Join(root, "baseline-format")
	r, err := repo.OpenDefault(data)
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	old := pythonBaseline(t)
	m := old.Manifest()
	fingerprint, err := deploy.Fingerprint(m)
	if err != nil {
		t.Fatal(err)
	}
	digests, err := deploy.Digests(m)
	if err != nil {
		t.Fatal(err)
	}
	agent := testjson.Decode[model.Agent](t, map[string]any{
		"id": m.AgentID, "profile_id": m.ProfileID, "service_type": m.ServiceType, "config_version": old.ConfigVersion(),
		"version": "1.8.60-go", "deployment_method": "docker",
		"applied_content": map[string]any{
			"deployment_id": m.DeploymentID, "profile_id": m.ProfileID, "service_type": m.ServiceType,
			"config_path": m.ConfigPath, "config_version": old.ConfigVersion(), "fingerprint": fingerprint, "files": digests,
		},
	})
	err = r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Agents = []model.Agent{agent}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := r.Config(ctx, m.ProfileID, repo.ResourceRoots{})
	if err != nil {
		t.Fatal(err)
	}
	rawCfg, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	source, err := deploy.SourceFromJSON(rawCfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = deploy.NewReceipts(data).Capture(ctx, m.AgentID, old, source, "https://panel.example.com", nil); err != nil {
		t.Fatal(err)
	}
	before, err := r.Store().Load()
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("unshare", "--user", "--map-root-user", "--net", "bash", "-lc", `ip link set lo up && exec "$1" -test.run '^TestBaselineWorkerIsolation$' -test.v`, "baseline-child", executable)
	command.Env = append(os.Environ(), "A14_BASELINE_TEST_COPY="+data, "A14_BASELINE_TEST_PROOF="+proofDir, "PRODDIFF_BASELINE_MIGRATION=1", "DATA_DIR="+data, "LOG_FILE="+filepath.Join(proofDir, "worker.log"), "LOG_LEVEL=ERROR")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("isolated worker: %v\n%s", err, output)
	}
	after, err := r.Store().Load()
	if err != nil || before.Head != after.Head {
		t.Fatal("proof worker changed DB metadata", err)
	}
	var result struct {
		Converted int `json:"converted"`
	}
	raw, err := os.ReadFile(filepath.Join(proofDir, "summary.json"))
	if err != nil || json.Unmarshal(raw, &result) != nil || result.Converted != 1 {
		t.Fatal("missing conversion evidence", string(raw), err)
	}
	for _, name := range []string{"before.tar.gz", "after.tar.gz", "receipt-before.json", "receipt-after.json", "candidate-main.yaml", "proof.json"} {
		if _, err = os.Stat(filepath.Join(proofDir, hash([]byte(m.AgentID)), name)); err != nil {
			t.Fatal("proof artifact missing", name, err)
		}
	}
}
