package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeStubbornRCService models OpenRC when start-stop-daemon gives up: stop
// prints the real failure and exits 1 while the daemon keeps running.
func fakeStubbornRCService(t *testing.T, cfg *Config) string {
	t.Helper()
	bin := t.TempDir()
	cfg.ServiceManager, cfg.ServiceUnit = "openrc", "test-core"
	script := "#!/bin/sh\necho \"$2\" >> " + filepath.Join(bin, "calls.log") + "\n" +
		"if [ \"$2\" = status ]; then cat " + filepath.Join(bin, "status.out") + "; exit $(cat " + filepath.Join(bin, "status.code") + "); fi\n" +
		"if [ \"$2\" = stop ]; then printf ' * start-stop-daemon: 1 process refused to stop\\n * ERROR: test-core failed to stop\\n'; exit 1; fi\n" +
		"if [ \"$2\" = zap ]; then printf ' * status: stopped\\n' > " + filepath.Join(bin, "status.out") + "; echo 3 > " + filepath.Join(bin, "status.code") + "; fi\nexit 0\n"
	writeActivationFixture(t, filepath.Join(bin, "rc-service"), []byte(script), 0700)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	setRCStatus(t, bin, " * status: started", "0")
	return bin
}

func shortOpenRCGrace(t *testing.T, grace time.Duration) {
	oldGrace, oldPoll := openrcStopGrace, openrcStopPoll
	openrcStopGrace, openrcStopPoll = grace, 20*time.Millisecond
	t.Cleanup(func() { openrcStopGrace, openrcStopPoll = oldGrace, oldPoll })
}

func TestOpenRCStopWaitsForCoreThatExitsAfterStopGivesUp(t *testing.T) {
	cfg := testDeploymentConfig(t)
	bin := fakeStubbornRCService(t, cfg)
	shortOpenRCGrace(t, 5*time.Second)
	go func() {
		time.Sleep(200 * time.Millisecond)
		// The daemon exits once its reload ends; OpenRC still records it as started.
		setRCStatus(t, bin, " * status: crashed", "32")
	}()
	if err := stopService(cfg); err != nil {
		t.Fatalf("a core that exits within the grace period is stopped: %v", err)
	}
	if err := startService(cfg); err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(filepath.Join(bin, "calls.log"))
	if !strings.HasSuffix(string(calls), "status\nzap\nstart\n") {
		t.Fatalf("start must zap the crashed record first: %q", calls)
	}
}

func TestOpenRCStopStillFailsWhenCoreNeverExits(t *testing.T) {
	cfg := testDeploymentConfig(t)
	fakeStubbornRCService(t, cfg)
	shortOpenRCGrace(t, 200*time.Millisecond)
	err := stopService(cfg)
	if err == nil {
		t.Fatal("a core that never exits must fail the stop")
	}
	summary, details := deploymentErrorFields(err)
	if !strings.Contains(summary, "service remains running after stop") || !strings.Contains(details, "refused to stop") || !strings.Contains(details, "仍未退出") {
		t.Fatalf("stop failure must keep the OpenRC output and say it waited: %q %q", summary, details)
	}
}

// waitRecovery waits until the background recovery releases the operation
// lock; its state starts as the old rollback_failed, so read it afterwards.
func waitRecovery(t *testing.T, cfg *Config, id string) *DeploymentState {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if release, err := AcquireServiceOperation(cfg); err == nil {
			release()
			s, err := loadDeployment(cfg, id)
			if err != nil {
				t.Fatal(err)
			}
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("recovery did not release the operation lock")
	return nil
}

func requestRecover(cfg *Config, id string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	DeploymentHandler(cfg)(w, httptest.NewRequest(http.MethodPost, "/api/deployments/"+id+"/recover", nil))
	return w
}

func TestRecoverEndpointRecoversFailedRollback(t *testing.T) {
	cfg, _, render := testOxiReloadDeployment(t)
	cfg.StartCommand = "printf 'ERROR fallback startup rejected\\n'; exit 1"
	state := submitBundle(t, cfg, "recover-me", map[string][]byte{"config.yaml": []byte(render("# reject-reload\n"))}, true)
	if state.Status != "rollback_failed" {
		t.Fatalf("setup: expected rollback_failed, got %+v", state)
	}
	if err := deploymentBlocked(cfg); err == nil {
		t.Fatal("setup: a failed rollback must block new deployments")
	}
	live := []byte(render("# live\n"))
	// The operator fixes the cause; recovery restores the backup and starts again.
	cfg.StartCommand = "printf 'start\\n' >> lifecycle-actions; touch running"
	w := requestRecover(cfg, "recover-me")
	if w.Code != http.StatusAccepted {
		t.Fatalf("recover must be accepted: %d %s", w.Code, w.Body)
	}
	var accepted map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &accepted)
	if accepted["recovering"] != true || accepted["status"] != "rolling_back" {
		t.Fatalf("unexpected acceptance: %s", w.Body)
	}
	s := waitRecovery(t, cfg, "recover-me")
	if s.Status != "rolled_back" || s.RollbackError != "" {
		t.Fatalf("recovery did not complete: %+v", s)
	}
	got, _ := os.ReadFile(cfg.ConfigPath)
	if string(got) != string(live) {
		t.Fatal("recovery must restore the configuration from before the deployment")
	}
	if err := deploymentBlocked(cfg); err != nil {
		t.Fatalf("recovered deployment must not block new ones: %v", err)
	}
	if w := requestRecover(cfg, "recover-me"); w.Code != http.StatusConflict {
		t.Fatalf("a recovered deployment needs no recovery: %d %s", w.Code, w.Body)
	}
}

func TestRecoverEndpointRejectsUnknownAndBusy(t *testing.T) {
	cfg, _, render := testOxiReloadDeployment(t)
	if w := requestRecover(cfg, "missing"); w.Code != http.StatusNotFound {
		t.Fatalf("unknown deployment: %d", w.Code)
	}
	state := submitBundle(t, cfg, "fine", map[string][]byte{"config.yaml": []byte(render("# candidate\n"))}, true)
	if state.Status != "succeeded" {
		t.Fatalf("setup: %+v", state)
	}
	if w := requestRecover(cfg, "fine"); w.Code != http.StatusConflict {
		t.Fatalf("a succeeded deployment needs no recovery: %d %s", w.Code, w.Body)
	}
	release, err := AcquireServiceOperation(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if w := requestRecover(cfg, "fine"); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "another service operation") {
		t.Fatalf("recovery must not run beside another operation: %d %s", w.Code, w.Body)
	}
}
