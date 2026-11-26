package subs

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

// Ports of the pure-function tests of backend/test_*.py. The tests that
// render through generate_mihomo_config / HTTP routes belong to the converter
// (#6) and route packages (#15) and are listed in the hand-back.

func mustLoadNode(t *testing.T, text string) *yamlx.Node {
	t.Helper()
	n, err := readSubscriptionYAMLNode(text, false)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func js(t *testing.T, v any) string { return compact(t, v) }

// ---- test_subscription_regex_filter.py

func TestFilterKeepsMatchingNamesAndIgnoresBlankOrInvalidPattern(t *testing.T) {
	items := []*yamlx.Node{
		yamlx.Map(yamlx.Str("name"), yamlx.Str("香港 01")),
		yamlx.Map(yamlx.Str("name"), yamlx.Str("日本 01")),
		yamlx.Map(yamlx.Str("name"), yamlx.Str("HK-02")),
	}
	names := func(l []*yamlx.Node) []string {
		var out []string
		for _, i := range l {
			out = append(out, yamlx.Get(i, "name").Value)
		}
		return out
	}
	got := names(FilterProxies(model.Subscription{RegexFilter: strp("香港|HK")}, items))
	if strings.Join(got, ",") != "香港 01,HK-02" {
		t.Fatal(got)
	}
	for _, sub := range []model.Subscription{{RegexFilter: strp("  ")}, {}, {RegexFilter: strp("(")}} {
		if len(FilterProxies(sub, items)) != len(items) {
			t.Fatalf("%+v must not filter", sub)
		}
	}
}

func TestValidateRegexFilter(t *testing.T) {
	for _, v := range []string{"", "^(?!.*剩余).*$"} {
		if msg := ValidateFilter(v); msg != "" {
			t.Fatalf("%q: %s", v, msg)
		}
	}
	// The detail after the Chinese prefix is the regex engine's error text.
	if got := ValidateFilter("("); !strings.HasPrefix(got, "节点筛选正则无效: ") {
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
	record := ExtractHosts(airportHosts)
	want := json.RawMessage(`{"entries":{"jp-node.example.net":"jp-entry.example.org","hk-node.example.net":["198.51.100.12","2001:db8::12"],"google.com":["203.0.113.66"]},"total":3,"skipped_count":0,"skipped":[]}`)
	if !testjson.Equal(want, record) {
		t.Fatal(js(t, record))
	}
}

func hostsDoc(key string, value any) string {
	v, _ := json.Marshal(value)
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
		{"node.test", map[string]any{"a": 1}, "invalid_value"},
		{"node.test", "node.test", "invalid_value"},
		{"node.test", "0.0.0.0", "invalid_value"},
		{"node.test", "224.0.0.1", "invalid_value"},
		{"node.test", many, "too_many_addresses"},
		{"198.51.100.1", "node.test", "invalid_domain"},
		{"localhost", "127.0.0.1", "invalid_domain"},
		{"bad_label-.test..", "198.51.100.1", "invalid_domain"},
	}
	for _, c := range cases {
		record := ExtractHosts(hostsDoc(c.key, c.value))
		if !testjson.Equal(json.RawMessage(`{"ok.test":["198.51.100.7"]}`), record.Entries) {
			t.Fatalf("%s: %s", c.key, js(t, record))
		}
		want := json.RawMessage(fmt.Sprintf(`[{"domain":%q,"reason":%q}]`, strings.ToLower(c.key), c.reason))
		if !testjson.Equal(want, record.Skipped) || record.Total != 2 || record.SkippedCount != 1 {
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
	record := ExtractHosts(b.String())
	if record.Entries.Len() != MaxEntries || record.SkippedCount != 30 || len(record.Skipped) != 20 {
		t.Fatal(record.SkippedCount)
	}
	for _, s := range record.Skipped {
		if s.Reason != "over_limit" {
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
		record := ExtractHosts(c.text)
		var got any
		if record != nil {
			got = record.Entries
		}
		if !testjson.Equal(json.RawMessage(c.want), got) {
			t.Fatalf("%q: %s", c.text, js(t, got))
		}
	}
}

func TestExtractReportsASectionItCannotUse(t *testing.T) {
	for text, reason := range map[string]string{
		"proxies: []\nhosts: [198.51.100.1]\n":       "not_mapping",
		"proxies: []\nhosts:\n  a.test: [unclosed\n": "parse_error",
	} {
		want := json.RawMessage(fmt.Sprintf(`{"entries":{},"total":0,"skipped_count":1,"skipped":[{"domain":"","reason":%q}]}`, reason))
		if got := ExtractHosts(text); !testjson.Equal(want, got) {
			t.Fatalf("%q: %s", text, js(t, got))
		}
	}
}

func hostsIndexFixture(t *testing.T, feeds map[string]string) (HostIndexLoader, *model.Config) {
	caches := map[string]*SubscriptionCache{}
	var subsList []model.Subscription
	for id, body := range feeds {
		doc := mustLoadNode(t, body)
		var nodes []json.RawMessage
		if proxies := yamlx.Get(doc, "proxies"); proxies != nil {
			for _, p := range proxies.Content {
				entry := yamlx.Map(
					yamlx.Str("name"), yamlx.Clone(yamlx.Get(p, "name")),
					yamlx.Str("server"), yamlx.Clone(yamlx.Get(p, "server")),
				)
				nodes = append(nodes, yamlToRaw(t, entry))
			}
		}
		url := "https://sub.test/" + id
		rawCache := rawJSON(t, map[string]any{
			"nodes":    nodes,
			"metadata": map[string]any{"url": url, "hosts": ExtractHosts(body)},
		})
		var cache SubscriptionCache
		if err := jsondecode.Unmarshal(rawCache, &cache); err != nil {
			t.Fatal(err)
		}
		caches[id] = &cache
		enabled := true
		subsList = append(subsList, model.Subscription{ID: id, Name: id, URL: url, Enabled: &enabled})
	}
	index := func(sub model.Subscription) (*HostIndex, error) {
		c := caches[sub.ID]
		if c == nil {
			return nil, nil
		}
		idx, err := NewHostIndex(c)
		if err != nil || idx == nil {
			return nil, err
		}
		return idx.ForSubscription(sub), nil
	}
	return index, &model.Config{Profile: &model.Profile{}, Subscriptions: subsList}
}

func providerFor(id string) *yamlx.Node {
	return yamlx.Map(
		yamlx.Str("proxy-providers"),
		yamlx.Map(
			yamlx.Str(id),
			yamlx.Map(
				yamlx.Str("type"), yamlx.Str("http"),
				yamlx.Str("url"), yamlx.Str("https://panel.test/api/subscriptions/"+id+"/proxies"),
			),
		),
	)
}

func TestMergesOnlyDomainsOfNodesTheConfigurationDelivers(t *testing.T) {
	index, config := hostsIndexFixture(t, map[string]string{"air": airportHosts})
	main := providerFor("air")
	report, _, err := MergeHosts(main, config, nil, nil, nil, index)
	if err != nil {
		t.Fatal(err)
	}
	wantHosts := json.RawMessage(`{"jp-node.example.net":"jp-entry.example.org","hk-node.example.net":["198.51.100.12","2001:db8::12"]}`)
	if !testjson.Equal(wantHosts, yamlToRaw(t, yamlx.Get(main, "hosts"))) {
		t.Fatal(string(yamlToRaw(t, yamlx.Get(main, "hosts"))))
	}
	if report.Merged != 2 || report.Ignored != 1 || report.DomainMapped != 1 {
		t.Fatal(js(t, report))
	}
	wantSubs := json.RawMessage(`[{"id":"air","name":"air","total":3,"merged":2,"ignored":1}]`)
	if !testjson.Equal(wantSubs, report.Subscriptions) {
		t.Fatal(js(t, report.Subscriptions))
	}
}

func TestHandWrittenHostsWinThenMosdnsThenSubscriptions(t *testing.T) {
	body := strings.Replace(airportHosts, "  google.com: 203.0.113.66\n", "  wild.example.net: 198.51.100.40\n", 1)
	body = strings.Replace(body, "  - {name: SG 01,", "  - {name: W, type: socks5, server: wild.example.net, port: 1}\n  - {name: SG 01,", 1)
	index, config := hostsIndexFixture(t, map[string]string{"air": body})
	main := providerFor("air")
	yamlx.Set(main, "hosts", yamlFromRaw(t, json.RawMessage(`{"jp-node.example.net":"192.0.2.10","+.wild.example.net":"192.0.2.11"}`)))
	report, _, err := MergeHosts(main, config, []string{"jp-node.example.net", "+.wild.example.net"}, []string{"hk-node.example.net"}, nil, index)
	if err != nil {
		t.Fatal(err)
	}
	wantHosts := json.RawMessage(`{"jp-node.example.net":"192.0.2.10","+.wild.example.net":"192.0.2.11"}`)
	if !testjson.Equal(wantHosts, yamlToRaw(t, yamlx.Get(main, "hosts"))) {
		t.Fatal(string(yamlToRaw(t, yamlx.Get(main, "hosts"))))
	}
	reasons := map[string]string{}
	for _, it := range report.Items {
		if it.Status == "ignored" {
			reasons[it.Domain] = it.Reason
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
	yamlx.Set(main, "hosts", yamlFromRaw(t, json.RawMessage(`{"+.example.org":"jp-node.example.net"}`)))
	report, _, err := MergeHosts(main, config, nil, nil, nil, index)
	if err != nil {
		t.Fatal(err)
	}
	hosts := yamlx.Get(main, "hosts")
	if yamlx.Get(hosts, "+.example.org").Value != "jp-node.example.net" || yamlx.Get(hosts, "jp-node.example.net") != nil {
		t.Fatal(string(yamlToRaw(t, hosts)))
	}
	found := false
	for _, it := range report.Items {
		if it.Domain == "jp-node.example.net" && it.Reason == "cycle" {
			found = true
		}
	}
	if !found {
		t.Fatal(js(t, report))
	}
}

func TestHostsDigestAndDescribeChange(t *testing.T) {
	a := ExtractHosts(airportHosts)
	da, _ := HostDigest(a)
	da2, _ := HostDigest(ExtractHosts(airportHosts))
	dnil, _ := HostDigest(nil)
	if da != da2 || da == dnil {
		t.Fatal("digest")
	}
	b := ExtractHosts(strings.Replace(airportHosts, "203.0.113.66", "203.0.113.67", 1))
	diff, err := CompareHosts(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !testjson.Equal(json.RawMessage(`{"added":0,"removed":0,"changed":1}`), diff) {
		t.Fatal(js(t, diff))
	}
}

// ---- test_proxy_server_dns.py (helpers)

func TestListenerAddressIsDerivedFromDnsListen(t *testing.T) {
	cases := []struct {
		listen string
		addr   string
		reason string
	}{
		{"0.0.0.0:1053", "udp://127.0.0.1:1053", ""}, {"[::]:53", "udp://127.0.0.1:53", ""}, {":7874", "udp://127.0.0.1:7874", ""},
		{"127.0.0.1:7874", "udp://127.0.0.1:7874", ""}, {"192.168.1.2:53", "udp://192.168.1.2:53", ""},
		{"[::1]:1053", "udp://[::1]:1053", ""}, {"localhost:53", "", "listen_unparsed"}, {"0.0.0.0", "", "listen_unparsed"},
		{"0.0.0.0:0", "", "listen_unparsed"}, {"", "", "no_listen"},
	}
	for _, c := range cases {
		a, r := DNSListener(c.listen)
		if a != c.addr || r != c.reason {
			t.Fatalf("%q: %q %q", c.listen, a, r)
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
	hosts := yamlFromRaw(t, json.RawMessage(`{"a.test":"b.test","b.test":"c.test","x.test":"y.test","y.test":"198.51.100.1","+.wild.test":["198.51.100.2"],"w.test":"n.wild.test","ip.test":["198.51.100.3"]}`))
	resolver := NewHostResolver(hosts)
	for domain, want := range map[string]string{"a.test": "c.test", "x.test": "", "w.test": "", "ip.test": "", "missing.test": ""} {
		if got := resolver.Target(domain); got != want {
			t.Fatalf("%s: %q", domain, got)
		}
	}
}

func TestCoreVersions(t *testing.T) {
	cases := []struct {
		text   string
		parsed string
		old    bool
	}{
		{"v1.19.33", "1.19.33", false}, {"v1.19.20", "1.19.20", false}, {"v1.19.19", "1.19.19", true},
		{"Mihomo Meta v1.18.10 linux amd64", "1.18.10", true}, {"1.20.0", "1.20.0", false},
		{"alpha-1a2b3c4", "", false}, {"", "", false},
	}
	for _, c := range cases {
		v, ok := CoreVersion(c.text)
		got := ""
		if ok {
			got = fmt.Sprintf("%s.%s.%s", v[0], v[1], v[2])
		}
		if got != c.parsed || IsCoreTooOld(c.text) != c.old {
			t.Fatalf("%q: %q %v", c.text, got, IsCoreTooOld(c.text))
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
	record, err := ExtractDialers(airportDialers)
	if err != nil {
		t.Fatal(err)
	}
	want := json.RawMessage(`{"targets":{"Hidden":{"type":"url-test","members":["Entry","HK"]},"Relay":{"type":"select","members":["Entry","HK"],"url":"http://x/204","interval":300,"lazy":true}},"unresolved":[]}`)
	if !testjson.Equal(want, record) {
		t.Fatal(js(t, record))
	}
}

func TestExtractReportsDynamicGroupsAsUnresolved(t *testing.T) {
	text := "proxies:\n  - {name: A, type: ss, dialer-proxy: Dyn}\nproxy-groups:\n  - {name: Dyn, type: select, use: [p]}\n"
	record, _ := ExtractDialers(text)
	if !testjson.Equal(json.RawMessage(`{"targets":{},"unresolved":["Dyn"]}`), record) {
		t.Fatal(js(t, record))
	}
	if r, _ := ExtractDialers("proxies: []\n"); r != nil {
		t.Fatal("no dialer-proxy means no record")
	}
}

func TestAdaptingTouchesOnlyAirportGroupsAndIsStable(t *testing.T) {
	var record DialersData
	if err := json.Unmarshal([]byte(`{"targets":{"Relay":{"type":"select","members":["X"]}},"unresolved":["Hidden"]}`), &record); err != nil {
		t.Fatal(err)
	}
	proxies := yamlFromRaw(t, json.RawMessage(`[{"name":"a","dialer-proxy":"Relay"},{"name":"b","dialer-proxy":"Hidden"},{"name":"c","dialer-proxy":"Other"},{"name":"d"}]`)).Content
	main := yamlFromRaw(t, json.RawMessage(`{"proxy-groups":[{"name":"Air/Relay"}],"proxies":[{"name":"Other"}]}`))
	got, err := AdaptProxies("Air", proxies, main, &record, true)
	if err != nil {
		t.Fatal(err)
	}
	want := json.RawMessage(`[{"name":"a","dialer-proxy":"Air/Relay"},{"name":"c","dialer-proxy":"Other"},{"name":"d"}]`)
	if !testjson.Equal(want, yamlToRaw(t, yamlx.Seq(got...))) {
		t.Fatal(string(yamlToRaw(t, yamlx.Seq(got...))))
	}
	again, err := AdaptProxies("Air", got, main, &record, true)
	if err != nil || !testjson.Equal(want, yamlToRaw(t, yamlx.Seq(again...))) {
		t.Fatal("adapting must be idempotent")
	}
	none, err := AdaptProxies("Air", proxies, main, &record, false)
	if err != nil || !testjson.Equal(json.RawMessage(`[{"name":"c","dialer-proxy":"Other"},{"name":"d"}]`), yamlToRaw(t, yamlx.Seq(none...))) {
		t.Fatal(string(yamlToRaw(t, yamlx.Seq(none...))))
	}
}

// ---- test_proxy_chains.py (validation and lowering contracts)

func chainNode(id string) string {
	return fmt.Sprintf(`{"id":%q,"name":%q,"type":"http","server":"%s.test","port":80}`, id, strings.ToUpper(id), id)
}

func chainProfileJSON(groups ...string) string {
	chain := `{"id":"chain","name":"Chain","type":"chain","chain":{"entry":{"type":"node","id":"a"},"exit":{"type":"node","id":"b"}}}`
	all := append([]string{chain}, groups...)
	return fmt.Sprintf(`{"profile_id":"default","nodes":[%s,%s],"proxy_groups":[%s],"rule_configs":[]}`,
		chainNode("a"), chainNode("b"), strings.Join(all, ","))
}

func chainProfileConfig(t *testing.T, groups ...string) *model.Config {
	t.Helper()
	cfg, err := decodeConfigRaw(json.RawMessage(chainProfileJSON(groups...)))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestInvalidChainMetadataIsRejectedBeforeConversion(t *testing.T) {
	mutations := map[string]func(t *testing.T) *model.Config{
		"missing_exit": func(t *testing.T) *model.Config {
			cfg := chainProfileConfig(t)
			cfg.Profile.ProxyGroups[0].Chain.Exit.ID = "missing"
			return cfg
		},
		"missing_entry": func(t *testing.T) *model.Config {
			cfg := chainProfileConfig(t)
			cfg.Profile.ProxyGroups[0].Chain.Entry.ID = "missing"
			return cfg
		},
		"disabled_exit": func(t *testing.T) *model.Config {
			cfg := chainProfileConfig(t)
			f := false
			cfg.Nodes[1].Enabled = &f
			return cfg
		},
		"disabled_entry": func(t *testing.T) *model.Config {
			cfg := chainProfileConfig(t)
			f := false
			cfg.Nodes[0].Enabled = &f
			return cfg
		},
		"duplicate_name": func(t *testing.T) *model.Config {
			cfg := chainProfileConfig(t)
			cfg.Profile.ProxyGroups[0].Name = "B"
			return cfg
		},
		"reserved_name": func(t *testing.T) *model.Config {
			cfg := chainProfileConfig(t)
			cfg.Profile.ProxyGroups[0].Name = "DIRECT"
			return cfg
		},
		"self_entry": func(t *testing.T) *model.Config {
			cfg := chainProfileConfig(t)
			cfg.Profile.ProxyGroups[0].Chain.Entry.ID = "b"
			return cfg
		},
		"subscription_exit": func(t *testing.T) *model.Config {
			cfg := chainProfileConfig(t)
			cfg.Nodes[1].SubscriptionID = "feed"
			return cfg
		},
		"subscription_entry": func(t *testing.T) *model.Config {
			cfg := chainProfileConfig(t)
			cfg.Nodes[0].SubscriptionID = "feed"
			return cfg
		},
		"malformed_entry": func(t *testing.T) *model.Config {
			raw := fmt.Sprintf(`{"profile_id":"default","nodes":[%s,%s],"proxy_groups":[{"id":"chain","name":"Chain","type":"chain","chain":{"entry":null,"exit":{"type":"node","id":"b"}}}],"rule_configs":[]}`,
				chainNode("a"), chainNode("b"))
			cfg, err := decodeConfigRaw(json.RawMessage(raw))
			if err != nil {
				t.Fatal(err)
			}
			return cfg
		},
		"malformed_chain": func(t *testing.T) *model.Config {
			raw := fmt.Sprintf(`{"profile_id":"default","nodes":[%s,%s],"proxy_groups":[{"id":"chain","name":"Chain","type":"chain","chain":[]}],"rule_configs":[]}`,
				chainNode("a"), chainNode("b"))
			cfg, err := decodeConfigRaw(json.RawMessage(raw))
			if err != nil {
				t.Fatal(err)
			}
			return cfg
		},
		"duplicate_id": func(t *testing.T) *model.Config {
			cfg := chainProfileConfig(t)
			dup := cfg.Profile.ProxyGroups[0]
			dup.Name = "Other"
			cfg.Profile.ProxyGroups = append(cfg.Profile.ProxyGroups, dup)
			return cfg
		},
	}
	for name, build := range mutations {
		p := build(t)
		if _, err := LowerChains(p); err == nil {
			t.Fatalf("%s: lowering must be rejected", name)
		} else {
			err = referenceFailure(err)
			if _, ok := err.(*DialerReferenceError); !ok {
				t.Fatalf("%s: %T (%v)", name, err, err)
			}
		}
	}
}

func TestLoweringIDsAreUniqueAndNestedObjectsAreIndependent(t *testing.T) {
	raw := fmt.Sprintf(`{"profile_id":"default","nodes":[%s,{"id":"b","name":"B","type":"http","server":"b.test","port":80,"params":{"headers":{"User-Agent":"shared"}}},%s],"proxy_groups":[{"id":"chain","name":"Chain","type":"chain","chain":{"entry":{"type":"node","id":"a"},"exit":{"type":"node","id":"b"}}},{"id":"chooser","name":"Choose","type":"select","proxies_order":[{"type":"strategy","id":"chain"}]}],"rule_configs":[]}`,
		chainNode("a"), chainNode("__chain__chain"))
	p, err := decodeConfigRaw(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	beforeNodes, _ := model.Marshal(p.Nodes)
	beforeGroups, _ := model.Marshal(p.Profile.ProxyGroups)
	runtime, err := LowerChains(p)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var derived *model.Node
	for i := range runtime.Nodes {
		n := &runtime.Nodes[i]
		if seen[n.ID] {
			t.Fatal("duplicate id")
		}
		seen[n.ID] = true
		if n.Name == "Chain" {
			derived = n
		}
	}
	for _, g := range runtime.Profile.ProxyGroups {
		if seen[g.ID] || g.Type == "chain" {
			t.Fatal("group id clash or chain left")
		}
		seen[g.ID] = true
	}
	order := runtime.Profile.ProxyGroups[0].ProxiesOrder
	wantOrder := json.RawMessage(fmt.Sprintf(`[{"type":"node","id":%q}]`, derived.ID))
	if !testjson.Equal(wantOrder, order) {
		t.Fatal(js(t, order))
	}
	if err := json.Unmarshal([]byte(`{"headers":{"User-Agent":"runtime"}}`), &derived.Params); err != nil {
		t.Fatal(err)
	}
	afterNodes, _ := model.Marshal(p.Nodes)
	afterGroups, _ := model.Marshal(p.Profile.ProxyGroups)
	if !testjson.Equal(json.RawMessage(beforeNodes), json.RawMessage(afterNodes)) ||
		!testjson.Equal(json.RawMessage(beforeGroups), json.RawMessage(afterGroups)) {
		t.Fatal("the profile must not change")
	}
	origParams, err := model.Marshal(runtime.Nodes[1].Params)
	if err != nil {
		t.Fatal(err)
	}
	if !testjson.Equal(json.RawMessage(`{"headers":{"User-Agent":"shared"}}`), json.RawMessage(origParams)) {
		t.Fatal("the original node must stay untouched")
	}
}

func TestIndirectGroupCycleChecksEveryBranch(t *testing.T) {
	p := chainProfileConfig(t, `{"id":"entry","name":"Entry","type":"select","manual_nodes":["a"],"include_groups":["nested"]}`,
		`{"id":"nested","name":"Nested","type":"select","include_groups":["chain"]}`)
	p.Profile.ProxyGroups[0].Chain.Entry = model.ResourceRef{Type: "group", ID: "entry"}
	if err := sharedDialers().Validate(p); err == nil {
		t.Fatal("cycle must be rejected")
	}
}

func TestFollowChainIsRejectedEvenWhenFollowerIsDisabled(t *testing.T) {
	p := chainProfileConfig(t, `{"id":"follower","name":"Follower","type":"select","follow_group":"chain","enabled":false}`)
	if err := sharedDialers().Validate(p); err == nil {
		t.Fatal("must be rejected")
	}
}

func TestManualOnlyConstraintIncludesNestedStaticGroupMembers(t *testing.T) {
	p := chainProfileConfig(t, `{"id":"entry","name":"Entry","type":"select","manual_nodes":["a"]}`)
	p.Profile.ProxyGroups[0].Chain.Entry = model.ResourceRef{Type: "group", ID: "entry"}
	p.Nodes[0].SubscriptionID = "feed"
	if err := sharedDialers().Validate(p); err == nil {
		t.Fatal("must be rejected")
	}
}

func TestDisabledChainCannotBeUsedByEnabledConsumer(t *testing.T) {
	for _, consumer := range []string{
		`{"id":"g","name":"Group","type":"select","include_groups":["chain"]}`,
		`{"id":"g","name":"Group","type":"select","proxies_order":[{"type":"strategy","id":"chain"}]}`,
		`{"id":"outer","name":"Outer","type":"chain","chain":{"entry":{"type":"group","id":"chain"},"exit":{"type":"node","id":"b"}}}`,
	} {
		p := chainProfileConfig(t, consumer)
		f := false
		p.Profile.ProxyGroups[0].Enabled = &f
		if _, err := LowerChains(p); err == nil {
			t.Fatalf("%s must be rejected", consumer)
		}
	}
}

func TestDisabledChainPreservesDisabledExitMetadataWithoutEmitting(t *testing.T) {
	p := chainProfileConfig(t)
	f := false
	p.Profile.ProxyGroups[0].Enabled = &f
	p.Nodes[1].Enabled = &f
	if err := sharedDialers().Validate(p); err != nil {
		t.Fatal(err)
	}
}

func TestRawDialerCycleThroughNamedChainIsRejected(t *testing.T) {
	p := chainProfileConfig(t)
	if err := json.Unmarshal([]byte(`{"dialer-proxy":"Chain"}`), &p.Nodes[0].Params); err != nil {
		t.Fatal(err)
	}
	if err := sharedDialers().Validate(p); err == nil {
		t.Fatal("must be rejected")
	}
}

func TestChainAsExitComposesAllHops(t *testing.T) {
	raw := fmt.Sprintf(`{"nodes":[%s,%s,%s],"proxy_groups":[
		{"id":"inner","name":"Inner","type":"chain","chain":{"entry":{"type":"node","id":"a"},"exit":{"type":"node","id":"b"}}},
		{"id":"outer","name":"Outer","type":"chain","chain":{"entry":{"type":"node","id":"c"},"exit":{"type":"group","id":"inner"}}}]}`,
		chainNode("a"), chainNode("b"), chainNode("c"))
	p, err := decodeConfigRaw(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := LowerChains(p)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]model.Node{}
	byID := map[string]model.Node{}
	for _, n := range runtime.Nodes {
		byName[n.Name] = n
		byID[n.ID] = n
	}
	// A -> (B -> C) is (A -> B) -> C: Outer dials through the cloned inner entry.
	outer := byName["Outer"]
	if outer.DialerRef == nil {
		t.Fatalf("outer %s", js(t, outer))
	}
	hop, ok := byID[outer.DialerRef.ID]
	if !ok || hop.DialerRef == nil {
		t.Fatalf("outer %s", js(t, outer))
	}
}

// test_reported_core_version_comes_from_domain_discovery_only, with the
// traffic store replaced by a map.
func TestReportedCoreVersionComesFromDomainDiscoveryOnly(t *testing.T) {
	statuses := map[string]CoreStatus{}
	calls := 0
	load := func(id string) CoreStatus { calls++; return statuses[id] }
	on := true
	offFlag := false
	mihomo := "mihomo"
	mosdnsKind := "mosdns"
	agent := CoreAgent{ID: "a1", ServiceType: &mihomo, DiscoveryEnabled: &on}
	if v := AgentCoreVersion(agent, load); v != "" {
		t.Fatalf("no report: %q", v)
	}
	statuses["a1"] = CoreStatus{MihomoVersion: "v1.19.10"}
	if v := AgentCoreVersion(agent, load); v != "v1.19.10" {
		t.Fatalf("got %q", v)
	}
	calls = 0
	off := agent
	off.DiscoveryEnabled = &offFlag
	mosdns := agent
	mosdns.ServiceType = &mosdnsKind
	if AgentCoreVersion(off, load) != "" || AgentCoreVersion(mosdns, load) != "" || calls != 0 {
		t.Fatalf("stale or non-Mihomo report used (%d loads)", calls)
	}
	old := CoreAgent{ID: "old", Name: "旧内核", DiscoveryEnabled: &on}
	statuses["old"] = CoreStatus{MihomoVersion: "v1.19.0"}
	other := CoreAgent{ID: "other", ProfileID: "p2", DiscoveryEnabled: &on}
	statuses["other"] = CoreStatus{MihomoVersion: "v1.19.0"}
	got := OldCoreAgents([]CoreAgent{agent, old, other}, "default", "default", load)
	if !testjson.Equal(json.RawMessage(`[{"name":"a1","version":"v1.19.10"},{"name":"旧内核","version":"v1.19.0"}]`), got) {
		t.Fatalf("outdated %s", js(t, got))
	}
	if !testjson.Equal(json.RawMessage("[]"), OldCoreAgents(nil, "default", "default", load)) {
		t.Fatal("empty result must be a list")
	}
}
