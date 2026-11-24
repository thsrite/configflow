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

	"configflow/internal/api/settings"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/substore"
	"configflow/internal/testjson"
)

const modePath = "/api/settings/dependencies/sub-store/mode"

// commands emulates node and the supervised process of the bundled Sub-Store.
type commands struct{}

func (commands) Run(_ context.Context, argv []string, _ time.Duration) (substore.CommandResult, error) {
	return substore.CommandResult{Stdout: "v26.0.0\n"}, nil
}

func (commands) Status() substore.ProcessState { return substore.ProcessRunning }
func (commands) Start(context.Context) error   { return nil }
func (commands) Stop(context.Context) error    { return nil }
func (commands) Restart(context.Context) error { return nil }

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
	env.Process = commands{}
	service := substore.NewService(env)
	service.Runtime.Directory = filepath.Join(base, "runtime")
	service.Dependencies.LatestReleaseURL = github.URL + "/repos/sub-store-org/Sub-Store/releases/latest"
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 20 * time.Millisecond})
	runner.Start(context.Background())
	release := make(chan struct{})
	service.Dependencies.Spawner = heldUpdateSpawner{runner: runner, release: release}
	t.Cleanup(func() { close(release); runner.Shutdown(context.Background()) })
	return service
}

func newSettingsWithSubStore(t *testing.T, svc *substore.Service) *httpxtest.Server {
	t.Helper()
	return httpxtest.New(t, httpxtest.Options{Auth: false, Setup: func(app *httpx.App) {
		svc.Repo = app.Repo
		svc.Env.Repo = app.Repo
		settings.NewHandler(app.Repo, svc, nil).Register(app.Router)
	}})
}

// test_sub_store_runtime.py::test_mode_route_validation_and_atomic_persistence
func TestModeRouteValidationAndAtomicPersistence(t *testing.T) {
	svc := subStoreWorld(t, true, `{}`)
	srv := newSettingsWithSubStore(t, svc)
	for _, body := range []string{"", "[]", "{}", `{"mode": "stop"}`, `{"mode": ["always"]}`, "not json"} {
		options := []httpxtest.Option{}
		if body != "" {
			options = append(options, httpxtest.Body([]byte(body), "application/json"))
		}
		res := srv.Post(modePath, options...).WantStatus(400)
		assertJSON(t, res, map[string]any{"success": false, "message": "请选择按需启动或常驻运行"})
	}
	before := systemMap(t, srv)
	res := srv.Post(modePath, httpxtest.JSON(map[string]any{"mode": "always"})).WantStatus(200)
	assertJSON(t, res, map[string]any{"success": true, "mode": "always"})
	after := systemMap(t, srv)
	if afterCfg := asMap(after["system_config"]); asString(afterCfg["sub_store_mode"]) != "always" {
		t.Fatal("mode not saved")
	}
	if !testjson.Equal(after["agents"], before["agents"]) || !testjson.Equal(after["backup"], before["backup"]) {
		t.Fatal("other sections changed")
	}
	beforeCfg := asMap(before["system_config"])
	afterCfg := asMap(after["system_config"])
	for key, value := range beforeCfg {
		if key == "sub_store_mode" {
			continue
		}
		if !testjson.Equal(afterCfg[key], value) {
			t.Fatalf("%s changed", key)
		}
	}
	authed := newServer(t, true)
	authed.Post(modePath, httpxtest.JSON(map[string]any{"mode": "on_demand"})).WantStatus(401)
}

// An external Sub-Store's mode is managed elsewhere: 409 with the message.
func TestModeRouteRefusesExternalSubStore(t *testing.T) {
	svc := subStoreWorld(t, false, `{}`)
	srv := newSettingsWithSubStore(t, svc)
	res := srv.Post(modePath, httpxtest.JSON(map[string]any{"mode": "always"})).WantStatus(409)
	assertJSON(t, res, map[string]any{"success": false, "message": "外部 Sub-Store 的运行模式由其部署环境管理"})
}

func TestDependencyListRoute(t *testing.T) {
	svc := subStoreWorld(t, true, `{"tag_name": "v2.41.0", "assets": [{"name": "sub-store.bundle.js", "browser_download_url": "x"}]}`)
	srv := newSettingsWithSubStore(t, svc)
	response := srv.Get("/api/settings/dependencies?refresh=1").WantStatus(200)
	var wire struct {
		Dependencies []struct {
			CurrentVersion string `json:"current_version"`
			LatestVersion  string `json:"latest_version"`
			HasUpdate      bool   `json:"has_update"`
			Runtime        string `json:"runtime"`
			Lifecycle      struct {
				State string `json:"state"`
			} `json:"lifecycle"`
			Update struct {
				FinishedAt json.Number `json:"finished_at"`
			} `json:"update"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(response.Body, &wire); err != nil || len(wire.Dependencies) != 1 || wire.Dependencies[0].Update.FinishedAt.String() != "0.0" {
		t.Fatal("idle completion time lost its floating-point JSON token", string(response.Body), err)
	}
	card := wire.Dependencies[0]
	if card.CurrentVersion != "2.40.0" || card.LatestVersion != "2.41.0" ||
		!card.HasUpdate || card.Lifecycle.State != "running" ||
		card.Runtime != "v26.0.0" {
		t.Fatalf("card %+v", card)
	}
	newServer(t, true).Get("/api/settings/dependencies").WantStatus(401)
}

func TestUpdateRoute(t *testing.T) {
	svc := subStoreWorld(t, true, `{"tag_name": "v2.41.0", "assets": [{"name": "sub-store.bundle.js", "browser_download_url": "x"}]}`)
	srv := newSettingsWithSubStore(t, svc)
	assertJSON(t, srv.Post("/api/settings/dependencies/sub-store/update").WantStatus(202),
		map[string]any{"success": true, "message": "开始更新到 2.41.0"})
	// The (not started) background task still holds the update lock.
	assertJSON(t, srv.Post("/api/settings/dependencies/sub-store/update").WantStatus(409),
		map[string]any{"success": false, "message": "已有更新任务在进行中"})
	svcExt := subStoreWorld(t, false, `{}`)
	srvExt := newSettingsWithSubStore(t, svcExt)
	assertJSON(t, srvExt.Post("/api/settings/dependencies/sub-store/update").WantStatus(409),
		map[string]any{"success": false, "message": "当前环境没有内置 Sub-Store，无法在线更新"})
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
