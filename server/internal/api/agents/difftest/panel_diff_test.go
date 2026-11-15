package difftest

// Flask↔Go differential of the A07 routes: the Mihomo panel relay
// (/api/agents/<id>/mihomo/<path:resource>), the OxiDNS rule update and the
// OxiDNS cache list/flush. Both servers point their Agents at one loopback
// fake Agent whose answer depends on the phase set before each pair of
// requests. Compared: status, headers and body bytes, the requests the fake
// Agent received from each server (method, path, query, Authorization,
// Accept, Content-Type, body) and the files under DATA_DIR. The core update
// check reaches GitHub only through the configured rule download proxy,
// seeded as a closed loopback port, so no request leaves the host.
//
//	rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v -run Panel ./internal/api/agents/difftest/'

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

type fakePanelAgent struct {
	mu    sync.Mutex
	phase string
	log   []string
}

func (f *fakePanelAgent) setPhase(phase string) {
	f.mu.Lock()
	f.phase = phase
	f.log = nil
	f.mu.Unlock()
}

func (f *fakePanelAgent) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.log
	f.log = nil
	return out
}

func panelNode(name string) *jsonx.Object {
	return jsonx.Obj("name", name, "type", "Trojan", "udp", true, "xudp", true, "tfo", false, "id", "x", "alive", true,
		"history", []any{jsonx.Obj("time", "t1", "delay", 300), jsonx.Obj("time", "t2", "delay", 120)},
		"extra", jsonx.Obj("https://cp.test/204", jsonx.Obj("alive", false, "history", []any{jsonx.Obj("delay", 90), jsonx.Obj("delay", 0)}),
			"odd", "not a dict"))
}

func (f *fakePanelAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	phase := f.phase
	f.log = append(f.log, fmt.Sprintf("%s %s ?%s auth=%q accept=%q type=%q body=%q", r.Method, r.URL.EscapedPath(),
		r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("Accept"), r.Header.Get("Content-Type"), body))
	f.mu.Unlock()
	send := func(status int, v any) {
		data, _ := jsonx.Default(v)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write(data)
	}
	raw := func(status int, data string) {
		w.WriteHeader(status)
		io.WriteString(w, data)
	}
	switch phase {
	case "401":
		send(401, jsonx.Obj("message", "nope"))
		return
	case "302":
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
		return
	case "404text":
		raw(404, "404 page not found")
		return
	case "notjson":
		raw(200, "not json")
		return
	case "empty":
		w.WriteHeader(204)
		return
	case "big":
		raw(200, strings.Repeat(" ", 16*1024*1024+1))
		return
	case "controller":
		send(409, jsonx.Obj("success", false, "code", "CONTROLLER_UNAVAILABLE", "message", "secret detail"))
		return
	case "codelist":
		send(502, jsonx.Obj("success", false, "code", []any{"x"}))
		return
	case "timeout":
		send(504, jsonx.Obj("message", "Timeout"))
		return
	case "long":
		send(418, jsonx.Obj("message", strings.Repeat("长", 400)))
		return
	case "coreerror":
		send(500, jsonx.Obj("message", "download failed: dial tcp: i/o timeout"))
		return
	case "partial":
		send(502, jsonx.Obj("success", false, "code", "CACHE_FLUSH_FAILED", "message", "core secret", "flushed", []any{"main_cache"}))
		return
	case "cacheunknown":
		send(409, jsonx.Obj("success", false, "code", "CACHE_UNKNOWN"))
		return
	case "taskfailed":
		send(200, jsonx.Obj("success", true, "task", jsonx.Obj("id", strings.Repeat("a", 32), "status", "failed", "phase", "done",
			"downloaded", 1, "download_total", 2, "reloaded", 0, "reload_total", 1, "code", "DOWNLOAD_FAILED", "message", "http://u:secret@x")))
		return
	case "taskbad":
		send(200, jsonx.Obj("success", true, "task", jsonx.Obj("id", "http://secret", "status", "running", "phase", "downloading",
			"downloaded", 0, "download_total", 2, "reloaded", 0, "reload_total", 1)))
		return
	case "busy":
		send(409, jsonx.Obj("success", false, "code", "RULE_UPDATE_BUSY", "message", "secret"))
		return
	case "nan":
		raw(200, `{"a": NaN, "b": 1e400, "c": -0.0, "d": 1.50}`)
		return
	case "bom":
		raw(200, "\xef\xbb\xbf{\"version\": \"v1\"}")
		return
	}
	path := r.URL.Path
	switch {
	case path == "/api/mihomo/version":
		version := "v1.19.32"
		if phase == "alpha" {
			version = "alpha-0a1b2c3"
		}
		send(200, jsonx.Obj("meta", true, "version", version))
	case path == "/api/mihomo/proxies":
		send(200, jsonx.Obj("proxies", jsonx.Obj("HK", panelNode("HK"), "G", jsonx.Obj("name", "G", "type", "Selector",
			"now", "HK", "all", []any{"HK"}, "hidden", false, "icon", "", "fixed", "", "testUrl", "u", "history", []any{}),
			"odd", "x"), "extra", 1))
	case path == "/api/mihomo/providers/proxies":
		send(200, jsonx.Obj("providers", jsonx.Obj(
			"sub", jsonx.Obj("name", "sub", "vehicleType", "HTTP", "subscriptionInfo", jsonx.Obj("Total", 1),
				"proxies", []any{panelNode("HK"), "odd"}),
			"Proxy", jsonx.Obj("name", "Proxy", "vehicleType", "Compatible", "proxies", []any{panelNode("DIRECT"), jsonx.Obj("type", "x")}),
			"none", nil)))
	case strings.HasPrefix(path, "/api/mihomo/"):
		send(200, jsonx.Obj("ok", true, "delay", 81))
	case path == "/api/oxidns/cache":
		send(200, jsonx.Obj("success", true, "caches", []any{"main_cache", "probe"}))
	case path == "/api/oxidns/cache/flush":
		var tags any = []any{"probe"}
		if value, err := jsonx.Parse(body); err == nil {
			tags = value.(*jsonx.Object).Value("tags")
		}
		send(200, jsonx.Obj("success", true, "flushed", tags))
	case path == "/api/oxidns/rules/update":
		status, state, phaseName, done := 202, "running", "downloading", 0
		if r.Method == "GET" {
			status, state, phaseName, done = 200, "succeeded", "done", 2
		}
		reloaded := 0
		if done == 2 {
			reloaded = 1
		}
		send(status, jsonx.Obj("success", true, "task", jsonx.Obj("id", strings.Repeat("a", 32), "status", state, "phase", phaseName,
			"downloaded", done, "download_total", 2, "reloaded", reloaded, "reload_total", 1, "token", "secret")))
	default:
		http.NotFound(w, r)
	}
}

func panelSeed(t *testing.T, dir string, port, closed int) {
	r, err := repo.OpenDefault(dir)
	if err != nil {
		t.Fatal(err)
	}
	agent := func(id, version, service string, p int) *jsonx.Object {
		return jsonx.Obj("id", id, "name", id, "token", "tok-"+id, "profile_id", "default", "service_type", service,
			"host", "127.0.0.1", "port", p, "version", version, "config_version", "0", "enabled", true, "status", "online")
	}
	_, err = r.UpdateSystemTransaction(context.Background(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		busy := agent("dns-busy", "1.8.36-go", "oxidns", port)
		busy.Set("oxidns_ha_publication", jsonx.Obj("status", "publishing", "kind", "deploy"))
		// Review cases: a bad host with a non-latin-1 token (origin checked
		// first), malformed HA publication records (Python raises).
		badHost := agent("dns-badhost", "1.8.36-go", "oxidns", port)
		badHost.Set("host", "bad/host")
		badHost.Set("token", "\u4ee4\u724c")
		haText := agent("dns-ha-text", "1.8.36-go", "oxidns", port)
		haText.Set("oxidns_ha_publication", "x")
		haList := agent("dns-ha-list", "1.8.36-go", "oxidns", port)
		haList.Set("oxidns_ha_publication", jsonx.Obj("status", []any{"a"}))
		haNumber := agent("dns-ha-number", "1.8.36-go", "oxidns", port)
		haNumber.Set("oxidns_ha_publication", jsonx.Obj("status", 1))
		haDone := agent("dns-ha-done", "1.8.36-go", "oxidns", port)
		haDone.Set("oxidns_ha_publication", jsonx.Obj("status", "rollback_failed"))
		// Below 1.8.55-go: Python's ranking collector never contacts these Agents.
		system.Set("agents", []any{
			agent("gw-old", "1.8.14-go", "mihomo", port), agent("gw", "1.8.15-go", "mihomo", port),
			agent("gw16", "1.8.16-go", "mihomo", port), agent("gw18", "1.8.18-go", "mihomo", port),
			agent("gw26", "1.8.26-go", "mihomo", port), agent("gw56", "1.8.56-go", "mihomo", port),
			agent("gw-gone", "1.8.20-go", "mihomo", closed),
			agent("dns", "1.8.36-go", "oxidns", port), agent("dns35", "1.8.35-go", "oxidns", port),
			agent("dns29", "1.8.29-go", "oxidns", port), agent("dns30", "1.8.30-go", "oxidns", port), busy,
			badHost, haText, haList, haNumber, haDone,
		})
		config := system.GetObject("system_config")
		if config == nil {
			config = jsonx.NewObject()
			system.Set("system_config", config)
		}
		config.Set("rule_fetch_proxy", "http://127.0.0.1:"+strconv.Itoa(closed))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type panelRequest struct {
	request
	phase string
}

func panelRequests() []panelRequest {
	J := "application/json"
	b := "/api/agents/"
	get := func(name, path, phase string) panelRequest {
		return panelRequest{request{name: name, method: "GET", path: path, admin: true}, phase}
	}
	with := func(name, method, path, body, contentType, phase string) panelRequest {
		return panelRequest{request{name: name, method: method, path: path, admin: true, body: body, contentType: contentType}, phase}
	}
	return []panelRequest{
		// Mihomo panel
		get("panel version", b+"gw/mihomo/version", ""),
		{request{name: "panel unauthenticated", method: "GET", path: b + "gw/mihomo/version"}, ""},
		{request{name: "panel agent token", method: "GET", path: b + "gw/mihomo/version", headers: map[string]string{"Authorization": "Bearer tok-gw"}}, ""},
		get("panel not allowed", b+"gw/mihomo/restart", ""),
		get("panel trailing slash", b+"gw/mihomo/proxies/", ""),
		with("panel HEAD", "HEAD", b+"gw/mihomo/version", "", "", ""),
		with("panel OPTIONS", "OPTIONS", b+"gw/mihomo/version", "", "", ""),
		get("panel proxies slimmed", b+"gw/mihomo/proxies", ""),
		get("panel providers slimmed", b+"gw/mihomo/providers/proxies", ""),
		with("panel switch", "PUT", b+"gw/mihomo/proxy?name=%E8%8A%82%E7%82%B9%2F%E9%A6%99%E6%B8%AF%2001", `{"name": "HK"}`, J, ""),
		with("panel switch text body", "PUT", b+"gw/mihomo/proxy?name=G", `{"name": "HK"}`, "text/plain", ""),
		with("panel switch extra key", "PUT", b+"gw/mihomo/proxy?name=G", `{"name": "HK", "x": 1}`, J, ""),
		with("panel switch control name", "PUT", b+"gw/mihomo/proxy?name=a%0Ab", `{"name": "HK"}`, J, ""),
		with("panel mode", "PATCH", b+"gw/mihomo/configs", `{"mode": "global"}`, J, ""),
		with("panel mode bad", "PATCH", b+"gw/mihomo/configs", `{"mode": "global", "tun": {"enable": false}}`, J, ""),
		with("panel delete connection", "DELETE", b+"gw/mihomo/connection?name=abc", "", "", "empty"),
		with("panel delete connections", "DELETE", b+"gw/mihomo/connections", "", "", ""),
		get("panel node delay", b+"gw/mihomo/proxy/delay?name=HK&timeout=%D9%A3%D9%A0%D9%A0%D9%A0&url=http://127.0.0.1:1/204?a=b&extra=x", ""),
		get("panel node delay default", b+"gw/mihomo/proxy/delay?name=%F0%9F%87%AF%F0%9F%87%B5+JP+50%25%3F%23", ""),
		get("panel group delay file url", b+"gw/mihomo/group/delay?name=Auto&url=file:///etc/passwd", ""),
		get("panel group delay bad timeout", b+"gw/mihomo/group/delay?name=Auto&timeout=1e3", ""),
		get("panel group delay timeout range", b+"gw/mihomo/group/delay?name=Auto&timeout=30001", ""),
		get("panel provider delay old", b+"gw/mihomo/providers/proxy/delay?provider=A&name=B", ""),
		get("panel provider delay", b+"gw18/mihomo/providers/proxy/delay?provider=%E8%AE%A2%E9%98%85%2FA&name=%E8%8A%82%E7%82%B9%2FB&timeout=7300", ""),
		get("panel provider delay no provider", b+"gw18/mihomo/providers/proxy/delay?name=B", ""),
		get("panel provider healthcheck", b+"gw/mihomo/providers/proxy/healthcheck?name=Airport", ""),
		with("panel provider update", "PUT", b+"gw/mihomo/providers/proxy?name=sub", `{"ignored": true}`, J, ""),
		with("panel rule provider update", "PUT", b+"gw/mihomo/providers/rule?name=r", "", "", ""),
		get("panel logs old", b+"gw/mihomo/logs", ""),
		get("panel logs", b+"gw56/mihomo/logs?level=debug&after=7&path=/etc", ""),
		get("panel logs default", b+"gw56/mihomo/logs", ""),
		get("panel logs superscript cursor", b+"gw56/mihomo/logs?after=%C2%B2", ""),
		get("panel logs bad level", b+"gw56/mihomo/logs?level=trace", ""),
		get("panel logs long cursor", b+"gw56/mihomo/logs?after=9999999999999999999", ""),
		get("panel flow old", b+"gw18/mihomo/flow", ""),
		get("panel flow", b+"gw26/mihomo/flow?url=http://ignored", ""),
		with("panel reload old", "PUT", b+"gw/mihomo/configs/reload", `{"path": "/tmp/evil.yaml"}`, J, ""),
		with("panel reload", "PUT", b+"gw16/mihomo/configs/reload", `{"path": "/tmp/evil.yaml"}`, J, ""),
		with("panel upgrade core error", "POST", b+"gw16/mihomo/upgrade", `{"channel": "alpha"}`, J, "coreerror"),
		with("panel fakeip flush", "POST", b+"gw/mihomo/cache/fakeip/flush", "", "", ""),
		with("panel dns flush", "POST", b+"gw/mihomo/cache/dns/flush", "", "", ""),
		get("panel rules", b+"gw/mihomo/rules", ""),
		get("panel old agent", b+"gw-old/mihomo/version", ""),
		get("panel oxidns agent", b+"dns/mihomo/version", ""),
		get("panel missing agent", b+"missing/mihomo/version", ""),
		get("panel unreachable", b+"gw-gone/mihomo/version", ""),
		get("panel agent 401", b+"gw/mihomo/version", "401"),
		get("panel agent redirect", b+"gw/mihomo/version", "302"),
		get("panel agent 404 text", b+"gw/mihomo/version", "404text"),
		get("panel agent not json", b+"gw/mihomo/version", "notjson"),
		get("panel agent too big", b+"gw/mihomo/version", "big"),
		get("panel controller unavailable", b+"gw/mihomo/version", "controller"),
		get("panel code list", b+"gw/mihomo/version", "codelist"),
		get("panel core timeout", b+"gw/mihomo/proxy/delay?name=HK", "timeout"),
		get("panel long message", b+"gw/mihomo/version", "long"),
		get("panel NaN reply", b+"gw/mihomo/version", "nan"),
		get("panel BOM reply", b+"gw/mihomo/version", "bom"),
		get("panel core update old", b+"gw/mihomo/core/update", ""),
		get("panel core update", b+"gw16/mihomo/core/update", ""),
		get("panel core update cached", b+"gw16/mihomo/core/update", ""),
		get("panel core update forced", b+"gw16/mihomo/core/update?force=1", ""),
		get("panel core update alpha", b+"gw16/mihomo/core/update", "alpha"),
		with("panel core update POST", "POST", b+"gw16/mihomo/core/update", "", "", ""),
		// OxiDNS cache
		get("cache list", b+"dns/oxidns/cache", ""),
		{request{name: "cache unauthenticated", method: "POST", path: b + "dns/oxidns/cache/flush", body: `{"tags": ["probe"]}`, contentType: J}, ""},
		{request{name: "cache agent token", method: "POST", path: b + "dns/oxidns/cache/flush", body: `{"tags": ["probe"]}`, contentType: J,
			headers: map[string]string{"Authorization": "Bearer tok-dns"}}, ""},
		with("cache flush", "POST", b+"dns/oxidns/cache/flush", `{"tags": ["probe", "main_cache"]}`, J, ""),
		with("cache flush query", "POST", b+"dns/oxidns/cache/flush?tag=probe", `{"tags": ["probe"]}`, J, ""),
		with("cache flush empty object", "POST", b+"dns/oxidns/cache/flush", `{}`, J, ""),
		with("cache flush extra key", "POST", b+"dns/oxidns/cache/flush", `{"tags": ["probe"], "agent_id": "other"}`, J, ""),
		with("cache flush bad tag", "POST", b+"dns/oxidns/cache/flush", `{"tags": ["../reload"]}`, J, ""),
		with("cache flush duplicate", "POST", b+"dns/oxidns/cache/flush", `{"tags": ["a", "a"]}`, J, ""),
		with("cache flush dict tag", "POST", b+"dns/oxidns/cache/flush", `{"tags": [{}]}`, J, ""),
		with("cache flush text", "POST", b+"dns/oxidns/cache/flush", `{"tags": ["probe"]}`, "text/plain", ""),
		with("cache flush bad json", "POST", b+"dns/oxidns/cache/flush", `{`, J, ""),
		with("cache flush BOM", "POST", b+"dns/oxidns/cache/flush", "\ufeff{\"tags\": [\"probe\"]}", J, ""),
		with("cache flush oversize", "POST", b+"dns/oxidns/cache/flush", strings.Repeat(" ", 16*1024+1), J, ""),
		with("cache list with body", "GET", b+"dns/oxidns/cache", `{}`, J, ""),
		with("cache flush GET", "GET", b+"dns/oxidns/cache/flush", "", "", ""),
		with("cache partial failure", "POST", b+"dns/oxidns/cache/flush", `{"tags": ["main_cache", "probe"]}`, J, "partial"),
		with("cache unknown", "POST", b+"dns/oxidns/cache/flush", `{"tags": ["probe"]}`, J, "cacheunknown"),
		with("cache 404 text", "POST", b+"dns/oxidns/cache/flush", `{"tags": ["probe"]}`, J, "404text"),
		with("cache 401", "POST", b+"dns/oxidns/cache/flush", `{"tags": ["probe"]}`, J, "401"),
		with("cache redirect", "POST", b+"dns/oxidns/cache/flush", `{"tags": ["probe"]}`, J, "302"),
		get("cache old agent", b+"dns35/oxidns/cache", ""),
		get("cache mihomo agent", b+"gw/oxidns/cache", ""),
		get("cache missing agent", b+"missing/oxidns/cache", ""),
		// OxiDNS rule update
		with("rules start", "POST", b+"dns30/oxidns/rules/update", `{}`, J, ""),
		with("rules start no body", "POST", b+"dns30/oxidns/rules/update", "", "", ""),
		get("rules status", b+"dns30/oxidns/rules/update", ""),
		{request{name: "rules unauthenticated", method: "POST", path: b + "dns30/oxidns/rules/update", body: `{}`, contentType: J}, ""},
		{request{name: "rules agent token", method: "GET", path: b + "dns30/oxidns/rules/update", headers: map[string]string{"Authorization": "Bearer tok-dns30"}}, ""},
		with("rules query", "POST", b+"dns30/oxidns/rules/update?url=http://x", `{}`, J, ""),
		with("rules key", "POST", b+"dns30/oxidns/rules/update", `{"tag": "x"}`, J, ""),
		with("rules list", "POST", b+"dns30/oxidns/rules/update", `[]`, J, ""),
		with("rules null", "POST", b+"dns30/oxidns/rules/update", `null`, J, ""),
		with("rules text", "POST", b+"dns30/oxidns/rules/update", `{}`, "text/plain", ""),
		with("rules bad json", "POST", b+"dns30/oxidns/rules/update", `{`, J, ""),
		with("rules oversize", "POST", b+"dns30/oxidns/rules/update", strings.Repeat(" ", 1025), J, ""),
		with("rules GET body", "GET", b+"dns30/oxidns/rules/update", `{}`, J, ""),
		with("rules PUT", "PUT", b+"dns30/oxidns/rules/update", `{}`, J, ""),
		get("rules failed task", b+"dns30/oxidns/rules/update", "taskfailed"),
		get("rules bad task", b+"dns30/oxidns/rules/update", "taskbad"),
		with("rules busy", "POST", b+"dns30/oxidns/rules/update", `{}`, J, "busy"),
		get("rules 404 text", b+"dns30/oxidns/rules/update", "404text"),
		get("rules 401", b+"dns30/oxidns/rules/update", "401"),
		get("rules not json", b+"dns30/oxidns/rules/update", "notjson"),
		with("rules HA busy", "POST", b+"dns-busy/oxidns/rules/update", `{}`, J, ""),
		get("rules HA busy status", b+"dns-busy/oxidns/rules/update", ""),
		with("rules old agent", "POST", b+"dns29/oxidns/rules/update", `{}`, J, ""),
		get("rules mihomo agent", b+"gw/oxidns/rules/update", ""),
		get("rules missing agent", b+"missing/oxidns/rules/update", ""),
		// Review (A07) cases
		get("review cache bad host non-latin-1 token", b+"dns-badhost/oxidns/cache", ""),
		with("review cache flush bad host", "POST", b+"dns-badhost/oxidns/cache/flush", `{"tags": ["probe"]}`, J, ""),
		with("review rules HA record text", "POST", b+"dns-ha-text/oxidns/rules/update", `{}`, J, ""),
		get("review rules HA record text GET", b+"dns-ha-text/oxidns/rules/update", ""),
		with("review rules HA status list", "POST", b+"dns-ha-list/oxidns/rules/update", `{}`, J, ""),
		with("review rules HA status number", "POST", b+"dns-ha-number/oxidns/rules/update", `{}`, J, ""),
		with("review rules HA rollback_failed", "POST", b+"dns-ha-done/oxidns/rules/update", `{}`, J, ""),
		get("review panel delay timeout 4301 digits", b+"gw/mihomo/proxy/delay?name=HK&timeout="+strings.Repeat("1", 4301), ""),
		get("review panel delay timeout spaces", b+"gw/mihomo/proxy/delay?name=HK&timeout=%E3%80%80%2B1_000%0B", ""),
		get("review panel logs Arabic cursor", b+"gw56/mihomo/logs?after=%D9%A1%D9%A2", ""),
		with("review panel name 513", "DELETE", b+"gw/mihomo/connection?name="+strings.Repeat("%E8%8A%82", 513), "", "", ""),
		get("review panel name 512", b+"gw/mihomo/proxy/delay?name="+strings.Repeat("%E8%8A%82", 512), ""),
		with("review panel switch name list", "PUT", b+"gw/mihomo/proxy?name=G", `{"name": ["HK"]}`, J, ""),
		with("review panel mode list", "PATCH", b+"gw/mihomo/configs", `{"mode": ["rule"]}`, J, ""),
		get("review panel core update force=0", b+"gw16/mihomo/core/update?force=0", ""),
	}
}

func TestPanelDifferentialAgainstFlask(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	fake := &fakePanelAgent{}
	agentServer := httptest.NewServer(fake)
	defer agentServer.Close()
	port, _ := strconv.Atoi(agentServer.URL[strings.LastIndex(agentServer.URL, ":")+1:])
	closedListener, _ := net.Listen("tcp4", "127.0.0.1:0")
	closed := closedListener.Addr().(*net.TCPAddr).Port
	closedListener.Close()

	base := t.TempDir()
	seedDir, pyDir, goDir := filepath.Join(base, "seed"), filepath.Join(base, "python"), filepath.Join(base, "go")
	for _, dir := range []string{seedDir, pyDir, goDir} {
		os.MkdirAll(dir, 0o755)
	}
	panelSeed(t, seedDir, port, closed)
	copyDir(t, seedDir, pyDir)
	copyDir(t, seedDir, goDir)
	pyBase, stopPython := startPython(t, pyDir)
	defer stopPython()
	goBase, stopGo := startGo(t, goDir)
	defer stopGo()
	pyToken, goToken := login(t, pyBase), login(t, goBase)
	failed := 0
	for _, r := range panelRequests() {
		fake.setPhase(r.phase)
		py := send(t, pyBase, r.request, pyToken, nil)
		pyCalls := fake.take()
		gor := send(t, goBase, r.request, goToken, nil)
		goCalls := fake.take()
		diffs := compare(r.request, py, gor)
		if strings.Join(pyCalls, "\n") != strings.Join(goCalls, "\n") {
			diffs = append(diffs, fmt.Sprintf("agent requests\n    python %q\n    go     %q", pyCalls, goCalls))
		}
		if len(diffs) > 0 {
			failed++
			t.Errorf("DIFF %s %s %s:\n  %s", r.name, r.method, r.path, strings.Join(diffs, "\n  "))
		} else {
			t.Logf("same %-34s %s %s → %d (%d agent requests)", r.name, r.method, r.path, py.status, len(pyCalls))
		}
	}
	if py, gofiles := strings.Join(dataFiles(t, pyDir), " "), strings.Join(dataFiles(t, goDir), " "); py != gofiles {
		failed++
		t.Errorf("DIFF files under DATA_DIR:\n  python %s\n  go     %s", py, gofiles)
	}
	t.Logf("%d requests + agent requests + DATA_DIR files, %d differences", len(panelRequests()), failed)
}

// --- ranking history collector (oxidns_ranking_history.start_loop) -------------

// fakeRankingAgent is a loopback OxiDNS Agent of HISTORY_AGENT_VERSION:
// a valid dashboard snapshot and deterministic complete rankings per window.
type fakeRankingAgent struct {
	mu    sync.Mutex
	calls map[string]int // "<since>-<until>" → requests
}

func (f *fakeRankingAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	send := func(v any) {
		data, _ := jsonx.Default(v)
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}
	switch r.URL.Path {
	case "/api/oxidns/overview":
		send(jsonx.Obj("success", true, "health", jsonx.Obj("version", "1.6.1", "status", "ok", "uptime_ms", 1, "instance_id", "core-one"),
			"capabilities", jsonx.Obj("query_recorders", []any{"records"}), "collected_at_ms", 1000,
			"source_revision", strings.Repeat("a", 64), "runtime_revision", strings.Repeat("d", 64), "metrics", ""))
	case "/api/oxidns/rankings":
		q := r.URL.Query()
		sinceMS, _ := strconv.ParseInt(q.Get("since_ms"), 10, 64)
		untilMS, _ := strconv.ParseInt(q.Get("until_ms"), 10, 64)
		f.mu.Lock()
		f.calls[fmt.Sprintf("%d-%d", sinceMS, untilMS)]++
		f.mu.Unlock()
		n := sinceMS / 900_000
		sample := n%40 + 3
		section := func(rows ...any) *jsonx.Object {
			return jsonx.Obj("sample_size", sample, "at_limit", false, "rows", rows)
		}
		send(jsonx.Obj("success", true, "recorder", q.Get("recorder"), "retention_days", 7,
			"clients", section(jsonx.Obj("key", "192.0.2.1", "count", sample-1), jsonx.Obj("key", fmt.Sprintf("192.0.2.%d", n%5+2), "count", 1)),
			"domains", section(jsonx.Obj("key", fmt.Sprintf("d%d.例子.test.", n%7), "count", sample), jsonx.Obj("key", "a.test.", "count", 2)),
			"blocked", jsonx.Obj("available", n%3 != 0, "sample_size", 1, "at_limit", false,
				"rows", []any{jsonx.Obj("key", fmt.Sprintf("ads%d.test.", n%2), "count", 1)})))
	default:
		http.NotFound(w, r)
	}
}

// rankingRows is every stored row of a ranking database (with rowid),
// sqlite_master and the journal mode.
func rankingRows(t *testing.T, path string) (string, int) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var out []string
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		return "unreadable: " + err.Error(), 0
	}
	out = append(out, "journal_mode="+mode)
	slots := 0
	for _, statement := range []string{"SELECT type, name, sql FROM sqlite_master ORDER BY name",
		"SELECT rowid, * FROM ranking_periods ORDER BY rowid", "SELECT rowid, * FROM ranking_rows ORDER BY rowid",
		"SELECT rowid, * FROM ranking_slots ORDER BY rowid"} {
		rows, err := db.Query(statement)
		if err != nil {
			return "unreadable: " + err.Error(), 0
		}
		columns, _ := rows.Columns()
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			rows.Scan(pointers...)
			out = append(out, fmt.Sprintf("%q", values))
			if strings.Contains(statement, "ranking_slots") {
				slots++
			}
		}
		rows.Close()
	}
	return strings.Join(out, "\n"), slots
}

// TestRankingCollectorDifferentialAgainstFlask lets both servers' ranking
// collectors run their first round (60 s after start, up to SLOTS_PER_ROUND
// slots) against one loopback Agent and compares the stored databases row
// by row, and that both asked for the same windows.
func TestRankingCollectorDifferentialAgainstFlask(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	fake := &fakeRankingAgent{calls: map[string]int{}}
	agentServer := httptest.NewServer(fake)
	defer agentServer.Close()
	port, _ := strconv.Atoi(agentServer.URL[strings.LastIndex(agentServer.URL, ":")+1:])
	base := t.TempDir()
	seedDir, pyDir, goDir := filepath.Join(base, "seed"), filepath.Join(base, "python"), filepath.Join(base, "go")
	for _, dir := range []string{seedDir, pyDir, goDir} {
		os.MkdirAll(dir, 0o755)
	}
	r, err := repo.OpenDefault(seedDir)
	if err != nil {
		t.Fatal(err)
	}
	heartbeat := naiveISO(time.Now().Add(30 * time.Second))
	agent := func(id, version string) *jsonx.Object {
		return jsonx.Obj("id", id, "name", id, "token", "tok-"+id, "profile_id", "default", "service_type", "oxidns",
			"host", "127.0.0.1", "port", port, "version", version, "enabled", true, "status", "online", "last_heartbeat", heartbeat)
	}
	_, err = r.UpdateSystemTransaction(context.Background(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", []any{agent("dns", "1.8.60-go"), agent("dns-old", "1.8.54-go")})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	copyDir(t, seedDir, pyDir)
	copyDir(t, seedDir, goDir)
	_, stopPython := startPython(t, pyDir)
	_, stopGo := startGo(t, goDir)
	pyPath := filepath.Join(pyDir, "metrics", "oxidns_rankings.sqlite3")
	goPath := filepath.Join(goDir, "metrics", "oxidns_rankings.sqlite3")
	deadline := time.Now().Add(150 * time.Second)
	for time.Now().Before(deadline) {
		_, pySlots := rankingRows(t, pyPath)
		_, goSlots := rankingRows(t, goPath)
		if pySlots >= 24 && goSlots >= 24 {
			break
		}
		time.Sleep(2 * time.Second)
	}
	stopGo()
	stopPython()
	pyRows, pySlots := rankingRows(t, pyPath)
	goRows, goSlots := rankingRows(t, goPath)
	if pySlots != 24 || goSlots != 24 {
		t.Fatalf("slots stored: python %d go %d", pySlots, goSlots)
	}
	if pyRows != goRows {
		t.Errorf("DIFF ranking database:\n  python %s\n  go     %s", pyRows, goRows)
	}
	fake.mu.Lock()
	for window, n := range fake.calls {
		if n != 2 {
			t.Errorf("DIFF window %s asked %d times (python + go should be 2)", window, n)
		}
	}
	windows := len(fake.calls)
	fake.mu.Unlock()
	if !strings.Contains(strings.Join(dataFiles(t, pyDir), " "), "metrics/oxidns_rankings.sqlite3") ||
		!strings.Contains(strings.Join(dataFiles(t, goDir), " "), "metrics/oxidns_rankings.sqlite3") {
		t.Errorf("database files: python %v go %v", dataFiles(t, pyDir), dataFiles(t, goDir))
	}
	t.Logf("ranking collector: %d windows, %d stored lines, identical=%v", windows, strings.Count(pyRows, "\n")+1, pyRows == goRows)
}
