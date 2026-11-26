package agents_test

import (
	"net/url"
	"testing"

	apiagents "configflow/internal/api/agents"
	"configflow/internal/httpx/httpxtest"
)

// test_shell_ha_prepare.py::test_unsupported_ha_requests_are_rejected.
func TestUnsupportedHARequestsAreRejected(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{})
	apiagents.NewHandler(apiagents.Deps{Repo: srv.Repo}).Register(srv.App.Router)
	for _, extra := range []url.Values{
		{"type": {"mihomo"}}, {"type": {"mosdns"}}, {"type": {"aio"}}, {"agent_type": {"shell"}},
		{"prepare_dns_ha": {"1"}}, {"prepare_dns_ha": {"yes"}}, {"prepare_dns_ha": {""}},
		{"service_manager": {"command"}, "stop_command": {"/bin/true"}, "start_command": {"/bin/true"}, "status_command": {"/bin/true"}},
	} {
		query := url.Values{"type": {"oxidns"}, "prepare_dns_ha": {"true"}}
		for k, v := range extra {
			query[k] = v
		}
		srv.Get("/api/agents/install-script?" + query.Encode()).WantStatus(400)
	}
}
