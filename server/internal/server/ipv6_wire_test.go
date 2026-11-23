package server

import (
	"configflow/internal/agents"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/routers"
	"context"
	"net/http/httptest"
	"slices"
	"testing"

	"configflow/internal/httpx/httpxtest"
)

func TestIPv6ServerRegistrationAndHEAD(t *testing.T) {
	app := NewApp()
	if !app.Router.HasEndpoint("agents.ipv6_readiness_operation") {
		t.Fatal("A23 missing")
	}
	fixture := httpxtest.New(t, httpxtest.Options{})
	for _, path := range []string{"/api/agents/source/ipv6-readiness", "/api/agents/source/ipv6-readiness/verify", "/api/agents/source/ipv6-readiness/verification-settings"} {
		before := fixture.Repo.CommitCount()
		request := httptest.NewRequest("HEAD", path, nil).WithContext(fixture.Context())
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, request)
		if rec.Code != 404 {
			t.Fatalf("HEAD %s: %d", path, rec.Code)
		}
		if before != fixture.Repo.CommitCount() {
			t.Fatal("HEAD wrote")
		}
	}
}

type ipv6HostClient struct{}

func (ipv6HostClient) Do(_ context.Context, r agents.Request) (*agents.Response, error) {
	return &agents.Response{StatusCode: 200, Body: []byte(`{"success":true,"interface":"eth0","addresses":[{"address":"2001:db8::2","usable":true}]}`)}, nil
}
func TestIPv6RouterHostOwnerRegistration(t *testing.T) {
	NewApp()
	fixture := httpxtest.New(t, httpxtest.Options{})
	m, e := agents.Get(fixture.Context())
	if e != nil {
		t.Fatal(e)
	}
	m.SetClient(ipv6HostClient{})
	host, e := routers.IPv6HostRequest(fixture.Context(), &routers.GatewayAgent{Agent: &model.Agent{ID: "source", Host: "192.0.2.2", Port: model.Int(8080), Token: "synthetic-source-token", ServiceType: "mihomo", ProfileID: "default"}})
	if e != nil || host.Interface != "eth0" || len(host.Addresses) != 1 {
		t.Fatal("R13 IPv6 host still unavailable", host, e)
	}
	if _, e = agents.Init(fixture.Context()); e != nil {
		t.Fatal(e)
	}
	names := lifecycle.Names()
	if !slices.Contains(names, "ipv6-verification-scheduler") {
		t.Fatal("A23 scheduler not registered", names)
	}
}
