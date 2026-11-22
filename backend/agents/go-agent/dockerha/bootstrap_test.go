package dockerha

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

var (
	testOwner   = strings.Repeat("ab", 32)
	testToken   = strings.Repeat("t", 43)
	testSession = strings.Repeat("a", 32)
	testAgentID = "agent_" + strings.Repeat("a", 32)
	testSecret  = strings.Repeat("z", 32)
)

// origin is a loopback enrollment endpoint recording every call.
type origin struct {
	mu       sync.Mutex
	server   *httptest.Server
	status   int
	location string
	body     string
	calls    []string
}

func newOrigin(t *testing.T) *origin {
	t.Helper()
	o := &origin{status: 200, body: `{"success": true, "agent_id": "` + testAgentID + `", "token": "` + testSecret + `"}`}
	o.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		o.mu.Lock()
		defer o.mu.Unlock()
		o.calls = append(o.calls, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization")+" "+string(raw))
		if o.location != "" {
			w.Header().Set("Location", o.location)
		}
		w.WriteHeader(o.status)
		io.WriteString(w, o.body)
	}))
	t.Cleanup(o.server.Close)
	return o
}

func (o *origin) set(status int, body, location string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.status, o.body, o.location = status, body, location
}

func (o *origin) recorded() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.calls...)
}

func (o *origin) url() string {
	return o.server.URL + "/api/agents/docker-oxidns-ha-enroll/" + testSession
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func readObject(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func settingsJSON(owner, url, token string) string {
	raw, _ := json.Marshal(map[string]string{"owner": owner, "url": url, "token": token})
	return string(raw)
}

// bundleFixture lays out prepared bundle files for one installation.
func bundleFixture(t *testing.T, url string) BootstrapPaths {
	t.Helper()
	dir := t.TempDir()
	paths := BootstrapPaths{Settings: filepath.Join(dir, "enrollment.json"), AgentDir: filepath.Join(dir, "agent"), Setup: filepath.Join(dir, "setup")}
	writeFile(t, paths.Settings, settingsJSON(testOwner, url, testToken), 0o600)
	writeFile(t, paths.Setup, testOwner+"\n", 0o600)
	if err := os.Mkdir(paths.AgentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return paths
}

func runBootstrap(paths BootstrapPaths) (string, error) {
	var out bytes.Buffer
	err := Bootstrap(context.Background(), paths, BundleClient(), &out)
	return out.String(), err
}

func message(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestBootstrapFirstEnrollmentThenOfflineRestart(t *testing.T) {
	o := newOrigin(t)
	paths := bundleFixture(t, o.url())
	out, err := runBootstrap(paths)
	if err != nil || out != msgIdentitySaved+"\n" {
		t.Fatalf("first: %q %v", out, err)
	}
	if calls := o.recorded(); len(calls) != 1 || calls[0] != "POST /api/agents/docker-oxidns-ha-enroll/"+testSession+" Bearer "+testToken+" {}" {
		t.Fatalf("calls %q", calls)
	}
	config := readObject(t, filepath.Join(paths.AgentDir, agentConfig))
	if len(config) != 2 || config["agent_id"] != testAgentID || config["token"] != testSecret {
		t.Fatalf("config %v", config)
	}
	marker := readObject(t, filepath.Join(paths.AgentDir, bundleMarker))
	digest, _ := identityDigest(map[string]any{"agent_id": testAgentID, "token": testSecret})
	if marker["owner"] != testOwner || marker["url"] != o.url() || marker["identity_digest"] != digest {
		t.Fatalf("marker %v", marker)
	}
	for _, name := range []string{agentConfig, bundleMarker} {
		if info, _ := os.Stat(filepath.Join(paths.AgentDir, name)); info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode %v", name, info.Mode())
		}
	}
	// The Agent adds its own fields; a restart with an expired credential and
	// no network still starts.
	config["service_type"] = "oxidns"
	updated, _ := json.Marshal(config)
	writeFile(t, filepath.Join(paths.AgentDir, agentConfig), string(updated), 0o600)
	o.set(401, `{"success": false}`, "")
	o.server.Close()
	out, err = runBootstrap(paths)
	if err != nil || out != msgIdentityVerified+"\n" {
		t.Fatalf("restart: %q %v", out, err)
	}
	entries, _ := os.ReadDir(paths.AgentDir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".compose-enroll-") {
			t.Fatalf("temporary file left: %s", entry.Name())
		}
	}
}

func TestBootstrapEnrollmentAnswers(t *testing.T) {
	tooLarge := `{"success": true, "pad": "` + strings.Repeat("x", 16400) + `"}`
	tests := []struct {
		name, body, location string
		status               int
		want                 string
	}{
		{"expired", `{"success": false}`, "", 401, "首次登记被拒绝（HTTP 401）；不跟随地址重定向。请确认最终服务地址；凭据过期时以相同设置重新生成安装包并准备后重试。"},
		{"redirect not followed", "", "http://127.0.0.1:1/elsewhere", 302, "首次登记被拒绝（HTTP 302）；不跟随地址重定向。请确认最终服务地址；凭据过期时以相同设置重新生成安装包并准备后重试。"},
		{"server error", "", "", 500, "首次登记被拒绝（HTTP 500）；不跟随地址重定向。请确认最终服务地址；凭据过期时以相同设置重新生成安装包并准备后重试。"},
		{"too large", tooLarge, "", 200, msgResponseTooLarge},
		{"not json", "<html>", "", 200, msgEnrollIncomplete},
		{"not object", `[1]`, "", 200, msgResponseInvalid},
		{"success false", `{"success": false, "agent_id": "` + testAgentID + `", "token": "` + testSecret + `"}`, "", 200, msgResponseInvalid},
		{"success string", `{"success": "true", "agent_id": "` + testAgentID + `", "token": "` + testSecret + `"}`, "", 200, msgResponseInvalid},
		{"short identity", `{"success": true, "agent_id": "short", "token": "` + testSecret + `"}`, "", 200, msgIdentityPartial},
		{"token not string", `{"success": true, "agent_id": "` + testAgentID + `", "token": 12345678901234567890}`, "", 200, msgIdentityPartial},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			o := newOrigin(t)
			o.set(test.status, test.body, test.location)
			paths := bundleFixture(t, o.url())
			_, err := runBootstrap(paths)
			if message(err) != test.want {
				t.Fatalf("got %q want %q", message(err), test.want)
			}
			if present(filepath.Join(paths.AgentDir, agentConfig)) {
				t.Fatal("identity written")
			}
			// Ownership is reserved before the request and kept for the retry.
			marker := readObject(t, filepath.Join(paths.AgentDir, bundleMarker))
			if len(marker) != 2 || marker["owner"] != testOwner || marker["url"] != o.url() {
				t.Fatalf("marker %v", marker)
			}
			if test.location != "" && len(o.recorded()) != 1 {
				t.Fatalf("calls %q", o.recorded())
			}
			// The retry with a working origin completes the same installation.
			o.set(200, `{"success": true, "agent_id": "`+testAgentID+`", "token": "`+testSecret+`"}`, "")
			if out, err := runBootstrap(paths); err != nil || out != msgIdentitySaved+"\n" {
				t.Fatalf("retry %q %v", out, err)
			}
		})
	}
}

func TestBootstrapUnreachableOrigin(t *testing.T) {
	o := newOrigin(t)
	paths := bundleFixture(t, o.url())
	o.server.Close()
	if _, err := runBootstrap(paths); message(err) != msgEnrollIncomplete {
		t.Fatal(err)
	}
}

func TestBootstrapIgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")
	o := newOrigin(t)
	paths := bundleFixture(t, o.url())
	if _, err := runBootstrap(paths); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapRefusals(t *testing.T) {
	digest, _ := identityDigest(map[string]any{"agent_id": testAgentID, "token": testSecret})
	identity := `{"agent_id": "` + testAgentID + `", "token": "` + testSecret + `"}`
	tests := []struct {
		name  string
		setup func(t *testing.T, p BootstrapPaths, url string)
		want  string
	}{
		{"settings missing", func(t *testing.T, p BootstrapPaths, _ string) { os.Remove(p.Settings) }, msgFileUnreadable},
		{"settings symlink", func(t *testing.T, p BootstrapPaths, _ string) {
			os.Rename(p.Settings, p.Settings+".real")
			os.Symlink(p.Settings+".real", p.Settings)
		}, msgFileUnreadable},
		{"settings directory", func(t *testing.T, p BootstrapPaths, _ string) {
			os.Remove(p.Settings)
			os.Mkdir(p.Settings, 0o700)
		}, msgFileUnreadable}, // Python could not open a directory as a file

		{"settings fifo", func(t *testing.T, p BootstrapPaths, _ string) {
			os.Remove(p.Settings)
			if err := syscall.Mkfifo(p.Settings, 0o600); err != nil {
				t.Fatal(err)
			}
		}, msgFileNotRegular},
		{"settings too large", func(t *testing.T, p BootstrapPaths, _ string) {
			writeFile(t, p.Settings, `{"owner": "`+strings.Repeat("x", 65536)+`"}`, 0o600)
		}, msgFileTooLarge},
		{"settings corrupt", func(t *testing.T, p BootstrapPaths, _ string) { writeFile(t, p.Settings, "{", 0o600) }, msgFileCorrupt},
		{"settings invalid utf-8", func(t *testing.T, p BootstrapPaths, _ string) {
			writeFile(t, p.Settings, "{\"owner\": \"\xff\"}", 0o600)
		}, msgFileCorrupt},
		{"settings not object", func(t *testing.T, p BootstrapPaths, _ string) { writeFile(t, p.Settings, "[]", 0o600) }, msgFileInvalid},
		{"owner invalid", func(t *testing.T, p BootstrapPaths, url string) {
			writeFile(t, p.Settings, settingsJSON(strings.ToUpper(testOwner), url, testToken), 0o600)
		}, msgOwnerInvalid},
		{"owner missing", func(t *testing.T, p BootstrapPaths, _ string) { writeFile(t, p.Settings, `{}`, 0o600) }, msgOwnerInvalid},
		{"setup missing", func(t *testing.T, p BootstrapPaths, _ string) { os.Remove(p.Setup) }, msgFileUnreadable},
		{"setup other owner", func(t *testing.T, p BootstrapPaths, _ string) { writeFile(t, p.Setup, strings.Repeat("cd", 32), 0o600) }, msgNotPrepared},
		{"setup invalid utf-8", func(t *testing.T, p BootstrapPaths, _ string) { writeFile(t, p.Setup, "\xff", 0o600) }, msgNotPrepared},
		{"agent dir missing", func(t *testing.T, p BootstrapPaths, _ string) { os.Remove(p.AgentDir) }, msgAgentDirInvalid},
		{"agent dir symlink", func(t *testing.T, p BootstrapPaths, _ string) {
			os.Rename(p.AgentDir, p.AgentDir+".real")
			os.Symlink(p.AgentDir+".real", p.AgentDir)
		}, msgAgentDirInvalid},
		{"lock directory", func(t *testing.T, p BootstrapPaths, _ string) { os.Mkdir(filepath.Join(p.AgentDir, bundleLock), 0o700) }, msgSaveFailed},
		{"lock symlink", func(t *testing.T, p BootstrapPaths, _ string) {
			os.Symlink(filepath.Join(p.AgentDir, "elsewhere"), filepath.Join(p.AgentDir, bundleLock))
		}, msgSaveFailed},
		{"lock fifo", func(t *testing.T, p BootstrapPaths, _ string) {
			if err := syscall.Mkfifo(filepath.Join(p.AgentDir, bundleLock), 0o600); err != nil {
				t.Fatal(err)
			}
		}, msgLockNotRegular},
		{"marker other owner", func(t *testing.T, p BootstrapPaths, url string) {
			writeFile(t, filepath.Join(p.AgentDir, bundleMarker), `{"owner": "`+strings.Repeat("cd", 32)+`", "url": "`+url+`"}`, 0o600)
		}, msgForeignInstall},
		{"marker other url", func(t *testing.T, p BootstrapPaths, _ string) {
			writeFile(t, filepath.Join(p.AgentDir, bundleMarker), `{"owner": "`+testOwner+`", "url": "http://other.example"}`, 0o600)
		}, msgForeignInstall},
		{"marker corrupt", func(t *testing.T, p BootstrapPaths, _ string) {
			writeFile(t, filepath.Join(p.AgentDir, bundleMarker), "not json", 0o600)
		}, msgFileCorrupt},
		{"marker dangling symlink", func(t *testing.T, p BootstrapPaths, _ string) {
			os.Symlink(filepath.Join(p.AgentDir, "missing"), filepath.Join(p.AgentDir, bundleMarker))
		}, msgFileUnreadable},
		{"identity without marker", func(t *testing.T, p BootstrapPaths, _ string) {
			writeFile(t, filepath.Join(p.AgentDir, agentConfig), identity, 0o600)
		}, msgIdentityMismatch},
		{"identity digest differs", func(t *testing.T, p BootstrapPaths, url string) {
			writeFile(t, filepath.Join(p.AgentDir, bundleMarker), `{"owner": "`+testOwner+`", "url": "`+url+`", "identity_digest": "`+strings.Repeat("0", 64)+`"}`, 0o600)
			writeFile(t, filepath.Join(p.AgentDir, agentConfig), identity, 0o600)
		}, msgIdentityMismatch},
		{"identity marker without digest", func(t *testing.T, p BootstrapPaths, url string) {
			writeFile(t, filepath.Join(p.AgentDir, bundleMarker), `{"owner": "`+testOwner+`", "url": "`+url+`"}`, 0o600)
			writeFile(t, filepath.Join(p.AgentDir, agentConfig), identity, 0o600)
		}, msgIdentityMismatch},
		{"identity incomplete", func(t *testing.T, p BootstrapPaths, url string) {
			writeFile(t, filepath.Join(p.AgentDir, bundleMarker), `{"owner": "`+testOwner+`", "url": "`+url+`", "identity_digest": "`+digest+`"}`, 0o600)
			writeFile(t, filepath.Join(p.AgentDir, agentConfig), `{"agent_id": "`+testAgentID+`"}`, 0o600)
		}, msgIdentityPartial},
		{"identity corrupt", func(t *testing.T, p BootstrapPaths, url string) {
			writeFile(t, filepath.Join(p.AgentDir, bundleMarker), `{"owner": "`+testOwner+`", "url": "`+url+`", "identity_digest": "`+digest+`"}`, 0o600)
			writeFile(t, filepath.Join(p.AgentDir, agentConfig), `{`, 0o600)
		}, msgFileCorrupt},
		{"identity symlink", func(t *testing.T, p BootstrapPaths, url string) {
			writeFile(t, filepath.Join(p.AgentDir, bundleMarker), `{"owner": "`+testOwner+`", "url": "`+url+`", "identity_digest": "`+digest+`"}`, 0o600)
			writeFile(t, filepath.Join(p.AgentDir, "real.json"), identity, 0o600)
			os.Symlink(filepath.Join(p.AgentDir, "real.json"), filepath.Join(p.AgentDir, agentConfig))
		}, msgFileUnreadable},
		{"server returned another identity", func(t *testing.T, p BootstrapPaths, url string) {
			writeFile(t, filepath.Join(p.AgentDir, bundleMarker), `{"owner": "`+testOwner+`", "url": "`+url+`", "identity_digest": "`+strings.Repeat("0", 64)+`"}`, 0o600)
		}, msgDifferentServer},
		{"url not enrollment path", func(t *testing.T, p BootstrapPaths, _ string) {
			writeFile(t, p.Settings, settingsJSON(testOwner, "http://panel.example/other", testToken), 0o600)
		}, msgURLInvalid},
		{"url with space", func(t *testing.T, p BootstrapPaths, _ string) {
			writeFile(t, p.Settings, settingsJSON(testOwner, "http://panel example/api/agents/docker-oxidns-ha-enroll/"+testSession, testToken), 0o600)
		}, msgURLInvalid},
		{"url with unicode space", func(t *testing.T, p BootstrapPaths, _ string) {
			writeFile(t, p.Settings, settingsJSON(testOwner, "http://panel　example/api/agents/docker-oxidns-ha-enroll/"+testSession, testToken), 0o600)
		}, msgURLInvalid},
		{"url ftp", func(t *testing.T, p BootstrapPaths, _ string) {
			writeFile(t, p.Settings, settingsJSON(testOwner, "ftp://panel.example/api/agents/docker-oxidns-ha-enroll/"+testSession, testToken), 0o600)
		}, msgURLInvalid},
		{"token short", func(t *testing.T, p BootstrapPaths, url string) {
			writeFile(t, p.Settings, settingsJSON(testOwner, url, "short"), 0o600)
		}, msgTokenInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			o := newOrigin(t)
			paths := bundleFixture(t, o.url())
			test.setup(t, paths, o.url())
			out, err := runBootstrap(paths)
			if message(err) != test.want {
				t.Fatalf("got %q (%q) want %q", message(err), out, test.want)
			}
			if test.want != msgDifferentServer && test.want != msgURLInvalid && test.want != msgTokenInvalid && len(o.recorded()) != 0 {
				t.Fatalf("enrollment called: %q", o.recorded())
			}
		})
	}
}

// TestBootstrapInvalidURLStillReservesOwnership: the marker is written
// before the URL and token checks, so a damaged bundle keeps its scope.
func TestBootstrapInvalidURLStillReservesOwnership(t *testing.T) {
	paths := bundleFixture(t, "http://panel.example/not-enroll")
	if _, err := runBootstrap(paths); message(err) != msgURLInvalid {
		t.Fatal(err)
	}
	if marker := readObject(t, filepath.Join(paths.AgentDir, bundleMarker)); marker["owner"] != testOwner {
		t.Fatalf("marker %v", marker)
	}
}

func TestIdentityDigestMatchesSortedCompactJSON(t *testing.T) {
	got, err := identityDigest(map[string]any{"token": testSecret, "agent_id": testAgentID, "extra": 1})
	sum := sha256.Sum256([]byte(`{"agent_id":"` + testAgentID + `","token":"` + testSecret + `"}`))
	if err != nil || got != hex.EncodeToString(sum[:]) {
		t.Fatal(got, err)
	}
	again, _ := identityDigest(map[string]any{"agent_id": testAgentID, "token": testSecret})
	if again != got {
		t.Fatal("digest depends on extra fields")
	}
	for _, value := range []any{nil, 5, "short", strings.Repeat("a", 257), "has space aaaaaaaaaaa", "ok_" + strings.Repeat("a", 253)} {
		_, err := identityDigest(map[string]any{"agent_id": value, "token": testSecret})
		if (err == nil) != (value == "ok_"+strings.Repeat("a", 253)) {
			t.Errorf("%v: %v", value, err)
		}
	}
}
