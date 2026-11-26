package routers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"configflow/internal/routers/device"
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/routerfake"
	"configflow/internal/routers/spec"
)

// TestServiceNilCollaboratorsFailClosed is the negative control for W9:
// a Service with nil IPv6Hosts, Rules, or ManualBaseline fails closed without
// panicking or falling back to package-level globals.
func TestServiceNilCollaboratorsFailClosed(t *testing.T) {
	svc := New(nil, Options{})
	if svc.HasIPv6Hosts() || svc.HasRules() || svc.HasManualBaseline() {
		t.Fatalf("zero Options reported wired collaborators: ipv6=%t rules=%t baseline=%t",
			svc.HasIPv6Hosts(), svc.HasRules(), svc.HasManualBaseline())
	}

	// 1. Nil IPv6Hosts fails closed with messageIPv6HostUnavailable.
	if _, err := svc.IPv6Host(t.Context(), r12Gateway()); err == nil || err.Error() != messageIPv6HostUnavailable {
		t.Fatalf("IPv6Host with nil fetcher = %v, want %q", err, messageIPv6HostUnavailable)
	}
	if host := inspectIPv6Host(t.Context(), "default", json.RawMessage(`{}`), r12Gateway(), nil); host.Error != messageIPv6HostUnavailable {
		t.Fatalf("inspectIPv6Host with nil fetcher = %+v, want error %q", host, messageIPv6HostUnavailable)
	}

	// 2. Nil Rules and nil Loader fail closed with device.ErrNotPorted when a rule set is selected.
	f := routerfake.New(nil)
	f.Reset(r08State())
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	r := &spec.Router{
		RouterSettings: spec.RouterSettings{
			ID:         "r1",
			Name:       "Synthetic",
			Type:       "routeros",
			Endpoint:   srv.URL,
			Username:   "synthetic-user",
			IPRuleSets: []string{"selected"},
		},
		Password: "synthetic-pass",
	}
	cfg := json.RawMessage(`{"rule_configs":[{"id":"selected","itemType":"ruleset","name":"R1","behavior":"ipcidr","policy":"PROXY","enabled":true}]}`)
	d := NewRouterOS(srv.URL, r.Username, r.Password, false)
	t.Cleanup(d.Close)
	if _, err := svc.Inspect(t.Context(), InspectRequest{ProfileID: "default", Config: cfg, Router: r, Driver: d}); !errors.Is(err, device.ErrNotPorted) {
		t.Fatalf("Inspect with nil Rules and nil Loader = %v, want %v", err, device.ErrNotPorted)
	}

	// 3. Nil ManualBaseline fails closed (logged warning, never panics).
	captureRelease(t.Context(), PublishRequest{ProfileID: "default"}, cfg, r, d, &spec.Desired{})
}

func TestServiceWiresCollaborators(t *testing.T) {
	calledIPv6 := false
	svc := New(nil, Options{
		IPv6Hosts: func(context.Context, *GatewayAgent) (ipv6.IPv6Host, error) {
			calledIPv6 = true
			return ipv6.IPv6Host{Interface: "eth0"}, nil
		},
		Rules: func(context.Context, json.RawMessage, spec.RuleSet, *spec.LibraryRule) (string, error) {
			return "", nil
		},
		ManualBaseline: func(context.Context, string, json.RawMessage, *spec.Router, Driver, *spec.Desired) error {
			return nil
		},
		NewRouterID: func() string { return "router_custom" },
	})
	if !svc.HasIPv6Hosts() || !svc.HasRules() || !svc.HasManualBaseline() {
		t.Fatal("wired Service did not report collaborators")
	}
	host, err := svc.IPv6Host(t.Context(), r12Gateway())
	if err != nil || !calledIPv6 || host.Interface != "eth0" {
		t.Fatalf("IPv6Host = %+v, %v (called=%t)", host, err, calledIPv6)
	}
	if ms := svc.Management(nil); ms.NewRouterID == nil || ms.NewRouterID() != "router_custom" {
		t.Fatalf("Management NewRouterID not forwarded: %+v", ms)
	}
}
