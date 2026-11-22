package routes

import (
	"agent/preflight"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type ipv6Fixture struct {
	cfg        *Config
	links      string
	routes     string
	v4         string
	writes     int
	failKey    string
	failCommit bool
}

func newIPv6Fixture(t *testing.T) *ipv6Fixture {
	t.Helper()
	oldOS, oldUID := gatewayOS, gatewayGeteuid
	oldRoot, oldDirectory := ipv6SysctlRoot, ipv6SysctlDirectory
	oldRead, oldWrite, oldRun, oldAssignments, oldSave := ipv6ReadFile, ipv6WriteValue, ipv6Run, ipv6Assignments, ipv6Save
	oldLookup, oldDial := ipv6Lookup, ipv6Dial
	t.Cleanup(func() {
		gatewayOS, gatewayGeteuid = oldOS, oldUID
		ipv6SysctlRoot, ipv6SysctlDirectory = oldRoot, oldDirectory
		ipv6ReadFile, ipv6WriteValue, ipv6Run, ipv6Assignments, ipv6Save = oldRead, oldWrite, oldRun, oldAssignments, oldSave
		ipv6Lookup, ipv6Dial = oldLookup, oldDial
	})
	root := t.TempDir()
	gatewayOS = "linux"
	gatewayGeteuid = func() int { return 0 }
	ipv6SysctlRoot = filepath.Join(root, "proc")
	ipv6SysctlDirectory = filepath.Join(root, "sysctl.d")
	if err := os.MkdirAll(ipv6SysctlDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	f := &ipv6Fixture{cfg: &Config{AgentConfigFile: filepath.Join(root, "config-mihomo.json"), ConfigPath: filepath.Join(root, "mihomo.yaml"), ServiceType: "mihomo", ServiceManager: "systemd", AgentID: "ipv6-test"}, links: `[{"ifindex":3,"ifname":"eth1","link_type":"ether","address":"00:11:22:33:44:55","addr_info":[{"family":"inet","local":"192.0.2.2","prefixlen":24,"scope":"global"},{"family":"inet6","local":"2001:db8:150:1::2","prefixlen":64,"scope":"global"},{"family":"inet6","local":"fe80::2","prefixlen":64,"scope":"link"}]},{"ifindex":7,"ifname":"Meta","link_type":"none","addr_info":[{"local":"fdfe::1","prefixlen":126}]}]`, routes: `[{"dst":"default","gateway":"fe80::1","dev":"eth1","protocol":"ra","metric":1024,"expires":1750}]`, v4: `[{"dst":"default","gateway":"192.0.2.1","dev":"eth1","protocol":"static"}]`}
	if err := os.WriteFile(f.cfg.ConfigPath, []byte("ipv6: true\ndns:\n  enable: true\n  enhanced-mode: fake-ip\n  ipv6: true\n  fake-ip-range6: fc00::1/64\n  nameserver: ['https://secret.invalid/?token=hidden']\nproxies: [{name: secret, password: hidden}]\ntun: {enable: true, auto-route: true}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, iface := range []string{"all", "default", "eth0", "eth1", "Meta"} {
		for _, field := range []string{"forwarding", "accept_ra", "accept_ra_defrtr", "autoconf", "disable_ipv6"} {
			value := "1"
			if field == "forwarding" || field == "disable_ipv6" {
				value = "0"
			}
			path := filepath.Join(ipv6SysctlRoot, ipv6Key(iface, field))
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(value+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	ipv6ReadFile = os.ReadFile
	ipv6WriteValue = func(path, value string) error {
		f.writes++
		if f.failKey != "" && strings.HasSuffix(path, f.failKey) {
			f.failKey = ""
			return fmt.Errorf("injected sysctl failure")
		}
		if err := os.WriteFile(path, []byte(value+"\n"), 0644); err != nil {
			return err
		}
		// Emulate the kernel's all.forwarding side effects so rollback tests must
		// restore every interface, including one unrelated to the selected LAN.
		if path == filepath.Join(ipv6SysctlRoot, ipv6Key("all", "forwarding")) {
			for _, iface := range []string{"default", "eth0", "eth1", "Meta"} {
				if err := os.WriteFile(filepath.Join(ipv6SysctlRoot, ipv6Key(iface, "forwarding")), []byte(value+"\n"), 0644); err != nil {
					return err
				}
			}
		}
		return nil
	}
	ipv6Run = func(name string, args ...string) (string, error) {
		switch name + " " + strings.Join(args, " ") {
		case "ip -j addr show":
			return f.links, nil
		case "ip -j -6 route show table main default":
			return f.routes, nil
		case "ip -j -4 route show table main default":
			return f.v4, nil
		default:
			return "", fmt.Errorf("unexpected command %s %v", name, args)
		}
	}
	ipv6Assignments = func(pending map[string][]byte) ([]preflight.SysctlAssignment, bool) {
		files := map[string][]byte{}
		paths, _ := filepath.Glob(filepath.Join(ipv6SysctlDirectory, "*.conf"))
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, true
			}
			files[path] = data
		}
		for path, data := range pending {
			files[path] = data
		}
		paths = nil
		for path := range files {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		out := []preflight.SysctlAssignment{}
		for _, path := range paths {
			for n, line := range strings.Split(string(files[path]), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				parts := strings.SplitN(line, "=", 2)
				if len(parts) != 2 {
					continue
				}
				key := strings.TrimSpace(parts[0])
				if !strings.Contains(key, "/") {
					key = strings.ReplaceAll(key, ".", "/")
				}
				out = append(out, preflight.SysctlAssignment{Key: key, Value: strings.TrimSpace(parts[1]), File: path, Line: n + 1, Persistent: true})
			}
		}
		return out, false
	}
	ipv6Save = func(path string, data []byte, mode os.FileMode) error {
		if f.failCommit && strings.HasSuffix(path, "ipv6-readiness.json") && strings.Contains(string(data), `"phase": "active"`) {
			return fmt.Errorf("injected commit failure")
		}
		return durableWrite(path, data, mode)
	}
	return f
}
func (f *ipv6Fixture) set(t *testing.T, iface, field, value string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(ipv6SysctlRoot, ipv6Key(iface, field)), []byte(value+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
}
func (f *ipv6Fixture) persist(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(ipv6SysctlDirectory, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
func (f *ipv6Fixture) ready(t *testing.T) {
	f.set(t, "all", "forwarding", "1")
	f.set(t, "eth1", "forwarding", "1")
	f.set(t, "eth1", "accept_ra", "2")
	f.persist(t, "50-user.conf", "net.ipv6.conf.all.forwarding=1\nnet.ipv6.conf.eth1.accept_ra=2\nnet.ipv6.conf.eth1.accept_ra_defrtr=1\n")
}

func TestIPv6ReadOnlyReportsRAAndUserPersistence(t *testing.T) {
	f := newIPv6Fixture(t)
	f.ready(t)
	report := inspectIPv6(f.cfg, "")
	if !report.ConfigurationReady || !report.CanPrepare || report.Interface != "eth1" || report.InterfaceIndex != 3 || report.Persistence.State != "system" || report.DefaultRoutes[0].Protocol != "ra" || report.DefaultRoutes[0].Expires != float64(1750) {
		t.Fatalf("wrong report: %+v", report)
	}
	if report.Connectivity.Status != "not_tested" || report.RebootVerified || f.writes != 0 {
		t.Fatal("inspection wrote settings or overclaimed verification")
	}
	data, _ := json.Marshal(report)
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "hidden") {
		t.Fatal("report exposed unrelated config")
	}
	if len(report.Addresses) != 2 || len(report.Interfaces) != 1 {
		t.Fatalf("TUN or IPv4 included: %+v", report)
	}
	path, _ := ipv6StatePath(f.cfg)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("inspection created state")
	}
	changed, err := prepareIPv6(f.cfg, "eth1", 3)
	if err != nil || changed {
		t.Fatalf("already prepared host should be untouched: %v %v", changed, err)
	}
	if _, err := os.Stat(ipv6ManagedPath()); !os.IsNotExist(err) {
		t.Fatal("took ownership of user's persistence")
	}
}
func TestIPv6GlobalRADoesNotReplaceInterfaceRA(t *testing.T) {
	f := newIPv6Fixture(t)
	f.ready(t)
	f.set(t, "all", "accept_ra", "2")
	f.set(t, "eth1", "accept_ra", "1")
	r := inspectIPv6(f.cfg, "")
	if r.ConfigurationReady || !r.CanPrepare {
		t.Fatalf("global RA hid per-interface failure: %+v", r)
	}
	changed, err := prepareIPv6(f.cfg, "eth1", 3)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	value, _ := ipv6ReadValue(ipv6Key("eth1", "accept_ra"))
	if value != "2" {
		t.Fatal("actual interface not repaired")
	}
	if _, err := os.Stat(ipv6ManagedPath()); !os.IsNotExist(err) {
		t.Fatal("duplicated complete user-owned persistence")
	}
}
func TestIPv6StaticRoutePreservesRA(t *testing.T) {
	f := newIPv6Fixture(t)
	f.routes = strings.ReplaceAll(f.routes, `"ra"`, `"static"`)
	f.set(t, "eth1", "accept_ra", "0")
	changed, err := prepareIPv6(f.cfg, "eth1", 3)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	state, err := loadIPv6Managed(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := state.Values[ipv6Key("eth1", "accept_ra")]; present {
		t.Fatal("static configuration was converted to RA")
	}
	value, _ := ipv6ReadValue(ipv6Key("eth1", "accept_ra"))
	if value != "0" {
		t.Fatal("static route RA value was changed")
	}
}
func TestIPv6MissingDefaultCanPrepareButIsNotReady(t *testing.T) {
	f := newIPv6Fixture(t)
	f.routes = "[]"
	report := inspectIPv6(f.cfg, "")
	if !report.CanPrepare || !report.RARequired || report.ConfigurationReady {
		t.Fatalf("missing route status incorrect: %+v", report)
	}
	if _, err := prepareIPv6(f.cfg, "eth1", 3); err != nil {
		t.Fatal(err)
	}
	report = inspectIPv6(f.cfg, "")
	if report.ConfigurationReady || report.Connectivity.Status != "not_tested" {
		t.Fatal("prepare fabricated a route or connectivity")
	}
}
func TestIPv6RejectsStaleOrAmbiguousInterface(t *testing.T) {
	for _, kind := range []string{"stale-index", "other-interface", "multipath", "two-defaults", "disabled"} {
		t.Run(kind, func(t *testing.T) {
			f := newIPv6Fixture(t)
			iface, index := "eth1", 3
			switch kind {
			case "stale-index":
				index = 4
			case "other-interface":
				iface = "eth0"
			case "multipath":
				f.routes = `[{"multipath":[{"dev":"eth1"}]}]`
			case "two-defaults":
				f.routes = `[{"dev":"eth1"},{"dev":"eth0"}]`
			case "disabled":
				f.set(t, "eth1", "disable_ipv6", "1")
			}
			if _, err := prepareIPv6(f.cfg, iface, index); err == nil {
				t.Fatal("unsafe preparation accepted")
			}
			if f.writes != 0 {
				t.Fatal("rejected preparation wrote sysctl")
			}
		})
	}
}
func TestIPv6PrepareIdempotentPersistsOnlyMissingValues(t *testing.T) {
	f := newIPv6Fixture(t)
	f.persist(t, "50-user.conf", "net.ipv6.conf.all.forwarding=1\n")
	userBefore, _ := os.ReadFile(filepath.Join(ipv6SysctlDirectory, "50-user.conf"))
	changed, err := prepareIPv6(f.cfg, "eth1", 3)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	content, err := os.ReadFile(ipv6ManagedPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "forwarding=") || !strings.Contains(string(content), "eth1/accept_ra=2") {
		t.Fatalf("duplicated existing values: %s", content)
	}
	state, err := loadIPv6Managed(f.cfg)
	if err != nil || state.Phase != "active" {
		t.Fatal(state, err)
	}
	writes := f.writes
	path, _ := ipv6StatePath(f.cfg)
	before, _ := os.ReadFile(path)
	changed, err = prepareIPv6(f.cfg, "eth1", 3)
	after, _ := os.ReadFile(path)
	if err != nil || changed || f.writes != writes || string(before) != string(after) {
		t.Fatalf("repeat application was not idempotent: %v %v %d -> %d", changed, err, writes, f.writes)
	}
	userAfter, _ := os.ReadFile(filepath.Join(ipv6SysctlDirectory, "50-user.conf"))
	if string(userBefore) != string(userAfter) {
		t.Fatal("user file changed")
	}
}
func TestIPv6PrepareRollsBackEveryForwardingSideEffect(t *testing.T) {
	f := newIPv6Fixture(t)
	f.failCommit = true
	before, err := ipv6RuntimeSnapshot(ipv6Desired("eth1", true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareIPv6(f.cfg, "eth1", 3); err == nil {
		t.Fatal("expected commit failure")
	}
	after, _ := ipv6RuntimeSnapshot(ipv6Desired("eth1", true))
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("runtime not restored: before=%v after=%v", before, after)
	}
	path, _ := ipv6StatePath(f.cfg)
	for _, path := range []string{path, ipv6ManagedPath()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("failed transaction left file: %s", path)
		}
	}
}
func TestIPv6PrepareRuntimeFailurePreservesUserFiles(t *testing.T) {
	f := newIPv6Fixture(t)
	f.failKey = ipv6Key("all", "forwarding")
	before, _ := ipv6RuntimeSnapshot(ipv6Desired("eth1", true))
	if _, err := prepareIPv6(f.cfg, "eth1", 3); err == nil {
		t.Fatal("expected sysctl failure")
	}
	after, _ := ipv6RuntimeSnapshot(ipv6Desired("eth1", true))
	if !reflect.DeepEqual(before, after) {
		t.Fatal("RA changes before forwarding failure not restored")
	}
}
func TestIPv6PrepareRejectsForeignOrOverridingFiles(t *testing.T) {
	for _, kind := range []string{"foreign", "modified", "late-override", "wildcard", "symlink", "wrong-mac"} {
		t.Run(kind, func(t *testing.T) {
			f := newIPv6Fixture(t)
			switch kind {
			case "foreign":
				f.persist(t, "99-configflow-ipv6.conf", "# user owns this\n")
			case "modified", "wrong-mac":
				if _, err := prepareIPv6(f.cfg, "eth1", 3); err != nil {
					t.Fatal(err)
				}
				if kind == "modified" {
					f.persist(t, "99-configflow-ipv6.conf", "# changed\n")
				} else {
					f.links = strings.ReplaceAll(f.links, "00:11:22:33:44:55", "00:11:22:33:44:66")
				}
			case "late-override":
				f.persist(t, "zz-override.conf", "net.ipv6.conf.eth1.accept_ra=0\n")
			case "wildcard":
				f.persist(t, "10-wildcard.conf", "net.ipv6.conf.*.accept_ra=0\n")
			case "symlink":
				if err := os.Symlink(filepath.Join(ipv6SysctlDirectory, "missing"), ipv6ManagedPath()); err != nil {
					t.Fatal(err)
				}
			}
			before := f.writes
			if _, err := prepareIPv6(f.cfg, "eth1", 3); err == nil {
				t.Fatal("unsafe takeover accepted")
			}
			if f.writes != before {
				t.Fatal("ownership conflict changed runtime")
			}
		})
	}
}
func TestIPv6AddressValidityFlags(t *testing.T) {
	f := newIPv6Fixture(t)
	f.ready(t)
	f.links = strings.ReplaceAll(f.links, `"local":"2001:db8:150:1::2"`, `"local":"2001:db8:150:1::2","flags":["tentative","temporary"]`)
	f.links = strings.ReplaceAll(f.links, `"local":"fe80::2"`, `"local":"fe80::2","dadfailed":true`)
	r := inspectIPv6(f.cfg, "")
	if r.ConfigurationReady || r.Addresses[0].Usable || !r.Addresses[0].Tentative || !r.Addresses[0].Temporary || !r.Addresses[1].DadFailed {
		t.Fatalf("invalid address counted ready: %+v", r.Addresses)
	}
}
func TestIPv6HTTPValidationAndTransportProbe(t *testing.T) {
	f := newIPv6Fixture(t)
	invoke := func(action, method, body string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		IPv6ReadinessHandler(f.cfg, action)(r, httptest.NewRequest(method, "/api/ipv6-readiness/"+action, strings.NewReader(body)))
		return r
	}
	if r := invoke("status", http.MethodPost, ""); r.Code != 405 {
		t.Fatal(r.Code)
	}
	for _, body := range []string{`{"interface":"../all","interface_index":3}`, `{"interface":"eth1","interface_index":3,"extra":true}`, `{"interface":"eth1","interface_index":3} {}`} {
		if r := invoke("prepare", http.MethodPost, body); r.Code != 400 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	for _, body := range []string{`{"host":"::1","port":80}`, `{"host":"1.2.3.4","port":80}`, `{"host":"x;touch /tmp/x","port":80}`, `{"host":"example.com","port":0}`} {
		if r := invoke("probe", http.MethodPost, body); r.Code != 400 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	ipv6Lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("192.0.2.1")}, {IP: net.ParseIP("2001:db8::2")}}, nil
	}
	calls := 0
	ipv6Dial = func(ctx context.Context, iface, address string) (net.Conn, error) {
		calls++
		if iface != "eth1" {
			t.Fatalf("probe bypassed uplink: %s", iface)
		}
		if address != "[2001:db8::2]:443" {
			t.Fatalf("unexpected dial %s", address)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded connection")
		}
		a, b := net.Pipe()
		b.Close()
		return a, nil
	}
	r := invoke("probe", http.MethodPost, `{"host":"node.example.com","port":443}`)
	if r.Code != 200 || calls != 1 || !strings.Contains(r.Body.String(), `"status":"ok"`) || !strings.Contains(r.Body.String(), `"scope":"node_transport"`) {
		t.Fatal(r.Code, r.Body.String(), calls)
	}
	if f.writes != 0 {
		t.Fatal("connectivity probe changed networking")
	}
}

func TestIPv6ProbeRejectsFakeAndTUNAddresses(t *testing.T) {
	for _, kind := range []string{"fake-domain", "fake-literal", "tun-literal", "closed-port", "bind-denied"} {
		t.Run(kind, func(t *testing.T) {
			f := newIPv6Fixture(t)
			ipv6Lookup = func(context.Context, string) ([]net.IPAddr, error) {
				return []net.IPAddr{{IP: net.ParseIP("fc00::5")}}, nil
			}
			calls := 0
			ipv6Dial = func(ctx context.Context, iface, address string) (net.Conn, error) {
				calls++
				if iface != "eth1" {
					t.Fatal("probe not bound to uplink")
				}
				return nil, fmt.Errorf("connection refused or cannot bind")
			}
			body := `{"host":"node.example.com","port":1080}`
			switch kind {
			case "fake-literal":
				body = `{"host":"fc00::5","port":1080}`
			case "tun-literal":
				body = `{"host":"fdfe::2","port":1080}`
			case "closed-port", "bind-denied":
				body = `{"host":"2001:db8::2","port":1081}`
			}
			recorder := httptest.NewRecorder()
			IPv6ReadinessHandler(f.cfg, "probe")(recorder, httptest.NewRequest(http.MethodPost, "/api/ipv6-readiness/probe", strings.NewReader(body)))
			if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `"status":"error"`) {
				t.Fatalf("false positive: %d %s", recorder.Code, recorder.Body.String())
			}
			if strings.HasPrefix(kind, "fake") || kind == "tun-literal" {
				if calls != 0 || !strings.Contains(recorder.Body.String(), "Fake-IP") {
					t.Fatal("attempted connection to synthetic target", calls, recorder.Body.String())
				}
			} else if calls != 1 {
				t.Fatal("probe silently retried without binding", calls)
			}
		})
	}
}

func TestIPv6PrepareUnsupportedPlatformDoesNotCreateOperationFiles(t *testing.T) {
	for _, scenario := range []string{"container", "non-root", "non-linux", "wrong-service"} {
		t.Run(scenario, func(t *testing.T) {
			f := newIPv6Fixture(t)
			switch scenario {
			case "container":
				f.cfg.ServiceManager = "s6"
			case "non-root":
				gatewayGeteuid = func() int { return 1000 }
			case "non-linux":
				gatewayOS = "darwin"
			case "wrong-service":
				f.cfg.ServiceType = "oxidns"
			}
			recorder := httptest.NewRecorder()
			IPv6ReadinessHandler(f.cfg, "prepare")(recorder, httptest.NewRequest(http.MethodPost, "/api/ipv6-readiness/prepare", strings.NewReader(`{"interface":"eth1","interface_index":3}`)))
			if recorder.Code != 409 {
				t.Fatalf("unsupported preparation accepted: %d %s", recorder.Code, recorder.Body.String())
			}
			if f.writes != 0 {
				t.Fatal("unsupported platform changed sysctl")
			}
			if _, err := os.Stat(deploymentRoot(f.cfg)); !os.IsNotExist(err) {
				t.Fatal("unsupported request created deployment operation files", err)
			}
			statePath, _ := ipv6StatePath(f.cfg)
			if _, err := os.Stat(statePath); !os.IsNotExist(err) {
				t.Fatal("unsupported request created preparation state", err)
			}
		})
	}
}
