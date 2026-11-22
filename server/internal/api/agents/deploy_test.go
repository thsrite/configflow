package agents

import (
	"bytes"
	core "configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/deploy"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/subs"
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newDeployHTTP(t *testing.T) (*httpxtest.Server, httpxtest.Option, *agentfake.Agent) {
	t.Helper()
	return newDeployHTTPWith(t, agentfake.Options{})
}

// newDeployHTTPWith is newDeployHTTP with fake Agent outcomes (identity fields are fixed).
func newDeployHTTPWith(t *testing.T, options agentfake.Options) (*httpxtest.Server, httpxtest.Option, *agentfake.Agent) {
	t.Helper()
	lifecycle.Stop(8 * time.Second)
	if e := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); e != nil {
		t.Fatal(e)
	}
	lifecycle.Start(context.Background())
	t.Cleanup(func() { lifecycle.Stop(8 * time.Second) })
	mihomoenv.Wire()
	deploy.Wire()
	options.AgentID, options.Token, options.ConfigPath = "publish-agent", "synthetic-publish-token", "installed.yaml"
	fake := agentfake.New(options)
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	host, port, _ := net.SplitHostPort(u.Host)
	n, _ := strconv.Atoi(port)
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(Register)})
	if err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
		return system.SetJSON("agents", mustRawJSON(t, []map[string]any{{
			"id": "publish-agent", "name": "Synthetic", "host": host, "port": n,
			"token": "synthetic-publish-token", "profile_id": "default", "service_type": "mihomo",
			"deployment_method": "docker", "version": "1.8.60-go", "enabled": true,
		}}))
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Repo.UpdateShared(s.Context(), func(_ context.Context, shared *model.Shared) error {
		if err := shared.SetJSON("nodes", json.RawMessage(`[]`)); err != nil {
			return err
		}
		return shared.SetJSON("subscriptions", json.RawMessage(`[]`))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Repo.UpdateProfile(s.Context(), "default", func(_ context.Context, profile *model.Profile) error {
		if err := profile.SetJSON("mihomo", json.RawMessage(`{"custom_config":"mode: rule\n"}`)); err != nil {
			return err
		}
		if err := profile.SetJSON("proxy_groups", json.RawMessage(`[]`)); err != nil {
			return err
		}
		return profile.SetJSON("rule_configs", json.RawMessage(`[]`))
	}); err != nil {
		t.Fatal(err)
	}
	m, err := core.Get(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return s, s.Login(), fake
}
func TestDeployHTTPPublishPreviewAndGuards(t *testing.T) {
	s, admin, fake := newDeployHTTP(t)
	base := "/api/agents/publish-agent"
	for _, path := range []string{"/push-config", "/generate-config", "/deployments/example/activate", "/deployments/example/rollback", "/deployments/example/recover"} {
		s.Post(base+path, httpxtest.JSON(map[string]any{})).WantStatus(401)
	}
	for _, path := range []string{"/deployments/example", "/generated-config/example/download", "/generated-config/example/file"} {
		s.Get(base + path).WantStatus(401)
	}
	s.Get(base + "/config").WantStatus(401)
	s.Get(base + "/config?token=bad").WantStatus(401)
	s.Get(base + "/config?token=synthetic-publish-token").WantStatus(200)
	s.Post(base+"/push-config", httpxtest.JSON([]any{1}), admin).WantStatus(400)
	s.Post(base+"/push-config", httpxtest.JSON(map[string]any{"expected_service_type": "mihomo"}), admin).WantStatus(400)
	s.Post(base+"/push-config", httpxtest.JSON(map[string]any{"restart": nil}), admin).WantStatus(400)
	s.Post(base+"/push-config", httpxtest.JSON(map[string]any{"deployment_id": "../bad"}), admin).WantStatus(400)
	rev, e := s.Repo.ConfigRevision(s.Context(), "default")
	if e != nil {
		t.Fatal(e)
	}
	s.Post(base+"/push-config", httpxtest.JSON(map[string]any{"expected_profile_id": "default", "expected_service_type": "mihomo", "expected_config_revision": rev + 1}), admin).WantStatus(409)
	generated := s.Post(base+"/generate-config", httpxtest.JSON(map[string]any{"expected_profile_id": "default", "expected_service_type": "mihomo", "base_url": "https://panel.example.com"}), admin)
	generated.WantStatus(200)
	pv := parseBodyMap(t, generated)
	key := mapStr(pv, "id")
	did := mapStr(pv, "deployment_id")
	if key == "" || did == "" || !strings.Contains(mapStr(pv, "content"), "mode: rule") {
		t.Fatal(pv)
	}
	file := s.Get(base+"/generated-config/"+key+"/file?path=installed.yaml", admin)
	file.WantStatus(200)
	if string(file.Body) != mapStr(pv, "content") {
		t.Fatal("preview bytes differ")
	}
	zip := s.Get(base+"/generated-config/"+key+"/download", admin)
	zip.WantStatus(200)
	if !strings.HasPrefix(string(zip.Body), "PK") {
		t.Fatal("not ZIP")
	}
	s.Get(base+"/generated-config/"+key+"/file?path=missing", admin).WantStatus(404)
	s.Get(base+"/generated-config/invalid/download", admin).WantStatus(409)
	pushed := s.Post(base+"/push-config", httpxtest.JSON(map[string]any{"generation_id": key, "restart": false}), admin)
	pushed.WantStatus(202)
	pushedObj := parseBodyMap(t, pushed)
	if mapStr(pushedObj, "deployment_id") != did {
		t.Fatal("new transaction for frozen generation")
	}
	if mapHas(pushedObj, "prepared_content") || mapHas(pushedObj, "oxidns_validation_receipt") {
		t.Fatal("private proof exposed")
	}
	s.Post(base+"/push-config", httpxtest.JSON(map[string]any{"deployment_id": "other"}), admin).WantStatus(409)
	s.Get(base+"/deployments/"+did, admin).WantStatus(200)
	s.Post(base+"/deployments/"+did+"/activate", admin).WantStatus(200)
	s.Post(base+"/deployments/"+did+"/rollback", admin).WantStatus(409)
	// Terminal rollback is a successful poll, irrespective of task success=false.
	s.Get(base+"/deployments/"+did, admin).WantStatus(200)
	s.Post(base+"/deployments/"+did+"/recover", admin).WantStatus(409)
	calls := fake.Calls()
	for _, call := range calls {
		if call.Path == "/api/deployments" && call.Headers.Get("X-Activate") != "false" {
			t.Fatal("wrong activation header")
		}
	}
}

func TestDeployHTTPEasyTierCoreVersions(t *testing.T) {
	tests := []struct {
		name           string
		version        string
		wantGenStatus  int
		wantPushStatus int
		wantWarnCount  int
		wantWarnSubstr string
	}{
		{
			name:           "alpha-100ff73",
			version:        "alpha-100ff73",
			wantGenStatus:  200,
			wantPushStatus: 202,
			wantWarnCount:  1,
			wantWarnSubstr: "alpha-100ff73",
		},
		{
			name:           "empty",
			version:        "",
			wantGenStatus:  200,
			wantPushStatus: 202,
			wantWarnCount:  0,
		},
		{
			name:           "release-v1.19.32",
			version:        "v1.19.32",
			wantGenStatus:  200,
			wantPushStatus: 202,
			wantWarnCount:  0,
		},
		{
			name:           "garbage-string",
			version:        "!!!garbage-version###",
			wantGenStatus:  200,
			wantPushStatus: 202,
			wantWarnCount:  1,
			wantWarnSubstr: "!!!garbage-version###",
		},
		{
			name:           "old-v1.19.30-rejected",
			version:        "v1.19.30",
			wantGenStatus:  400,
			wantPushStatus: 400,
			wantWarnCount:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			subs.ResetCoreVersionWarnState()
			t.Cleanup(subs.ResetCoreVersionWarnState)

			var logs bytes.Buffer
			root, err := logx.Setup(logx.Config{Level: logx.Warning, Console: &logs})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				root.Close()
				_, _ = logx.Setup(logx.Config{Level: logx.LevelFromEnv(), Console: os.Stderr})
			})

			s, admin, _ := newDeployHTTP(t)
			if err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
				return model.SetStoredMember(&system.Agents[0], "domain_discovery_enabled", json.RawMessage(`true`))
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Repo.UpdateProfile(s.Context(), "default", func(_ context.Context, profile *model.Profile) error {
				return profile.SetJSON("mihomo", json.RawMessage(`{
					"custom_config": "mode: rule\n",
					"easytier": {
						"enabled": true,
						"name": "EasyTier",
						"apply_to": "all",
						"routes": ["192.0.2.0/24"],
						"options": {
							"network-name": "lab-net",
							"peers": ["tcp://198.51.100.10:11010"]
						}
					}
				}`))
			}); err != nil {
				t.Fatal(err)
			}

			base := "/api/agents/publish-agent"
			if tc.version != "" {
				s.Post(base+"/traffic-report", httpxtest.JSON(map[string]any{
					"schema":         1,
					"status":         "running",
					"mihomo_version": tc.version,
					"window_start":   "2026-11-21T00:00:00Z",
					"window_end":     "2026-11-21T00:05:00Z",
					"ip_only_conns":  0,
					"dropped":        0,
					"items":          []any{},
				}), httpxtest.Header("Authorization", "Bearer synthetic-publish-token")).WantStatus(200)
			}

			// 1) GET /config (Agent pull)
			pull := s.Get(base + "/config?token=synthetic-publish-token")
			if tc.wantGenStatus == 200 {
				pull.WantStatus(200)
				if !strings.Contains(mapStr(parseBodyMap(t, pull), "content"), "type: easytier") {
					t.Fatalf("pull config missing easytier proxy: %s", pull.Text())
				}
			} else {
				if pull.Status == 200 {
					t.Fatalf("expected old core version pull failure, got 200")
				}
			}

			// 2) POST /generate-config
			gen := s.Post(base+"/generate-config", httpxtest.JSON(map[string]any{
				"expected_profile_id":   "default",
				"expected_service_type": "mihomo",
				"base_url":              "https://panel.example.com",
			}), admin)
			gen.WantStatus(tc.wantGenStatus)
			if tc.wantGenStatus == 200 {
				content := mapStr(parseBodyMap(t, gen), "content")
				if !strings.Contains(content, "type: easytier") {
					t.Fatalf("generated config missing easytier proxy: %s", content)
				}
			} else if !strings.Contains(mapStr(parseBodyMap(t, gen), "message"), "v1.19.30") {
				t.Fatalf("unexpected rejection message: %s", gen.Text())
			}

			// 3) POST /push-config (direct publish without generation_id, exercises prepareContent)
			push := s.Post(base+"/push-config", httpxtest.JSON(map[string]any{
				"deployment_id": "m16-" + strings.ReplaceAll(tc.name, ".", "-"),
				"restart":       false,
				"base_url":      "https://panel.example.com",
			}), admin)
			push.WantStatus(tc.wantPushStatus)
			if tc.wantPushStatus == 400 && !strings.Contains(mapStr(parseBodyMap(t, push), "message"), "v1.19.30") {
				t.Fatalf("unexpected push rejection message: %s", push.Text())
			}

			// Verify per-Agent throttled warning log across all 3 entry points.
			logText := logs.String()
			gotWarns := strings.Count(logText, "按未知版本处理")
			if gotWarns != tc.wantWarnCount {
				t.Fatalf("warning count=%d, want %d\nlogs:\n%s", gotWarns, tc.wantWarnCount, logText)
			}
			if tc.wantWarnSubstr != "" && !strings.Contains(logText, tc.wantWarnSubstr) {
				t.Fatalf("warning log missing %q:\n%s", tc.wantWarnSubstr, logText)
			}
		})
	}
}

// TestDeployHTTPAcceptedRecoveryIsAccepted is lab L2 D1: an accepted recovery
// answers 202 (Python 500 made the Agents page report a failure while the
// device recovered); a rejected one keeps its status code and rollback_failed.
func TestDeployHTTPAcceptedRecoveryIsAccepted(t *testing.T) {
	tests := []struct {
		name        string
		unsupported bool
		want        int
		wantStatus  string
	}{
		{"accepted", false, 202, "rolling_back"},
		{"agent before 1.8.57", true, 404, "rollback_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, admin, _ := newDeployHTTPWith(t, agentfake.Options{RollbackOutcome: "rollback_failed", RecoverUnsupported: test.unsupported})
			base := "/api/agents/publish-agent"
			pushed := s.Post(base+"/push-config", httpxtest.JSON(map[string]any{"restart": false}), admin)
			pushed.WantStatus(202)
			did := mapStr(parseBodyMap(t, pushed), "deployment_id")
			s.Post(base+"/deployments/"+did+"/activate", admin).WantStatus(200)
			s.Post(base+"/deployments/"+did+"/rollback", admin).WantStatus(409) // the fake answers the final state at once
			if got := mapStr(parseBodyMap(t, s.Get(base+"/deployments/"+did, admin)), "status"); got != "rollback_failed" {
				t.Fatalf("status before recovery = %q", got)
			}
			recovered := s.Post(base+"/deployments/"+did+"/recover", admin)
			recovered.WantStatus(test.want)
			if got := mapStr(parseBodyMap(t, recovered), "status"); got != test.wantStatus {
				t.Fatalf("recover status = %q, want %q", got, test.wantStatus)
			}
			if test.unsupported {
				return
			}
			if got := mapStr(parseBodyMap(t, s.Get(base+"/deployments/"+did, admin)), "status"); got != "rolled_back" {
				t.Fatalf("status after recovery = %q", got)
			}
		})
	}
}
