package agents_test

import (
	"net/url"
	"testing"

	"configflow/internal/agents"
	apiagents "configflow/internal/api/agents"
	"configflow/internal/httpx/httpxtest"
)

// test_oxidns_docker_install.py::test_authenticated_docker_install_api_preserves_ha_choices.
func TestAuthenticatedDockerInstallAPIPreservesHAChoices(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, DefaultFactories: true})
	m, err := agents.New(srv.Context(), srv.Repo, agents.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	apiagents.NewHandler(apiagents.Deps{Repo: srv.Repo, Agents: m}).Register(srv.App.Router)
	admin := srv.Login()
	for _, endpoint := range []string{"compose", "run"} {
		params := url.Values{"server_url": {"https://config.example"}, "agent_name": {"dns-a"}, "agent_ip": {"192.168.50.20"},
			"enable_mihomo": {"false"}, "enable_oxidns": {"true"}, "enable_dns_ha": {"true"}, "network_mode": {"host"},
			"ha_host_id_path": {"/volume1/configflow/host-id"}}
		res := srv.Get("/api/agents/docker-agent-"+endpoint+"?"+params.Encode(), admin).WantStatus(200)
		if !res.Contains("/volume1/configflow/host-id") || !res.Contains("ha-companion") {
			t.Errorf("%s: %s", endpoint, res.Text())
		}
		params.Set("network_mode", "bridge")
		res = srv.Get("/api/agents/docker-agent-"+endpoint+"?"+params.Encode(), admin).WantStatus(400)
		if res.Object().Value("success") != false {
			t.Error(res.Text())
		}
	}
}
