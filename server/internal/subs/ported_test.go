package subs

import (
	"fmt"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

// Ports of the pure-function tests of backend/test_*.py. The tests that
// render through generate_mihomo_config / HTTP routes belong to the converter
// (#6) and route packages (#15) and are listed in the hand-back.

func mustLoad(t *testing.T, text string) any {
	t.Helper()
	v, err := yamlx.SafeLoad(text)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func obj(t *testing.T, text string) *jsonx.Object {
	t.Helper()
	v, err := jsonx.ParseString(text)
	if err != nil {
		t.Fatal(err)
	}
	return v.(*jsonx.Object)
}

func js(t *testing.T, v any) string { return compact(t, v) }

// ---- test_subscription_regex_filter.py

func TestFilterKeepsMatchingNamesAndIgnoresBlankOrInvalidPattern(t *testing.T) {
	items := []any{obj(t, `{"name":"香港 01"}`), obj(t, `{"name":"日本 01"}`), obj(t, `{"name":"HK-02"}`)}
	names := func(l []any) []string {
		var out []string
		for _, i := range l {
			out = append(out, i.(*jsonx.Object).GetString("name"))
		}
		return out
	}
	got := names(FilterSubscriptionItems(obj(t, `{"regex_filter":"香港|HK"}`), items))
	if strings.Join(got, ",") != "香港 01,HK-02" {
		t.Fatal(got)
	}
	for _, sub := range []string{`{"regex_filter":"  "}`, `{}`, `{"regex_filter":"("}`} {
		if len(FilterSubscriptionItems(obj(t, sub), items)) != len(items) {
			t.Fatalf("%s must not filter", sub)
		}
	}
}

func TestValidateRegexFilter(t *testing.T) {
	for _, v := range []any{nil, "", "^(?!.*剩余).*$"} {
		if msg := ValidateRegexFilter(v); msg != "" {
			t.Fatalf("%v: %s", v, msg)
		}
	}
	if ValidateRegexFilter("(") == "" || ValidateRegexFilter(jsonx.Int(123)) == "" {
		t.Fatal("invalid values must be reported")
	}
	// The detail after the Chinese prefix is the regex engine's error text.
	if got := ValidateRegexFilter("("); !strings.HasPrefix(got, "节点筛选正则无效: ") {
		t.Fatal(got)
	}
}

// ---- test_subscription_hosts.py (extraction and scoped merge)

const airportHosts = `mixed-port: 7890
proxies:
  - {name: HK 01, type: socks5, server: jp-node.example.net, port: 1080}
  - name: JP 01
    type: socks5
    server: HK-NODE.example.net.
    port: 1080
    # an indented key named hosts must not be read as the top-level section
    hosts: ignored.example
  - {name: SG 01, type: socks5, server: 203.0.113.9, port: 1080}
hosts:
  jp-node.example.net: jp-entry.example.org
  hk-node.example.net: [198.51.100.12, '2001:db8::12']
  google.com: 203.0.113.66
proxy-groups:
  - {name: PROXY, type: select, proxies: [HK 01, JP 01, SG 01]}
`

func TestExtractReadsOnlyTheTopLevelSectionAndNormalizes(t *testing.T) {
	record := ExtractSubscriptionHosts(airportHosts)
	want := `{"entries":{"jp-node.example.net":"jp-entry.example.org","hk-node.example.net":["198.51.100.12","2001:db8::12"],"google.com":["203.0.113.66"]},"total":3,"skipped_count":0,"skipped":[]}`
	if js(t, record) != want {
		t.Fatal(js(t, record))
	}
}

func hostsDoc(key string, value any) string {
	v, _ := jsonx.Compact(value)
	return fmt.Sprintf("proxies: []\nhosts: {%q: %s, ok.test: 198.51.100.7}\n", key, v)
}

func TestExtractDropsUnsafeOrMalformedEntriesWithAReason(t *testing.T) {
	many := make([]any, 9)
	for i := range many {
		many[i] = fmt.Sprintf("198.51.100.%d", i)
	}
	cases := []struct {
		key    string
		value  any
		reason string
	}{
		{"*.example.net", "198.51.100.1", "wildcard"},
		{"+.example.net", "198.51.100.1", "wildcard"},
		{".example.net", "198.51.100.1", "wildcard"},
		{"node.test", "lan", "invalid_value"},
		{"node.test", "198.51.100.1,198.51.100.2", "invalid_value"},
		{"node.test", []any{}, "invalid_value"},
		{"node.test", []any{"198.51.100.1", "other.test"}, "invalid_value"},
		{"node.test", obj(t, `{"a":1}`), "invalid_value"},
		{"node.test", "node.test", "invalid_value"},
		{"node.test", "0.0.0.0", "invalid_value"},
		{"node.test", "224.0.0.1", "invalid_value"},
		{"node.test", many, "too_many_addresses"},
		{"198.51.100.1", "node.test", "invalid_domain"},
		{"localhost", "127.0.0.1", "invalid_domain"},
		{"bad_label-.test..", "198.51.100.1", "invalid_domain"},
	}
	for _, c := range cases {
		record := ExtractSubscriptionHosts(hostsDoc(c.key, c.value))
		if js(t, record.Value("entries")) != `{"ok.test":["198.51.100.7"]}` {
			t.Fatalf("%s: %s", c.key, js(t, record))
		}
		want := fmt.Sprintf(`[{"domain":%q,"reason":%q}]`, strings.ToLower(c.key), c.reason)
		if js(t, record.Value("skipped")) != want || js(t, record.Value("total")) != "2" || js(t, record.Value("skipped_count")) != "1" {
			t.Fatalf("%s: %s", c.key, js(t, record))
		}
	}
}

func TestExtractBoundsEntriesAndStoredSamples(t *testing.T) {
	var b strings.Builder
	b.WriteString("proxies: []\nhosts:\n")
	for i := 0; i < MaxEntries+30; i++ {
		fmt.Fprintf(&b, "  n%d.test: 198.51.100.1\n", i)
	}
	record := ExtractSubscriptionHosts(b.String())
	if record.GetObject("entries").Len() != MaxEntries || record.GetInt("skipped_count") != 30 || len(record.GetList("skipped")) != 20 {
		t.Fatal(js(t, record.Value("skipped_count")))
	}
	for _, s := range record.GetList("skipped") {
		if s.(*jsonx.Object).GetString("reason") != "over_limit" {
			t.Fatal(js(t, s))
		}
	}
}

func TestExtractHandlesBodiesWithoutOrWithFlowHosts(t *testing.T) {
	cases := []struct{ text, want string }{
		{"c3M6Ly9ZV1Z6TFRJMU5pMW5ZMjA2Y0dGemMzZHZjbVE9QDE5OC41MS4xMDAuMTo4Mzg4I2hvc3Rz", "null"},
		{"proxies: []\n", "null"},
		{"proxies:\n  - {name: a, type: http, server: a.test, port: 80}\n    # hosts: here\n", "null"},
		{"proxies: []\nhosts: {node.test: 198.51.100.4}\n", `{"node.test":["198.51.100.4"]}`},
		{"hosts:\n  node.test:\n  - 198.51.100.4\nproxies: []\n", `{"node.test":["198.51.100.4"]}`},
	}
	for _, c := range cases {
		record := ExtractSubscriptionHosts(c.text)
		var got any
		if record != nil {
			got = record.Value("entries")
		}
		if js(t, got) != c.want {
			t.Fatalf("%q: %s", c.text, js(t, got))
		}
	}
}

func TestExtractReportsASectionItCannotUse(t *testing.T) {
	for text, reason := range map[string]string{
		"proxies: []\nhosts: [198.51.100.1]\n":       "not_mapping",
		"proxies: []\nhosts:\n  a.test: [unclosed\n": "parse_error",
	} {
		want := fmt.Sprintf(`{"entries":{},"total":0,"skipped_count":1,"skipped":[{"domain":"","reason":%q}]}`, reason)
		if got := js(t, ExtractSubscriptionHosts(text)); got != want {
			t.Fatalf("%q: %s", text, got)
		}
	}
}

func hostsIndexFixture(t *testing.T, feeds map[string]string) (HostsIndexOf, *jsonx.Object) {
	caches := jsonx.NewObject()
	var subs []any
	for id, body := range feeds {
		doc := mustLoad(t, body).(*jsonx.Object)
		var nodes []any
		for _, p := range doc.GetList("proxies") {
			po := p.(*jsonx.Object)
			nodes = append(nodes, jsonx.Obj("name", po.Value("name"), "server", po.Value("server")))
		}
		url := "https://sub.test/" + id
		caches.Set(id, jsonx.Obj("nodes", nodes, "metadata", jsonx.Obj("url", url, "hosts", ExtractSubscriptionHosts(body))))
		subs = append(subs, jsonx.Obj("id", id, "name", id, "url", url, "enabled", true))
	}
	index := func(sub *jsonx.Object) *HostsIndex {
		c := asObj(caches.Value(sub.GetString("id")))
		if c == nil {
			return nil
		}
		return NewHostsIndex(c).ForURL(sub)
	}
	return index, jsonx.Obj("subscriptions", subs, "subscription_aggregations", []any{}, "nodes", []any{})
}

func providerFor(id string) *jsonx.Object {
	return jsonx.Obj("proxy-providers", jsonx.Obj(id, jsonx.Obj("type", "http", "url", "https://panel.test/api/subscriptions/"+id+"/proxies")))
}

func TestMergesOnlyDomainsOfNodesTheConfigurationDelivers(t *testing.T) {
	index, config := hostsIndexFixture(t, map[string]string{"air": airportHosts})
	main := providerFor("air")
	report := MergeSubscriptionHosts(main, config, nil, nil, nil, nil, index)
	if js(t, main.Value("hosts")) != `{"jp-node.example.net":"jp-entry.example.org","hk-node.example.net":["198.51.100.12","2001:db8::12"]}` {
		t.Fatal(js(t, main.Value("hosts")))
	}
	if report.GetInt("merged") != 2 || report.GetInt("ignored") != 1 || report.GetInt("domain_mapped") != 1 {
		t.Fatal(js(t, report))
	}
	if js(t, report.Value("subscriptions")) != `[{"id":"air","name":"air","total":3,"merged":2,"ignored":1}]` {
		t.Fatal(js(t, report.Value("subscriptions")))
	}
}

func TestHandWrittenHostsWinThenMosdnsThenSubscriptions(t *testing.T) {
	body := strings.Replace(airportHosts, "  google.com: 203.0.113.66\n", "  wild.example.net: 198.51.100.40\n", 1)
	body = strings.Replace(body, "  - {name: SG 01,", "  - {name: W, type: socks5, server: wild.example.net, port: 1}\n  - {name: SG 01,", 1)
	index, config := hostsIndexFixture(t, map[string]string{"air": body})
	main := providerFor("air")
	main.Set("hosts", obj(t, `{"jp-node.example.net":"192.0.2.10","+.wild.example.net":"192.0.2.11"}`))
	report := MergeSubscriptionHosts(main, config, []string{"jp-node.example.net", "+.wild.example.net"}, []string{"hk-node.example.net"}, nil, nil, index)
	if js(t, main.Value("hosts")) != `{"jp-node.example.net":"192.0.2.10","+.wild.example.net":"192.0.2.11"}` {
		t.Fatal(js(t, main.Value("hosts")))
	}
	reasons := map[string]string{}
	for _, it := range report.GetList("items") {
		o := it.(*jsonx.Object)
		if o.GetString("status") == "ignored" {
			reasons[o.GetString("domain")] = o.GetString("reason")
		}
	}
	want := map[string]string{"jp-node.example.net": "custom_hosts", "wild.example.net": "custom_hosts", "hk-node.example.net": "mosdns_hosts"}
	if fmt.Sprint(reasons) != fmt.Sprint(want) {
		t.Fatal(reasons)
	}
}

func TestAMappingLoopIsNeverEmitted(t *testing.T) {
	index, config := hostsIndexFixture(t, map[string]string{"air": airportHosts})
	main := providerFor("air")
	main.Set("hosts", obj(t, `{"+.example.org":"jp-node.example.net"}`))
	report := MergeSubscriptionHosts(main, config, nil, nil, nil, nil, index)
	hosts := main.GetObject("hosts")
	if hosts.GetString("+.example.org") != "jp-node.example.net" || hosts.Has("jp-node.example.net") {
		t.Fatal(js(t, hosts))
	}
	found := false
	for _, it := range report.GetList("items") {
		o := it.(*jsonx.Object)
		if o.GetString("domain") == "jp-node.example.net" && o.GetString("reason") == "cycle" {
			found = true
		}
	}
	if !found {
		t.Fatal(js(t, report))
	}
}

func TestHostsDigestAndDescribeChange(t *testing.T) {
	a := ExtractSubscriptionHosts(airportHosts)
	if HostsDigest(a) != HostsDigest(ExtractSubscriptionHosts(airportHosts)) || HostsDigest(a) == HostsDigest(nil) {
		t.Fatal("digest")
	}
	b := ExtractSubscriptionHosts(strings.Replace(airportHosts, "203.0.113.66", "203.0.113.67", 1))
	if js(t, DescribeChange(a, b)) != `{"added":0,"removed":0,"changed":1}` {
		t.Fatal(js(t, DescribeChange(a, b)))
	}
}

// ---- test_proxy_server_dns.py (helpers)

func TestListenerAddressIsDerivedFromDnsListen(t *testing.T) {
	cases := []struct {
		listen any
		addr   string
		reason string
	}{
		{"0.0.0.0:1053", "udp://127.0.0.1:1053", ""}, {"[::]:53", "udp://127.0.0.1:53", ""}, {":7874", "udp://127.0.0.1:7874", ""},
		{"127.0.0.1:7874", "udp://127.0.0.1:7874", ""}, {"192.168.1.2:53", "udp://192.168.1.2:53", ""},
		{"[::1]:1053", "udp://[::1]:1053", ""}, {"localhost:53", "", "listen_unparsed"}, {"0.0.0.0", "", "listen_unparsed"},
		{"0.0.0.0:0", "", "listen_unparsed"}, {"", "", "no_listen"}, {nil, "", "no_listen"},
	}
	for _, c := range cases {
		a, r := ListenerAddress(c.listen)
		if a != c.addr || r != c.reason {
			t.Fatalf("%v: %q %q", c.listen, a, r)
		}
	}
}

func TestCompressMergesOnlyWithinOneRegistrableDomain(t *testing.T) {
	cases := []struct {
		domains []string
		want    string
	}{
		{[]string{"a.example.org", "b.example.org"}, "+.example.org"},
		{[]string{"hk1.node.example.org", "jp2.node.example.org"}, "+.node.example.org"},
		{[]string{"example.org", "x.example.org"}, "+.example.org"},
		{[]string{"only.example.org"}, "only.example.org"},
		{[]string{"a.github.io", "b.github.io"}, "a.github.io,b.github.io"},
		{[]string{"example.co.uk", "other.co.uk"}, "example.co.uk,other.co.uk"},
		{[]string{"a.example.co.uk", "b.example.co.uk"}, "+.example.co.uk"},
		{[]string{"edge-a.shared.lab.invalid", "edge-b.shared.lab.invalid"}, "edge-a.shared.lab.invalid,edge-b.shared.lab.invalid"},
	}
	for _, c := range cases {
		if got := strings.Join(Compress(c.domains), ","); got != c.want {
			t.Fatalf("%v: %s", c.domains, got)
		}
	}
}

func TestFinalTargetFollowsHostsLikeMihomo(t *testing.T) {
	hosts := obj(t, `{"a.test":"b.test","b.test":"c.test","x.test":"y.test","y.test":"198.51.100.1","+.wild.test":["198.51.100.2"],"w.test":"n.wild.test","ip.test":["198.51.100.3"]}`)
	for domain, want := range map[string]string{"a.test": "c.test", "x.test": "", "w.test": "", "ip.test": "", "missing.test": ""} {
		if got := FinalTarget(hosts, domain); got != want {
			t.Fatalf("%s: %q", domain, got)
		}
	}
}

func TestCoreVersions(t *testing.T) {
	cases := []struct {
		text   any
		parsed string
		old    bool
	}{
		{"v1.19.33", "1.19.33", false}, {"v1.19.20", "1.19.20", false}, {"v1.19.19", "1.19.19", true},
		{"Mihomo Meta v1.18.10 linux amd64", "1.18.10", true}, {"1.20.0", "1.20.0", false},
		{"alpha-1a2b3c4", "", false}, {"", "", false}, {nil, "", false},
	}
	for _, c := range cases {
		v, ok := ParseCoreVersion(c.text)
		got := ""
		if ok {
			got = fmt.Sprintf("%s.%s.%s", v[0], v[1], v[2])
		}
		if got != c.parsed || CoreTooOld(c.text) != c.old {
			t.Fatalf("%v: %q %v", c.text, got, CoreTooOld(c.text))
		}
	}
}

// ---- test_subscription_dialers.py (extraction and adapting)

const airportDialers = `proxies:
  - {name: Entry, type: ss, server: example.com, port: 1}
  - {name: HK, type: ss, server: example.com, port: 1, dialer-proxy: Relay}
  - {name: JP, type: ss, server: example.com, port: 1, dialer-proxy: Hidden}
  - {name: Plain, type: ss, server: example.com, port: 1, dialer-proxy: Entry}
proxy-groups:
  - {name: Relay, type: select, proxies: [Entry, Hidden, DIRECT], url: "http://x/204", interval: 300, lazy: true}
  - {name: Hidden, type: url-test, proxies: [Entry, HK]}
  - {name: Dyn, type: select, use: [p]}
`

func TestExtractRecordsOnlyWhatChainedNodesNeed(t *testing.T) {
	record, err := ExtractSubscriptionDialers(airportDialers)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"targets":{"Hidden":{"type":"url-test","members":["Entry","HK"]},"Relay":{"type":"select","members":["Entry","HK"],"url":"http://x/204","interval":300,"lazy":true}},"unresolved":[]}`
	if js(t, record) != want {
		t.Fatal(js(t, record))
	}
}

func TestExtractReportsDynamicGroupsAsUnresolved(t *testing.T) {
	text := "proxies:\n  - {name: A, type: ss, dialer-proxy: Dyn}\nproxy-groups:\n  - {name: Dyn, type: select, use: [p]}\n"
	record, _ := ExtractSubscriptionDialers(text)
	if js(t, record) != `{"targets":{},"unresolved":["Dyn"]}` {
		t.Fatal(js(t, record))
	}
	if r, _ := ExtractSubscriptionDialers("proxies: []\n"); r != nil {
		t.Fatal("no dialer-proxy means no record")
	}
}

func TestAdaptingTouchesOnlyAirportGroupsAndIsStable(t *testing.T) {
	record := obj(t, `{"targets":{"Relay":{"type":"select","members":["X"]}},"unresolved":["Hidden"]}`)
	proxies := []any{obj(t, `{"name":"a","dialer-proxy":"Relay"}`), obj(t, `{"name":"b","dialer-proxy":"Hidden"}`), obj(t, `{"name":"c","dialer-proxy":"Other"}`), obj(t, `{"name":"d"}`)}
	main := obj(t, `{"proxy-groups":[{"name":"Air/Relay"}],"proxies":[{"name":"Other"}]}`)
	got := AdaptSubscriptionProxies("Air", proxies, main, record, true)
	want := `[{"name":"a","dialer-proxy":"Air/Relay"},{"name":"c","dialer-proxy":"Other"},{"name":"d"}]`
	if js(t, got) != want {
		t.Fatal(js(t, got))
	}
	if again := AdaptSubscriptionProxies("Air", got, main, record, true); js(t, again) != want {
		t.Fatal("adapting must be idempotent")
	}
	if none := AdaptSubscriptionProxies("Air", proxies, main, record, false); js(t, none) != `[{"name":"c","dialer-proxy":"Other"},{"name":"d"}]` {
		t.Fatal(js(t, none))
	}
}

// ---- test_proxy_chains.py (validation and lowering contracts)

func chainNode(id string) string {
	return fmt.Sprintf(`{"id":%q,"name":%q,"type":"http","server":"%s.test","port":80}`, id, strings.ToUpper(id), id)
}

func chainProfile(t *testing.T, groups ...string) *jsonx.Object {
	chain := `{"id":"chain","name":"Chain","type":"chain","chain":{"entry":{"type":"node","id":"a"},"exit":{"type":"node","id":"b"}}}`
	all := append([]string{chain}, groups...)
	return obj(t, fmt.Sprintf(`{"profile_id":"default","nodes":[%s,%s],"proxy_groups":[%s],"rule_configs":[]}`,
		chainNode("a"), chainNode("b"), strings.Join(all, ",")))
}

func TestInvalidChainMetadataIsRejectedBeforeConversion(t *testing.T) {
	mutations := map[string]func(p *jsonx.Object){
		"missing_exit": func(p *jsonx.Object) {
			p.GetList("proxy_groups")[0].(*jsonx.Object).GetObject("chain").GetObject("exit").Set("id", "missing")
		},
		"missing_entry": func(p *jsonx.Object) {
			p.GetList("proxy_groups")[0].(*jsonx.Object).GetObject("chain").GetObject("entry").Set("id", "missing")
		},
		"disabled_exit":  func(p *jsonx.Object) { p.GetList("nodes")[1].(*jsonx.Object).Set("enabled", false) },
		"disabled_entry": func(p *jsonx.Object) { p.GetList("nodes")[0].(*jsonx.Object).Set("enabled", false) },
		"duplicate_name": func(p *jsonx.Object) { p.GetList("proxy_groups")[0].(*jsonx.Object).Set("name", "B") },
		"reserved_name":  func(p *jsonx.Object) { p.GetList("proxy_groups")[0].(*jsonx.Object).Set("name", "DIRECT") },
		"self_entry": func(p *jsonx.Object) {
			p.GetList("proxy_groups")[0].(*jsonx.Object).GetObject("chain").GetObject("entry").Set("id", "b")
		},
		"subscription_exit":  func(p *jsonx.Object) { p.GetList("nodes")[1].(*jsonx.Object).Set("subscription_id", "feed") },
		"subscription_entry": func(p *jsonx.Object) { p.GetList("nodes")[0].(*jsonx.Object).Set("subscription_id", "feed") },
		"malformed_entry": func(p *jsonx.Object) {
			p.GetList("proxy_groups")[0].(*jsonx.Object).GetObject("chain").Set("entry", nil)
		},
		"malformed_chain": func(p *jsonx.Object) { p.GetList("proxy_groups")[0].(*jsonx.Object).Set("chain", []any{}) },
		"duplicate_id": func(p *jsonx.Object) {
			g := p.GetList("proxy_groups")
			dup := g[0].(*jsonx.Object).DeepCopy()
			dup.Set("name", "Other")
			p.Set("proxy_groups", append(g, dup))
		},
	}
	for name, mutate := range mutations {
		p := chainProfile(t)
		mutate(p)
		if _, err := LowerProxyChains(p); err == nil {
			t.Fatalf("%s: lowering must be rejected", name)
		} else if _, ok := err.(*DialerReferenceError); !ok {
			t.Fatalf("%s: %T", name, err)
		}
	}
}

func TestLoweringIDsAreUniqueAndNestedObjectsAreIndependent(t *testing.T) {
	p := chainProfile(t, `{"id":"chooser","name":"Choose","type":"select","proxies_order":[{"type":"strategy","id":"chain"}]}`)
	p.Set("nodes", append(p.GetList("nodes"), obj(t, chainNode("__chain__chain"))))
	p.GetList("nodes")[1].(*jsonx.Object).Set("params", obj(t, `{"headers":{"User-Agent":"shared"}}`))
	before := p.DeepCopy()
	runtime, err := LowerProxyChains(p)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var derived *jsonx.Object
	for _, n := range runtime.GetList("nodes") {
		o := n.(*jsonx.Object)
		if seen[o.GetString("id")] {
			t.Fatal("duplicate id")
		}
		seen[o.GetString("id")] = true
		if o.GetString("name") == "Chain" {
			derived = o
		}
	}
	for _, g := range runtime.GetList("proxy_groups") {
		o := g.(*jsonx.Object)
		if seen[o.GetString("id")] || o.GetString("type") == "chain" {
			t.Fatal("group id clash or chain left")
		}
		seen[o.GetString("id")] = true
	}
	order := runtime.GetList("proxy_groups")[0].(*jsonx.Object).GetList("proxies_order")
	if js(t, order) != fmt.Sprintf(`[{"type":"node","id":%q}]`, derived.GetString("id")) {
		t.Fatal(js(t, order))
	}
	derived.GetObject("params").GetObject("headers").Set("User-Agent", "runtime")
	if !jsonx.Equal(p, before) {
		t.Fatal("the profile must not change")
	}
	if runtime.GetList("nodes")[1].(*jsonx.Object).GetObject("params").GetObject("headers").GetString("User-Agent") != "shared" {
		t.Fatal("the original node must stay untouched")
	}
}

func TestIndirectGroupCycleChecksEveryBranch(t *testing.T) {
	p := chainProfile(t, `{"id":"entry","name":"Entry","type":"select","manual_nodes":["a"],"include_groups":["nested"]}`,
		`{"id":"nested","name":"Nested","type":"select","include_groups":["chain"]}`)
	p.GetList("proxy_groups")[0].(*jsonx.Object).GetObject("chain").Set("entry", obj(t, `{"type":"group","id":"entry"}`))
	if err := ValidateDialers(p); err == nil {
		t.Fatal("cycle must be rejected")
	}
}

func TestFollowChainIsRejectedEvenWhenFollowerIsDisabled(t *testing.T) {
	p := chainProfile(t, `{"id":"follower","name":"Follower","type":"select","follow_group":"chain","enabled":false}`)
	if err := ValidateDialers(p); err == nil {
		t.Fatal("must be rejected")
	}
}

func TestManualOnlyConstraintIncludesNestedStaticGroupMembers(t *testing.T) {
	p := chainProfile(t, `{"id":"entry","name":"Entry","type":"select","manual_nodes":["a"]}`)
	p.GetList("proxy_groups")[0].(*jsonx.Object).GetObject("chain").Set("entry", obj(t, `{"type":"group","id":"entry"}`))
	p.GetList("nodes")[0].(*jsonx.Object).Set("subscription_id", "feed")
	if err := ValidateDialers(p); err == nil {
		t.Fatal("must be rejected")
	}
}

func TestDisabledChainCannotBeUsedByEnabledConsumer(t *testing.T) {
	for _, consumer := range []string{
		`{"id":"g","name":"Group","type":"select","include_groups":["chain"]}`,
		`{"id":"g","name":"Group","type":"select","proxies_order":[{"type":"strategy","id":"chain"}]}`,
		`{"id":"outer","name":"Outer","type":"chain","chain":{"entry":{"type":"group","id":"chain"},"exit":{"type":"node","id":"b"}}}`,
	} {
		p := chainProfile(t, consumer)
		p.GetList("proxy_groups")[0].(*jsonx.Object).Set("enabled", false)
		if _, err := LowerProxyChains(p); err == nil {
			t.Fatalf("%s must be rejected", consumer)
		}
	}
}

func TestDisabledChainPreservesDisabledExitMetadataWithoutEmitting(t *testing.T) {
	p := chainProfile(t)
	p.GetList("proxy_groups")[0].(*jsonx.Object).Set("enabled", false)
	p.GetList("nodes")[1].(*jsonx.Object).Set("enabled", false)
	if err := ValidateDialers(p); err != nil {
		t.Fatal(err)
	}
}

func TestRawDialerCycleThroughNamedChainIsRejected(t *testing.T) {
	p := chainProfile(t)
	p.GetList("nodes")[0].(*jsonx.Object).Set("params", obj(t, `{"dialer-proxy":"Chain"}`))
	if err := ValidateDialers(p); err == nil {
		t.Fatal("must be rejected")
	}
}

func TestChainAsExitComposesAllHops(t *testing.T) {
	p := obj(t, fmt.Sprintf(`{"nodes":[%s,%s,%s],"proxy_groups":[
		{"id":"inner","name":"Inner","type":"chain","chain":{"entry":{"type":"node","id":"a"},"exit":{"type":"node","id":"b"}}},
		{"id":"outer","name":"Outer","type":"chain","chain":{"entry":{"type":"node","id":"c"},"exit":{"type":"group","id":"inner"}}}]}`,
		chainNode("a"), chainNode("b"), chainNode("c")))
	runtime, err := LowerProxyChains(p)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*jsonx.Object{}
	byID := map[string]*jsonx.Object{}
	for _, n := range runtime.GetList("nodes") {
		o := n.(*jsonx.Object)
		byName[o.GetString("name")] = o
		byID[o.GetString("id")] = o
	}
	// A -> (B -> C) is (A -> B) -> C: Outer dials through the cloned inner entry.
	outer := byName["Outer"]
	hop := byID[outer.GetObject("dialer_ref").GetString("id")]
	if hop == nil || hop.GetObject("dialer_ref") == nil {
		t.Fatalf("outer %s", js(t, outer))
	}
}

// test_reported_core_version_comes_from_domain_discovery_only, with the
// traffic store replaced by a map.
func TestReportedCoreVersionComesFromDomainDiscoveryOnly(t *testing.T) {
	statuses := map[string]*jsonx.Object{}
	calls := 0
	load := func(id string) *jsonx.Object { calls++; return statuses[id] }
	agent := jsonx.Obj("id", "a1", "service_type", "mihomo", "domain_discovery_enabled", true)
	if v := ReportedCoreVersion(agent, load); v != "" {
		t.Fatalf("no report: %q", v)
	}
	statuses["a1"] = jsonx.Obj("status", "running", "mihomo_version", "v1.19.10", "items", []any{})
	if v := ReportedCoreVersion(agent, load); v != "v1.19.10" {
		t.Fatalf("got %q", v)
	}
	calls = 0
	off := agent.Clone()
	off.Set("domain_discovery_enabled", false)
	mosdns := agent.Clone()
	mosdns.Set("service_type", "mosdns")
	if ReportedCoreVersion(off, load) != "" || ReportedCoreVersion(mosdns, load) != "" || calls != 0 {
		t.Fatalf("stale or non-Mihomo report used (%d loads)", calls)
	}
	old := jsonx.Obj("id", "old", "name", "旧内核", "domain_discovery_enabled", true)
	statuses["old"] = jsonx.Obj("mihomo_version", "v1.19.0")
	other := jsonx.Obj("id", "other", "profile_id", "p2", "domain_discovery_enabled", true)
	statuses["other"] = jsonx.Obj("mihomo_version", "v1.19.0")
	got := OutdatedAgents([]*jsonx.Object{agent, old, other}, "default", "default", load)
	if js(t, got) != `[{"name":"a1","version":"v1.19.10"},{"name":"旧内核","version":"v1.19.0"}]` {
		t.Fatalf("outdated %s", js(t, got))
	}
	if js(t, OutdatedAgents(nil, "default", "default", load)) != "[]" {
		t.Fatal("empty result must be a list")
	}
}
