package routers

import (
	"configflow/internal/routers/device"
	"encoding/json"
	"net/netip"
	"slices"
	"testing"

	"configflow/internal/routerdoc"
)

// Review R01: helpers the corpus does not drive (expected values follow
// backend/routers/base.py and lan_clients.py line by line).
func TestR01ReviewModelHelpers(t *testing.T) {
	for in, want := range map[string]string{"": "cf", " tg ": "cf-tg", "CF-x": "CF-x", "cf:y": "cf:y", "cfx": "cf-cfx", "cf": "cf"} {
		if got := device.ManagedComment(in); got != want {
			t.Errorf("device.ManagedComment(%q) = %q, want %q", in, got, want)
		}
	}
	zero, one := int64(0), int64(1)
	ok := &PolicyRouteEntry{Enabled: true, Gateway: "10.0.0.2", Src: []string{"10.0.0.2"}, SrcInvert: true, Protocol: "any", Priority: &zero}
	bad := *ok
	bad.Priority = &one
	if !device.PolicyRouteOK(ok, "10.0.0.2") || device.PolicyRouteOK(&bad, "10.0.0.2") || device.PolicyRouteOK(ok, "") {
		t.Error("PolicyRouteOK")
	}
	state := &RouterState{PolicyRoutes: []*PolicyRouteEntry{{Comment: "x"}, {Comment: PolicyAutoComment}, {Comment: PolicyManualComment}}}
	if device.PolicyRoute(state.PolicyRoutes, PolicyManualComment) != state.PolicyRoutes[2] {
		t.Error("PolicyRoute")
	}
	RetirePolicyRoutes(state)
	if state.PolicyRoutes != nil || len(state.RetiredPolicyRoutes) != 2 {
		t.Errorf("RetirePolicyRoutes: %+v", state)
	}
	state = &RouterState{PolicyRoutes: []*PolicyRouteEntry{{Comment: "x"}}}
	if RetirePolicyRoutes(state); state.RetiredPolicyRoutes == nil || len(state.RetiredPolicyRoutes) != 0 {
		t.Error("RetirePolicyRoutes must leave an empty, non-nil list")
	}
	gw := "10.0.0.2"
	state = &RouterState{
		Routes: []*RouteEntry{device.NewRoute("1", "91.108.4.0/22", gw, true, ""), device.NewRoute("2", "1.1.1.0/24", gw, false, "")},
		ActiveRoutes: []*RouteEntry{device.NewRoute("a", "91.108.4.0/22", gw, true, ""), device.NewRoute("b", "1.1.1.0/24", gw, true, ""),
			device.NewRoute("c", "8.8.8.0/24", "10.0.0.9", true, "")},
	}
	stale := StaleRoutes(state, gw)
	if len(stale) != 1 || stale[0].ID != "b" || len(StaleRoutes(state, "")) != 0 {
		t.Errorf("StaleRoutes = %v", stale)
	}
	want := "路由器当前路由表中有 1 条指向 10.0.0.2 的路由不在静态路由配置里（1.1.1.0/24）：这是修改或删除静态路由后残留的内核路由，" +
		"优先于分流规则，会让 10.0.0.2 自身的流量回环。界面和接口都无法删除，需要重启路由器清除。"
	if got := StaleRouteWarning(stale, gw); got != want {
		t.Errorf("StaleRouteWarning = %q", got)
	}
}

func TestR01ReviewSightings(t *testing.T) {
	weak, strong := routerdoc.NewSighting("192.168.0.5"), routerdoc.NewSighting("192.168.0.5")
	weak.MAC, weak.Presence = "AA:BB:CC:DD:EE:FF", "recent"
	weak.AddSource("online")
	weak.AddSource("arp")
	strong.Presence = "online"
	strong.AddSource("lease")
	merged := routerdoc.Better(weak, strong)
	if merged != strong || merged.MAC != weak.MAC || !slices.Equal(merged.Sources, []string{"arp", "lease", "online"}) {
		t.Errorf("Better = %+v", merged)
	}
	data, _ := json.Marshal(merged)
	if string(data) != `{"mac":"AA:BB:CC:DD:EE:FF","hostname":"","sources":["arp","lease","online"],"presence":"online","basis":"","last_seen":null,"router_id":""}` {
		t.Errorf("public view %s", data)
	}
	reading := routerdoc.NewReading()
	reading.LAN = append(reading.LAN, netip.MustParsePrefix("192.168.0.0/24"))
	if !reading.Covers("192.168.0.9") || reading.Covers("10.0.0.1") || reading.Covers("bad") {
		t.Error("Covers")
	}
}

// Accepted difference (documented in PROGRESS): Go rejects management
// addresses urllib's urlparse lets through but no HTTP client can connect to
// (non-numeric port, spaces, bad escapes); everything usable stays accepted.
func TestR01ReviewEndpoints(t *testing.T) {
	for _, e := range []string{"http://192.168.88.1", "https://router.lan:8443/", "http://[fd00::1]:80", "HTTP://H", "http://h/;x",
		"http://user:pw@h", "http://h?q", "http://h#f", "https://路由器.lan"} {
		if !validEndpoint(e) {
			t.Errorf("%q rejected", e)
		}
	}
	for _, e := range []string{"http://a:abc", "http://a b", "http://h:-1", "http://[::1", "ftp://h", "http://h/a", "http:///x"} {
		if validEndpoint(e) {
			t.Errorf("%q accepted", e)
		}
	}
}
