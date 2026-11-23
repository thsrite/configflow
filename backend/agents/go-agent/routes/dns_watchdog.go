package routes

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Service watchdog. The service manager (supervise-daemon, systemd) restarts a
// crashed core, but a hung one keeps its process alive while it stops serving.
// The watchdog probes the core locally (DNS listener for OxiDNS/MosDNS, the
// external controller for mihomo) and restarts the service when it stops
// answering, without fighting deployments or deliberate stops.

const watchdogProbeName = "configflow-watchdog.invalid."

// watchdogTarget is what gets probed; String never includes the secret.
type watchdogTarget struct {
	kind     string // "dns" or "http"
	address  string
	secret   string
	revision [sha256.Size]byte // failures must never carry across configuration changes
}

func (t watchdogTarget) String() string { return t.kind + "://" + t.address }

type serviceWatchdog struct {
	cfg *Config

	interval    time.Duration
	failures    int           // consecutive failed probes before restarting
	grace       time.Duration // after a restart, time for the core to start
	maxRestarts int           // per window; beyond it only log
	window      time.Duration

	probe   func(target watchdogTarget) error
	running func(cfg *Config) (bool, error)
	lock    func(cfg *Config) (func(), error)
	restart func(cfg *Config) ([]byte, error)
	now     func() time.Time

	consecutive  int
	restarts     []time.Time
	pausedUntil  time.Time
	gaveUp       bool
	lastTarget   watchdogTarget
	pausedReason string
}

func newDNSWatchdog(cfg *Config) *serviceWatchdog {
	return &serviceWatchdog{
		cfg: cfg, interval: 10 * time.Second, failures: 3, grace: 60 * time.Second,
		maxRestarts: 3, window: time.Hour,
		probe: probeWatchdogTarget, running: serviceRunning, lock: AcquireServiceOperation,
		restart: RestartService, now: time.Now,
	}
}

// StartDNSWatchdog runs the watchdog for OxiDNS, MosDNS and mihomo unless disabled.
func StartDNSWatchdog(cfg *Config) {
	switch cfg.ServiceType {
	case "oxidns", "mosdns", "mihomo":
	default:
		return
	}
	if cfg.DNSWatchdogDisabled {
		return
	}
	w := newDNSWatchdog(cfg)
	log.Printf("Watchdog enabled for %s: probe every %s, restart after %d silent probes, at most %d restarts per %s",
		cfg.ServiceType, w.interval, w.failures, w.maxRestarts, w.window)
	go func() {
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for range ticker.C {
			w.tick()
		}
	}()
}

// tick runs one probe round; it returns what it did, for tests and logs.
func (w *serviceWatchdog) tick() string {
	now := w.now()
	if now.Before(w.pausedUntil) {
		return "grace"
	}
	// Only check that no deployment, restart or upgrade owns the service; the
	// lock is not held while probing, or the periodic disk cleanup (same
	// lock, same start time) would collide with every tick that lines up.
	release, err := w.lock(w.cfg)
	if err != nil {
		w.consecutive = 0
		return "busy"
	}
	if running, err := w.running(w.cfg); err != nil || !running {
		release()
		// Crashes are the service manager's job; a stopped service stays stopped.
		w.consecutive = 0
		return "not-running"
	}
	target, err := watchdogTargetFor(w.cfg)
	release()
	if err != nil {
		return w.pause(err)
	}
	if w.pausedReason != "" {
		log.Printf("Watchdog: %s probe available again", w.cfg.ServiceType)
		w.pausedReason = ""
	}
	if target != w.lastTarget {
		w.consecutive = 0
		w.lastTarget = target
	}
	if err = w.probe(target); err == nil {
		if w.consecutive > 0 {
			log.Printf("Watchdog: %s answering again", target)
		}
		w.consecutive, w.gaveUp = 0, false
		return "ok"
	}
	w.consecutive++
	log.Printf("Watchdog: probe %d/%d to %s failed: %v", w.consecutive, w.failures, target, err)
	if w.consecutive < w.failures {
		return "failing"
	}
	recent := w.restarts[:0]
	for _, at := range w.restarts {
		if now.Sub(at) < w.window {
			recent = append(recent, at)
		}
	}
	w.restarts = recent
	if len(w.restarts) >= w.maxRestarts {
		if !w.gaveUp {
			log.Printf("Watchdog: %d restarts within %s, not restarting again until the window passes", len(w.restarts), w.window)
			w.gaveUp = true
		}
		return "budget-exhausted"
	}
	release, err = w.lock(w.cfg)
	if err != nil {
		w.consecutive = 0
		return "busy"
	}
	defer release()
	// A deployment may have completed while the probe was waiting. Recheck the
	// exact configuration under the operation lock before acting on its failure.
	current, err := watchdogTargetFor(w.cfg)
	if err != nil {
		return w.pause(err)
	}
	if current != target {
		w.consecutive = 0
		w.lastTarget = current
		return "configuration-changed"
	}
	if running, err := w.running(w.cfg); err != nil || !running {
		w.consecutive = 0
		return "not-running"
	}
	log.Printf("Watchdog: %s is running but not answering, restarting %s", target, w.cfg.ServiceType)
	w.restarts = append(w.restarts, now)
	w.consecutive = 0
	w.pausedUntil = now.Add(w.grace)
	if _, err = w.restart(w.cfg); err != nil {
		log.Printf("Watchdog: restart failed: %v", err)
		return "restart-failed"
	}
	return "restarted"
}

func (w *serviceWatchdog) pause(err error) string {
	w.consecutive = 0
	w.lastTarget = watchdogTarget{}
	if reason := err.Error(); reason != w.pausedReason {
		log.Printf("Watchdog: %s probe paused: %s", w.cfg.ServiceType, reason)
		w.pausedReason = reason
	}
	return "probe-unavailable"
}

// A DNS probe is safe only when this exact active configuration handles the
// reserved name locally, before any upstream or other user-supplied executor.
func watchdogTargetFor(cfg *Config) (watchdogTarget, error) {
	data, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		return watchdogTarget{}, fmt.Errorf("cannot read active configuration")
	}
	var doc map[string]any
	if err = yaml.Unmarshal(data, &doc); err != nil || len(doc) == 0 {
		return watchdogTarget{}, fmt.Errorf("cannot parse active configuration")
	}
	revision := sha256.Sum256(data)
	if cfg.ServiceType == "mihomo" {
		if controller, _ := doc["external-controller"].(string); controller != "" {
			secret, _ := doc["secret"].(string)
			return watchdogTarget{kind: "http", address: loopbackAddress(controller), secret: secret, revision: revision}, nil
		}
		return watchdogTarget{}, fmt.Errorf("no external-controller; unguarded DNS fallback is disabled")
	}
	if cfg.ServiceType != "oxidns" && cfg.ServiceType != "mosdns" {
		return watchdogTarget{}, fmt.Errorf("unsupported service type")
	}
	address, err := guardedWatchdogDNSAddress(cfg, doc)
	return watchdogTarget{kind: "dns", address: address, revision: revision}, err
}

func probeWatchdogTarget(t watchdogTarget) error {
	if t.kind == "http" {
		return controllerAnswers(t.address, t.secret)
	}
	return dnsAnswers(t.address)
}

// controllerAnswers treats any HTTP response from mihomo's controller as alive:
// a 401 still proves the core is serving, and liveness is all that matters here.
func controllerAnswers(address, secret string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/version", nil)
	if err != nil {
		return err
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	client := http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("controller unreachable")
	}
	resp.Body.Close()
	return nil
}

// watchdogDNSAddress never trusts a health address independently of the core's
// actual local UDP listeners and the entry sequence that handles their queries.
func watchdogDNSAddress(cfg *Config) (string, error) {
	target, err := watchdogTargetFor(cfg)
	if err == nil && target.kind != "dns" {
		return "", fmt.Errorf("no guarded DNS target")
	}
	return target.address, err
}

func guardedWatchdogDNSAddress(cfg *Config, doc map[string]any) (string, error) {
	if include := doc["include"]; include != nil {
		values, ok := include.([]any)
		if !ok || len(values) != 0 {
			return "", fmt.Errorf("included DNS configuration cannot be checked for a local probe rule")
		}
	}
	healthAddress := ""
	if cfg.HealthDNSAddress != "" {
		var err error
		healthAddress, err = oxMonitorLocalAddress(loopbackAddress(cfg.HealthDNSAddress))
		if err != nil {
			return "", fmt.Errorf("health_dns_address is not a supported local IP endpoint")
		}
	}
	plugins, _ := doc["plugins"].([]any)
	byTag := map[string]map[string]any{}
	for _, value := range plugins {
		plugin, ok := value.(map[string]any)
		if !ok {
			return "", fmt.Errorf("unsupported DNS plugin configuration")
		}
		if tag, _ := plugin["tag"].(string); tag != "" {
			if byTag[tag] != nil {
				return "", fmt.Errorf("ambiguous DNS plugin tags")
			}
			byTag[tag] = plugin
		}
	}
	var selected map[string]any
	address := ""
	for _, value := range plugins {
		plugin := value.(map[string]any)
		if plugin["type"] != "udp_server" {
			continue
		}
		args, _ := plugin["args"].(map[string]any)
		listen, _ := args["listen"].(string)
		local, err := oxMonitorLocalAddress(loopbackAddress(listen))
		if err != nil {
			return "", fmt.Errorf("UDP listener is not a supported local IP endpoint")
		}
		if healthAddress != "" && local != healthAddress {
			continue
		}
		if selected != nil {
			if local == address {
				return "", fmt.Errorf("ambiguous UDP listener address")
			}
			continue
		}
		selected, address = args, local
	}
	if selected == nil {
		return "", fmt.Errorf("no UDP listener matches the local health address")
	}
	entry, _ := selected["entry"].(string)
	sequence := byTag[entry]
	steps, _ := sequence["args"].([]any)
	if sequence["type"] != "sequence" || len(steps) == 0 || !isWatchdogLocalRule(steps[0]) {
		return "", fmt.Errorf("UDP entry lacks the first local watchdog rule; publish the configuration to enable probing")
	}
	return address, nil
}

func isWatchdogLocalRule(value any) bool {
	rule, ok := value.(map[string]any)
	if !ok || len(rule) != 2 || rule["exec"] != "reject 3" {
		return false
	}
	matches, _ := rule["matches"].([]any)
	return len(matches) == 1 && matches[0] == "qname full:"+strings.TrimSuffix(watchdogProbeName, ".")
}

// dnsAnswers sends one UDP query and accepts any well-formed reply with the
// same ID: NXDOMAIN or SERVFAIL still prove the core is serving, and upstream
// outages must not look like a hung core.
func dnsAnswers(address string) error {
	conn, err := net.DialTimeout("udp", address, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	var idBytes [2]byte
	if _, err = rand.Read(idBytes[:]); err != nil {
		return err
	}
	query := buildDNSQuery(binary.BigEndian.Uint16(idBytes[:]), watchdogProbeName)
	if err = conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	if _, err = conn.Write(query); err != nil {
		return err
	}
	reply := make([]byte, 1500)
	for {
		n, err := conn.Read(reply)
		if err != nil {
			return fmt.Errorf("no reply: %w", err)
		}
		// Header: ID, flags with QR set.
		if n >= 12 && reply[0] == query[0] && reply[1] == query[1] && reply[2]&0x80 != 0 {
			return nil
		}
	}
}

func buildDNSQuery(id uint16, name string) []byte {
	msg := make([]byte, 12, 64)
	binary.BigEndian.PutUint16(msg[0:], id)
	msg[2] = 0x01                          // RD
	binary.BigEndian.PutUint16(msg[4:], 1) // QDCOUNT
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			if label := name[start:i]; label != "" {
				msg = append(msg, byte(len(label)))
				msg = append(msg, label...)
			}
			start = i + 1
		}
	}
	msg = append(msg, 0, 0, 1, 0, 1) // root, QTYPE A, QCLASS IN
	return msg
}
