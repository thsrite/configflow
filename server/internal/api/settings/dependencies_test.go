package settings_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/substore"
)

const modePath = "/api/settings/dependencies/sub-store/mode"

// commands emulates supervisorctl and node for the bundled Sub-Store.
type commands struct{}

func (commands) Run(_ context.Context, argv []string, _ time.Duration) (substore.CommandResult, error) {
	if argv[0] == "node" {
		return substore.CommandResult{Stdout: "v26.0.0\n"}, nil
	}
	return substore.CommandResult{Stdout: "sub-store RUNNING pid 1\n"}, nil
}

// subStoreWorld installs a service with a bundled Sub-Store answering on
// loopback (version 2.40.0) and a GitHub API answering latestRelease.
func subStoreWorld(t *testing.T, builtin bool, latestRelease string) *substore.Service {
	base := t.TempDir()
	builtinDir := filepath.Join(base, "opt")
	os.MkdirAll(builtinDir, 0o755)
	if builtin {
		os.WriteFile(filepath.Join(builtinDir, substore.SubStoreAsset), []byte("bundle"), 0o644)
		os.WriteFile(filepath.Join(builtinDir, "VERSION"), []byte("2.40.0\n"), 0o644)
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"success","data":{"version":"2.40.0"}}`)
	}))
	t.Cleanup(fake.Close)
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, latestRelease)
	}))
	t.Cleanup(github.Close)
	port := fake.URL[strings.LastIndex(fake.URL, ":")+1:]
	vars := map[string]string{"SUB_STORE_BUILTIN_DIR": builtinDir, "SUB_STORE_BACKEND_API_PORT": port, "SUB_STORE_URL": fake.URL}
	env := substore.NewEnv()
	env.LookupEnv = func(key string) (string, bool) { v, ok := vars[key]; return v, ok }
	env.DataDir = func() string { return filepath.Join(base, "data") }
	env.Commands = commands{}
	service := substore.NewService(env)
	service.Runtime.Directory = filepath.Join(base, "runtime")
	service.Dependencies.LatestReleaseURL = github.URL + "/repos/sub-store-org/Sub-Store/releases/latest"
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 20 * time.Millisecond})
	runner.Start(context.Background())
	release := make(chan struct{})
	service.Dependencies.Spawner = heldUpdateSpawner{runner: runner, release: release}
	t.Cleanup(func() { close(release); runner.Shutdown(context.Background()) })
	t.Cleanup(substore.SetDefault(service))
	return service
}

// test_sub_store_runtime.py::test_mode_route_validation_and_atomic_persistence
func TestModeRouteValidationAndAtomicPersistence(t *testing.T) {
	subStoreWorld(t, true, `{}`)
	srv := newServer(t, false)
	for _, body := range []string{"", "[]", "{}", `{"mode": "stop"}`, `{"mode": ["always"]}`, "not json"} {
		options := []httpxtest.Option{}
		if body != "" {
			options = append(options, httpxtest.Body([]byte(body), "application/json"))
		}
		srv.Post(modePath, options...).WantStatus(400).
			WantJSON(jsonx.Obj("success", false, "message", "请选择按需启动或常驻运行"))
	}
	before := system(t, srv)
	srv.Post(modePath, httpxtest.JSON(jsonx.Obj("mode", "always"))).WantStatus(200).
		WantJSON(jsonx.Obj("success", true, "mode", "always"))
	after := system(t, srv)
	if after.GetObject("system_config").GetString("sub_store_mode") != "always" {
		t.Fatal("mode not saved")
	}
	if !jsonx.Equal(after.Value("agents"), before.Value("agents")) || !jsonx.Equal(after.Value("backup"), before.Value("backup")) {
		t.Fatal("other sections changed")
	}
	for key, value := range before.GetObject("system_config").All() {
		if !jsonx.Equal(after.GetObject("system_config").Value(key), value) {
			t.Fatalf("%s changed", key)
		}
	}
	authed := newServer(t, true)
	authed.Post(modePath, httpxtest.JSON(jsonx.Obj("mode", "on_demand"))).WantStatus(401)
}

// An external Sub-Store's mode is managed elsewhere: 409 with the message.
func TestModeRouteRefusesExternalSubStore(t *testing.T) {
	subStoreWorld(t, false, `{}`)
	srv := newServer(t, false)
	srv.Post(modePath, httpxtest.JSON(jsonx.Obj("mode", "always"))).WantStatus(409).
		WantJSON(jsonx.Obj("success", false, "message", "外部 Sub-Store 的运行模式由其部署环境管理"))
}

func TestDependencyListRoute(t *testing.T) {
	subStoreWorld(t, true, `{"tag_name": "v2.41.0", "assets": [{"name": "sub-store.bundle.js", "browser_download_url": "x"}]}`)
	srv := newServer(t, false)
	response := srv.Get("/api/settings/dependencies?refresh=1").WantStatus(200)
	var wire struct {
		Dependencies []struct {
			Update struct {
				FinishedAt json.Number `json:"finished_at"`
			} `json:"update"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(response.Body, &wire); err != nil || len(wire.Dependencies) != 1 || wire.Dependencies[0].Update.FinishedAt.String() != "0.0" {
		t.Fatal("idle completion time lost its floating-point JSON token", string(response.Body), err)
	}
	body := response.Object()
	list := body.GetList("dependencies")
	if len(list) != 1 {
		t.Fatalf("body %s", jsonx.Repr(body))
	}
	card := list[0].(*jsonx.Object)
	if card.GetString("current_version") != "2.40.0" || card.GetString("latest_version") != "2.41.0" ||
		card.Value("has_update") != true || card.GetObject("lifecycle").GetString("state") != "running" ||
		card.GetString("runtime") != "v26.0.0" {
		t.Fatalf("card %s", jsonx.Repr(card))
	}
	newServer(t, true).Get("/api/settings/dependencies").WantStatus(401)
}

func TestUpdateRoute(t *testing.T) {
	subStoreWorld(t, true, `{"tag_name": "v2.41.0", "assets": [{"name": "sub-store.bundle.js", "browser_download_url": "x"}]}`)
	srv := newServer(t, false)
	srv.Post("/api/settings/dependencies/sub-store/update").WantStatus(202).
		WantJSON(jsonx.Obj("success", true, "message", "开始更新到 2.41.0"))
	// The (not started) background task still holds the update lock.
	srv.Post("/api/settings/dependencies/sub-store/update").WantStatus(409).
		WantJSON(jsonx.Obj("success", false, "message", "已有更新任务在进行中"))
	subStoreWorld(t, false, `{}`)
	srv.Post("/api/settings/dependencies/sub-store/update").WantStatus(409).
		WantJSON(jsonx.Obj("success", false, "message", "当前环境没有内置 Sub-Store，无法在线更新"))
}

// heldUpdateSpawner retains the route's in-progress observation without
// dropping work: every accepted callback runs once in a tracked goroutine.
type heldUpdateSpawner struct {
	runner  *lifecycle.Runtime
	release <-chan struct{}
}

func (s heldUpdateSpawner) Spawn(ctx context.Context, name string, fn func(context.Context)) error {
	return s.runner.Spawn(ctx, name, func(worker context.Context) {
		select {
		case <-s.release:
		case <-worker.Done():
		}
		fn(worker)
	})
}
