package server

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

	"configflow/internal/agents"
	apiagents "configflow/internal/api/agents"
	apionboarding "configflow/internal/api/gatewayonboarding"
	apisetup "configflow/internal/api/setup"
	"configflow/internal/gateway/host"
	"configflow/internal/gateway/installation"
	"configflow/internal/gateway/onboarding"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/setup"
	"configflow/internal/testjson"
)

func TestSetupInstallationServerRegistrationUsesRealOwner(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{RepoOptions: RepoOptions(), Auth: true})
	setupHandler := apisetup.NewHandler(srv.Repo, nil)
	setupHandler.Register(srv.App.Router)
	onboardingHandler := &apionboarding.Handler{Repo: srv.Repo, DNSLoop: onboarding.CheckDNSLoop}
	onboardingHandler.Register(srv.App.Router)
	admin := srv.Login()
	ctx := srv.Context()
	const id = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const profile = "setup_bbbbbbbbbbbbbbbbbbbb"
	srv.Post("/api/setup", httpxtest.JSON(map[string]string{"id": id, "name": "Synthetic", "goal": "gateway"}), admin).WantStatus(200)
	// Keep C36's own wizard path and session. Without A22 an abandon preview has
	// no installation evidence; the production wire must replace this 503.
	srv.Post("/api/setup/"+id+"/abandon", httpxtest.JSON(map[string]bool{"dry_run": true}), admin).WantStatus(503)
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	app, err := New(context.Background(), Options{Repo: srv.Repo, Runtime: rt})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Agents.Close)
	setupHandler.Repo = srv.Repo
	setupHandler.Owners = &app.SetupOwners
	apiagents.NewHandler(apiagents.Deps{Repo: srv.Repo, Agents: app.Agents}).Register(srv.App.Router)
	onboardingHandler.Repo = srv.Repo
	onboardingHandler.AdoptOwnedWrite = setup.AdoptOwnedWrite
	err = srv.Repo.UpdateShared(ctx, func(_ context.Context, c *model.Shared) error {
		c.Nodes = []model.Node{testjson.Decode[model.Node](t, map[string]any{
			"id": "n", "name": "Example", "type": "ss", "server": "192.0.2.2", "port": 443,
			"cipher": "aes-128-gcm", "password": "synthetic-node-secret",
		})}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.Post("/api/setup/"+id+"/source", httpxtest.JSON(map[string]any{"node_ids": []string{"n"}}), admin).WantStatus(200)
	srv.Post("/api/setup/"+id+"/prepare", httpxtest.JSON(map[string]any{"domain_rules": map[string]any{"direct": []string{}, "proxy": []string{}}, "local_dns": "https://192.0.2.53/dns-query", "controller_secret": "synthetic-controller"}), admin).WantStatus(200)
	static := t.TempDir()
	t.Setenv("AGENTS_STATIC_DIR", static)
	if err = os.WriteFile(filepath.Join(static, "configflow-agent-linux-amd64"), []byte("synthetic-binary"), 0600); err != nil {
		t.Fatal(err)
	}
	srv.Post("/api/profiles/"+profile+"/gateway-onboarding", httpxtest.JSON(map[string]any{"rule_set_ids": []string{}, "direct_rule_set_ids": []string{}, "proxy_rule_set_ids": []string{}, "local_dns": "https://192.0.2.53/dns-query"}), admin).WantStatus(200)
	revision, err := srv.Repo.ConfigRevision(ctx, profile)
	if err != nil {
		t.Fatal(err)
	}
	create := srv.Post("/api/agents/gateway-installations", httpxtest.JSON(map[string]any{"profile_id": profile, "config_revision": revision, "name": "synthetic-linux", "server_url": "https://panel.example.com", "use_as_server_domain": true, "network": host.Spec{Interface: "eth0", Address: "192.0.2.10", LANCIDR: "192.0.2.0/24", UpstreamGateway: "192.0.2.1"}}), admin).WantStatus(200)
	installationID := create.Object().GetString("id")
	path := filepath.Join(srv.DataDir, "gateway-installations", installationID+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record installation.Record
	if err = json.Unmarshal(b, &record); err != nil {
		t.Fatal(err)
	}
	preview := srv.Post("/api/setup/"+id+"/abandon", httpxtest.JSON(map[string]bool{"dry_run": true}), admin).WantStatus(200)
	if !strings.Contains(string(preview.Body), "安装") || strings.Contains(string(preview.Body), `"delete_profile":true`) {
		t.Fatal("HasInstallation owner did not protect profile", string(preview.Body))
	}
	proofs := map[string]map[string]string{}
	for _, kind := range []string{"mihomo", "oxidns"} {
		response := srv.Post("/api/agents/register", httpxtest.JSON(map[string]any{"installation_id": record.ID, "installation_token": record.Token, "name": record.Name + "-" + kind, "host": record.Network.Address, "port": record.Ports[kind], "service_type": kind, "deployment_method": "shell", "version": "1.8.60-go"})).WantStatus(200).Object()
		proofs[kind] = map[string]string{"id": response.GetString("id"), "token": response.GetString("token")}
	}
	srv.Post("/api/agents/gateway-installations/"+record.ID+"/enroll", httpxtest.JSON(map[string]any{"agents": proofs}), httpxtest.Bearer(record.Token)).WantStatus(200)
	// Seed the public deployment receipts to test C36 owner registration only;
	// real conversion, upload and polling live in installation's Agent tests.
	m := app.Agents
	err = srv.Repo.UpdateSystem(ctx, func(_ context.Context, c *model.System) error {
		for i := range c.Agents {
			kind := c.Agents[i].ServiceType
			dep := testjson.Decode[model.Deployment](t, map[string]any{"deployment_id": "synthetic-" + kind, "status": "succeeded"})
			c.Agents[i].LatestDeployment = &dep
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &record); err != nil {
		t.Fatal(err)
	}
	record.Phase = "published"
	record.Deployments = map[string]json.RawMessage{}
	for kind := range proofs {
		record.Deployments[kind] = json.RawMessage(`{"deployment_id":"synthetic-` + kind + `","status":"succeeded"}`)
	}
	b, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	device := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if r.Method != "GET" || r.URL.Path != "/api/linux-gateway/status" || len(body) != 0 || r.Header.Get("Authorization") != "Bearer "+proofs["mihomo"]["token"] {
			t.Error("invalid raw GatewayStatus request")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"supported":true,"managed":{"active":true,"pending":false,"healthy":true}}`))
	}))
	defer device.Close()
	m.SetClient(installationStatusClient{target: device.URL})
	srv.Post("/api/setup/"+id+"/check", httpxtest.JSON(map[string]any{}), admin).WantStatus(200)
	if calls != 1 {
		t.Fatal("C36 did not read through actual A20 transport", calls)
	}
	// The same latest session now becomes unusable after a manager restart,
	// despite persisted status fields. Freshness is owned by A01.
	err = srv.Repo.UpdateSystem(ctx, func(_ context.Context, c *model.System) error {
		for i := range c.Agents {
			c.Agents[i].LastHeartbeat = "2020-01-01T00:00:00+00:00"
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	agents.ResetRuntime()
	restarted, err := agents.New(ctx, srv.Repo)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	service := installation.Service{Repository: srv.Repo, Manager: restarted}
	status, err := service.Latest(ctx, profile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(status), `"status":"offline"`) {
		t.Fatal("latest trusted stored online status")
	}
}

type installationStatusClient struct{ target string }

func (c installationStatusClient) Do(ctx context.Context, r agents.Request) (*agents.Response, error) {
	at := strings.Index(r.URL, "/api/")
	if at < 0 {
		return nil, io.ErrUnexpectedEOF
	}
	r.URL = c.target + r.URL[at:]
	return (agents.HTTPClient{}).Do(ctx, r)
}
