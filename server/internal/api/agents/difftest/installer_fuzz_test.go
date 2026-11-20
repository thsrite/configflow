package difftest

// Randomised Flask↔Go differential of the installer routes (A08 review):
// seeded random query strings (raw bytes, percent escapes, "+", repeated and
// empty keys, shell metacharacters, Unicode, long values, malformed ports and
// addresses) for install-script and the Docker snippets, random JSON bodies
// for install-command (every field with every JSON type) and core-install-command.
// Set CONFIGFLOW_INSTALLER_FUZZ_ITERATIONS to select that many original
// cases per phase for profiling; unset runs the unchanged complete corpus.
// Lone surrogate escapes are left out: jsonx decodes them as U+FFFD (documented
// jsonx limitation, see jsonx/doc.go).
//
//	rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v -run InstallerFuzz ./internal/api/agents/difftest/'

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/repo"
)

type installerFuzz struct{ rng *rand.Rand }

var fuzzAtoms = []string{"", "a", "0", "mihomo", "oxidns", "mosdns", "aio", "go", "GO", "shell", "true", "false", "True",
	"eth0", "node-1", "$(id)", "`id`", "${HOME}", "a'b", `a"b`, "{config_json}", "{agent_port}", "é", "例子", "😀",
	"İ", "ß", "１２", "٣", "\u2047", "\uff0f", "\u00a0", "\u3000", "\u2028", "\u0085", "\x0b", "\x1c", "\t", "\n", "\r",
	"\x7f", ";", "|", "&", "\\", "#", "%", "+", "=", "/", ":", "@", "[", "]", " ", "8080", "53", "1053", "65535",
	"65536", "0", "-1", " 8082 ", "8_0", "0x1f", "192.168.1.53", "192.168.1.0/24", "192.168.1.1", "/etc/machine-id",
	"/etc/mihomo/config.yaml", "/opt/configflow-agent/x", "systemctl restart mihomo", "rc-service oxidns restart",
	"command", "systemd", "openrc", "http://10.0.0.2:5001", "https://[fd00::1]:5001/", "http://u:p@h#f", "macvlan",
	"host", "bridge", "clash.service", "\U0001e4f1"}

func (f installerFuzz) text(parts int) string {
	var b strings.Builder
	for i := f.rng.IntN(parts + 1); i > 0; i-- {
		if f.rng.IntN(10) < 8 {
			b.WriteString(fuzzAtoms[f.rng.IntN(len(fuzzAtoms))])
		} else {
			b.WriteRune(rune(0xa0 + f.rng.IntN(0xd000)))
		}
	}
	s := b.String()
	if f.rng.IntN(50) == 0 {
		s = strings.Repeat(s+"x", 100+f.rng.IntN(800))
	}
	return s
}

func (f installerFuzz) headerText(parts int) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(f.text(parts)))
}

// escape renders one query component: strict percent-encoding, loose (only
// the separators escaped, spaces as "+"), or raw bytes including invalid
// UTF-8 escapes.
func (f installerFuzz) escape(s string) string {
	switch f.rng.IntN(4) {
	case 0:
		return url.QueryEscape(s)
	case 1:
		var b strings.Builder
		for i := 0; i < len(s); i++ {
			c := s[i]
			switch {
			case c == ' ':
				b.WriteByte('+')
			case c == '&' || c == '#' || c == '%' || c == '+' || c == '=' || c < 0x21 || c >= 0x7f:
				fmt.Fprintf(&b, "%%%02X", c)
			default:
				b.WriteByte(c)
			}
		}
		return b.String()
	case 2:
		return url.PathEscape(s)
	default:
		extra := []string{"%ff", "%fe", "%e4%b8", "%c0%af", "%zz", "%", "%2", "%00", "+", "%2B", "%20", ";", "%0A"}
		return url.QueryEscape(s) + extra[f.rng.IntN(len(extra))]
	}
}

func (f installerFuzz) query(fields []string, values func(string) string) string {
	var pairs []string
	for _, field := range fields {
		if f.rng.IntN(3) != 0 {
			continue
		}
		pairs = append(pairs, f.escape(field)+"="+f.escape(values(field)))
		if f.rng.IntN(15) == 0 {
			pairs = append(pairs, f.escape(field)+"="+f.escape(f.text(2))) // repeated key: the first wins
		}
	}
	if f.rng.IntN(10) == 0 {
		pairs = append(pairs, fields[f.rng.IntN(len(fields))]) // key without "="
	}
	if f.rng.IntN(10) == 0 {
		pairs = append(pairs, "")
	}
	f.rng.Shuffle(len(pairs), func(i, j int) { pairs[i], pairs[j] = pairs[j], pairs[i] })
	return strings.Join(pairs, "&")
}

var scriptFields = []string{"name", "type", "port", "agent_ip", "config_path", "restart_command", "server_url",
	"agent_type", "install_core", "service_manager", "service_unit", "service_binary", "stop_command", "start_command",
	"status_command", "health_url", "health_dns_address", "health_dns_name", "deployment_health_timeout",
	"mihomo_agent_port", "oxidns_agent_port", "mihomo_config_path", "oxidns_config_path", "mihomo_service_unit",
	"oxidns_service_unit", "mihomo_service_binary", "oxidns_service_binary", "ipv4_forwarding", "prepare_dns_ha", "unknown"}

func (f installerFuzz) scriptValue(field string) string {
	pick := func(options ...string) string {
		if f.rng.IntN(4) == 0 {
			return f.text(3)
		}
		return options[f.rng.IntN(len(options))]
	}
	switch field {
	case "type":
		return pick("mihomo", "oxidns", "mosdns", "aio", "Mihomo", "")
	case "install_core", "ipv4_forwarding", "prepare_dns_ha":
		return pick("true", "false", "true", "false", "True", "1", "")
	case "agent_type":
		return pick("go", "GO", " go ", "shell", "Go\u3000", "")
	case "port", "mihomo_agent_port", "oxidns_agent_port", "deployment_health_timeout":
		return pick(strconv.Itoa(f.rng.IntN(70000)), "8080", "8082", "53", "1053", " 9090 ", "0", "-3", "１２", "8_0", "",
			strings.Repeat("9", 4301), "x")
	case "service_manager":
		return pick("", "systemd", "openrc", "command", "runit")
	case "server_url":
		return pick("http://10.0.0.2:5001", " https://cfg.test/ ", "http://h/it's", "http://[fd00::2]:5001", "")
	}
	return f.text(4)
}

var dockerFields = []string{"server_url", "agent_name", "agent_ip", "data_dir", "network_mode", "enable_mihomo",
	"enable_mosdns", "mihomo_port", "mosdns_port", "enable_oxidns", "oxidns_port", "enable_dns_ha", "ha_parent",
	"ha_subnet", "ha_gateway", "ha_host_id_path"}

func (f installerFuzz) dockerValue(field string) string {
	pick := func(options ...string) string {
		if f.rng.IntN(4) == 0 {
			return f.text(3)
		}
		return options[f.rng.IntN(len(options))]
	}
	switch field {
	case "network_mode":
		return pick("host", "bridge", "macvlan", "my net")
	case "enable_mihomo", "enable_mosdns", "enable_oxidns", "enable_dns_ha":
		return pick("true", "false", "TRUE", "1", "")
	case "mihomo_port", "mosdns_port", "oxidns_port":
		return pick(strconv.Itoa(f.rng.IntN(70000)), "8082", "1023", "1024", "65535", "65536", "７８９０", "x", "")
	case "agent_ip":
		return pick("192.168.1.53", "192.168.1.53", "10.0.0.5", "192.168.1.0", "127.0.0.1", "")
	case "ha_subnet":
		return pick("192.168.1.0/24", " 192.168.1.0/24 ", "192.168.1.0/255.255.255.0", "192.168.1.5/24", "10.0.0.0/8")
	case "ha_parent":
		return pick("eth0", " enp1s0 ", "a.b", "")
	case "ha_gateway":
		return pick("192.168.1.1", "", "192.168.1.53")
	}
	return f.text(4)
}

// jsonValue is a random JSON value for an install-command field.
func (f installerFuzz) jsonValue(field string) string {
	str := func(s string) string {
		return encodeStringJSON(s, f.rng.IntN(2) == 0)
	}
	switch f.rng.IntN(12) {
	case 0:
		return []string{"true", "false"}[f.rng.IntN(2)]
	case 1:
		return []string{"0", "1", "8080", "65535", "65536", "-1", "8080.0", "1e3", "53", "1053", "100000000000000000000"}[f.rng.IntN(11)]
	case 2:
		return []string{"null", "[]", "{}", `["a"]`, `{"a":1}`}[f.rng.IntN(5)]
	case 3:
		return str(strings.Repeat("é", 4095+f.rng.IntN(3)))
	case 4:
		return str("a\x00b")
	}
	switch field {
	case "install_core", "ipv4_forwarding", "prepare_dns_ha":
		return []string{"true", "false"}[f.rng.IntN(2)]
	case "port", "mihomo_agent_port", "oxidns_agent_port", "deployment_health_timeout":
		return strconv.Itoa(f.rng.IntN(70000))
	}
	return str(f.scriptValue(field))
}

func (f installerFuzz) commandBody() string {
	var parts []string
	fields := scriptFields[:len(scriptFields)-1]
	for _, field := range fields {
		if f.rng.IntN(3) == 0 {
			parts = append(parts, `"`+field+`":`+f.jsonValue(field))
		}
	}
	if f.rng.IntN(20) == 0 {
		parts = append(parts, `"extra":1`)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// validPairs is a mostly valid install-script parameter set whose free-text
// fields (URLs, addresses, commands, paths, binaries, health checks) carry
// random text, so the text reaches the rendered script.
func (f installerFuzz) validPairs() [][2]string {
	service := []string{"mihomo", "oxidns", "mosdns", "aio"}[f.rng.IntN(4)]
	pairs := [][2]string{{"type", service}}
	add := func(key, value string) { pairs = append(pairs, [2]string{key, value}) }
	maybe := func(key string, make func() string) {
		if f.rng.IntN(2) == 0 {
			add(key, make())
		}
	}
	urlText := func() string {
		return []string{"http://10.0.0.2:5001", "https://[fd00::2]:5001/", "http://h/it's $(id) `x` \\ \"q\"",
			"http://例子.测试/{config_json}", "http://u:p@h/#f", f.text(4)}[f.rng.IntN(6)]
	}
	maybe("server_url", urlText)
	maybe("agent_ip", func() string { return f.text(3) })
	maybe("name", func() string { return []string{"node-1", "A_b-9", "x"}[f.rng.IntN(3)] })
	installCore := f.rng.IntN(4) == 0 && service != "mosdns"
	if installCore {
		add("install_core", "true")
	}
	if service == "aio" {
		if !installCore {
			maybe("mihomo_config_path", func() string { return "/etc/m " + f.text(2) + "/c.yaml" })
			maybe("oxidns_config_path", func() string { return "/srv/o" + f.text(2) + "/c.yaml" })
			maybe("mihomo_service_binary", func() string { return f.text(3) + "m" })
			maybe("oxidns_service_binary", func() string { return "/usr/bin/" + f.text(2) + "o" })
			maybe("mihomo_service_unit", func() string { return []string{"clash", "m@x.service", "mihomo"}[f.rng.IntN(3)] })
		}
		maybe("mihomo_agent_port", func() string { return strconv.Itoa(2000 + f.rng.IntN(3000)) })
		return pairs
	}
	maybe("port", func() string { return strconv.Itoa(2000 + f.rng.IntN(60000)) })
	for _, key := range []string{"health_url", "health_dns_address", "health_dns_name"} {
		maybe(key, func() string { return f.text(3) })
	}
	if !installCore {
		maybe("config_path", func() string { return f.text(3) })
		maybe("service_binary", func() string { return f.text(3) })
		if f.rng.IntN(3) == 0 {
			add("service_manager", "command")
			add("restart_command", f.text(3))
			for _, key := range []string{"stop_command", "start_command", "status_command"} {
				add(key, f.text(3)+"x")
			}
		}
	}
	if service == "oxidns" && f.rng.IntN(3) == 0 {
		add("prepare_dns_ha", "true")
	}
	if service == "mihomo" && f.rng.IntN(3) == 0 {
		add("ipv4_forwarding", []string{"true", "false"}[f.rng.IntN(2)])
	}
	return pairs
}

func (f installerFuzz) validQuery() string {
	var parts []string
	for _, p := range f.validPairs() {
		parts = append(parts, url.QueryEscape(p[0])+"="+url.QueryEscape(p[1]))
	}
	return strings.Join(parts, "&")
}

func (f installerFuzz) validBody() string {
	var parts []string
	for _, p := range f.validPairs() {
		key := encodeStringJSON(p[0], true)
		var value string
		switch p[0] {
		case "install_core", "ipv4_forwarding", "prepare_dns_ha":
			value = p[1]
		case "port", "mihomo_agent_port", "oxidns_agent_port":
			value = p[1]
		default:
			value = encodeStringJSON(p[1], f.rng.IntN(2) == 0)
		}
		parts = append(parts, key+":"+value)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func TestInstallerFuzzAgainstFlask(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	base := t.TempDir()
	seedDir, pyDir, goDir := filepath.Join(base, "seed"), filepath.Join(base, "python"), filepath.Join(base, "go")
	for _, dir := range []string{seedDir, pyDir, goDir} {
		os.MkdirAll(dir, 0o755)
	}
	// Installer rendering has no dependency on monitoring history, existing
	// Agents or the large application template. Keep the same minimal persisted
	// configuration on both sides; registration issuance still uses the real repo.
	if _, err := repo.Open(seedDir, repo.Options{}); err != nil {
		t.Fatal(err)
	}
	copyDir(t, seedDir, pyDir)
	copyDir(t, seedDir, goDir)
	pyBase, stopPython := startPython(t, pyDir)
	defer stopPython()
	nativeDir := filepath.Join(base, "native-python")
	copyDir(t, seedDir, nativeDir)
	nativeBase, stopNative := startPythonArgs(t, nativeDir, "server/internal/api/agents/difftest/testdata/n810_installer_app.py")
	defer stopNative()
	nativeToken := login(t, nativeBase)
	nativeDownloads := map[string]string{}
	bindOriginal := n810InstallerBinding(t)

	// The real issuance owner expires prior capabilities in its normal write.
	// Advance only this request runtime, leaving JWT/link clocks and production
	// background-loop timers alone. No extra repository cleanup transaction.
	clock := lifecycletest.NewClock(time.Now())
	runtime := lifecycle.NewRuntime(lifecycle.Options{Clock: clock, Mode: lifecycle.ModeManual})
	runtime.Start(context.Background())
	defer runtime.Shutdown(context.Background())
	goBase, stopGo := startGoWithRequestRuntime(t, goDir, runtime)
	defer stopGo()
	pyToken, goToken := login(t, pyBase), login(t, goBase)
	f := installerFuzz{rand.New(rand.NewPCG(20261116, 8))}
	// Diagnostic limits select prefixes of each phase. Generate skipped inputs
	// too, so the fixed RNG stream and every later phase retain the full corpus.
	iterations := 0
	if raw := os.Getenv("CONFIGFLOW_INSTALLER_FUZZ_ITERATIONS"); raw != "" {
		var err error
		iterations, err = strconv.Atoi(raw)
		if err != nil || iterations < 1 {
			t.Fatal("CONFIGFLOW_INSTALLER_FUZZ_ITERATIONS must be a positive integer")
		}
	}
	selected := func(i int) bool { return iterations == 0 || i < iterations }
	type phaseTime struct {
		count                            int
		prepare, python, goHTTP, compare time.Duration
	}
	timings := map[string]*phaseTime{}
	defer func() {
		for _, kind := range []string{"install-script", "compose", "run", "install-command", "core-install-command"} {
			if v := timings[kind]; v != nil {
				t.Logf("installer timing %s count=%d prepare=%s python=%s go=%s compare=%s per_iteration=%s", kind, v.count, v.prepare, v.python, v.goHTTP, v.compare, (v.prepare+v.python+v.goHTTP+v.compare)/time.Duration(v.count))
			}
		}
	}()
	counts := map[string]int{}
	failed, same := 0, 0
	check := func(kind string, r installerRequest, mask ...string) (response, response) {
		timing := timings[kind]
		if timing == nil {
			timing = &phaseTime{}
			timings[kind] = timing
		}
		begin := time.Now()

		clock.Advance(25 * time.Hour)
		timing.prepare += time.Since(begin)
		begin = time.Now()
		py := sendInstaller(t, pyBase, r, pyToken)
		timing.python += time.Since(begin)
		begin = time.Now()
		gor := sendInstaller(t, goBase, r, goToken)
		timing.goHTTP += time.Since(begin)
		begin = time.Now()
		counts[fmt.Sprintf("%s %d", kind, py.status)]++
		diffs := compareInstaller(py, gor, mask)
		oracleResponse := py
		if len(diffs) > 0 && r.method == "GET" && py.status == 200 && gor.status == 200 {
			if canonical, changed := nativeQueryVariant(r.path); changed {
				alternative := r
				alternative.path = canonical
				expected := sendInstaller(t, pyBase, alternative, pyToken)
				oracleResponse = expected
				assertNativeOutputSyntax(t, kind, gor.body)
				diffs = compareInstaller(expected, gor, mask)
				if len(diffs) == 0 {
					counts["EXPECTED native invalid UTF-8 "+kind]++
				}
			}
		}
		if len(diffs) > 0 && kind == "compose" {
			if expected, ok := nativeWriterHTTPExpected(t, r.path, oracleResponse); ok {
				diffs = compareInstaller(expected, gor, mask)
				if len(diffs) == 0 {
					counts["EXPECTED native writer NEL compose"]++
				}
			}
		}
		if len(diffs) > 0 && approvedInstallerIntegerRefusal(r, py, gor) {
			diffs = nil
			counts["EXPECTED native integer "+kind]++
		}
		if len(diffs) > 0 && (kind == "install-script" || kind == "install-command") {
			nativeRequest := r
			if path, changed := nativeQueryVariant(r.path); changed {
				nativeRequest.path = path
			}
			expected := sendInstaller(t, nativeBase, nativeRequest, nativeToken)
			if next := compareInstaller(expected, gor, mask); len(next) == 0 {
				bindOriginal(r, py, mask)
				diffs = nil
				counts["EXPECTED native HTTP binding "+kind]++
			} else {
				diffs = append(diffs, "native policy reference: "+strings.Join(next, "; "))
			}
			if expected.status == 200 && kind == "install-command" {
				value, _ := parsePythonJSON(expected.body)
				nativeDownloads[r.body] = value.(*orderedObject).GetString("download_path")
			}
		}
		if len(diffs) > 0 {
			failed++
			if failed <= 30 {
				t.Errorf("DIFF %s %s %q:\n  %s", kind, r.path, r.body, strings.Join(diffs, "\n  "))
			}
		} else {
			same++
		}
		timing.compare += time.Since(begin)
		timing.count++
		return py, gor
	}
	for i := 0; i < 1500; i++ {
		query := f.query(scriptFields, f.scriptValue)
		if f.rng.IntN(2) == 0 {
			query = f.validQuery()
		}
		r := installerRequest{method: "GET", path: "/api/agents/install-script?" + query}
		if f.rng.IntN(5) == 0 {
			// Header values the Go client can send (no control characters);
			// non-ASCII text goes out as raw UTF-8 bytes (latin-1 on both servers).
			r.headers = map[string]string{"X-Forwarded-Proto": f.headerText(1), "X-Forwarded-Host": f.headerText(2)}
		}
		if f.rng.IntN(10) == 0 {
			// ASCII only: the Go client would IDNA-encode a Unicode Host.
			r.host = []string{"[fd00::5]:5001", "panel.test", "h:1", "x_y.test:80"}[f.rng.IntN(4)]
		}
		if selected(i) {
			check("install-script", r)
		}
	}
	for _, kind := range []string{"compose", "run"} {
		for i := 0; i < 700; i++ {
			r := installerRequest{method: "GET", admin: true, path: "/api/agents/docker-agent-" + kind + "?" + f.query(dockerFields, f.dockerValue)}
			if selected(i) {
				check(kind, r)
			}
		}
	}
	for i := 0; i < 1000; i++ {
		body := f.commandBody()
		if f.rng.IntN(2) == 0 {
			body = f.validBody()
		}
		r := installerRequest{method: "POST", path: "/api/agents/install-command", admin: true,
			contentType: "application/json", body: body}
		if !selected(i) {
			continue
		}
		py, gor := check("install-command", r, "download_path", "expires_at")
		if py.status == 200 && gor.status == 200 {
			pyAnswer, _ := parsePythonJSON(py.body)
			goAnswer, _ := parsePythonJSON(gor.body)
			pyDown := sendInstaller(t, pyBase, installerRequest{method: "GET", path: pyAnswer.(*orderedObject).GetString("download_path")}, "")
			goDown := sendInstaller(t, goBase, installerRequest{method: "GET", path: goAnswer.(*orderedObject).GetString("download_path")}, "")
			if reference, ok := nativeDownloads[r.body]; ok {
				pyDown = sendInstaller(t, nativeBase, installerRequest{method: "GET", path: reference}, "")
			}
			if diffs := compareInstaller(pyDown, goDown, nil); len(diffs) > 0 {
				failed++
				t.Errorf("DIFF download of %q:\n  %s", r.body, strings.Join(diffs, "\n  "))
			} else {
				same++
			}
			for _, dir := range []string{pyDir, goDir} {
				execDB(t, dir, "DELETE FROM snapshots")
			}
		}
	}
	for i := 0; i < 150; i++ {
		body := []string{`{"target":` + f.jsonValue("target") + `}`, `{"target":"both"}`, `{"target":"mihomo","x":1}`,
			`{"target":"oxidns"}`, `{}`, `[]`, f.commandBody()}[f.rng.IntN(7)]
		if !selected(i) {
			continue
		}
		py, _ := check("core-install-command", installerRequest{method: "POST", path: "/api/agents/core-install-command",
			admin: true, contentType: "application/json", body: body}, "download_path", "expires_at")
		if py.status == 200 {
			for _, dir := range []string{pyDir, goDir} {
				execDB(t, dir, "DELETE FROM snapshots")
			}
		}
	}
	if count := counts["EXPECTED native writer NEL compose"]; iterations == 0 && count != 4 {
		t.Errorf("explicit NEL writer expectations used %d times, want 4", count)
	}
	t.Logf("installer fuzz: %d comparisons equal, %d differ; statuses %v", same, failed, counts)
}
