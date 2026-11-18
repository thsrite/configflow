package check

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"configflow/internal/model"
)

func res(id string, status int64, finalURL string, redirects []string, markers map[string]string, errText string) RegionResult {
	if finalURL == "" {
		finalURL = "https://x.test/" + id
	}
	if redirects == nil {
		redirects = []string{}
	}
	var m model.Map[NullableText]
	for k, v := range markers {
		m.Set(k, NullableText{Value: v, Valid: true})
	}
	code := model.Int(status)
	return RegionResult{
		ID:        id,
		Status:    &code,
		FinalURL:  finalURL,
		Redirects: redirects,
		Markers:   m,
		Error:     errText,
		hasID:     true,
	}
}

func ok(id string) RegionResult                      { return res(id, 200, "", nil, nil, "") }
func st(id string, status int64) RegionResult        { return res(id, status, "", nil, nil, "") }
func mk(id string, m map[string]string) RegionResult { return res(id, 200, "", nil, m, "") }
func failed(id, text string) RegionResult            { return res(id, 200, "", nil, nil, text) }
func moved(id string, status int64, url string) RegionResult {
	return res(id, status, url, nil, nil, "")
}

func evaluate(t *testing.T, id string, results ...RegionResult) ServiceVerdict {
	t.Helper()
	service, _ := ServiceByID(id)
	byID := make(map[string]RegionResult, len(results))
	for _, r := range results {
		byID[r.ID] = r
	}
	out, err := service.Evaluate(byID)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOpenAIObservedDatacenterResponseIsAvailableWithNote(t *testing.T) {
	r := evaluate(t, "openai",
		mk("openai-compliance", map[string]string{"consent": "cookie_consent_required"}),
		res("openai-ios", 403, "", nil, map[string]string{"dc": `"type":"dc"`}, ""),
		mk("openai-trace", map[string]string{"loc": "US"}))
	if r.Status != "available" || r.Region.Value != "US" || !strings.Contains(r.Reason, "数据中心") {
		t.Fatal(r)
	}
}

func TestOpenAIBlockedAndUnknown(t *testing.T) {
	for _, c := range []struct {
		compliance, ios map[string]string
		status, reason  string
	}{
		{map[string]string{"unsupported": "unsupported_country"}, nil, "blocked", msgOpenAIUnsupported},
		{map[string]string{"consent": "x"}, map[string]string{"vpn": "VPN"}, "blocked", msgOpenAIVPN},
		{nil, nil, "unknown", msgResponseUnknown},
	} {
		r := evaluate(t, "openai", mk("openai-compliance", c.compliance), mk("openai-ios", c.ios))
		if r.Status != c.status || r.Reason != c.reason {
			t.Fatal(r)
		}
	}
}

func TestClaude(t *testing.T) {
	blocked := evaluate(t, "claude",
		res("claude-web", 200, "https://www.anthropic.com/app-unavailable-in-region", []string{"https://claude.ai/"}, nil, ""),
		mk("claude-trace", map[string]string{"loc": "HK"}))
	if blocked.Status != "blocked" || blocked.Region.Value != "HK" {
		t.Fatal(blocked)
	}
	if evaluate(t, "claude", ok("claude-web")).Status != "available" {
		t.Fatal("available")
	}
	challenge := evaluate(t, "claude", st("claude-web", 403))
	if challenge.Status != "unknown" || !strings.Contains(challenge.Reason, "Cloudflare") {
		t.Fatal(challenge)
	}
	if evaluate(t, "claude", failed("claude-web", "timeout")).Status != "unknown" {
		t.Fatal("timeout")
	}
}

func TestNetflix(t *testing.T) {
	full := evaluate(t, "netflix",
		res("netflix-licensed-1", 200, "https://www.netflix.com/hk-en/title/81280792", []string{"https://www.netflix.com/title/81280792"}, nil, ""),
		st("netflix-licensed-2", 404), ok("netflix-original"))
	if full.Status != "available" || full.Region.Value != "HK" || full.Reason != msgNetflixFull {
		t.Fatal(full)
	}
	originals := evaluate(t, "netflix",
		st("netflix-licensed-1", 404), st("netflix-licensed-2", 404),
		moved("netflix-original", 200, "https://www.netflix.com/title/80018499"))
	if originals.Status != "partial" || originals.Region.Value != "US" {
		t.Fatal(originals)
	}
	if evaluate(t, "netflix", st("netflix-licensed-1", 403), st("netflix-licensed-2", 403), st("netflix-original", 403)).Status != "blocked" {
		t.Fatal("blocked")
	}
	if evaluate(t, "netflix", failed("netflix-licensed-1", "timeout")).Status != "unknown" {
		t.Fatal("unknown")
	}
	if evaluate(t, "claude", failed("claude-web", "tls")).Status != "unknown" {
		t.Fatal("error with status")
	}
}

func TestYouTube(t *testing.T) {
	if evaluate(t, "youtube", mk("youtube-premium", map[string]string{"gl": "US"})).Region.Value != "US" ||
		evaluate(t, "youtube", mk("youtube-premium", map[string]string{"blocked": "x", "gl": "HK"})).Status != "blocked" ||
		evaluate(t, "youtube", mk("youtube-premium", map[string]string{"gl": "CN"})).Status != "blocked" ||
		evaluate(t, "youtube", ok("youtube-premium")).Status != "unknown" {
		t.Fatal("youtube")
	}
}

func TestServiceRequestsFitAgentLimits(t *testing.T) {
	requests := BuildServiceRequests()
	if requests[0].ID != "exit" || len(requests) > 12 {
		t.Fatal(len(requests))
	}
	seen := map[string]bool{}
	for _, r := range requests {
		if seen[r.ID] || r.Markers.Len() > 8 {
			t.Fatal(r.ID)
		}
		seen[r.ID] = true
	}
}

func ptrResult(r RegionResult) *RegionResult { return &r }

func TestClassifyDomainResult(t *testing.T) {
	url := "https://shop.test/"
	for _, c := range []struct {
		result *RegionResult
		state  string
	}{
		{ptrResult(st("d", 451)), "restricted"},
		{ptrResult(res("d", 403, "", nil, map[string]string{"block": "not available in your country"}, "")), "restricted"},
		{ptrResult(moved("d", 200, "https://shop.test/unavailable-region")), "restricted"},
		{ptrResult(moved("d", 200, "https://shop.test/")), "ok"},
		{ptrResult(moved("d", 403, "https://shop.test/")), "denied"},
		{ptrResult(failed("d", "timeout")), "error"},
		{nil, "error"},
	} {
		out, err := ClassifyDomain(c.result, url)
		if err != nil || out.State != c.state {
			t.Fatal(c, out, err)
		}
	}
}

func TestSummarizeDomain(t *testing.T) {
	for _, c := range []struct {
		states  []string
		verdict string
	}{
		{[]string{"restricted", "ok"}, "suspected"},
		{[]string{"restricted", "restricted"}, "all_restricted"},
		{[]string{"ok", "denied"}, "no_difference"},
		{[]string{"error", "error"}, "unreachable"},
	} {
		var per model.Map[TargetState]
		for i, s := range c.states {
			per.Set("t"+string(rune('0'+i)), TargetState{State: Text(s)})
		}
		out := SummarizeTargets(per)
		if out.Verdict != c.verdict {
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
	target := func(name, kind string, alive bool) AvailableTarget {
		return AvailableTarget{Name: name, Type: kind, Alive: alive}
	}
	targets := []AvailableTarget{
		target("DIRECT", "Direct", true),
		target("GLOBAL", "Selector", true),
		target("ConfigFlow-Region-Probe", "Selector", true),
		target("自动选择", "URLTest", true),
		target("🚀 节点选择", "Selector", true),
		target("🇭🇰 香港 01", "Shadowsocks", false),
		target("🇭🇰 香港 02", "Shadowsocks", true),
		target("🇺🇸 美国 01", "Vmess", true),
		target("🇺🇸 美国 02", "Vmess", true),
		target("未知节点", "Trojan", true),
	}
	picked := ChooseTargets(targets, "🚀 节点选择")
	var got []string
	for _, p := range picked {
		got = append(got, p.Name+"/"+p.Kind)
	}
	if strings.Join(got, ",") != "DIRECT/direct,🚀 节点选择/group,自动选择/group,🇭🇰 香港 02/node,🇺🇸 美国 01/node" {
		t.Fatal(got)
	}
}

func TestStoresTreatCorruptFilesAsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a1.json")
	_ = os.WriteFile(path, []byte("{not json"), 0o644)
	probes := NewProbeStore(dir)
	if hosts := probes.LoadEntries("a1", time.Time{}); hosts.Len() != 0 {
		t.Fatal(hosts)
	}
	_ = probes.Delete("a1")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("probe file kept")
	}
	_ = os.WriteFile(path, []byte("{not json"), 0o644)
	regions := NewRegionStore(dir)
	if regions.LoadData("a1").Domains.Len() != 0 {
		t.Fatal("domains")
	}
	_ = regions.Delete("a1")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("region file kept")
	}
	if err := regions.Delete("a1"); err != nil {
		t.Fatal(err)
	}
}

func stats(okCount, total int64, delays ...float64) *PathStats {
	var list []float64
	if delays == nil {
		for i := int64(0); i < okCount; i++ {
			list = append(list, 100)
		}
	} else {
		list = append(list, delays...)
	}
	var errs model.Map[model.Int]
	if total > okCount {
		errs.Set("timeout", model.Int(total-okCount))
	}
	return &PathStats{
		OK:     model.Int(okCount),
		Total:  model.Int(total),
		Delays: list,
		Errors: errs,
	}
}

func TestJudge(t *testing.T) {
	defaults := DefaultJudgeOptions()
	with := func(f func(*JudgeOptions)) JudgeOptions { o := defaults; f(&o); return o }
	empty := &PathStats{Delays: []float64{}}
	for i, c := range []struct {
		direct, proxy *PathStats
		options       JudgeOptions
		verdict       string
		target        string
		confidence    int64
	}{
		{stats(0, 2), stats(2, 2), defaults, "needs_proxy", "proxy", 85},
		{stats(0, 2), stats(2, 2), with(func(o *JudgeOptions) { o.PassiveFailing = true }), "needs_proxy", "proxy", 99},
		{stats(0, 3), stats(2, 3), defaults, "needs_proxy", "proxy", 70},
		{stats(0, 2), stats(2, 2), with(func(o *JudgeOptions) { o.DirectHealthy = false }), "direct_down", "", 0},
		{stats(2, 2), stats(0, 2), defaults, "direct_only", "direct", 85},
		{stats(2, 2), stats(0, 2), with(func(o *JudgeOptions) { o.ProxyHealthy = false }), "proxy_down", "", 0},
		{stats(2, 2, 100, 120), stats(2, 2, 300, 300), defaults, "both_ok", "direct", 60},
		{stats(2, 2, 900, 900), stats(2, 2, 200, 200), defaults, "both_ok", "proxy", 50},
		{stats(0, 2), stats(0, 2), defaults, "unreachable", "", 0},
		{stats(0, 2), stats(0, 2), with(func(o *JudgeOptions) { o.ProxyHealthy = false }), "proxy_down", "", 0},
		{stats(1, 3), stats(1, 3), defaults, "flaky", "", 0},
		{stats(1, 1), empty, defaults, "incomplete", "", 0},
	} {
		out := JudgePath(c.direct, c.proxy, c.options)
		wantValid := c.target != ""
		if out.Verdict != c.verdict || out.Target.Valid != wantValid || out.Target.Value != c.target ||
			out.Confidence != c.confidence || len(out.Reasons) == 0 {
			t.Errorf("case %d: %+v", i, out)
		}
	}
}

func TestJudgeExplainsMissingProxyPath(t *testing.T) {
	var errs model.Map[model.Int]
	errs.Set("no_path", 1)
	out := JudgePath(stats(1, 1), &PathStats{OK: 0, Total: 1, Errors: errs}, DefaultJudgeOptions())
	if out.Verdict != "incomplete" || !strings.Contains(out.Reasons[0], "部署配置") {
		t.Fatal(out)
	}
}

func TestProbeURLFollowsPort(t *testing.T) {
	if ProbeURL("a.com", 443) != "https://a.com/" || ProbeURL("a.com", 0) != "https://a.com/" ||
		ProbeURL("a.com", 80) != "http://a.com/" || ProbeURL("a.com", 8443) != "https://a.com:8443/" {
		t.Fatal("probe url")
	}
}

func mustTimedRecord(t *testing.T, raw string) TimedRecord {
	t.Helper()
	var r TimedRecord
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestProbeStoreKeepsLatestAndExpires(t *testing.T) {
	store := NewProbeStore(t.TempDir())
	old := time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local)
	var oldEntries model.Map[TimedRecord]
	oldEntries.Set("old.com", mustTimedRecord(t, `{"checked_at":"`+NaiveISOSeconds(old)+`","verdict":"unreachable"}`))
	if err := store.SaveEntries("a1", oldEntries, old); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.Local)
	var newEntries model.Map[TimedRecord]
	newEntries.Set("new.com", mustTimedRecord(t, `{"checked_at":"`+NaiveISOSeconds(now)+`","verdict":"needs_proxy"}`))
	if err := store.SaveEntries("a1", newEntries, now); err != nil {
		t.Fatal(err)
	}
	hosts := store.LoadEntries("a1", now)
	if strings.Join(hosts.Keys(), ",") != "new.com" {
		t.Fatal(hosts.Keys())
	}
}

func TestDomainSuggestionPrefersHighestConfidence(t *testing.T) {
	probe := func(raw string) HostProbe {
		return HostProbe{Probe: json.RawMessage(raw)}
	}
	out := DomainSuggestion([]HostProbe{
		probe(`{"host":"a","verdict":"both_ok","target":"direct","confidence":60,"checked_at":"2"}`),
		probe(`{"host":"b","verdict":"needs_proxy","target":"proxy","confidence":85,"checked_at":"1"}`),
		probe(`null`),
	})
	if out == nil || out.Host.Value != "b" {
		t.Fatal(out)
	}
	out = DomainSuggestion([]HostProbe{
		probe(`{"host":"c","verdict":"unreachable","target":null,"confidence":0,"checked_at":"3"}`),
	})
	if out == nil || out.Verdict.Value != "unreachable" {
		t.Fatal(out)
	}
	if out := DomainSuggestion([]HostProbe{probe(`null`)}); out != nil {
		t.Fatal(out)
	}
}

func TestProbeStoreKeepsTheNewest5000Hosts(t *testing.T) {
	store := NewProbeStore(t.TempDir())
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.Local)
	var results model.Map[TimedRecord]
	for i := 0; i < MaxProbedHosts+3; i++ {
		checked := "2026-10-01T00:00:00"
		if i < 3 {
			checked = "2026-09-20T00:00:00"
		}
		results.Set("h"+strconv.Itoa(i), TimedRecord{CheckedAt: StrictString(checked)})
	}
	if err := store.SaveEntries("a", results, now); err != nil {
		t.Fatal(err)
	}
	hosts := store.LoadEntries("a", now)
	_, hasH0 := hosts.Get("h0")
	_, hasH3 := hosts.Get("h3")
	if hosts.Len() != MaxProbedHosts || hasH0 || !hasH3 {
		t.Fatal(hosts.Len())
	}
}

func TestCacheExpiryAndBound(t *testing.T) {
	store := NewRegionStore(t.TempDir())
	now := time.Now()
	iso := func(tm time.Time) StrictString { return StrictString(tm.Format("2006-01-02T15:04:05.000000")) }
	err := store.UpdateData("a", func(data *RegionData) error {
		data.Websites.Set("old.test", TimedRecord{CheckedAt: iso(now.AddDate(0, 0, -31))})
		for i := 0; i < 1001; i++ {
			data.Websites.Set(strconv.Itoa(i)+".test", TimedRecord{CheckedAt: iso(now)})
		}
		return nil
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	websites := store.LoadData("a").Websites
	_, hasOld := websites.Get("old.test")
	_, has0 := websites.Get("0.test")
	_, has1000 := websites.Get("1000.test")
	if websites.Len() != 1000 || hasOld || has0 || !has1000 {
		t.Fatal(websites.Len())
	}
}
