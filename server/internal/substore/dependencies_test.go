package substore

// Port of backend/test_dependencies.py (version choice, base URL, online
// update). Python patched requests/subprocess/_restart_sub_store/
// _wait_for_version; here GitHub is a loopback server, supervisorctl and
// node are a fake CommandRunner and the waits run on a fake clock against
// the fake Sub-Store.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// depsHarness is the Python fixture `dirs`.
type depsHarness struct {
	*harness
	d       *Dependencies
	ctx     context.Context
	runtime string
	github  *httptest.Server
	mu      sync.Mutex
	release string // JSON of the latest release
	content []byte // the bundle the download URL serves
	apiHits int
}

func newDepsHarness(t *testing.T) *depsHarness {
	h := newHarness(t, harnessOptions{builtin: true, version: "2.40.0"})
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dh := &depsHarness{harness: h, d: h.service.Dependencies, ctx: repo.WithRepository(context.Background(), r),
		runtime: filepath.Join(h.data, "sub-store", "runtime")}
	h.unsetenv("SUB_STORE_URL")
	dh.github = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dh.mu.Lock()
		defer dh.mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/repos/") {
			dh.apiHits++
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			io.WriteString(w, dh.release)
			return
		}
		w.Write(dh.content)
	}))
	t.Cleanup(dh.github.Close)
	dh.d.LatestReleaseURL = dh.github.URL + "/repos/sub-store-org/Sub-Store/releases/latest"
	return dh
}

func (dh *depsHarness) writeRuntime(version string) {
	os.MkdirAll(dh.runtime, 0o755)
	os.WriteFile(filepath.Join(dh.runtime, SubStoreAsset), []byte("runtime"), 0o644)
	os.WriteFile(filepath.Join(dh.runtime, "VERSION"), []byte(version+"\n"), 0o644)
}

func (dh *depsHarness) releaseFor(content []byte, version string) *jsonx.Object {
	sum := sha256.Sum256(content)
	return jsonx.Obj("version", version, "download_url", dh.github.URL+"/x/sub-store.bundle.js",
		"sha256", hex.EncodeToString(sum[:]), "published_at", "", "release_url", "", "notes", "",
		"size", jsonx.Int(len(content)))
}

func (dh *depsHarness) serve(content []byte) {
	dh.mu.Lock()
	dh.content = content
	dh.mu.Unlock()
	dh.d.MinBundleSize = 1
}

func readText(t *testing.T, path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// test_dependencies.py::test_version_compare
func TestVersionCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"2.42.3", "2.42.2", true}, {"v2.43.0", "2.42.10", true}, {"2.42.3", "2.42.3", false}, {"bad", "2.42.3", false},
		{"2.42.3", "", true}} {
		if IsNewer(c.a, c.b) != c.want {
			t.Errorf("is_newer(%q, %q) != %v", c.a, c.b, c.want)
		}
	}
}

// test_dependencies.py::test_active_bundle_prefers_newer_runtime
func TestActiveBundlePrefersNewerRuntime(t *testing.T) {
	dh := newDepsHarness(t)
	dir, version, _ := dh.service.Env.ActiveBundle()
	if dir != dh.builtin || version != "2.40.0" {
		t.Fatalf("%s %s", dir, version)
	}
	dh.writeRuntime("2.41.0")
	dir, version, _ = dh.service.Env.ActiveBundle()
	if dir != dh.runtime || version != "2.41.0" {
		t.Fatalf("%s %s", dir, version)
	}
}

// test_dependencies.py::test_image_upgrade_overrides_older_online_update
func TestImageUpgradeOverridesOlderOnlineUpdate(t *testing.T) {
	dh := newDepsHarness(t)
	dh.writeRuntime("2.39.9")
	if dir, version, _ := dh.service.Env.ActiveBundle(); dir != dh.builtin || version != "2.40.0" {
		t.Fatalf("%s %s", dir, version)
	}
}

// test_dependencies.py::test_base_url_always_builtin_when_bundled
func TestBaseURLAlwaysBuiltinWhenBundled(t *testing.T) {
	dh := newDepsHarness(t)
	dh.unsetenv("SUB_STORE_BACKEND_API_PORT")
	dh.setenv("SUB_STORE_URL", "http://sub-store:3001")
	if base := must[string](t)(dh.service.Env.BaseURL()); base != "http://127.0.0.1:3001" {
		t.Fatalf("base %s", base)
	}
}

// test_dependencies.py::test_custom_builtin_port_is_shared_by_client_readiness_and_update
func TestCustomBuiltinPortIsSharedByClientReadinessAndUpdate(t *testing.T) {
	dh := newDepsHarness(t)
	dh.setenv("SUB_STORE_BACKEND_API_PORT", dh.store.port())
	// An external URL must not divert the bundled process or its conversions.
	dh.setenv("SUB_STORE_URL", "http://unrelated-service:3001")
	expected := "http://127.0.0.1:" + dh.store.port()
	if base := must[string](t)(dh.service.Env.BaseURL()); base != expected {
		t.Fatalf("base %s", base)
	}
	dh.store.setVersion("2.40.0")
	if !must[bool](t)(dh.service.Runtime.isReady(dh.ctx)) {
		t.Fatal("not ready")
	}
	if v := must[string](t)(dh.d.waitForVersion(dh.ctx, "2.40.0", 40)); v != "2.40.0" {
		t.Fatalf("version %s", v)
	}
	dh.release = `{"tag_name": "v2.40.0", "assets": [{"name": "sub-store.bundle.js", "browser_download_url": "x"}]}`
	if ok, message := dh.d.StartSubStoreUpdate(dh.ctx); ok || message != "当前已是最新版本 2.40.0" {
		t.Fatalf("%v %s", ok, message)
	}
	if dh.store.envRequests != 3 {
		t.Fatalf("%d version requests", dh.store.envRequests)
	}
}

// test_dependencies.py::test_invalid_builtin_port_never_falls_back_to_an_unrelated_service
func TestInvalidBuiltinPortNeverFallsBackToAnUnrelatedService(t *testing.T) {
	for _, value := range []string{"", "0", "65536", "-1", "+3001", "30.01", "3001/path", "３００１"} {
		t.Run(value, func(t *testing.T) {
			dh := newDepsHarness(t)
			dh.setenv("SUB_STORE_BACKEND_API_PORT", value)
			if _, err := dh.service.Env.BaseURL(); err == nil || !strings.Contains(err.Error(), "SUB_STORE_BACKEND_API_PORT") ||
				pythonClass(err) != "ValueError" {
				t.Fatalf("base url err %v", err)
			}
			if _, err := dh.service.Runtime.isReady(dh.ctx); err == nil || !strings.Contains(err.Error(), "SUB_STORE_BACKEND_API_PORT") {
				t.Fatalf("ready err %v", err)
			}
			if dh.store.envRequests != 0 {
				t.Fatal("requested")
			}
		})
	}
}

// Valid forms of the port (ASCII digits, surrounding whitespace, leading zeros).
func TestBuiltinPortForms(t *testing.T) {
	dh := newDepsHarness(t)
	for value, want := range map[string]string{" 3001 ": "3001", "03001": "3001", "1": "1", "65535": "65535", " 3001\x1c": "3001"} {
		dh.setenv("SUB_STORE_BACKEND_API_PORT", value)
		if got := must[string](t)(dh.service.Env.BuiltinSubStoreURL()); got != "http://127.0.0.1:"+want {
			t.Errorf("%q: %s", value, got)
		}
	}
}

// test_dependencies.py::test_base_url_falls_back_to_env_without_bundle
func TestBaseURLFallsBackToEnvWithoutBundle(t *testing.T) {
	dh := newDepsHarness(t)
	os.Remove(filepath.Join(dh.builtin, SubStoreAsset))
	dh.setenv("SUB_STORE_URL", "http://dev-sub-store:3001/")
	if base := must[string](t)(dh.service.Env.BaseURL()); base != "http://dev-sub-store:3001" {
		t.Fatalf("base %s", base)
	}
}

// test_dependencies.py::test_update_refused_without_bundle
func TestUpdateRefusedWithoutBundle(t *testing.T) {
	dh := newDepsHarness(t)
	os.Remove(filepath.Join(dh.builtin, SubStoreAsset))
	ok, message := dh.d.StartSubStoreUpdate(dh.ctx)
	if ok || !strings.Contains(message, "内置") {
		t.Fatalf("%v %s", ok, message)
	}
}

func (dh *depsHarness) run(release *jsonx.Object) {
	dh.d.updateLock.Lock()
	dh.d.runUpdate(dh.ctx, release)
}

func (dh *depsHarness) unlocked(t *testing.T) {
	if !dh.d.updateLock.TryLock() {
		t.Fatal("update lock still held")
	}
	dh.d.updateLock.Unlock()
}

// test_dependencies.py::test_update_success_writes_runtime_bundle
func TestUpdateSuccessWritesRuntimeBundle(t *testing.T) {
	dh := newDepsHarness(t)
	dh.setenv("SUB_STORE_BACKEND_API_PORT", dh.store.port())
	content := []byte(`console.log("sub-store")`)
	dh.serve(content)
	dh.commands.runVersion = "2.41.0"
	dh.run(dh.releaseFor(content, "2.41.0"))
	state := dh.d.GetUpdateState()
	if state.GetString("state") != "success" || state.GetString("message") != "Sub-Store 已更新到 2.41.0" {
		t.Fatalf("state %s", jsonx.Repr(state))
	}
	if readText(t, filepath.Join(dh.runtime, SubStoreAsset)) != string(content) ||
		strings.TrimSpace(readText(t, filepath.Join(dh.runtime, "VERSION"))) != "2.41.0" {
		t.Fatal("bundle not written")
	}
	if dh.commands.restarts != 1 {
		t.Fatalf("restarts %d", dh.commands.restarts)
	}
	dh.unlocked(t)
	if lifecycle := dh.service.Runtime.read(); lifecycle.GetString("state") != "running" {
		t.Fatalf("runtime state %s", jsonx.Repr(lifecycle))
	}
	check := dh.commands.calls[len(dh.commands.calls)-2]
	if strings.Join(check, " ") != "node --check "+filepath.Join(dh.runtime, "sub-store.download.js") {
		t.Fatalf("calls %v", dh.commands.calls)
	}
}

// test_dependencies.py::test_update_rolls_back_when_new_version_does_not_start
func TestUpdateRollsBackWhenNewVersionDoesNotStart(t *testing.T) {
	dh := newDepsHarness(t)
	dh.setenv("SUB_STORE_BACKEND_API_PORT", dh.store.port())
	dh.writeRuntime("2.40.5")
	content := []byte("broken bundle")
	dh.serve(content)
	dh.commands.onRestart = func(n int) (CommandResult, error) {
		if n == 1 {
			dh.store.setVersion("") // the new version never answers
		} else {
			dh.store.setVersion("2.40.5")
		}
		return CommandResult{}, nil
	}
	dh.run(dh.releaseFor(content, "2.41.0"))
	state := dh.d.GetUpdateState()
	if state.GetString("state") != "failed" || state.GetString("message") != "更新失败：Sub-Store 2.41.0 启动后未就绪；已回滚到 2.40.5" {
		t.Fatalf("state %s", jsonx.Repr(state))
	}
	if strings.TrimSpace(readText(t, filepath.Join(dh.runtime, "VERSION"))) != "2.40.5" ||
		readText(t, filepath.Join(dh.runtime, SubStoreAsset)) != "runtime" {
		t.Fatal("not rolled back")
	}
	if dh.commands.restarts != 2 {
		t.Fatalf("restarts %d", dh.commands.restarts)
	}
	if _, err := os.Stat(dh.runtime + ".bak"); err == nil {
		t.Fatal("backup left behind")
	}
	if state.Value("finished_at") == jsonx.Float(0) {
		t.Fatal("finished_at not set")
	}
}

// test_dependencies.py::test_update_rejects_checksum_mismatch
func TestUpdateRejectsChecksumMismatch(t *testing.T) {
	dh := newDepsHarness(t)
	dh.setenv("SUB_STORE_BACKEND_API_PORT", dh.store.port())
	dh.serve([]byte("tampered"))
	dh.run(dh.releaseFor([]byte("original"), "2.41.0"))
	state := dh.d.GetUpdateState()
	if state.GetString("state") != "failed" || state.GetString("message") != "更新失败：文件校验失败（SHA256 不匹配），请检查 GitHub 代理设置" {
		t.Fatalf("state %s", jsonx.Repr(state))
	}
	if _, err := os.Stat(filepath.Join(dh.runtime, SubStoreAsset)); err == nil {
		t.Fatal("bundle installed")
	}
	if _, err := os.Stat(filepath.Join(dh.runtime, "sub-store.download.js")); err == nil {
		t.Fatal("download left behind")
	}
	if dh.commands.restarts != 0 {
		t.Fatal("restarted")
	}
}

// test_dependencies.py::test_update_restores_previous_bundle_when_restart_raises
func TestUpdateRestoresPreviousBundleWhenRestartRaises(t *testing.T) {
	dh := newDepsHarness(t)
	dh.setenv("SUB_STORE_BACKEND_API_PORT", dh.store.port())
	dh.writeRuntime("2.40.5")
	content := []byte("new bundle")
	dh.serve(content)
	dh.commands.onRestart = func(n int) (CommandResult, error) {
		if n == 1 {
			return CommandResult{Stderr: "restart failed\n", ReturnCode: 1}, nil
		}
		dh.store.setVersion("2.40.5")
		return CommandResult{}, nil
	}
	dh.run(dh.releaseFor(content, "2.41.0"))
	if dh.commands.restarts != 2 {
		t.Fatalf("restarts %d", dh.commands.restarts)
	}
	message := dh.d.GetUpdateState().GetString("message")
	if message != "更新失败：exception_type=RuntimeError；已回滚到 2.40.5" {
		t.Fatalf("message %s", message)
	}
	if readText(t, filepath.Join(dh.runtime, SubStoreAsset)) != "runtime" ||
		strings.TrimSpace(readText(t, filepath.Join(dh.runtime, "VERSION"))) != "2.40.5" {
		t.Fatal("not restored")
	}
}

// Other failure paths of the update: too small, invalid JavaScript, a
// rollback that does not come back, a busy runtime.
func TestUpdateFailureMessages(t *testing.T) {
	t.Run("too small", func(t *testing.T) {
		dh := newDepsHarness(t)
		dh.serve([]byte("tiny"))
		dh.d.MinBundleSize = 500 * 1024
		dh.run(dh.releaseFor([]byte("tiny"), "2.41.0"))
		if m := dh.d.GetUpdateState().GetString("message"); m != "更新失败：下载的文件过小（4 字节），可能不是有效的 Sub-Store 后端" {
			t.Fatalf("message %s", m)
		}
	})
	t.Run("not javascript", func(t *testing.T) {
		dh := newDepsHarness(t)
		dh.serve([]byte("not js"))
		dh.commands.checkCode = 1
		dh.run(dh.releaseFor([]byte("not js"), "2.41.0"))
		if m := dh.d.GetUpdateState().GetString("message"); m != "更新失败：下载的文件不是有效的 JavaScript：SyntaxError: Unexpected token" {
			t.Fatalf("message %s", m)
		}
	})
	t.Run("rollback incomplete", func(t *testing.T) {
		dh := newDepsHarness(t)
		dh.setenv("SUB_STORE_BACKEND_API_PORT", dh.store.port())
		dh.serve([]byte("x"))
		dh.commands.onRestart = func(int) (CommandResult, error) { dh.store.setVersion(""); return CommandResult{}, nil }
		dh.run(dh.releaseFor([]byte("x"), "2.41.0"))
		if m := dh.d.GetUpdateState().GetString("message"); m != "更新失败：Sub-Store 2.41.0 启动后未就绪；回滚未完成，请检查容器日志" {
			t.Fatalf("message %s", m)
		}
		if _, err := os.Stat(dh.runtime); err == nil {
			t.Fatal("runtime dir kept without a previous online update")
		}
	})
	t.Run("maintenance fails", func(t *testing.T) {
		// Anything that escapes _install_release (the maintenance locks) is
		// reported as busy.
		dh := newDepsHarness(t)
		dh.d.runtime = &Runtime{env: dh.service.Env, Directory: filepath.Join(t.TempDir(), "file")}
		os.WriteFile(dh.d.runtime.Directory, nil, 0o644)
		dh.d.updateLock.Lock()
		dh.d.runUpdate(dh.ctx, dh.releaseFor([]byte("x"), "2.41.0"))
		if m := dh.d.GetUpdateState().GetString("message"); m != "Sub-Store 正忙或服务不可用，请稍后重试" {
			t.Fatalf("message %s", m)
		}
		dh.unlocked(t)
	})
}

// The latest release: parsed fields, the 30-minute cache, failures keep
// the last good result.
func TestFetchLatestRelease(t *testing.T) {
	dh := newDepsHarness(t)
	body := strings.Repeat("说明", 1500)
	dh.release = fmt.Sprintf(`{"tag_name": "V2.43.1", "published_at": "2026-11-01T00:00:00Z", "html_url": "https://github.com/r",
		"body": %q, "assets": [{"name": "other"}, {"name": "sub-store.bundle.js", "browser_download_url": "https://github.com/a/sub-store.bundle.js",
		"size": 600000, "digest": "sha256:abc"}]}`, body)
	data, message := dh.d.FetchLatestRelease(dh.ctx, false)
	if message != "" {
		t.Fatal(message)
	}
	want := jsonx.Obj("version", "2.43.1", "published_at", "2026-11-01T00:00:00Z", "release_url", "https://github.com/r",
		"notes", strings.Repeat("说明", 1000), "download_url", "https://github.com/a/sub-store.bundle.js",
		"size", jsonx.Int(600000), "sha256", "abc")
	if !jsonx.Equal(data, want) || strings.Join(data.Keys(), ",") != strings.Join(want.Keys(), ",") {
		t.Fatalf("data %s", jsonx.Repr(data))
	}
	dh.d.FetchLatestRelease(dh.ctx, false)
	if dh.apiHits != 1 {
		t.Fatal("not cached")
	}
	dh.clock.advance(30*60 - 1)
	dh.d.FetchLatestRelease(dh.ctx, false)
	if dh.apiHits != 1 {
		t.Fatal("cache expired early")
	}
	dh.release = `{"assets": []}`
	kept, message := dh.d.FetchLatestRelease(dh.ctx, true)
	if kept != data || message != "检查更新失败：exception_type=ValueError" {
		t.Fatalf("%v %s", kept, message)
	}
	for release, want := range map[string]string{
		`[]`:                "检查更新失败：exception_type=AttributeError",
		`{"assets": null}`:  "检查更新失败：exception_type=TypeError",
		`{"assets": ["x"]}`: "检查更新失败：exception_type=AttributeError",
		`not json`:          "检查更新失败：exception_type=JSONDecodeError",
		`{"assets": [{"name": "sub-store.bundle.js", "digest": 5}]}`:      "检查更新失败：exception_type=AttributeError",
		`{"body": {"a": 1}, "assets": [{"name": "sub-store.bundle.js"}]}`: "检查更新失败：exception_type=TypeError",
	} {
		dh.release = release
		if _, message := dh.d.FetchLatestRelease(dh.ctx, true); message != want {
			t.Errorf("%s: %s", release, message)
		}
	}
	dh.github.Close()
	if _, message := dh.d.FetchLatestRelease(dh.ctx, true); message != "检查更新失败：exception_type=ConnectionError" {
		t.Fatalf("message %s", message)
	}
}

// get_sub_store_status: the dependency card.
func TestGetSubStoreStatus(t *testing.T) {
	dh := newDepsHarness(t)
	dh.setenv("SUB_STORE_BACKEND_API_PORT", dh.store.port())
	dh.store.setVersion("2.40.0")
	dh.writeRuntime("2.41.0")
	dh.release = `{"tag_name": "v2.42.0", "published_at": "p", "html_url": "u", "body": "n",
		"assets": [{"name": "sub-store.bundle.js", "browser_download_url": "d"}]}`
	dh.service.Runtime.getMode = func(context.Context) (string, error) { return "on_demand", nil }
	status := must[*jsonx.Object](t)(dh.d.GetSubStoreStatus(dh.ctx, false))
	want := jsonx.Obj("key", "sub-store", "name", "Sub-Store", "description", "订阅解析与节点格式转换",
		"homepage", "https://github.com/sub-store-org/Sub-Store", "running", true,
		"lifecycle", jsonx.Obj("managed", true, "mode", "on_demand", "state", "running", "idle_seconds", jsonx.Int(300), "error", ""),
		"current_version", "2.40.0", "builtin_version", "2.40.0", "online_updated", true, "runtime", "v26.0.0",
		"latest_version", "2.42.0", "latest_published_at", "p", "release_url", "u", "release_notes", "n", "check_error", "",
		"has_update", true, "updatable", true,
		"update", jsonx.Obj("state", "idle", "message", "", "target_version", "", "finished_at", jsonx.Float(0)))
	if !jsonx.Equal(status, want) {
		t.Fatalf("status %s", jsonx.Repr(status))
	}
	// Without the bundle: detection only.
	os.Remove(filepath.Join(dh.builtin, SubStoreAsset))
	dh.setenv("SUB_STORE_URL", dh.store.srv.URL)
	status = must[*jsonx.Object](t)(dh.d.GetSubStoreStatus(dh.ctx, false))
	if status.Value("updatable") != false || status.GetString("runtime") != "" || status.GetString("builtin_version") != "" ||
		status.Value("online_updated") != false || status.GetObject("lifecycle").GetString("mode") != "external" {
		t.Fatalf("status %s", jsonx.Repr(status))
	}
}

// start_sub_store_update through to success (the background run is
// synchronous in tests).
func TestStartSubStoreUpdateRunsTheUpdate(t *testing.T) {
	dh := newDepsHarness(t)
	dh.setenv("SUB_STORE_BACKEND_API_PORT", dh.store.port())
	dh.store.setVersion("2.40.0")
	content := []byte("console.log(1)")
	dh.serve(content)
	sum := sha256.Sum256(content)
	dh.release = fmt.Sprintf(`{"tag_name": "v2.41.0", "assets": [{"name": "sub-store.bundle.js",
		"browser_download_url": %q, "digest": "sha256:%s"}]}`, dh.github.URL+"/dl/sub-store.bundle.js", hex.EncodeToString(sum[:]))
	dh.commands.runVersion = "2.41.0"
	ok, message := dh.d.StartSubStoreUpdate(dh.ctx)
	if !ok || message != "开始更新到 2.41.0" {
		t.Fatalf("%v %s", ok, message)
	}
	state := dh.d.GetUpdateState()
	if state.GetString("state") != "success" || state.GetString("target_version") != "2.41.0" {
		t.Fatalf("state %s", jsonx.Repr(state))
	}
	dh.unlocked(t)
	dh.d.updateLock.Lock()
	if ok, message := dh.d.StartSubStoreUpdate(dh.ctx); ok || message != "已有更新任务在进行中" {
		t.Fatalf("%v %s", ok, message)
	}
	dh.d.updateLock.Unlock()
	// GitHub unreachable: the last good release is still used (and is current).
	dh.github.Close()
	if ok, message := dh.d.StartSubStoreUpdate(dh.ctx); ok || message != "当前已是最新版本 2.41.0" {
		t.Fatalf("%v %s", ok, message)
	}
	fresh := newDepsHarness(t)
	fresh.github.Close()
	if ok, message := fresh.d.StartSubStoreUpdate(fresh.ctx); ok || message != "检查更新失败：exception_type=ConnectionError" {
		t.Fatalf("%v %s", ok, message)
	}
}
