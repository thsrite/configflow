package difftest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const nativeFixedIPv4Message = "请填写有效的固定局域网 IPv4 地址；Host 模式填写宿主机地址，Macvlan 模式填写容器地址"

// numericGuideBoundary binds every old numeric-IP HTTP request literally. The
// Python session is inspected before cleaning up only that case's test files.
func numericGuideBoundary(r request) (int, bool) {
	cases := []struct {
		body   *orderedObject
		status int
	}{
		{haNewBody("agent_ip", 3232248340, "agent_name", "int-dns"), 200},
		{haNewBody("agent_ip", 3232248340, "network_mode", "macvlan", "ha_parent", "eth0", "ha_subnet", "192.168.50.0/24"), 500},
	}
	for _, method := range []string{"compose", "compose_direct", "compose_compact"} {
		cases = append(cases, struct {
			body   *orderedObject
			status int
		}{haNewBody("install_method", method, "agent_name", method+"-int", "agent_ip", 3232248340, "data_dir", "/srv/oxidns-int", "oxidns_port", 8083), 200})
	}
	for _, c := range cases {
		if r.method == "POST" && r.path == haGuide && r.body == jsonText(c.body) {
			return c.status, true
		}
	}
	return 0, false
}

func checkNumericGuideBoundary(t *testing.T, r request, p, g response, python *haSide) bool {
	t.Helper()
	old, ok := numericGuideBoundary(r)
	if !ok {
		return false
	}
	answer, err := parsePythonJSON(g.body)
	if err != nil || g.status != 400 || !jsonEqual(answer, oObj("success", false, "message", nativeFixedIPv4Message)) || p.status != old {
		t.Fatalf("numeric guide boundary changed: Python %d Go %d %s", p.status, g.status, g.body)
	}
	if old == 200 {
		value, err := parsePythonJSON(p.body)
		if err != nil || !value.(*orderedObject).GetBool("success") {
			t.Fatal("old numeric guide success changed")
		}
		body, _ := parsePythonJSONString(r.body)
		method := body.(*orderedObject).GetString("install_method")
		var session string
		if method == "" {
			_, found := normalizeHA(p.body)
			session = found["session"]
		} else {
			capability, err := composeEnrollment(method, value.(*orderedObject), body.(*orderedObject))
			if err != nil {
				t.Fatal(err)
			}
			session = capability.URL[strings.LastIndexByte(capability.URL, '/')+1:]
		}
		path := filepath.Join(python.dir, "docker-ha-installations", session+".json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		stored, err := parsePythonJSON(raw)
		if err != nil || !jsonEqual(stored.(*orderedObject).Value("host"), int64(3232248340)) {
			t.Fatal("Python no longer stores the numeric host")
		}
		index := findIndex(t, python.dir, session)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(index); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("EXPECTED exact numeric guide request: Python %d / native 400; original numeric host is unsupported", old)
	return true
}

// approvedSessionHTTPBoundary is limited to named synthetic corrupt-record
// edits; complete old rows/results are also SHA-bound by the installer corpus.
func approvedSessionHTTPBoundary(t *testing.T, name string, p, g response) bool {
	t.Helper()
	rules := map[string][2]int{"enroll list json": {500, 401}, "reissue list json": {500, 409}, "enroll nan expires": {200, 401}, "reissue nan expires": {200, 409}, "reissue index utf-8": {400, 409}}
	rule, ok := rules[name]
	if !ok {
		return false
	}
	message := "安装记录无法读取，请保留现场并检查服务端数据。"
	if rule[1] == 401 {
		message = "安装凭据无效，请重新生成安装命令。"
	}
	parsed, err := parsePythonJSON(g.body)
	if p.status != rule[0] || g.status != rule[1] || err != nil || !jsonEqual(parsed, oObj("success", false, "message", message)) {
		t.Fatalf("corrupt session policy changed for %s: Python %d Go %d %s", name, p.status, g.status, g.body)
	}
	if rule[0] == 500 && !strings.Contains(string(p.body), "Internal Server Error") {
		t.Fatal("old corrupt-session error class changed")
	}
	if rule[0] == 200 {
		original, err := parsePythonJSON(p.body)
		if err != nil || !original.(*orderedObject).GetBool("success") {
			t.Fatal("old NaN admission changed")
		}
	}
	if name == "reissue index utf-8" && !strings.Contains(string(p.body), "codec can't decode byte") {
		t.Fatal("old UTF-8 refusal changed")
	}
	t.Logf("EXPECTED %s: Python %d / native %d, fail closed", name, p.status, g.status)
	return true
}
