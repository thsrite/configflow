package installer

// Fake host commands for the installer shell tests, implemented in Go inside
// this test binary. A stub file is a tiny /bin/sh wrapper that re-executes the
// test binary with CONFIGFLOW_INSTALLER_STUB set; TestMain then runs the named
// stub instead of the tests. The stubs used to be Python programs run by the
// system python3; the tests no longer need any interpreter.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/testgolden"
)

const (
	stubEnv      = "CONFIGFLOW_INSTALLER_STUB"
	stubArgv0Env = "CONFIGFLOW_INSTALLER_STUB_ARGV0"
)

func TestMain(m *testing.M) {
	if name := os.Getenv(stubEnv); name != "" {
		os.Exit(runStub(name, os.Getenv(stubArgv0Env), os.Args[1:]))
	}
	code := m.Run()
	if agentBuild.dir != "" {
		os.RemoveAll(agentBuild.dir)
	}
	os.Exit(code)
}

// stubScript returns an executable /bin/sh wrapper that runs the named Go stub
// with the wrapper's own path as argv[0]. vars are exported KEY=VALUE pairs
// baked into the wrapper; marker, when set, is written as a comment so tests
// can recognize the file by content.
func stubScript(t *testing.T, name, marker string, vars ...string) string {
	t.Helper()
	if _, ok := stubs[name]; !ok {
		t.Fatalf("unknown installer stub %q", name)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary path: %v", err)
	}
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	if marker != "" {
		b.WriteString("# " + marker + "\n")
	}
	for _, kv := range append([]string{stubEnv + "=" + name}, vars...) {
		key, value, _ := strings.Cut(kv, "=")
		b.WriteString(key + "=" + shlexQuote(value) + "; export " + key + "\n")
	}
	b.WriteString(stubArgv0Env + "=\"$0\"; export " + stubArgv0Env + "\n")
	// A -race test binary sleeps atexit_sleep_ms (1 s by default) on exit while
	// other threads are alive; thousands of stub calls would add an hour.
	b.WriteString("GORACE=\"${GORACE:+$GORACE }atexit_sleep_ms=0\"; export GORACE\n")
	b.WriteString("exec " + shlexQuote(exe) + " \"$@\"\n")
	return b.String()
}

// stubCall is one stub invocation.
type stubCall struct {
	argv0 string
	name  string // basename of argv0
	args  []string
	out   io.Writer
	err   io.Writer
}

// stubExit ends a stub early with a status (raise SystemExit(n) in the old
// Python stubs); stubFailure ends it with a message on stderr and status 1.
type (
	stubExit    int
	stubFailure string
)

func runStub(name, argv0 string, args []string) (code int) {
	run, ok := stubs[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown installer stub %q\n", name)
		return 2
	}
	c := &stubCall{argv0: argv0, name: filepath.Base(argv0), args: args, out: os.Stdout, err: os.Stderr}
	defer func() {
		switch v := recover().(type) {
		case nil:
		case stubExit:
			code = int(v)
		case stubFailure:
			fmt.Fprintln(os.Stderr, string(v))
			code = 1
		default:
			fmt.Fprintf(os.Stderr, "stub %s %q: %v\n", name, args, v)
			code = 1
		}
	}()
	run(c)
	return 0
}

func (c *stubCall) exit(code int)                 { panic(stubExit(code)) }
func (c *stubCall) fail(format string, a ...any)  { panic(stubFailure(fmt.Sprintf(format, a...))) }
func (c *stubCall) println(a ...any)              { fmt.Fprintln(c.out, a...) }
func (c *stubCall) eprintln(a ...any)             { fmt.Fprintln(c.err, a...) }
func (c *stubCall) has(value string) bool         { return slices.Contains(c.args, value) }
func (c *stubCall) argsAre(values ...string) bool { return slices.Equal(c.args, values) }
func (c *stubCall) joined() string                { return strings.Join(c.args, " ") }
func (c *stubCall) check(ok bool, format string, a ...any) {
	if !ok {
		c.fail("assertion failed: "+format, a...)
	}
}

// after returns the argument following flag; a missing flag fails the stub.
func (c *stubCall) after(flag string) string {
	i := slices.Index(c.args, flag)
	if i < 0 || i+1 >= len(c.args) {
		c.fail("missing %s in %q", flag, c.args)
	}
	return c.args[i+1]
}

func (c *stubCall) appendFile(path, text string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		c.fail("%v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		c.fail("%v", err)
	}
}

func (c *stubCall) log(text string) { c.appendFile(os.Getenv("EVENTS"), text+"\n") }

// logJSON appends value as one JSON line.
func (c *stubCall) logJSON(path string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		c.fail("%v", err)
	}
	c.appendFile(path, string(data)+"\n")
}

func (c *stubCall) read(path string) string {
	data, err := testgolden.ReadFileOutside(path)
	if err != nil {
		c.fail("%v", err)
	}
	return string(data)
}

func (c *stubCall) write(path, text string) {
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		c.fail("%v", err)
	}
}

func (c *stubCall) readJSON(path string, value any) {
	if err := json.Unmarshal([]byte(c.read(path)), value); err != nil {
		c.fail("%s: %v", path, err)
	}
}

func (c *stubCall) writeJSON(path string, value any, indent bool) {
	var data []byte
	var err error
	if indent {
		data, err = json.MarshalIndent(value, "", "  ")
	} else {
		data, err = json.Marshal(value)
	}
	if err != nil {
		c.fail("%v", err)
	}
	c.write(path, string(data))
}

func (c *stubCall) copyFile(src, dst string) {
	c.write(dst, c.read(src))
}

func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }
func sha256File(c *stubCall, path string) string {
	sum := sha256.Sum256([]byte(c.read(path)))
	return hex.EncodeToString(sum[:])
}

// agentConfig reads the JSON config named by -config.
func (c *stubCall) agentConfig() map[string]any {
	var cfg map[string]any
	c.readJSON(c.after("-config"), &cfg)
	return cfg
}

func str(m map[string]any, key string) string { s, _ := m[key].(string); return s }

var stubs = map[string]func(*stubCall){
	"single-agent":       singleAgentStub,
	"core-service":       coreServiceStub,
	"core-package":       corePackageStub,
	"core-validate":      coreValidateStub,
	"core-download":      coreDownloadStub,
	"sha256sum":          sha256sumStub,
	"core-dig":           coreDigStub,
	"core-forbidden":     coreForbiddenStub,
	"gateway-core":       gatewayCoreStub,
	"gateway-agent":      gatewayAgentStub,
	"gateway-manager":    gatewayManagerStub,
	"gateway-curl":       gatewayCurlStub,
	"gateway-sysctl":     gatewaySysctlStub,
	"gateway-package":    gatewayPackageStub,
	"dependency-package": dependencyPackageStub,
	"l1-manager":         l1ManagerStub,
	"aio-agent":          aioAgentStub,
	"aio-sysctl":         aioSysctlStub,
	"standalone-sysctl":  standaloneSysctlStub,
	"update-candidate":   updateCandidateStub,
	"update-systemctl":   updateSystemctlStub,
	"ha-systemctl":       haSystemctlStub,
	"ha-package":         haPackageStub,
	"docker-log":         dockerLogStub,
	"preflight-docker":   preflightDockerStub,
	"guide-docker":       guideDockerStub,
	"compose-docker":     composeDockerStub,
}

// singleAgentStub is the fake Agent binary of the single-service installer.
func singleAgentStub(c *stubCall) {
	c.log("agent " + c.joined())
	if c.argsAre("-h") || c.argsAre("-version") {
		c.exit(0)
	}
	cfg := c.agentConfig()
	c.check(str(cfg, "service_manager") == os.Getenv("TEST_MANAGER"), "service_manager %v", cfg["service_manager"])
	if c.has("-check-environment") && os.Getenv("VERBOSE_ENVIRONMENT") == "yes" {
		c.println("verbose-environment-detail-for-log-only")
	}
	if c.has("-check-deployments") && os.Getenv("PENDING_DEPLOYMENT") == "yes" {
		c.eprintln("2026/10/18 12:00:00 Deployment check requires attention: pending publication; inspect its journal")
	}
}

// coreServiceStub is systemctl / rc-service / rc-update of the core installer.
func coreServiceStub(c *stubCall) {
	name, args := c.name, c.args
	c.log(name + " " + c.joined())
	if args[0] == "show" {
		switch {
		case c.has("--property=MainPID"):
			c.println("12345")
		case os.Getenv("EXISTING_SERVICE") == args[1]:
			c.println("loaded")
		default:
			c.println("not-found")
		}
		c.exit(0)
	}
	action := args[0]
	if name == "rc-service" {
		action = args[1]
	}
	var service string
	switch name {
	case "systemctl":
		service = args[len(args)-1]
	case "rc-service":
		service = args[0]
	default:
		service = args[1]
	}
	root := os.Getenv("ROOT")
	if name == "systemctl" && service == "systemd-resolved" {
		if action == "is-active" {
			if os.Getenv("RESOLVED_ACTIVE") != "" {
				c.exit(0)
			}
			c.exit(3)
		}
		if action == "restart" && fileExists(filepath.Join(root, "etc/systemd/resolved.conf.d/configflow.conf")) {
			// The stub listener is gone once the drop-in disables it.
			for _, network := range []string{"tcp", "udp", "tcp6", "udp6"} {
				path := filepath.Join(root, "proc/net", network)
				var kept []string
				for _, line := range strings.SplitAfter(c.read(path), "\n") {
					if !strings.Contains(line, "3500007F:0035") && !strings.Contains(line, "3600007F:0035") {
						kept = append(kept, line)
					}
				}
				c.write(path, strings.Join(kept, ""))
			}
		}
		c.exit(0)
	}
	if os.Getenv("FAIL_SERVICE") == action+":"+service || os.Getenv("FAIL_ROLLBACK") == action+":"+service {
		if name == "rc-service" && action == "start" {
			c.write(filepath.Join(root, "etc", service, service+".log"), "core startup error: fixture failure\n")
		}
		c.exit(38)
	}
	if name == "rc-update" || service != "mihomo" {
		return
	}
	iface := filepath.Join(root, "sys/class/net/configflow-tun")
	fdinfo := filepath.Join(root, "proc/12345/fdinfo")
	switch action {
	case "start":
		counter := filepath.Join(root, "run/mihomo-start-count")
		count := 1
		if fileExists(counter) {
			n, err := strconv.Atoi(strings.TrimSpace(c.read(counter)))
			if err != nil {
				c.fail("%v", err)
			}
			count = n + 1
		}
		c.write(counter, strconv.Itoa(count))
		if count > 1 && os.Getenv("FAIL_FALLBACK_START") != "" {
			c.exit(38)
		}
		c.write(filepath.Join(root, "run/mihomo.pid"), "12345")
		enabled := strings.Contains(c.read(filepath.Join(root, "etc/mihomo/config.yaml")), "tun:\n  enable: true\n")
		if enabled && os.Getenv("NO_TUN_INTERFACE") == "" && os.Getenv("LATE_TUN_READY") == "" {
			os.MkdirAll(iface, 0o755)
			c.write(filepath.Join(iface, "tun_flags"), envOr("TUN_FLAGS", "0x1001")+"\n")
			c.write(filepath.Join(iface, "flags"), envOr("TUN_LINK_FLAGS", "0x1001")+"\n")
			os.MkdirAll(fdinfo, 0o755)
			c.write(filepath.Join(fdinfo, "8"), "iff:\t"+envOr("TUN_OWNER_DEVICE", "configflow-tun")+"\n")
		}
	case "stop":
		os.RemoveAll(iface)
		os.RemoveAll(fdinfo)
	}
}

func envOr(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// coreArgsHave reports whether any argument equals one of names.
func (c *stubCall) anyArg(names ...string) bool {
	for _, n := range names {
		if c.has(n) {
			return true
		}
	}
	return false
}

// corePackageStub is the package manager of the core installer.
func corePackageStub(c *stubCall) {
	c.log("package " + c.name + " " + c.joined() + " DEBIAN_FRONTEND=" + os.Getenv("DEBIAN_FRONTEND"))
	updating := c.argsAre("update")
	want := "install"
	if updating {
		want = "update"
	}
	if os.Getenv("FAIL_PACKAGE") == want {
		c.exit(41)
	}
	if updating || os.Getenv("PACKAGE_NO_COMMANDS") != "" {
		c.exit(0)
	}
	commands := os.Getenv("PATH")
	if c.anyArg("tzdata", "timezone") {
		zone := filepath.Join(os.Getenv("ROOT"), "usr/share/zoneinfo/Asia/Shanghai")
		os.MkdirAll(filepath.Dir(zone), 0o755)
		c.write(zone, "TZif")
	}
	unavailable := filepath.Join(os.Getenv("FIXTURE"), "unavailable-commands")
	entries, err := testgolden.ReadDirOutside(unavailable)
	if err != nil {
		c.fail("%v", err)
	}
	for _, e := range entries {
		// Restore the real fixture command only after a successful fake transaction.
		if err := os.Rename(filepath.Join(unavailable, e.Name()), filepath.Join(commands, e.Name())); err != nil {
			c.fail("%v", err)
		}
	}
}

// coreValidateStub is the fake mihomo/oxidns binary the core installer validates.
func coreValidateStub(c *stubCall) {
	name, flag := "oxidns", "-c"
	if c.has("-t") {
		name, flag = "mihomo", "-f"
	}
	c.log("validate " + name)
	info, err := os.Stat(c.after(flag))
	c.check(err == nil && info.Mode().IsRegular(), "config is a file")
	if os.Getenv("FAIL_VALIDATE") == name {
		c.exit(29)
	}
}

// coreDownloadStub is curl (--output) or wget (-O) of the core installer.
func coreDownloadStub(c *stubCall) {
	url := c.args[len(c.args)-1]
	c.log("download " + url)
	if os.Getenv("FAIL_DOWNLOAD") != "" {
		c.exit(22)
	}
	source := "oxidns.tar.gz"
	if strings.Contains(url, "MetaCubeX") {
		source = "mihomo.gz"
	}
	flag := "--output"
	if c.name == "wget" {
		flag = "-O"
	}
	output := c.after(flag)
	c.copyFile(filepath.Join(os.Getenv("FIXTURE"), source), output)
	if os.Getenv("BAD_CHECKSUM") == source {
		c.appendFile(output, "bad")
	}
}

// sha256sumStub supports "sha256sum FILE..." and "sha256sum -c LIST".
func sha256sumStub(c *stubCall) {
	if len(c.args) > 0 && c.args[0] == "-c" {
		for _, line := range strings.Split(strings.TrimRight(c.read(c.args[1]), "\n"), "\n") {
			digest, file, _ := strings.Cut(strings.TrimRight(line, " \t\r"), "  ")
			if sha256File(c, file) != digest {
				c.println(file + ": FAILED")
				c.exit(1)
			}
			c.println(file + ": OK")
		}
		return
	}
	for _, file := range c.args {
		c.println(sha256File(c, file) + "  " + file)
	}
}

func coreDigStub(c *stubCall) {
	c.log("dig " + c.joined())
	switch os.Getenv("FAIL_DIG") {
	case "exit":
		c.exit(9)
	case "servfail":
		c.println(";; ->>HEADER<<- opcode: QUERY, status: SERVFAIL, id: 17")
		c.println(";; flags: qr rd ra; QUERY: 1, ANSWER: 0, AUTHORITY: 0, ADDITIONAL: 0")
	default:
		c.println(";; ->>HEADER<<- opcode: QUERY, status: NOERROR, id: 17")
		c.println(";; flags: qr rd ra; QUERY: 1, ANSWER: 1, AUTHORITY: 0, ADDITIONAL: 0")
	}
}

func coreForbiddenStub(c *stubCall) {
	c.log("FORBIDDEN " + c.name)
	c.exit(99)
}

// gatewayCoreStub is the fake mihomo/oxidns binary of the gateway installer.
func gatewayCoreStub(c *stubCall) {
	if c.has("-v") {
		version := "v1.19.32"
		if strings.Contains(c.argv0, "/usr/local/bin/") {
			version = envOr("EXISTING_MIHOMO_VERSION", "v1.19.32")
		}
		c.println("Mihomo Meta " + version + " linux amd64")
		c.exit(0)
	}
	if c.has("--version") {
		c.println("oxidns 1.6.1")
		c.exit(0)
	}
	c.log("core-check " + c.name + " " + c.joined())
	if os.Getenv("FAIL_CHECK") == c.name {
		c.exit(41)
	}
	path := ""
	if c.has("-f") {
		path = c.after("-f")
	} else {
		path = c.after("-c")
	}
	info, err := os.Stat(path)
	c.check(err == nil && info.Mode().IsRegular(), "config is a file")
}

// gatewayAgentStub is the fake Agent binary of the gateway installer.
func gatewayAgentStub(c *stubCall) {
	if c.argsAre("-h") {
		c.exit(0)
	}
	if c.argsAre("-version") {
		c.println(envOr("AGENT_VERSION", "1.8.25-go"))
		c.exit(0)
	}
	cfg := c.agentConfig()
	kind := str(cfg, "service_type")
	c.log("agent-check " + kind + " " + c.args[len(c.args)-1])
	if c.has("-prepare-tun") {
		c.log("prepare-tun:" + kind + " " + strings.Join(c.args[2:], " "))
		if os.Getenv("FULL_TUN") != "yes" {
			c.fail("Gateway installation must not prepare TUN")
		}
		c.exit(0)
	}
	if kind == os.Getenv("FAIL_AGENT_CHECK") {
		c.exit(42)
	}
}

// gatewayManagerStub is systemctl / rc-service / rc-update of the gateway installer.
func gatewayManagerStub(c *stubCall) {
	root := os.Getenv("FAKE_ROOT")
	name, args := c.name, c.args
	c.log(name + " " + c.joined())
	activePath := filepath.Join(root, "active")
	active := map[string]bool{}
	for _, s := range strings.Fields(c.read(activePath)) {
		active[s] = true
	}
	saveActive := func() {
		var names []string
		for s := range active {
			names = append(names, s)
		}
		sort.Strings(names)
		c.write(activePath, strings.Join(names, " "))
	}
	service := ""
	if name == "systemctl" && len(args) > 1 {
		service = args[1]
	} else if len(args) > 0 {
		service = args[0]
	}
	service = strings.TrimSuffix(service, ".service")
	unit := filepath.Join(root, "etc/systemd/system", service+".service")
	if name == "systemctl" && args[0] == "show" {
		if c.has("--property=LoadState") {
			if fileExists(unit) {
				c.println("loaded")
			} else {
				c.println("not-found")
			}
		} else {
			command := ""
			for _, l := range strings.Split(c.read(unit), "\n") {
				if strings.HasPrefix(l, "ExecStart=") {
					command = l[10:]
					break
				}
			}
			c.println("{ path=" + strings.Fields(command)[0] + " ; argv[]=" + command + " ; }")
		}
		c.exit(0)
	}
	if name == "systemctl" && args[0] == "is-active" {
		if active[args[len(args)-1]] {
			c.exit(0)
		}
		c.exit(3)
	}
	action := ""
	if name == "systemctl" {
		action = args[0]
	} else if len(args) > 1 {
		action = args[1]
	}
	if name == "rc-service" && action == "status" {
		if active[service] {
			c.exit(0)
		}
		c.exit(3)
	}
	if action == "start" {
		if os.Getenv("FAIL_START") == service {
			c.exit(43)
		}
		active[service] = true
		saveActive()
		if kind, ok := strings.CutPrefix(service, "configflow-agent-"); ok {
			p := filepath.Join(root, "opt/configflow-agent", kind, "config.json")
			cfg := map[string]any{}
			c.readJSON(p, &cfg)
			cfg["agent_id"] = "registered-" + kind
			cfg["token"] = "agent-token-" + kind
			c.writeJSON(p, cfg, true)
		}
	}
	if action == "stop" {
		delete(active, service)
		saveActive()
	}
	if name == "systemctl" && action == "restart" && service == "systemd-resolved" &&
		fileExists(filepath.Join(root, "etc/systemd/resolved.conf.d/configflow.conf")) {
		c.write(filepath.Join(root, "resolved-stub-off"), "")
	}
}

// gatewayCurlStub is curl of the gateway installer (downloads and the
// completion report).
func gatewayCurlStub(c *stubCall) {
	url := c.args[len(c.args)-1]
	out := c.after("-o")
	assets := os.Getenv("ASSETS")
	c.log("download " + url)
	if c.has("--data-binary") {
		body := strings.TrimPrefix(c.after("--data-binary"), "@")
		headers := strings.TrimPrefix(c.after("-H"), "@")
		var data struct {
			Agents map[string]map[string]any `json:"agents"`
		}
		c.readJSON(body, &data)
		c.check(str(data.Agents["mihomo"], "token") == "agent-token-mihomo", "mihomo token")
		c.check(str(data.Agents["oxidns"], "id") == "registered-oxidns", "oxidns id")
		c.check(strings.HasPrefix(c.read(headers), "Authorization: Bearer session-token-"), "authorization header")
		c.write(out, `{"success":true}`)
		c.exit(0)
	}
	if os.Getenv("FAIL_GITHUB") == "yes" && strings.Contains(url, "github.com/") {
		c.exit(45)
	}
	src := "agent"
	switch {
	case strings.Contains(url, "mihomo-linux-"):
		src = "mihomo.gz"
	case strings.Contains(url, "oxidns-"):
		src = "oxidns.tar.gz"
	}
	if os.Getenv("FAIL_DOWNLOAD") == src {
		c.exit(44)
	}
	c.copyFile(filepath.Join(assets, src), out)
	if os.Getenv("CORRUPT") == src {
		c.write(out, "bad checksum")
	}
}

// gatewaySysctlStub is sysctl for full-TUN gateway installs.
func gatewaySysctlStub(c *stubCall) {
	c.log("sysctl " + c.joined())
	c.check(len(c.args) == 3 && c.args[0] == "-q" && c.args[1] == "-w" &&
		(c.args[2] == "net.ipv4.ip_forward=0" || c.args[2] == "net.ipv4.ip_forward=1"), "%q", c.args)
	c.write(os.Getenv("FAKE_ROOT")+"/ip_forward", c.args[2][len(c.args[2])-1:]+"\n")
}

// gatewayPackageStub is a gateway package manager; GATEWAY_PACKAGE_FAIL=1 fails installs.
func gatewayPackageStub(c *stubCall) {
	c.log(c.name + " " + c.joined())
	if c.argsAre("update") {
		c.exit(0)
	}
	if os.Getenv("GATEWAY_PACKAGE_FAIL") == "1" {
		c.exit(1)
	}
	root := os.Getenv("FAKE_ROOT")
	pending := filepath.Join(root, "pending")
	for _, p := range []struct {
		pkg   string
		tools []string
	}{{"nftables", []string{"nft"}}, {"iproute2", []string{"ss", "ip"}}, {"iproute", []string{"ss", "ip"}}} {
		if !c.has(p.pkg) {
			continue
		}
		for _, tool := range p.tools {
			if fileExists(filepath.Join(pending, tool)) {
				os.Rename(filepath.Join(pending, tool), filepath.Join(root, "commands", tool))
			}
		}
	}
}

// dependencyPackageStub is the package manager of the dependency-install tests.
func dependencyPackageStub(c *stubCall) {
	var frontend any
	if value, ok := os.LookupEnv("DEBIAN_FRONTEND"); ok {
		frontend = value
	}
	c.logJSON(os.Getenv("PACKAGE_EVENTS"), []any{c.name, c.args, frontend})
	updating := c.argsAre("update")
	if events := os.Getenv("EVENTS"); events != "" {
		if updating {
			c.appendFile(events, "package-update\n")
		} else {
			c.appendFile(events, "package-install\n")
		}
	}
	want := "install"
	if updating {
		want = "update"
	}
	if os.Getenv("FAIL_PACKAGE") == want {
		c.exit(41)
	}
	if updating || os.Getenv("PACKAGE_NO_COMMANDS") != "" {
		c.exit(0)
	}
	if c.anyArg("tzdata", "timezone") {
		zone := filepath.Join(os.Getenv("TZDIR"), "Asia/Shanghai")
		os.MkdirAll(filepath.Dir(zone), 0o755)
		c.write(zone, "TZif")
	}
	procps := "procps"
	if c.name == "pacman" || c.name == "dnf" || c.name == "yum" {
		procps = "procps-ng"
	}
	providers := map[string][]string{"awk": {"gawk"}, "grep": {"grep"}, "sed": {"sed"}, "wget": {"curl"},
		"sysctl": {procps}, "cmp": {"diffutils"}, "nft": {"nftables"}, "ss": {"iproute2", "iproute"},
		"tar": {"tar"}, "gzip": {"gzip"}}
	unavailable := os.Getenv("TEST_UNAVAILABLE")
	entries, err := testgolden.ReadDirOutside(unavailable)
	if err != nil {
		c.fail("%v", err)
	}
	for _, e := range entries {
		packages, ok := providers[e.Name()]
		if !ok {
			packages = []string{"coreutils"}
		}
		if c.anyArg(packages...) {
			os.Rename(filepath.Join(unavailable, e.Name()), filepath.Join(os.Getenv("TEST_COMMANDS"), e.Name()))
		}
	}
}

// l1ManagerStub is a stateful systemctl / rc-service / rc-update for the L1
// rollback tests.
func l1ManagerStub(c *stubCall) {
	path := os.Getenv("L1_MANAGER_STATE")
	var state map[string]any
	c.readJSON(path, &state)
	c.log(c.name + " " + c.joined())
	save := func() { c.writeJSON(path, state, false) }
	args := c.args
	var name, action string
	switch c.name {
	case "systemctl":
		action = args[0]
		if action == "show" {
			c.println("loaded")
			c.exit(0)
		}
		if action == "daemon-reload" {
			c.exit(0)
		}
		name = args[len(args)-1]
	case "rc-service":
		name, action = args[0], args[1]
	default:
		action = args[0]
		if action == "-u" {
			c.exit(0)
		}
		name = args[1]
	}
	services, _ := state["services"].(map[string]any)
	item, ok := services[name].(map[string]any)
	if !ok {
		c.exit(0)
	}
	flag := func(key string) bool { b, _ := item[key].(bool); return b }
	switch action {
	case "is-active", "status":
		if flag("active") {
			c.exit(0)
		}
		c.exit(3)
	case "is-enabled":
		if flag("enabled") {
			c.println("enabled")
			c.exit(0)
		}
		c.println("disabled")
		c.exit(1)
	case "enable", "add", "disable", "del":
		item["enabled"] = action == "enable" || action == "add"
		if c.name == "rc-update" {
			etc := os.Getenv("L1_ETC")
			link := filepath.Join(etc, "runlevels/default", name)
			os.MkdirAll(filepath.Dir(link), 0o755)
			os.Remove(link)
			if flag("enabled") {
				if err := os.Symlink(filepath.Join(etc, "init.d", name), link); err != nil {
					c.fail("%v", err)
				}
			}
		}
		save()
		c.exit(0)
	case "stop":
		item["active"] = false
		save()
		c.exit(0)
	case "start":
		binary, config := str(item, "binary"), str(item, "config")
		if !fileExists(binary) || !fileExists(config) {
			c.exit(4)
		}
		if name == os.Getenv("L1_FAIL_UNIT") && strings.Contains(c.read(binary), "L1_CANDIDATE") {
			item["active"] = false
			state["candidate_failed"] = true
			save()
			c.exit(49)
		}
		starts, _ := state["starts"].([]any)
		state["starts"] = append(starts, map[string]any{"unit": name, "binary": sha256File(c, binary), "config": sha256File(c, config)})
		item["active"] = true
		save()
		c.exit(0)
	}
	c.fail("unhandled manager operation %q", args)
}

// aioAgentStub is the fake Agent binary of the AIO installer.
func aioAgentStub(c *stubCall) {
	if c.argsAre("-h") || c.argsAre("-version") {
		c.exit(0)
	}
	configPath := c.after("-config")
	cfg := c.agentConfig()
	kind := str(cfg, "service_type")
	events := os.Getenv("EVENTS")
	c.log("agent " + kind + " " + c.args[len(c.args)-1] + " " + c.args[1])
	if c.has("-prepare-tun") {
		c.check(kind == "mihomo", "Only the Mihomo candidate prepares TUN")
		c.check(strings.Contains(configPath, ".aio-install.") && strings.HasSuffix(configPath, "/mihomo-config.json"), "candidate path")
		c.log("prepare-tun:" + kind + ":" + configPath)
		if os.Getenv("TUN_PREPARE_FAIL") == "yes" {
			c.eprintln("TUN 自动准备失败：请检查容器设备和 CAP_NET_ADMIN 权限。")
			c.exit(51)
		}
		c.exit(0)
	}
	if c.has("-check-environment") {
		if ready := os.Getenv("MISSING_TOOL_READY"); ready != "" {
			c.write(ready, "")
		}
		if lines := os.Getenv("AIO_AGENT_CHECK_PRINTS"); lines != "" {
			c.println(lines)
		}
		if os.Getenv("VERBOSE_ENVIRONMENT") == "yes" {
			c.println("verbose-environment-detail-for-log-only")
		}
		c.check(c.has("-environment-peer-config"), "AIO must inspect both services together")
		peerPath := c.after("-environment-peer-config")
		var peer map[string]any
		c.readJSON(peerPath, &peer)
		c.check(str(peer, "service_type") == "oxidns", "peer service_type")
		c.check(str(peer, "service_manager") == "systemd" || str(peer, "service_manager") == "openrc", "peer manager")
		c.check(str(cfg, "service_manager") == str(peer, "service_manager"), "same manager")
		c.check(fmt.Sprint(cfg["agent_port"]) != fmt.Sprint(peer["agent_port"]), "distinct ports")
		previous := c.read(events)
		c.check(strings.Contains(previous, "agent mihomo -validate-service"), "mihomo validated first")
		c.check(strings.Contains(previous, "agent oxidns -validate-service"), "oxidns validated first")
		c.log("environment peer oxidns " + peerPath)
		if os.Getenv("FAIL_ENVIRONMENT") == "yes" {
			c.eprintln("IPv4 转发未开启；修复建议：确认网关用途后配置持久化转发。")
			c.exit(48)
		}
	}
	if kind == os.Getenv("FAIL_VALIDATION") && c.has("-validate-service") {
		c.exit(46)
	}
	if kind == os.Getenv("FAIL_JOURNAL") && c.has("-require-clean-deployments") {
		c.exit(47)
	}
	if kind == os.Getenv("FAIL_JOURNAL_AFTER_STOP") && c.has("-require-clean-deployments") && strings.Contains(c.read(events), "stop") {
		c.exit(47)
	}
}

func (c *stubCall) forwardWrite() (value string) {
	if !(c.argsAre("-q", "-w", "net.ipv4.ip_forward=0") || c.argsAre("-q", "-w", "net.ipv4.ip_forward=1")) {
		c.fail("Unexpected sysctl command in isolated installer test: %q", c.args)
	}
	_, value, _ = strings.Cut(c.args[2], "=")
	return value
}

// aioSysctlStub is sysctl of the AIO installer.
func aioSysctlStub(c *stubCall) {
	c.log("sysctl " + c.joined())
	current := os.Getenv("FORWARD_PROC")
	if c.argsAre("-n", "net.ipv4.ip_forward") || c.argsAre("net.ipv4.ip_forward") {
		value := strings.TrimSpace(c.read(current))
		if c.has("-n") {
			c.println(value)
		} else {
			c.println("net.ipv4.ip_forward = " + value)
		}
		c.exit(0)
	}
	value := c.forwardWrite()
	if os.Getenv("FAIL_SYSCTL_WRITES") == "yes" || (value == "1" && os.Getenv("FAIL_SYSCTL_ENABLE") == "yes") {
		c.eprintln("sysctl: permission denied on key net.ipv4.ip_forward")
		c.exit(50)
	}
	if os.Getenv("SYSCTL_WRITE_NO_EFFECT") != "yes" {
		c.write(current, value+"\n")
	}
	if value == "1" && os.Getenv("DELETE_FORWARD_CONFIG_ON_ENABLE") == "yes" {
		if err := os.Remove(os.Getenv("FORWARD_CONFIG")); err != nil && !errors.Is(err, os.ErrNotExist) {
			c.fail("%v", err)
		}
	}
}

// standaloneSysctlStub is sysctl of the single-service forwarding installer.
func standaloneSysctlStub(c *stubCall) {
	c.log("sysctl " + c.joined())
	value := c.forwardWrite()
	if os.Getenv("FAIL_SYSCTL_WRITES") == "yes" {
		c.eprintln("sysctl: permission denied on key net.ipv4.ip_forward")
		c.exit(50)
	}
	c.write(os.Getenv("FORWARD_PROC"), value+"\n")
}

// updateCandidateStub is the new Agent binary of the offline update package.
func updateCandidateStub(c *stubCall) {
	switch {
	case c.has("-version"):
		c.println("1.8.31-go")
	case c.has("-check-environment"):
		c.println("FULL-ENVIRONMENT-EVIDENCE: interface Meta belongs to PID 42")
		report := os.NewFile(3, "report")
		if report == nil {
			c.fail("report descriptor 3 is not open")
		}
		if _, err := report.Write([]byte("[通过] TUN：Meta · 已确认核心持有\n")); err != nil {
			c.fail("report descriptor 3: %v", err)
		}
		if os.Getenv("ENVIRONMENT_FAIL") != "" {
			c.eprintln("environment-specific-failure")
			c.exit(7)
		}
	default:
		c.println("FULL-DEPLOYMENT-EVIDENCE: no pending journals")
	}
}

// updateSystemctlStub is systemctl of the offline update tests; UPDATE_BINARY
// and UPDATE_AGENT_CONFIG are baked into the wrapper.
func updateSystemctlStub(c *stubCall) {
	statePath := os.Getenv("STATE")
	var state map[string]any
	c.readJSON(statePath, &state)
	args := c.args
	unit := ""
	for _, a := range args {
		if strings.HasSuffix(a, ".service") {
			unit = a
			break
		}
	}
	const agentUnit = "configflow-agent-mihomo.service"
	binary := os.Getenv("UPDATE_BINARY")
	running, _ := state["running"].(bool)
	switch args[0] {
	case "stop", "start", "restart":
		c.log(c.joined())
		c.check(unit == agentUnit, "core lifecycle was invoked")
		if args[0] == "start" && os.Getenv("START_FAIL") != "" && strings.Contains(c.read(binary), "1.8.31-go") {
			c.eprintln("start-specific-failure")
			c.exit(1)
		}
		state["running"] = args[0] != "stop"
		if args[0] == "stop" && os.Getenv("CORE_CHANGED") != "" {
			state["core_pid"] = 43
		}
		c.writeJSON(statePath, state, false)
		c.println("RAW-SERVICE-DIAGNOSTIC: " + args[0])
	case "is-active":
		if unit != agentUnit || running {
			c.exit(0)
		}
		c.exit(3)
	case "show":
		prop := ""
		for _, a := range args {
			if v, ok := strings.CutPrefix(a, "--property="); ok {
				prop = v
				break
			}
		}
		mainPID := fmt.Sprint(state["core_pid"])
		if unit == agentUnit {
			mainPID = "0"
			if running {
				mainPID = "84"
			}
		}
		values := map[string]string{
			"LoadState":    "loaded",
			"ExecStart":    "{ path=" + binary + " ; argv[]=" + binary + " -config " + os.Getenv("UPDATE_AGENT_CONFIG") + " ; ignore_errors=no ; }",
			"ControlGroup": "/system.slice/" + unit,
			"MainPID":      mainPID,
		}
		if os.Getenv("UNKNOWN_HOOK") != "" {
			values["ExecStop"] = "custom-hook"
		}
		c.println(values[prop])
	default:
		c.fail("assertion failed: %q", args)
	}
}

// haSystemctlStub is systemctl of the DNS HA preparation tests.
func haSystemctlStub(c *stubCall) {
	root := os.Getenv("HA_ROOT")
	c.logJSON(os.Getenv("HA_EVENTS"), []any{"systemctl", c.args})
	mask := filepath.Join(root, "run/systemd/system/keepalived.service")
	state := filepath.Join(root, "state")
	switch c.args[0] {
	case "show":
		switch {
		case isSymlink(mask):
			c.println("masked")
		case fileExists(filepath.Join(state, "unit")):
			c.println("loaded")
		default:
			c.println("not-found")
		}
	case "is-active":
		if fileExists(filepath.Join(state, "active")) {
			c.exit(0)
		}
		c.exit(3)
	case "is-enabled":
		if fileExists(filepath.Join(state, "enabled")) {
			c.println("enabled")
			c.exit(0)
		}
		c.println("disabled")
		c.exit(1)
	case "mask":
		if err := os.Symlink("/dev/null", mask); err != nil {
			c.fail("%v", err)
		}
		if os.Getenv("MASK_PARTIAL_FAILURE") != "" {
			c.exit(8)
		}
	case "disable":
		os.Remove(filepath.Join(state, "enabled"))
	}
}

// haPackageStub is the package manager of the DNS HA preparation tests.
func haPackageStub(c *stubCall) {
	root, commands := os.Getenv("HA_ROOT"), os.Getenv("HA_COMMANDS")
	c.logJSON(os.Getenv("HA_EVENTS"), []any{c.name, c.args})
	if c.argsAre("update") {
		c.exit(0)
	}
	if os.Getenv("FAIL_PACKAGE") == "before" {
		c.exit(43)
	}
	executable := func(path, text string) {
		if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
			c.fail("%v", err)
		}
		os.Chmod(path, 0o755)
	}
	if c.has("keepalived") {
		executable(filepath.Join(commands, "keepalived"), "#!/bin/sh\nexit 0\n")
		c.write(filepath.Join(root, "state/unit"), "")
		c.write(filepath.Join(root, "state/enabled"), "")
		c.write(filepath.Join(root, "etc/keepalived/keepalived.conf"), "vrrp_instance SAMPLE { virtual_router_id 51 }\n")
		if c.name != "apk" && !isSymlink(filepath.Join(root, "run/systemd/system/keepalived.service")) {
			c.write(filepath.Join(root, "state/unexpected-start"), "")
		}
		if os.Getenv("FAIL_PACKAGE") == "after-keepalived" {
			c.exit(44)
		}
	}
	if c.has("keepalived-openrc") {
		executable(filepath.Join(root, "etc/init.d/keepalived"), "#!/bin/sh\n")
	}
	if c.has("iproute") || c.has("iproute2") {
		executable(filepath.Join(commands, "ip"), "#!/bin/sh\nprintf \"[]\\n\"\n")
	}
}

// dockerLogStub records every docker invocation as a JSON line.
func dockerLogStub(c *stubCall) { c.logJSON(os.Getenv("DOCKER_EVENTS"), c.args) }

// preflightDockerStub is docker for the HA preflight collector tests.
func preflightDockerStub(c *stubCall) {
	args := c.args
	if events := os.Getenv("CF_TEST_DOCKER_EVENTS"); events != "" {
		c.logJSON(events, args)
	}
	switch {
	case len(args) >= 2 && args[0] == "context" && args[1] == "show":
		c.println("default")
	case len(args) >= 2 && args[0] == "context" && args[1] == "inspect":
		if args[2] == "remote-context" {
			c.println("ssh://private-user:secret@remote")
		} else {
			c.println("unix:///var/run/docker.sock")
		}
	case args[0] == "version":
		if os.Getenv("CF_TEST_DAEMON_FAIL") != "" {
			c.eprintln("Cannot connect to the Docker daemon")
			c.exit(1)
		}
		c.println(`{"client":"29.9.2","server":"29.9.2"}`)
	case args[0] == "compose":
		c.println("2.30.3")
	case args[0] == "info":
		if os.Getenv("CF_TEST_DAEMON_FAIL") != "" {
			c.exit(1)
		}
		switch format := args[len(args)-1]; {
		case format == "{{.OSType}}":
			c.println("linux")
		case strings.Contains(format, ".SecurityOptions"):
			c.println(`["name=seccomp"]`)
		default:
			c.println(`{"os":"Arch Linux","kernel":"6.18.3"}`)
		}
	default:
		c.exit(2)
	}
}

// guideDockerStub is the stateful docker of the HA guide (migration) tests.
func guideDockerStub(c *stubCall) {
	args := c.args
	path := os.Getenv("FAKE_STATE")
	var state map[string]any
	c.readJSON(path, &state)
	c.logJSON(os.Getenv("FAKE_EVENTS"), args)
	save := func() { c.writeJSON(path, state, false) }
	containers, _ := state["containers"].(map[string]any)
	truthy := func(key string) bool {
		switch v := state[key].(type) {
		case nil:
			return false
		case bool:
			return v
		case string:
			return v != ""
		case float64:
			return v != 0
		default:
			return true
		}
	}
	lookup := func(value string) (string, map[string]any) {
		for id, raw := range containers {
			item, _ := raw.(map[string]any)
			if value == id || value == str(item, "name") {
				return id, item
			}
		}
		c.exit(1)
		return "", nil
	}
	labelValue := func() string {
		_, v, _ := strings.Cut(c.after("--label"), "=")
		return v
	}
	switch args[0] {
	case "info":
		if strings.Contains(args[len(args)-1], ".OSType") {
			c.println("linux")
		} else {
			c.println("[]")
		}
	case "image":
		if strings.Contains(c.joined(), ".Config.") {
			c.println("[]")
		} else {
			c.println("{}")
		}
	case "network":
		network, _ := state["network"].(map[string]any)
		switch {
		case args[1] == "create":
			_, parent, _ := strings.Cut(c.after("--opt"), "=")
			gateway := ""
			if c.has("--gateway") {
				gateway = c.after("--gateway")
			}
			state["network"] = map[string]any{"name": args[len(args)-1], "owner": labelValue(), "parent": parent,
				"subnet": c.after("--subnet"), "gateway": gateway}
			save()
			c.println("network-id")
		case network == nil:
			c.exit(1)
		case c.has("--format"):
			format := c.after("--format")
			switch {
			case strings.Contains(format, ".Driver"):
				c.println("macvlan")
			case strings.Contains(format, ".Options"):
				c.println(str(network, "parent"))
			case strings.Contains(format, ".Labels"):
				c.println(str(network, "owner"))
			case strings.Contains(format, "len "):
				c.println("1")
			case strings.Contains(format, ".Subnet"):
				c.println(str(network, "subnet"))
			default:
				c.println(str(network, "gateway"))
			}
		default:
			c.println("{}")
		}
	case "inspect", "container":
		if args[0] == "container" {
			args = args[1:]
			c.args = args
		}
		id, item := lookup(args[len(args)-1])
		format := ""
		if c.has("--format") {
			format = c.after("--format")
		}
		switch {
		case strings.Contains(format, ".Id"):
			c.println(id)
		case strings.Contains(format, ".Name") && !strings.Contains(format, ".Type") && !strings.Contains(format, ".RestartPolicy"):
			c.println("/" + str(item, "name"))
		case strings.Contains(format, ".State.Running"):
			running, _ := item["running"].(bool)
			c.println(strconv.FormatBool(running))
		case strings.Contains(format, "len .Mounts"):
			if n, ok := state["mount_count"]; ok {
				c.println(fmt.Sprint(n))
			} else {
				c.println("2")
			}
		case strings.Contains(format, ".Type"):
			key := "core_mount"
			if strings.Contains(format, "/opt/configflow-agent") {
				key = "agent_mount"
			}
			mount, _ := state[key].(map[string]any)
			c.println(str(mount, "type") + "|" + str(mount, "source") + "|true")
		case strings.Contains(format, ".Config.Labels"):
			c.println(str(item, "owner"))
		case strings.Contains(format, ".Config."):
			c.println("[]")
		case strings.Contains(format, ".Image"):
			c.println("original-image")
		case strings.Contains(format, ".RestartPolicy"):
			c.println("always:0")
		default:
			c.println("{}")
		}
	case "exec":
		if len(args) >= 4 && strings.Join(args[len(args)-4:], " ") == "configflow-agent svc status oxidns" {
			if truthy("fatal") {
				c.println("oxidns FATAL")
				c.exit(3)
			}
			c.println("oxidns RUNNING pid 123, uptime 0:00:10")
			c.exit(0)
		}
		c.fail("unexpected fake Docker exec: %q", args)
	case "update":
		_, item := lookup(args[len(args)-1])
		for _, a := range args {
			if v, ok := strings.CutPrefix(a, "--restart="); ok {
				item["restart"] = v
				break
			}
		}
		save()
	case "start", "stop":
		id, item := lookup(args[len(args)-1])
		item["running"] = args[0] == "start"
		save()
		c.println(id)
	case "rename":
		_, item := lookup(args[1])
		item["name"] = args[2]
		save()
	case "cp":
	case "run":
		if c.has("-version") {
			// The ha-* capability probe of the selected Agent image.
			if truthy("old_agent") {
				c.println("1.8.61-go")
			} else {
				c.println("1.9.3-go")
			}
			c.exit(0)
		}
		if c.has("ha-migration-check") {
			// The migration check of the old container, run by the target image.
			io.Copy(io.Discard, os.Stdin)
			if truthy("reject_existing") {
				c.exit(1)
			}
			c.println("HEARTBEAT_INTERVAL=42\nDEPLOYMENT_HEALTH_TIMEOUT=30\nAGENT_HOST=0.0.0.0")
			c.exit(0)
		}
		// Probe only; no installation state is changed.
		if c.has("--network") && c.after("--network") == "host" && c.has("--rm") && truthy("port_busy") {
			c.eprintln("宿主机的 UDP/TCP 53 端口不可用")
			c.exit(1)
		}
		if c.has("--rm") {
			if truthy("ha_journal") {
				c.exit(1)
			}
			c.exit(0)
		}
		name := c.after("--name")
		id := strings.Repeat("c", 64)
		if strings.HasSuffix(name, "-ha") {
			id = strings.Repeat("b", 64)
		}
		if truthy("fail_before_main") && !strings.HasSuffix(name, "-ha") {
			if c.has("--cidfile") {
				c.write(c.after("--cidfile"), "")
			}
			c.exit(123)
		}
		owner := ""
		if c.has("--label") {
			owner = labelValue()
		}
		item := map[string]any{"name": name, "running": true, "owner": owner}
		containers[id] = item
		save()
		if c.has("--cidfile") {
			c.write(c.after("--cidfile"), id+"\n")
		}
		if truthy("fail_main") && !strings.HasSuffix(name, "-ha") {
			item["running"] = false
			save()
			c.exit(123)
		}
		c.println(id)
	default:
		c.fail("unexpected fake Docker command: %q", args)
	}
}

// composeDockerStub is the read-only docker of the compose bundle tests: any
// mutating operation fails.
func composeDockerStub(c *stubCall) {
	args := c.args
	var state map[string]any
	c.readJSON(os.Getenv("CF_COMPOSE_STATE"), &state)
	c.logJSON(os.Getenv("CF_COMPOSE_EVENTS"), args)
	format := ""
	if c.has("--format") {
		format = c.after("--format")
	}
	between := func(prefix string) string {
		_, rest, ok := strings.Cut(format, prefix+` "`)
		if !ok {
			c.fail("no %s in format %q", prefix, format)
		}
		value, _, _ := strings.Cut(rest, `"`)
		return value
	}
	labels := func(item map[string]any) string {
		values, _ := item["labels"].(map[string]any)
		return str(values, between("Labels"))
	}
	two := func(a, b string) bool { return len(args) >= 2 && args[0] == a && args[1] == b }
	switch {
	case two("context", "show"):
		c.println("default")
	case two("context", "inspect"):
		c.println(envOrState(state, "endpoint", "unix:///var/run/docker.sock"))
	case args[0] == "info":
		switch f := args[len(args)-1]; {
		case strings.Contains(f, ".OSType"):
			c.println("linux")
		case strings.Contains(f, ".SecurityOptions"):
			c.println("[]")
		default:
			c.println("Arch Linux 6.18")
		}
	case two("compose", "version"):
		c.println(envOrState(state, "version", "2.20.1"))
	case two("container", "inspect"):
		containers, _ := state["containers"].(map[string]any)
		item, ok := containers[args[len(args)-1]].(map[string]any)
		if !ok {
			c.exit(1)
		}
		switch {
		case strings.Contains(format, ".Labels"):
			c.println(labels(item))
		case strings.Contains(format, ".Mounts"):
			target := between(".Destination")
			volumes, _ := item["volumes"].([]any)
			line := ""
			for _, raw := range volumes {
				m, _ := raw.(map[string]any)
				if str(m, "target") == target {
					readOnly, _ := m["read_only"].(bool)
					line = "bind|" + str(m, "source") + "|" + strconv.FormatBool(!readOnly)
					break
				}
			}
			c.println(line)
		default:
			c.println("{}")
		}
	case two("network", "inspect"):
		networks, _ := state["networks"].(map[string]any)
		item, ok := networks[args[len(args)-1]].(map[string]any)
		if !ok {
			c.exit(1)
		}
		ipam := func() map[string]any {
			i, _ := item["ipam"].(map[string]any)
			configs, _ := i["config"].([]any)
			first, _ := configs[0].(map[string]any)
			return first
		}
		switch {
		case strings.Contains(format, ".Driver"):
			c.println(str(item, "driver"))
		case strings.Contains(format, ".Options"):
			opts, _ := item["driver_opts"].(map[string]any)
			c.println(str(opts, "parent"))
		case strings.Contains(format, ".Labels"):
			c.println(labels(item))
		case strings.Contains(format, "len .IPAM"):
			c.println("1")
		case strings.Contains(format, ".Subnet"):
			c.println(str(ipam(), "subnet"))
		case strings.Contains(format, ".Gateway"):
			c.println(str(ipam(), "gateway"))
		default:
			c.println("{}")
		}
	default:
		c.fail("unexpected mutating Docker operation %q", args)
	}
}

func envOrState(state map[string]any, key, fallback string) string {
	if v, ok := state[key].(string); ok {
		return v
	}
	return fallback
}
