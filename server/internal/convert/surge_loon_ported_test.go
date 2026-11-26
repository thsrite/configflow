package convert_test

// Hand-ported Python tests of the Surge (C19) and Loon (C20) converters. The
// recorded replay (surge_loon_recorded_test.go) also replays every converter
// call these and the other Python tests make; these keep the assertions
// readable and run without the recording.

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/rules"
	"configflow/internal/substore"
)

func mustJSON(t *testing.T, text string) *convert.JObj {
	t.Helper()
	v, err := convert.ParseJ([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return v.(*convert.JObj)
}

func withFakeSubStore(t *testing.T) {
	t.Helper()
	t.Cleanup(convert.OverrideMihomoEnvironmentForTest(t, corpusEnv(nil, nil)))
}

func surgeOf(t *testing.T, config *convert.JObj) string {
	t.Helper()
	cfg, err := decodeClientConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	out, err := convert.SurgeConfig(context.Background(), cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func loonOf(t *testing.T, config *convert.JObj) string {
	t.Helper()
	cfg, err := decodeClientConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	out, err := convert.LoonConfig(context.Background(), cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// udpSection is test_udp_fallback.py::_section.
func udpSection(output, name string) []string {
	_, after, _ := strings.Cut(output, "["+name+"]\n")
	before, _, _ := strings.Cut(after, "\n\n[")
	return strings.Split(strings.TrimSpace(before), "\n")
}

func wantLines(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines\n got  %q\n want %q", got, want)
	}
}

// --- test_snell_shadow_tls.py (export half; the import cases need C35's config_importer)

func surgeExport(t *testing.T, proxy string) string {
	t.Helper()
	withFakeSubStore(t)
	node := mustJSON(t, proxy)
	raw := proxyYAML(t, node)
	line, _, err := convert.SurgeNode(model.Node{Name: node.GetString("name"), ProxyString: &raw})
	if err != nil {
		t.Fatal(err)
	}
	return line
}

func proxyYAML(t *testing.T, node *convert.JObj) string {
	t.Helper()
	b, err := convert.CompactJ(node)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) // JSON is YAML; parse_uri_list reads it as an object
}

func TestSurgeExportSnellShadowTLSFromObfsOpts(t *testing.T) {
	line := surgeExport(t, `{"name":"HK","type":"snell","server":"1.2.3.4","port":443,"psk":"abcd","version":5,"reuse":true,"udp":true,
		"obfs-opts":{"mode":"shadow-tls","host":"www.apple.com","password":"stpw","version":3}}`)
	want := "HK = snell, 1.2.3.4, 443, psk=abcd, version=5, reuse=true, udp-relay=true, " +
		"shadow-tls-password=stpw, shadow-tls-sni=www.apple.com, shadow-tls-version=3"
	if line != want {
		t.Fatalf("got %q", line)
	}
}

func TestSurgeExportSnellObfsFromObfsOpts(t *testing.T) {
	line := surgeExport(t, `{"name":"HK","type":"snell","server":"1.2.3.4","port":443,"psk":"abcd","version":4,
		"obfs-opts":{"mode":"tls","host":"bing.com"}}`)
	if want := "HK = snell, 1.2.3.4, 443, psk=abcd, version=4, obfs=tls, obfs-host=bing.com"; line != want {
		t.Fatalf("got %q", line)
	}
}

// --- test_surge_loon_group_exclusion.py

func exclusionConfig(t *testing.T) *convert.JObj {
	return mustJSON(t, `{
		"profile_id": "home",
		"system_config": {"server_domain": "https://config.test", "config_token": "secret /&?"},
		"nodes": [{"id": "manual", "name": "手动-排除", "type": "ss", "server": "node.test", "port": 443,
		           "params": {"cipher": "aes-128-gcm", "password": "pw"}}],
		"subscriptions": [{"id": "sub", "name": "Feed"}],
		"subscription_aggregations": [{"id": "agg", "name": "Aggregate"}],
		"proxy_groups": [
			{"id": "reference", "name": "引用-排除", "type": "select", "manual_nodes": ["DIRECT"]},
			{"id": "mixed", "name": "混合", "type": "select", "manual_nodes": ["manual"],
			 "include_groups": ["reference"], "subscriptions": ["sub"], "aggregations": ["agg"],
			 "regex": "(?i)HK|港", "exclude_regex": "排除|倍率 [2-9]|&,+",
			 "aggregation_regex": "JP|日本", "aggregation_exclude_regex": "维护|到期"}
		]}`)
}

func policyQueries(t *testing.T, line string) map[string]url.Values {
	t.Helper()
	out := map[string]url.Values{}
	for _, part := range strings.Split(line, "policy-path = ")[1:] {
		raw, _, _ := strings.Cut(part, ",")
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		out[u.Path] = u.Query()
	}
	return out
}

func groupAt(config *convert.JObj, i int) *convert.JObj {
	return config.GetList("proxy_groups")[i].(*convert.JObj)
}

func TestSurgeFiltersEachDynamicSourceWithoutFilteringExplicitMembers(t *testing.T) {
	config := exclusionConfig(t)
	cfg := mustDecodeConfig(t, config)
	before, err := model.Marshal(cfg.Profile)
	if err != nil {
		t.Fatal(err)
	}
	group := groupAt(config, 1)
	gModel, err := convert.DecodeJ[model.ProxyGroup](group)
	if err != nil {
		t.Fatal(err)
	}
	line, err := convert.SurgeGroup(gModel, cfg, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "混合 = select, 手动-排除, 引用-排除, ") {
		t.Fatalf("line %q", line)
	}
	queries := policyQueries(t, line)
	want := map[string]url.Values{
		"/api/profiles/home/subscriptions/sub/proxies": {"format": {"surge"}, "token": {"secret /&?"},
			"regex": {group.GetString("regex")}, "exclude_regex": {group.GetString("exclude_regex")}},
		"/api/profiles/home/aggregations/agg/provider": {"format": {"surge"}, "token": {"secret /&?"},
			"regex": {group.GetString("aggregation_regex")}, "exclude_regex": {group.GetString("aggregation_exclude_regex")}},
	}
	if !reflect.DeepEqual(queries, want) {
		t.Fatalf("queries %v", queries)
	}
	after, err := model.Marshal(cfg.Profile)
	if strings.Contains(line, "policy-regex-filter") || err != nil || string(before) != string(after) {
		t.Fatal("filter or mutation")
	}
}

func TestSurgeFollowInheritsExclusionOnlyForBothSources(t *testing.T) {
	config := exclusionConfig(t)
	source := groupAt(config, 1)
	source.Set("regex", "")
	source.Set("aggregation_regex", "")
	follower := convert.JObjKV("id", "follow", "name", "跟随", "type", "select", "follow_group", "mixed",
		"regex", "wrong", "exclude_regex", "wrong", "aggregation_regex", "wrong", "aggregation_exclude_regex", "wrong")
	config.Set("proxy_groups", append(config.GetList("proxy_groups"), follower))
	fModel, err := convert.DecodeJ[model.ProxyGroup](follower)
	if err != nil {
		t.Fatal(err)
	}
	line, err := convert.SurgeGroup(fModel, mustDecodeConfig(t, config), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "跟随 = select, 手动-排除, 引用-排除, ") {
		t.Fatalf("line %q", line)
	}
	queries := policyQueries(t, line)
	if got := queries["/api/profiles/home/subscriptions/sub/proxies"]["exclude_regex"]; !reflect.DeepEqual(got, []string{source.GetString("exclude_regex")}) {
		t.Fatalf("subscription exclusion %v", got)
	}
	if got := queries["/api/profiles/home/aggregations/agg/provider"]["exclude_regex"]; !reflect.DeepEqual(got, []string{source.GetString("aggregation_exclude_regex")}) {
		t.Fatalf("aggregation exclusion %v", got)
	}
	for _, q := range queries {
		if q.Has("regex") {
			t.Fatalf("regex inherited: %v", q)
		}
	}
}

func TestSurgeLegacyMembersFilterBeforeAddingExplicitNodes(t *testing.T) {
	withFakeSubStore(t)
	config := mustJSON(t, `{"nodes": [{"name": "HK 01", "subscription_id": "sub"}, {"name": "HK 到期", "subscription_id": "sub"},
		{"name": "JP 01", "subscription_id": "sub"}]}`)
	group, err := convert.DecodeJ[model.ProxyGroup](mustJSON(t, `{"subscriptions": ["sub"], "regex": "HK", "exclude_regex": "到期", "proxies": ["HK 到期"]}`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := convert.ClientGroupNodes(group, mustDecodeConfig(t, config))
	if err != nil || !reflect.DeepEqual(got, []string{"HK 01", "HK 到期"}) {
		t.Fatalf("got %v %v", got, err)
	}
}

// remoteEntries is test_surge_loon_group_exclusion.py::_remote_entries.
func remoteEntries(t *testing.T, output string) map[string][2]any {
	out := map[string][2]any{}
	for _, line := range loonSection(output, "Remote Proxy") {
		name, settings, _ := strings.Cut(line, " = ")
		raw, _, _ := strings.Cut(settings, ",")
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		out[name] = [2]any{u.Path, u.Query()}
	}
	return out
}

// loonSection is test_loon_generation.py::_section.
func loonSection(config, name string) []string {
	lines := strings.Split(config, "\n")
	start := -1
	for i, line := range lines {
		if line == "["+name+"]" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil
	}
	var out []string
	for _, line := range lines[start:] {
		if strings.HasPrefix(line, "[") {
			break
		}
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func TestLoonSeparatesSubscriptionAndAggregationFilters(t *testing.T) {
	withFakeSubStore(t)
	config := exclusionConfig(t)
	original := config.DeepCopy()
	output := loonOf(t, config)
	want := map[string][2]any{
		"Feed": {"/api/profiles/home/subscriptions/sub/proxies",
			url.Values{"format": {"loon"}, "token": {"secret /&?"}, "exclude_regex": {"排除|倍率 [2-9]|&,+"}}},
		"Aggregate": {"/api/profiles/home/aggregations/agg/provider",
			url.Values{"format": {"loon"}, "token": {"secret /&?"}, "exclude_regex": {"维护|到期"}}},
	}
	if got := remoteEntries(t, output); !reflect.DeepEqual(got, want) {
		t.Fatalf("remote %v", got)
	}
	wantLines(t, loonSection(output, "Remote Filter"), []string{
		`混合-筛选 = NameRegex,Feed,FilterKey="(?i)HK|港"`,
		`混合-聚合筛选 = NameRegex,Aggregate,FilterKey="JP|日本"`,
	})
	if !containsLine(loonSection(output, "Proxy Group"), "混合 = select,手动-排除,引用-排除,混合-筛选,混合-聚合筛选") {
		t.Fatal(output)
	}
	if !convert.EqualJ(config, original) {
		t.Fatal("config mutated")
	}
}

func containsLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

func TestLoonRemoteReuseRespectsEachGroupExclusionAndFollowSource(t *testing.T) {
	withFakeSubStore(t)
	config := exclusionConfig(t)
	source := groupAt(config, 1)
	source.Set("regex", "")
	source.Set("aggregation_regex", "")
	config.Set("proxy_groups", append(config.GetList("proxy_groups"),
		convert.JObjKV("id", "plain", "name", "全部", "type", "select", "subscriptions", []any{"sub"}),
		convert.JObjKV("id", "different", "name", "另一个筛选", "type", "select", "subscriptions", []any{"sub"}, "exclude_regex", "另一种排除"),
		convert.JObjKV("id", "follow", "name", "跟随", "type", "select", "follow_group", "mixed", "exclude_regex", "wrong",
			"aggregation_exclude_regex", "wrong", "regex", "wrong", "aggregation_regex", "wrong")))
	output := loonOf(t, config)
	entries := remoteEntries(t, output)
	if len(entries) != 4 {
		t.Fatalf("entries %v", entries)
	}
	byExclusion := map[string]string{}
	for name, entry := range entries {
		if strings.Contains(entry[0].(string), "/subscriptions/") {
			byExclusion[entry[1].(url.Values).Get("exclude_regex")] = name
		}
	}
	if len(byExclusion) != 3 || byExclusion[""] == "" || byExclusion[source.GetString("exclude_regex")] == "" || byExclusion["另一种排除"] == "" {
		t.Fatalf("by exclusion %v", byExclusion)
	}
	groups := loonSection(output, "Proxy Group")
	if !containsLine(groups, "全部 = select,"+byExclusion[""]) || !containsLine(groups, "另一个筛选 = select,"+byExclusion["另一种排除"]) {
		t.Fatal(groups)
	}
	var mixed string
	for _, line := range groups {
		if strings.HasPrefix(line, "混合 = ") {
			mixed = strings.TrimPrefix(line, "混合 = ")
		}
	}
	if !containsLine(groups, "跟随 = "+mixed) || len(loonSection(output, "Remote Filter")) != 0 {
		t.Fatal(output)
	}
}

// --- test_loon_generation.py (pure-function cases; the route cases are C34's)

func TestUnsupportedTransportsAreSkipped(t *testing.T) {
	for _, raw := range []string{
		`{"name": "grpc", "type": "vless", "server": "a.test", "port": 443, "params": {"uuid": "u", "network": "grpc"}}`,
		`{"name": "t", "type": "tuic", "server": "a.test", "port": 443}`,
	} {
		node, err := convert.DecodeJ[model.Node](mustJSON(t, raw))
		if err != nil {
			t.Fatal(err)
		}
		if line, err := convert.LoonNode(node); err != nil || line != "" {
			t.Fatalf("%s: %q %v", raw, line, err)
		}
	}
}

func TestDefaultGeneralKeepsFakeIPRangeInTun(t *testing.T) {
	for _, line := range convert.LoonDefaultGeneral {
		if strings.HasPrefix(line, "bypass-tun") && strings.Contains(line, "198.18.0.0/15") {
			t.Fatal(line)
		}
	}
}

func TestLogicalRulesWithUnsupportedSubrulesAreCommented(t *testing.T) {
	config := mustJSON(t, `{"rule_configs": [
		{"itemType": "rule", "rule_type": "AND", "value": "((DOMAIN,a.test),(DST-PORT,443))", "policy": "DIRECT"},
		{"itemType": "rule", "rule_type": "OR", "value": "((PROCESS-NAME,curl),(DOMAIN,b.test))", "policy": "DIRECT"},
		{"itemType": "ruleset", "name": "Apple", "policy": "DIRECT",
		 "url": "https://raw.githubusercontent.com/blackmatrix7/ios_rule_script/master/rule/Clash/Apple/Apple.list"}]}`)
	lines, remote, err := convert.LoonRules(mustDecodeConfig(t, config), "")
	if err != nil {
		t.Fatal(err)
	}
	if lines[1] != "AND,((DOMAIN,a.test),(DEST-PORT,443)),DIRECT" || !strings.HasPrefix(lines[2], "# ") {
		t.Fatal(lines)
	}
	if !strings.HasPrefix(remote[1], "https://raw.githubusercontent.com/blackmatrix7/ios_rule_script/master/rule/Loon/Apple/Apple.list,") {
		t.Fatal(remote)
	}
}

func TestNodeFormatsFollowLoonDocs(t *testing.T) {
	line := func(nodeType, params string) string {
		node, err := convert.DecodeJ[model.Node](mustJSON(t, `{"name": "n", "type": "`+nodeType+`", "server": "a.test", "port": 443, "params": `+params+`}`))
		if err != nil {
			t.Fatal(err)
		}
		out, err := convert.LoonNode(node)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if got := line("vless", `{"uuid": "u", "tls": true, "flow": "xtls-rprx-vision", "servername": "s.test",
		"reality-opts": {"public-key": "pk", "short-id": "ab"}}`); got != `n = VLESS,a.test,443,"u",transport=tcp,flow=xtls-rprx-vision,over-tls=true,sni=s.test,`+
		`skip-cert-verify=false,public-key="pk",short-id=ab,udp=false` {
		t.Fatal(got)
	}
	if got := line("anytls", `{"password": "p", "sni": "s.test", "udp": true}`); got != `n = AnyTLS,a.test,443,"p",sni=s.test,skip-cert-verify=false,udp=true` {
		t.Fatal(got)
	}
	_, after, _ := strings.Cut(line("vmess", `{"uuid": "u", "network": "ws", "ws-opts": {"path": "/a,b"}}`), ",transport=ws,")
	if !strings.HasPrefix(after, `path="/a,b"`) {
		t.Fatal(after)
	}
	if got := line("hysteria2", `{"password": "p", "obfs": "salamander", "obfs-password": "o"}`); strings.Count(got, "salamander-password=o") != 1 {
		t.Fatal(got)
	}
}

func TestLoonRejectsUnknownPolicy(t *testing.T) {
	withFakeSubStore(t)
	config := mustJSON(t, `{"rule_configs": [{"id": "r", "itemType": "rule", "rule_type": "DOMAIN", "value": "a.test",
		"policy": "Missing", "enabled": true}]}`)
	_, err := convert.LoonConfig(context.Background(), mustDecodeConfig(t, config), "")
	var strategy *rules.StrategyReferenceError
	if !errors.As(err, &strategy) {
		t.Fatalf("err %v", err)
	}
}

// --- test_udp_fallback.py (Surge / Loon halves)

func udpRule(id, kind, value, policy string, extra ...any) *convert.JObj {
	r := convert.JObjKV("id", id, "itemType", "rule", "rule_type", kind, "value", value, "policy", policy, "enabled", true)
	for i := 0; i+1 < len(extra); i += 2 {
		r.Set(extra[i].(string), extra[i+1])
	}
	return r
}

func udpConfig(rulesList []any, clients ...any) *convert.JObj {
	c := convert.JObjKV("profile_id", "default", "system_config", convert.JObjKV("server_domain", "https://config.test"),
		"rule_configs", rulesList, "rule_library", []any{})
	for i := 0; i+1 < len(clients); i += 2 {
		c.Set(clients[i].(string), clients[i+1])
	}
	return c
}

var afterMatch = func() *convert.JObj {
	return udpRule("quic", "AND", "((NETWORK,udp),(DST-PORT,443/3478))", "REJECT")
}

func TestDisabledMatchDoesNotChooseTheFallback(t *testing.T) {
	withFakeSubStore(t)
	config := udpConfig([]any{udpRule("lan", "DOMAIN", "lan.test", "DIRECT"),
		udpRule("final", "MATCH", "", "DIRECT", "udp_fallback", "REJECT", "enabled", false)})
	if strings.Contains(surgeOf(t, config), "udp-policy-not-supported-behaviour") ||
		!strings.Contains(loonOf(t, config), "udp-fallback-mode = DIRECT") {
		t.Fatal("fallback chosen by a disabled MATCH")
	}
}

func TestSurgeKeepsFinalLastAndWritesTheNativeFallback(t *testing.T) {
	withFakeSubStore(t)
	output := surgeOf(t, udpConfig([]any{
		udpRule("udp", "NETWORK", "udp", "REJECT"),
		udpRule("ntp", "AND", "((NETWORK,udp),(DST-PORT,123))", "DIRECT"),
		udpRule("ports", "DST-PORT", "443/3478", "REJECT"),
		udpRule("final", "MATCH", "", "DIRECT", "udp_fallback", "REJECT"),
		afterMatch(),
	}))
	if !containsLine(udpSection(output, "General"), "udp-policy-not-supported-behaviour = REJECT") {
		t.Fatal(output)
	}
	wantLines(t, udpSection(output, "Rule"), []string{
		"PROTOCOL,UDP,REJECT",
		"AND,((PROTOCOL,UDP), (DEST-PORT,123)),DIRECT",
		"# Surge 不支持一条规则写多个端口，已忽略: DST-PORT,443/3478,REJECT",
		"FINAL,DIRECT",
		"# 仅 Mihomo 生效（位于 MATCH 之后，只匹配节点不支持 UDP 而跳过的连接），已忽略: AND,((NETWORK,udp),(DST-PORT,443/3478)),REJECT",
	})
}

func TestSurgeBaseConfigWinsOverTheOption(t *testing.T) {
	withFakeSubStore(t)
	config := udpConfig([]any{udpRule("final", "MATCH", "", "DIRECT", "udp_fallback", "REJECT")},
		"surge", convert.JObjKV("custom_config", "[General]\nudp-policy-not-supported-behaviour = DIRECT\n"))
	wantLines(t, udpSection(surgeOf(t, config), "General"), []string{"udp-policy-not-supported-behaviour = DIRECT"})
}

func TestLoonRulesAfterMatchNeverMoveBeforeFinal(t *testing.T) {
	withFakeSubStore(t)
	config := udpConfig([]any{
		udpRule("ntp", "AND", "((NETWORK,udp),(DST-PORT,123))", "DIRECT"),
		udpRule("final", "MATCH", "", "DIRECT", "udp_fallback", "REJECT"),
		udpRule("late", "DOMAIN", "late.test", "REJECT"),
		convert.JObjKV("id", "set", "itemType", "ruleset", "library_rule_id", "lib", "name", "Late", "url", "/rules/late",
			"policy", "REJECT", "enabled", true),
	})
	config.Set("rule_library", []any{convert.JObjKV("id", "lib", "name", "Late", "source_type", "content",
		"content", "DOMAIN,late.test", "behavior", "domain", "url", "/rules/late")})
	output := loonOf(t, config)
	if !containsLine(udpSection(output, "General"), "udp-fallback-mode = REJECT") || strings.Contains(output, "udp-fallback-mode = DIRECT") {
		t.Fatal(output)
	}
	ruleLines := udpSection(output, "Rule")
	if ruleLines[0] != "AND,((PROTOCOL,UDP),(DEST-PORT,123)),DIRECT" || ruleLines[len(ruleLines)-1] != "FINAL,DIRECT" ||
		!strings.HasPrefix(ruleLines[1], "# 仅 Mihomo 生效") || !strings.HasSuffix(ruleLines[1], "DOMAIN,late.test,REJECT") {
		t.Fatal(ruleLines)
	}
	wantLines(t, udpSection(output, "Remote Rule"), []string{
		"# 仅 Mihomo 生效（位于 MATCH 之后，只匹配节点不支持 UDP 而跳过的连接），已忽略: RULE-SET,Late,REJECT"})
}

func TestLoonCustomGeneralGetsTheOptionUnlessItSetsOne(t *testing.T) {
	withFakeSubStore(t)
	rulesList := func() []any {
		return []any{udpRule("final", "MATCH", "", "DIRECT", "udp_fallback", "REJECT"), udpRule("port", "DST-PORT", "443/3478", "REJECT")}
	}
	general := udpSection(loonOf(t, udpConfig(rulesList(), "loon", convert.JObjKV("custom_config", "[General]\nip-mode = ipv4-only"))), "General")
	wantLines(t, general, []string{"ip-mode = ipv4-only", "udp-fallback-mode = REJECT"})
	kept := udpConfig(rulesList(), "loon", convert.JObjKV("custom_config", "[General]\nudp-fallback-mode = DIRECT"))
	wantLines(t, udpSection(loonOf(t, kept), "General"), []string{"udp-fallback-mode = DIRECT"})
	if !strings.HasPrefix(udpSection(loonOf(t, kept), "Rule")[0], "# 仅 Mihomo 生效") {
		t.Fatal("rule after MATCH not commented first")
	}
}

func TestLoonPortListBeforeMatchIsIgnoredNotRewritten(t *testing.T) {
	withFakeSubStore(t)
	output := loonOf(t, udpConfig([]any{udpRule("port", "DST-PORT", "443/3478", "REJECT"), udpRule("final", "MATCH", "", "DIRECT")}))
	wantLines(t, udpSection(output, "Rule"), []string{"# Loon 不支持一条规则写多个端口，已忽略: DST-PORT,443/3478,REJECT", "FINAL,DIRECT"})
}

func TestNTPDirectGoesFirstInSurgeAndLoon(t *testing.T) {
	withFakeSubStore(t)
	config := udpConfig([]any{udpRule("lan", "DOMAIN", "lan.test", "DIRECT"), udpRule("final", "MATCH", "", "DIRECT", "ntp_direct", true)})
	if got := udpSection(surgeOf(t, config), "Rule")[0]; got != "AND,((PROTOCOL,UDP), (DEST-PORT,123)),DIRECT" {
		t.Fatal(got)
	}
	if got := udpSection(loonOf(t, config), "Rule")[0]; got != "AND,((PROTOCOL,UDP),(DEST-PORT,123)),DIRECT" {
		t.Fatal(got)
	}
	if list := config.GetList("rule_configs"); len(list) != 2 || list[0].(*convert.JObj).GetString("id") != "lan" {
		t.Fatal("rule_configs changed")
	}
}

func TestNTPDirectIsOffUnlessTheEffectiveMatchEnablesIt(t *testing.T) {
	withFakeSubStore(t)
	for _, match := range []*convert.JObj{udpRule("final", "MATCH", "", "DIRECT"),
		udpRule("final", "MATCH", "", "DIRECT", "ntp_direct", true, "enabled", false)} {
		config := udpConfig([]any{udpRule("lan", "DOMAIN", "lan.test", "DIRECT"), match})
		if strings.Contains(surgeOf(t, config)+loonOf(t, config), "DEST-PORT,123") {
			t.Fatal("NTP rule emitted")
		}
	}
}

// --- Go-side checks

// Sub-Store not wired: a URI node string fails closed instead of skipping
// the dialer check.
func TestSurgeLoonFailClosedWithoutSubStore(t *testing.T) {
	t.Cleanup(convert.OverrideMihomoEnvironmentForTest(t, convert.DefaultMihomoEnvironment()))
	config := mustJSON(t, `{"nodes": [{"id": "n", "name": "N", "proxy_string": "ss://YWVzOnB3@h.example.test:1#x"}]}`)
	for _, generate := range []func(context.Context, *model.Config, string) (string, error){
		convert.SurgeConfig, convert.LoonConfig} {
		_, err := generate(context.Background(), mustDecodeConfig(t, config.DeepCopy()), "")
		var hook *convert.HookMissingError
		if !errors.As(err, &hook) {
			t.Fatalf("err %v", err)
		}
	}
	// Without URI node strings Sub-Store is not needed.
	if _, err := convert.SurgeConfig(context.Background(), &model.Config{Profile: &model.Profile{}}, ""); err != nil {
		t.Fatal(err)
	}
}

func TestProxiesTextSkipsFailingNodes(t *testing.T) {
	proxies, err := decodeYAMLProxies([]any{
		convert.JObjKV("name", "A", "type", "ss", "server", "a.test", "port", convert.JInt(1), "cipher", "aes-128-gcm", "password", "p"),
		convert.JObjKV("name", "B", "type", "ss", "server", false, "port", convert.JInt(1)), // incompatible stored server type is skipped; numeric text uses the shared codec
		convert.JObjKV("name", "W", "type", "wireguard", "server", "w.test", "port", convert.JInt(1)),
	})
	if err != nil {
		t.Fatal(err)
	}
	text, err := convert.ClientProxyText(proxies, substore.ProxyNodes, "surge")
	if err != nil || text != "A = ss, a.test, 1, encrypt-method=aes-128-gcm, password=p" {
		t.Fatalf("surge %q %v", text, err)
	}
	text, err = convert.ClientProxyText(proxies, substore.ProxyNodes, "loon")
	if err != nil || text != `A = Shadowsocks,a.test,1,aes-128-gcm,"p",fast-open=false,udp=false` {
		t.Fatalf("loon %q %v", text, err)
	}
	dialerProxies, err := decodeYAMLProxies([]any{convert.JObjKV("name", "D", "dialer-proxy", "R")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = convert.ClientProxyText(dialerProxies, substore.ProxyNodes, "loon")
	var dialer *convert.DialerReferenceError
	if !errors.As(err, &dialer) || err.Error() != "Loon 订阅暂不支持 dialer-proxy，不能静默移除拨号配置" {
		t.Fatalf("err %v", err)
	}
}
