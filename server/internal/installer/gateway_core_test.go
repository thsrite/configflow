package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Port of backend/test_gateway_core_install.py: the opt-in gateway core
// installer against a fully isolated fake host. These tests swap
// CoreReleases (monkeypatch of CORE_RELEASES), so none of them is parallel.

func gatewayGenerate(mutate func(*GatewayInstallOptions)) (string, error) {
	o := GatewayInstallOptions{MihomoAgentPort: 8080, OxidnsAgentPort: 8082, ServerURL: "https://cf.test", AgentName: "gateway", AgentIP: "192.168.8.2",
		AgentChecksums: map[string]string{"linux-amd64": strings.Repeat("a", 64), "linux-arm64": strings.Repeat("b", 64)},
		InstallationID: "install-session-0123456789", EnrollmentURL: "https://cf.test/api/enroll",
		EnrollmentToken: "session-token-01234567890123456789"}
	if mutate != nil {
		mutate(&o)
	}
	return RenderGatewayInstall(o)
}

func TestGatewayInvalidInputRejected(t *testing.T) {
	for i, mutate := range []func(*GatewayInstallOptions){
		func(o *GatewayInstallOptions) { o.AgentName = "name;touch /tmp/bad" }, func(o *GatewayInstallOptions) { o.AgentIP = "127.0.0.1" },
		func(o *GatewayInstallOptions) { o.AgentIP = "bad" }, func(o *GatewayInstallOptions) { o.ServerURL = "file:///etc/passwd" },
		func(o *GatewayInstallOptions) { o.ServerURL = "https://user:pass@cf.test" },
		func(o *GatewayInstallOptions) { o.EnrollmentURL = "https://other.test/enroll" },
		func(o *GatewayInstallOptions) { o.EnrollmentToken = "x\nfoo" }, func(o *GatewayInstallOptions) { o.InstallationID = "bad" },
		func(o *GatewayInstallOptions) { o.AgentChecksums = map[string]string{} },
		func(o *GatewayInstallOptions) { o.AgentChecksums = map[string]string{"linux-amd64": "invalid"} },
		func(o *GatewayInstallOptions) {
			o.AgentChecksums = map[string]string{"linux-armv7": strings.Repeat("a", 64)}
		},
		func(o *GatewayInstallOptions) { o.MihomoAgentPort = 8082 },
	} {
		if _, err := gatewayGenerate(mutate); err == nil {
			t.Errorf("case %d accepted", i)
		} else if _, isValue := err.(*ValueError); !isValue {
			t.Errorf("case %d: %T %v", i, err, err)
		}
	}
}

func TestOptInDoesNotChangeOrdinaryAIOAndShSyntax(t *testing.T) {
	ordinary := ok(RenderAIOInstall(DefaultAIOOptions("https://cf.test", "ordinary")))
	contains(t, ordinary, "read -r choice < /dev/tty")
	notContains(t, ordinary, `"installation_token":`)
	notContains(t, ordinary, "configflow-gateway")
	contains(t, ordinary, "-prepare-tun")
	notContains(t, ordinary, "CONFIGFLOW_FORWARDING_OWNER")
	script := ok(gatewayGenerate(nil))
	notContains(t, script, "read -r choice < /dev/tty")
	contains(t, script, `"installation_id": "install-session-0123456789"`)
	contains(t, script, `"installation_token": "session-token-01234567890123456789"`)
	if r := runShell(t, "sh", "-n", runOptions{args: []string{writeTemp(t, "script.sh", script)}}); r.code != 0 {
		t.Fatal(r.stderr)
	}
	notContains(t, script, "releases/latest")
	if strings.Count(script, `CONFIGFLOW_FORWARDING_OWNER=gateway sh "$GATEWAY_STAGE/install-agents.sh"`) != 1 {
		t.Error("forwarding owner")
	}
	for _, needle := range []string{"sysctl -w", "enable-ipv4-forwarding.sh", "99-configflow-aio-forward.conf", "-prepare-tun", "nft add", "curl | sh"} {
		notContains(t, script, needle)
	}
}

func TestHashesExactServedAgentFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENTS_STATIC_DIR", dir)
	writeFile(t, filepath.Join(dir, "configflow-agent-linux-amd64"), "new binary", 0o644)
	got, err := LocalAgentChecksums()
	if err != nil || len(got) != 1 || got["linux-amd64"] != sha256Hex("new binary") {
		t.Fatal(got, err)
	}
}

type gatewayHost struct {
	tmp, script, events, source string
	env                         env
}

func (h *gatewayHost) path(rel string) string { return filepath.Join(h.tmp, rel) }

func (h *gatewayHost) run(t *testing.T, overrides ...string) runResult {
	return runShell(t, "sh", h.script, runOptions{env: h.env.with(overrides...), replace: true, timeout: 30e9})
}

func (h *gatewayHost) eventsText(t *testing.T) string { return eventsText(t, h.events) }

func (h *gatewayHost) clearEvents(t *testing.T) { writeFile(t, h.events, "", 0o644) }

type hostOpts struct {
	manager, architecture string
	fullTun               bool
}

// newGatewayHost is host(tmp_path, monkeypatch, manager, architecture, full_tun).
func newGatewayHost(t *testing.T, o hostOpts) *gatewayHost {
	if o.manager == "" {
		o.manager = "systemd"
	}
	if o.architecture == "" {
		o.architecture = "x86_64"
	}
	tmp := t.TempDir()
	events := filepath.Join(tmp, "events")
	assets := filepath.Join(tmp, "assets")
	commands := filepath.Join(tmp, "commands")
	os.MkdirAll(assets, 0o755)
	os.MkdirAll(commands, 0o755)
	core := stubScript(t, "gateway-core", "")
	agent := stubScript(t, "gateway-agent", "")
	writeFile(t, filepath.Join(assets, "agent"), agent, 0o644)
	var mihomo, oxidns bytes.Buffer
	gz := gzip.NewWriter(&mihomo)
	gz.Write([]byte(core))
	gz.Close()
	gz = gzip.NewWriter(&oxidns)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "oxidns", Mode: 0o755, Size: int64(len(core)), Typeflag: tar.TypeReg})
	tw.Write([]byte(core))
	tw.Close()
	gz.Close()
	writeFile(t, filepath.Join(assets, "mihomo.gz"), mihomo.String(), 0o644)
	writeFile(t, filepath.Join(assets, "oxidns.tar.gz"), oxidns.String(), 0o644)
	original := CoreReleases
	releases := make([]CoreRelease, len(original))
	for i, release := range original {
		release.Assets = map[string]ReleaseAsset{}
		for arch, asset := range original[i].Assets {
			release.Assets[arch] = asset
		}
		digest := sha256Hex(mihomo.String())
		if release.Service == "oxidns" {
			digest = sha256Hex(oxidns.String())
		}
		for _, arch := range []string{"linux-amd64", "linux-arm64"} {
			asset := release.Assets[arch]
			asset.SHA256 = digest
			release.Assets[arch] = asset
		}
		releases[i] = release
	}
	CoreReleases = releases
	defer func() { CoreReleases = original }()
	hashes := map[string]string{"linux-amd64": sha256Hex(agent), "linux-arm64": sha256Hex(agent)}
	source := ok(gatewayGenerate(func(g *GatewayInstallOptions) { g.AgentChecksums = hashes; g.FullTun = o.fullTun }))
	source = replaceAll(source, [][2]string{
		{"/etc/", tmp + "/etc/"}, {"/etc ", tmp + "/etc "},
		{"/usr/local", tmp + "/usr/local"}, {"/opt/", tmp + "/opt/"},
		{"/opt ", tmp + "/opt "}, {"/run/", tmp + "/run/"},
		{"/run;", tmp + "/run;"}, {"/var/log", tmp + "/logs"},
		{"/proc/[0-9]*/status", tmp + "/agent-process"},
		{"/dev/net/tun", "/dev/null"}, {"/proc/sys/net/ipv4/ip_forward", tmp + "/ip_forward"},
		{"/dev/tty", tmp + "/tty"},
	})
	source = darwinSed(source)
	runDir := "run"
	if o.manager == "systemd" {
		runDir = "run/systemd/system"
	}
	for _, dir := range []string{"etc", "usr/local/bin", "opt", runDir, "logs"} {
		os.MkdirAll(filepath.Join(tmp, dir), 0o755)
	}
	writeFile(t, filepath.Join(tmp, "ip_forward"), "0\n", 0o644)
	writeFile(t, filepath.Join(tmp, "agent-process"), "PPid:\tmissing\n", 0o644)
	writeFile(t, filepath.Join(tmp, "active"), "", 0o644)
	writeFile(t, filepath.Join(tmp, "zoneinfo/Asia/Shanghai"), "TZif", 0o644)
	managerCode := stubScript(t, "gateway-manager", "")
	curl := stubScript(t, "gateway-curl", "")
	sysctl := "#!/bin/sh\nprintf \"forbidden-sysctl %s\\n\" \"$*\" >> \"$EVENTS\"\nexit 98\n"
	if o.fullTun {
		sysctl = stubScript(t, "gateway-sysctl", "")
	}
	mocks := map[string]string{
		"id":    "#!/bin/sh\necho 0\n",
		"uname": "#!/bin/sh\nif [ \"$1\" = -s ]; then echo Linux; else echo " + o.architecture + "; fi\n",
		"curl":  curl, "ip": "#!/bin/sh\nexit 0\n", "nft": "#!/bin/sh\n[ \"${FAIL_NFT:-}\" != yes ]\n",
		"ss": `#!/bin/sh
case "${BUSY_OWNER:-}" in *systemd-resolve*) [ -e "$FAKE_ROOT/resolved-stub-off" ] && exit 0 ;; esac
if [ -n "${BUSY_PORT:-}" ]; then echo "udp UNCONN 0 0 ${BUSY_ADDR:-0.0.0.0}:$BUSY_PORT 0.0.0.0:* ${BUSY_OWNER:-}"; fi
`,
		"sysctl":   sysctl,
		"modprobe": "#!/bin/sh\nprintf \"forbidden-modprobe %s\\n\" \"$*\" >> \"$EVENTS\"\nexit 97\n",
		"mknod":    "#!/bin/sh\nprintf \"forbidden-mknod %s\\n\" \"$*\" >> \"$EVENTS\"\nexit 97\n",
		"sleep":    "#!/bin/sh\nexit 0\n",
	}
	if o.manager == "systemd" {
		mocks["systemctl"] = managerCode
	} else {
		mocks["rc-service"] = managerCode
		mocks["rc-update"] = managerCode
	}
	writeCommands(t, commands, mocks, "")
	script := writeFile(t, filepath.Join(tmp, "install.sh"), source, 0o644)
	e := baseEnv().with("PATH="+commands+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_ROOT="+tmp, "EVENTS="+events,
		"ASSETS="+assets, "TZDIR="+filepath.Join(tmp, "zoneinfo"))
	if o.fullTun {
		e = e.with("FULL_TUN=yes", "AGENT_VERSION=1.8.40-go")
	}
	return &gatewayHost{tmp: tmp, script: script, events: events, source: source, env: e}
}

func TestGatewayNewInstallValidatesBeforeMutationRegistersAndNeverAppliesForwarding(t *testing.T) {
	for _, c := range [][2]string{{"systemd", "x86_64"}, {"openrc", "aarch64"}} {
		manager := c[0]
		h := newGatewayHost(t, hostOpts{manager: manager, architecture: c[1]})
		r := h.run(t)
		if r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		log := h.eventsText(t)
		first := "rc-update -u"
		if manager == "systemd" {
			first = "enable"
		}
		a, b, c := strings.Index(log, "core-check mihomo"), strings.Index(log, "core-check oxidns"), strings.Index(log, first)
		if a < 0 || !(a < b && b < c) {
			t.Errorf("order: %s", log)
		}
		if readFile(t, h.path("ip_forward")) != "0\n" {
			t.Error("forwarding changed")
		}
		for _, needle := range []string{"forbidden-sysctl", "prepare-tun:", "forbidden-modprobe", "forbidden-mknod"} {
			notContains(t, log, needle)
		}
		if exists(h.path("etc/sysctl.d/99-configflow-aio-forward.conf")) || exists(h.path("opt/configflow-agent/mihomo/enable-ipv4-forwarding.sh")) {
			t.Error("forwarding files")
		}
		contains(t, readFile(t, h.path("etc/mihomo/config.yaml")), "enable: false")
		oxidns := readFile(t, h.path("etc/oxidns/config.yaml"))
		contains(t, oxidns, "127.0.0.1:53")
		notContains(t, oxidns, "0.0.0.0:53")
		if strings.Count(log, "configflow-agent-linux-") != 1 {
			t.Error("agent downloads")
		}
		notContains(t, r.stdout+r.stderr, "session-token")
		notContains(t, r.stdout+r.stderr, "agent-token")
		for _, kind := range []string{"mihomo", "oxidns"} {
			var cfg map[string]any
			json.Unmarshal([]byte(readFile(t, h.path("opt/configflow-agent/"+kind+"/config.json"))), &cfg)
			if cfg["installation_id"] != "install-session-0123456789" || cfg["service_binary"] != h.path("usr/local/bin/"+kind) {
				t.Errorf("%s cfg %v", kind, cfg)
			}
		}
		if exists(h.path("run/configflow-gateway-install.lock")) {
			t.Error("lock left")
		}
	}
}

func TestGatewayMissingTimezoneDataInstalledBeforeCores(t *testing.T) {
	h := newGatewayHost(t, hostOpts{manager: "openrc"})
	writeFile(t, h.path("commands/apk"), "#!/bin/sh\nprintf \"apk %s\\n\" \"$*\" >> \"$EVENTS\"\nmkdir -p \"$TZDIR/Asia\" && printf TZif > \"$TZDIR/Asia/Shanghai\"\n", 0o755)
	empty := h.path("empty-zoneinfo")
	r := h.run(t, "TZDIR="+empty)
	if r.code != 0 {
		t.Fatal(r.stdout + r.stderr)
	}
	log := h.eventsText(t)
	if strings.Count(log, "apk add --no-cache tzdata") != 1 || strings.Index(log, "apk add --no-cache tzdata") > strings.Index(log, "rc-update -u") {
		t.Error(log)
	}
	if !exists(filepath.Join(empty, "Asia/Shanghai")) {
		t.Error("no zoneinfo")
	}
}

func TestGatewayTimezoneDataInstallFailureStopsBeforeMutation(t *testing.T) {
	h := newGatewayHost(t, hostOpts{manager: "openrc"})
	writeFile(t, h.path("commands/apk"), "#!/bin/sh\nexit 1\n", 0o755)
	r := h.run(t, "TZDIR="+h.path("empty-zoneinfo"))
	if r.code == 0 {
		t.Fatal("succeeded")
	}
	contains(t, r.stdout+r.stderr, "tzdata")
	if exists(h.path("etc/oxidns")) {
		t.Error("oxidns written")
	}
}

var gatewayPackageManagers = []string{"apk", "apt-get", "dnf", "yum", "pacman", "zypper"}

// withoutHostTools is without_host_tools(tmp_path, hidden).
func withoutHostTools(t *testing.T, tmp string, hidden []string) string {
	shadow := filepath.Join(tmp, "host-tools")
	os.MkdirAll(shadow, 0o755)
	skip := map[string]bool{}
	for _, name := range append(append([]string{}, hidden...), gatewayPackageManagers...) {
		skip[name] = true
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			target := filepath.Join(shadow, entry.Name())
			if skip[entry.Name()] {
				continue
			}
			if _, err := os.Lstat(target); err == nil {
				continue
			}
			full := filepath.Join(dir, entry.Name())
			info, err := os.Stat(full)
			if err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
				os.Symlink(full, target)
			}
		}
	}
	return filepath.Join(tmp, "commands") + string(os.PathListSeparator) + shadow
}

// gatewayPackageManager is gateway_package_manager(tmp_path, name, fail).
func gatewayPackageManager(t *testing.T, tmp, name string, fail bool) {
	failValue := "0"
	if fail {
		failValue = "1"
	}
	writeFile(t, filepath.Join(tmp, "commands", name), stubScript(t, "gateway-package", "", "GATEWAY_PACKAGE_FAIL="+failValue), 0o755)
}

// allOutput is all_output(result): terminal output plus the full log.
func allOutput(r runResult) string {
	text := r.stdout + r.stderr
	for _, line := range strings.Split(text, "\n") {
		if path, found := strings.CutPrefix(line, "完整日志："); found {
			if data, err := os.ReadFile(path); err == nil {
				text += string(data)
			}
		}
	}
	return text
}

func (h *gatewayHost) hideTools(t *testing.T, tools ...string) string {
	os.MkdirAll(h.path("pending"), 0o755)
	for _, tool := range tools {
		if err := os.Rename(h.path("commands/"+tool), h.path("pending/"+tool)); err != nil {
			t.Fatal(err)
		}
	}
	return withoutHostTools(t, h.tmp, tools)
}

func TestMissingGatewayToolsInstalledAndRecheckedBeforeAnyChange(t *testing.T) {
	for _, c := range [][3]string{
		{"openrc", "apk", "apk add --no-cache nftables iproute2"},
		{"systemd", "apt-get", "apt-get install -y --no-install-recommends nftables iproute2"},
		{"systemd", "pacman", "pacman -S --needed --noconfirm nftables iproute2"},
		{"systemd", "dnf", "dnf install -y nftables iproute"},
	} {
		manager, pm, transaction := c[0], c[1], c[2]
		h := newGatewayHost(t, hostOpts{manager: manager})
		path := h.hideTools(t, "nft", "ss")
		gatewayPackageManager(t, h.tmp, pm, false)
		r := h.run(t, "PATH="+path)
		if r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		contains(t, allOutput(r), "缺少工具：nft ss")
		log := h.eventsText(t)
		first := "systemctl enable"
		if manager == "openrc" {
			first = "rc-update -u"
		}
		a, b, c := strings.Index(log, transaction), strings.Index(log, "core-check mihomo"), strings.Index(log, first)
		if strings.Count(log, transaction) != 1 || !(a < b && b < c) {
			t.Errorf("%s: %s", pm, log)
		}
		h.clearEvents(t)
		if r := h.run(t, "PATH="+path); r.code != 0 {
			t.Fatal(r.stderr)
		}
		notContains(t, h.eventsText(t), pm+" ")
	}
}

func TestBusyboxIPWithoutJSONInstallsIproute2(t *testing.T) {
	h := newGatewayHost(t, hostOpts{manager: "openrc"})
	path := h.hideTools(t, "ip")
	writeFile(t, h.path("commands/ip"), "#!/bin/sh\n[ \"$1\" != -j ]\n", 0o755)
	writeFile(t, h.path("pending/ip"), "#!/bin/sh\nexit 0\n", 0o755)
	gatewayPackageManager(t, h.tmp, "apk", false)
	r := h.run(t, "PATH="+path)
	if r.code != 0 {
		t.Fatal(r.stdout + r.stderr)
	}
	if strings.Count(h.eventsText(t), "apk add --no-cache iproute2") != 1 {
		t.Error(h.eventsText(t))
	}
}

func TestFailedGatewayToolInstallStopsWithCopyableCommand(t *testing.T) {
	for _, c := range [][2]string{
		{"apk", "apk add --no-cache nftables iproute2"},
		{"apt-get", "apt-get update && apt-get install -y --no-install-recommends nftables iproute2"},
	} {
		h := newGatewayHost(t, hostOpts{manager: "openrc"})
		path := h.hideTools(t, "nft", "ss")
		gatewayPackageManager(t, h.tmp, c[0], true)
		r := h.run(t, "PATH="+path)
		if r.code == 0 {
			t.Fatal("succeeded")
		}
		contains(t, allOutput(r), "可在 root 下手动执行："+c[1])
		if exists(h.path("etc/mihomo")) || exists(h.path("etc/oxidns")) || exists(h.path("opt/configflow-agent")) {
			t.Error("files")
		}
		notContains(t, h.eventsText(t), "download ")
	}
}

func TestGatewayWithoutPackageManagerNamesThePackages(t *testing.T) {
	h := newGatewayHost(t, hostOpts{manager: "openrc"})
	path := h.hideTools(t, "nft")
	r := h.run(t, "PATH="+path)
	if r.code == 0 {
		t.Fatal("succeeded")
	}
	out := allOutput(r)
	contains(t, out, "未找到支持的包管理器")
	contains(t, out, "请手动安装 nftables 后重新运行")
	if exists(h.path("etc/mihomo")) {
		t.Error("files")
	}
}

func TestGatewayPreflightDownloadAndValidationFailuresLeaveHostUntouched(t *testing.T) {
	for _, failure := range [][]string{{"CORRUPT=oxidns.tar.gz"}, {"FAIL_DOWNLOAD=agent"}, {"FAIL_CHECK=oxidns"},
		{"AGENT_VERSION=1.8.24-go"}, {"AGENT_VERSION=invalid"}, {"AGENT_VERSION=1.8.25-beta"}, {"BUSY_PORT=53"}, {"FAIL_NFT=yes"}} {
		h := newGatewayHost(t, hostOpts{})
		if r := h.run(t, failure...); r.code == 0 {
			t.Fatalf("%v: succeeded", failure)
		}
		if exists(h.path("etc/mihomo")) || exists(h.path("etc/oxidns")) || exists(h.path("opt/configflow-agent")) || readFile(t, h.path("ip_forward")) != "0\n" {
			t.Errorf("%v: host changed", failure)
		}
	}
}

const resolvedOwner = `users:(("systemd-resolve",pid=367,fd=18))`

func resolvedHost(t *testing.T, answer *string) *gatewayHost {
	h := newGatewayHost(t, hostOpts{})
	writeFile(t, h.path("active"), "systemd-resolved", 0o644)
	if answer != nil {
		writeFile(t, h.path("tty"), *answer+"\n", 0o644)
	}
	return h
}

var resolvedBusy = []string{"BUSY_PORT=53", "BUSY_ADDR=127.0.0.53%lo", "BUSY_OWNER=" + resolvedOwner}

func TestSystemdResolvedStubWithoutTerminalNamesTheFix(t *testing.T) {
	h := resolvedHost(t, nil)
	r := h.run(t, resolvedBusy...)
	if r.code == 0 {
		t.Fatal("succeeded")
	}
	for _, needle := range []string{"systemd-resolved", "没有可交互的终端", "DNSStubListener=no"} {
		contains(t, r.stdout+r.stderr, needle)
	}
	if exists(h.path("etc/systemd/resolved.conf.d")) || exists(h.path("etc/oxidns")) {
		t.Error("files")
	}
	notContains(t, h.eventsText(t), "restart systemd-resolved")
}

func TestSystemdResolvedStubFixedAfterUserConfirms(t *testing.T) {
	h := resolvedHost(t, strp("Y"))
	r := h.run(t, resolvedBusy...)
	if r.code != 0 {
		t.Fatal(r.stdout + r.stderr)
	}
	contains(t, readFile(t, h.path("tty")), "[y/N]")
	if !strings.HasSuffix(readFile(t, h.path("etc/systemd/resolved.conf.d/configflow.conf")), "DNSStubListener=no\n") {
		t.Error("drop-in")
	}
	contains(t, h.eventsText(t), "systemctl restart systemd-resolved")
	if !exists(h.path("etc/oxidns")) {
		t.Error("oxidns not installed")
	}
}

func TestSystemdResolvedStubLeftAloneUnlessUserTypesY(t *testing.T) {
	for _, answer := range []string{"", "n", "no"} {
		answer := answer
		h := resolvedHost(t, &answer)
		r := h.run(t, resolvedBusy...)
		if r.code == 0 {
			t.Fatal("succeeded")
		}
		contains(t, r.stdout+r.stderr, "已取消自动修复")
		if exists(h.path("etc/systemd/resolved.conf.d")) || exists(h.path("etc/oxidns")) {
			t.Error("files")
		}
	}
}

func TestOtherDNSOwnerIsNeverOfferedTheResolvedFix(t *testing.T) {
	h := resolvedHost(t, strp("Y"))
	r := h.run(t, "BUSY_PORT=53", `BUSY_OWNER=users:(("dnsmasq",pid=9,fd=4))`)
	out := r.stdout + r.stderr
	if r.code == 0 || strings.Contains(out, "DNSStubListener") || !strings.Contains(out, "已被占用") {
		t.Error(out)
	}
	notContains(t, readFile(t, h.path("tty")), "[y/N]")
}

func TestFreshCoresAreNotReportedAsExistingButRerunsAre(t *testing.T) {
	h := newGatewayHost(t, hostOpts{})
	fresh := h.run(t)
	if fresh.code != 0 {
		t.Fatal(fresh.stdout + fresh.stderr)
	}
	out := fresh.stdout + fresh.stderr
	notContains(t, out, "已有核心配置已保留")
	notContains(t, out, "不安装核心")
	contains(t, out, "为上一步准备好的 Mihomo 和 OxiDNS 安装 Agent")
	rerun := h.run(t, "FAIL_GITHUB=yes")
	if rerun.code != 0 {
		t.Fatal(rerun.stdout + rerun.stderr)
	}
	contains(t, rerun.stdout+rerun.stderr, "已有核心配置已保留")
}

func TestRerunPreservesPublishedConfigsAndExistingAgentIdentity(t *testing.T) {
	h := newGatewayHost(t, hostOpts{})
	if r := h.run(t); r.code != 0 {
		t.Fatal(r.stderr)
	}
	for _, kind := range []string{"mihomo", "oxidns"} {
		writeFile(t, h.path("etc/"+kind+"/config.yaml"), "# user published configuration\n", 0o644)
	}
	h.clearEvents(t)
	r := h.run(t, "FAIL_GITHUB=yes")
	if r.code != 0 {
		t.Fatal(r.stdout + r.stderr)
	}
	notContains(t, h.eventsText(t), "download https://github.com/")
	for _, kind := range []string{"mihomo", "oxidns"} {
		var cfg map[string]any
		json.Unmarshal([]byte(readFile(t, h.path("opt/configflow-agent/"+kind+"/config.json"))), &cfg)
		if readFile(t, h.path("etc/"+kind+"/config.yaml")) != "# user published configuration\n" || cfg["agent_id"] != "registered-"+kind {
			t.Errorf("%s: %v", kind, cfg)
		}
	}
}

func TestOpenRCRerunUpgradesEarlierCoreScriptToWaitForExit(t *testing.T) {
	h := newGatewayHost(t, hostOpts{manager: "openrc"})
	if r := h.run(t); r.code != 0 {
		t.Fatal(r.stderr)
	}
	retry := "retry=\"TERM/30/KILL/5\"\n"
	for _, kind := range []string{"mihomo", "oxidns"} {
		unit := h.path("etc/init.d/" + kind)
		text := readFile(t, unit)
		contains(t, text, retry)
		os.WriteFile(unit, []byte(strings.ReplaceAll(text, retry, "")), 0o644)
	}
	r := h.run(t, "FAIL_GITHUB=yes")
	if r.code != 0 {
		t.Fatal(r.stdout + r.stderr)
	}
	for _, kind := range []string{"mihomo", "oxidns"} {
		unit := h.path("etc/init.d/" + kind)
		info, _ := os.Stat(unit)
		if strings.Count(readFile(t, unit), retry) != 1 || info.Mode().Perm() != 0o755 {
			t.Errorf("%s not upgraded", kind)
		}
	}
}

func TestOpenRCRerunStillRefusesOtherCoreScriptEdits(t *testing.T) {
	h := newGatewayHost(t, hostOpts{manager: "openrc"})
	if r := h.run(t); r.code != 0 {
		t.Fatal(r.stderr)
	}
	unit := h.path("etc/init.d/oxidns")
	edited := strings.ReplaceAll(strings.ReplaceAll(readFile(t, unit), "retry=\"TERM/30/KILL/5\"\n", ""), "need net", "need net localmount")
	os.WriteFile(unit, []byte(edited), 0o755)
	r := h.run(t, "FAIL_GITHUB=yes")
	if r.code == 0 {
		t.Fatal("succeeded")
	}
	contains(t, r.stdout+r.stderr, "已被修改")
	if readFile(t, unit) != edited {
		t.Error("unit rewritten")
	}
}

func TestPartialStartFailureCanResumeWithoutRewritingConfigs(t *testing.T) {
	h := newGatewayHost(t, hostOpts{})
	failed := h.run(t, "FAIL_START=oxidns")
	if failed.code == 0 {
		t.Fatal("succeeded")
	}
	before := readFile(t, h.path("etc/mihomo/config.yaml"))
	contains(t, failed.stderr, "重新运行同一命令")
	if r := h.run(t); r.code != 0 {
		t.Fatal(r.stdout + r.stderr)
	}
	if readFile(t, h.path("etc/mihomo/config.yaml")) != before {
		t.Error("config rewritten")
	}
}

func TestPartialForeignLayoutIsRejectedWithoutOverwrite(t *testing.T) {
	h := newGatewayHost(t, hostOpts{})
	path := writeFile(t, h.path("etc/mihomo/config.yaml"), "external config", 0o644)
	if r := h.run(t); r.code == 0 {
		t.Fatal("succeeded")
	}
	if readFile(t, path) != "external config" || exists(h.path("etc/oxidns")) || exists(h.path("etc/mihomo/.configflow-gateway-core-v1")) {
		t.Error("files")
	}
}

func TestCompleteExistingLayoutAttachesWithoutMutatingCores(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newGatewayHost(t, hostOpts{manager: manager})
		if r := h.run(t); r.code != 0 {
			t.Fatal(r.stderr)
		}
		snapshots := map[string]string{}
		for _, kind := range []string{"mihomo", "oxidns"} {
			os.Remove(h.path("etc/" + kind + "/.configflow-gateway-core-v1"))
			path := writeFile(t, h.path("etc/"+kind+"/config.yaml"), "# existing external config\n", 0o644)
			snapshots[path] = readFile(t, path)
			unit := h.path("etc/init.d/" + kind)
			if manager == "systemd" {
				unit = h.path("etc/systemd/system/" + kind + ".service")
			}
			snapshots[unit] = readFile(t, unit)
			snapshots[h.path("usr/local/bin/"+kind)] = readFile(t, h.path("usr/local/bin/"+kind))
		}
		h.clearEvents(t)
		r := h.run(t, "FAIL_GITHUB=yes")
		if r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		contains(t, r.stdout, "保留并接入已有")
		sameFiles(t, snapshots)
		log := h.eventsText(t)
		notContains(t, log, "download https://github.com/")
		if strings.Count(log, "configflow-agent-linux-") != 1 {
			t.Error("agent downloads")
		}
		notContains(t, log, "systemctl start mihomo")
		notContains(t, log, "rc-service oxidns start")
	})
}

type fileState struct {
	data  string
	mtime int64
	mode  os.FileMode
}

func statFile(t *testing.T, path string) fileState {
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fileState{readFile(t, path), info.ModTime().UnixNano(), info.Mode()}
}

func githubDownloads(log string) []string {
	var out []string
	for _, line := range lines(log) {
		if strings.HasPrefix(line, "download https://github.com/") {
			out = append(out, line)
		}
	}
	return out
}

func TestOneExternalCoreIsPreservedWhileOnlyMissingPeerIsInstalled(t *testing.T) {
	for _, existing := range []string{"mihomo", "oxidns"} {
		h := newGatewayHost(t, hostOpts{})
		if r := h.run(t); r.code != 0 {
			t.Fatal(r.stderr)
		}
		missing := "oxidns"
		if existing == "oxidns" {
			missing = "mihomo"
		}
		os.Remove(h.path("etc/" + existing + "/.configflow-gateway-core-v1"))
		original := map[string]fileState{}
		for _, p := range []string{h.path("etc/" + existing + "/config.yaml"), h.path("usr/local/bin/" + existing), h.path("etc/systemd/system/" + existing + ".service")} {
			original[p] = statFile(t, p)
		}
		os.RemoveAll(h.path("etc/" + missing))
		os.Remove(h.path("usr/local/bin/" + missing))
		os.Remove(h.path("etc/systemd/system/" + missing + ".service"))
		var active []string
		for _, s := range strings.Fields(readFile(t, h.path("active"))) {
			if s != missing {
				active = append(active, s)
			}
		}
		writeFile(t, h.path("active"), strings.Join(active, " "), 0o644)
		h.clearEvents(t)
		r := h.run(t)
		if r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		log := h.eventsText(t)
		downloads := githubDownloads(log)
		want := "oxidns-"
		if missing == "mihomo" {
			want = "mihomo-linux-"
		}
		if len(downloads) != 1 || !strings.Contains(downloads[0], want) {
			t.Error(downloads)
		}
		for p, state := range original {
			if statFile(t, p) != state {
				t.Errorf("%s changed", p)
			}
		}
		notContains(t, log, "systemctl start "+existing)
		if !exists(h.path("etc/"+missing+"/.configflow-gateway-core-v1")) || readFile(t, h.path("ip_forward")) != "0\n" {
			t.Error("missing core or forwarding")
		}
	}
}

func coreChecks(log, kind string) []string {
	var out []string
	for _, line := range lines(log) {
		if strings.HasPrefix(line, "core-check "+kind+" ") {
			out = append(out, line)
		}
	}
	return out
}

func TestOwnedMissingBinaryDownloadsOnlyThatCoreAndChecksPreservedConfig(t *testing.T) {
	for _, kind := range []string{"mihomo", "oxidns"} {
		h := newGatewayHost(t, hostOpts{})
		if r := h.run(t); r.code != 0 {
			t.Fatal(r.stderr)
		}
		binary := h.path("usr/local/bin/" + kind)
		os.Remove(binary)
		config := writeFile(t, h.path("etc/"+kind+"/config.yaml"), "# retained published config\n", 0o644)
		h.clearEvents(t)
		r := h.run(t)
		if r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		log := h.eventsText(t)
		downloads := githubDownloads(log)
		want := "oxidns-"
		if kind == "mihomo" {
			want = "mihomo-linux-"
		}
		if len(downloads) != 1 || !strings.Contains(downloads[0], want) {
			t.Error(downloads)
		}
		checks := coreChecks(log, kind)
		if readFile(t, config) != "# retained published config\n" || !exists(binary) || len(checks) != 1 || !strings.Contains(checks[0], config) {
			t.Errorf("%s: %v", kind, checks)
		}
	}
}

func TestOwnedMissingConfigChecksBootstrapWithExistingBinaryWithoutGithub(t *testing.T) {
	for _, kind := range []string{"mihomo", "oxidns"} {
		h := newGatewayHost(t, hostOpts{})
		if r := h.run(t); r.code != 0 {
			t.Fatal(r.stderr)
		}
		config := h.path("etc/" + kind + "/config.yaml")
		os.Remove(config)
		binary := h.path("usr/local/bin/" + kind)
		original := readFile(t, binary)
		h.clearEvents(t)
		r := h.run(t, "FAIL_GITHUB=yes")
		if r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		log := h.eventsText(t)
		notContains(t, log, "download https://github.com/")
		checks := coreChecks(log, kind)
		if readFile(t, binary) != original || !exists(config) || len(checks) != 1 || !strings.Contains(checks[0], "/"+kind+".yaml") || strings.Contains(checks[0], config) {
			t.Errorf("%s: %v", kind, checks)
		}
	}
}

func TestPartialRecoveryNativeCheckFailurePreservesMissingCore(t *testing.T) {
	for _, kind := range []string{"mihomo", "oxidns"} {
		h := newGatewayHost(t, hostOpts{})
		if r := h.run(t); r.code != 0 {
			t.Fatal(r.stderr)
		}
		binary := h.path("usr/local/bin/" + kind)
		os.Remove(binary)
		config := h.path("etc/" + kind + "/config.yaml")
		original := readFile(t, config)
		h.clearEvents(t)
		if r := h.run(t, "FAIL_CHECK="+kind); r.code == 0 {
			t.Fatal("succeeded")
		}
		if exists(binary) || readFile(t, config) != original {
			t.Error("files")
		}
		notContains(t, h.eventsText(t), "systemctl enable")
	}
}

func TestExistingMihomoMustBeSupportedStableReleaseBeforeAnyInstallChanges(t *testing.T) {
	for _, version := range []string{"v1.19.32-alpha", "v1.19.32-custom", "v1.18.9", "alpha"} {
		h := newGatewayHost(t, hostOpts{})
		if r := h.run(t); r.code != 0 {
			t.Fatal(r.stderr)
		}
		before := map[string]string{}
		for _, kind := range []string{"mihomo", "oxidns"} {
			before[h.path("etc/"+kind+"/config.yaml")] = readFile(t, h.path("etc/"+kind+"/config.yaml"))
		}
		h.clearEvents(t)
		r := h.run(t, "EXISTING_MIHOMO_VERSION="+version)
		if r.code == 0 {
			t.Fatalf("%s accepted", version)
		}
		contains(t, r.stderr, "稳定版")
		notContains(t, h.eventsText(t), "systemctl enable")
		sameFiles(t, before)
	}
}

// withGatewayDependencyDelta is with_gateway_dependency_delta(expected).
func withGatewayDependencyDelta(t *testing.T, expected string) string {
	d := deltas(t)
	require(t, strings.Count(expected, d.GatewayOldNeed) == 1, "old need() missing")
	helper := withoutReviewedInstallerDeltas(t, AgentInstallDependencies, false)
	expected = strings.Replace(expected, d.GatewayOldNeed, helper, 1)
	start := strings.Index(expected, "for dependency in ip nft ss tar gzip")
	end := strings.Index(expected, "[ -c /dev/net/tun ]")
	require(t, start >= 0 && end > start && strings.Contains(expected[start:end], "apk add --no-cache tzdata"), "dependency loop")
	return expected[:start] + d.GatewayDependencyCall + expected[end:]
}

func TestSelectiveGatewayScriptIsByteIdenticalToThePreviousRelease(t *testing.T) {
	expected := withGatewayDependencyDelta(t, releaseBaseline(t, "gateway_install_selective_0ef9e89.sh"))
	if got := withoutReviewedInstallerDeltas(t, ok(gatewayGenerate(nil)), false); got != expected {
		t.Fatalf("selective gateway script differs from the baseline (%d vs %d bytes)", len(got), len(expected))
	}
	notContains(t, expected, "CONFIGFLOW_INSTALLER_FORWARDING")
}

func TestOrdinaryAIOScriptIsThePreviousReleasePlusIntentionalForwardingContextDelta(t *testing.T) {
	expected := releaseBaseline(t, "go_aio_install_0ef9e89.sh")
	for _, p := range deltas(t).OrdinaryAIOForwardingDeltas {
		require(t, strings.Count(expected, p[0]) == 1, "delta %q", p[0])
		expected = strings.Replace(expected, p[0], p[1], 1)
	}
	o := DefaultAIOOptions("https://cf.test", "ordinary")
	o.AgentIP = "192.168.8.3"
	o.BinaryDownloadURL = "https://cf.test/api/agents/download"
	script := ok(RenderAIOInstall(o))
	if got := withoutReviewedInstallerDeltas(t, script, false); got != expected {
		t.Fatalf("ordinary AIO script differs from the baseline (%d vs %d bytes)", len(got), len(expected))
	}
	if strings.Count(script, "export CONFIGFLOW_INSTALLER_FORWARDING\n") != 1 || strings.Count(script, "unset CONFIGFLOW_INSTALLER_FORWARDING\n") != 1 {
		t.Error("forwarding context")
	}
}

func TestAssumedTunPreparationIsOnlyForTheGatewayInstaller(t *testing.T) {
	// tun_prepare_assume_enabled=1 (an int) is a Python type case ruled out by Go's bool.
	for i, mutate := range []func(*AIOInstallOptions){
		func(o *AIOInstallOptions) { o.TunPrepareAssumeEnabled = true },
		func(o *AIOInstallOptions) {
			o.TunPrepareAssumeEnabled, o.ManageTun, o.NonInteractive = true, false, true
			o.VerifiedAgentPath = "$GATEWAY_STAGE/configflow-agent"
		},
	} {
		o := DefaultAIOOptions("https://cf.test", "ordinary")
		mutate(&o)
		if _, err := RenderAIOInstall(o); err == nil {
			t.Errorf("case %d accepted", i)
		} else if _, isValue := err.(*ValueError); !isValue {
			t.Errorf("case %d: %T", i, err)
		}
	}
}

func TestFullTunScriptPersistsForwardingAndPreparesTun(t *testing.T) {
	// generate(full_tun="yes") is a Python type case ruled out by Go's bool.
	script := ok(gatewayGenerate(func(o *GatewayInstallOptions) { o.FullTun = true }))
	for _, needle := range []string{"prepare_ipv4_forwarding", "install_ipv4_forwarding_hook", "persist_ipv4_forwarding",
		"-prepare-tun -assume-tun-enabled;", "$3>=40", "Agent 1.8.40-go 或更新",
		"if [ -e /opt/configflow-agent/mihomo/linux-gateway.json ] || [ -L /opt/configflow-agent/mihomo/linux-gateway.json ]",
		"# ConfigFlow Linux gateway installation. Full-TUN router split",
		"请回到 ConfigFlow 安装向导，继续发布 Mihomo、发布 OxiDNS，再检查转发与 TUN。",
		"agent_ensure_dependencies ha_ip nft ss tar gzip sha256sum cmp zoneinfo",
		`"$GATEWAY_STAGE/configflow-agent" -version >/dev/null`,
		"agent_ensure_dependencies zoneinfo sysctl cmp ln"} {
		contains(t, script, needle)
	}
	notContains(t, script, "CONFIGFLOW_FORWARDING_OWNER")
	notContains(t, script, "$3>=25")
	if strings.Count(script, "CONFIGFLOW_INSTALLER_FORWARDING=persist\nexport CONFIGFLOW_INSTALLER_FORWARDING\nif cf_environment_check \"$BINARY_TMP\"") != 1 ||
		strings.Count(script, "unset CONFIGFLOW_INSTALLER_FORWARDING\n") != 1 {
		t.Error("forwarding context")
	}
	if r := runShell(t, "sh", "-n", runOptions{args: []string{writeTemp(t, "script.sh", script)}}); r.code != 0 {
		t.Fatal(r.stderr)
	}
}

func TestFullTunInstallPersistsForwardingAndPreparesAssumedTun(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newGatewayHost(t, hostOpts{manager: manager, fullTun: true})
		r := h.run(t)
		if r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		if readFile(t, h.path("ip_forward")) != "1\n" ||
			readFile(t, h.path("etc/sysctl.d/99-configflow-aio-forward.conf")) != "# Managed by ConfigFlow Shell AIO IPv4 forwarding v1\nnet.ipv4.ip_forward=1\n" {
			t.Error("forwarding not persisted")
		}
		hook := h.path("opt/configflow-agent/mihomo/enable-ipv4-forwarding.sh")
		info, err := os.Stat(hook)
		if err != nil || info.Mode().Perm() != 0o755 {
			t.Error("hook mode")
		}
		unit := h.path("etc/init.d/configflow-agent-mihomo")
		if manager == "systemd" {
			unit = h.path("etc/systemd/system/configflow-agent-mihomo.service")
		}
		contains(t, readFile(t, unit), "/bin/sh "+hook)
		contains(t, h.eventsText(t), "prepare-tun:mihomo -prepare-tun -assume-tun-enabled")
		contains(t, readFile(t, h.path("etc/mihomo/config.yaml")), "enable: false")
		if exists(h.path("opt/configflow-agent/mihomo/linux-gateway.json")) {
			t.Error("gateway record")
		}
	})
}

func TestFullTunInstallRequiresTheForwardingAgent(t *testing.T) {
	h := newGatewayHost(t, hostOpts{fullTun: true})
	r := h.run(t, "AGENT_VERSION=1.8.39-go")
	if r.code == 0 {
		t.Fatal("succeeded")
	}
	contains(t, r.stdout+r.stderr, "1.8.40-go")
	if exists(h.path("etc/mihomo")) || readFile(t, h.path("ip_forward")) != "0\n" {
		t.Error("host changed")
	}
}

func TestFullTunInstallRefusesAHostStillOnTheGatewayTakeover(t *testing.T) {
	h := newGatewayHost(t, hostOpts{fullTun: true})
	writeFile(t, h.path("opt/configflow-agent/mihomo/linux-gateway.json"), "{}", 0o644)
	r := h.run(t)
	if r.code == 0 {
		t.Fatal("succeeded")
	}
	contains(t, r.stdout+r.stderr, "中撤销接管")
	if readFile(t, h.path("ip_forward")) != "0\n" || exists(h.path("etc/mihomo")) {
		t.Error("host changed")
	}
	if exists(h.events) {
		notContains(t, h.eventsText(t), "sysctl")
	}
}
