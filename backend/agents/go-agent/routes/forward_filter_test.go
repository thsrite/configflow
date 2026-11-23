package routes

import (
	"agent/netfilter"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type forwardFilterFixture struct {
	cfg    *Config
	calls  atomic.Int32
	target netfilter.Target
	mu     sync.Mutex
	now    time.Time
}

func newForwardFilterFixture(t *testing.T, config string) *forwardFilterFixture {
	t.Helper()
	oldInspect, oldNow, oldOS, oldBudget := forwardFilterInspect, forwardFilterNow, gatewayOS, netfilter.TotalBudget
	t.Cleanup(func() {
		forwardFilterInspect, forwardFilterNow, gatewayOS, netfilter.TotalBudget = oldInspect, oldNow, oldOS, oldBudget
		forwardFilterCache.present = false
	})
	forwardFilterCache.present = false
	f := &forwardFilterFixture{cfg: &Config{ServiceType: "mihomo", ConfigPath: filepath.Join(t.TempDir(), "config.yaml")}, now: time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)}
	gatewayOS = "linux"
	forwardFilterNow = func() time.Time {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.now
	}
	forwardFilterInspect = func(_ context.Context, target netfilter.Target) netfilter.Result {
		f.calls.Add(1)
		f.mu.Lock()
		f.target = target
		f.mu.Unlock()
		return netfilter.Result{Protocol: 1, State: netfilter.StateMayDrop, Device: target.Device, Policy: "drop", Remedies: []netfilter.Remedy{{Source: "iptables", Commands: []string{"iptables -I FORWARD -o " + target.Device + " -j ACCEPT"}}}}
	}
	f.write(t, config)
	return f
}

func (f *forwardFilterFixture) write(t *testing.T, config string) {
	t.Helper()
	if err := os.WriteFile(f.cfg.ConfigPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
}

func (f *forwardFilterFixture) advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

const fullTUNConfig = "tun:\n  enable: true\n  auto-route: true\n  auto-redirect: true\n"

func TestForwardFilterApplicability(t *testing.T) {
	for _, tc := range []struct {
		name, config, service, goos, state, reason, device string
		redirect                                           bool
	}{
		{"aio full tun", fullTUNConfig, "mihomo", "linux", netfilter.StateMayDrop, "", "Meta", true},
		{"custom device", "tun:\n  enable: true\n  auto-route: true\n  device: utun9\n", "mihomo", "linux", netfilter.StateMayDrop, "", "utun9", false},
		{"no auto-route", "tun:\n  enable: true\n", "mihomo", "linux", netfilter.StateNotApplicable, "no_full_tun", "", false},
		{"tun disabled", "tun:\n  enable: false\n  auto-route: true\n", "mihomo", "linux", netfilter.StateNotApplicable, "no_full_tun", "", false},
		{"no tun", "mode: rule\n", "mihomo", "linux", netfilter.StateNotApplicable, "no_full_tun", "", false},
		{"gateway marker enabled", fullTUNConfig + "x-configflow-linux-gateway:\n  enabled: true\n  version: 1\n", "mihomo", "linux", netfilter.StateNotApplicable, "linux_gateway", "", false},
		{"gateway marker disabled", fullTUNConfig + "x-configflow-linux-gateway:\n  enabled: false\n", "mihomo", "linux", netfilter.StateNotApplicable, "linux_gateway", "", false},
		{"route-address-set", fullTUNConfig + "  route-address-set: [cf_gateway_fake_ip]\n", "mihomo", "linux", netfilter.StateNotApplicable, "route_restricted", "", false},
		{"route-address", fullTUNConfig + "  route-address: [198.18.0.0/16]\n", "mihomo", "linux", netfilter.StateNotApplicable, "route_restricted", "", false},
		{"empty route-address", fullTUNConfig + "  route-address: []\n", "mihomo", "linux", netfilter.StateMayDrop, "", "Meta", true},
		{"oxidns", fullTUNConfig, "oxidns", "linux", netfilter.StateNotApplicable, "not_mihomo", "", false},
		{"broken config", "tun: [\n", "mihomo", "linux", netfilter.StateUnknown, "config_error", "", false},
		{"not linux", fullTUNConfig, "mihomo", "darwin", netfilter.StateNotApplicable, "not_linux", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newForwardFilterFixture(t, tc.config)
			f.cfg.ServiceType = tc.service
			gatewayOS = tc.goos
			result := forwardFilterStatus(f.cfg)
			if result.State != tc.state || result.Reason != tc.reason || result.Protocol != 1 || result.Sources == nil || result.Remedies == nil {
				t.Fatalf("result: %+v", result)
			}
			inspected := tc.state == netfilter.StateMayDrop
			if got := f.calls.Load() == 1; got != inspected {
				t.Fatalf("inspect calls=%d", f.calls.Load())
			}
			if inspected && (f.target.Device != tc.device || f.target.AutoRedirect != tc.redirect) {
				t.Fatalf("target: %+v", f.target)
			}
		})
	}
}

func TestForwardFilterTargetCarriesStackAndDNSHijack(t *testing.T) {
	// ConfigFlow's standalone full TUN; the AIO publish excludes this host.
	standalone := fullTUNConfig + "  stack: mixed\n  dns-hijack: [any:53, tcp://any:53]\n"
	f := newForwardFilterFixture(t, standalone)
	forwardFilterLANAddresses = func() []net.IP { return []net.IP{net.ParseIP("10.97.0.20").To4()} }
	t.Cleanup(func() { forwardFilterLANAddresses = netfilter.LANAddresses })
	forwardFilterStatus(f.cfg)
	if want := (netfilter.Target{Device: "Meta", AutoRedirect: true, Stack: "mixed", DNSHijack: true, LAN: []net.IP{net.ParseIP("10.97.0.20").To4()}}); !reflect.DeepEqual(f.target, want) {
		t.Fatalf("standalone target: %+v", f.target)
	}
	f.write(t, standalone+"  route-exclude-address: [127.0.0.0/8, 10.97.0.20/32]\n")
	if result := forwardFilterStatus(f.cfg); result.Cached || f.target.DNSHijack || f.target.Stack != "mixed" {
		t.Fatalf("aio target: %+v %+v", f.target, result)
	}
	if result := forwardFilterStatus(f.cfg); result.Flows == nil {
		t.Fatalf("flows must be a list: %+v", result)
	}
}

func TestForwardFilterCacheExpiresAndFollowsConfigChanges(t *testing.T) {
	f := newForwardFilterFixture(t, fullTUNConfig)
	if result := forwardFilterStatus(f.cfg); result.Cached || f.calls.Load() != 1 {
		t.Fatalf("first: %+v", result)
	}
	if result := forwardFilterStatus(f.cfg); !result.Cached || f.calls.Load() != 1 || result.State != netfilter.StateMayDrop {
		t.Fatalf("cached: %+v", result)
	}
	f.advance(31 * time.Second)
	if result := forwardFilterStatus(f.cfg); result.Cached || f.calls.Load() != 2 {
		t.Fatalf("expired: %+v", result)
	}
	f.write(t, "tun:\n  enable: true\n  auto-route: true\n  device: tun-cf\n")
	if result := forwardFilterStatus(f.cfg); result.Cached || f.calls.Load() != 3 || f.target.Device != "tun-cf" {
		t.Fatalf("config change: %+v", result)
	}
	// A publish that turns the host into a selective gateway stops inspection.
	f.write(t, fullTUNConfig+"x-configflow-linux-gateway:\n  enabled: true\n")
	if result := forwardFilterStatus(f.cfg); result.State != netfilter.StateNotApplicable || f.calls.Load() != 3 {
		t.Fatalf("gateway: %+v", result)
	}
}

func TestForwardFilterWarningIsCachedOnlyBriefly(t *testing.T) {
	// The operator runs the suggested commands and refreshes right away.
	f := newForwardFilterFixture(t, fullTUNConfig)
	forwardFilterStatus(f.cfg)
	f.advance(4 * time.Second)
	if result := forwardFilterStatus(f.cfg); !result.Cached || f.calls.Load() != 1 {
		t.Fatalf("within 5s: %+v", result)
	}
	f.advance(2 * time.Second)
	forwardFilterInspect = func(_ context.Context, target netfilter.Target) netfilter.Result {
		f.calls.Add(1)
		return netfilter.Result{Protocol: 1, State: netfilter.StateOK, Device: target.Device, Policy: "accept"}
	}
	if result := forwardFilterStatus(f.cfg); result.Cached || result.State != netfilter.StateOK || f.calls.Load() != 2 {
		t.Fatalf("after the fix: %+v", result)
	}
	// A clean result keeps the longer cache.
	f.advance(20 * time.Second)
	if result := forwardFilterStatus(f.cfg); !result.Cached || f.calls.Load() != 2 {
		t.Fatalf("ok cached: %+v", result)
	}
}

func TestForwardFilterConcurrentCallsShareOneInspection(t *testing.T) {
	f := newForwardFilterFixture(t, fullTUNConfig)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			forwardFilterStatus(f.cfg)
		}()
	}
	wg.Wait()
	if f.calls.Load() != 1 {
		t.Fatalf("calls=%d", f.calls.Load())
	}
}

func TestForwardFilterIsBoundedWhenInspectionHangs(t *testing.T) {
	f := newForwardFilterFixture(t, fullTUNConfig)
	netfilter.TotalBudget = 100 * time.Millisecond
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	forwardFilterInspect = func(context.Context, netfilter.Target) netfilter.Result {
		<-release // ignores its context on purpose
		return netfilter.Result{State: netfilter.StateOK}
	}
	start := time.Now()
	result := forwardFilterStatus(f.cfg)
	if result.State != netfilter.StateUnknown || result.Reason != "timeout" || time.Since(start) > 2*time.Second || result.Sources == nil {
		t.Fatalf("hang: %+v after %s", result, time.Since(start))
	}
}

func TestLinuxGatewayStatusAddsForwardFilterOnly(t *testing.T) {
	g := newGatewayFixture(t)
	call := func() map[string]any {
		recorder := httptest.NewRecorder()
		LinuxGatewayHandler(g.cfg, "status")(recorder, httptest.NewRequest(http.MethodGet, "/api/linux-gateway/status", nil))
		var response map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || recorder.Code != 200 {
			t.Fatalf("status failed: %d %s", recorder.Code, recorder.Body.String())
		}
		return response
	}
	oldInspect := forwardFilterInspect
	t.Cleanup(func() { forwardFilterInspect = oldInspect; forwardFilterCache.present = false })
	forwardFilterCache.present = false
	calls := 0
	forwardFilterInspect = func(_ context.Context, target netfilter.Target) netfilter.Result {
		calls++
		return netfilter.Result{Protocol: 1, State: netfilter.StateMayDrop, Device: target.Device, Sources: []netfilter.Source{}, Remedies: []netfilter.Remedy{}}
	}

	// Selective gateway: not applicable, nothing inspected, other fields unchanged.
	gateway := call()
	filter, _ := gateway["forward_filter"].(map[string]any)
	if filter["state"] != "not_applicable" || filter["reason"] != "linux_gateway" || calls != 0 {
		t.Fatalf("gateway host: %v calls=%d", gateway["forward_filter"], calls)
	}
	delete(gateway, "forward_filter")
	want := []string{"detection", "enabled_in_config", "ipv4_forwarding", "managed", "success", "supported", "tun"}
	got := []string{}
	for key := range gateway {
		got = append(got, key)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("status keys changed: %v", got)
	}

	if err := os.WriteFile(g.cfg.ConfigPath, []byte(fullTUNConfig), 0600); err != nil {
		t.Fatal(err)
	}
	full := call()
	filter, _ = full["forward_filter"].(map[string]any)
	if filter["state"] != "may_drop" || filter["device"] != "Meta" || calls != 1 || full["success"] != true {
		t.Fatalf("full tun host: %v", full)
	}
}
