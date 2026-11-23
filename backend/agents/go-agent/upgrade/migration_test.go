package upgrade

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testConfig(t *testing.T, manager string) (*Config, string) {
	t.Helper()
	root := t.TempDir()
	binary := filepath.Join(root, "agent")
	if err := os.WriteFile(binary, []byte("core"), 0755); err != nil {
		t.Fatal(err)
	}
	c := &Config{Path: filepath.Join(root, "config.json"), Values: map[string]any{
		"agent_id": "retained-id", "token": "retained-token", "server_url": "http://server.test", "agent_port": float64(8080),
		"config_path": filepath.Join(root, "core/config.yaml"), "service_type": "mihomo", "service_binary": binary,
		"restart_command": "systemctl restart custom-core.service", "unknown": map[string]any{"keep": true}}}
	if manager == "openrc" {
		c.Values["restart_command"] = "rc-service custom-core restart"
	}
	b, _ := json.Marshal(c.Values)
	if err := os.WriteFile(c.Path, b, 0600); err != nil {
		t.Fatal(err)
	}
	return c, root
}
func TestMigrationPreservesAndRestores(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		t.Run(manager, func(t *testing.T) {
			c, root := testConfig(t, manager)
			original, _ := os.ReadFile(c.Path)
			extra := filepath.Join(root, "etc/conf.d/custom-core")
			if manager == "openrc" {
				os.MkdirAll(filepath.Dir(extra), 0755)
				os.WriteFile(extra, []byte("rc_need=\"custom-dependency\"\n"), 0640)
			}
			plan, err := MigrationPlan(c, filepath.Join(root, "agent"), root)
			if err != nil {
				t.Fatal(err)
			}
			var snapshots []Snapshot
			for _, change := range plan {
				snap, err := Capture(change.Path)
				if err != nil {
					t.Fatal(err)
				}
				snapshots = append(snapshots, snap)
			}
			if err := ApplyChanges(plan); err != nil {
				t.Fatal(err)
			}
			migrated, err := ReadConfig(c.Path)
			if err != nil {
				t.Fatal(err)
			}
			if migrated.String("agent_id") != "retained-id" || migrated.String("token") != "retained-token" || migrated.Values["unknown"].(map[string]any)["keep"] != true {
				t.Fatal("lost identity/unknown fields")
			}
			second, err := MigrationPlan(migrated, filepath.Join(root, "agent"), root)
			if err != nil {
				t.Fatal(err)
			}
			for i := range plan {
				if !bytes.Equal(plan[i].Data, second[i].Data) {
					t.Fatalf("migration not idempotent: %s", plan[i].Path)
				}
			}
			if manager == "openrc" {
				b, _ := os.ReadFile(extra)
				if !strings.Contains(string(b), "custom-dependency") {
					t.Fatal("lost custom OpenRC settings")
				}
			}
			if err := Restore(snapshots); err != nil {
				t.Fatal(err)
			}
			restored, _ := os.ReadFile(c.Path)
			if !bytes.Equal(original, restored) {
				t.Fatal("rollback did not restore config bytes")
			}
			for _, snap := range snapshots {
				if !snap.Existed {
					if _, err := os.Stat(snap.Path); !os.IsNotExist(err) {
						t.Fatal("new startup file survived rollback")
					}
				}
			}
		})
	}
}
func TestRejectAmbiguousLifecycleAndSymlinks(t *testing.T) {
	c, root := testConfig(t, "systemd")
	c.Values["restart_command"] = "sh -c 'systemctl restart custom-core'"
	if _, err := MigrationPlan(c, filepath.Join(root, "agent"), root); err == nil {
		t.Fatal("guessed custom command")
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(c.Path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(link); err == nil {
		t.Fatal("accepted symlink")
	}
	c.Values["deployment_method"] = "docker"
	if err := c.InferLifecycle(); err == nil {
		t.Fatal("accepted Docker binary upgrade")
	}
}
func TestDownloadIntegrityAndFailures(t *testing.T) {
	body := []byte("a complete executable fixture")
	h := sha256.Sum256(body)
	for _, tc := range []struct {
		name    string
		code    int
		data    []byte
		success bool
	}{{"valid", 200, body, true}, {"corrupt", 200, []byte("wrong"), false}, {"too-large", 200, append(append([]byte{}, body...), 1), false}, {"server-error", 503, body, false}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.code); w.Write(tc.data) }))
			defer server.Close()
			c := &Config{Values: map[string]any{"server_url": server.URL}}
			err := download(c, &Job{Digest: hex.EncodeToString(h[:]), Size: int64(len(body))}, filepath.Join(t.TempDir(), "candidate"))
			if (err == nil) != tc.success {
				t.Fatalf("unexpected integrity result: %v", err)
			}
		})
	}
}
func TestDurableStatusOwnershipAndLock(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "agent")
	job := Job{Status: Status{ID: "1234567890123456", AgentID: "owner"}}
	if err := job.save(CurrentPath(binary), "checking"); err != nil {
		t.Fatal(err)
	}
	if !Pending(binary) {
		t.Fatal("lost pending state")
	}
	if _, err := ReadStatus(binary, "other"); !os.IsNotExist(err) {
		t.Fatal("exposed another identity's status")
	}
	release, err := lock(filepath.Join(root, "lock"))
	if err != nil {
		t.Fatal(err)
	}
	if next, err := lock(filepath.Join(root, "lock")); err == nil {
		next()
		t.Fatal("allowed concurrent worker")
	}
	release()
	if err := job.save(CurrentPath(binary), "rollback_failed"); err != nil {
		t.Fatal(err)
	}
	if !Pending(binary) {
		t.Fatal("allowed update over failed rollback")
	}
}

func TestPrepareIdempotentContentAndRecoveryBlock(t *testing.T) {
	c, root := testConfig(t, "systemd")
	binary := filepath.Join(root, "agent")
	req := Request{ID: "1234567890123456", Version: Version, SHA256: strings.Repeat("a", 64), Size: 12}
	job := Job{Status: Status{ID: req.ID, Version: req.Version, AgentID: "retained-id"}, Digest: req.SHA256, Size: req.Size}
	if err := job.save(CurrentPath(binary), "checking"); err != nil {
		t.Fatal(err)
	}
	if result, err := Prepare(c.Path, binary, req); err != nil || result.ID != req.ID {
		t.Fatalf("idempotent retry failed: %v", err)
	}
	req.Size++
	if _, err := Prepare(c.Path, binary, req); err == nil {
		t.Fatal("same ID accepted different content")
	}
	req.ID = "1234567890123457"
	if _, err := Prepare(c.Path, binary, req); err == nil {
		t.Fatal("second update accepted during pending work")
	}
	if err := job.save(CurrentPath(binary), "rollback_failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(c.Path, binary, req); err == nil {
		t.Fatal("second update accepted before recovery")
	}
}

func TestOldWorkerCannotOverwriteNextJob(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "agent")
	old := Job{Status: Status{ID: "1234567890123456", AgentID: "owner"}, Binary: binary}
	oldPath := filepath.Join(rootFor(binary), "job-"+old.ID+".json")
	if err := old.save(CurrentPath(binary), "queued"); err != nil {
		t.Fatal(err)
	}
	if err := old.save(oldPath, "succeeded"); err != nil {
		t.Fatal(err)
	}
	next := Job{Status: Status{ID: "2234567890123456", AgentID: "owner"}, Binary: binary}
	if err := next.save(CurrentPath(binary), "queued"); err != nil {
		t.Fatal(err)
	}
	if err := old.save(oldPath, "succeeded"); err != nil {
		t.Fatal(err)
	}
	current, err := loadJob(CurrentPath(binary))
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != next.ID || current.Status.Status != "queued" {
		t.Fatal("old worker corrupted current task")
	}
}

func TestUnreadableJournalBlocksMutations(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "agent")
	if Pending(binary) {
		t.Fatal("missing journal blocked fresh installation")
	}
	if err := AtomicWrite(CurrentPath(binary), []byte("broken journal"), 0600); err != nil {
		t.Fatal(err)
	}
	if !Pending(binary) {
		t.Fatal("unreadable journal allowed service mutations")
	}
}
