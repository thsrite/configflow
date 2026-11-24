package routers_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	api "configflow/internal/api/routers"
	"configflow/internal/httpx/httpxtest"
	core "configflow/internal/routers"
	"configflow/internal/routers/routerfake"
)

// Review R15 F1: Python stores str(RouterError) in last_probe. The iKuai
// redirect message names the management origin the user must enter; the
// diagnostic "download address" filter must not replace it.
func TestReviewR15ProbeRedirectKeepsManagementOrigin(t *testing.T) {
	seed := routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{{Func: "login", Status: 302, Location: "https://198.51.100.7:8443/Action/login?token=synthetic-token"}}}
	fake := routerfake.NewIkuai(seed)
	server := httptest.NewServer(fake)
	defer server.Close()
	cfg := inputConfig(server.URL)
	r := cfg["routers"].(map[string]any)["items"].([]any)[0].(map[string]any)
	r["type"] = "ikuai"
	r["username"] = routerfake.IkuaiUser
	r["password"] = routerfake.IkuaiPassword
	const want = "iKuai 要求跳转到 https://198.51.100.7:8443（HTTP 302）。请核对浏览器打开的管理地址，在「编辑路由器」中修改地址后重试。"
	s := httpxtest.New(t, httpxtest.Options{})
	saveFixtureConfig(t, s, cfg)
	api.New(api.Deps{Repo: s.Repo, Routers: core.New(s.Repo, core.Options{})}).Register(s.App.Router)
	result := s.Post("/api/routers/router-one/probe")
	result.WantStatus(502)
	got, _ := decodeJSON(t, result)["message"].(string)
	if got != want || strings.Contains(got, "synthetic-token") {
		t.Fatalf("message=%q want %q", got, want)
	}
	profile, err := s.Repo.Profile(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	stored := profile.Routers.Items[0].LastProbe
	if stored == nil || stored.Message == nil || *stored.Message != want {
		t.Fatalf("stored message=%+v", stored)
	}
}
