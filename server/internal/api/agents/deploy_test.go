package agents

import (
	core "configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/deploy"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newDeployHTTP(t *testing.T) (*httpxtest.Server, httpxtest.Option, *agentfake.Agent) {
	t.Helper()
	lifecycle.Stop(8 * time.Second)
	if e := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); e != nil {
		t.Fatal(e)
	}
	lifecycle.Start(context.Background())
	t.Cleanup(func() { lifecycle.Stop(8 * time.Second) })
	mihomoenv.Wire()
	deploy.Wire()
	fake := agentfake.New(agentfake.Options{AgentID: "publish-agent", Token: "synthetic-publish-token", ConfigPath: "installed.yaml"})
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
