package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// Port of backend/test_core_install_script.py: the standalone core installer
// with real archives and checksum checks, isolated init, files, /proc, /dev
// and /sys views, and stub commands on a PATH of its own.

type coreOpts struct {
	manager        string // systemd (default), openrc, none
	tunDevice      string // ready (default), missing, regular, denied
	capStatus      string
	capNone        bool
	forwarding     string
	forwardingNone bool
	existingTun    bool
	noTZ           bool
	noStat, noDig  bool
	downloadTool   string
	packageManager string
	missing        []string
	settings       []string
}

type coreHarness struct {
	tmp, installer, root, events string
	env                          env
}

type coreResult struct {
	runResult
	logPath, detail string
}

func (h *coreHarness) eventsText(t *testing.T) string { return eventsText(t, h.events) }

func (h *coreHarness) path(rel string) string { return filepath.Join(h.root, rel) }

// rootPathPattern is (?<![A-Za-z0-9_])/(etc|run|usr|lib|sbin|proc|dev|sys)/.
func rootPaths(source, root string) string {
	var out strings.Builder
	for i := 0; i < len(source); {
		if source[i] == '/' && (i == 0 || !isWordByte(source[i-1])) {
			matched := false
			for _, name := range []string{"etc", "run", "usr", "lib", "sbin", "proc", "dev", "sys"} {
				if strings.HasPrefix(source[i+1:], name+"/") {
					out.WriteString(root + "/" + name + "/")
					i += len(name) + 2
					matched = true
					break
				}
			}
			if matched {
				continue
			}
		}
		out.WriteByte(source[i])
		i++
	}
	return out.String()
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

const coreServiceBody = `name = os.path.basename(sys.argv[0])
args = sys.argv[1:]
with open(os.environ['EVENTS'], 'a') as log: log.write(name + ' ' + ' '.join(args) + '\n')
if args[0] == 'show':
    print('12345' if '--property=MainPID' in args else ('loaded' if os.environ.get('EXISTING_SERVICE') == args[1] else 'not-found'))
    raise SystemExit(0)
action = args[0] if name != 'rc-service' else args[1]
service = args[-1] if name == 'systemctl' else (args[0] if name == 'rc-service' else args[1])
if name == 'systemctl' and service == 'systemd-resolved':
    if action == 'is-active':
        raise SystemExit(0 if os.environ.get('RESOLVED_ACTIVE') else 3)
    if action == 'restart':
        # The stub listener is gone once the drop-in disables it.
        root = os.environ['ROOT']
        if os.path.exists(os.path.join(root, 'etc/systemd/resolved.conf.d/configflow.conf')):
            for network in ('tcp', 'udp', 'tcp6', 'udp6'):
                path = os.path.join(root, 'proc/net', network)
                lines = open(path).read().splitlines(True)
                open(path, 'w').write(''.join(line for line in lines if '3500007F:0035' not in line and '3600007F:0035' not in line))
    raise SystemExit(0)
if os.environ.get('FAIL_SERVICE') == action + ':' + service or os.environ.get('FAIL_ROLLBACK') == action + ':' + service:
    if name == 'rc-service' and action == 'start':
        with open(os.path.join(os.environ['ROOT'], 'etc', service, service + '.log'), 'w') as log:
            log.write('core startup error: fixture failure\n')
    raise SystemExit(38)
if name != 'rc-update' and service == 'mihomo':
    from pathlib import Path
    import shutil
    root = Path(os.environ['ROOT'])
    interface = root / 'sys/class/net/configflow-tun'
    fdinfo = root / 'proc/12345/fdinfo'
    if action == 'start':
        counter = root / 'run/mihomo-start-count'
        count = int(counter.read_text()) + 1 if counter.exists() else 1
        counter.write_text(str(count))
        if count > 1 and os.environ.get('FAIL_FALLBACK_START'): raise SystemExit(38)
        (root / 'run/mihomo.pid').write_text('12345')
        enabled = 'tun:\n  enable: true\n' in (root / 'etc/mihomo/config.yaml').read_text()
        if enabled and not os.environ.get('NO_TUN_INTERFACE') and not os.environ.get('LATE_TUN_READY'):
            interface.mkdir(parents=True, exist_ok=True)
            (interface / 'tun_flags').write_text(os.environ.get('TUN_FLAGS', '0x1001') + '\n')
            (interface / 'flags').write_text(os.environ.get('TUN_LINK_FLAGS', '0x1001') + '\n')
            fdinfo.mkdir(parents=True, exist_ok=True)
            (fdinfo / '8').write_text('iff:\t' + os.environ.get('TUN_OWNER_DEVICE', 'configflow-tun') + '\n')
    elif action == 'stop':
        shutil.rmtree(interface, ignore_errors=True)
        shutil.rmtree(fdinfo, ignore_errors=True)
`

const corePackageBody = `from pathlib import Path
import shutil
name = os.path.basename(sys.argv[0])
with open(os.environ['EVENTS'], 'a') as log:
    log.write('package ' + name + ' ' + ' '.join(sys.argv[1:]) + ' DEBIAN_FRONTEND=' + os.environ.get('DEBIAN_FRONTEND', '') + '\n')
updating = sys.argv[1:] == ['update']
if os.environ.get('FAIL_PACKAGE') == ('update' if updating else 'install'): raise SystemExit(41)
if updating or os.environ.get('PACKAGE_NO_COMMANDS'): raise SystemExit(0)
commands = Path(os.environ['PATH'])
if 'tzdata' in sys.argv or 'timezone' in sys.argv:
    zone = Path(os.environ['ROOT']) / 'usr/share/zoneinfo/Asia/Shanghai'
    zone.parent.mkdir(parents=True, exist_ok=True)
    zone.write_text('TZif')
for executable in (Path(os.environ['FIXTURE']) / 'unavailable-commands').iterdir():
    # Restore the real fixture command only after a successful fake transaction.
    shutil.move(str(executable), commands / executable.name)
`

// newCoreHarness is harness(tmp_path, manager, **options).
func newCoreHarness(t *testing.T, o coreOpts) *coreHarness {
	if o.manager == "" {
		o.manager = "systemd"
	}
	tmp := t.TempDir()
	python := pythonExe(t)
	root := filepath.Join(tmp, "root")
	commands := filepath.Join(tmp, "commands")
	os.MkdirAll(commands, 0o755)
	for _, dir := range []string{"run", "etc/systemd/system", "etc/init.d", "usr/local/bin", "sbin", "proc/net", "proc/self", "proc/sys/net/ipv4", "dev/net", "sys/class/net"} {
		os.MkdirAll(filepath.Join(root, dir), 0o755)
	}
	switch o.manager {
	case "systemd":
		os.MkdirAll(filepath.Join(root, "run/systemd/system"), 0o755)
	case "openrc":
		writeFile(t, filepath.Join(root, "sbin/openrc-run"), "#!/bin/sh\n", 0o755)
	}
	for _, network := range []string{"tcp", "udp", "tcp6", "udp6"} {
		writeFile(t, filepath.Join(root, "proc/net", network), "sl local_address rem_address st\n", 0o644)
	}
	switch o.tunDevice {
	case "regular":
		writeFile(t, filepath.Join(root, "dev/net/tun"), "not a device", 0o644)
	case "missing":
	case "denied":
		os.Symlink("/dev/tty", filepath.Join(root, "dev/net/tun"))
	default:
		os.Symlink("/dev/null", filepath.Join(root, "dev/net/tun"))
	}
	if !o.capNone {
		status := o.capStatus
		if status == "" {
			status = "CapEff:\t0000000000001000\nCapBnd:\t0000000000001000\n"
		}
		writeFile(t, filepath.Join(root, "proc/self/status"), status, 0o644)
	}
	if !o.forwardingNone {
		value := o.forwarding
		if value == "" {
			value = "1"
		}
		writeFile(t, filepath.Join(root, "proc/sys/net/ipv4/ip_forward"), value+"\n", 0o644)
	}
	if o.existingTun {
		os.MkdirAll(filepath.Join(root, "sys/class/net/configflow-tun"), 0o755)
	}
	if !o.noTZ {
		writeFile(t, filepath.Join(root, "usr/share/zoneinfo/Asia/Shanghai"), "TZif", 0o644)
	}
	events := writeFile(t, filepath.Join(tmp, "events"), "", 0o644)
	fakeCore := []byte("#!" + python + ` -S
import os, sys
from pathlib import Path
name = 'mihomo' if '-t' in sys.argv else 'oxidns'
with open(os.environ['EVENTS'], 'a') as log: log.write('validate ' + name + '\n')
config = Path(sys.argv[sys.argv.index('-f' if name == 'mihomo' else '-c') + 1])
assert config.is_file()
if os.environ.get('FAIL_VALIDATE') == name: raise SystemExit(29)
`)
	var mihomo, oxidns bytes.Buffer
	gz := gzip.NewWriter(&mihomo)
	gz.Write(fakeCore)
	gz.Close()
	gz = gzip.NewWriter(&oxidns)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "oxidns", Mode: 0o644, Size: int64(len(fakeCore)), Typeflag: tar.TypeReg})
	tw.Write(fakeCore)
	tw.Close()
	gz.Close()
	writeFile(t, filepath.Join(tmp, "mihomo.gz"), mihomo.String(), 0o644)
	writeFile(t, filepath.Join(tmp, "oxidns.tar.gz"), oxidns.String(), 0o644)
	source := ok(GenerateCoreInstallScript(false))
	source = regexp.MustCompile(`MIHOMO_SHA=[0-9a-f]{64}`).ReplaceAllLiteralString(source, "MIHOMO_SHA="+sha256Hex(mihomo.String()))
	source = regexp.MustCompile(`OXIDNS_SHA=[0-9a-f]{64}`).ReplaceAllLiteralString(source, "OXIDNS_SHA="+sha256Hex(oxidns.String()))
	source = rootPaths(source, root)
	source = strings.ReplaceAll(source, "in /run /etc ", "in "+root+"/run "+root+"/etc ")
	installer := writeFile(t, filepath.Join(tmp, "install.sh"), source, 0o644)
	bodies := map[string]string{
		"id": "#!/bin/sh\nprintf '%s\\n' \"${FAKE_UID-0}\"\n",
		"uname": `#!/bin/sh
set -eu
if [ "$1" = -s ]; then printf '%s\n' "${OS_NAME-Linux}"; else printf '%s\n' "${ARCH-x86_64}"; fi
`,
		"sleep": `#!/bin/sh
set -eu
if [ -n "${LATE_TUN_READY-}" ] && [ "$1" = 1 ]; then
    counter="$ROOT/run/tun-wait-count"
    count=0
    if [ -f "$counter" ]; then IFS= read -r count < "$counter" || [ -n "$count" ]; fi
    count=$((count + 1))
    printf '%s' "$count" > "$counter"
    if [ "$count" -ge "$LATE_TUN_READY" ]; then
        interface="$ROOT/sys/class/net/configflow-tun"
        fdinfo="$ROOT/proc/12345/fdinfo"
        mkdir -p "$interface" "$fdinfo"
        printf '0x1001' > "$interface/tun_flags"
        printf '0x1001' > "$interface/flags"
        printf 'iff: configflow-tun' > "$fdinfo/8"
    fi
fi
`,
		"stat": "#!/bin/sh\nprintf '%s\\n' \"${DEVICE_NUMBER-a:c8}\"\n",
		"curl": `import shutil
with open(os.environ['EVENTS'], 'a') as log: log.write('download ' + sys.argv[-1] + '\n')
if os.environ.get('FAIL_DOWNLOAD'): raise SystemExit(22)
source = 'mihomo.gz' if 'MetaCubeX' in sys.argv[-1] else 'oxidns.tar.gz'
shutil.copyfile(os.path.join(os.environ['FIXTURE'], source), sys.argv[sys.argv.index('--output') + 1])
if os.environ.get('BAD_CHECKSUM') == source:
    with open(sys.argv[sys.argv.index('--output') + 1], 'ab') as file: file.write(b'bad')
`,
		"sha256sum": `import hashlib
if sys.argv[1] == '-c':
    for line in open(sys.argv[2]):
        digest, file = line.rstrip().split('  ', 1)
        if hashlib.sha256(open(file, 'rb').read()).hexdigest() != digest:
            print(file + ': FAILED'); raise SystemExit(1)
        print(file + ': OK')
else:
    for file in sys.argv[1:]: print(hashlib.sha256(open(file, 'rb').read()).hexdigest() + '  ' + file)
`,
	}
	if o.manager == "systemd" {
		bodies["systemctl"] = coreServiceBody
	} else {
		bodies["rc-service"] = coreServiceBody
		bodies["rc-update"] = coreServiceBody
	}
	bodies["dig"] = `with open(os.environ['EVENTS'], 'a') as log: log.write('dig ' + ' '.join(sys.argv[1:]) + '\n')
if os.environ.get('FAIL_DIG') == 'exit': raise SystemExit(9)
if os.environ.get('FAIL_DIG') == 'servfail':
    print(';; ->>HEADER<<- opcode: QUERY, status: SERVFAIL, id: 17')
    print(';; flags: qr rd ra; QUERY: 1, ANSWER: 0, AUTHORITY: 0, ADDITIONAL: 0')
else:
    print(';; ->>HEADER<<- opcode: QUERY, status: NOERROR, id: 17')
    print(';; flags: qr rd ra; QUERY: 1, ANSWER: 1, AUTHORITY: 0, ADDITIONAL: 0')
`
	if o.noStat {
		delete(bodies, "stat")
	}
	if o.noDig {
		delete(bodies, "dig")
	}
	if o.downloadTool == "wget" {
		bodies["wget"] = strings.ReplaceAll(bodies["curl"], "'--output'", "'-O'")
		delete(bodies, "curl")
	}
	if o.packageManager != "" {
		bodies[o.packageManager] = corePackageBody
	}
	for _, name := range []string{"sysctl", "ip", "iptables", "nft", "modprobe", "pct"} {
		bodies[name] = "with open(os.environ['EVENTS'], 'a') as log: log.write('FORBIDDEN ' + os.path.basename(sys.argv[0]) + '\\n')\nraise SystemExit(99)"
	}
	for name, body := range bodies {
		if !strings.HasPrefix(body, "#!") {
			body = "#!" + python + " -S\nimport os, sys\n" + body + "\n"
		}
		writeFile(t, filepath.Join(commands, name), body, 0o755)
	}
	for _, name := range []string{"awk", "cat", "chmod", "cp", "gzip", "mkdir", "mktemp", "mv", "rm", "rmdir", "sed", "sleep", "tail", "tar", "sh"} {
		if !exists(filepath.Join(commands, name)) {
			real, err := exec.LookPath(name)
			if err != nil {
				t.Fatal(err)
			}
			os.Symlink(real, filepath.Join(commands, name))
		}
	}
	unavailable := filepath.Join(tmp, "unavailable-commands")
	os.MkdirAll(unavailable, 0o755)
	for _, name := range o.missing {
		if err := os.Rename(filepath.Join(commands, name), filepath.Join(unavailable, name)); err != nil {
			t.Fatal(err)
		}
	}
	e := baseEnv().without("DEBIAN_FRONTEND", "TZDIR").with("PATH="+commands, "EVENTS="+events, "FIXTURE="+tmp,
		"ROOT="+root, "CONFIGFLOW_INSTALL_LOG_DIR="+tmp).with(o.settings...)
	return &coreHarness{tmp: tmp, installer: installer, root: root, events: events, env: e}
}

var coreLogPath = regexp.MustCompile(`完整日志[：:]\s*([^\r\n]+)`)

func (h *coreHarness) run(t *testing.T, args ...string) coreResult {
	r := runShell(t, "sh", h.installer, runOptions{args: args, env: h.env, replace: true, timeout: 30e9, setsid: true})
	out := coreResult{runResult: r}
	if m := coreLogPath.FindStringSubmatch(r.stdout + r.stderr); m != nil {
		out.logPath = strings.TrimSpace(m[1])
	}
	if data, err := os.ReadFile(out.logPath); out.logPath != "" && err == nil {
		out.detail = string(data)
	} else {
		out.detail = r.stdout + r.stderr
	}
	return out
}

func (h *coreHarness) assertUninstalled(t *testing.T) {
	t.Helper()
	for _, service := range []string{"mihomo", "oxidns"} {
		for _, rel := range []string{"usr/local/bin/" + service, "etc/" + service, "etc/systemd/system/" + service + ".service", "etc/init.d/" + service} {
			if exists(h.path(rel)) {
				t.Errorf("%s exists", rel)
			}
		}
	}
	if exists(h.path("run/configflow-core-install.lock")) {
		t.Error("lock left")
	}
}

// seedExistingCore is seed_existing_core: an externally managed install.
func (h *coreHarness) seedExistingCore(t *testing.T, service, manager string) map[string]string {
	unit := "etc/init.d/" + service
	if manager == "systemd" {
		unit = "etc/systemd/system/" + service + ".service"
	}
	contents := map[string]string{
		"usr/local/bin/" + service:        "existing core executable\n",
		"etc/" + service + "/config.yaml": "# existing user configuration\n",
		"etc/" + service + "/state.db":    "existing runtime state\x00\x01",
		unit:                              "# existing service definition\n",
	}
	for rel, data := range contents {
		writeFile(t, h.path(rel), data, 0o644)
	}
	os.Chmod(h.path("usr/local/bin/"+service), 0o755)
	return contents
}

func (h *coreHarness) assertExistingCoreUntouched(t *testing.T, service string, contents map[string]string) {
	t.Helper()
	for rel, data := range contents {
		if readFile(t, h.path(rel)) != data {
			t.Errorf("%s changed", rel)
		}
	}
	if exists(h.path("etc/" + service + "/.configflow-core-install")) {
		t.Error("receipt written")
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`^systemctl (?:start|stop|restart|enable|disable) ` + service + `(?:\.service)?$`),
		regexp.MustCompile(`^rc-service ` + service + ` (?:start|stop|restart)$`),
		regexp.MustCompile(`^rc-update (?:add|del) ` + service + `(?: |$)`),
	}
	for _, line := range strings.Split(h.eventsText(t), "\n") {
		for _, p := range patterns {
			if p.MatchString(line) {
				t.Errorf("service operation: %s", line)
			}
		}
	}
}

// operationalEvents ignores read-only systemd discovery.
func (h *coreHarness) operationalEvents(t *testing.T) []string {
	var out []string
	for _, line := range lines(h.eventsText(t)) {
		if !strings.HasPrefix(line, "systemctl show ") {
			out = append(out, line)
		}
	}
	return out
}

func lines(text string) []string {
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func (h *coreHarness) appendFile(t *testing.T, rel, text string) {
	f, err := os.OpenFile(h.path(rel), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(text)
	f.Close()
}

func yamlValue(t *testing.T, text string, keys ...any) any {
	t.Helper()
	document, err := readCompose(text)
	if err != nil {
		t.Fatal(err)
	}
	var v any = document
	for _, key := range keys {
		switch k := key.(type) {
		case string:
			v = jsonx.Path(v, k)
		case int:
			list, _ := v.([]any)
			if k >= len(list) {
				return nil
			}
			v = list[k]
		}
	}
	return v
}

func yamlIs(t *testing.T, got any, wantJSON string) {
	t.Helper()
	want, err := jsonx.ParseString(wantJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !jsonx.Equal(got, want) {
		data, _ := jsonx.Compact(got)
		t.Errorf("got %s want %s", data, wantJSON)
	}
}

func (h *coreHarness) mihomoConfig(t *testing.T, keys ...any) any {
	return yamlValue(t, readFile(t, h.path("etc/mihomo/config.yaml")), keys...)
}

func ensureFail(t *testing.T, r coreResult) {
	t.Helper()
	if r.code == 0 {
		t.Fatalf("succeeded:\n%s%s", r.stdout, r.stderr)
	}
}

func ensureOK(t *testing.T, r coreResult) {
	t.Helper()
	if r.code != 0 {
		t.Fatalf("exit %d:\n%s%s", r.code, r.stdout, r.stderr)
	}
}

func serviceEvent(manager, systemd, openrc string) string {
	if manager == "systemd" {
		return systemd
	}
	return openrc
}

func TestGeneratedCoreScriptSyntaxAndBootstrapConfig(t *testing.T) {
	source := ok(GenerateCoreInstallScript(false))
	if !strings.HasPrefix(source, "#!/bin/sh\n# ConfigFlow core installer interface: 1\n") {
		t.Fatal("header")
	}
	path := writeTemp(t, "script.sh", source)
	if r := runShell(t, "sh", "-n", runOptions{args: []string{path}}); r.code != 0 {
		t.Fatal(r.stderr)
	}
	_, rest, _ := strings.Cut(source, "<<MIHOMO_CONFIG\n")
	body, _, _ := strings.Cut(rest, "\nMIHOMO_CONFIG")
	yamlIs(t, yamlValue(t, body, "tun", "enable"), `"$TUN_ENABLED"`)
	yamlIs(t, yamlValue(t, body, "tun", "dns-hijack"), `["any:1053", "tcp://any:1053"]`)
	yamlIs(t, yamlValue(t, body, "tun", "auto-route"), `true`)
	yamlIs(t, yamlValue(t, body, "tun", "auto-redirect"), `false`)
	yamlIs(t, yamlValue(t, body, "dns", "listen"), `"0.0.0.0:1053"`)
	yamlIs(t, yamlValue(t, body, "dns", "nameserver"), `["223.5.5.5", "119.29.29.29"]`)
	for _, key := range []string{"port", "socks-port", "mixed-port", "redir-port", "tproxy-port"} {
		yamlIs(t, yamlValue(t, body, key), `0`)
	}
	notContains(t, source, "/opt/configflow-agent")
	notContains(t, source, "iptables")
	contains(t, source, "sysctl -w net.ipv4.ip_forward=1")
}

var allCoreTools = []string{"awk", "cat", "chmod", "cp", "gzip", "mkdir", "mktemp", "mv", "rm", "rmdir", "sed", "sha256sum", "sleep", "tail", "tar", "curl"}

func TestMissingDependenciesInstalledBeforeCoreDownload(t *testing.T) {
	for _, c := range [][2]string{
		{"apk", "apk add --no-cache"}, {"apt-get", "apt-get install -y --no-install-recommends"}, {"dnf", "dnf install -y"},
		{"yum", "yum install -y"}, {"pacman", "pacman -S --needed --noconfirm"}, {"zypper", "zypper --non-interactive install --no-recommends"},
	} {
		manager, prefix := c[0], c[1]
		t.Run(manager, func(t *testing.T) {
			t.Parallel()
			init := "systemd"
			if manager == "apk" {
				init = "openrc"
			}
			h := newCoreHarness(t, coreOpts{manager: init, packageManager: manager, missing: allCoreTools})
			ensureOK(t, h.run(t))
			log := h.eventsText(t)
			var packages []string
			for _, line := range lines(log) {
				if strings.HasPrefix(line, "package ") {
					packages = append(packages, line)
				}
			}
			transaction := ""
			for _, line := range packages {
				if strings.HasPrefix(line, "package "+prefix) {
					transaction = line
					break
				}
			}
			if head, _, _ := strings.Cut(transaction, " DEBIAN_FRONTEND="); head != "package "+prefix+" gawk coreutils gzip sed tar curl ca-certificates" {
				t.Fatalf("transaction %q", transaction)
			}
			a, b, c := strings.Index(log, transaction), strings.Index(log, "download "), strings.Index(log, "validate mihomo")
			if !(a < b && b < c) {
				t.Error("order")
			}
			if manager == "apt-get" {
				if !strings.HasPrefix(packages[0], "package apt-get update ") || !strings.HasSuffix(transaction, "DEBIAN_FRONTEND=noninteractive") {
					t.Error(packages)
				}
			} else if len(packages) != 1 {
				t.Error(packages)
			}
			if strings.Contains(log, "-Sy") || strings.Contains(log, " upgrade ") || !exists(h.path("etc/mihomo/.configflow-core-install")) {
				t.Error("upgrade or no receipt")
			}
			ensureOK(t, h.run(t))
			if h.eventsText(t) != log {
				t.Error("second run changed something")
			}
		})
	}
}

func TestMissingTimezoneDataInstalledForNewOxidns(t *testing.T) {
	for _, c := range [][2]string{{"apk", "tzdata"}, {"apt-get", "tzdata"}, {"zypper", "timezone"}} {
		manager, pkg := c[0], c[1]
		t.Run(manager, func(t *testing.T) {
			t.Parallel()
			init := "systemd"
			if manager == "apk" {
				init = "openrc"
			}
			h := newCoreHarness(t, coreOpts{manager: init, packageManager: manager, noTZ: true})
			ensureOK(t, h.run(t, "--core", "oxidns"))
			var installs []string
			for _, line := range lines(h.eventsText(t)) {
				if strings.HasPrefix(line, "package ") && !strings.Contains(line, " update ") {
					installs = append(installs, line)
				}
			}
			if len(installs) != 1 {
				t.Fatal(installs)
			}
			if head, _, _ := strings.Cut(installs[0], " DEBIAN_FRONTEND="); !strings.HasSuffix(head, " "+pkg) {
				t.Error(installs)
			}
			if !exists(h.path("usr/share/zoneinfo/Asia/Shanghai")) {
				t.Error("no zoneinfo")
			}
		})
	}
}

func TestMihomoOnlyInstallDoesNotRequireTimezoneData(t *testing.T) {
	t.Parallel()
	h := newCoreHarness(t, coreOpts{packageManager: "apk", noTZ: true})
	ensureOK(t, h.run(t, "--core", "mihomo"))
	notContains(t, h.eventsText(t), "package ")
}

func TestExistingWgetAndCustomTrustArePreserved(t *testing.T) {
	// Python's env-dict assertions on SSL_CERT_FILE/CURL_CA_BUNDLE check the
	// test's own dict; the observable part is that no curl was installed.
	for _, missing := range [][]string{nil, {"tar"}} {
		h := newCoreHarness(t, coreOpts{packageManager: "apk", downloadTool: "wget", missing: missing,
			settings: []string{"SSL_CERT_FILE=/custom/company-ca.pem", "CURL_CA_BUNDLE=/custom/curl-ca.pem"}})
		ensureOK(t, h.run(t))
		var packages []string
		for _, line := range lines(h.eventsText(t)) {
			if strings.HasPrefix(line, "package ") {
				packages = append(packages, line)
			}
		}
		want := []string(nil)
		if missing != nil {
			want = []string{"package apk add --no-cache tar DEBIAN_FRONTEND="}
		}
		if fmt.Sprint(packages) != fmt.Sprint(want) {
			t.Errorf("packages %v want %v", packages, want)
		}
		if exists(filepath.Join(h.env.get("PATH"), "curl")) {
			t.Error("curl installed")
		}
	}
}

func TestMissingDependenciesWithoutSupportedPackageManagerStopBeforeCoreChanges(t *testing.T) {
	t.Parallel()
	h := newCoreHarness(t, coreOpts{missing: []string{"tar", "curl"}})
	r := h.run(t)
	ensureFail(t, r)
	contains(t, r.stderr, "未找到支持的包管理器")
	contains(t, r.stderr, "tar curl ca-certificates")
	if len(h.operationalEvents(t)) != 0 {
		t.Error(h.operationalEvents(t))
	}
	h.assertUninstalled(t)
}

func TestDependencyInstallFailureNeverDownloadsOrInstallsCores(t *testing.T) {
	for _, manager := range []string{"apk", "apt-get", "dnf", "yum", "pacman", "zypper"} {
		manager := manager
		t.Run(manager, func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, coreOpts{packageManager: manager, missing: []string{"tar"}, settings: []string{"FAIL_PACKAGE=install"}})
			r := h.run(t)
			ensureFail(t, r)
			for _, needle := range []string{"系统依赖安装失败", "软件源及包管理器锁", "系统依赖会保留"} {
				contains(t, r.stderr, needle)
			}
			for _, line := range h.operationalEvents(t) {
				if !strings.HasPrefix(line, "package ") {
					t.Error(line)
				}
			}
			h.assertUninstalled(t)
		})
	}
}

func TestFailedAptUpdateDoesNotInstallPackagesOrCores(t *testing.T) {
	t.Parallel()
	h := newCoreHarness(t, coreOpts{packageManager: "apt-get", missing: []string{"tar"}, settings: []string{"FAIL_PACKAGE=update"}})
	ensureFail(t, h.run(t))
	if fmt.Sprint(h.operationalEvents(t)) != "[package apt-get update DEBIAN_FRONTEND=]" {
		t.Error(h.operationalEvents(t))
	}
	h.assertUninstalled(t)
}

func TestNominalDependencyInstallRechecksActualCommands(t *testing.T) {
	t.Parallel()
	h := newCoreHarness(t, coreOpts{packageManager: "apk", missing: []string{"tar", "curl"}, settings: []string{"PACKAGE_NO_COMMANDS=1"}})
	r := h.run(t)
	ensureFail(t, r)
	contains(t, r.stderr, "包管理器执行后仍缺少依赖：tar curl/wget")
	contains(t, r.stderr, "尚未下载或安装核心")
	notContains(t, h.eventsText(t), "download ")
	h.assertUninstalled(t)
}

func TestUnsupportedEnvironmentNeverInstallsDependencies(t *testing.T) {
	for i, c := range []struct {
		manager  string
		settings []string
	}{{"systemd", []string{"FAKE_UID=1000"}}, {"systemd", []string{"OS_NAME=Darwin"}}, {"systemd", []string{"ARCH=riscv64"}}, {"none", nil}} {
		c := c
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, coreOpts{manager: c.manager, packageManager: "apk", missing: []string{"tar", "curl"}, settings: c.settings})
			ensureFail(t, h.run(t))
			if h.eventsText(t) != "" {
				t.Error(h.eventsText(t))
			}
			h.assertUninstalled(t)
		})
	}
}

func TestLaterCoreFailureKeepsInstalledDependencies(t *testing.T) {
	t.Parallel()
	h := newCoreHarness(t, coreOpts{packageManager: "apk", missing: []string{"tar"}, settings: []string{"FAIL_DOWNLOAD=1"}})
	r := h.run(t)
	ensureFail(t, r)
	contains(t, r.stderr, "系统依赖会保留")
	contains(t, r.stderr, "ca-certificates / 自定义 CA 信任")
	if !exists(filepath.Join(h.env.get("PATH"), "tar")) {
		t.Error("tar not installed")
	}
	contains(t, h.eventsText(t), "package apk add --no-cache tar")
	notContains(t, h.eventsText(t), "package apk del")
	h.assertUninstalled(t)
}

func TestInstallTwoCoresAndPreserveConfigurationOnSecondRun(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		manager := manager
		t.Run(manager, func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, coreOpts{manager: manager})
			ensureOK(t, h.run(t))
			initial := h.eventsText(t)
			enable := serviceEvent(manager, "enable mihomo", "add mihomo")
			if strings.Index(initial, "validate oxidns") > strings.Index(initial, enable) || strings.Count(initial, "dig ") != 4 {
				t.Error(initial)
			}
			for _, service := range []string{"mihomo", "oxidns"} {
				info, err := os.Stat(h.path("usr/local/bin/" + service))
				if err != nil || info.Mode()&0o111 == 0 || !exists(h.path("etc/"+service+"/.configflow-core-install")) {
					t.Errorf("%s not installed", service)
				}
			}
			config := h.path("etc/mihomo/config.yaml")
			writeFile(t, config, "# user's published configuration\n", 0o644)
			r := h.run(t)
			ensureOK(t, r)
			contains(t, r.detail, "跳过")
			if h.eventsText(t) != initial || readFile(t, config) != "# user's published configuration\n" || exists(h.path("run/configflow-core-install.lock")) {
				t.Error("second run changed something")
			}
		})
	}
}

func TestServiceFailureRollsBackBothCores(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		for _, failure := range []string{"start:oxidns", "enable:oxidns"} {
			manager, failure := manager, failure
			if manager == "openrc" && strings.HasPrefix(failure, "enable:") {
				failure = strings.Replace(failure, "enable:", "add:", 1)
			}
			t.Run(manager+"/"+failure, func(t *testing.T) {
				t.Parallel()
				h := newCoreHarness(t, coreOpts{manager: manager, settings: []string{"FAIL_SERVICE=" + failure}})
				ensureFail(t, h.run(t))
				h.assertUninstalled(t)
				log := h.eventsText(t)
				contains(t, log, serviceEvent(manager, "systemctl stop mihomo", "rc-service mihomo stop"))
				contains(t, log, serviceEvent(manager, "systemctl disable oxidns", "rc-update del oxidns default"))
			})
		}
	}
}

func TestFailedDownloadChecksumOrValidationWritesNoCores(t *testing.T) {
	for _, setting := range []string{"BAD_CHECKSUM=mihomo.gz", "BAD_CHECKSUM=oxidns.tar.gz", "FAIL_VALIDATE=mihomo", "FAIL_VALIDATE=oxidns", "FAIL_DOWNLOAD=1"} {
		setting := setting
		t.Run(setting, func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, coreOpts{settings: []string{setting}})
			ensureFail(t, h.run(t))
			h.assertUninstalled(t)
			notContains(t, h.eventsText(t), " enable ")
			notContains(t, h.eventsText(t), " start ")
			if globAny(filepath.Join(h.tmp, "stage.*")) {
				t.Error("stage left")
			}
		})
	}
}

func TestDNSFailureEvenZeroExitServfailRollsBack(t *testing.T) {
	for _, failure := range []string{"exit", "servfail"} {
		failure := failure
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, coreOpts{settings: []string{"FAIL_DIG=" + failure}})
			r := h.run(t)
			ensureFail(t, r)
			contains(t, r.stderr, "DNS 查询")
			contains(t, h.eventsText(t), "systemctl stop oxidns")
			h.assertUninstalled(t)
		})
	}
}

func TestExistingPartialConfigurationIsSkippedWhileOtherCoreInstalls(t *testing.T) {
	t.Parallel()
	h := newCoreHarness(t, coreOpts{})
	path := writeFile(t, h.path("etc/oxidns/config.yaml"), "original config", 0o644)
	r := h.run(t)
	ensureOK(t, r)
	contains(t, r.detail, "跳过")
	if readFile(t, path) != "original config" || !exists(h.path("usr/local/bin/mihomo")) || exists(h.path("usr/local/bin/oxidns")) {
		t.Error("files")
	}
	notContains(t, h.eventsText(t), "svenshi/oxidns")
	for _, line := range h.operationalEvents(t) {
		if !strings.HasPrefix(line, "download ") && strings.Contains(line, "oxidns") {
			t.Errorf("oxidns touched: %s", line)
		}
	}
}

func TestSingleCoreInstallsOnlySelectedBinaryServiceAndDNS(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		for _, c := range []struct{ selected, port, asset, other string }{
			{"mihomo", "1053", "MetaCubeX/mihomo", "oxidns"}, {"oxidns", "53", "svenshi/oxidns", "mihomo"},
		} {
			manager, c := manager, c
			t.Run(manager+"/"+c.selected, func(t *testing.T) {
				t.Parallel()
				h := newCoreHarness(t, coreOpts{manager: manager})
				r := h.run(t, "--core", c.selected)
				ensureOK(t, r)
				log := h.eventsText(t)
				var downloads, queries []string
				for _, line := range lines(log) {
					if strings.HasPrefix(line, "download ") {
						downloads = append(downloads, line)
					}
					if strings.HasPrefix(line, "dig ") {
						queries = append(queries, line)
					}
				}
				if len(downloads) != 1 || !strings.Contains(downloads[0], c.asset) {
					t.Error(downloads)
				}
				contains(t, log, "validate "+c.selected)
				notContains(t, log, "validate "+c.other)
				if !exists(h.path("usr/local/bin/"+c.selected)) || !exists(h.path("etc/"+c.selected+"/.configflow-core-install")) ||
					exists(h.path("usr/local/bin/"+c.other)) || exists(h.path("etc/"+c.other)) {
					t.Error("files")
				}
				tcp := 0
				for _, q := range queries {
					if !strings.Contains(q, " -p "+c.port+" ") {
						t.Error(q)
					}
					if strings.Contains(q, "+tcp") {
						tcp++
					}
				}
				if len(queries) != 2 || tcp != 1 {
					t.Error(queries)
				}
				notContains(t, strings.Join(h.operationalEvents(t), "\n"), c.other)
				if c.selected == "oxidns" {
					yamlIs(t, yamlValue(t, readFile(t, h.path("etc/oxidns/config.yaml")), "plugins", 0, "args", "upstreams"),
						`[{"addr": "udp://223.5.5.5"}, {"addr": "udp://119.29.29.29"}]`)
					contains(t, r.detail, "公共")
					notContains(t, r.detail, "IPv4 转发")
					notContains(t, r.detail, "tun.enable")
				}
			})
		}
	}
}

func TestBothInstallsMissingCoreWithoutTouchingExistingPeer(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		for _, c := range [][3]string{{"mihomo", "oxidns", "041D"}, {"oxidns", "mihomo", "0035"}} {
			manager, existing, fresh, occupied := manager, c[0], c[1], c[2]
			t.Run(manager+"/"+existing, func(t *testing.T) {
				t.Parallel()
				h := newCoreHarness(t, coreOpts{manager: manager})
				contents := h.seedExistingCore(t, existing, manager)
				h.appendFile(t, "proc/net/udp", "0: 00000000:"+occupied+" 00000000:0000 07\n")
				r := h.run(t, "--core", "both")
				ensureOK(t, r)
				contains(t, r.detail, "跳过")
				if !exists(h.path("etc/" + fresh + "/.configflow-core-install")) {
					t.Error("no receipt")
				}
				h.assertExistingCoreUntouched(t, existing, contents)
				log := h.eventsText(t)
				var downloads, queries []string
				for _, line := range lines(log) {
					if strings.HasPrefix(line, "download ") {
						downloads = append(downloads, line)
					}
					if strings.HasPrefix(line, "dig ") {
						queries = append(queries, line)
					}
				}
				if len(downloads) != 1 || len(queries) != 2 {
					t.Error(downloads, queries)
				}
				notContains(t, log, "validate "+existing)
				contains(t, log, "validate "+fresh)
				port := "1053"
				if fresh == "oxidns" {
					port = "53"
				}
				for _, q := range queries {
					if !strings.Contains(q, " -p "+port+" ") {
						t.Error(q)
					}
				}
				if fresh == "oxidns" {
					yamlIs(t, yamlValue(t, readFile(t, h.path("etc/oxidns/config.yaml")), "plugins", 0, "args", "upstreams"), `[{"addr": "udp://127.0.0.1:1053"}]`)
					contains(t, r.detail, "127.0.0.1:1053")
					notContains(t, r.detail, "tun.enable")
				} else {
					contains(t, r.detail, "已有 OxiDNS 的上游保持原样")
					contains(t, r.detail, "127.0.0.1:1053")
				}
			})
		}
	}
}

func TestExistingSelectedCoresSkipWithoutPackageOrServiceOperations(t *testing.T) {
	for _, selected := range []string{"both", "mihomo", "oxidns"} {
		selected := selected
		t.Run(selected, func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, coreOpts{packageManager: "apk", missing: []string{"awk", "tar", "curl", "sha256sum", "cat"}})
			contents := map[string]map[string]string{}
			for _, service := range []string{"mihomo", "oxidns"} {
				contents[service] = h.seedExistingCore(t, service, "systemd")
			}
			r := h.run(t, "--core", selected)
			ensureOK(t, r)
			contains(t, r.detail, "跳过")
			if len(h.operationalEvents(t)) != 0 {
				t.Error(h.operationalEvents(t))
			}
			for service, files := range contents {
				h.assertExistingCoreUntouched(t, service, files)
			}
			if exists(h.path("run/configflow-core-install.lock")) {
				t.Error("lock left")
			}
		})
	}
}

func TestExternalMihomoInstallationIsRecognizedWithoutDefaultConfig(t *testing.T) {
	for _, evidence := range []string{"binary", "service", "path", "loaded-service"} {
		evidence := evidence
		t.Run(evidence, func(t *testing.T) {
			t.Parallel()
			existing := ""
			if evidence == "loaded-service" {
				existing = "mihomo.service"
			}
			h := newCoreHarness(t, coreOpts{settings: []string{"EXISTING_SERVICE=" + existing}})
			path := ""
			switch evidence {
			case "binary":
				path = h.path("usr/local/bin/mihomo")
			case "service":
				path = h.path("etc/systemd/system/mihomo.service")
			case "path":
				path = filepath.Join(h.env.get("PATH"), "mihomo")
			}
			if path != "" {
				writeFile(t, path, "#!/bin/sh\nexit 97\n", 0o755)
			}
			r := h.run(t)
			ensureOK(t, r)
			contains(t, r.detail, "跳过")
			if exists(h.path("etc/mihomo")) || !exists(h.path("etc/oxidns/.configflow-core-install")) {
				t.Error("files")
			}
			notContains(t, h.eventsText(t), "MetaCubeX/mihomo")
			for _, line := range h.operationalEvents(t) {
				if strings.Contains(line, "mihomo") {
					t.Error(line)
				}
			}
			if path != "" && readFile(t, path) != "#!/bin/sh\nexit 97\n" {
				t.Error("evidence changed")
			}
		})
	}
}

func TestInvalidCoreArgumentsRejectBeforeDependencyOrServiceActions(t *testing.T) {
	for i, args := range [][]string{{"--core", "invalid"}, {"--core"}, {"--bad-option"}, {"mihomo"}} {
		args := args
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, coreOpts{packageManager: "apk", missing: []string{"tar", "curl"}})
			ensureFail(t, h.run(t, args...))
			if h.eventsText(t) != "" {
				t.Error(h.eventsText(t))
			}
			h.assertUninstalled(t)
		})
	}
}

func TestSingleCoreIgnoresUnrelatedDNSPort(t *testing.T) {
	for _, c := range [][2]string{{"oxidns", "041D"}, {"mihomo", "0035"}} {
		for _, n := range [][2]string{{"tcp", "0A"}, {"udp", "07"}, {"tcp6", "0A"}, {"udp6", "07"}} {
			selected, other, network, state := c[0], c[1], n[0], n[1]
			t.Run(selected+"/"+network, func(t *testing.T) {
				t.Parallel()
				h := newCoreHarness(t, coreOpts{})
				h.appendFile(t, "proc/net/"+network, "0: 00000000:"+other+" 00000000:0000 "+state+"\n")
				ensureOK(t, h.run(t, "--core", selected))
				if !exists(h.path("etc/" + selected + "/.configflow-core-install")) {
					t.Error("no receipt")
				}
			})
		}
	}
}

func TestSingleCoreChecksItsOwnDNSPortBeforeDownload(t *testing.T) {
	for _, c := range [][2]string{{"oxidns", "0035"}, {"mihomo", "041D"}} {
		selected, port := c[0], c[1]
		t.Run(selected, func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, coreOpts{})
			h.appendFile(t, "proc/net/udp", "0: 00000000:"+port+" 00000000:0000 07\n")
			r := h.run(t, "--core", selected)
			ensureFail(t, r)
			contains(t, r.stderr, "端口被占用")
			notContains(t, h.eventsText(t), "download ")
			h.assertUninstalled(t)
		})
	}
}

func TestInstallMissingCoreFailurePreservesExistingMihomo(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		for _, failure := range []string{"service", "dns", "package"} {
			manager, failure := manager, failure
			t.Run(manager+"/"+failure, func(t *testing.T) {
				t.Parallel()
				o := coreOpts{manager: manager, settings: []string{"FAIL_DIG=servfail"}}
				switch failure {
				case "service":
					o.settings = []string{"FAIL_SERVICE=start:oxidns"}
				case "package":
					o.settings = []string{"FAIL_PACKAGE=install"}
					o.packageManager, o.missing = "apk", []string{"tar"}
				}
				h := newCoreHarness(t, o)
				contents := h.seedExistingCore(t, "mihomo", manager)
				r := h.run(t, "--core", "both")
				ensureFail(t, r)
				h.assertExistingCoreUntouched(t, "mihomo", contents)
				if exists(h.path("usr/local/bin/oxidns")) || exists(h.path("etc/oxidns")) || exists(h.path("run/configflow-core-install.lock")) {
					t.Error("files")
				}
				if failure == "dns" {
					contains(t, r.detail+r.stderr, "127.0.0.1:1053")
					notContains(t, r.detail, "公共")
				}
				if failure == "package" {
					notContains(t, h.eventsText(t), "download ")
				}
			})
		}
	}
}

func TestPortConflictPreventsDownloadAndMutation(t *testing.T) {
	for _, c := range [][3]string{{"udp", "0035", "07"}, {"tcp", "041D", "0A"}, {"udp6", "041D", "07"}, {"tcp6", "0035", "0A"}} {
		c := c
		t.Run(c[0], func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, coreOpts{})
			h.appendFile(t, "proc/net/"+c[0], "0: 00000000:"+c[1]+" 00000000:0000 "+c[2]+"\n")
			r := h.run(t)
			ensureFail(t, r)
			contains(t, r.stderr, "端口被占用")
			notContains(t, h.eventsText(t), "download ")
			h.assertUninstalled(t)
		})
	}
}

func TestUnsupportedEnvironmentWritesNothing(t *testing.T) {
	for _, setting := range []string{"FAKE_UID=1000", "ARCH=riscv64"} {
		setting := setting
		t.Run(setting, func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, coreOpts{settings: []string{setting}})
			ensureFail(t, h.run(t))
			if len(h.operationalEvents(t)) != 0 {
				t.Error(h.operationalEvents(t))
			}
			h.assertUninstalled(t)
		})
	}
}

func TestConcurrentInstallKeepsOtherProcessLock(t *testing.T) {
	t.Parallel()
	h := newCoreHarness(t, coreOpts{})
	pid := writeFile(t, h.path("run/configflow-core-install.lock/pid"), "123456", 0o644)
	r := h.run(t)
	ensureFail(t, r)
	contains(t, r.stderr, "另一个核心安装")
	if readFile(t, pid) != "123456" || len(h.operationalEvents(t)) != 0 {
		t.Error("lock or events changed")
	}
}

func TestFailedServiceStopPreservesManageableCoreFiles(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		manager := manager
		t.Run(manager, func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, coreOpts{manager: manager, settings: []string{"FAIL_SERVICE=start:oxidns", "FAIL_ROLLBACK=stop:mihomo"}})
			r := h.run(t)
			ensureFail(t, r)
			contains(t, r.stderr, "回滚未完成")
			unit := serviceEvent(manager, "etc/systemd/system/mihomo.service", "etc/init.d/mihomo")
			if !exists(h.path("usr/local/bin/mihomo")) || !exists(h.path("etc/mihomo/config.yaml")) || !exists(h.path(unit)) ||
				exists(h.path("etc/mihomo/.configflow-core-install")) || exists(h.path("run/configflow-core-install.lock")) {
				t.Error("files")
			}
		})
	}
}

func TestNoDigReportsUnverifiedDNSWithoutRejectingInstall(t *testing.T) {
	t.Parallel()
	h := newCoreHarness(t, coreOpts{noDig: true})
	r := h.run(t)
	ensureOK(t, r)
	contains(t, r.detail, "实际 DNS 解析尚未验证")
	notContains(t, r.detail, "UDP/TCP DNS 查询均已通过")
	contains(t, r.stdout, "未验证（缺少 dig）")
	contains(t, r.stdout, "实际解析未验证")
	notContains(t, r.stdout, "UDP/TCP 查询通过")
	if !exists(h.path("etc/mihomo/.configflow-core-install")) {
		t.Error("no receipt")
	}
}

func TestDefaultOutputSummarizesEveryCoreSelectionAndKeepsVerboseLog(t *testing.T) {
	for _, selected := range []string{"both", "mihomo", "oxidns"} {
		selected := selected
		t.Run(selected, func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, coreOpts{})
			r := h.run(t, "--core", selected)
			ensureOK(t, r)
			out := r.stdout + r.stderr
			for _, needle := range []string{"检查核心安装环境与依赖", "下载安装", "验证新装核心的 DNS", "UDP/TCP 查询均通过", "完整日志："} {
				contains(t, r.stdout, needle)
			}
			contains(t, out, "核心安装完成")
			for _, needle := range []string{"HEADER", "初始上游为公共 DNS", "首次从面板发布前"} {
				notContains(t, r.stdout, needle)
			}
			info, err := os.Stat(r.logPath)
			if r.logPath == "" || err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
				t.Errorf("log %q", r.logPath)
			}
			contains(t, r.detail, "status: NOERROR")
			if selected == "both" || selected == "mihomo" {
				contains(t, r.stdout, "Mihomo：新装")
				contains(t, r.stdout, "TUN：已启用")
				contains(t, out, "DIRECT 直连")
				contains(t, out, "重启后是否保持尚未确认")
			}
			if selected == "both" || selected == "oxidns" {
				contains(t, r.stdout, "OxiDNS：新装")
			}
			if selected == "oxidns" {
				notContains(t, r.stdout, "TUN：")
				notContains(t, r.stdout, "IPv4 转发")
				notContains(t, r.stdout, "DIRECT 直连")
			}
			contains(t, out, "如需面板管理，请继续安装对应 Agent")
		})
	}
}

func TestEmbeddedCorePhaseDoesNotAskForASeparateAgent(t *testing.T) {
	for _, c := range [][2]string{{"single", "mihomo"}, {"single", "oxidns"}, {"aio", "both"}} {
		kind, selected := c[0], c[1]
		t.Run(kind+"/"+selected, func(t *testing.T) {
			t.Parallel()
			var combined string
			if kind == "single" {
				combined = ok(GenerateGoAgentInstallScript(GoAgentOptions{ServerURL: "https://config.test", AgentName: "single", ServiceType: selected, InstallCore: true}))
			} else {
				o := NewAIOOptions("https://config.test", "cohost")
				o.InstallCore = true
				combined = ok(GenerateGoAIOInstallScript(o))
			}
			start := strings.Index(combined, "(\nset -- --core "+selected+"\n")
			preamble := combined[start : start+strings.Index(combined[start:], "#!/bin/sh")]
			h := newCoreHarness(t, coreOpts{})
			writeFile(t, h.installer, preamble+pyRstrip(readFile(t, h.installer))+"\n)\n", 0o644)
			r := h.run(t)
			ensureOK(t, r)
			contains(t, r.stdout+r.stderr, "核心安装完成")
			notContains(t, r.stdout+r.stderr, "继续安装对应 Agent")
			notContains(t, r.detail, "独立的 Agent 接入安装")
		})
	}
}

func TestExistingCoreSummaryDoesNotClaimDNSOrTunSuccess(t *testing.T) {
	t.Parallel()
	h := newCoreHarness(t, coreOpts{})
	for _, service := range []string{"mihomo", "oxidns"} {
		h.seedExistingCore(t, service, "systemd")
	}
	r := h.run(t)
	ensureOK(t, r)
	for _, needle := range []string{"全部跳过", "运行状态未验证", "DNS：未验证"} {
		contains(t, r.stdout, needle)
	}
	notContains(t, r.stdout, "UDP/TCP 查询通过")
	notContains(t, r.stdout, "TUN：已启用")
	if len(h.operationalEvents(t)) != 0 {
		t.Error(h.operationalEvents(t))
	}
}

func TestTunRuntimeFallbackSummaryIsClearAndPreservesDetailedRepairHelp(t *testing.T) {
	t.Parallel()
	h := newCoreHarness(t, coreOpts{settings: []string{"NO_TUN_INTERFACE=1"}})
	r := h.run(t, "--core", "mihomo")
	ensureOK(t, r)
	contains(t, r.stdout, "TUN：已关闭")
	contains(t, r.stdout+r.stderr, "TUN 未启用")
	contains(t, r.stdout+r.stderr, "修复步骤见详细日志")
	notContains(t, r.stdout, "TUN：已启用")
	contains(t, r.stdout, "UDP/TCP 查询通过")
	notContains(t, r.stdout, "pct set <CTID> --devN")
	contains(t, r.detail, "pct set <CTID> --devN")
}

func TestChangedBinaryOrServiceIsPreservedAndNotReinstalled(t *testing.T) {
	for _, target := range []string{"usr/local/bin/mihomo", "etc/systemd/system/oxidns.service"} {
		target := target
		t.Run(filepath.Base(target), func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, coreOpts{})
			ensureOK(t, h.run(t))
			writeFile(t, h.path(target), "custom replacement", 0o644)
			initial := h.eventsText(t)
			ensureOK(t, h.run(t))
			if readFile(t, h.path(target)) != "custom replacement" || h.eventsText(t) != initial {
				t.Error("reinstalled")
			}
		})
	}
}

func TestArchitectureSelectsMatchingOfficialAssets(t *testing.T) {
	for _, c := range [][3]string{
		{"aarch64", "mihomo-linux-arm64-v1.19.32.gz", "oxidns-aarch64-unknown-linux-musl.tar.gz"},
		{"armv7l", "mihomo-linux-armv7-v1.19.32.gz", "oxidns-armv7-unknown-linux-musleabihf.tar.gz"},
	} {
		c := c
		t.Run(c[0], func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, coreOpts{settings: []string{"ARCH=" + c[0]}})
			ensureOK(t, h.run(t))
			contains(t, h.eventsText(t), c[1])
			contains(t, h.eventsText(t), c[2])
		})
	}
}

func TestOpenRCStartFailureShowsCoreLogBeforeCleanup(t *testing.T) {
	t.Parallel()
	h := newCoreHarness(t, coreOpts{manager: "openrc", settings: []string{"FAIL_SERVICE=start:oxidns"}})
	r := h.run(t)
	ensureFail(t, r)
	contains(t, r.stderr, "core startup error: fixture failure")
	h.assertUninstalled(t)
}

func TestTunReadyAutoEnablesWithoutChangingForwarding(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		for _, forwarding := range []string{"1", "0", ""} {
			manager, forwarding := manager, forwarding
			t.Run(manager+"/"+forwarding, func(t *testing.T) {
				t.Parallel()
				h := newCoreHarness(t, coreOpts{manager: manager, forwarding: forwarding, forwardingNone: forwarding == ""})
				r := h.run(t)
				ensureOK(t, r)
				yamlIs(t, h.mihomoConfig(t, "tun"), `{"enable": true, "device": "configflow-tun", "stack": "mixed", "auto-route": true,
					"auto-detect-interface": true, "auto-redirect": false, "dns-hijack": ["any:1053", "tcp://any:1053"]}`)
				yamlIs(t, h.mihomoConfig(t, "dns", "enable"), `true`)
				yamlIs(t, h.mihomoConfig(t, "rules"), `["MATCH,DIRECT"]`)
				yamlIs(t, h.mihomoConfig(t, "proxies"), `[]`)
				for _, needle := range []string{"已确认 Mihomo 创建并持有", "安装结果：tun.enable=true", "自动调整本机路由"} {
					contains(t, r.detail, needle)
				}
				notContains(t, h.eventsText(t), "FORBIDDEN")
				forward := h.path("proc/sys/net/ipv4/ip_forward")
				if forwarding == "" {
					if exists(forward) {
						t.Error("forward file created")
					}
					contains(t, r.detail, "IPv4 转发：状态无法读取")
				} else {
					if readFile(t, forward) != forwarding+"\n" {
						t.Error("forwarding changed")
					}
					contains(t, r.detail, "当前为 "+forwarding)
				}
				if forwarding != "1" {
					for _, needle := range []string{"sysctl -w net.ipv4.ip_forward=1", "99-configflow-forward.conf", "仅做二层网桥"} {
						contains(t, r.detail, needle)
					}
				}
				if exists(h.path("etc/sysctl.d")) {
					t.Error("sysctl.d written")
				}
			})
		}
	}
}

func TestUnavailableTunInstallsDNSAndPrintsRepairSteps(t *testing.T) {
	for i, c := range []struct {
		o      coreOpts
		reason string
	}{
		{coreOpts{tunDevice: "missing"}, "未提供"},
		{coreOpts{tunDevice: "regular"}, "不是字符设备"},
		{coreOpts{tunDevice: "denied"}, "无法读写打开"},
		{coreOpts{settings: []string{"DEVICE_NUMBER=a:3"}}, "无法确认"},
		{coreOpts{settings: []string{"DEVICE_NUMBER="}}, "无法确认"},
		{coreOpts{noStat: true}, "缺少 stat"},
		{coreOpts{capNone: true}, "无法读取或解析"},
		{coreOpts{capStatus: "CapEff:\t1000\n"}, "无法读取或解析"},
		{coreOpts{capStatus: "CapEff:\tXXXX\nCapBnd:\t1000\n"}, "无法读取或解析"},
		{coreOpts{capStatus: "CapEff:\t10000000000000000\nCapBnd:\t1000\n"}, "无法读取或解析"},
		{coreOpts{capStatus: "CapEff:\t0\nCapBnd:\t1000\n"}, "缺少有效 CAP_NET_ADMIN"},
		{coreOpts{capStatus: "CapEff:\t1000\nCapBnd:\t0\n"}, "能力上限 CapBnd"},
		{coreOpts{existingTun: true}, "接口 configflow-tun 已存在"},
	} {
		c := c
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, c.o)
			r := h.run(t)
			ensureOK(t, r)
			yamlIs(t, h.mihomoConfig(t, "tun", "enable"), `false`)
			yamlIs(t, h.mihomoConfig(t, "dns", "enable"), `true`)
			for _, needle := range []string{c.reason, "PVE 宿主机", "pct set <CTID> --devN", "面板的 Mihomo 基础配置中设置 tun.enable: true",
				"首次从面板发布前", "发布以面板配置为准", "安装结果：tun.enable=false"} {
				contains(t, r.detail, needle)
			}
			if strings.Count(h.eventsText(t), "dig ") != 4 {
				t.Error("dig count")
			}
			notContains(t, h.eventsText(t), "FORBIDDEN")
			notContains(t, r.detail, "已确认 Mihomo 创建并持有")
			if !exists(h.path("etc/oxidns/.configflow-core-install")) {
				t.Error("no oxidns receipt")
			}
		})
	}
}

func TestCapabilityProbeSupportsUppercaseAnd32bitMasks(t *testing.T) {
	for i, status := range []string{
		"CapEff:\t00000000FFFFFFFF\nCapBnd:\t00000000FFFFFFFF\n",
		"CapEff:\t1000\nCapBnd:\t1000\n",
		"CapEff:\t000000000000B000\nCapBnd:\t000000000000D000\n",
	} {
		status := status
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, coreOpts{capStatus: status})
			ensureOK(t, h.run(t))
			yamlIs(t, h.mihomoConfig(t, "tun", "enable"), `true`)
		})
	}
}

func TestRuntimeTunFailureFallsBackToDNSWithGuidance(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		for _, setting := range []string{"NO_TUN_INTERFACE=1", "TUN_FLAGS=0x1002", "TUN_LINK_FLAGS=0x1000", "TUN_OWNER_DEVICE=another-tun"} {
			manager, setting := manager, setting
			t.Run(manager+"/"+setting, func(t *testing.T) {
				t.Parallel()
				h := newCoreHarness(t, coreOpts{manager: manager, settings: []string{setting}})
				r := h.run(t)
				ensureOK(t, r)
				yamlIs(t, h.mihomoConfig(t, "tun", "enable"), `false`)
				yamlIs(t, h.mihomoConfig(t, "dns", "enable"), `true`)
				yamlIs(t, h.mihomoConfig(t, "dns", "listen"), `"0.0.0.0:1053"`)
				for _, needle := range []string{"将关闭本次初始配置的 TUN，保留 DNS 服务安装", "服务管理器权限或容器设备策略", "安装结果：tun.enable=false"} {
					contains(t, r.detail, needle)
				}
				log := h.eventsText(t)
				stop := serviceEvent(manager, "systemctl stop mihomo", "rc-service mihomo stop")
				start := serviceEvent(manager, "systemctl start mihomo", "rc-service mihomo start")
				if strings.Count(log, start) != 2 || strings.Count(log, stop) != 1 {
					t.Error("start/stop counts")
				}
				a, b, c, d := strings.Index(log, start), strings.Index(log, stop), strings.LastIndex(log, start), strings.Index(log, "dig ")
				if !(a < b && b < c && c < d) || strings.Count(log, "dig ") != 4 || strings.Contains(log, "FORBIDDEN") {
					t.Error("order")
				}
				for _, service := range []string{"mihomo", "oxidns"} {
					if !exists(h.path("etc/" + service + "/.configflow-core-install")) {
						t.Error("no receipt")
					}
				}
			})
		}
	}
}

func TestRuntimeFallbackStopFailurePreservesExistingRunningConfig(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		manager := manager
		t.Run(manager, func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, coreOpts{manager: manager, settings: []string{"NO_TUN_INTERFACE=1", "FAIL_ROLLBACK=stop:mihomo"}})
			r := h.run(t)
			ensureFail(t, r)
			contains(t, r.stderr, "关闭 TUN 前停止 Mihomo 失败")
			contains(t, r.stderr, "回滚未完成")
			yamlIs(t, h.mihomoConfig(t, "tun", "enable"), `true`)
			if !exists(h.path("usr/local/bin/mihomo")) || exists(h.path("etc/mihomo/.configflow-core-install")) {
				t.Error("files")
			}
			notContains(t, h.eventsText(t), "dig ")
		})
	}
}

func TestRuntimeFallbackStartFailureRollsBackInstallation(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		manager := manager
		t.Run(manager, func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, coreOpts{manager: manager, settings: []string{"NO_TUN_INTERFACE=1", "FAIL_FALLBACK_START=1"}})
			r := h.run(t)
			ensureFail(t, r)
			contains(t, r.stderr, "关闭 TUN 后 Mihomo 启动失败")
			h.assertUninstalled(t)
		})
	}
}

func TestReinstallDoesNotEnableTunAfterDeviceIsFixed(t *testing.T) {
	t.Parallel()
	h := newCoreHarness(t, coreOpts{tunDevice: "missing"})
	ensureOK(t, h.run(t))
	config := h.path("etc/mihomo/config.yaml")
	oldConfig, oldEvents := readFile(t, config), h.eventsText(t)
	os.Symlink("/dev/null", h.path("dev/net/tun"))
	r := h.run(t)
	ensureOK(t, r)
	contains(t, r.detail, "跳过")
	if readFile(t, config) != oldConfig || h.eventsText(t) != oldEvents {
		t.Error("changed")
	}
	yamlIs(t, yamlValue(t, oldConfig, "tun", "enable"), `false`)
}

func TestSlowTunStartGetsTimeToInitialize(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		manager := manager
		t.Run(manager, func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, coreOpts{manager: manager, settings: []string{"LATE_TUN_READY=3"}})
			r := h.run(t)
			ensureOK(t, r)
			yamlIs(t, h.mihomoConfig(t, "tun", "enable"), `true`)
			if readFile(t, h.path("run/tun-wait-count")) != "3" {
				t.Error("wait count")
			}
			contains(t, r.detail, "已确认 Mihomo 创建并持有")
			notContains(t, r.detail, "保留 DNS 服务安装")
		})
	}
}

func TestTunWaitIsBoundedBeforeDNSFallback(t *testing.T) {
	t.Parallel()
	h := newCoreHarness(t, coreOpts{settings: []string{"LATE_TUN_READY=99"}})
	ensureOK(t, h.run(t))
	yamlIs(t, h.mihomoConfig(t, "tun", "enable"), `false`)
	if readFile(t, h.path("run/tun-wait-count")) != "10" {
		t.Error("wait count")
	}
}

func (h *coreHarness) resolvedStub(t *testing.T, answer *string) {
	h.appendFile(t, "proc/net/udp", "0: 3500007F:0035 00000000:0000 07\n1: 3600007F:0035 00000000:0000 07\n")
	h.appendFile(t, "proc/net/tcp", "0: 3500007F:0035 00000000:0000 0A\n1: 3600007F:0035 00000000:0000 0A\n")
	if answer != nil {
		writeFile(t, h.path("dev/tty"), *answer+"\n", 0o644)
	}
}

func strp(s string) *string { return &s }

func TestResolvedStubWithoutTerminalStopsWithTheFix(t *testing.T) {
	t.Parallel()
	h := newCoreHarness(t, coreOpts{settings: []string{"RESOLVED_ACTIVE=1"}})
	h.resolvedStub(t, nil)
	r := h.run(t, "--core", "oxidns")
	ensureFail(t, r)
	contains(t, r.stderr, "没有可交互的终端")
	contains(t, r.stderr, "DNSStubListener=no")
	notContains(t, h.eventsText(t), "download ")
	notContains(t, h.eventsText(t), "restart systemd-resolved")
	if exists(h.path("etc/systemd/resolved.conf.d")) {
		t.Error("drop-in written")
	}
	h.assertUninstalled(t)
}

func TestResolvedStubDisabledAfterUserConfirmsThenInstalls(t *testing.T) {
	t.Parallel()
	h := newCoreHarness(t, coreOpts{settings: []string{"RESOLVED_ACTIVE=1"}})
	h.resolvedStub(t, strp("y"))
	ensureOK(t, h.run(t, "--core", "oxidns"))
	contains(t, readFile(t, h.path("dev/tty")), "[y/N]")
	if !strings.HasSuffix(readFile(t, h.path("etc/systemd/resolved.conf.d/configflow.conf")), "DNSStubListener=no\n") {
		t.Error("drop-in")
	}
	contains(t, h.eventsText(t), "systemctl restart systemd-resolved")
	if !exists(h.path("usr/local/bin/oxidns")) {
		t.Error("oxidns not installed")
	}
}

func TestResolvedStubLeftAloneUnlessUserTypesY(t *testing.T) {
	for _, answer := range []string{"", "n", "no"} {
		answer := answer
		t.Run(answer, func(t *testing.T) {
			t.Parallel()
			h := newCoreHarness(t, coreOpts{settings: []string{"RESOLVED_ACTIVE=1"}})
			h.resolvedStub(t, &answer)
			r := h.run(t, "--core", "oxidns")
			ensureFail(t, r)
			contains(t, r.stderr, "已取消自动修复")
			if exists(h.path("etc/systemd/resolved.conf.d")) {
				t.Error("drop-in written")
			}
			h.assertUninstalled(t)
		})
	}
}

func TestResolvedFixOnlyOfferedForALoneRunningStub(t *testing.T) {
	for _, variant := range []string{"other-listener", "resolved-inactive", "openrc"} {
		variant := variant
		t.Run(variant, func(t *testing.T) {
			t.Parallel()
			o := coreOpts{settings: []string{"RESOLVED_ACTIVE=1"}}
			if variant == "resolved-inactive" {
				o.settings = nil
			}
			if variant == "openrc" {
				o.manager = "openrc"
			}
			h := newCoreHarness(t, o)
			h.resolvedStub(t, strp("y"))
			if variant == "other-listener" {
				h.appendFile(t, "proc/net/udp", "2: 00000000:0035 00000000:0000 07\n")
			}
			r := h.run(t, "--core", "oxidns")
			ensureFail(t, r)
			contains(t, r.stderr, "端口被占用")
			notContains(t, readFile(t, h.path("dev/tty")), "[y/N]")
			if exists(h.path("etc/systemd/resolved.conf.d")) {
				t.Error("drop-in written")
			}
			h.assertUninstalled(t)
		})
	}
}
