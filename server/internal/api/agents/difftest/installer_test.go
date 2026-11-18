package difftest

// Flask↔Go differential of the installer routes (A08): install scripts and
// install-command links (generate → download through /api/agents/i/<token>),
// stale, expired, unknown and malformed links, a full link store, the core
// installer, the gateway network probe and the Docker Agent snippets.
//
//	rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v -run Installer ./internal/api/agents/difftest/'

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/installer"
	"configflow/internal/installer/installertest"
	"configflow/internal/jsonx"

	_ "modernc.org/sqlite"
)

type installerRequest struct {
	name        string
	method      string
	path        string
	admin       bool
	host        string
	headers     map[string]string
	body        string
	contentType string
	// download fetches the answer's download_path and compares it too.
	download bool
}

func sendInstaller(t *testing.T, base string, r installerRequest, token string) response {
	var body io.Reader
	if r.body != "" {
		body = strings.NewReader(r.body)
	}
	req, err := http.NewRequest(r.method, base+r.path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "panel.test:5001"
	if r.host != "" {
		req.Host = r.host
	}
	if r.contentType != "" {
		req.Header.Set("Content-Type", r.contentType)
	}
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	if r.admin {
		req.Header.Set("Authorization", token)
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s: %v", r.name, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return response{r.method, resp.StatusCode, resp.Header, data}
}

var installerHeaders = []string{"Content-Type", "Content-Disposition", "Cache-Control", "X-Content-Type-Options"}

func compareInstaller(py, gor response, mask []string) []string {
	py.header = httpxtest.O9ExpectedHeaders(py.header, gor.header)
	var diffs []string
	migrated := httpxtest.MigratedAPIError(py.method, py.status, gor.status, py.header, gor.header, py.body, gor.body)
	if py.status != gor.status {
		diffs = append(diffs, fmt.Sprintf("status python %d go %d", py.status, gor.status))
	}
	for _, name := range installerHeaders {
		// L1 D8 restores the exact Python plain-text body/status/type. The
		// approved error-header exception removes the download attachment name.
		if name == "Content-Disposition" && (py.status == 404 || py.status == 503) &&
			py.header.Get("Content-Type") == "text/plain; charset=utf-8" && gor.header.Get("Content-Type") == py.header.Get("Content-Type") &&
			py.header.Get(name) == `attachment; filename="configflow-install.sh"` && gor.header.Get(name) == "" {
			continue
		}
		if migrated && name == "Content-Type" {
			continue
		}
		if a, b := py.header.Get(name), gor.header.Get(name); a != b {
			diffs = append(diffs, fmt.Sprintf("header %s python %q go %q", name, a, b))
		}
	}
	if migrated {
		// Step B #3: exact error-envelope migration only.
	} else if strings.HasPrefix(py.header.Get("Content-Type"), "application/json") {
		a, errA := jsonx.Parse(py.body)
		b, errB := jsonx.Parse(gor.body)
		if obj, ok := b.(*jsonx.Object); ok && gor.status == 200 {
			if obj.Has("install_command") || obj.Has("verification_command") {
				raw, err := installertest.GuideBaseline(gor.body)
				if err != nil {
					return []string{err.Error()}
				}
				b, err = jsonx.Parse(raw)
				if err != nil {
					return []string{err.Error()}
				}
			}
			if script, ok := obj.Value("script").(string); ok {
				baseline, err := installertest.PythonBaseline(script)
				if err != nil {
					return []string{err.Error()}
				}
				obj.Set("script", baseline)
			}
		}
		if errA != nil || errB != nil || !jsonx.Equal(maskJSON(a, mask), maskJSON(b, mask)) {
			diffs = append(diffs, fmt.Sprintf("json python %.400s go %.400s", py.body, gor.body))
		}
	} else if py.status == 200 && gor.status == 200 && strings.HasPrefix(py.header.Get("Content-Type"), "text/yaml") {
		if !equalComposeYAML(string(py.body), string(gor.body)) {
			diffs = append(diffs, "Compose YAML semantics differ")
		}
	} else {
		body := string(gor.body)
		if gor.status == 200 && strings.HasPrefix(gor.header.Get("Content-Type"), "text/plain") {
			var err error
			body, err = installertest.PythonBaseline(body)
			if err != nil {
				return []string{err.Error()}
			}
		}
		if string(py.body) != body {
			diffs = append(diffs, fmt.Sprintf("body (%d vs %d bytes) %s", len(py.body), len(gor.body), firstLineDiff(string(py.body), body)))
		}
	}
	return diffs
}

func firstLineDiff(a, b string) string {
	la, lb := strings.SplitAfter(a, "\n"), strings.SplitAfter(b, "\n")
	for i := 0; i < len(la) || i < len(lb); i++ {
		var x, y string
		if i < len(la) {
			x = la[i]
		}
		if i < len(lb) {
			y = lb[i]
		}
		if x != y {
			return fmt.Sprintf("line %d python %q go %q", i+1, x, y)
		}
	}
	return ""
}

func qs(pairs ...string) string {
	values := url.Values{}
	for i := 0; i < len(pairs); i += 2 {
		values.Add(pairs[i], pairs[i+1])
	}
	return values.Encode()
}

func installerRequests() []installerRequest {
	script := func(name, query string, extra ...func(*installerRequest)) installerRequest {
		r := installerRequest{name: name, method: "GET", path: "/api/agents/install-script?" + query}
		for _, f := range extra {
			f(&r)
		}
		return r
	}
	var out []installerRequest
	// Public install script: every form option and odd values.
	out = append(out,
		script("script default", ""),
		script("script raw query", "name=a%20b&type=mihomo&port=8080&&=x&name=second"),
		script("script plus space", "name=a+b&server_url=+http://x+"),
		script("script forwarded", "type=oxidns", func(r *installerRequest) {
			r.headers = map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "cfg.example.test"}
		}),
		script("script forwarded proto only", "type=oxidns", func(r *installerRequest) {
			r.headers = map[string]string{"X-Forwarded-Proto": "https"}
		}),
		script("script forwarded latin-1 host", "type=oxidns", func(r *installerRequest) {
			r.headers = map[string]string{"X-Forwarded-Proto": "http", "X-Forwarded-Host": "h\xe9st"}
		}),
		script("script ipv6 host", "type=mosdns", func(r *installerRequest) { r.host = "[fd00::5]:5001" }),
		script("script host no port", "", func(r *installerRequest) { r.host = "panel.test" }),
	)
	for _, service := range []string{"mihomo", "oxidns", "mosdns", "aio", "shell", "", "MIHOMO"} {
		for _, extra := range [][]string{
			{}, {"install_core", "true"}, {"install_core", "false"}, {"install_core", "yes"},
			{"prepare_dns_ha", "true"}, {"prepare_dns_ha", "false"}, {"prepare_dns_ha", "1"},
			{"ipv4_forwarding", "true"}, {"ipv4_forwarding", "false"}, {"ipv4_forwarding", "on"},
			{"install_core", "true", "prepare_dns_ha", "true"}, {"install_core", "true", "ipv4_forwarding", "false"},
		} {
			pairs := append([]string{"type", service, "name", "node-1", "server_url", "http://10.0.0.2:5001", "agent_ip", " 10.0.0.9 "}, extra...)
			out = append(out, script(fmt.Sprintf("script %s %v", service, extra), qs(pairs...)))
		}
	}
	for _, agentType := range []string{"go", "GO", " Go ", "shell", "SHELL", "python", "", "go\u3000", "\u0130"} {
		out = append(out, script("script agent_type "+agentType, qs("agent_type", agentType)))
		out = append(out, script("script agent_type core "+agentType, qs("agent_type", agentType, "install_core", "true")))
	}
	for _, port := range []string{"8080", " 8080 ", "0080", "１２３", "8_080", "abc", "", "0", "65536", "-1", "1053", "53",
		strings.Repeat("9", 4301), strings.Repeat("1", 4400) + "x"} {
		out = append(out, script("script port "+port[:min(len(port), 12)], qs("port", port)))
		out = append(out, script("script port core "+port[:min(len(port), 12)], qs("port", port, "install_core", "true")))
		out = append(out, script("script port oxidns core "+port[:min(len(port), 12)], qs("port", port, "install_core", "true", "type", "oxidns")))
		out = append(out, script("script health timeout "+port[:min(len(port), 12)], qs("deployment_health_timeout", port)))
	}
	for _, name := range []string{"My-Agent", "bad name", "x\n", "ünï", "", strings.Repeat("A", 200)} {
		out = append(out, script("script name "+name, qs("name", name)))
		out = append(out, script("script aio name "+name, qs("name", name, "type", "aio")))
	}
	out = append(out,
		script("script custom lifecycle", qs("service_manager", "command", "stop_command", " stop x ", "start_command", "start 'y'",
			"status_command", "status $z", "restart_command", "docker restart m", "service_unit", " clash.service ",
			"service_binary", "/opt/clash", "health_url", "http://127.0.0.1:9090/\"x\"", "health_dns_address", "127.0.0.1:53",
			"health_dns_name", "例子.test", "config_path", "/opt/clash/config.yaml")),
		script("script restart only", qs("restart_command", "docker restart m")),
		script("script systemd unit", qs("service_manager", "systemd", "service_unit", "mihomo.service", "restart_command", "")),
		script("script openrc", qs("service_manager", "openrc", "restart_command", "rc-service mihomo restart", "ipv4_forwarding", "true")),
		script("script runit", qs("service_manager", "runit")),
		script("script config path empty", qs("config_path", "")),
		script("script server url odd", qs("server_url", " http://h/it's/{config_json}/$(id) ", "agent_ip", "\u2028x")),
		script("script server url unicode", qs("server_url", "http://例子.测试:8080", "type", "aio")),
		script("script aio custom", qs("type", "aio", "mihomo_agent_port", "9090", "oxidns_agent_port", "9091",
			"mihomo_config_path", " /etc/clash/config.yaml ", "oxidns_config_path", "/srv/oxidns/c.yaml",
			"mihomo_service_unit", "clash.service", "oxidns_service_unit", "oxi", "mihomo_service_binary", "/usr/bin/clash",
			"oxidns_service_binary", " oxidns ")),
		script("script aio core", qs("type", "aio", "install_core", "true")),
		script("script aio core custom", qs("type", "aio", "install_core", "true", "mihomo_service_unit", "clash")),
		script("script aio ports bad", qs("type", "aio", "mihomo_agent_port", " 8080", "oxidns_agent_port", "8082")),
		script("script aio same ports", qs("type", "aio", "mihomo_agent_port", "8082")),
		script("script aio agent dir", qs("type", "aio", "mihomo_config_path", "/opt/configflow-agent/m.yaml")),
		script("script ha core", qs("type", "oxidns", "prepare_dns_ha", "true", "install_core", "true", "port", "8082")),
		script("script ha custom", qs("type", "oxidns", "prepare_dns_ha", "true", "service_manager", "command")),
		script("script invalid utf-8", "name=%ff%fe&server_url=http://x/%e4%b8"),
	)
	// Docker Agent snippets (panel login required).
	docker := func(kind, name, query string) installerRequest {
		return installerRequest{name: kind + " " + name, method: "GET", admin: true, path: "/api/agents/docker-agent-" + kind + "?" + query}
	}
	for _, kind := range []string{"compose", "run"} {
		out = append(out,
			installerRequest{name: kind + " no login", method: "GET", path: "/api/agents/docker-agent-" + kind},
			docker(kind, "default", ""),
			docker(kind, "bridge all", qs("server_url", "http://10.0.0.2:5001", "agent_name", "box", "agent_ip", "10.0.0.5",
				"network_mode", "bridge", "enable_mihomo", "TRUE", "enable_mosdns", "true", "mihomo_port", "７８９０", "mosdns_port", "x")),
			docker(kind, "oxidns bridge", qs("enable_mihomo", "false", "enable_oxidns", "true", "network_mode", "bridge",
				"oxidns_port", "9000", "data_dir", "/srv/cf data")),
			docker(kind, "mosdns+oxidns", qs("enable_mosdns", "true", "enable_oxidns", "true")),
			docker(kind, "bad name", qs("agent_name", "bad name")),
			docker(kind, "name newline", qs("agent_name", "a\n")),
			docker(kind, "odd url", qs("server_url", "http://h/it's $(x)", "data_dir", "$HOME/a:b")),
			docker(kind, "ha host", qs("enable_dns_ha", "true", "enable_mihomo", "false", "enable_oxidns", "true",
				"agent_ip", "192.168.1.53", "ha_host_id_path", " /etc/machine-id ")),
			docker(kind, "ha macvlan", qs("enable_dns_ha", "true", "enable_mihomo", "false", "enable_oxidns", "true",
				"network_mode", "macvlan", "agent_ip", "192.168.1.53", "ha_parent", " eth0 ", "ha_subnet", "192.168.1.0/24",
				"ha_gateway", "192.168.1.1", "data_dir", "/srv/ha data")),
			docker(kind, "ha macvlan netmask", qs("enable_dns_ha", "true", "enable_mihomo", "false", "enable_oxidns", "true",
				"network_mode", "macvlan", "agent_ip", "192.168.1.53", "ha_parent", "eth0", "ha_subnet", "192.168.1.0/255.255.255.0")),
			docker(kind, "ha bad subnet", qs("enable_dns_ha", "true", "enable_mihomo", "false", "enable_oxidns", "true",
				"network_mode", "macvlan", "agent_ip", "192.168.1.53", "ha_parent", "eth0", "ha_subnet", "192.168.1.5/24")),
			docker(kind, "ha bridge", qs("enable_dns_ha", "true", "enable_mihomo", "false", "enable_oxidns", "true",
				"network_mode", "bridge", "agent_ip", "192.168.1.53")),
			docker(kind, "ha low port", qs("enable_dns_ha", "true", "enable_mihomo", "false", "enable_oxidns", "true",
				"agent_ip", "192.168.1.53", "oxidns_port", "80")),
			docker(kind, "ha bad port text", qs("enable_dns_ha", "true", "enable_mihomo", "false", "enable_oxidns", "true",
				"agent_ip", "192.168.1.53", "oxidns_port", "eighty")),
			docker(kind, "ha identity", qs("enable_dns_ha", "true", "enable_mihomo", "false", "enable_oxidns", "true",
				"agent_ip", "192.168.1.53", "ha_host_id_path", "relative")),
		)
	}
	// Core installer, probe.
	out = append(out,
		installerRequest{name: "core script", method: "GET", path: "/api/agents/core-install-script"},
		installerRequest{name: "probe", method: "GET", path: "/api/agents/gateway-network-probe.sh"},
	)
	return out
}

// commandRequests create install-command links; each answer's download is
// compared as well.
func commandRequests() []installerRequest {
	post := func(name, body string) installerRequest {
		return installerRequest{name: name, method: "POST", path: "/api/agents/install-command", admin: true,
			contentType: "application/json", body: body, download: true}
	}
	core := func(name, body string) installerRequest {
		return installerRequest{name: name, method: "POST", path: "/api/agents/core-install-command", admin: true,
			contentType: "application/json", body: body, download: true}
	}
	return []installerRequest{
		{name: "command no login", method: "POST", path: "/api/agents/install-command", contentType: "application/json", body: `{}`},
		post("command empty", `{}`),
		post("command mihomo", `{"name":"node-1","type":"mihomo","port":8080,"agent_ip":"10.0.0.9","server_url":"http://10.0.0.2:5001","ipv4_forwarding":false}`),
		post("command oxidns ha core", `{"name":"ha-1","type":"oxidns","port":8082,"install_core":true,"prepare_dns_ha":true,"server_url":"http://10.0.0.2:5001"}`),
		post("command aio core", `{"name":"box","type":"aio","install_core":true,"mihomo_agent_port":9090,"oxidns_agent_port":9091}`),
		post("command no server url", `{"name":"auto","type":"mosdns"}`),
		post("command unicode", `{"name":"n","agent_ip":"\u00e9\u2028\ud83d\ude00","health_dns_name":"例子"}`),
		post("command unknown key", `{"name":"n","extra":1}`),
		post("command list", `[1]`),
		post("command null", `null`),
		post("command bool as string", `{"install_core":"true"}`),
		post("command bool as int", `{"prepare_dns_ha":1}`),
		post("command port float", `{"port":8080.0}`),
		post("command port bool", `{"port":true}`),
		post("command port zero", `{"port":0}`),
		post("command port big", `{"port":100000000000000000000000}`),
		post("command port string", `{"port":"8080"}`),
		post("command name number", `{"name":5}`),
		post("command name nul", `{"name":"a\u0000b"}`),
		post("command name long", `{"name":"`+strings.Repeat("é", 4097)+`"}`),
		post("command name 4096", `{"name":"`+strings.Repeat("é", 4096)+`"}`),
		post("command agent type shell", `{"agent_type":"shell"}`),
		post("command agent type Go", `{"agent_type":"Go"}`),
		post("command agent type go", `{"agent_type":"go","type":"oxidns"}`),
		post("command invalid render", `{"type":"aio","mihomo_agent_port":8082}`),
		post("command digits limit", `{"port":`+strings.Repeat("1", 4301)+`}`),
		post("command duplicate key", `{"name":"a","name":"b"}`),
		post("command bad json", `{"name":`),
		post("command bom", "\ufeff{}"),
		{name: "command text type", method: "POST", path: "/api/agents/install-command", admin: true, contentType: "text/plain", body: `{}`},
		post("command too large", `{"name":"`+strings.Repeat("a", 33*1024)+`"}`),
		core("core both", `{"target":"both"}`),
		core("core mihomo", `{"target":"mihomo"}`),
		core("core oxidns", `{"target":"oxidns"}`),
		core("core bad target", `{"target":"mosdns"}`),
		core("core extra key", `{"target":"both","x":1}`),
		core("core list target", `{"target":["both"]}`),
		core("core too large", `{"target":"`+strings.Repeat("b", 1100)+`"}`),
		{name: "core no login", method: "POST", path: "/api/agents/core-install-command", contentType: "application/json", body: `{"target":"both"}`},
	}
}

func snapshotsDB(t *testing.T, dataDir string) *sql.DB {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dataDir, "install-commands", "snapshots.sqlite3")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func execDB(t *testing.T, dataDir, statement string, args ...any) {
	db := snapshotsDB(t, dataDir)
	defer db.Close()
	if _, err := db.Exec(statement, args...); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
}

type snapshotRow struct {
	script    string
	expires   int64
	generator sql.NullString
}

func snapshotRows(t *testing.T, dataDir string) map[string]snapshotRow {
	db := snapshotsDB(t, dataDir)
	defer db.Close()
	rows, err := db.Query("SELECT token, script, expires_at, generator FROM snapshots")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]snapshotRow{}
	for rows.Next() {
		var token string
		var row snapshotRow
		if err := rows.Scan(&token, &row.script, &row.expires, &row.generator); err != nil {
			t.Fatal(err)
		}
		out[token] = row
	}
	return out
}

func TestInstallerDifferentialAgainstFlask(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	base := t.TempDir()
	seedDir, pyDir, goDir := filepath.Join(base, "seed"), filepath.Join(base, "python"), filepath.Join(base, "go")
	for _, dir := range []string{seedDir, pyDir, goDir} {
		os.MkdirAll(dir, 0o755)
	}
	seed(t, seedDir)
	copyDir(t, seedDir, pyDir)
	copyDir(t, seedDir, goDir)
	pyBase, stopPython := startPython(t, pyDir)
	defer stopPython()
	goBase, stopGo := startGo(t, goDir)
	defer stopGo()
	pyToken, goToken := login(t, pyBase), login(t, goBase)
	failed, same := 0, 0
	check := func(name string, py, gor response, mask ...string) {
		if diffs := compareInstaller(py, gor, mask); len(diffs) > 0 {
			failed++
			t.Errorf("DIFF %s:\n  %s", name, strings.Join(diffs, "\n  "))
		} else {
			same++
			t.Logf("same %-50.50s → %d %d bytes", name, py.status, len(py.body))
		}
	}
	both := func(r installerRequest) (response, response) {
		return sendInstaller(t, pyBase, r, pyToken), sendInstaller(t, goBase, r, goToken)
	}
	download := func(path string) installerRequest {
		return installerRequest{name: "download", method: "GET", path: path}
	}

	// Links before the store exists: 404 and no database file is created.
	for _, token := range []string{"AAAAAAAAAAAAAAAAAAAAAA", "short", "AAAAAAAAAAAAAAAAAAAAA%3D", "A%2FAAAAAAAAAAAAAAAAAAAA"} {
		py, gor := both(download("/api/agents/i/" + token))
		check("before store "+token, py, gor)
	}
	for _, dir := range []string{pyDir, goDir} {
		if _, err := os.Stat(filepath.Join(dir, "install-commands")); !os.IsNotExist(err) {
			t.Errorf("%s: install-commands exists before any link (%v)", dir, err)
		}
	}

	for _, r := range installerRequests() {
		py, gor := both(r)
		if strings.HasSuffix(r.name, " name newline") {
			// Coordinator decision 6: Python's validate_agent_name lets one
			// trailing "\n" through; Go refuses it like any invalid name.
			if py.status == 200 && gor.status == 400 && strings.Contains(string(gor.body), "Invalid agent name: only alphanumeric characters") {
				t.Logf("deliberate difference (decision 6): %s — python 200, go 400", r.name)
			} else {
				failed++
				t.Errorf("deliberate difference changed: %s python %d go %d %.200s", r.name, py.status, gor.status, gor.body)
			}
			continue
		}
		check(r.name, py, gor)
	}
	scripts := map[string][2]string{}
	for _, r := range commandRequests() {
		py, gor := both(r)
		check(r.name, py, gor, "download_path", "expires_at")
		if !r.download || py.status != 200 || gor.status != 200 {
			continue
		}
		pyAnswer, _ := jsonx.Parse(py.body)
		goAnswer, _ := jsonx.Parse(gor.body)
		pyPath := pyAnswer.(*jsonx.Object).GetString("download_path")
		goPath := goAnswer.(*jsonx.Object).GetString("download_path")
		if !strings.HasPrefix(goPath, "/api/agents/i/") || len(goPath) != len("/api/agents/i/")+22 {
			t.Errorf("%s: go download_path %q", r.name, goPath)
		}
		pyExpires := pyAnswer.(*jsonx.Object).GetInt("expires_at")
		goExpires := goAnswer.(*jsonx.Object).GetInt("expires_at")
		if d := goExpires - pyExpires; d < -5 || d > 5 || goExpires-time.Now().Unix() > installer.TTLSeconds {
			t.Errorf("%s: expires_at python %d go %d", r.name, pyExpires, goExpires)
		}
		pyDown := sendInstaller(t, pyBase, download(pyPath), "")
		goDown := sendInstaller(t, goBase, download(goPath), "")
		check(r.name+" → download", pyDown, goDown)
		if string(goDown.body) != goAnswer.(*jsonx.Object).GetString("script") {
			t.Errorf("%s: go download differs from the previewed script", r.name)
		}
		scripts[r.name] = [2]string{strings.TrimPrefix(pyPath, "/api/agents/i/"), strings.TrimPrefix(goPath, "/api/agents/i/")}
	}

	// L1 D3: exact approved source deltas and each side's own release identity.
	pyRows, goRows := snapshotRows(t, pyDir), snapshotRows(t, goDir)
	for name, tokens := range scripts {
		a, b := pyRows[tokens[0]], goRows[tokens[1]]
		baseline, err := installertest.PythonBaseline(b.script)
		if err != nil || a.script != baseline || !a.generator.Valid || !b.generator.Valid ||
			a.generator.String != installertest.LegacyGeneratorFingerprint || b.generator.String != installer.GeneratorFingerprint {
			t.Errorf("stored row %s: python generator %v go %v, approved script equal %v (%v)", name, a.generator, b.generator, a.script == baseline, err)
		}
	}

	// A link from an older generator, one without a generator, an expired one
	// and an unknown one; each side's own database gets the same rows.
	now := time.Now().Unix()
	for _, dir := range []string{pyDir, goDir} {
		fingerprint := installer.GeneratorFingerprint
		if dir == pyDir {
			fingerprint = installertest.LegacyGeneratorFingerprint
		}
		execDB(t, dir, "INSERT INTO snapshots (token, script, expires_at, generator) VALUES (?, ?, ?, ?)",
			"STALEstaleSTALEstale00", "#!/bin/sh\necho old\n", now+3600, "0123456789abcdef0123456789abcdef")
		execDB(t, dir, "INSERT INTO snapshots (token, script, expires_at, generator) VALUES (?, ?, ?, NULL)",
			"LEGACYlegacyLEGACY_-01", "#!/bin/sh\necho legacy\n", now+3600)
		execDB(t, dir, "INSERT INTO snapshots (token, script, expires_at, generator) VALUES (?, ?, ?, ?)",
			"EXPIREDexpiredEXPIRED0", "#!/bin/sh\necho expired\n", now-1, fingerprint)
		execDB(t, dir, "INSERT INTO snapshots (token, script, expires_at, generator) VALUES (?, ?, ?, ?)",
			"CURRENTcurrentCURRENT0", "#!/bin/sh\necho current\n", now+3600, fingerprint)
	}
	for _, token := range []string{"STALEstaleSTALEstale00", "LEGACYlegacyLEGACY_-01", "EXPIREDexpiredEXPIRED0",
		"CURRENTcurrentCURRENT0", "UNKNOWNunknownUNKNOWN0", "CURRENTcurrentCURRENT", "CURRENTcurrentCURRENT0x"} {
		py, gor := both(download("/api/agents/i/" + token))
		check("link "+token, py, gor)
	}
	for _, dir := range []string{pyDir, goDir} {
		if _, ok := snapshotRows(t, dir)["EXPIREDexpiredEXPIRED0"]; ok {
			t.Errorf("%s: expired link not removed", dir)
		}
	}

	// A full store keeps existing links valid (503) until links expire.
	for _, dir := range []string{pyDir, goDir} {
		fingerprint := installer.GeneratorFingerprint
		if dir == pyDir {
			fingerprint = installertest.LegacyGeneratorFingerprint
		}
		count := len(snapshotRows(t, dir))
		for i := count; i < installer.MaxSnapshots; i++ {
			execDB(t, dir, "INSERT INTO snapshots (token, script, expires_at, generator) VALUES (?, ?, ?, ?)",
				fmt.Sprintf("FILL%018d", i), "x", now+60, fingerprint)
		}
	}
	full := commandRequests()[2]
	py, gor := both(full)
	check("store full", py, gor)
	coreFull := installerRequest{name: "core full", method: "POST", path: "/api/agents/core-install-command", admin: true,
		contentType: "application/json", body: `{"target":"both"}`}
	py, gor = both(coreFull)
	check("core store full", py, gor)
	for _, dir := range []string{pyDir, goDir} {
		execDB(t, dir, "UPDATE snapshots SET expires_at = ? WHERE token LIKE 'FILL%'", now-10)
	}
	py, gor = both(full)
	check("store after expiry", py, gor, "download_path", "expires_at")

	// A database from before the generator column: the column is added and
	// the old link is stale.
	for _, dir := range []string{pyDir, goDir} {
		path := filepath.Join(dir, "install-commands", "snapshots.sqlite3")
		os.Remove(path)
		db, err := sql.Open("sqlite", "file:"+path)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range []string{
			"CREATE TABLE snapshots (token TEXT PRIMARY KEY, script TEXT NOT NULL, expires_at INTEGER NOT NULL)",
			fmt.Sprintf("INSERT INTO snapshots VALUES ('OLDSCHEMAoldschema0000', 'old', %d)", now+3600),
		} {
			if _, err := db.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
		db.Close()
		os.Chmod(path, 0o644)
		os.Chmod(filepath.Dir(path), 0o755)
	}
	py, gor = both(download("/api/agents/i/OLDSCHEMAoldschema0000"))
	check("old schema link", py, gor)
	for _, dir := range []string{pyDir, goDir} {
		path := filepath.Join(dir, "install-commands", "snapshots.sqlite3")
		info, _ := os.Stat(path)
		dirInfo, _ := os.Stat(filepath.Dir(path))
		if info.Mode().Perm() != 0o600 || dirInfo.Mode().Perm() != 0o700 {
			t.Errorf("%s: modes %v %v", dir, info.Mode().Perm(), dirInfo.Mode().Perm())
		}
		var columns []string
		db := snapshotsDB(t, dir)
		rows, err := db.Query("SELECT name FROM pragma_table_info('snapshots')")
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var name string
			rows.Scan(&name)
			columns = append(columns, name)
		}
		rows.Close()
		db.Close()
		sort.Strings(columns)
		if strings.Join(columns, ",") != "expires_at,generator,script,token" {
			t.Errorf("%s: columns %v", dir, columns)
		}
	}
	t.Logf("installer routes: %d comparisons equal, %d differ", same, failed)
}
