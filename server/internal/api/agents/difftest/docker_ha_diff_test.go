package difftest

// Flask↔Go differential of the Docker HA routes (A09):
// POST /api/agents/docker-oxidns-ha-guide (check / inspect / verify /
// generate new and migrate, body limits and malformed bodies, profile
// header) and POST /api/agents/docker-oxidns-ha-enroll/<session_id>
// (issue, replay, refusals, expiry, renewal, changed identity, takeover,
// hand-edited installation records). Random values (session id, token,
// owner hash, expiry) are captured per side and replaced by placeholders;
// the installation records under DATA_DIR and the Agent records are
// compared after the requests. The Compose methods (A10) are compared in
// docker_ha_compose_diff_test.go.
//
//	rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v -run DockerHA ./internal/api/agents/difftest/'

import (
	"encoding/base64"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

var (
	sessionPattern = regexp.MustCompile(`docker-oxidns-ha-enroll/([0-9a-f]{32})`)
	tokenPattern   = regexp.MustCompile(`\\"token\\": \\"([A-Za-z0-9_-]{43})\\"`)
	ownerPattern   = regexp.MustCompile(`cf_owner=([0-9a-f]{64})`)
)

type haSide struct {
	base, token, dir string
	session, secret  string // the capability of the main installation
	agentID          string
}

// normalizeHA replaces this side's random values in an answer body.
func normalizeHA(body []byte) ([]byte, map[string]string) {
	text := string(body)
	found := map[string]string{}
	if m := sessionPattern.FindStringSubmatch(text); m != nil {
		found["session"] = m[1]
		text = strings.ReplaceAll(text, m[1], "<session>")
	}
	if m := tokenPattern.FindStringSubmatch(text); m != nil {
		found["token"] = m[1]
		text = strings.ReplaceAll(text, m[1], "<token>")
	}
	if m := ownerPattern.FindStringSubmatch(text); m != nil {
		found["owner"] = m[1]
		text = strings.ReplaceAll(text, m[1], "<owner>")
	}
	return []byte(text), found
}

func haBlock(fields map[string][2]string) string {
	order := []string{"kernel", "docker_endpoint", "docker_version", "daemon_os", "daemon_platform", "daemon_security",
		"compose", "ipv4", "udp53", "tcp53", "nesting", "machine_fingerprint"}
	defaults := map[string][2]string{"kernel": {"0", "Linux"}, "docker_endpoint": {"0", "local-unix"},
		"docker_version": {"0", `{"client":"29.9.2","server":"29.9.2"}`}, "daemon_os": {"0", "linux"},
		"daemon_platform": {"0", `{"os":"Arch Linux","kernel":"6.18.3"}`}, "daemon_security": {"0", `["name=seccomp"]`},
		"compose": {"0", "2.30.3"}, "ipv4": {"0", "2: eth0 inet 192.168.10.20/24 scope global eth0\n"},
		"udp53": {"0", ""}, "tcp53": {"0", ""}, "nesting": {"0", "none"}, "machine_fingerprint": {"0", strings.Repeat("a", 64)}}
	lines := []string{"CONFIGFLOW_DOCKER_HA_PREFLIGHT_V1_BEGIN"}
	for _, key := range order {
		v := defaults[key]
		if f, ok := fields[key]; ok {
			v = f
		}
		lines = append(lines, "CFHA1\t"+key+"\t"+v[0]+"\t"+base64.StdEncoding.EncodeToString([]byte(v[1])))
	}
	return strings.Join(append(lines, "CONFIGFLOW_DOCKER_HA_PREFLIGHT_V1_END"), "\n") + "\n"
}

func jsonText(v any) string {
	data, err := encodeDefaultJSON(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func haNewBody(pairs ...any) *orderedObject {
	body := oObj("server_url", "https://panel.example", "agent_name", "new-dns", "agent_ip", "192.168.50.20",
		"data_dir", "/srv/oxidns", "oxidns_port", 8082, "network_mode", "host", "ha_host_id_path", "/srv/host-id")
	body.Update(oObj(pairs...))
	return body
}

const haGuide = "/api/agents/docker-oxidns-ha-guide"

func guideReq(name string, body any, extra ...func(*request)) request {
	r := request{name: name, method: "POST", path: haGuide, admin: true, contentType: "application/json"}
	switch b := body.(type) {
	case string:
		r.body = b
	default:
		r.body = jsonText(b)
	}
	for _, f := range extra {
		f(&r)
	}
	return r
}

func haReadOnlyRequests() []request {
	block := haBlock(nil)
	var out []request
	out = append(out,
		guideReq("guide no login", oObj("action", "inspect"), func(r *request) { r.admin = false }),
		guideReq("guide empty body", ""),
		guideReq("guide not json", "{bad"),
		guideReq("guide list", "[]"),
		guideReq("guide string", `"x"`),
		guideReq("guide null", "null"),
		guideReq("guide bom", "\ufeff{\"action\": \"inspect\"}"),
		guideReq("guide utf-16", utf16(`{"action": "inspect"}`)),
		guideReq("guide invalid utf-8", "{\"action\": \"inspect\", \"x\": \"\xff\"}"),
		guideReq("guide deep", strings.Repeat("[", 5000)+strings.Repeat("]", 5000)),
		guideReq("guide digit limit", `{"action": "inspect", "n": `+strings.Repeat("9", 4301)+`}`),
		guideReq("guide digit limit large", `{"action": "check", "n": `+strings.Repeat("9", 20000)+`}`),
		guideReq("guide 16k inspect", oObj("action", "inspect", "padding", strings.Repeat("x", 17000))),
		guideReq("guide 16k exact", `{"action": "inspect", "p": "`+strings.Repeat("x", 16384-30)+`"}`),
		guideReq("guide 16k garbage", strings.Repeat("x", 17000)),
		guideReq("guide 16k list", "["+strings.Repeat(`"x",`, 4000)+`"x"]`),
		guideReq("guide 128k check", oObj("action", "check", "output", strings.Repeat("x", 129*1024))),
		guideReq("guide 100k check", oObj("action", "check", "output", strings.Repeat("terminal notes\n", 6000)+block)),
		guideReq("guide 100k check no action", oObj("output", strings.Repeat("x", 100000))),
		guideReq("guide no content type", oObj("action", "inspect"), func(r *request) { r.contentType = "" }),
		guideReq("guide text content type", oObj("action", "inspect"), func(r *request) { r.contentType = "text/plain" }),
	)
	checks := []struct {
		name   string
		output any
		mode   any
	}{
		{"pass", block, "host"}, {"pass macvlan", block, "macvlan"}, {"default mode", block, nil}, {"bridge", block, "bridge"},
		{"mode null", block, oObj()}, {"legacy", "old legacy text", "host"}, {"number", 5, "host"}, {"missing", nil, "host"},
		{"oversize", strings.Repeat("例", 32769), "host"}, {"crlf", strings.ReplaceAll(block, "\n", "\r\n"), "host"},
		{"markdown", strings.ReplaceAll(block, "\n", "\\\n"), "host"},
		{"busy port", haBlock(map[string][2]string{"udp53": {"0", "UNCONN 0 0 127.0.0.53%lo:53 0.0.0.0:*"}}), "host"},
		{"busy port macvlan", haBlock(map[string][2]string{"udp53": {"0", "UNCONN 0 0 127.0.0.53%lo:53 0.0.0.0:*"}}), "macvlan"},
		{"desktop", haBlock(map[string][2]string{"daemon_platform": {"0", `{"os":"Docker Desktop","kernel":"linuxkit"}`}}), "host"},
		{"unicode iface", haBlock(map[string][2]string{"ipv4": {"0", "2: 网卡<x> inet 10.0.0.2/8 scope global\n"}}), "host"},
		{"compose digits", haBlock(map[string][2]string{"compose": {"0", "2." + strings.Repeat("9", 4301) + ".0"}}), "host"},
		{"truncated", block[:len(block)/2], "host"}, {"emoji", "\U0001f600" + block, "macvlan"},
		{"lone surrogate escape", nil, "host"},
	}
	for _, c := range checks {
		body := oObj("action", "check")
		if c.output != nil {
			body.Set("output", c.output)
		}
		if c.mode != nil {
			if o, ok := c.mode.(*orderedObject); ok && o.Len() == 0 {
				body.Set("network_mode", nil)
			} else {
				body.Set("network_mode", c.mode)
			}
		}
		out = append(out, guideReq("check "+c.name, body))
	}
	for _, name := range []any{"", "existing", "x;echo bad", 0, 1, nil, []any{}, "a.b_c-1", strings.Repeat("a", 129)} {
		out = append(out, guideReq(fmt.Sprintf("inspect %.20v", name), oObj("action", "inspect", "container_name", name)))
	}
	for _, vip := range []any{"192.168.10.8", "127.0.0.1", "0.0.0.0", "::1", nil, 3232238088, true, false, 1.5, "01.2.3.4", "255.255.255.255"} {
		out = append(out, guideReq(fmt.Sprintf("verify %v", vip), oObj("action", "verify", "vip", vip)))
	}
	out = append(out, guideReq("verify missing", oObj("action", "verify")))
	for _, action := range []any{"bogus", nil, 1, "CHECK"} {
		out = append(out, guideReq(fmt.Sprintf("action %v", action), oObj("action", action)))
	}
	// Generation refusals (no capability is issued before a refusal).
	for _, change := range [][]any{
		{"server_url", "http://user:pw@x.test"}, {"server_url", "ftp://x"}, {"server_url", "http://[bad"}, {"server_url", 5},
		{"agent_ip", "127.0.0.1"}, {"agent_ip", ""}, {"agent_ip", nil}, {"data_dir", "relative"}, {"data_dir", "/a/../b"},
		{"ha_host_id_path", "/a:b"}, {"oxidns_port", true}, {"oxidns_port", "8082"}, {"oxidns_port", 80}, {"image", "bad image"},
		{"create_host_identity", "true"}, {"install_method", "other"}, {"mode", "x"}, {"agent_name", "bad name"},
		{"agent_name", 5}, {"network_mode", "bridge"}, {"network_mode", "macvlan"}, {"network_mode", "macvlan", "ha_parent", "eth0", "ha_subnet", "192.168.50.0/24", "ha_parent", 5},
		{"network_mode", "macvlan", "ha_parent", "eth0", "ha_subnet", "x"}, {"mode", "migrate", "install_method", "compose"},
		{"agent_ip", 3232248340, "network_mode", "macvlan", "ha_parent", "eth0", "ha_subnet", "192.168.50.0/24"},
	} {
		out = append(out, guideReq(fmt.Sprintf("generate refused %v", change), haNewBody(change...)))
	}
	// Migration (deterministic: no capability).
	migrate := haNewBody("mode", "migrate", "container_name", "original-oxidns", "existing_agent_name", "original-dns",
		"ha_data_dir", "/srv/cf/new-ha", "backup_dir", "/srv/cf/backup",
		"agent_mount", oObj("type", "bind", "source", "/srv/cf/old-agent"), "core_mount", oObj("type", "volume", "source", "old_core"))
	out = append(out,
		guideReq("migrate", migrate),
		guideReq("migrate macvlan", withPairs(migrate, "network_mode", "macvlan", "ha_parent", "eth0", "ha_subnet", "192.168.50.0/24", "ha_gateway", "192.168.50.1")),
		guideReq("migrate identity", withPairs(migrate, "create_host_identity", true)),
		guideReq("migrate overlapping", withPairs(migrate, "backup_dir", "/srv/cf/new-ha/b")),
		guideReq("migrate same mounts", withPairs(migrate, "core_mount", oObj("type", "bind", "source", "/srv/cf/old-agent"))),
		guideReq("migrate bad name", withPairs(migrate, "existing_agent_name", "bad name")),
		guideReq("migrate dollar url", withPairs(migrate, "server_url", "https://panel.example/$X")),
	)
	return out
}

func withPairs(o *orderedObject, pairs ...any) *orderedObject {
	out := o.Clone()
	out.Update(oObj(pairs...))
	return out
}

func compareHA(t *testing.T, name string, py, gor response, same, failed *int, mask ...string) {
	t.Helper()
	if approvedSessionHTTPBoundary(t, name, py, gor) {
		*same++
		return
	}
	pyNorm, gorNorm := py, gor
	if name == "generate refused [server_url http://[bad]" {
		p, pe := parsePythonJSON(py.body)
		g, ge := parsePythonJSON(gor.body)
		if pe != nil || ge != nil || py.status != 400 || gor.status != 400 {
			*failed++
			t.Errorf("invalid IPv6 refusal changed")
			return
		}
		po, pok := p.(*orderedObject)
		goObject, gok := g.(*orderedObject)
		if !pok || !gok || po.GetBool("success") || goObject.GetBool("success") || po.GetString("message") != "Invalid IPv6 URL" || !strings.HasPrefix(goObject.GetString("message"), `parse "http://[bad":`) {
			*failed++
			t.Errorf("unexpected IPv6 diagnostics: %s / %s", py.body, gor.body)
			return
		}
		// O12/Step B #12a: urllib and net/url have different English errors.
		// Only this named input's diagnostic text differs; other fields/headers
		// are still compared by the normal comparator below.
		po.Set("message", goObject.GetString("message"))
		pyNorm.body, _ = encodeResponseJSON(po)
		t.Log("EXPECTED invalid IPv6 URL: native net/url diagnostic")
	}
	pyNorm.body, _ = normalizeHA(pyNorm.body)
	gorNorm.body, _ = normalizeHA(gor.body)
	// N7-3c: guide Compose is generated configuration. Only an independently
	// parsed equal document may replace its byte layout for the JSON check;
	// scripts, fields, statuses, headers and installation files remain strict.
	if pyNorm.status == 200 && gorNorm.status == 200 {
		pyNorm.body = equalGuideComposeLayout(pyNorm.body, gorNorm.body)
	}
	if diffs := compareInstaller(pyNorm, gorNorm, append([]string{"enrollment_expires_at"}, mask...)); len(diffs) > 0 {
		*failed++
		t.Errorf("DIFF %s:\n  %s", name, strings.Join(diffs, "\n  "))
	} else {
		*same++
		t.Logf("same %-60.60s → %d %d bytes", name, py.status, len(py.body))
	}
}

// installationFiles lists DATA_DIR/docker-ha-installations with random
// values replaced: session files are named after their Agent name, index
// files keep their (deterministic) name and point at that label.
func installationFiles(t *testing.T, dir string) map[string]string {
	root := filepath.Join(dir, "docker-ha-installations")
	out := map[string]string{}
	info, err := os.Stat(root)
	if err != nil {
		return out
	}
	out["."] = info.Mode().Perm().String()
	entries, _ := os.ReadDir(root)
	labels := map[string]string{}
	for _, entry := range entries {
		if index, found := strings.CutSuffix(entry.Name(), ".index"); found {
			data, _ := os.ReadFile(filepath.Join(root, entry.Name()))
			labels[strings.TrimSpace(string(data))] = "<session of " + index[:12] + ">"
		}
	}
	for _, entry := range entries {
		if session, found := strings.CutSuffix(entry.Name(), ".json"); found {
			if _, ok := labels[session]; ok {
				continue
			}
			data, _ := os.ReadFile(filepath.Join(root, entry.Name()))
			label := "<unreferenced unreadable session>"
			if value, err := parsePythonJSON(data); err == nil {
				if o, ok := value.(*orderedObject); ok {
					label = "<session " + strValue(o.Value("name")) + ">"
				}
			}
			labels[session] = label
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		// O3-6a retires only this server resource lock. Installation data,
		// permissions and unexpected files remain part of the comparison.
		if name == ".issue.lock" {
			continue
		}
		data, _ := os.ReadFile(filepath.Join(root, name))
		text := string(data)
		if label, ok := labels[strings.TrimSpace(text)]; ok {
			text = strings.Replace(text, strings.TrimSpace(text), label, 1)
		}
		if session, found := strings.CutSuffix(name, ".json"); found {
			name = labels[session] + ".json"
			if value, err := parsePythonJSON(data); err == nil {
				if o, ok := value.(*orderedObject); ok {
					for _, key := range []string{"id", "token", "token_hash", "expires"} {
						if o.Has(key) {
							o.Set(key, "<masked>")
						}
					}
					text = jsonText(o)
				}
			}
		}
		fi, _ := entry.Info()
		out[name] = fi.Mode().Perm().String() + " " + text
	}
	return out
}

func TestDockerHADifferentialAgainstFlask(t *testing.T) {
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
	py := &haSide{base: pyBase, token: login(t, pyBase), dir: pyDir}
	gs := &haSide{base: goBase, token: login(t, goBase), dir: goDir}
	same, failed := 0, 0
	both := func(r request, values ...map[string]string) (response, response) {
		pv, gv := map[string]string{}, map[string]string{}
		if len(values) == 2 {
			pv, gv = values[0], values[1]
		}

		before := installationFiles(t, goDir)
		p, g := send(t, py.base, r, py.token, pv), send(t, gs.base, r, gs.token, gv)
		if _, ok := numericGuideBoundary(r); ok {
			if !reflect.DeepEqual(before, installationFiles(t, goDir)) {
				t.Fatal("numeric guide wrote native installation files")
			}
		}
		return p, g
	}
	check := func(r request, mask ...string) (response, response) {
		p, g := both(r)
		var nativeReference *n810APIResult
		if variant, ok := n810Variant(r); ok {
			reference := send(t, py.base, variant, py.token, nil)
			value := n810Result(t, r, reference, nil)
			nativeReference = &value
		}
		n810RecordAPI(t, r, p, nil, nativeReference)
		if diffs, handled := n810CompareAPI(t, r, p, g, nil, nil); handled {
			if len(diffs) > 0 {
				failed++
				t.Errorf("bound native HA policy %s: %v", r.name, diffs)
			} else {
				same++
			}
			return p, g
		}
		if checkNumericGuideBoundary(t, r, p, g, py) {
			same++
			return p, g
		}
		compareHA(t, r.name, p, g, &same, &failed, mask...)
		return p, g
	}

	for _, r := range haReadOnlyRequests() {
		if r.name == "check compose digits" {
			p, g := both(r)
			if p.status != 400 || !strings.Contains(string(p.body), "Exceeds the limit (4300 digits)") {
				t.Fatal("old machine version digit limit changed")
			}
			expected, _ := both(guideReq("native compose digit reference", oObj("action", "check", "output", haBlock(nil), "network_mode", "host")))
			compareHA(t, "native unlimited Compose component", expected, g, &same, &failed)
			continue
		}
		check(r)
	}
	// Deliberate fix of a Python validation bug (A09 decision): a server_url
	// with one trailing "\n" passes Python's re.match(...$) and is refused
	// in Go (migrate mode: no capability is issued on the Python side).
	{
		body := haNewBody("mode", "migrate", "container_name", "original-oxidns", "existing_agent_name", "original-dns",
			"ha_data_dir", "/srv/cf/new-ha", "backup_dir", "/srv/cf/backup", "server_url", "https://panel.example\n",
			"agent_mount", oObj("type", "bind", "source", "/srv/cf/old-agent"), "core_mount", oObj("type", "volume", "source", "old_core"))
		p, g := both(guideReq("deliberate server_url trailing newline", body))
		want := `{"message":"请填写完整的 ConfigFlow HTTP(S) 地址","success":false}`
		goAnswer, _ := parsePythonJSON(g.body)
		if data, _ := encodeSortedJSON(goAnswer); p.status != 200 || g.status != 400 || string(data) != want {
			failed++
			t.Errorf("deliberate difference changed: python %d, go %d %s", p.status, g.status, g.body)
		} else {
			t.Logf("deliberate difference: server_url with a trailing newline — python 200, go 400 (A09 decision)")
		}
	}
	// A lone surrogate escape: Python's json.loads keeps it, jsonx decodes U+FFFD
	// (jsonx-wide limitation); only logged.
	{
		p, g := both(guideReq("check lone surrogate", `{"action": "check", "output": "\ud800"}`))
		t.Logf("lone surrogate: python %d %.120s | go %d %.120s", p.status, p.body, g.status, g.body)
	}
	if files := installationFiles(t, pyDir); len(files) != 0 {
		t.Errorf("python issued a capability for refused or read-only requests: %v", files)
	}
	if files := installationFiles(t, goDir); len(files) != 0 {
		t.Errorf("go issued a capability for refused or read-only requests: %v", files)
	}

	// New installations: the capability is issued and the answer carries it.
	issue := func(name string, body *orderedObject, extra ...func(*request)) (map[string]string, map[string]string) {
		p, g := check(guideReq(name, body, extra...))
		_, pf := normalizeHA(p.body)
		_, gf := normalizeHA(g.body)
		return pf, gf
	}
	pf, gf := issue("generate script host", haNewBody())
	py.session, py.secret, gs.session, gs.secret = pf["session"], pf["token"], gf["session"], gf["token"]
	if py.session == "" || gs.session == "" || py.secret == "" || gs.secret == "" {
		t.Fatalf("no capability: python %v go %v", pf, gf)
	}
	pf2, gf2 := issue("generate script host again", haNewBody())
	if pf2["session"] != py.session || gf2["session"] != gs.session || pf2["token"] != py.secret || gf2["token"] != gs.secret {
		t.Errorf("regeneration changed the capability: python %v→%v go %v→%v", pf, pf2, gf, gf2)
	}
	issue("generate script host action key", haNewBody("action", "generate"))
	issue("generate macvlan", haNewBody("network_mode", "macvlan", "ha_parent", "eth0", "ha_subnet", "192.168.50.0/24", "ha_gateway", "192.168.50.1"))
	issue("generate identity", haNewBody("create_host_identity", true, "agent_name", "id-dns"))
	issue("generate odd url", haNewBody("server_url", "https://panel.example/it's/$(id)/", "agent_name", "odd-dns"))
	issue("generate int agent ip", haNewBody("agent_ip", 3232248340, "agent_name", "int-dns"))
	issue("generate extra fields", haNewBody("agent_name", "extra-dns", "extra", oObj("nested", []any{1, 2.5, nil, "例"}), "ha_gateway", 0))
	issue("generate other profile header", haNewBody("agent_name", "other-dns"), func(r *request) { r.headers = map[string]string{"X-ConfigFlow-Profile": "other"} })
	issue("generate other profile query", haNewBody("agent_name", "query-dns"), func(r *request) { r.path += "?profile=other" })
	check(guideReq("generate bad profile header", haNewBody("agent_name", "bad-profile"), func(r *request) { r.headers = map[string]string{"X-ConfigFlow-Profile": "bad id!"} }))
	check(guideReq("generate missing profile header", haNewBody("agent_name", "missing-profile"), func(r *request) { r.headers = map[string]string{"X-ConfigFlow-Profile": "missing"} }))
	check(guideReq("generate empty profile header", haNewBody("agent_name", "empty-profile"), func(r *request) { r.headers = map[string]string{"X-ConfigFlow-Profile": ""} }))
	// Compose methods (A10): each side's answer against the Go generators
	// given that side's capability (docker_ha_compose_diff_test.go).
	for _, body := range composeBodies() {
		name := "generate " + body.GetString("install_method") + " " + body.GetString("agent_name")
		for attempt := 0; attempt < 2; attempt++ { // the second request re-issues the same capability
			r := guideReq(name, body)
			p, g := both(r)
			if checkNumericGuideBoundary(t, r, p, g, py) {
				same++
				continue
			}
			if diffs := append(compareComposeAnswer("python", body, p), compareComposeAnswer("go", body, g)...); len(diffs) > 0 {
				failed++
				t.Errorf("DIFF %s:\n  %s", name, strings.Join(diffs, "\n  "))
			} else {
				same++
				t.Logf("same %-60.60s → %d %d bytes", name, p.status, len(p.body))
			}
		}
	}

	// Enrollment refusals.
	enroll := func(name, session, token, body string, extra ...func(*request)) request {
		r := request{name: name, method: "POST", path: "/api/agents/docker-oxidns-ha-enroll/" + session, body: body, contentType: "application/json"}
		if token != "" {
			r.headers = map[string]string{"Authorization": token}
		}
		for _, f := range extra {
			f(&r)
		}
		return r
	}
	values := []map[string]string{{"session": py.session, "secret": py.secret}, {"session": gs.session, "secret": gs.secret}}
	checkV := func(r request, mask ...string) (response, response) {
		p, g := both(r, values...)
		compareHA(t, r.name, p, g, &same, &failed, mask...)
		return p, g
	}
	for _, r := range []request{
		enroll("enroll no auth", "{session}", "", "{}"),
		enroll("enroll basic auth", "{session}", "Basic eDp5", "{}"),
		enroll("enroll short token", "{session}", "Bearer abc", "{}"),
		enroll("enroll wrong token", "{session}", "Bearer "+strings.Repeat("x", 43), "{}"),
		enroll("enroll token with space", "{session}", "Bearer {secret} x", "{}"),
		enroll("enroll bad session", "ABC", "Bearer {secret}", "{}"),
		enroll("enroll upper session", strings.Repeat("A", 32), "Bearer {secret}", "{}"),
		enroll("enroll unknown session", strings.Repeat("e", 32), "Bearer {secret}", "{}"),
		enroll("enroll body fields", "{session}", "Bearer {secret}", `{"host": "192.168.50.99"}`),
		enroll("enroll body list", "{session}", "Bearer {secret}", `[]`),
		enroll("enroll body empty", "{session}", "Bearer {secret}", ``),
		enroll("enroll body garbage", "{session}", "Bearer {secret}", `{bad`),
		enroll("enroll body oversize", "{session}", "Bearer {secret}", strings.Repeat("x", 17000)),
		enroll("enroll body oversize json", "{session}", "Bearer {secret}", `{"x": "`+strings.Repeat("x", 17000)+`"}`),
		enroll("enroll body null", "{session}", "Bearer {secret}", `null`),
		enroll("enroll no content type", "{session}", "Bearer {secret}", `{"a": 1}`, func(r *request) { r.contentType = "" }),
		enroll("enroll get", "{session}", "Bearer {secret}", ``, func(r *request) { r.method = "GET" }),
		// A09 review: Authorization spellings around the capability token.
		enroll("enroll lowercase bearer", "{session}", "bearer {secret}", "{}"),
		enroll("enroll double space", "{session}", "Bearer  {secret}", "{}"),
		enroll("enroll trailing tab", "{session}", "Bearer {secret}\t", "{}"),
		enroll("enroll trailing space", "{session}", "Bearer {secret} ", "{}"),
		enroll("enroll padded token", "{session}", "Bearer {secret}=", "{}"),
		enroll("enroll latin-1 token", "{session}", "Bearer "+strings.Repeat("\xe9", 43), "{}"),
		enroll("enroll trailing slash", "{session}/", "Bearer {secret}", "{}"),
		enroll("enroll session suffix", "{session}0", "Bearer {secret}", "{}"),
		enroll("enroll profile header", "{session}", "Bearer {secret}", "{}", func(r *request) {
			r.headers["X-ConfigFlow-Profile"] = "bad id!"
		}),
	} {
		// Spellings both servers accept (trailing white space is OWS) enroll
		// with random identities: mask them.
		checkV(r, "agent_id", "token")
	}
	// Success and replay of a lost answer.
	p, g := checkV(enroll("enroll ok", "{session}", "Bearer {secret}", "{}"), "agent_id", "token")
	pyIdentity, _ := parsePythonJSON(p.body)
	goIdentity, _ := parsePythonJSON(g.body)
	p2, g2 := checkV(enroll("enroll replay", "{session}", "Bearer {secret}", "{}"), "agent_id", "token")
	if string(p2.body) != string(p.body) || string(g2.body) != string(g.body) {
		t.Errorf("replay differs: python %s / %s go %s / %s", p.body, p2.body, g.body, g2.body)
	}
	py.agentID = pyIdentity.(*orderedObject).GetString("agent_id")
	gs.agentID = goIdentity.(*orderedObject).GetString("agent_id")
	agentValues := []map[string]string{{"session": py.session, "secret": py.secret, "agent": py.agentID}, {"session": gs.session, "secret": gs.secret, "agent": gs.agentID}}
	// Changed identity: the Agent's port is edited.
	{
		r := request{name: "edit agent port", method: "PUT", path: "/api/agents/{agent}", admin: true, contentType: "application/json", body: `{"port": 12345}`}
		p, g := both(r, agentValues...)
		compareHA(t, r.name, p, g, &same, &failed, "agent.id", "agent.token", "agent.created_at", "agent.docker_ha_installation_id", "agent.updated_at")
		checkV(enroll("enroll changed identity", "{session}", "Bearer {secret}", "{}"))
		r.name, r.body = "restore agent port", `{"port": 8082}`
		p, g = both(r, agentValues...)
		compareHA(t, r.name, p, g, &same, &failed, "agent.id", "agent.token", "agent.created_at", "agent.docker_ha_installation_id", "agent.updated_at")
		checkV(enroll("enroll restored identity", "{session}", "Bearer {secret}", "{}"), "agent_id", "token")
	}
	// Expiry and renewal keep the installation.
	editSession := func(side *haSide, session string, edit func(*orderedObject) string) {
		path := filepath.Join(side.dir, "docker-ha-installations", session+".json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		value, _ := parsePythonJSON(data)
		if err := os.WriteFile(path, []byte(edit(value.(*orderedObject))), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	expire := func(o *orderedObject) string {
		o.Set("expires", float64(time.Now().UnixNano())/1e9-1)
		return jsonText(o)
	}
	editSession(py, py.session, expire)
	editSession(gs, gs.session, expire)
	checkV(enroll("enroll expired", "{session}", "Bearer {secret}", "{}"))
	pf3, gf3 := issue("generate after expiry", haNewBody())
	if pf3["session"] != py.session || gf3["session"] != gs.session || pf3["token"] == py.secret || gf3["token"] == gs.secret {
		t.Errorf("renewal: python %v go %v", pf3, gf3)
	}
	checkV(enroll("enroll old token after renewal", "{session}", "Bearer {secret}", "{}"))
	renewed := []map[string]string{{"session": py.session, "secret": pf3["token"]}, {"session": gs.session, "secret": gf3["token"]}}
	{
		p, g := both(enroll("enroll renewed", "{session}", "Bearer {secret}", "{}"), renewed...)
		compareHA(t, "enroll renewed", p, g, &same, &failed, "agent_id", "token")
		if string(p.body) != string(p2.body) || string(g.body) != string(g2.body) {
			t.Errorf("renewed enrollment returns another identity")
		}
	}
	// Changed settings: another installation that collides with the first Agent.
	pc, gc := issue("generate changed data dir", haNewBody("data_dir", "/different/data"))
	changed := []map[string]string{{"session": pc["session"], "secret": pc["token"]}, {"session": gc["session"], "secret": gc["token"]}}
	{
		p, g := both(enroll("enroll changed settings", "{session}", "Bearer {secret}", "{}"), changed...)
		compareHA(t, "enroll changed settings", p, g, &same, &failed)
	}
	// Takeover of a registered device is refused.
	{
		r := request{name: "register taken", method: "POST", admin: true, path: "/api/agents/register", contentType: "application/json",
			body: `{"name": "taken-dns-oxidns", "host": "192.168.50.30", "port": 8082, "service_type": "oxidns", "deployment_method": "docker"}`}
		p, g := both(r)
		compareHA(t, r.name, p, g, &same, &failed, "id", "token")
		pt, gt := issue("generate taken", haNewBody("agent_name", "taken-dns", "agent_ip", "192.168.50.30"))
		taken := []map[string]string{{"session": pt["session"], "secret": pt["token"]}, {"session": gt["session"], "secret": gt["token"]}}
		p, g = both(enroll("enroll takeover", "{session}", "Bearer {secret}", "{}"), taken...)
		compareHA(t, "enroll takeover", p, g, &same, &failed)
	}
	// Other-profile installation enrolls into its profile.
	po, gol := issue("generate other profile again", haNewBody("agent_name", "other-dns"), func(r *request) { r.headers = map[string]string{"X-ConfigFlow-Profile": "other"} })
	{
		other := []map[string]string{{"session": po["session"], "secret": po["token"]}, {"session": gol["session"], "secret": gol["token"]}}
		p, g := both(enroll("enroll other profile", "{session}", "Bearer {secret}", "{}"), other...)
		compareHA(t, "enroll other profile", p, g, &same, &failed, "agent_id", "token")
	}

	// Hand-edited installation records (one installation per case).
	handEdits := []struct {
		name string
		edit func(*orderedObject) string
	}{
		{"invalid json", func(*orderedObject) string { return "{bad" }},
		{"list json", func(*orderedObject) string { return "[]" }},
		{"invalid utf-8", func(*orderedObject) string { return "{\"x\": \"\xff\"}" }},
		{"no expires", func(o *orderedObject) string { o.Delete("expires"); return jsonText(o) }},
		{"string expires", func(o *orderedObject) string { o.Set("expires", "soon"); return jsonText(o) }},
		{"nan expires", func(o *orderedObject) string { o.Set("expires", nanValue()); return jsonText(o) }},
		{"huge expires", func(o *orderedObject) string { o.Set("expires", bigPow(10, 30)); return jsonText(o) }},
		{"bool expires", func(o *orderedObject) string { o.Set("expires", true); return jsonText(o) }},
		{"no token hash", func(o *orderedObject) string { o.Delete("token_hash"); return jsonText(o) }},
		{"int token hash", func(o *orderedObject) string { o.Set("token_hash", 5); return jsonText(o) }},
		{"non-ascii token hash", func(o *orderedObject) string { o.Set("token_hash", "é"); return jsonText(o) }},
		{"bad profile", func(o *orderedObject) string { o.Set("profile_id", "bad id!"); return jsonText(o) }},
		{"int profile", func(o *orderedObject) string { o.Set("profile_id", 5); return jsonText(o) }},
		{"missing profile", func(o *orderedObject) string { o.Set("profile_id", "gone"); return jsonText(o) }},
		{"no name", func(o *orderedObject) string { o.Delete("name"); return jsonText(o) }},
		{"float port", func(o *orderedObject) string { o.Set("port", 8082.0); return jsonText(o) }},
	}
	for i, h := range handEdits {
		name := fmt.Sprintf("hand-%d", i)
		pi, gi := issue("generate "+name, haNewBody("agent_name", name))
		editSession(py, pi["session"], h.edit)
		editSession(gs, gi["session"], h.edit)
		hv := []map[string]string{{"session": pi["session"], "secret": pi["token"]}, {"session": gi["session"], "secret": gi["token"]}}
		p, g := both(enroll("enroll "+h.name, "{session}", "Bearer {secret}", "{}"), hv...)
		compareHA(t, "enroll "+h.name, p, g, &same, &failed, "agent_id", "token")
		p, g = both(guideReq("reissue "+h.name, haNewBody("agent_name", name)))
		compareHA(t, "reissue "+h.name, p, g, &same, &failed)
	}
	// Index records: wrong content, invalid UTF-8, a missing session file.
	indexEdits := map[string]func(side *haSide, index, session string){
		"index garbage": func(_ *haSide, index, _ string) { os.WriteFile(index, []byte("not-a-session"), 0o600) },
		"index utf-8":   func(_ *haSide, index, _ string) { os.WriteFile(index, []byte("\xff\xfe"), 0o600) },
		"index newline": func(_ *haSide, index, session string) { os.WriteFile(index, []byte(session+"\n"), 0o600) },
		"index missing session": func(side *haSide, _, session string) {
			os.Remove(filepath.Join(side.dir, "docker-ha-installations", session+".json"))
		},
	}
	names := make([]string, 0, len(indexEdits))
	for name := range indexEdits {
		names = append(names, name)
	}
	sort.Strings(names)
	for i, name := range names {
		agentName := fmt.Sprintf("index-%d", i)
		pi, gi := issue("generate "+agentName, haNewBody("agent_name", agentName))
		for _, pair := range []struct {
			side    *haSide
			session string
		}{{py, pi["session"]}, {gs, gi["session"]}} {
			index := findIndex(t, pair.side.dir, pair.session)
			indexEdits[name](pair.side, index, pair.session)
		}
		p, g := both(guideReq("reissue "+name, haNewBody("agent_name", agentName)))
		compareHA(t, "reissue "+name, p, g, &same, &failed)
	}

	// Stored state: installation records and Agents.
	if _, err := os.Lstat(filepath.Join(goDir, "docker-ha-installations", ".issue.lock")); !os.IsNotExist(err) {
		t.Errorf("Go created retired .issue.lock: %v", err)
	}
	pyFiles, goFiles := installationFiles(t, pyDir), installationFiles(t, goDir)
	var keys []string
	for k := range pyFiles {
		keys = append(keys, k)
	}
	for k := range goFiles {
		if _, ok := pyFiles[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if pyFiles[k] != goFiles[k] {
			failed++
			t.Errorf("installation file %s:\n  python %.300q\n  go     %.300q", k, pyFiles[k], goFiles[k])
		}
	}
	t.Logf("installation files compared: %d", len(keys))
	stopPython()
	stopGo()
	pyAgents, goAgents := finalAgents(t, pyDir), finalAgents(t, goDir)
	for _, list := range []any{pyAgents, goAgents} {
		for _, item := range list.([]any) {
			if o := item.(*orderedObject); o.Has("docker_ha_installation_id") {
				o.Set("docker_ha_installation_id", "<masked>")
			}
		}
	}

	// Python alone registers the known hand-5 NaN session. Verify that one
	// admitted identity, then compare every remaining record normally.
	var filtered []any
	skipped := 0
	for _, item := range pyAgents.([]any) {
		o := item.(*orderedObject)
		if o.GetString("name") == "hand-5-oxidns" {
			if !o.Has("docker_ha_installation_id") {
				t.Fatal("old NaN Agent lost its marker")
			}
			skipped++
			continue
		}
		filtered = append(filtered, item)
	}
	if skipped != 1 {
		t.Fatalf("expected one Python NaN enrollment, got %d", skipped)
	}
	pyAgents = filtered
	if !jsonEqual(pyAgents, goAgents) {
		failed++
		pyText, _ := encodeCompactJSON(pyAgents)
		goText, _ := encodeCompactJSON(goAgents)
		t.Errorf("stored agents differ:\npython %s\ngo     %s", pyText, goText)
	}
	t.Logf("docker HA routes: %d comparisons equal, %d differ", same, failed)
}

func findIndex(t *testing.T, dir, session string) string {
	root := filepath.Join(dir, "docker-ha-installations")
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".index") {
			data, _ := os.ReadFile(filepath.Join(root, entry.Name()))
			if string(data) == session {
				return filepath.Join(root, entry.Name())
			}
		}
	}
	t.Fatalf("no index for %s in %s", session, dir)
	return ""
}

func nanValue() float64 { return math.NaN() }

func bigPow(base, exponent int64) *big.Int {
	return new(big.Int).Exp(big.NewInt(base), big.NewInt(exponent), nil)
}
