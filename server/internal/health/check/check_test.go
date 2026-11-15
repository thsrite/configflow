package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

// Ports of the pure cases of backend/test_region_check.py and
// backend/test_domain_probe.py (the job/route/generation cases are C37/C18,
// see PROGRESS-config C16).

func res(id string, status any, finalURL string, redirects []any, markers *jsonx.Object, errText string) *jsonx.Object {
	if finalURL == "" {
		finalURL = "https://x.test/" + id
	}
	if redirects == nil {
		redirects = []any{}
	}
	if markers == nil {
		markers = jsonx.NewObject()
	}
	return jsonx.Obj("id", id, "status", status, "final_url", finalURL, "redirects", redirects, "markers", markers,
		"error", errText, "elapsed_ms", jsonx.Int(10))
}

func ok(id string) *jsonx.Object                  { return res(id, jsonx.Int(200), "", nil, nil, "") }
func st(id string, status int) *jsonx.Object      { return res(id, jsonx.Int(status), "", nil, nil, "") }
func mk(id string, m *jsonx.Object) *jsonx.Object { return res(id, jsonx.Int(200), "", nil, m, "") }
func failed(id, text string) *jsonx.Object        { return res(id, jsonx.Int(200), "", nil, nil, text) }
func moved(id string, status int, url string) *jsonx.Object {
	return res(id, jsonx.Int(status), url, nil, nil, "")
}

func evaluate(t *testing.T, id string, results ...*jsonx.Object) *jsonx.Object {
	service, _ := ServiceByID(id)
	byID := jsonx.NewObject()
	for _, r := range results {
		byID.Set(r.GetString("id"), r)
	}
	out, err := service.Evaluate(byID)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOpenAIObservedDatacenterResponseIsAvailableWithNote(t *testing.T) {
	r := evaluate(t, "openai", mk("openai-compliance", jsonx.Obj("consent", "cookie_consent_required")),
		res("openai-ios", jsonx.Int(403), "", nil, jsonx.Obj("dc", `"type":"dc"`), ""),
		mk("openai-trace", jsonx.Obj("loc", "US")))
	if r.GetString("status") != "available" || r.GetString("region") != "US" || !strings.Contains(r.GetString("reason"), "数据中心") {
		t.Fatal(r)
	}
}

func TestOpenAIBlockedAndUnknown(t *testing.T) {
	for _, c := range []struct {
		compliance, ios *jsonx.Object
		status, reason  string
	}{
		{jsonx.Obj("unsupported", "unsupported_country"), jsonx.NewObject(), "blocked", "所在地区不受支持"},
		{jsonx.Obj("consent", "x"), jsonx.Obj("vpn", "VPN"), "blocked", "被识别为代理 / VPN"},
		{jsonx.NewObject(), jsonx.NewObject(), "unknown", "响应不符合已知模式"},
	} {
		r := evaluate(t, "openai", mk("openai-compliance", c.compliance), mk("openai-ios", c.ios))
		if r.GetString("status") != c.status || r.GetString("reason") != c.reason {
			t.Fatal(r)
		}
	}
}

func TestClaude(t *testing.T) {
	blocked := evaluate(t, "claude", res("claude-web", jsonx.Int(200), "https://www.anthropic.com/app-unavailable-in-region",
		[]any{"https://claude.ai/"}, nil, ""), mk("claude-trace", jsonx.Obj("loc", "HK")))
	if blocked.GetString("status") != "blocked" || blocked.GetString("region") != "HK" {
		t.Fatal(blocked)
	}
	if evaluate(t, "claude", ok("claude-web")).GetString("status") != "available" {
		t.Fatal("available")
	}
	challenge := evaluate(t, "claude", st("claude-web", 403))
	if challenge.GetString("status") != "unknown" || !strings.Contains(challenge.GetString("reason"), "Cloudflare") {
		t.Fatal(challenge)
	}
	if evaluate(t, "claude", failed("claude-web", "timeout")).GetString("status") != "unknown" {
		t.Fatal("timeout")
	}
}

func TestNetflix(t *testing.T) {
	full := evaluate(t, "netflix", res("netflix-licensed-1", jsonx.Int(200), "https://www.netflix.com/hk-en/title/81280792",
		[]any{"https://www.netflix.com/title/81280792"}, nil, ""), st("netflix-licensed-2", 404), ok("netflix-original"))
	if full.GetString("status") != "available" || full.GetString("region") != "HK" || full.GetString("reason") != "完整解锁" {
		t.Fatal(full)
	}
	originals := evaluate(t, "netflix", st("netflix-licensed-1", 404), st("netflix-licensed-2", 404),
		moved("netflix-original", 200, "https://www.netflix.com/title/80018499"))
	if originals.GetString("status") != "partial" || originals.GetString("region") != "US" {
		t.Fatal(originals)
	}
	if evaluate(t, "netflix", st("netflix-licensed-1", 403), st("netflix-licensed-2", 403), st("netflix-original", 403)).GetString("status") != "blocked" {
		t.Fatal("blocked")
	}
	if evaluate(t, "netflix", failed("netflix-licensed-1", "timeout")).GetString("status") != "unknown" {
		t.Fatal("unknown")
	}
	if evaluate(t, "claude", failed("claude-web", "tls")).GetString("status") != "unknown" {
		t.Fatal("error with status")
	}
}

func TestYouTube(t *testing.T) {
	if evaluate(t, "youtube", mk("youtube-premium", jsonx.Obj("gl", "US"))).GetString("region") != "US" ||
		evaluate(t, "youtube", mk("youtube-premium", jsonx.Obj("blocked", "x", "gl", "HK"))).GetString("status") != "blocked" ||
		evaluate(t, "youtube", mk("youtube-premium", jsonx.Obj("gl", "CN"))).GetString("status") != "blocked" ||
		evaluate(t, "youtube", ok("youtube-premium")).GetString("status") != "unknown" {
		t.Fatal("youtube")
	}
}

func TestServiceRequestsFitAgentLimits(t *testing.T) {
	requests := ServiceRequests()
	if requests[0].(*jsonx.Object).GetString("id") != "exit" || len(requests) > 12 {
		t.Fatal(len(requests))
	}
	seen := map[string]bool{}
	for _, r := range requests {
		id := r.(*jsonx.Object).GetString("id")
		if seen[id] || r.(*jsonx.Object).GetObject("markers").Len() > 8 {
			t.Fatal(id)
		}
		seen[id] = true
	}
}

func TestClassifyDomainResult(t *testing.T) {
	url := "https://shop.test/"
	for _, c := range []struct {
		result any
		state  string
	}{
		{st("d", 451), "restricted"},
		{res("d", jsonx.Int(403), "", nil, jsonx.Obj("block", "not available in your country"), ""), "restricted"},
		{moved("d", 200, "https://shop.test/unavailable-region"), "restricted"},
		{moved("d", 200, "https://shop.test/"), "ok"},
		{moved("d", 403, "https://shop.test/"), "denied"},
		{failed("d", "timeout"), "error"},
		{nil, "error"},
	} {
		out, err := ClassifyDomainResult(c.result, url)
		if err != nil || out.GetString("state") != c.state {
			t.Fatal(c, out, err)
		}
	}
}

func TestSummarizeDomain(t *testing.T) {
	for _, c := range []struct {
		states  []string
		verdict string
	}{{[]string{"restricted", "ok"}, "suspected"}, {[]string{"restricted", "restricted"}, "all_restricted"},
		{[]string{"ok", "denied"}, "no_difference"}, {[]string{"error", "error"}, "unreachable"}} {
		per := jsonx.NewObject()
		for i, s := range c.states {
			per.Set("t"+string(rune('0'+i)), jsonx.Obj("state", s))
		}
		out, err := SummarizeDomain(per)
		if err != nil || out.GetString("verdict") != c.verdict {
			t.Fatal(c, out)
		}
	}
}

func TestRegionOfName(t *testing.T) {
	for name, want := range map[string]string{"🇺🇸 美国 01": "US", "HK-IEPL-02": "HK", "RUSSIA 01": "", "Tokyo 03": "JP"} {
		if got := RegionOfName(name); got != want {
			t.Errorf("%s: %q", name, got)
		}
	}
}

func TestPickTargets(t *testing.T) {
	target := func(name, kind string, alive bool) any { return jsonx.Obj("name", name, "type", kind, "alive", alive) }
	targets := []any{target("DIRECT", "Direct", true), target("GLOBAL", "Selector", true),
		target("ConfigFlow-Region-Probe", "Selector", true), target("自动选择", "URLTest", true),
		target("🚀 节点选择", "Selector", true), target("🇭🇰 香港 01", "Shadowsocks", false),
		target("🇭🇰 香港 02", "Shadowsocks", true), target("🇺🇸 美国 01", "Vmess", true),
		target("🇺🇸 美国 02", "Vmess", true), target("未知节点", "Trojan", true)}
	picked, err := PickTargets(targets, "🚀 节点选择")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range picked {
		got = append(got, p.(*jsonx.Object).GetString("name")+"/"+p.(*jsonx.Object).GetString("kind"))
	}
	if strings.Join(got, ",") != "DIRECT/direct,🚀 节点选择/group,自动选择/group,🇭🇰 香港 02/node,🇺🇸 美国 01/node" {
		t.Fatal(got)
	}
}

// test_stores_treat_corrupt_files_as_empty (probe and region stores; the
// traffic store case is A05's).
func TestStoresTreatCorruptFilesAsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a1.json")
	os.WriteFile(path, []byte("{not json"), 0o644)
	probes := NewProbeStore(dir)
	if hosts, err := probes.Load("a1", time.Time{}); err != nil || hosts.Len() != 0 {
		t.Fatal(hosts, err)
	}
	probes.Delete("a1")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("probe file kept")
	}
	os.WriteFile(path, []byte("{not json"), 0o644)
	regions := NewRegionStore(dir)
	if regions.Load("a1").GetObject("domains").Len() != 0 {
		t.Fatal("domains")
	}
	regions.Delete("a1")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("region file kept")
	}
	if err := regions.Delete("a1"); err != nil { // a missing file is fine
		t.Fatal(err)
	}
}

func stats(okCount, total int, delays ...int) *jsonx.Object {
	list := []any{}
	if delays == nil {
		for i := 0; i < okCount; i++ {
			list = append(list, jsonx.Int(100))
		}
	} else {
		for _, d := range delays {
			list = append(list, jsonx.Int(d))
		}
	}
	errs := jsonx.NewObject()
	if total > okCount {
		errs.Set("timeout", jsonx.Int(total-okCount))
	}
	return jsonx.Obj("ok", jsonx.Int(okCount), "total", jsonx.Int(total), "delays", list, "errors", errs)
}

func TestJudge(t *testing.T) {
	defaults := DefaultJudgeOptions()
	with := func(f func(*JudgeOptions)) JudgeOptions { o := defaults; f(&o); return o }
	empty := jsonx.Obj("ok", jsonx.Int(0), "total", jsonx.Int(0), "delays", []any{}, "errors", jsonx.NewObject())
	for i, c := range []struct {
		direct, proxy *jsonx.Object
		options       JudgeOptions
		verdict       string
		target        any
		confidence    int64
	}{
		{stats(0, 2), stats(2, 2), defaults, "needs_proxy", "proxy", 85},
		{stats(0, 2), stats(2, 2), with(func(o *JudgeOptions) { o.PassiveFailing = true }), "needs_proxy", "proxy", 99},
		{stats(0, 3), stats(2, 3), defaults, "needs_proxy", "proxy", 70},
		{stats(0, 2), stats(2, 2), with(func(o *JudgeOptions) { o.DirectHealthy = false }), "direct_down", nil, 0},
		{stats(2, 2), stats(0, 2), defaults, "direct_only", "direct", 85},
		{stats(2, 2), stats(0, 2), with(func(o *JudgeOptions) { o.ProxyHealthy = false }), "proxy_down", nil, 0},
		{stats(2, 2, 100, 120), stats(2, 2, 300, 300), defaults, "both_ok", "direct", 60},
		{stats(2, 2, 900, 900), stats(2, 2, 200, 200), defaults, "both_ok", "proxy", 50},
		{stats(0, 2), stats(0, 2), defaults, "unreachable", nil, 0},
		{stats(0, 2), stats(0, 2), with(func(o *JudgeOptions) { o.ProxyHealthy = false }), "proxy_down", nil, 0},
		{stats(1, 3), stats(1, 3), defaults, "flaky", nil, 0},
		{stats(1, 1), empty, defaults, "incomplete", nil, 0},
	} {
		out, err := Judge(c.direct, c.proxy, c.options)
		if err != nil || out.GetString("verdict") != c.verdict || !jsonx.Equal(out.Value("target"), c.target) ||
			out.GetInt("confidence") != c.confidence || len(out.GetList("reasons")) == 0 {
			t.Errorf("case %d: %v %v", i, out, err)
		}
	}
}

func TestJudgeExplainsMissingProxyPath(t *testing.T) {
	out, _ := Judge(stats(1, 1), jsonx.Obj("ok", jsonx.Int(0), "total", jsonx.Int(1), "delays", []any{},
		"errors", jsonx.Obj("no_path", jsonx.Int(1))), DefaultJudgeOptions())
	if out.GetString("verdict") != "incomplete" || !strings.Contains(out.GetList("reasons")[0].(string), "部署配置") {
		t.Fatal(out)
	}
}

func TestProbeURLFollowsPort(t *testing.T) {
	if ProbeURL("a.com", jsonx.Int(443)) != "https://a.com/" || ProbeURL("a.com", jsonx.Int(0)) != "https://a.com/" ||
		ProbeURL("a.com", jsonx.Int(80)) != "http://a.com/" || ProbeURL("a.com", jsonx.Int(8443)) != "https://a.com:8443/" {
		t.Fatal("probe url")
	}
}

func TestProbeStoreKeepsLatestAndExpires(t *testing.T) {
	store := NewProbeStore(t.TempDir())
	old := time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local)
	store.Save("a1", jsonx.Obj("old.com", jsonx.Obj("checked_at", NaiveISOSeconds(old), "verdict", "unreachable")), old)
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.Local)
	store.Save("a1", jsonx.Obj("new.com", jsonx.Obj("checked_at", NaiveISOSeconds(now), "verdict", "needs_proxy")), now)
	hosts, _ := store.Load("a1", now)
	if strings.Join(hosts.Keys(), ",") != "new.com" {
		t.Fatal(hosts.Keys())
	}
}

func TestDomainSuggestionPrefersHighestConfidence(t *testing.T) {
	probe := func(host, verdict string, target any, confidence int, checked string) any {
		return jsonx.Obj("probe", jsonx.Obj("host", host, "verdict", verdict, "target", target, "confidence", jsonx.Int(confidence), "checked_at", checked))
	}
	out, _ := DomainSuggestion([]any{probe("a", "both_ok", "direct", 60, "2"), probe("b", "needs_proxy", "proxy", 85, "1"), jsonx.Obj("probe", nil)})
	if out.GetString("host") != "b" {
		t.Fatal(out)
	}
	out, _ = DomainSuggestion([]any{probe("c", "unreachable", nil, 0, "3")})
	if out.GetString("verdict") != "unreachable" {
		t.Fatal(out)
	}
	if out, _ := DomainSuggestion([]any{jsonx.Obj("probe", nil)}); out != nil {
		t.Fatal(out)
	}
}

func TestProbeStoreKeepsTheNewest5000Hosts(t *testing.T) {
	store := NewProbeStore(t.TempDir())
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.Local)
	results := jsonx.NewObject()
	for i := 0; i < MaxProbedHosts+3; i++ {
		checked := "2026-10-01T00:00:00"
		if i < 3 {
			checked = "2026-09-20T00:00:00"
		}
		results.Set("h"+jsonx.Int(i).String(), jsonx.Obj("checked_at", checked))
	}
	store.Save("a", results, now)
	hosts, _ := store.Load("a", now)
	if hosts.Len() != MaxProbedHosts || hosts.Has("h0") || !hosts.Has("h3") {
		t.Fatal(hosts.Len())
	}
}
