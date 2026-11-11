package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"gopkg.in/yaml.v3"
)

const oxRankingTestPlugins = `- {tag: records, type: query_recorder, args: {retention_days: 3}}
- {tag: adguard_rules, type: adguard_rule, args: {files: [ads.txt]}}
- {tag: adguard_block, type: black_hole, args: {mode: nxdomain}}
- tag: adguard_reject
  type: sequence
  args:
    - exec: $adguard_block
    - exec: accept
- tag: sequence_main
  type: sequence
  args:
    - exec: metrics_collector metrics
    - exec: $records
    - exec: prefer_ipv4
    - matches: [question $adguard_rules]
      exec: goto adguard_reject
- tag: forward_local
  type: forward
  args:
    upstreams:
      - {tag: local, addr: 'udp://192.0.2.53'}
      - {tag: local, addr: 'tls://dns.example:853'}
      - {addr: 'https://user:secret@dns.example/dns-query?token=secret#token'}
- {tag: server, type: udp_server, args: {entry: sequence_main, listen: ':53'}}
`

func oxRankingConfigForTest(t *testing.T, yamlText string) oxRankingConfig {
	t.Helper()
	var plugins []interface{}
	if err := yaml.Unmarshal([]byte(yamlText), &plugins); err != nil {
		t.Fatal(err)
	}
	return oxMonitorRankingConfig(plugins)
}

func TestOxiDNSRankingsUseCoreAggregatesAndConfirmedBlockPath(t *testing.T) {
	var cfg *Config
	var calls []string
	var mu sync.Mutex
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Error("statistics must never mutate the core")
		}
		username, password, ok := r.BasicAuth()
		if !ok || username != "monitor-user" || password != "core-secret" || r.Header.Get("Cookie") != "" {
			t.Error("invalid authentication boundary")
		}
		mu.Lock()
		calls = append(calls, r.URL.RequestURI())
		mu.Unlock()
		if r.URL.Path == "/api/health" {
			fmt.Fprint(w, oxMonitorTestHealth)
			return
		}
		if r.URL.Path == "/api/reload/status" {
			oxRankingTestRuntime(t, w, cfg)
			return
		}
		if r.URL.Query().Get("limit") != "10" || r.URL.Query().Get("since_ms") != "100" || r.URL.Query().Get("until_ms") != "200" || r.URL.Query().Has("recorder") {
			t.Errorf("incorrect aggregate range: %s", r.URL)
		}
		switch r.URL.Path {
		case "/api/plugins/records/stats/top_clients":
			fmt.Fprint(w, `{"ok":true,"sample_size":10000,"rows":[{"key":"192.0.2.1","count":4000,"share":0.4}]}`)
		case "/api/plugins/records/stats/top_qnames":
			if matcher := r.URL.Query().Get("matcher_tag"); matcher != "" {
				if matcher != "qs.match.sequence_main.3.0.question" || r.URL.Query().Get("status") != "has_response" {
					t.Errorf("unsafe block inference: %s", r.URL)
				}
				fmt.Fprint(w, `{"ok":true,"sample_size":20,"rows":[{"key":"ads.example.","count":20,"share":1}]}`)
			} else {
				fmt.Fprint(w, `{"ok":true,"sample_size":10000,"rows":[{"key":"example.com.","count":1000,"share":0.1}]}`)
			}
		default:
			t.Errorf("rankings used unexpected API: %s", r.URL)
			w.WriteHeader(500)
		}
	}))
	defer core.Close()
	cfg = oxMonitorTestConfig(t, core.URL, oxRankingTestPlugins)
	before, _ := os.ReadFile(cfg.ConfigPath)
	response := oxMonitorTestRequest(cfg, "GET", "/api/oxidns/rankings?since_ms=100&until_ms=200")
	var result struct {
		Success         bool           `json:"success"`
		Recorder        string         `json:"recorder"`
		SourceRevision  string         `json:"source_revision"`
		RuntimeRevision string         `json:"runtime_revision"`
		InstanceID      string         `json:"instance_id"`
		CollectedAt     int64          `json:"collected_at_ms"`
		Retention       int            `json:"retention_days"`
		SampleLimit     int            `json:"sample_limit"`
		Clients         oxRankingGroup `json:"clients"`
		Domains         oxRankingGroup `json:"domains"`
		Blocked         struct {
			Available bool   `json:"available"`
			Matcher   string `json:"matcher_tag"`
			oxRankingGroup
		} `json:"blocked"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != 200 || !result.Success || result.Recorder != "records" || result.SourceRevision == "" || len(result.RuntimeRevision) != 64 || result.InstanceID != "test-instance" || result.CollectedAt <= 0 || result.Retention != 3 || result.SampleLimit != 10000 {
		t.Fatalf("invalid metadata: %d %s (%v)", response.Code, response.Body.String(), err)
	}
	if !result.Clients.AtLimit || !result.Domains.AtLimit || !result.Blocked.Available || result.Blocked.AtLimit || result.Blocked.SampleSize != 20 || result.Blocked.Matcher != "qs.match.sequence_main.3.0.question" {
		t.Fatalf("incorrect sample/block semantics: %s", response.Body.String())
	}
	after, _ := os.ReadFile(cfg.ConfigPath)
	if string(before) != string(after) {
		t.Fatal("statistics changed configuration")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 7 || calls[0] != "/api/health" || calls[6] != "/api/health" {
		t.Fatalf("snapshot must be bracketed by health checks: %v", calls)
	}
}

func TestOxiDNSRankingsDoNotGuessCustomOrNegativeResponseBlocks(t *testing.T) {
	for _, test := range []struct{ name, text string }{
		{"additional condition", strings.Replace(oxRankingTestPlugins, "matches: [question $adguard_rules]", "matches: [question $adguard_rules, qtype A]", 1)},
		{"different action", strings.Replace(oxRankingTestPlugins, "exec: goto adguard_reject", "exec: goto remote_dns", 1)},
		{"overwritten response", strings.Replace(oxRankingTestPlugins, "- exec: accept", "- exec: $forward_remote", 1)},
		{"provider changed", strings.Replace(oxRankingTestPlugins, "type: adguard_rule", "type: domain_set", 1)},
		{"blackhole changed", strings.Replace(oxRankingTestPlugins, "type: black_hole", "type: forward", 1)},
		{"blackhole custom address", strings.Replace(oxRankingTestPlugins, "args: {mode: nxdomain}", "args: {mode: custom, ips: [192.0.2.1]}", 1)},
		{"blackhole unknown arguments", strings.Replace(oxRankingTestPlugins, "args: {mode: nxdomain}", "args: {mode: nxdomain, unknown: true}", 1)},
		{"unknown wrapper", strings.Replace(oxRankingTestPlugins, "exec: prefer_ipv4", "exec: $custom_wrapper", 1)},
		{"late recorder", strings.Replace(oxRankingTestPlugins, "- exec: $records\n", "- exec: $another\n", 1)},
		{"duplicate sequence", oxRankingTestPlugins + "- {tag: sequence_main, type: sequence, args: [{exec: accept}]}\n"},
		{"outer wrapper", strings.Replace(oxRankingTestPlugins, "entry: sequence_main", "entry: wrapper", 1) + "- {tag: wrapper, type: sequence, args: [{exec: 'jump sequence_main'}, {exec: '$forward_local'}]}\n"},
		{"alternate wrapped entry", oxRankingTestPlugins + "- {tag: wrapper, type: sequence, args: [{exec: 'jump sequence_main'}, {exec: '$forward_local'}]}\n"},
		{"unknown caller", oxRankingTestPlugins + "- {tag: custom, type: custom, args: {child: sequence_main}}\n"},
		{"no direct DNS listener", strings.Replace(oxRankingTestPlugins, "entry: sequence_main", "entry: other", 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if result := oxRankingConfigForTest(t, test.text); len(result.blocked) != 0 {
				t.Fatalf("guessed custom block: %#v", result.blocked)
			}
		})
	}
	result := oxRankingConfigForTest(t, oxRankingTestPlugins)
	if !reflect.DeepEqual(result.blocked["records"], []string{"qs.match.sequence_main.3.0.question"}) {
		t.Fatal("generated AdGuard path was not recognized")
	}
	// Blocked query types precede AdGuard; current and earlier drafts differ in action.
	for _, action := range []string{"black_hole nodata short_circuit=true", "reject 0"} {
		text := strings.Replace(oxRankingTestPlugins, "    - exec: prefer_ipv4\n",
			"    - exec: prefer_ipv4\n    - matches: [qtype 65 255]\n      exec: "+action+"\n", 1)
		if result := oxRankingConfigForTest(t, text); !reflect.DeepEqual(result.blocked["records"], []string{"qs.match.sequence_main.4.0.question"}) {
			t.Fatalf("%s blocked qtypes hid the AdGuard path: %#v", action, result.blocked)
		}
	}
}

func TestOxiDNSRankingsUpstreamIdentityAndAddressRedaction(t *testing.T) {
	result := oxRankingConfigForTest(t, oxRankingTestPlugins)
	want := []oxUpstreamLabel{
		{"forward_local", "local", "udp://192.0.2.53"},
		{"forward_local", "local#1", "tls://dns.example:853"},
		{"forward_local", "https://user:secret@dns.example/dns-query?token=secret#token", "https://dns.example/dns-query"},
	}
	if !reflect.DeepEqual(result.upstreams, want) {
		t.Fatalf("incorrect metric identity/display mapping: %#v", result.upstreams)
	}
	duplicateAddress := `- tag: f
  type: forward
  args:
    upstreams:
      - {addr: 1.1.1.1}
      - {addr: 1.1.1.1}
      - {addr: 1.1.1.1}
`
	labels := oxRankingConfigForTest(t, duplicateAddress).upstreams
	if len(labels) != 3 || labels[1].Upstream != "1.1.1.1#1" || labels[2].Upstream != "1.1.1.1#2" {
		t.Fatalf("duplicate unnamed upstream mapping: %#v", labels)
	}
}

func TestOxiDNSRankingsValidationAndEmptyResponses(t *testing.T) {
	for _, body := range []string{
		`{"ok":true,"sample_size":null,"rows":[]}`,
		`{"ok":true,"sample_size":10001,"rows":[]}`,
		`{"ok":true,"sample_size":1,"rows":null}`,
		`{"ok":false,"sample_size":0,"rows":[],"message":"private-database"}`,
		`{"ok":true,"sample_size":0,"rows":[{"key":"x.","count":1}]}`,
		`{"ok":true,"sample_size":1,"rows":[{"key":"x.","count":0}]}`,
		`{"ok":true,"sample_size":1,"rows":[{"key":"x.","count":1},{"key":"x.","count":1}]}`,
		`{"ok":true,"sample_size":1,"rows":[{"key":"x.\nsecret","count":1}]}`,
	} {
		if _, err := oxDecodeRanking([]byte(body), false, oxRankingLimit); err == nil {
			t.Errorf("accepted malformed ranking: %s", body)
		}
	}
	if _, err := oxDecodeRanking([]byte(`{"ok":true,"sample_size":1,"rows":[{"key":"192.0.2.1","count":1},{"key":"192.0.2.2","count":1}]}`), true, oxRankingLimit); err == nil {
		t.Fatal("client total exceeded recorded query count")
	}
	if result, err := oxDecodeRanking([]byte(`{"ok":true,"sample_size":0,"rows":[]}`), true, oxRankingLimit); err != nil || result.Rows == nil || result.AtLimit {
		t.Fatalf("empty ranking: %#v %v", result, err)
	}
	if result, err := oxDecodeRanking([]byte(`{"ok":true,"sample_size":2,"rows":[{"key":"multi.example.","count":3,"share":0}]}`), false, oxRankingLimit); err != nil || result.Rows[0].Share != 1.5 {
		t.Fatalf("multi-question occurrence counts must be preserved: %#v %v", result, err)
	}
}

func TestOxiDNSRankingsRejectInvalidRequestsBeforeCore(t *testing.T) {
	var calls atomic.Int32
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer core.Close()
	cfg := oxMonitorTestConfig(t, core.URL, oxRankingTestPlugins)
	for _, query := range []string{"", "recorder=records", "since_ms=-1", "since_ms=9223372036854775808", "since_ms=1&since_ms=2", "since_ms=3&until_ms=2", "since_ms=1&limit=10000", "since_ms=1&matcher_tag=ads", "since_ms=1&recorder=../flush", "since_ms=1&url=http://evil.test", "since_ms=1&qname=abc", "since_ms=1&rows=0", "since_ms=1&rows=2001", "since_ms=1&rows=-5", "since_ms=1&rows=10.5", "since_ms=1&rows=010", "since_ms=1&rows=10&rows=20", "since_ms=1&rows=all"} {
		oxMonitorTestError(t, oxMonitorTestRequest(cfg, "GET", "/api/oxidns/rankings?"+query), 400, "INVALID_QUERY")
	}
	for _, method := range []string{"POST", "DELETE", "PUT"} {
		oxMonitorTestError(t, oxMonitorTestRequest(cfg, method, "/api/oxidns/rankings?since_ms=1"), 405, "METHOD_NOT_ALLOWED")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid ranking request contacted core")
	}
}

func TestOxiDNSRankingsDefaultRecorderAndUnavailableBlocks(t *testing.T) {
	var cfg *Config
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			fmt.Fprint(w, oxMonitorTestHealth)
			return
		}
		if r.URL.Path == "/api/reload/status" {
			oxRankingTestRuntime(t, w, cfg)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/plugins/a/stats/") || r.URL.Query().Get("until_ms") == "" || r.URL.Query().Has("matcher_tag") {
			t.Errorf("unexpected core call: %s", r.URL)
		}
		fmt.Fprint(w, `{"ok":true,"sample_size":0,"rows":[]}`)
	}))
	defer core.Close()
	cfg = oxMonitorTestConfig(t, core.URL, "- {tag: z, type: query_recorder}\n- {tag: a, type: query_recorder}\n")
	response := oxMonitorTestRequest(cfg, "GET", "/api/oxidns/rankings?since_ms=1")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"recorder":"a"`) || !strings.Contains(response.Body.String(), `"available":false`) || !strings.Contains(response.Body.String(), `"retention_days":7`) {
		t.Fatalf("unexpected defaults: %d %s", response.Code, response.Body.String())
	}
	// Existing query selection still requires the user to choose an instance.
	oxMonitorTestError(t, oxMonitorTestRequest(cfg, "GET", "/api/oxidns/queries"), 400, "INVALID_QUERY")
}

func TestOxiDNSMonitorRejectsSnapshotAcrossRestartOrConfigChange(t *testing.T) {
	for _, endpoint := range []string{"/api/oxidns/overview", "/api/oxidns/rankings?since_ms=1"} {
		for _, configChange := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/config=%v", endpoint, configChange), func(t *testing.T) {
				var health atomic.Int32
				var cfg *Config
				core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/reload/status" {
						oxRankingTestRuntime(t, w, cfg)
						return
					}
					if r.URL.Path == "/api/health" {
						if health.Add(1) == 2 {
							if configChange {
								file, _ := os.OpenFile(cfg.ConfigPath, os.O_APPEND|os.O_WRONLY, 0600)
								_, _ = file.WriteString("# changed\n")
								_ = file.Close()
							} else {
								fmt.Fprint(w, strings.ReplaceAll(oxMonitorTestHealth, "test-instance", "restarted"))
								return
							}
						}
						fmt.Fprint(w, oxMonitorTestHealth)
						return
					}
					if r.URL.Path == "/api/metrics" {
						fmt.Fprint(w, "query_total 5\n")
						return
					}
					fmt.Fprint(w, `{"ok":true,"sample_size":0,"rows":[]}`)
				}))
				defer core.Close()
				cfg = oxMonitorTestConfig(t, core.URL, "- {tag: records, type: query_recorder}\n")
				oxMonitorTestError(t, oxMonitorTestRequest(cfg, "GET", endpoint), 409, "SNAPSHOT_CHANGED")
			})
		}
	}
}

func oxRankingTestRuntime(t *testing.T, w http.ResponseWriter, cfg *Config) {
	t.Helper()
	data, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		t.Error(err)
	}
	fmt.Fprintf(w, `{"status":"idle","pending":false,"in_progress":false,"running_version":%q,"last_started_ms":null,"last_completed_ms":null,"last_success_ms":null}`, sha256Hex(data))
}

func TestOxiDNSMonitorRequiresAppliedAndStableRunningConfiguration(t *testing.T) {
	for _, endpoint := range []string{"/api/oxidns/overview", "/api/oxidns/rankings?since_ms=1"} {
		for _, scenario := range []string{"unapplied", "pending", "in_progress", "same_file_reload", "missing_runtime_fields"} {
			t.Run(endpoint+"/"+scenario, func(t *testing.T) {
				var cfg *Config
				var statusCalls atomic.Int32
				core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet {
						t.Error("monitoring mutated core")
					}
					switch r.URL.Path {
					case "/api/health":
						fmt.Fprint(w, oxMonitorTestHealth)
					case "/api/reload/status":
						count := statusCalls.Add(1)
						data, _ := os.ReadFile(cfg.ConfigPath)
						version := sha256Hex(data)
						state, pending, running, times := "idle", "false", "false", "null"
						switch scenario {
						case "unapplied":
							version = strings.Repeat("0", 64)
						case "pending":
							state, pending = "pending", "true"
						case "in_progress":
							state, running = "in_progress", "true"
						case "same_file_reload":
							if count > 1 {
								state, times = "ok", "123456"
							}
						case "missing_runtime_fields":
							fmt.Fprintf(w, `{"status":"idle","pending":false,"in_progress":false,"running_version":%q}`, version)
							return
						}
						fmt.Fprintf(w, `{"status":%q,"pending":%s,"in_progress":%s,"running_version":%q,"last_started_ms":%s,"last_completed_ms":%s,"last_success_ms":%s}`, state, pending, running, version, times, times, times)
					case "/api/metrics":
						fmt.Fprint(w, "server_request_total{plugin_tag=\"server\",protocol=\"udp\"} 3\n")
					default:
						fmt.Fprint(w, `{"ok":true,"sample_size":0,"rows":[]}`)
					}
				}))
				defer core.Close()
				cfg = oxMonitorTestConfig(t, core.URL, "- {tag: records, type: query_recorder}\n")
				status, code := 409, "SNAPSHOT_CHANGED"
				if scenario == "missing_runtime_fields" {
					status, code = 502, "INVALID_RESPONSE"
				}
				oxMonitorTestError(t, oxMonitorTestRequest(cfg, "GET", endpoint), status, code)
			})
		}
	}
}

func TestOxiDNSMonitorRuntimeRevisionChangesAfterSameFileReload(t *testing.T) {
	var cfg *Config
	var reloaded atomic.Bool
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			fmt.Fprint(w, oxMonitorTestHealth)
		case "/api/reload/status":
			data, _ := os.ReadFile(cfg.ConfigPath)
			times := "null"
			if reloaded.Load() {
				times = "123456"
			}
			fmt.Fprintf(w, `{"status":"idle","pending":false,"in_progress":false,"running_version":%q,"last_started_ms":%s,"last_completed_ms":%s,"last_success_ms":%s}`, sha256Hex(data), times, times, times)
		case "/api/metrics":
			fmt.Fprint(w, "server_request_total{plugin_tag=\"server\",protocol=\"udp\"} 3\n")
		}
	}))
	defer core.Close()
	cfg = oxMonitorTestConfig(t, core.URL, "- {tag: records, type: query_recorder}\n")
	read := func() map[string]interface{} {
		response := oxMonitorTestRequest(cfg, "GET", "/api/oxidns/overview")
		var body map[string]interface{}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != 200 {
			t.Fatalf("overview: %d %s", response.Code, response.Body.String())
		}
		return body
	}
	before := read()
	reloaded.Store(true)
	after := read()
	if before["source_revision"] != after["source_revision"] || before["runtime_revision"] == after["runtime_revision"] {
		t.Fatal("same-file core reload did not change runtime identity")
	}
}

// Shaped like ConfigFlow's rule DNS actions output (OxiDNS 1.6.1 checked).
const oxRankingRulePlugins = `- {tag: records, type: query_recorder}
- {tag: adguard_rules, type: adguard_rule, args: {files: [ads.txt]}}
- {tag: adguard_block, type: black_hole, args: {mode: nxdomain}}
- tag: adguard_reject
  type: sequence
  args:
    - exec: $adguard_block
    - exec: accept
- {tag: rules_domain_1, type: domain_set, args: {exps: [domain:direct.test]}}
- {tag: rules_domain_2, type: domain_set, args: {exps: [domain:refused.test]}}
- {tag: rules_domain_3, type: domain_set, args: {exps: [domain:nx.test]}}
- {tag: rules_domain_4_2, type: domain_set, args: {exps: [domain:nodata.test]}}
- {tag: rules_domain_5, type: domain_set, args: {exps: [domain:bh.test]}}
- {tag: rules_domain_6, type: domain_set, args: {exps: [domain:up.test]}}
- {tag: rules_ip_1, type: ip_set, args: {ips: [203.0.113.0/28]}}
- {tag: rules_ip_2, type: ip_set, args: {ips: [203.0.113.64/26]}}
- {tag: rules_ip_3, type: ip_set, args: {ips: [203.0.113.128/26]}}
- {tag: custom_set, type: domain_set, args: {exps: [domain:custom.test]}}
- {tag: cf_dns_blackhole, type: black_hole, args: {mode: 'null', short_circuit: true}}
- tag: local_probe
  type: sequence
  args:
    - exec: $local_probe_cache
    - {matches: ['!has_resp'], exec: $forward_local}
    - exec: return
- tag: sequence_main
  type: sequence
  args:
    - exec: metrics_collector metrics
    - exec: $records
    - exec: prefer_ipv4
    - {matches: [qtype 65 255], exec: reject 0}
    - {matches: [question $adguard_rules], exec: goto adguard_reject}
    - {matches: [qname $rules_domain_1], exec: goto china_dns}
    - {matches: [qname $rules_domain_2], exec: reject}
    - {matches: [qname $rules_domain_3], exec: reject 3}
    - {matches: [qname $rules_domain_4_2], exec: reject 0}
    - {matches: [qname $rules_domain_5], exec: $cf_dns_blackhole}
    - {matches: [qname $rules_domain_6], exec: goto cf_dns_up_8c1812f0bae3}
    - exec: jump local_probe
    - {matches: [resp_ip $rules_ip_1], exec: goto direct_ip}
    - {matches: [resp_ip $rules_ip_2], exec: reject 3}
    - {matches: [resp_ip $rules_ip_3], exec: $cf_dns_blackhole}
    - exec: drop_resp
    - exec: goto proxy_dns_seq
- {tag: server, type: udp_server, args: {entry: sequence_main, listen: ':53'}}
`

func oxRankingRuleMatchers(indexes ...int) []string {
	result := []string{}
	for _, index := range indexes {
		kind := "qname"
		if index == 4 {
			kind = "question"
		} else if index > 11 {
			kind = "resp_ip"
		}
		result = append(result, fmt.Sprintf("qs.match.sequence_main.%d.0.%s", index, kind))
	}
	return result
}

func TestRankingsCountRuleBlocks(t *testing.T) {
	all := oxRankingRuleMatchers(4, 6, 7, 8, 9, 13, 14)
	if got := oxRankingConfigForTest(t, oxRankingRulePlugins).blocked["records"]; !reflect.DeepEqual(got, all) {
		t.Fatalf("rule blocks: %#v", got)
	}
	for _, test := range []struct {
		name, old, new string
		want           []string
	}{
		// Without short_circuit later entries replace the answer; nothing after counts.
		{"blackhole continues", "mode: 'null', short_circuit: true", "mode: 'null'", oxRankingRuleMatchers(4, 6, 7, 8)},
		{"blackhole bare null", "mode: 'null', short_circuit: true", "mode: null, short_circuit: true", oxRankingRuleMatchers(4, 6, 7, 8)},
		{"blackhole other type", "tag: cf_dns_blackhole, type: black_hole", "tag: cf_dns_blackhole, type: forward", oxRankingRuleMatchers(4, 6, 7, 8)},
		{"custom provider", "qname $rules_domain_3]", "qname $custom_set]", oxRankingRuleMatchers(4, 6, 8, 9, 13, 14)},
		{"missing provider", "qname $rules_domain_3]", "qname $rules_domain_9]", oxRankingRuleMatchers(4, 6, 8, 9, 13, 14)},
		{"wrong matcher kind", "qname $rules_domain_3]", "resp_ip $rules_domain_3]", oxRankingRuleMatchers(4, 6, 8, 9, 13, 14)},
		{"additional condition", "[qname $rules_domain_3]", "[qname $rules_domain_3, qtype 1]", oxRankingRuleMatchers(4, 6, 8, 9, 13, 14)},
		{"adguard not standard", "type: adguard_rule", "type: domain_set", oxRankingRuleMatchers(6, 7, 8, 9, 13, 14)},
		{"unknown wrapper", "exec: jump local_probe", "exec: $custom_wrapper", oxRankingRuleMatchers(4, 6, 7, 8, 9)},
		{"jump to unknown plugin", "exec: jump local_probe", "exec: jump forward_local", oxRankingRuleMatchers(4, 6, 7, 8, 9)},
		{"unconditional goto", "    - exec: jump local_probe\n", "    - exec: goto china_dns\n", oxRankingRuleMatchers(4, 6, 7, 8, 9)},
		{"no recorder", "    - exec: $records\n", "", nil},
		{"outer wrapper", "entry: sequence_main", "entry: other", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			text := strings.Replace(oxRankingRulePlugins, test.old, test.new, 1)
			if text == oxRankingRulePlugins {
				t.Fatal("fixture replacement did not apply")
			}
			if got := oxRankingConfigForTest(t, text).blocked["records"]; !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %#v want %#v", got, test.want)
			}
		})
	}
	// A device-policy style jump before the AdGuard entry does not hide it.
	policy := strings.Replace(oxRankingRulePlugins, "    - exec: prefer_ipv4\n", "    - exec: prefer_ipv4\n    - exec: jump cf_device_policy\n", 1) +
		"- {tag: cf_device_policy, type: sequence, args: [{matches: [client_ip 192.0.2.9], exec: goto china_dns}]}\n"
	shifted := []string{}
	for _, matcher := range oxRankingRuleMatchers(5, 7, 8, 9, 10, 14, 15) {
		shifted = append(shifted, strings.Replace(strings.Replace(matcher, ".5.0.qname", ".5.0.question", 1), ".14.0.qname", ".14.0.resp_ip", 1))
	}
	if got := oxRankingConfigForTest(t, policy).blocked["records"]; !reflect.DeepEqual(got, shifted) {
		t.Fatalf("policy jump: %#v want %#v", got, shifted)
	}
}

func TestRankingsMergeRuleBlockSamples(t *testing.T) {
	rows := func(pairs ...interface{}) []oxRankingRow {
		result := []oxRankingRow{}
		for i := 0; i < len(pairs); i += 2 {
			result = append(result, oxRankingRow{Key: pairs[i].(string), Count: uint64(pairs[i+1].(int))})
		}
		return result
	}
	single := oxRankingGroup{SampleSize: 3, Rows: rows("a.", 3), AtLimit: false}
	if got := oxMergeRankings([]oxRankingGroup{single}, oxRankingLimit); !reflect.DeepEqual(got, single) {
		t.Fatalf("single group changed: %#v", got)
	}
	merged := oxMergeRankings([]oxRankingGroup{
		{SampleSize: 4, Rows: rows("a.", 2, "b.", 2)},
		{SampleSize: 0, Rows: rows()},
		{SampleSize: 3, Rows: rows("c.", 2, "a.", 1)},
	}, oxRankingLimit)
	want := []oxRankingRow{{"a.", 3, 3.0 / 7}, {"b.", 2, 2.0 / 7}, {"c.", 2, 2.0 / 7}}
	if merged.SampleSize != 7 || merged.AtLimit || !reflect.DeepEqual(merged.Rows, want) {
		t.Fatalf("merged: %#v", merged)
	}
	many := []oxRankingGroup{{SampleSize: oxMonitorClientSampleLimit, AtLimit: true}, {SampleSize: 1, Rows: rows("z.", 1)}}
	for i := 0; i < 12; i++ {
		many[0].Rows = append(many[0].Rows, oxRankingRow{Key: fmt.Sprintf("d%02d.", i), Count: uint64(100 - i)})
	}
	capped := oxMergeRankings(many, oxRankingLimit)
	if capped.SampleSize != oxMonitorClientSampleLimit || !capped.AtLimit || len(capped.Rows) != oxRankingLimit || capped.Rows[0].Key != "d00." || capped.Rows[9].Key != "d09." {
		t.Fatalf("capped: %#v", capped)
	}
}

func TestRankingsFetchEveryRuleBlockMatcher(t *testing.T) {
	var cfg *Config
	var mu sync.Mutex
	var matchers []string
	samples := map[string]string{
		"qs.match.sequence_main.4.0.question": `{"ok":true,"sample_size":3,"rows":[{"key":"ads.example.","count":3}]}`,
		"qs.match.sequence_main.6.0.qname":    `{"ok":true,"sample_size":2,"rows":[{"key":"refused.test.","count":1},{"key":"ads.example.","count":1}]}`,
		"qs.match.sequence_main.14.0.resp_ip": `{"ok":true,"sample_size":1,"rows":[{"key":"ipbh.test.","count":1}]}`,
	}
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			fmt.Fprint(w, oxMonitorTestHealth)
			return
		case "/api/reload/status":
			oxRankingTestRuntime(t, w, cfg)
			return
		}
		if matcher := r.URL.Query().Get("matcher_tag"); matcher != "" {
			if len(r.URL.Query()["matcher_tag"]) != 1 || r.URL.Query().Get("status") != "has_response" || r.URL.Path != "/api/plugins/records/stats/top_qnames" {
				t.Errorf("unsafe block request: %s", r.URL)
			}
			mu.Lock()
			matchers = append(matchers, matcher)
			mu.Unlock()
			if body, ok := samples[matcher]; ok {
				fmt.Fprint(w, body)
				return
			}
		}
		fmt.Fprint(w, `{"ok":true,"sample_size":0,"rows":[]}`)
	}))
	defer core.Close()
	cfg = oxMonitorTestConfig(t, core.URL, oxRankingRulePlugins)
	response := oxMonitorTestRequest(cfg, "GET", "/api/oxidns/rankings?since_ms=1")
	var result struct {
		Blocked struct {
			Available bool     `json:"available"`
			Reason    string   `json:"reason"`
			Matcher   *string  `json:"matcher_tag"`
			Matchers  []string `json:"matcher_tags"`
			oxRankingGroup
		} `json:"blocked"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != 200 {
		t.Fatalf("rankings: %d %s", response.Code, response.Body.String())
	}
	all := oxRankingRuleMatchers(4, 6, 7, 8, 9, 13, 14)
	if !reflect.DeepEqual(matchers, all) {
		t.Fatalf("fetched matchers %v", matchers)
	}
	blocked := result.Blocked
	// ads.example. is counted under both the AdGuard and a rule matcher.
	want := []oxRankingRow{{"ads.example.", 4, 4.0 / 6}, {"ipbh.test.", 1, 1.0 / 6}, {"refused.test.", 1, 1.0 / 6}}
	if !blocked.Available || blocked.Reason != "standard_adguard_rule_dns" || blocked.Matcher != nil || !reflect.DeepEqual(blocked.Matchers, all) ||
		blocked.SampleSize != 6 || blocked.AtLimit || !reflect.DeepEqual(blocked.Rows, want) {
		t.Fatalf("merged block ranking: %s", response.Body.String())
	}
}

func TestRankingsTooManyRuleBlocksUnavailable(t *testing.T) {
	var cfg *Config
	var blockCalls atomic.Int32
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			fmt.Fprint(w, oxMonitorTestHealth)
			return
		case "/api/reload/status":
			oxRankingTestRuntime(t, w, cfg)
			return
		}
		if r.URL.Query().Has("matcher_tag") {
			blockCalls.Add(1)
		}
		fmt.Fprint(w, `{"ok":true,"sample_size":0,"rows":[]}`)
	}))
	defer core.Close()
	var text strings.Builder
	text.WriteString("- {tag: records, type: query_recorder}\n- {tag: server, type: udp_server, args: {entry: sequence_main}}\n")
	main := "- tag: sequence_main\n  type: sequence\n  args:\n    - exec: $records\n"
	for i := 1; i <= oxRankingBlockLimit+1; i++ {
		fmt.Fprintf(&text, "- {tag: rules_domain_%d, type: domain_set, args: {exps: [domain:r%d.test]}}\n", i, i)
		main += fmt.Sprintf("    - {matches: [qname $rules_domain_%d], exec: reject}\n", i)
	}
	cfg = oxMonitorTestConfig(t, core.URL, text.String()+main)
	response := oxMonitorTestRequest(cfg, "GET", "/api/oxidns/rankings?since_ms=1")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"blocked":{"at_limit":false,"available":false,"reason":"too_many_block_rules"`) || blockCalls.Load() != 0 {
		t.Fatalf("too many block rules: %d %s (%d block calls)", response.Code, response.Body.String(), blockCalls.Load())
	}
}

// ConfigFlow asks for every name of a short period to add periods up exactly.
func TestOxiDNSRankingsReturnMoreRowsOnRequest(t *testing.T) {
	var cfg *Config
	limits := map[string][]string{}
	var mu sync.Mutex
	many := func(prefix string, n int) string {
		rows := []string{}
		for i := 0; i < n; i++ {
			rows = append(rows, fmt.Sprintf(`{"key":"%s%d.example.","count":%d,"share":0}`, prefix, i, n-i))
		}
		return strings.Join(rows, ",")
	}
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			fmt.Fprint(w, oxMonitorTestHealth)
			return
		case "/api/reload/status":
			oxRankingTestRuntime(t, w, cfg)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		mu.Lock()
		limits[r.URL.Path] = append(limits[r.URL.Path], r.URL.Query().Get("limit"))
		mu.Unlock()
		switch {
		case r.URL.Path == "/api/plugins/records/stats/top_clients":
			fmt.Fprintf(w, `{"ok":true,"sample_size":3,"rows":[{"key":"192.0.2.1","count":2,"share":0},{"key":"192.0.2.2","count":1,"share":0}]}`)
		case r.URL.Query().Get("matcher_tag") != "":
			fmt.Fprintf(w, `{"ok":true,"sample_size":9000,"rows":[%s]}`, many("ad", limit))
		default:
			fmt.Fprintf(w, `{"ok":true,"sample_size":9000,"rows":[%s]}`, many("site", limit))
		}
	}))
	defer core.Close()
	cfg = oxMonitorTestConfig(t, core.URL, oxRankingTestPlugins)
	var result struct {
		RowsLimit int            `json:"rows_limit"`
		Domains   oxRankingGroup `json:"domains"`
		Blocked   oxRankingGroup `json:"blocked"`
		Clients   oxRankingGroup `json:"clients"`
	}
	response := oxMonitorTestRequest(cfg, "GET", "/api/oxidns/rankings?since_ms=100&until_ms=200&rows=2000")
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != 200 {
		t.Fatalf("rows=2000: %d %s", response.Code, response.Body.String()[:200])
	}
	if result.RowsLimit != 2000 || len(result.Domains.Rows) != 2000 || result.Domains.SampleSize != 9000 || result.Domains.AtLimit || len(result.Clients.Rows) != 2 {
		t.Fatalf("more rows: limit %d, %d domains, sample %d", result.RowsLimit, len(result.Domains.Rows), result.Domains.SampleSize)
	}
	if len(result.Blocked.Rows) != 2000 || result.Blocked.Rows[0].Key != "ad0.example." {
		t.Fatalf("blocked rows: %d", len(result.Blocked.Rows))
	}
	mu.Lock()
	for path, seen := range limits {
		for _, limit := range seen {
			if limit != "2000" {
				t.Fatalf("%s asked the core for %s rows", path, limit)
			}
		}
	}
	limits = map[string][]string{}
	mu.Unlock()
	// Without rows= nothing changes: ten rows, as older ConfigFlow versions expect.
	response = oxMonitorTestRequest(cfg, "GET", "/api/oxidns/rankings?since_ms=100&until_ms=200")
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != 200 || result.RowsLimit != 10 || len(result.Domains.Rows) != 10 {
		t.Fatalf("default: %d rows, limit %d", len(result.Domains.Rows), result.RowsLimit)
	}
	// A core answering with more rows than were asked for is rejected.
	if _, err := oxDecodeRanking([]byte(`{"ok":true,"sample_size":9,"rows":[`+many("x", 11)+`]}`), false, 10); err == nil {
		t.Fatal("eleven rows accepted for ten")
	}
	if got, err := oxDecodeRanking([]byte(`{"ok":true,"sample_size":9,"rows":[`+many("x", 11)+`]}`), false, 11); err != nil || len(got.Rows) != 11 {
		t.Fatalf("eleven rows for eleven: %v", err)
	}
}

