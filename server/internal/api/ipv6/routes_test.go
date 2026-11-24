package ipv6

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

type fakeClient struct{}

func (fakeClient) Do(_ context.Context, r agents.Request) (*agents.Response, error) {
	body := `{"success":true,"interface":"eth0","interface_index":2,"can_prepare":true,"checks":[{"id":"interface","status":"pass","message":"synthetic evidence"}],"persistence":{},"core":{"ipv6":true,"dns.ipv6":false}}`
	if strings.HasSuffix(r.URL, "/api/capabilities") {
		body = `{}`
	} else if strings.HasSuffix(r.URL, "/probe") {
		body = `{"success":true,"connectivity":{"status":"ok","message":"synthetic TCP"}}`
	}
	return &agents.Response{StatusCode: 200, Body: []byte(body)}, nil
}

type routeCase struct {
	Name, Method, Path, Body, Profile string
	Admin                             bool
}
type routeAnswer struct {
	Status int    `json:"status"`
	Body   any    `json:"body"`
	Cache  string `json:"cache"`
}

func project(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, value := range x {
			switch k {
			case "check_token":
				x[k] = "opaque"
			case "checked_at":
				x[k] = "timestamp"
			default:
				x[k] = project(value)
			}
		}
	case []any:
		for i, item := range x {
			x[i] = project(item)
		}
	}
	return v
}
func TestRoutesAgainstFlask(t *testing.T) {
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: time.Millisecond})
	rt.Start(context.Background())
	t.Cleanup(func() { rt.Stop(time.Second) })
	srv := httpxtest.New(t, httpxtest.Options{Auth: true})
	srv.App.Runtime = rt
	m, e := agents.New(srv.Context(), srv.Repo)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Close)
	m.SetClient(fakeClient{})
	NewHandler(srv.Repo, m, nil).Register(srv.App.Router)
	agentsRaw, _ := json.Marshal([]any{
		map[string]any{"id": "source", "name": "Synthetic", "host": "192.0.2.2", "port": 8080, "token": "synthetic-source-token", "profile_id": "default", "service_type": "mihomo", "deployment_method": "shell", "enabled": true, "status": "online", "last_heartbeat": time.Now().Format(time.RFC3339), "version": "1.8.50-go"},
		map[string]any{"id": "client", "name": "Client", "host": "192.0.2.3", "port": 8080, "token": "synthetic-client-token", "profile_id": "default", "service_type": "oxidns", "deployment_method": "shell", "enabled": true, "status": "online", "last_heartbeat": "2000-01-01T00:00:00+00:00"},
	})
	e = srv.Repo.UpdateSystem(srv.Context(), func(_ context.Context, sys *model.System) error {
		return sys.SetJSON("agents", agentsRaw)
	})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = srv.Repo.CreateProfileRecord(srv.Context(), repo.ProfileCreation{ID: "other", Name: "Other"}, ""); e != nil {
		t.Fatal(e)
	}
	_, e = srv.Repo.UpdateProfile(srv.Context(), "default", func(_ context.Context, c *model.Profile) error {
		return c.SetJSON("mihomo", json.RawMessage(`{"custom_config":"ipv6: true\ndns: {ipv6: false}\n","linux_gateway":{"enabled":true}}`))
	})
	if e != nil {
		t.Fatal(e)
	}
	root := "/api/agents/source/ipv6-readiness"
	cases := []routeCase{{"status", "GET", root, "", "default", true}, {"cached", "GET", root, "", "default", true}, {"prepare-current", "POST", root + "/prepare", `{"interface":"eth0","check_token":"@last"}`, "default", true}, {"probe", "POST", root + "/probe", `{}`, "default", true}, {"verification-read", "GET", root + "/verify", "", "default", true}, {"settings-read", "GET", root + "/verification-settings", "", "default", true}, {"settings-put", "PUT", root + "/verification-settings", `{"client_agent_id":"","test_url":"https://site.example/"}`, "default", true}, {"auth", "GET", root, "", "default", false}, {"profile-missing", "GET", root, "", "", true}, {"profile-conflict", "POST", root + "/probe", `{"profile_id":"other"}`, "default", true}, {"wrong-binding", "GET", root, "", "other", true}, {"malformed", "POST", root + "/probe", `{`, "default", true}, {"not-object", "POST", root + "/probe", `[]`, "default", true}, {"wrong-type", "PUT", root + "/verification-settings", `{"client_agent_id":null,"test_url":""}`, "default", true}, {"missing-field", "PUT", root + "/verification-settings", `{}`, "default", true}, {"no-token", "POST", root + "/prepare", `{"interface":"eth0","check_token":"x"}`, "default", true}, {"stale-token", "POST", root + "/prepare", `{"interface":"eth0","check_token":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, "default", true}, {"probe-external", "POST", root + "/probe", `{"host":"other.example"}`, "default", true}, {"verify-external", "POST", root + "/verify", `{"test_url":"https://other.example/"}`, "default", true}, {"bad-revision", "POST", root + "/verify", `{"expected_config_revision":true}`, "default", true}, {"head", "HEAD", root, "", "default", true}, {"unknown", "POST", root + "/other", `{}`, "default", true}, {"large", "POST", root + "/probe", strings.Repeat(" ", 4097), "default", true}}
	admin := srv.Login()
	answers := []routeAnswer{}
	lastToken := ""
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			request := httptest.NewRequest(tc.Method, tc.Path, strings.NewReader(strings.ReplaceAll(tc.Body, "@last", lastToken)))
			request.Header.Set("Content-Type", "application/json")
			if tc.Profile != "" {
				request.Header.Set("X-ConfigFlow-Profile", tc.Profile)
			}
			if tc.Admin {
				admin(request)
			}
			rec := httptest.NewRecorder()
			before := srv.Repo.CommitCount()
			srv.Handler().ServeHTTP(rec, request)
			if tc.Method == "HEAD" && srv.Repo.CommitCount() != before {
				t.Fatal("HEAD wrote repository")
			}
			var body any
			if tc.Method != "HEAD" {
				if json.Unmarshal(rec.Body.Bytes(), &body) != nil {
					t.Fatal("not JSON", rec.Code, rec.Body.String())
				}
			}
			if d, ok := body.(map[string]any); ok {
				if token, ok := d["check_token"].(string); ok {
					lastToken = token
				}
			}
			answers = append(answers, routeAnswer{rec.Code, project(body), rec.Header().Get("Cache-Control")})
		})
	}
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		return
	}
	payload, _ := json.Marshal(cases)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), "testdata/routes_reference.py")
	rootDir, _ := filepath.Abs("../../../..")
	cmd.Env = append(os.Environ(), "PYTHONPATH="+rootDir, "PYTHONDONTWRITEBYTECODE=1")
	cmd.Stdin = bytes.NewReader(payload)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, e := cmd.Output()
	if e != nil {
		t.Fatal(e, stderr.String())
	}
	var expected []routeAnswer
	if e = json.Unmarshal(out, &expected); e != nil {
		t.Fatal(e, string(out), stderr.String())
	}
	if len(expected) != len(answers) {
		t.Fatal("reference count", len(expected), len(answers))
	}
	for i, want := range expected {
		if !reflect.DeepEqual(answers[i], want) {
			a, _ := json.Marshal(answers[i])
			b, _ := json.Marshal(want)
			t.Errorf("%s\nGo %s\nFlask %s", cases[i].Name, a, b)
		}
	}
}
