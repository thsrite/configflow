package routes

import (
	"agent/preflight"
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testDeploymentConfig(t *testing.T) *Config {
	t.Helper()
	// Lifecycle fixtures use in-process DNS/HTTP servers, not an installed core
	// owning those sockets. Keep them independent of the host /proc snapshot;
	// DNS rejection/activation tests replace this hook with explicit findings,
	// while the preflight package tests listener ownership and DNS on the wire.
	useDNSCheck(t, func(preflight.DNSCandidate, preflight.DNSOptions) preflight.Report { return nil })
	root := t.TempDir()
	binary := filepath.Join(root, "mihomo-test")
	if e := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0700); e != nil {
		t.Fatal(e)
	}
	cfg := &Config{AgentID: "test-agent", ServiceType: "mihomo", ConfigPath: filepath.Join(root, "config.yaml"), ServiceManager: "command", ServiceBinary: binary, StopCommand: "rm -f running", StartCommand: "touch running", StatusCommand: "test -f running", DeploymentHealthTimeout: 1}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := os.ReadFile(cfg.ConfigPath)
		if bytes.Contains(b, []byte("reject-start: true")) {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"version":"test"}`)
	}))
	t.Cleanup(server.Close)
	cfg.HealthURL = server.URL
	return cfg
}
func testBundle(t *testing.T, cfg *Config, id string, files map[string][]byte, edit func(*DeploymentManifest)) ([]byte, string) {
	t.Helper()
	m := DeploymentManifest{ProtocolVersion: 1, DeploymentID: id, AgentID: cfg.AgentID, ServiceType: cfg.ServiceType, ConfigPath: filepath.Base(cfg.ConfigPath)}
	for path, data := range files {
		h := sha256.Sum256(data)
		m.Files = append(m.Files, DeploymentFile{Path: path, Size: int64(len(data)), SHA256: hex.EncodeToString(h[:]), Role: "rule"})
	}
	if edit != nil {
		edit(&m)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	mb, _ := json.Marshal(m)
	all := map[string][]byte{"manifest.json": mb}
	for p, b := range files {
		all["files/"+p] = b
	}
	for p, b := range all {
		if e := tw.WriteHeader(&tar.Header{Name: p, Mode: 0600, Size: int64(len(b)), Typeflag: tar.TypeReg}); e != nil {
			t.Fatal(e)
		}
		tw.Write(b)
	}
	tw.Close()
	gz.Close()
	h := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(h[:])
}

const deploymentTestWaitTimeout = 60 * time.Second

// waitDeploymentState polls until loadDeployment(cfg, id) satisfies match and,
// when waitLock is true, the background worker has released the service
// operation lock.
func waitDeploymentState(t *testing.T, cfg *Config, id string, waitLock bool, match func(*DeploymentState) bool, label string) *DeploymentState {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), deploymentTestWaitTimeout)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	var last *DeploymentState
	for {
		if s, err := loadDeployment(cfg, id); err == nil {
			last = s
			if match(s) {
				if !waitLock {
					return s
				}
				if release, err := AcquireServiceOperation(cfg); err == nil {
					release()
					return s
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("%s timed out after %s (last state: %+v)", label, deploymentTestWaitTimeout, last)
			return nil
		case <-ticker.C:
		}
	}
}

func waitOperationLockReleased(t *testing.T, cfg *Config, label string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), deploymentTestWaitTimeout)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		if release, err := AcquireServiceOperation(cfg); err == nil {
			release()
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("%s timed out after %s waiting for operation lock", label, deploymentTestWaitTimeout)
			return
		case <-ticker.C:
		}
	}
}

func submitBundle(t *testing.T, cfg *Config, id string, files map[string][]byte, activate bool, edits ...func(*DeploymentManifest)) *DeploymentState {
	t.Helper()
	var edit func(*DeploymentManifest)
	if len(edits) > 0 {
		edit = edits[0]
	}
	body, digest := testBundle(t, cfg, id, files, edit)
	req := httptest.NewRequest(http.MethodPost, "/api/deployments", bytes.NewReader(body))
	req.Header.Set("X-Deployment-ID", id)
	req.Header.Set("X-Content-SHA256", digest)
	if !activate {
		req.Header.Set("X-Activate", "false")
	}
	w := httptest.NewRecorder()
	DeploymentUploadHandler(cfg)(w, req)
	if w.Code != 202 {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	return waitDeploymentState(t, cfg, id, true, func(s *DeploymentState) bool {
		switch s.Status {
		case "ready":
			return !activate
		case "succeeded", "failed", "rolled_back", "rollback_failed":
			return true
		default:
			return false
		}
	}, "deployment "+id)
}
func TestDeploymentPreservesBinaryEmptyAndUnmanagedFiles(t *testing.T) {
	cfg := testDeploymentConfig(t)
	root := filepath.Dir(cfg.ConfigPath)
	os.WriteFile(cfg.ConfigPath, []byte("mode: old\n"), 0600)
	os.WriteFile(filepath.Join(root, "running"), nil, 0600)
	os.WriteFile(filepath.Join(root, "cache.db"), []byte("keep-runtime"), 0600)
	files := map[string][]byte{"config.yaml": []byte("mode: rule\n"), "ruleset/binary.mrs": {0, 0xff, 0xfe, 1, 0}, "ruleset/empty.txt": {}}
	s := submitBundle(t, cfg, "binary-empty", files, true)
	if s.Status != "succeeded" || s.ConfigVersion == "" {
		t.Fatalf("state %+v", s)
	}
	for p, want := range files {
		if p == "config.yaml" {
			continue
		}
		got, e := os.ReadFile(filepath.Join(root, p))
		if e != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s bytes differ: %x %v", p, got, e)
		}
	}
	b, _ := os.ReadFile(filepath.Join(root, "cache.db"))
	if string(b) != "keep-runtime" {
		t.Fatal("unmanaged runtime resource changed")
	}
}
func TestDeploymentFailedHealthRestoresRemovedAndNewFiles(t *testing.T) {
	cfg := testDeploymentConfig(t)
	root := filepath.Dir(cfg.ConfigPath)
	first := submitBundle(t, cfg, "first", map[string][]byte{"config.yaml": []byte("mode: rule\n"), "ruleset/old": []byte("old")}, true)
	if first.Status != "succeeded" {
		t.Fatal(first)
	}
	oldConfig, _ := os.ReadFile(cfg.ConfigPath)
	s := submitBundle(t, cfg, "unhealthy", map[string][]byte{"config.yaml": []byte("mode: global\nreject-start: true\n"), "ruleset/new": []byte("new")}, true)
	if s.Status != "rolled_back" || s.ConfigVersion != "" || s.FailedStage != "checking" {
		t.Fatalf("state %+v", s)
	}
	got, _ := os.ReadFile(cfg.ConfigPath)
	if !bytes.Equal(got, oldConfig) {
		t.Fatal("configuration not restored")
	}
	got, _ = os.ReadFile(filepath.Join(root, "ruleset/old"))
	if string(got) != "old" {
		t.Fatal("stale file not restored")
	}
	if _, e := os.Stat(filepath.Join(root, "ruleset/new")); !os.IsNotExist(e) {
		t.Fatal("new file not removed")
	}
	active, _ := os.ReadFile(filepath.Join(deploymentRoot(cfg), "active-manifest.json"))
	if !bytes.Contains(active, []byte(`"deployment_id":"first"`)) {
		t.Fatal("active version not restored")
	}
}
func TestDeploymentRejectsDamagedManifestBeforeTouchingLive(t *testing.T) {
	cfg := testDeploymentConfig(t)
	os.WriteFile(cfg.ConfigPath, []byte("mode: old\n"), 0600)
	body, digest := testBundle(t, cfg, "bad-digest", map[string][]byte{"config.yaml": []byte("mode: rule\n")}, func(m *DeploymentManifest) { m.Files[0].SHA256 = strings.Repeat("0", 64) })
	dir := deploymentPath(cfg, "bad-digest")
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, "bundle.tar.gz"), body, 0600)
	s := &DeploymentState{DeploymentID: "bad-digest", ArchiveSHA256: digest}
	saveDeployment(cfg, s, "verifying")
	prepareDeployment(cfg, s, true)
	if s.Status != "failed" {
		t.Fatal(s.Status)
	}
	b, _ := os.ReadFile(cfg.ConfigPath)
	if string(b) != "mode: old\n" {
		t.Fatal("live configuration changed")
	}
}
func TestDeploymentRejectsTraversalAndLinks(t *testing.T) {
	for _, kind := range []string{"traversal", "symlink", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			cfg := testDeploymentConfig(t)
			dir := deploymentPath(cfg, kind)
			os.MkdirAll(dir, 0700)
			var buf bytes.Buffer
			gz := gzip.NewWriter(&buf)
			tw := tar.NewWriter(gz)
			h := &tar.Header{Name: "files/../escape", Size: 0, Typeflag: tar.TypeReg}
			if kind == "symlink" {
				h.Name = "files/link"
				h.Typeflag = tar.TypeSymlink
				h.Linkname = "/tmp"
			}
			if kind == "duplicate" {
				h.Name = "files/config.yaml"
				tw.WriteHeader(h)
			}
			tw.WriteHeader(h)
			tw.Close()
			gz.Close()
			os.WriteFile(filepath.Join(dir, "bundle.tar.gz"), buf.Bytes(), 0600)
			s := &DeploymentState{DeploymentID: kind}
			if e := extractDeployment(cfg, s); e == nil {
				t.Fatal("unsafe tar accepted")
			}
		})
	}
}
func TestDeploymentReadyTamperingAndIdempotentUpload(t *testing.T) {
	cfg := testDeploymentConfig(t)
	files := map[string][]byte{"config.yaml": []byte("mode: rule\n")}
	s := submitBundle(t, cfg, "staged", files, false)
	if s.Status != "ready" {
		t.Fatal(s)
	}
	body, _ := testBundle(t, cfg, "staged", files, nil)
	req := httptest.NewRequest("POST", "/api/deployments", bytes.NewReader(body))
	req.Header.Set("X-Deployment-ID", "staged")
	req.Header.Set("X-Content-SHA256", s.ArchiveSHA256)
	w := httptest.NewRecorder()
	DeploymentUploadHandler(cfg)(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	req = httptest.NewRequest("POST", "/api/deployments", bytes.NewReader(body))
	req.Header.Set("X-Deployment-ID", "staged")
	req.Header.Set("X-Content-SHA256", strings.Repeat("0", 64))
	w = httptest.NewRecorder()
	DeploymentUploadHandler(cfg)(w, req)
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	os.WriteFile(filepath.Join(deploymentPath(cfg, "staged"), "stage/config.yaml"), []byte("tamper"), 0600)
	activateDeployment(cfg, s)
	if s.Status != "failed" {
		t.Fatal(s.Status)
	}
	if _, e := os.Stat(cfg.ConfigPath); !os.IsNotExist(e) {
		t.Fatal("staged activation touched live")
	}
}
func TestDeploymentCrashRecoveryRestoresBeforeBootAndConfirmsHealth(t *testing.T) {
	cfg := testDeploymentConfig(t)
	root := filepath.Dir(cfg.ConfigPath)
	os.WriteFile(cfg.ConfigPath, []byte("mode: old\n"), 0600)
	os.WriteFile(filepath.Join(root, "running"), nil, 0600)
	s := submitBundle(t, cfg, "interrupted", map[string][]byte{"config.yaml": []byte("mode: new\n"), "rules/new": []byte("new")}, false)
	if e := loadEffectiveManifest(cfg, s); e != nil {
		t.Fatal(e)
	}
	s.WasRunning = true
	if e := backupDeployment(cfg, s); e != nil {
		t.Fatal(e)
	}
	if e := saveDeployment(cfg, s, "replacing"); e != nil {
		t.Fatal(e)
	}
	if e := replaceDeployment(cfg, s); e != nil {
		t.Fatal(e)
	}
	if e := RecoverDeployments(cfg, false); e != nil {
		t.Fatal(e)
	}
	restored, _ := loadDeployment(cfg, s.DeploymentID)
	if restored.Status != "recovery_pending" {
		t.Fatal(restored.Status)
	}
	b, _ := os.ReadFile(cfg.ConfigPath)
	if string(b) != "mode: old\n" {
		t.Fatal("old files not restored")
	}
	if _, e := os.Stat(filepath.Join(root, "rules/new")); !os.IsNotExist(e) {
		t.Fatal("new file survived recovery")
	}
	if e := RecoverDeployments(cfg, true); e != nil {
		t.Fatal(e)
	}
	restored, _ = loadDeployment(cfg, s.DeploymentID)
	if restored.Status != "rolled_back" {
		t.Fatal(restored.Status)
	}
}
func TestDeploymentRollbackCorruptBackupBlocksNewOperations(t *testing.T) {
	cfg := testDeploymentConfig(t)
	os.WriteFile(cfg.ConfigPath, []byte("mode: old\n"), 0600)
	s := submitBundle(t, cfg, "broken-backup", map[string][]byte{"config.yaml": []byte("mode: rule\n")}, false)
	loadEffectiveManifest(cfg, s)
	backupDeployment(cfg, s)
	saveDeployment(cfg, s, "replacing")
	replaceDeployment(cfg, s)
	os.WriteFile(filepath.Join(deploymentPath(cfg, s.DeploymentID), "backup/config.yaml"), []byte("corrupt"), 0600)
	if e := RecoverDeployments(cfg, false); e == nil {
		t.Fatal("corrupt recovery succeeded")
	}
	s, _ = loadDeployment(cfg, s.DeploymentID)
	if s.Status != "rollback_failed" {
		t.Fatal(s.Status)
	}
	if e := deploymentBlocked(cfg); e == nil {
		t.Fatal("rollback failure did not block new deployment")
	}
}
func TestDeploymentOperationLockBlocksRestartAndLegacyUpdate(t *testing.T) {
	cfg := testDeploymentConfig(t)
	release, e := AcquireServiceOperation(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	w := httptest.NewRecorder()
	RestartHandler(cfg)(w, httptest.NewRequest("POST", "/api/restart", nil))
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	ConfigUpdateHandler(cfg)(w, httptest.NewRequest("POST", "/api/config/update", strings.NewReader(`{"config":"new"}`)))
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
}

func TestDeploymentNativeFailureAndLiveSymlinkLeaveOldRunning(t *testing.T) {
	for _, scenario := range []string{"native", "symlink"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := testDeploymentConfig(t)
			root := filepath.Dir(cfg.ConfigPath)
			old := []byte("mode: old\n")
			os.WriteFile(cfg.ConfigPath, old, 0600)
			os.WriteFile(filepath.Join(root, "running"), nil, 0600)
			files := map[string][]byte{"config.yaml": []byte("mode: rule\n")}
			if scenario == "native" {
				files["config.yaml"] = []byte("mode: rule\nsecret: native-credential-secret\n")
				os.WriteFile(cfg.ServiceBinary, []byte("#!/bin/sh\necho 'ERROR invalid provider native-credential-secret'\nexit 1\n"), 0700)
			} else {
				outside := t.TempDir()
				os.Symlink(outside, filepath.Join(root, "ruleset"))
				files["ruleset/new"] = []byte("new")
			}
			s := submitBundle(t, cfg, "invalid-"+scenario, files, true)
			if s.Status != "failed" {
				t.Fatalf("%+v", s)
			}
			b, _ := os.ReadFile(cfg.ConfigPath)
			if !bytes.Equal(b, old) {
				t.Fatal("old config changed")
			}
			if _, e := os.Stat(filepath.Join(root, "running")); e != nil {
				t.Fatal("old service was stopped")
			}
			if strings.Contains(s.Error+s.ErrorDetails, "credential-secret") {
				t.Fatal("native output leaked")
			}
			if scenario == "native" && !strings.Contains(s.ErrorDetails, "invalid provider") {
				t.Fatal("useful native failure details missing")
			}
		})
	}
}
func TestDeploymentStartFailureAndFirstPublishRollback(t *testing.T) {
	for _, first := range []bool{false, true} {
		t.Run(fmt.Sprint(first), func(t *testing.T) {
			cfg := testDeploymentConfig(t)
			root := filepath.Dir(cfg.ConfigPath)
			if !first {
				os.WriteFile(cfg.ConfigPath, []byte("mode: old\n"), 0600)
				os.WriteFile(filepath.Join(root, "running"), nil, 0600)
			}
			cfg.StartCommand = "if grep -q reject-start config.yaml; then exit 1; fi; touch running"
			s := submitBundle(t, cfg, "start-failure", map[string][]byte{"config.yaml": []byte("mode: rule\nreject-start: true\n"), "rules/new": []byte("new")}, true)
			if s.Status != "rolled_back" || s.FailedStage != "starting" {
				t.Fatalf("%+v", s)
			}
			if first {
				if _, e := os.Stat(cfg.ConfigPath); !os.IsNotExist(e) {
					t.Fatal("first publish left a config")
				}
				if _, e := os.Stat(filepath.Join(root, "running")); !os.IsNotExist(e) {
					t.Fatal("first publish started old nonexistent service")
				}
			} else {
				b, _ := os.ReadFile(cfg.ConfigPath)
				if string(b) != "mode: old\n" {
					t.Fatal("old config not restored")
				}
			}
		})
	}
}

func TestDNSHealthUsesActualAnswerAndRejectsMalformedResponse(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(fmt.Sprint(valid), func(t *testing.T) {
			socket, e := net.ListenPacket("udp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer socket.Close()
			go func() {
				for {
					buf := make([]byte, 2048)
					n, addr, e := socket.ReadFrom(buf)
					if e != nil {
						return
					}
					if n < 12 {
						continue
					}
					qend := 12
					for qend < n && buf[qend] != 0 {
						qend += int(buf[qend]) + 1
					}
					qend += 5
					if qend > n {
						continue
					}
					response := append([]byte(nil), buf[:qend]...)
					response[2] = 0x81
					response[3] = 0x80
					response[6] = 0
					response[7] = 1
					response[10] = 0
					response[11] = 0
					if valid {
						response = append(response, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 127, 0, 0, 1)
					}
					socket.WriteTo(response, addr)
				}
			}()
			e = dnsHealthQuery(socket.LocalAddr().String(), "acceptance.test")
			if valid && e != nil {
				t.Fatal(e)
			}
			if !valid && e == nil {
				t.Fatal("malformed DNS answer passed")
			}
		})
	}
}

func TestDeploymentDiskAndBackupFailuresKeepOriginalConfiguration(t *testing.T) {
	for _, scenario := range []string{"space", "backup"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := testDeploymentConfig(t)
			root := filepath.Dir(cfg.ConfigPath)
			os.WriteFile(cfg.ConfigPath, []byte("mode: old\n"), 0600)
			os.WriteFile(filepath.Join(root, "running"), nil, 0600)
			if scenario == "space" {
				previous := deploymentFreeBytes
				deploymentFreeBytes = func(string) (uint64, error) { return 0, nil }
				defer func() { deploymentFreeBytes = previous }()
			} else {
				previous := deploymentBackupCopy
				deploymentBackupCopy = func(string, string, os.FileMode) error { return fmt.Errorf("simulated backup write failure") }
				defer func() { deploymentBackupCopy = previous }()
			}
			s := submitBundle(t, cfg, "fault-"+scenario, map[string][]byte{"config.yaml": []byte("mode: rule\n")}, true)
			expected := "failed"
			if scenario == "backup" {
				expected = "rolled_back"
			}
			if s.Status != expected {
				t.Fatalf("%+v", s)
			}
			b, _ := os.ReadFile(cfg.ConfigPath)
			if string(b) != "mode: old\n" {
				t.Fatal("failure modified original config")
			}
			if _, e := os.Stat(filepath.Join(root, "running")); e != nil {
				t.Fatal("failure left original service stopped")
			}
		})
	}
}
func TestDeploymentPreservesFileModesAndOwnershipOnCommitAndRollback(t *testing.T) {
	cfg := testDeploymentConfig(t)
	root := filepath.Dir(cfg.ConfigPath)
	os.WriteFile(cfg.ConfigPath, []byte("mode: old\n"), 0640)
	if os.Geteuid() == 0 {
		if e := os.Chown(cfg.ConfigPath, 65534, 65534); e != nil {
			t.Fatal(e)
		}
	}
	oldInfo, e := os.Stat(cfg.ConfigPath)
	if e != nil {
		t.Fatal(e)
	}
	uid, gid := fileOwnership(oldInfo)
	s := submitBundle(t, cfg, "ownership", map[string][]byte{"config.yaml": []byte("mode: rule\n"), "rules/new": []byte("rule")}, true)
	if s.Status != "succeeded" {
		t.Fatal(s)
	}
	for _, p := range []string{cfg.ConfigPath, filepath.Join(root, "rules/new"), filepath.Join(root, "rules")} {
		info, e := os.Stat(p)
		if e != nil {
			t.Fatal(e)
		}
		actualUID, actualGID := fileOwnership(info)
		if actualUID != uid || actualGID != gid {
			t.Fatalf("owner changed on %s", p)
		}
	}
	info, _ := os.Stat(cfg.ConfigPath)
	if info.Mode().Perm() != 0640 {
		t.Fatalf("config mode changed: %v", info.Mode())
	}
	s = submitBundle(t, cfg, "ownership-rollback", map[string][]byte{"config.yaml": []byte("reject-start: true\n")}, true)
	if s.Status != "rolled_back" {
		t.Fatal(s)
	}
	info, _ = os.Stat(cfg.ConfigPath)
	actualUID, actualGID := fileOwnership(info)
	if info.Mode().Perm() != 0640 || actualUID != uid || actualGID != gid {
		t.Fatal("rollback lost config metadata")
	}
}

func TestMihomoProxyProbeRejectsOccupiedPortAndChecksSOCKSHandshake(t *testing.T) {
	for _, valid := range []bool{false, true} {
		t.Run(fmt.Sprint(valid), func(t *testing.T) {
			listener, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer listener.Close()
			go func() {
				conn, e := listener.Accept()
				if e != nil {
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(2 * time.Second))
				request := make([]byte, 4)
				if _, e := io.ReadFull(conn, request); e != nil {
					return
				}
				if valid {
					conn.Write([]byte{5, 2})
				} else {
					conn.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
				}
			}()
			e = probeMihomoProxy(listener.Addr().String(), "mixed-port")
			if valid && e != nil {
				t.Fatal(e)
			}
			if !valid && e == nil {
				t.Fatal("unrelated occupied listener passed health")
			}
		})
	}
}

func TestDeploymentCarriesNativeGeoResourceThroughSecondOfflinePublish(t *testing.T) {
	cfg := testDeploymentConfig(t)
	root := filepath.Dir(cfg.ConfigPath)
	if e := os.WriteFile(cfg.ServiceBinary, []byte("#!/bin/sh\nprintf immutable-geo-snapshot > Country.mmdb\n"), 0700); e != nil {
		t.Fatal(e)
	}
	first := submitBundle(t, cfg, "geo-first", map[string][]byte{"config.yaml": []byte("mode: rule\n")}, true)
	if first.Status != "succeeded" {
		t.Fatalf("first publish: %+v", first)
	}
	want := []byte("immutable-geo-snapshot")
	got, e := os.ReadFile(filepath.Join(root, "Country.mmdb"))
	if e != nil || !bytes.Equal(got, want) {
		t.Fatal("native Geo asset was not installed")
	}
	// The second validator models an offline core: the staged Geo database must
	// exist already, and the validator cannot recreate it from the network.
	if e := os.WriteFile(cfg.ServiceBinary, []byte("#!/bin/sh\ntest -f Country.mmdb || exit 1\n"), 0700); e != nil {
		t.Fatal(e)
	}
	second := submitBundle(t, cfg, "geo-offline", map[string][]byte{"config.yaml": []byte("mode: global\n")}, true)
	if second.Status != "succeeded" {
		t.Fatalf("second offline publish: %+v", second)
	}
	got, e = os.ReadFile(filepath.Join(root, "Country.mmdb"))
	if e != nil || !bytes.Equal(got, want) {
		t.Fatal("second publish deleted its validated Geo dependency")
	}
	manifestData, e := os.ReadFile(filepath.Join(deploymentPath(cfg, second.DeploymentID), "effective-manifest.json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(manifestData, &second.Manifest); e != nil {
		t.Fatal(e)
	}
	if e = verifyManifestFiles(root, second.Manifest); e != nil {
		t.Fatal(e)
	}
	found := false
	for _, entry := range second.Manifest.Files {
		if entry.Path == "Country.mmdb" && entry.Role == "resource" {
			found = true
		}
	}
	if !found {
		t.Fatal("Geo ownership did not carry into active manifest")
	}
}

func TestOlderStagedCandidateCannotDeleteNewerPersistentGeoResource(t *testing.T) {
	cfg := testDeploymentConfig(t)
	root := filepath.Dir(cfg.ConfigPath)
	staged := submitBundle(t, cfg, "geo-before-resource", map[string][]byte{"config.yaml": []byte("mode: rule\n")}, false)
	if staged.Status != "ready" {
		t.Fatal(staged)
	}
	os.WriteFile(cfg.ServiceBinary, []byte("#!/bin/sh\nprintf new-persistent-geo > Country.mmdb\n"), 0700)
	fresh := submitBundle(t, cfg, "geo-resource-added", map[string][]byte{"config.yaml": []byte("mode: global\n")}, true)
	if fresh.Status != "succeeded" {
		t.Fatal(fresh)
	}
	activateDeployment(cfg, staged)
	if staged.Status != "succeeded" {
		t.Fatal(staged)
	}
	data, e := os.ReadFile(filepath.Join(root, "Country.mmdb"))
	if e != nil || string(data) != "new-persistent-geo" {
		t.Fatal("activating older stage deleted newer persistent resource")
	}
	manifestData, e := os.ReadFile(filepath.Join(deploymentPath(cfg, staged.DeploymentID), "effective-manifest.json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(manifestData, &staged.Manifest); e != nil {
		t.Fatal(e)
	}
	if e = verifyManifestFiles(root, staged.Manifest); e != nil {
		t.Fatal(e)
	}
	found := false
	for _, f := range staged.Manifest.Files {
		if f.Path == "Country.mmdb" && f.Role == "resource" {
			found = true
		}
	}
	if !found {
		t.Fatal("activated old candidate omitted persistent resource checksum")
	}

}
func TestFailedFirstGeoPublishRemovesUncommittedResource(t *testing.T) {
	cfg := testDeploymentConfig(t)
	root := filepath.Dir(cfg.ConfigPath)
	os.WriteFile(cfg.ServiceBinary, []byte("#!/bin/sh\nprintf uncommitted-geo > Country.mmdb\n"), 0700)
	failed := submitBundle(t, cfg, "geo-uncommitted", map[string][]byte{"config.yaml": []byte("reject-start: true\n")}, true)
	if failed.Status != "rolled_back" {
		t.Fatal(failed)
	}
	if _, e := os.Stat(filepath.Join(root, "Country.mmdb")); !os.IsNotExist(e) {
		t.Fatal("failed first publish retained newly created Geo asset")
	}
}

func TestRecoveryCleansOnlyTransactionsInterruptedBeforeInitialJournal(t *testing.T) {
	for _, start := range []bool{false, true} {
		for _, scenario := range []string{"empty", "partial-initial", "complete-initial", "corrupt-state", "received-bundle", "stage", "later-temp", "symlink-temp"} {
			t.Run(fmt.Sprintf("%v/%s", start, scenario), func(t *testing.T) {
				cfg := &Config{ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), ServiceType: "mihomo"}
				os.WriteFile(cfg.ConfigPath, []byte("live-untouched"), 0600)
				id := "initial-write-interruption"
				dir := deploymentPath(cfg, id)
				if e := durableMkdirAll(dir, 0700); e != nil {
					t.Fatal(e)
				}
				switch scenario {
				case "partial-initial":
					os.WriteFile(filepath.Join(dir, ".write-partial"), []byte(`{"success":true,"deployment_id":`), 0600)
				case "complete-initial":
					data, _ := json.Marshal(DeploymentState{DeploymentID: id, Status: "receiving"})
					os.WriteFile(filepath.Join(dir, ".write-initial"), data, 0600)
				case "corrupt-state":
					os.WriteFile(filepath.Join(dir, "state.json"), []byte("corrupt"), 0600)
				case "received-bundle":
					os.WriteFile(filepath.Join(dir, "bundle.tar.gz"), []byte("received"), 0600)
				case "stage":
					os.Mkdir(filepath.Join(dir, "stage"), 0700)
				case "later-temp":
					data, _ := json.Marshal(DeploymentState{DeploymentID: id, Status: "replacing"})
					os.WriteFile(filepath.Join(dir, ".write-later"), data, 0600)
				case "symlink-temp":
					os.Symlink(cfg.ConfigPath, filepath.Join(dir, ".write-link"))
				}
				e := RecoverDeployments(cfg, start)
				safe := scenario == "empty" || scenario == "partial-initial" || scenario == "complete-initial"
				if safe {
					if e != nil {
						t.Fatal(e)
					}
					if _, e := os.Stat(dir); !os.IsNotExist(e) {
						t.Fatal("initial unstarted directory retained")
					}
				} else {
					if e == nil {
						t.Fatal("ambiguous transaction was discarded")
					}
					if _, e := os.Stat(dir); e != nil {
						t.Fatal("ambiguous transaction removed")
					}
				}
				live, e := os.ReadFile(cfg.ConfigPath)
				if e != nil || string(live) != "live-untouched" {
					t.Fatal("recovery modified old configuration")
				}
			})
		}
	}
}
