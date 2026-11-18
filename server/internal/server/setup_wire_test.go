package server

import (
	"testing"

	apisetup "configflow/internal/api/setup"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/setup"
	"configflow/internal/yamlx"
)

func TestSetupServerOwnerRegistrationRemovesUnavailable(t *testing.T) {
	t.Cleanup(setup.SetExternalOwners(setup.ExternalOwners{}))
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(apisetup.Register), Auth: true, Setup: func(*httpx.App) { NewApp() }})
	admin := srv.Login()
	id := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	srv.Post("/api/setup", httpxtest.JSON(map[string]string{"id": id, "goal": "router", "name": "Synthetic"}), admin).WantStatus(200)
	path := "/api/setup/" + id + "/prepare"
	srv.Post(path, httpxtest.JSON(map[string]any{}), admin).WantStatus(503)
	called := false
	t.Cleanup(setup.RegisterExternalOwners(setup.ExternalOwners{DNS: &setup.DNSOwner{
		DefaultLocalDNS: "https://dns.example.com/dns-query", DNSTarget: "127.0.0.1:1053", Nameservers: func() []string { return []string{} },
		LocalUpstreams: func(string) (*yamlx.Node, error) {
			called = true
			return nil, &setup.Error{Message: "synthetic owner validation", Status: 400}
		},
		PrepareMihomoDNS: func(doc *yamlx.Node) (*yamlx.Node, error) { return doc, nil },
	}}))
	srv.Post(path, httpxtest.JSON(map[string]any{}), admin).WantStatus(400)
	if !called {
		t.Fatal("registered owner was not dispatched")
	}
}
