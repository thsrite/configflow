package webdav_test

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/api/settings"
	"configflow/internal/auth"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/repo/repotest"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/testgolden"
	"configflow/internal/webdav"
)

const davJWTSecret = "c35-synthetic-jwt-secret"

var davFixedTime = time.Date(2026, 10, 22, 12, 0, 0, 123456000, time.UTC)

type fakeDAV struct {
	mu      sync.Mutex
	dirs    map[string]bool
	files   map[string]json.RawMessage
	fail    string
	code    int
	uploads int
	server  *httptest.Server
}

func newFakeDAV(t *testing.T) *fakeDAV {
	t.Helper()
	d := &fakeDAV{dirs: map[string]bool{"/": true}, files: map[string]json.RawMessage{}}
	d.server = httptest.NewServer(http.HandlerFunc(d.serve))
	t.Cleanup(d.server.Close)
	return d
}

func (d *fakeDAV) serve(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	user, password, ok := r.BasicAuth()
	if !ok || user != "fixture-user" || password != "fixture-password" {
		w.WriteHeader(401)
		return
	}
	remote := r.URL.Path
	if d.fail == r.Method {
		w.WriteHeader(d.code)
		fmt.Fprint(w, "fixture-user fixture-password private-response-body https://credential:secret@example.com/")
		return
	}
	switch r.Method {
	case "HEAD":
		if !d.dirs[remote] && d.files[remote] == nil {
			w.WriteHeader(404)
		}
	case "MKCOL":
		d.dirs[remote] = true
		w.WriteHeader(201)
	case "PUT":
		data, _ := io.ReadAll(r.Body)
		d.files[remote] = data
		d.uploads++
		w.WriteHeader(201)
	case "PROPFIND":
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(207)
		fmt.Fprint(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">`)
		names := []string{remote}
		for name := range d.files {
			if strings.HasPrefix(name, remote) {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprint(w, `<d:response><d:href>`)
			_ = xml.EscapeText(w, []byte(name))
			fmt.Fprint(w, `</d:href><d:propstat><d:prop><d:resourcetype/></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)
		}
		fmt.Fprint(w, `</d:multistatus>`)
	case "DELETE":
		delete(d.files, remote)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func (d *fakeDAV) setFailure(method string, status int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fail, d.code = method, status
}

func (d *fakeDAV) fileNames() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, 0, len(d.files))
	for name := range d.files {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

type davLoopbackClient struct {
	client *http.Client
	host   string
}

func (c davLoopbackClient) Do(req *http.Request) (*http.Response, error) {
	if req.URL.Host != c.host {
		return nil, fmt.Errorf("non-fixture network request refused")
	}
	return c.client.Do(req)
}

type davWorld struct {
	t     *testing.T
	srv   *httpxtest.Server
	dav   *fakeDAV
	token string
}

func newDAVWorld(t *testing.T, retention map[string]any) *davWorld {
	t.Helper()
	gd := newFakeDAV(t)
	srv := httpxtest.New(t, httpxtest.Options{
		Auth:      true,
		Username:  "diff-admin",
		Password:  "diff-password",
		SecretKey: davJWTSecret,
	})
	ctx := srv.Context()
	backup := map[string]any{
		"webdav_url":      gd.server.URL,
		"webdav_username": "fixture-user",
		"webdav_password": "fixture-password",
		"webdav_path":     "/backups/",
		"auto_backup":     false,
	}
	if err := srv.Repo.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		raw, err := json.Marshal(backup)
		if err != nil {
			return err
		}
		if err := s.SetJSON("backup", raw); err != nil {
			return err
		}
		s.SystemConfig.ServerDomain = "http://127.0.0.1:5001"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(retention) > 0 {
		for k, v := range retention {
			backup[k] = v
		}
		raw, err := json.Marshal(backup)
		if err != nil {
			t.Fatal(err)
		}
		repotest.WriteRow(t, srv.Repo.DatabasePath(), "system", "", "backup", raw)
	}
	service := webdav.NewService(davLoopbackClient{gd.server.Client(), strings.TrimPrefix(gd.server.URL, "http://")})
	service.Now = func() time.Time { return davFixedTime }
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	t.Cleanup(func() { rt.Stop(5 * time.Second) })
	registry := tasks.NewRegistry(srv.Repo, rt)
	service.Repo = srv.Repo
	service.Tasks = registry
	service.Notifications = notifications.New(notifications.Options{
		Repo: srv.Repo, Tasks: registry,
		Agents: func(context.Context) ([]model.Agent, error) { return []model.Agent{}, nil },
		Clock:  func() time.Time { return davFixedTime },
	})
	for _, slot := range registry.Missing() {
		registry.SetRowBuilder(slot, func(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error) { return []*tasks.Task{}, nil })
	}
	service.Register(rt, registry)
	service.Subscribe(srv.Repo)
	settings.NewHandler(srv.Repo, nil, service).Register(srv.App.Router)
	token, err := auth.EncodeHS256(map[string]any{"username": "diff-admin", "exp": time.Now().Add(time.Hour).Unix()}, davJWTSecret)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = service.WaitIdle(ctx)
	})
	return &davWorld{t: t, srv: srv, dav: gd, token: token}
}

var davURLRe = regexp.MustCompile(`http://127\.0\.0\.1:\d+`)

func normalizeDAVGolden(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = normalizeDAVGolden(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeDAVGolden(item)
		}
		return out
	case string:
		return davURLRe.ReplaceAllString(x, "<DAV>")
	}
	return v
}

func (w *davWorld) record(ref *testgolden.Reference, label, method, path, body string, useAuth bool) {
	w.t.Helper()
	body = strings.ReplaceAll(body, "<DAV>", w.dav.server.URL)
	var opts []httpxtest.Option
	if useAuth {
		opts = append(opts, httpxtest.Bearer(w.token))
	}
	if body != "" {
		opts = append(opts, httpxtest.Body([]byte(body), "application/json"))
	}
	resp := w.srv.Do(method, path, opts...)
	var parsed any
	if err := json.Unmarshal(resp.Body, &parsed); err != nil {
		parsed = string(resp.Body)
	}
	key, _ := json.Marshal(map[string]string{"case": label})
	actual, _ := json.Marshal(normalizeDAVGolden(map[string]any{
		"status": resp.Status,
		"body":   parsed,
		"files":  w.dav.fileNames(),
	}))
	ref.Assert(w.t, key, actual)
}

func TestWebDAVGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})
	const conn = `{"webdav_url":"<DAV>","webdav_username":"fixture-user","webdav_password":"******","webdav_path":"one/two"}`

	t.Run("routes-and-validation", func(t *testing.T) {
		w := newDAVWorld(t, nil)
		w.record(ref, "get-config", "GET", "/api/backup/config", "", true)
		invalidBodies := []string{
			`null`, `[]`, `"text"`, `{}`, `{"unknown":true}`, `{"webdav_url":null}`,
			`{"auto_backup":"false"}`, `{"keep_latest":true}`, `{"keep_latest":1001}`,
			`{"keep_latest":-1}`, `{"keep_daily":3651}`, `{"keep_latest":1.5}`,
			`{"keep_latest":null}`, `{"webdav_username":3}`,
		}
		for _, endpoint := range []string{"config", "test", "now"} {
			for i, body := range invalidBodies {
				w.record(ref, fmt.Sprintf("invalid-%s-%d", endpoint, i), "POST", "/api/backup/"+endpoint, body, true)
			}
		}
		w.record(ref, "config-boundary", "POST", "/api/backup/config", `{"keep_latest":0,"keep_daily":3650,"webdav_password":"******","webdav_path":"/new/"}`, true)
		w.record(ref, "test-unauth", "POST", "/api/backup/test", conn, false)
		w.record(ref, "now-unauth", "POST", "/api/backup/now", conn, false)
		w.record(ref, "test-ok", "POST", "/api/backup/test", conn, true)
		w.record(ref, "now-ok", "POST", "/api/backup/now", conn, true)
		w.record(ref, "test-saved", "POST", "/api/backup/test", `{"auto_backup":false}`, true)
		w.record(ref, "now-saved", "POST", "/api/backup/now", `{"auto_backup":false}`, true)
	})

	t.Run("failures-and-retention", func(t *testing.T) {
		for _, method := range []string{"", "HEAD", "MKCOL", "PUT", "PROPFIND", "DELETE"} {
			w := newDAVWorld(t, nil)
			for i := range 4 {
				w.dav.files[fmt.Sprintf("/one/two/config_backup_20200101_10000%d_000000.json", i)] = []byte(`{}`)
			}
			w.dav.files["/one/two/foreign.txt"] = []byte(`"keep"`)
			if method != "" {
				w.dav.setFailure(method, 403)
			}
			body := `{"webdav_url":"<DAV>","webdav_username":"fixture-user","webdav_password":"******","webdav_path":"one/two","keep_latest":1,"keep_daily":0}`
			label := method
			if label == "" {
				label = "none"
			}
			w.record(ref, "fail-"+label+"-test", "POST", "/api/backup/test", body, true)
			w.record(ref, "fail-"+label+"-now", "POST", "/api/backup/now", body, true)
		}
	})

	t.Run("security-and-overrides", func(t *testing.T) {
		w := newDAVWorld(t, nil)
		w.record(ref, "userinfo-save", "POST", "/api/backup/config", `{"webdav_url":"https://uri-user:uri-private-secret@example.com/dav/"}`, true)
		w.record(ref, "userinfo-get", "GET", "/api/backup/config", "", true)
		w.record(ref, "blank-password-keep", "POST", "/api/backup/config", `{"webdav_password":""}`, true)

		w2 := newDAVWorld(t, map[string]any{"webdav_username": 42, "webdav_path": 17})
		override := `{"webdav_url":"<DAV>","webdav_username":"fixture-user","webdav_password":"fixture-password","webdav_path":"one/two"}`
		w2.record(ref, "override-malformed", "POST", "/api/backup/now", override, true)

		w3 := newDAVWorld(t, map[string]any{"keep_latest": "5", "keep_daily": -3})
		for i := range 31 {
			w3.dav.files[fmt.Sprintf("/one/two/config_backup_20200101_%06d_000000.json", i)] = []byte(`{}`)
		}
		w3.record(ref, "imported-retention-get", "GET", "/api/backup/config", "", true)
		w3.record(ref, "imported-retention-now", "POST", "/api/backup/now", conn, true)
	})

	t.Run("directory-variants", func(t *testing.T) {
		for i, dirPath := range []string{"", "/", "one//two", "/directory with spaces/", "/备份/", "/a%2Fb/"} {
			w := newDAVWorld(t, nil)
			enc, _ := json.Marshal(dirPath)
			body := fmt.Sprintf(`{"webdav_url":"<DAV>","webdav_username":"fixture-user","webdav_password":"******","webdav_path":%s}`, enc)
			w.record(ref, fmt.Sprintf("dir-%d-test", i), "POST", "/api/backup/test", body, true)
			w.record(ref, fmt.Sprintf("dir-%d-now", i), "POST", "/api/backup/now", body, true)
		}
	})
}
