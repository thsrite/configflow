package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func requestRollback(cfg *Config, id string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	DeploymentHandler(cfg)(w, httptest.NewRequest(http.MethodPost, "/api/deployments/"+id+"/rollback", nil))
	return w
}

func waitRollback(t *testing.T, cfg *Config, id string) *DeploymentState {
	t.Helper()
	return waitDeploymentState(t, cfg, id, true, func(s *DeploymentState) bool {
		return s.Status == "rolled_back" || s.Status == "rollback_failed"
	}, "manual rollback "+id)
}

func TestManualRollbackRestoresCurrentRevisionOnlyAndKeepsState(t *testing.T) {
	cfg := testDeploymentConfig(t)
	root := filepath.Dir(cfg.ConfigPath)
	first := submitBundle(t, cfg, "manual-first", map[string][]byte{"config.yaml": []byte("mode: rule\n"), "rules/old": []byte("old")}, true)
	if first.Status != "succeeded" {
		t.Fatal(first)
	}
	before, _ := os.ReadFile(cfg.ConfigPath)
	os.MkdirAll(filepath.Join(root, "state"), 0700)
	os.WriteFile(filepath.Join(root, "state/queries.db"), []byte("keep"), 0600)
	next := submitBundle(t, cfg, "manual-next", map[string][]byte{"config.yaml": []byte("mode: global\n"), "rules/new": []byte("new")}, true)
	if next.Status != "succeeded" {
		t.Fatal(next)
	}
	if w := requestRollback(cfg, "manual-first"); w.Code != 409 {
		t.Fatalf("stale rollback: %d %s", w.Code, w.Body)
	}
	w := requestRollback(cfg, "manual-next")
	if w.Code != 202 {
		t.Fatalf("manual rollback: %d %s", w.Code, w.Body)
	}
	result := waitRollback(t, cfg, "manual-next")
	if result.Status != "rolled_back" || !result.Success || !result.ManualRollback || result.ConfigVersion == "" {
		t.Fatalf("bad result: %+v", result)
	}
	after, _ := os.ReadFile(cfg.ConfigPath)
	if !bytes.Equal(before, after) {
		t.Fatal("configuration differs after rollback")
	}
	if got, _ := os.ReadFile(filepath.Join(root, "rules/old")); string(got) != "old" {
		t.Fatal("old rule not restored")
	}
	if _, err := os.Stat(filepath.Join(root, "rules/new")); !os.IsNotExist(err) {
		t.Fatal("new rule not removed")
	}
	if got, _ := os.ReadFile(filepath.Join(root, "state/queries.db")); string(got) != "keep" {
		t.Fatal("runtime state overwritten")
	}
	var active DeploymentManifest
	data, _ := os.ReadFile(filepath.Join(deploymentRoot(cfg), "active-manifest.json"))
	json.Unmarshal(data, &active)
	if active.DeploymentID != "manual-first" {
		t.Fatal("active manifest not restored")
	}
	if w := requestRollback(cfg, "manual-next"); w.Code != 200 {
		t.Fatal("rollback retry is not idempotent")
	}
}

func TestManualRollbackRefusesCorruptBackupAndLiveDriftBeforeStopping(t *testing.T) {
	for _, corruption := range []string{"backup", "live"} {
		t.Run(corruption, func(t *testing.T) {
			cfg := testDeploymentConfig(t)
			root := filepath.Dir(cfg.ConfigPath)
			os.WriteFile(cfg.ConfigPath, []byte("mode: old\n"), 0600)
			os.WriteFile(filepath.Join(root, "running"), nil, 0600)
			s := submitBundle(t, cfg, "manual-corrupt", map[string][]byte{"config.yaml": []byte("mode: rule\n")}, true)
			if s.Status != "succeeded" {
				t.Fatal(s)
			}
			path := cfg.ConfigPath
			if corruption == "backup" {
				path = filepath.Join(deploymentPath(cfg, s.DeploymentID), "backup/config.yaml")
			}
			os.WriteFile(path, []byte("mode: changed\n"), 0600)
			before, _ := os.ReadFile(cfg.ConfigPath)
			if w := requestRollback(cfg, s.DeploymentID); w.Code != 409 {
				t.Fatalf("accepted corrupt %s: %d", corruption, w.Code)
			}
			after, _ := os.ReadFile(cfg.ConfigPath)
			if !bytes.Equal(before, after) {
				t.Fatal("rejected rollback changed current configuration")
			}
			if _, err := os.Stat(filepath.Join(root, "running")); err != nil {
				t.Fatal("rejected rollback stopped healthy service")
			}
			current, _ := loadDeployment(cfg, s.DeploymentID)
			if current.Status != "succeeded" || current.ManualRollback {
				t.Fatal("rejection mutated successful journal")
			}
		})
	}
}

// Cores refresh HTTP rule providers on their own interval. That is not an
// external edit, so it must not block rolling the publication back.
func TestManualRollbackAcceptsCoreRefreshedRuleProvider(t *testing.T) {
	config := []byte("mode: rule\nrule-providers:\n  geo:\n    type: http\n    url: https://config.test/geo\n    path: ./rules/geo.list\n")
	for _, test := range []struct {
		name, path string
		want       int
	}{
		{"refreshed provider", "rules/geo.list", 202},
		{"edited unrelated rule", "rules/manual.list", 409},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := testDeploymentConfig(t)
			root := filepath.Dir(cfg.ConfigPath)
			first := submitBundle(t, cfg, "refresh-first", map[string][]byte{"config.yaml": []byte("mode: global\n")}, true)
			next := submitBundle(t, cfg, "refresh-next", map[string][]byte{"config.yaml": config,
				"rules/geo.list": []byte("DOMAIN,old.test"), "rules/manual.list": []byte("DOMAIN,keep.test")}, true)
			if first.Status != "succeeded" || next.Status != "succeeded" {
				t.Fatal(first, next)
			}
			os.WriteFile(filepath.Join(root, test.path), []byte("DOMAIN,new.test"), 0600)
			if w := requestRollback(cfg, "refresh-next"); w.Code != test.want {
				t.Fatalf("rollback after %s: %d %s", test.name, w.Code, w.Body)
			}
			if test.want == 202 {
				if result := waitRollback(t, cfg, "refresh-next"); result.Status != "rolled_back" {
					t.Fatalf("rollback did not complete: %+v", result)
				}
			}
		})
	}
}

func TestManualRollbackFailedHealthRetainsJournalAndRecovers(t *testing.T) {
	cfg := testDeploymentConfig(t)
	root := filepath.Dir(cfg.ConfigPath)
	os.WriteFile(cfg.ConfigPath, []byte("mode: old\n"), 0600)
	os.WriteFile(filepath.Join(root, "running"), nil, 0600)
	var unhealthy atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unhealthy.Load() {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	cfg.HealthURL = server.URL
	s := submitBundle(t, cfg, "manual-health", map[string][]byte{"config.yaml": []byte("mode: rule\n")}, true)
	if s.Status != "succeeded" {
		t.Fatal(s)
	}
	unhealthy.Store(true)
	if w := requestRollback(cfg, s.DeploymentID); w.Code != 202 {
		t.Fatalf("rollback not accepted: %d %s", w.Code, w.Body)
	}
	failed := waitRollback(t, cfg, s.DeploymentID)
	if failed.Status != "rollback_failed" || failed.Success || !failed.ManualRollback || failed.RollbackError == "" {
		t.Fatalf("failed health result: %+v", failed)
	}
	if err := deploymentBlocked(cfg); err == nil {
		t.Fatal("failed rollback must block another publication")
	}
	if got, _ := os.ReadFile(cfg.ConfigPath); string(got) != "mode: old\n" {
		t.Fatal("old files were not restored")
	}
	unhealthy.Store(false)
	if err := RecoverDeployments(cfg, true); err != nil {
		t.Fatal(err)
	}
	recovered, _ := loadDeployment(cfg, s.DeploymentID)
	if recovered.Status != "rolled_back" || !recovered.Success || !recovered.ManualRollback || recovered.RollbackError != "" {
		t.Fatalf("manual recovery result: %+v", recovered)
	}
}
