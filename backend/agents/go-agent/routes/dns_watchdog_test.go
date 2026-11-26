package routes

import (
	"agent/internal/logx"
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// udpResponder answers each query with reply(query) (nil = stay silent).
func udpResponder(t *testing.T, reply func(query []byte) []byte) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			if out := reply(append([]byte(nil), buf[:n]...)); out != nil {
				conn.WriteTo(out, addr)
			}
		}
	}()
	return conn.LocalAddr().String()
}

func nxdomain(query []byte) []byte {
	out := append([]byte(nil), query...)
	out[2] |= 0x80 // QR
	out[3] = 0x83  // RA + RCODE NXDOMAIN
	return out
}

func TestDNSAnswersAcceptsAnyReplyWithMatchingID(t *testing.T) {
	if err := dnsAnswers(udpResponder(t, nxdomain)); err != nil {
		t.Fatalf("NXDOMAIN must count as answering: %v", err)
	}
	queries := make(chan []byte, 1)
	addr := udpResponder(t, func(q []byte) []byte {
		select {
		case queries <- q:
		default:
		}
		return nil
	})
	if err := dnsAnswers(addr); err == nil {
		t.Fatal("silent server must fail")
	}
	var seen []byte
	select {
	case seen = <-queries:
	default:
		t.Fatal("silent server received no query")
	}
	if len(seen) < 12 || string(seen[12:]) != "\x13configflow-watchdog\x07invalid\x00\x00\x01\x00\x01" {
		t.Fatalf("unexpected query encoding: %q", seen)
	}
	wrongID := udpResponder(t, func(q []byte) []byte { out := nxdomain(q); out[0] ^= 0xff; return out })
	if err := dnsAnswers(wrongID); err == nil {
		t.Fatal("reply with another ID must not count")
	}
}

func TestWatchdogDNSAddress(t *testing.T) {
	base := guardedWatchdogConfig("0.0.0.0:53")
	for _, service := range []string{"oxidns", "mosdns"} {
		for _, test := range []struct {
			name, config, health, want string
		}{
			{"first-udp", base, "", "127.0.0.1:53"},
			{"matching-override", base, "127.0.0.1:53", "127.0.0.1:53"},
			{"wildcard-override", base, "0.0.0.0:53", "127.0.0.1:53"},
			{"ipv6", guardedWatchdogConfig("[::]:53"), "[::1]:53", "[::1]:53"},
			{"empty-includes", "include: []\n" + base, "", "127.0.0.1:53"},
			{"second-listener", base + "  - {tag: another, type: udp_server, args: {listen: '127.0.0.1:1053', entry: main}}\n", "127.0.0.1:1053", "127.0.0.1:1053"},
			{"different-override-port", base, "127.0.0.1:5353", ""},
			{"remote-override", base, "192.0.2.123:53", ""},
			{"hostname-override", base, "localhost:53", ""},
			{"remote-listener", guardedWatchdogConfig("192.0.2.123:53"), "", ""},
			{"hostname-listener", guardedWatchdogConfig("localhost:53"), "", ""},
			{"zero-port", guardedWatchdogConfig("127.0.0.1:0"), "", ""},
			{"missing-guard", strings.Replace(base, "qname full:configflow-watchdog.invalid", "qname full:other.invalid", 1), "", ""},
			{"suffix-match", strings.Replace(base, "qname full:", "qname domain:", 1), "", ""},
			{"wrong-response", strings.Replace(base, "reject 3", "reject 2", 1), "", ""},
			{"extra-condition", strings.Replace(base, "invalid']", "invalid', 'qtype 1']", 1), "", ""},
			{"extra-rule-field", strings.Replace(base, "exec: 'reject 3'", "exec: 'reject 3', custom: true", 1), "", ""},
			{"guard-not-first", strings.Replace(base, "    args:\n", "    args:\n      - {exec: '$upstream'}\n", 1), "", ""},
			{"unknown-entry", strings.ReplaceAll(base, "entry: main", "entry: other"), "", ""},
			{"non-sequence-entry", strings.Replace(base, "type: sequence", "type: forward", 1), "", ""},
			{"duplicate-tags", base + "  - {tag: main, type: sequence, args: []}\n", "", ""},
			{"duplicate-listener", base + "  - {tag: another, type: udp_server, args: {listen: '127.0.0.1:53', entry: main}}\n", "", ""},
			{"includes", "include: [more.yaml]\n" + base, "", ""},
			{"missing-listener", "plugins: []\n", "127.0.0.1:53", ""},
			{"broken-yaml", "plugins: [", "127.0.0.1:53", ""},
		} {
			t.Run(service+"/"+test.name, func(t *testing.T) {
				cfg := &Config{ServiceType: service, ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), HealthDNSAddress: test.health}
				writeWatchdogConfig(t, cfg, test.config)
				target, err := watchdogTargetFor(cfg)
				got := target.address
				if test.want == "" {
					if err == nil && target.kind == "dns" {
						t.Fatalf("unsafe configuration accepted: %q", got)
					}
				} else if err != nil || target.kind != "dns" || got != test.want {
					t.Fatalf("got %q, %v; want %q", got, err, test.want)
				}
			})
		}
	}
}

func guardedWatchdogConfig(listen string) string {
	return fmt.Sprintf("plugins:\n  - tag: main\n    type: sequence\n    args:\n      - {matches: ['qname full:configflow-watchdog.invalid'], exec: 'reject 3'}\n      - {exec: '$upstream'}\n  - {tag: tcp, type: tcp_server, args: {listen: '0.0.0.0:5300', entry: main}}\n  - {tag: udp, type: udp_server, args: {listen: %q, entry: main}}\n", listen)
}

func writeWatchdogConfig(t *testing.T, cfg *Config, content string) {
	t.Helper()
	if err := os.WriteFile(cfg.ConfigPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestWatchdogTargetForMihomo(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{ServiceType: "mihomo", ConfigPath: filepath.Join(dir, "config.yaml")}
	os.WriteFile(cfg.ConfigPath, []byte("external-controller: 0.0.0.0:9090\nsecret: s3cret\ndns:\n  enable: true\n  listen: 0.0.0.0:1053\n"), 0600)
	target, err := watchdogTargetFor(cfg)
	if err != nil || target.kind != "http" || target.address != "127.0.0.1:9090" || target.secret != "s3cret" {
		t.Fatalf("target = %+v, %v", target, err)
	}
	if strings.Contains(target.String(), "s3cret") {
		t.Fatal("target must not print its secret")
	}
	os.WriteFile(cfg.ConfigPath, []byte("dns:\n  enable: true\n  listen: 0.0.0.0:1053\n"), 0600)
	if target, err = watchdogTargetFor(cfg); err == nil {
		t.Fatalf("unguarded mihomo DNS fallback must stay disabled: %+v", target)
	}
	os.WriteFile(cfg.ConfigPath, []byte("mode: rule\n"), 0600)
	if _, err = watchdogTargetFor(cfg); err == nil {
		t.Fatal("nothing to probe must be an error")
	}
}

func TestControllerAnswersAcceptsAnyHTTPResponse(t *testing.T) {
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	address := strings.TrimPrefix(server.URL, "http://")
	if err := controllerAnswers(address, "s3cret"); err != nil || auth != "Bearer s3cret" {
		t.Fatalf("401 must count as alive: %v auth=%q", err, auth)
	}
	server.Close()
	if err := controllerAnswers(address, ""); err == nil {
		t.Fatal("closed controller must fail")
	}
}

type watchdogFakes struct {
	held     bool // service lock currently taken by the watchdog
	now      time.Time
	healthy  bool
	running  bool
	busy     bool
	restarts int
}

func newTestWatchdog(t *testing.T, f *watchdogFakes) *serviceWatchdog {
	t.Helper()
	cfg := &Config{ServiceType: "oxidns", ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), HealthDNSAddress: "127.0.0.1:53"}
	writeWatchdogConfig(t, cfg, guardedWatchdogConfig("127.0.0.1:53"))
	w := newDNSWatchdog(cfg)
	w.now = func() time.Time { return f.now }
	w.probe = func(watchdogTarget) error {
		if f.held {
			t.Error("the service lock must not be held while probing")
		}
		if f.healthy {
			return nil
		}
		return errors.New("timeout")
	}
	w.running = func(*Config) (bool, error) { return f.running, nil }
	w.lock = func(*Config) (func(), error) {
		if f.busy || f.held {
			return nil, errDeploymentBusy
		}
		f.held = true
		return func() { f.held = false }, nil
	}
	w.restart = func(*Config) ([]byte, error) {
		if !f.held {
			t.Error("restarting must hold the service lock")
		}
		f.restarts++
		return nil, nil
	}
	return w
}

func TestWatchdogRestartsHungCoreAfterConsecutiveFailures(t *testing.T) {
	f := &watchdogFakes{now: time.Unix(1000, 0), running: true}
	w := newTestWatchdog(t, f)
	steps := []string{}
	for i := 0; i < 3; i++ {
		steps = append(steps, w.tick())
		f.now = f.now.Add(10 * time.Second)
	}
	if want := []string{"failing", "failing", "restarted"}; !equalStrings(steps, want) || f.restarts != 1 {
		t.Fatalf("steps=%v restarts=%d", steps, f.restarts)
	}
	if got := w.tick(); got != "grace" {
		t.Fatalf("restart must be followed by a grace period, got %s", got)
	}
	f.now = f.now.Add(61 * time.Second)
	f.healthy = true
	if got := w.tick(); got != "ok" {
		t.Fatalf("got %s", got)
	}
}

func TestWatchdogRecoveryResetsFailureCount(t *testing.T) {
	f := &watchdogFakes{now: time.Unix(1000, 0), running: true}
	w := newTestWatchdog(t, f)
	w.tick()
	w.tick()
	f.healthy = true
	w.tick()
	f.healthy = false
	w.tick()
	w.tick()
	if f.restarts != 0 {
		t.Fatal("a successful probe must reset the consecutive count")
	}
}

func TestWatchdogLeavesStoppedOrBusyServiceAlone(t *testing.T) {
	f := &watchdogFakes{now: time.Unix(1000, 0), running: false}
	w := newTestWatchdog(t, f)
	for i := 0; i < 5; i++ {
		if got := w.tick(); got != "not-running" {
			t.Fatalf("stopped service: got %s", got)
		}
	}
	f.running, f.busy = true, true
	for i := 0; i < 5; i++ {
		if got := w.tick(); got != "busy" {
			t.Fatalf("deployment in progress: got %s", got)
		}
	}
	if f.restarts != 0 {
		t.Fatal("watchdog must not restart a stopped service or during a deployment")
	}
}

func TestWatchdogRestartBudget(t *testing.T) {
	f := &watchdogFakes{now: time.Unix(1000, 0), running: true}
	w := newTestWatchdog(t, f)
	results := map[string]int{}
	for i := 0; i < 40; i++ { // 40 min of a permanently hung core
		results[w.tick()]++
		f.now = f.now.Add(time.Minute)
	}
	if f.restarts != 3 || results["budget-exhausted"] == 0 {
		t.Fatalf("restarts=%d results=%v", f.restarts, results)
	}
	f.now = f.now.Add(time.Hour) // window passes
	for i := 0; i < 3; i++ {
		w.tick()
	}
	if f.restarts != 4 {
		t.Fatalf("budget must refill after the window, restarts=%d", f.restarts)
	}
}

func TestWatchdogPausesUnsafeConfigurationsWithoutProbingAndResumes(t *testing.T) {
	f := &watchdogFakes{now: time.Unix(1000, 0), running: true}
	w := newTestWatchdog(t, f)
	w.tick()
	w.tick()
	writeWatchdogConfig(t, w.cfg, strings.Replace(guardedWatchdogConfig("127.0.0.1:53"), "reject 3", "reject 2", 1))
	var logs bytes.Buffer
	previousOutput := logx.SetOutput(&logs)
	t.Cleanup(func() { logx.SetOutput(previousOutput) })
	probe := w.probe
	w.probe = func(watchdogTarget) error {
		t.Fatal("an unsafe configuration must not send even one DNS probe")
		return nil
	}
	for i := 0; i < 4; i++ {
		if got := w.tick(); got != "probe-unavailable" || w.consecutive != 0 {
			t.Fatalf("unsafe configuration: got %s, failures=%d", got, w.consecutive)
		}
	}
	if count := strings.Count(logs.String(), "probe paused:"); count != 1 {
		t.Fatalf("unchanged pause reason must log once, got %d: %s", count, logs.String())
	}
	writeWatchdogConfig(t, w.cfg, guardedWatchdogConfig("127.0.0.1:53"))
	w.probe = probe
	if got := w.tick(); got != "failing" || w.consecutive != 1 || f.restarts != 0 {
		t.Fatalf("new publication must resume with a fresh failure count: %s, failures=%d, restarts=%d", got, w.consecutive, f.restarts)
	}
	if strings.Count(logs.String(), "probe available again") != 1 {
		t.Fatalf("resume was not logged: %s", logs.String())
	}
}

func TestWatchdogUnreadableConfigurationAndMihomoFallbackNeverProbe(t *testing.T) {
	for _, scenario := range []string{"missing", "invalid", "mihomo-dns-only"} {
		t.Run(scenario, func(t *testing.T) {
			f := &watchdogFakes{now: time.Unix(1000, 0), running: true}
			w := newTestWatchdog(t, f)
			w.consecutive = 2
			switch scenario {
			case "missing":
				if err := os.Remove(w.cfg.ConfigPath); err != nil {
					t.Fatal(err)
				}
			case "invalid":
				writeWatchdogConfig(t, w.cfg, "plugins: [")
			case "mihomo-dns-only":
				w.cfg.ServiceType = "mihomo"
				writeWatchdogConfig(t, w.cfg, "dns: {enable: true, listen: '127.0.0.1:53'}\n")
			}
			w.probe = func(watchdogTarget) error {
				t.Fatal("unavailable safe target must never be probed")
				return nil
			}
			if got := w.tick(); got != "probe-unavailable" || w.consecutive != 0 || f.restarts != 0 {
				t.Fatalf("got %s, failures=%d, restarts=%d", got, w.consecutive, f.restarts)
			}
		})
	}
}

func TestWatchdogConfigurationChangesResetFailures(t *testing.T) {
	f := &watchdogFakes{now: time.Unix(1000, 0), running: true}
	w := newTestWatchdog(t, f)
	w.tick()
	w.tick()
	writeWatchdogConfig(t, w.cfg, guardedWatchdogConfig("127.0.0.1:53")+"# new publication\n")
	if got := w.tick(); got != "failing" || w.consecutive != 1 || f.restarts != 0 {
		t.Fatalf("old failures carried into new publication: %s, failures=%d, restarts=%d", got, w.consecutive, f.restarts)
	}
}

func TestWatchdogRechecksConfigurationAndRunningStateBeforeRestart(t *testing.T) {
	for _, scenario := range []string{"new-publication", "removed-guard", "stopped"} {
		t.Run(scenario, func(t *testing.T) {
			f := &watchdogFakes{now: time.Unix(1000, 0), running: true}
			w := newTestWatchdog(t, f)
			w.tick()
			w.tick()
			w.probe = func(watchdogTarget) error {
				switch scenario {
				case "new-publication":
					writeWatchdogConfig(t, w.cfg, guardedWatchdogConfig("127.0.0.1:53")+"# new publication\n")
				case "removed-guard":
					writeWatchdogConfig(t, w.cfg, "plugins: []\n")
				case "stopped":
					f.running = false
				}
				return errors.New("timeout")
			}
			if got := w.tick(); got == "restarted" || f.restarts != 0 || w.consecutive != 0 {
				t.Fatalf("stale failure restarted changed service: %s, failures=%d, restarts=%d", got, w.consecutive, f.restarts)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
