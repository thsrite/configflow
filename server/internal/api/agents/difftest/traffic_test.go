package difftest

// A05 (traffic history and traffic store) part of the Flask↔Go
// differential: seeded Agents with domain discovery / traffic history, a
// saved traffic store file, a 1.8.51-go traffic_history.sqlite3 (legacy
// tables, migrated on first open by both), default discovery rulesets for
// provider_revisions; the eight A05 routes with success and failure cases;
// after the graceful stop the SQLite schema and rows and the traffic store
// files (timestamps masked) are compared.

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/rules"

	_ "modernc.org/sqlite"
)

const (
	trafficAgentToken = agentToken + "-traffic"
	oldAgentToken     = agentToken + "-traffic-old"
	dnsAgentToken     = agentToken + "-dns"
	editAgentToken    = agentToken + "-edit"
)

func seedTraffic(t *testing.T, dir string) {
	r, err := repo.OpenDefault(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err = r.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", append(system.GetList("agents"),
			jsonx.Obj("id", "agent-traffic", "name", "traffic agent", "token", trafficAgentToken,
				"profile_id", "default", "service_type", "mihomo", "host", "127.0.0.4", "port", 9, "version", "1.8.52-go",
				"config_version", "0", "enabled", true, "domain_discovery_enabled", true),
			jsonx.Obj("id", "agent-traffic-old", "name", "old traffic agent", "token", oldAgentToken,
				"profile_id", "other", "service_type", "mihomo", "host", "127.0.0.5", "port", 9, "version", "1.8.50-go",
				"config_version", "0", "enabled", true, "domain_discovery_enabled", true, "traffic_history_enabled", true,
				"traffic_history_since", "2026-11-01T00:00:00+00:00"),
			// A05 review: hand-edited traffic store file (see seedTrafficReview).
			jsonx.Obj("id", "agent-edit", "name", "edited store agent", "token", editAgentToken,
				"profile_id", "default", "service_type", "mihomo", "host", "127.0.0.7", "port", 9, "version", "1.8.52-go",
				"config_version", "0", "enabled", true, "domain_discovery_enabled", true),
			jsonx.Obj("id", "agent-dns", "name", "dns agent", "token", dnsAgentToken,
				"profile_id", "default", "service_type", "oxidns", "host", "127.0.0.6", "port", 9, "version", "1.8.52-go",
				"config_version", "0", "enabled", true, "domain_discovery_enabled", true, "traffic_history_enabled", true)))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Default discovery rulesets: one served from its cache file, one from
	// its content, one regional, one referenced only after MATCH.
	cached := jsonx.Obj("id", "rs-proxy", "name", "域名发现-代理", "source_type", "content", "behavior", "classical", "content", "DOMAIN,a.example")
	if _, err := r.UpdateSharedTransaction(ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("rule_library", append(shared.GetList("rule_library"),
			jsonx.Obj("id", "rs-direct", "name", "域名发现-直连", "source_type", "content", "behavior", "domain", "content", "+.example.com"),
			cached,
			jsonx.Obj("id", "rs-us", "name", "域名发现-🇺🇸 美国", "source_type", "content", "behavior", "domain", "content", "+.example.us"),
			jsonx.Obj("id", "rs-late", "name", "after match", "source_type", "content", "behavior", "domain", "content", "x")))
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.UpdateProfileTransaction(ctx, "default", func(_ context.Context, profile *jsonx.Object) (*jsonx.Object, error) {
		profile.Set("rule_configs", append([]any{
			jsonx.Obj("id", "r1", "itemType", "ruleset", "library_rule_id", "rs-direct", "policy", "DIRECT", "enabled", true),
			jsonx.Obj("id", "r2", "itemType", "ruleset", "library_rule_id", "rs-proxy", "policy", "PROXY", "enabled", true),
			jsonx.Obj("id", "r3", "itemType", "ruleset", "library_rule_id", "rs-us", "policy", "US", "enabled", true),
			jsonx.Obj("id", "r4", "itemType", "rule", "rule_type", "MATCH", "policy", "DIRECT", "enabled", true),
			jsonx.Obj("id", "r5", "itemType", "ruleset", "library_rule_id", "rs-late", "policy", "DIRECT", "enabled", true),
		}, profile.GetList("rule_configs")...))
		profile.Set("domain_discovery", jsonx.Obj("direct_ruleset", "rs-direct", "proxy_ruleset", "rs-proxy",
			"regional_rulesets", jsonx.Obj("US", "rs-us", "late", "rs-late", "gone", "rs-missing")))
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	rulesDir, _ := r.SharedRulesDir()
	cachePath := filepath.Join(rulesDir, rules.RuleCacheFilename(cached))
	os.MkdirAll(filepath.Dir(cachePath), 0o755)
	if err := os.WriteFile(cachePath, []byte("DOMAIN,a.example\nDOMAIN,b.example\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A saved traffic store file: an expired day and yesterday.
	now := time.Now()
	day := func(offset int) string { return now.AddDate(0, 0, offset).Format("2006-01-02") }
	stored := fmt.Sprintf(`{"status":{"value":"running","at":"%sT08:00:00","mihomo_version":"v1.18.0"},"days":{"%s":{"ip_only_conns":1,"dropped":0,"hosts":{}},"%s":{"ip_only_conns":2,"dropped":1,"hosts":{"old.example.com":{"first_seen":"%sT08:00:00","last_seen":"%sT08:00:00","by_route":{"Match\t\tdirect\t漏网之鱼":{"conns":3,"fails":1,"fail_kinds":{"reset":1},"zero_dl":0,"up":5,"down":6}},"ports":{"443":4}}}}}}`,
		day(-1), day(-10), day(-1), day(-1), day(-1))
	os.MkdirAll(filepath.Join(dir, "traffic"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, "traffic", "agent-traffic.json"), []byte(stored), 0o644); err != nil {
		t.Fatal(err)
	}

	// A05 review: a hand-edited store file — today's hosts is a list (kept
	// by a report without items, AttributeError with items), counters are a
	// float and a bool, the status is a str (AttributeError when the report
	// has no mihomo_version), an odd day key "zzz" is never pruned.
	edited := fmt.Sprintf(`{"status": "old", "days": {"%s": {"ip_only_conns": 1.5, "dropped": true, "hosts": []}, "zzz": {}, "%s": {}}}`, day(0), day(-6))
	if err := os.WriteFile(filepath.Join(dir, "traffic", "agent-edit.json"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	// A traffic history from 1.8.51-go (legacy tables) with a deleted Agent.
	os.MkdirAll(filepath.Join(dir, "metrics"), 0o755)
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: filepath.Join(dir, "metrics", "traffic_history.sqlite3")}).String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	hour := now.Unix() / 3600 * 3600
	for _, statement := range []string{
		"PRAGMA journal_mode=DELETE",
		"CREATE TABLE usage (agent_id TEXT, bucket INTEGER, span INTEGER, source TEXT, policy_group TEXT, outbound TEXT," +
			" up INTEGER, down INTEGER, conns INTEGER, PRIMARY KEY (agent_id, bucket, span, source, policy_group, outbound))",
		"CREATE TABLE targets (agent_id TEXT, bucket INTEGER, span INTEGER, source TEXT, target TEXT, outbound TEXT," +
			" up INTEGER, down INTEGER, conns INTEGER, PRIMARY KEY (agent_id, bucket, span, source, target, outbound))",
		fmt.Sprintf("INSERT INTO usage VALUES ('agent-traffic', %d, 3600, '10.0.0.2', 'Proxy', 'HK', 1, 10, 1), "+
			"('agent-traffic', %d, 3600, '10.0.0.2', 'DIRECT', 'DIRECT', 1, 5, 1), ('agent-traffic', %d, 3600, '', '', '', 0, 3, 0), "+
			"('agent-traffic', %d, 3600, '10.0.0.3', 'Proxy', 'JP', 2, 20, 2), ('gone', %d, 3600, '10.0.0.9', 'P', 'Q', 1, 1, 1)",
			hour-3600, hour-3600, hour-3600, hour-9*86400, hour-3600),
		// A05 review: chain tables already present next to the legacy ones,
		// with a REAL counter (SQLite turns an int64 overflow of
		// `up + excluded.up` into a REAL) and a counter near the int64 limit
		// that the next save overflows.
		"CREATE TABLE traffic_usage ( agent_id TEXT NOT NULL, bucket INTEGER NOT NULL, span INTEGER NOT NULL, source TEXT NOT NULL, chain TEXT NOT NULL," +
			" up INTEGER NOT NULL, down INTEGER NOT NULL, conns INTEGER NOT NULL, PRIMARY KEY (agent_id, bucket, span, source, chain)) WITHOUT ROWID",
		"CREATE TABLE traffic_targets ( agent_id TEXT NOT NULL, bucket INTEGER NOT NULL, span INTEGER NOT NULL, source TEXT NOT NULL, target TEXT NOT NULL," +
			" chain TEXT NOT NULL, up INTEGER NOT NULL, down INTEGER NOT NULL, conns INTEGER NOT NULL," +
			" PRIMARY KEY (agent_id, bucket, span, source, target, chain)) WITHOUT ROWID",
		fmt.Sprintf("INSERT INTO traffic_usage VALUES ('agent-traffic', %d, 3600, '10.0.0.8', 'G' || char(31) || 'N', 1.3835058055282164e19, 3, 1), "+
			"('agent-traffic', %d, 3600, '10.0.0.8', 'X', 9223372036854775000, 0, 0), ('agent-traffic', %d, 3600, '10.0.0.2', 'Proxy' || char(31) || 'HK', 7, 7, 7)",
			hour-2*3600, hour, hour-3600),
		fmt.Sprintf("INSERT INTO traffic_targets VALUES ('agent-traffic', %d, 3600, '10.0.0.8', 'big.example', 'G' || char(31) || 'N', 1.3835058055282164e19, 3, 1)",
			hour-2*3600),
		fmt.Sprintf("INSERT INTO targets VALUES ('agent-traffic', %d, 3600, '10.0.0.2', 'example.com', 'HK', 1, 10, 1), "+
			"('agent-traffic', %d, 3600, '10.0.0.3', 'example.net', 'JP', 2, 20, 2)", hour-3600, hour-3*86400),
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}

func trafficRequests() []request {
	J := "application/json"
	hour := time.Now().Unix() / 3600 * 3600
	auth := func(token string) map[string]string { return map[string]string{"Authorization": "Bearer " + token} }
	item := func(host string, extra string) string {
		return `{"host": "` + host + `", "port": 443, "network": "tcp", "rule": "Match", "rule_payload": "", "policy": "漏网之鱼", ` +
			`"outlet": "direct", "conns": 2, "fails": 1, "fail_kinds": {"timeout": 1}, "zero_dl": 0, "up": 10, "down": 20` + extra + `}`
	}
	report := func(status, version string, items ...string) string {
		return `{"schema": 1, "status": "` + status + `", "mihomo_version": ` + version + `, "window_start": "2026-10-10T10:00:00Z", ` +
			`"window_end": "2026-10-10T10:05:00Z", "ip_only_conns": 3, "dropped": 0, "items": [` + strings.Join(items, ", ") + `]}`
	}
	history := func(schema int, usage, domains string) string {
		return fmt.Sprintf(`{"schema": %d, "usage": [%s], "domains": [%s], "dropped": 0}`, schema, usage, domains)
	}
	usage := func(offset int64, source, chain string, down int) string {
		return fmt.Sprintf(`{"hour": %d, "source": "%s", "chain": %s, "up": 1, "down": %d, "conns": 1}`, hour+offset*3600, source, chain, down)
	}
	target := func(offset int64, source, domain, chain string, down int) string {
		return fmt.Sprintf(`{"hour": %d, "source": "%s", "domain": "%s", "chain": %s, "up": 2, "down": %d, "conns": 1}`, hour+offset*3600, source, domain, chain, down)
	}
	ai, direct := `["人工智能", "美国自建", "IEPL专线"]`, `["全球直连", "DIRECT"]`
	report1 := history(2, usage(0, "10.0.0.2", ai, 100)+", "+usage(0, "10.0.0.2", direct, 40)+", "+usage(-1, "", ai, 9)+", "+usage(-60, "10.0.0.2", ai, 1),
		target(0, "10.0.0.2", "www.Example.COM.", ai, 100)+", "+target(0, "10.0.0.2", "2001:DB8::1", direct, 40)+", "+target(0, "10.0.0.2", "", ai, 7))
	legacy := `{"schema": 1, "usage": [{"hour": ` + fmt.Sprint(hour) + `, "source": "10.0.0.7", "group": "Proxy", "outbound": "HK", "up": 1, "down": 2, "conns": 1}], ` +
		`"domains": [{"hour": ` + fmt.Sprint(hour) + `, "source": "10.0.0.7", "domain": "a.b.example.co.uk", "outbound": "HK", "up": 1, "down": 2, "conns": 1}]}`
	withoutRule := strings.Replace(item("norule.example.com", ""), `"rule": "Match", `, "", 1)
	stateMask := []string{"data.since", "data.start"}
	// Point times come from each server's own heartbeat (A04 history).
	trendMask := []string{"data.trend.*.timestamp"}
	statsMask := []string{"data.stats.start_time", "data.stats.end_time"}
	traffic := "/api/agents/agent-traffic/traffic-history"
	return append([]request{
		// traffic-report (domain discovery)
		{name: "traffic report no token", method: "POST", path: "/api/agents/agent-traffic/traffic-report", body: report("running", `"v1.19.0"`), contentType: J},
		{name: "traffic report wrong token", method: "POST", path: "/api/agents/agent-traffic/traffic-report", body: report("running", `"v1.19.0"`), contentType: J, headers: auth("wrong")},
		{name: "traffic report unknown agent", method: "POST", path: "/api/agents/missing/traffic-report", body: report("running", `"v1.19.0"`), contentType: J, headers: auth(trafficAgentToken)},
		{name: "traffic report oxidns agent", method: "POST", path: "/api/agents/agent-dns/traffic-report", body: report("running", `"v1.19.0"`), contentType: J, headers: auth(dnsAgentToken)},
		{name: "traffic report disabled", method: "POST", path: "/api/agents/agent-busy/traffic-report", body: report("running", `"v1.19.0"`), contentType: J, headers: auth(agentToken + "-busy")},
		{name: "traffic report", method: "POST", path: "/api/agents/agent-traffic/traffic-report", contentType: J, headers: auth(trafficAgentToken),
			body: report("running", `"v1.19.32"`, item("www.blocked.com", ""), item("Bad.COM", ""), item("api.example.io", `, "port": 80`))},
		{name: "traffic report merge", method: "POST", path: "/api/agents/agent-traffic/traffic-report", contentType: J, headers: auth(trafficAgentToken),
			body: report("unreachable", `null`, item("www.blocked.com", `, "port": 8443`), item("old.example.com", ""))},
		{name: "traffic report other profile", method: "POST", path: "/api/agents/agent-traffic-old/traffic-report", contentType: J, headers: auth(oldAgentToken),
			body: report("no_controller", `""`, item("x.example.org", ""))},
		{name: "traffic report bad schema", method: "POST", path: "/api/agents/agent-traffic/traffic-report", contentType: J, headers: auth(trafficAgentToken),
			body: strings.Replace(report("running", `"v"`), `"schema": 1`, `"schema": 9`, 1)},
		{name: "traffic report schema true", method: "POST", path: "/api/agents/agent-traffic/traffic-report", contentType: J, headers: auth(trafficAgentToken),
			body: strings.Replace(report("auth_failed", `"v2"`), `"schema": 1`, `"schema": true`, 1)},
		{name: "traffic report status list", method: "POST", path: "/api/agents/agent-traffic/traffic-report", contentType: J, headers: auth(trafficAgentToken),
			body: `{"schema": 1, "status": ["running"]}`},
		{name: "traffic report item without rule", method: "POST", path: "/api/agents/agent-traffic/traffic-report", contentType: J, headers: auth(trafficAgentToken),
			body: report("running", `"v3"`, withoutRule)},
		{name: "traffic report null", method: "POST", path: "/api/agents/agent-traffic/traffic-report", body: `null`, contentType: J, headers: auth(trafficAgentToken)},
		{name: "traffic report text/plain", method: "POST", path: "/api/agents/agent-traffic/traffic-report", body: `{}`, contentType: "text/plain", headers: auth(trafficAgentToken)},
		{name: "traffic report malformed", method: "POST", path: "/api/agents/agent-traffic/traffic-report", body: `{"schema":`, contentType: J, headers: auth(trafficAgentToken)},
		{name: "traffic report oversized", method: "POST", path: "/api/agents/agent-traffic/traffic-report", body: strings.Repeat(" ", 256*1024+1), contentType: J, headers: auth(trafficAgentToken)},

		// traffic history: Agent reports and the switch
		{name: "traffic history report disabled", method: "POST", path: traffic, body: report1, contentType: J, headers: auth(trafficAgentToken)},
		{name: "traffic history report no token", method: "POST", path: traffic, body: report1, contentType: J},
		{name: "traffic history report oxidns", method: "POST", path: "/api/agents/agent-dns/traffic-history", body: report1, contentType: J, headers: auth(dnsAgentToken)},
		{name: "traffic history switch no login", method: "PUT", path: traffic, body: `{"enabled": true}`, contentType: J},
		{name: "traffic history switch bad", method: "PUT", path: traffic, admin: true, body: `{"enabled": 1}`, contentType: J},
		{name: "traffic history switch bad clear", method: "PUT", path: traffic, admin: true, body: `{"enabled": false, "clear": "yes"}`, contentType: J},
		{name: "traffic history switch list", method: "PUT", path: traffic, admin: true, body: `[]`, contentType: J},
		{name: "traffic history switch oxidns", method: "PUT", path: "/api/agents/agent-dns/traffic-history", admin: true, body: `{"enabled": true}`, contentType: J},
		{name: "traffic history switch unknown", method: "PUT", path: "/api/agents/missing/traffic-history", admin: true, body: `{"enabled": true}`, contentType: J},
		{name: "traffic history switch on", method: "PUT", path: traffic, admin: true, body: `{"enabled": true}`, contentType: J, mask: stateMask},
		{name: "traffic history switch on again", method: "PUT", path: traffic, admin: true, body: `{"enabled": true}`, contentType: J, mask: stateMask},
		{name: "traffic history report", method: "POST", path: traffic, body: report1, contentType: J, headers: auth(trafficAgentToken)},
		{name: "traffic history report again", method: "POST", path: traffic, body: history(2, usage(0, "10.0.0.2", ai, 1), ""), contentType: J, headers: auth(trafficAgentToken)},
		{name: "traffic history report legacy", method: "POST", path: traffic, body: legacy, contentType: J, headers: auth(trafficAgentToken)},
		{name: "traffic history report bad", method: "POST", path: traffic, body: `{"schema": 2, "usage": "x", "domains": []}`, contentType: J, headers: auth(trafficAgentToken)},
		{name: "traffic history report bad hour", method: "POST", path: traffic, body: history(2, `{"hour": 1.5, "source": "s", "chain": [], "up": 1, "down": 1, "conns": 1}`, ""), contentType: J, headers: auth(trafficAgentToken)},
		{name: "traffic history report oversized", method: "POST", path: traffic, body: strings.Repeat(" ", 4*1024*1024+1), contentType: J, headers: auth(trafficAgentToken)},
		{name: "traffic history report null", method: "POST", path: traffic, body: `null`, contentType: J, headers: auth(trafficAgentToken)},

		// traffic history: queries
		{name: "traffic history query", method: "GET", path: traffic, admin: true, mask: stateMask},
		{name: "traffic history query no login", method: "GET", path: traffic},
		{name: "traffic history query unknown", method: "GET", path: "/api/agents/missing/traffic-history", admin: true},
		{name: "traffic history query oxidns", method: "GET", path: "/api/agents/agent-dns/traffic-history", admin: true},
		{name: "traffic history query bad range", method: "GET", path: traffic + "?range=1y", admin: true},
		{name: "traffic history query bad by", method: "GET", path: traffic + "?by=port", admin: true},
		{name: "traffic history query destination", method: "GET", path: traffic + "?by=destination", admin: true, mask: stateMask},
		{name: "traffic history query chain 7d", method: "GET", path: traffic + "?range=7d&by=chain", admin: true, mask: stateMask},
		{name: "traffic history query outbound", method: "GET", path: traffic + "?by=outbound", admin: true, mask: stateMask},
		{name: "traffic history query group 30d", method: "GET", path: traffic + "?range=30d&by=group", admin: true, mask: stateMask},
		{name: "traffic history query destination 30d", method: "GET", path: traffic + "?range=30d&by=destination", admin: true, mask: stateMask},
		{name: "traffic history query old agent", method: "GET", path: "/api/agents/agent-traffic-old/traffic-history?range=7d", admin: true, mask: stateMask},

		// retention
		{name: "traffic retention no login", method: "PUT", path: "/api/agents/traffic-history/retention", body: `{"days": 30}`, contentType: J},
		{name: "traffic retention bad", method: "PUT", path: "/api/agents/traffic-history/retention", admin: true, body: `{"days": 7}`, contentType: J},
		{name: "traffic retention string", method: "PUT", path: "/api/agents/traffic-history/retention", admin: true, body: `{"days": "90"}`, contentType: J},
		{name: "traffic retention bool", method: "PUT", path: "/api/agents/traffic-history/retention", admin: true, body: `{"days": true}`, contentType: J},
		{name: "traffic retention float", method: "PUT", path: "/api/agents/traffic-history/retention", admin: true, body: `{"days": 180.0}`, contentType: J},
		{name: "traffic retention list", method: "PUT", path: "/api/agents/traffic-history/retention", admin: true, body: `[180]`, contentType: J},
		{name: "traffic retention text/plain", method: "PUT", path: "/api/agents/traffic-history/retention", admin: true, body: `{"days": 180}`, contentType: "text/plain"},
		{name: "traffic retention", method: "PUT", path: "/api/agents/traffic-history/retention", admin: true, body: `{"days": 180}`, contentType: J},
		{name: "traffic history query after retention", method: "GET", path: traffic + "?range=24h&by=source", admin: true, mask: stateMask},

		// clear and switch off
		{name: "traffic history old report", method: "POST", path: "/api/agents/agent-traffic-old/traffic-history", body: report1, contentType: J, headers: auth(oldAgentToken)},
		{name: "traffic history clear no login", method: "DELETE", path: "/api/agents/agent-traffic-old/traffic-history"},
		{name: "traffic history clear unknown", method: "DELETE", path: "/api/agents/missing/traffic-history", admin: true},
		{name: "traffic history clear oxidns", method: "DELETE", path: "/api/agents/agent-dns/traffic-history", admin: true},
		{name: "traffic history clear", method: "DELETE", path: "/api/agents/agent-traffic-old/traffic-history", admin: true},
		{name: "traffic history old report again", method: "POST", path: "/api/agents/agent-traffic-old/traffic-history", body: report1, contentType: J, headers: auth(oldAgentToken)},
		{name: "traffic history old switch off clear", method: "PUT", path: "/api/agents/agent-traffic-old/traffic-history", admin: true, body: `{"enabled": false, "clear": true}`, contentType: J, mask: stateMask},
		{name: "traffic history old report off", method: "POST", path: "/api/agents/agent-traffic-old/traffic-history", body: report1, contentType: J, headers: auth(oldAgentToken)},
		{name: "traffic history old switch on", method: "PUT", path: "/api/agents/agent-traffic-old/traffic-history", admin: true, body: `{"enabled": true}`, contentType: J, mask: stateMask},
		{name: "traffic history old report on", method: "POST", path: "/api/agents/agent-traffic-old/traffic-history", body: report1, contentType: J, headers: auth(oldAgentToken)},
		// Deleting the Agent drops its traffic history and traffic store file.
		{name: "traffic agent delete", method: "DELETE", path: "/api/agents/agent-traffic-old", admin: true},

		// monitoring-history traffic stats and trend
		{name: "traffic stats", method: "GET", path: "/api/agents/agent-busy/traffic/stats", admin: true, mask: statsMask},
		{name: "traffic stats week", method: "GET", path: "/api/agents/agent-busy/traffic/stats?period=week", admin: true, mask: statsMask},
		{name: "traffic stats hours_24", method: "GET", path: "/api/agents/agent-busy/traffic/stats?period=hours_24", admin: true, mask: statsMask},
		{name: "traffic stats odd period", method: "GET", path: "/api/agents/agent-busy/traffic/stats?period=x", admin: true, mask: statsMask},
		{name: "traffic stats no history", method: "GET", path: "/api/agents/agent-traffic/traffic/stats?period=today", admin: true},
		{name: "traffic stats no login", method: "GET", path: "/api/agents/agent-busy/traffic/stats"},
		{name: "traffic stats unknown", method: "GET", path: "/api/agents/missing/traffic/stats", admin: true},
		{name: "traffic trend", method: "GET", path: "/api/agents/agent-busy/traffic/trend", admin: true, mask: trendMask},
		{name: "traffic trend 1h 1min", method: "GET", path: "/api/agents/agent-busy/traffic/trend?hours=1&interval=1", admin: true, mask: trendMask},
		{name: "traffic trend bad values", method: "GET", path: "/api/agents/agent-busy/traffic/trend?hours=x&interval=%D9%A3", admin: true, mask: trendMask},
		{name: "traffic trend zero interval", method: "GET", path: "/api/agents/agent-busy/traffic/trend?interval=0", admin: true, mask: trendMask},
		{name: "traffic trend overflow", method: "GET", path: "/api/agents/agent-busy/traffic/trend?hours=10000000000", admin: true},
		{name: "traffic trend no login", method: "GET", path: "/api/agents/agent-busy/traffic/trend"},
		{name: "traffic trend unknown", method: "GET", path: "/api/agents/missing/traffic/trend", admin: true},
	}, trafficReviewRequests(report, history, usage, target, auth)...)
}

// trafficReviewRequests are the cases added by the A05 review.
func trafficReviewRequests(report func(string, string, ...string) string, history func(int, string, string) string,
	usage func(int64, string, string, int) string, target func(int64, string, string, string, int) string,
	auth func(string) map[string]string) []request {
	J := "application/json"
	hour := time.Now().Unix() / 3600 * 3600
	traffic := "/api/agents/agent-traffic/traffic-history"
	stateMask := []string{"data.since", "data.start"}
	edit := "/api/agents/agent-edit/traffic-report"
	big := fmt.Sprintf(`{"hour": %d, "source": "10.0.0.8", "chain": ["X"], "up": 4611686018427387903, "down": 0, "conns": 0}`, hour)
	return []request{
		// REAL counters and an int beyond int64 (stored + pending) in every view.
		{name: "review traffic history query real", method: "GET", path: traffic, admin: true, mask: stateMask},
		{name: "review traffic history report big", method: "POST", path: traffic, body: history(2, big, ""), contentType: J, headers: auth(trafficAgentToken)},
		{name: "review traffic history query big source", method: "GET", path: traffic + "?by=source", admin: true, mask: stateMask},
		{name: "review traffic history query big destination", method: "GET", path: traffic + "?by=destination&range=7d", admin: true, mask: stateMask},
		{name: "review traffic history query big chain", method: "GET", path: traffic + "?by=chain&range=30d", admin: true, mask: stateMask},
		{name: "review traffic history query big outbound", method: "GET", path: traffic + "?by=outbound", admin: true, mask: stateMask},
		{name: "review traffic history query big group", method: "GET", path: traffic + "?by=group", admin: true, mask: stateMask},
		// IPv6 scope ids keep spaces and case; Unicode 16 letters are not
		// lowered by Python 3.11; final sigma.
		{name: "review traffic history report odd targets", method: "POST", path: traffic, contentType: J, headers: auth(trafficAgentToken),
			body: history(2, "", target(0, "10.0.0.2", `fe80::1%A B`, `["G"]`, 1)+", "+target(0, "10.0.0.2", `\ua7cb.Example.COM`, `["G"]`, 2)+", "+
				target(0, "10.0.0.2", `A\u03a3.gr`, `["G"]`, 3)+", "+target(0, "10.0.0.2", `\u2160\u03a3.example.org`, `["G"]`, 4))},
		{name: "review traffic history query odd targets", method: "GET", path: traffic + "?by=destination", admin: true, mask: stateMask},
		// Hand-edited traffic store file.
		{name: "review traffic report edited no items", method: "POST", path: edit, body: report("running", `"v1.19.1"`), contentType: J, headers: auth(editAgentToken)},
		{name: "review traffic report edited keep version", method: "POST", path: edit, body: report("unreachable", `null`), contentType: J, headers: auth(editAgentToken)},
		{name: "review traffic report edited items", method: "POST", path: edit, contentType: J, headers: auth(editAgentToken),
			body: report("running", `"v1.19.1"`, `{"host": "a.example.com", "outlet": "direct", "policy": "P", "rule": "Match"}`)},
	}
}

var trafficTimestamp = regexp.MustCompile(`"(at|first_seen|last_seen)":"[^"]*"`)

// compareTraffic compares, after the graceful stop, the traffic history
// database (schema, journal mode, rows) and the traffic store files
// (timestamps masked).
func compareTraffic(t *testing.T, seedDir, pyDir, goDir string) int {
	failed := 0
	dump := func(dir string) string {
		path := filepath.Join(dir, "metrics", "traffic_history.sqlite3")
		if _, err := os.Stat(path); err != nil {
			return "missing"
		}
		db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path}).String()+"?mode=ro")
		if err != nil {
			return err.Error()
		}
		defer db.Close()
		var out []string
		for _, statement := range []string{
			"SELECT type, name, tbl_name, sql FROM sqlite_master ORDER BY name",
			"PRAGMA journal_mode",
			"SELECT * FROM traffic_usage ORDER BY agent_id, bucket, span, source, chain",
			"SELECT * FROM traffic_targets ORDER BY agent_id, bucket, span, source, target, chain",
		} {
			rows, err := db.Query(statement)
			if err != nil {
				out = append(out, statement+": "+err.Error())
				continue
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
			}
			rows.Close()
		}
		return strings.Join(out, "\n")
	}
	if py, gor := dump(pyDir), dump(goDir); py != gor {
		failed++
		t.Errorf("DIFF traffic_history.sqlite3:\n  python %s\n  go     %s", py, gor)
	} else {
		t.Logf("same traffic_history.sqlite3 (%d lines)", strings.Count(py, "\n")+1)
	}
	files := func(dir string) map[string]string {
		out := map[string]string{}
		entries, _ := os.ReadDir(filepath.Join(dir, "traffic"))
		for _, entry := range entries {
			data, _ := os.ReadFile(filepath.Join(dir, "traffic", entry.Name()))
			out[entry.Name()] = trafficTimestamp.ReplaceAllString(string(data), `"$1":"<t>"`)
		}
		return out
	}
	pyFiles, goFiles := files(pyDir), files(goDir)
	var names []string
	for name := range pyFiles {
		names = append(names, name)
	}
	for name := range goFiles {
		if _, ok := pyFiles[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if name == "agent-edit.json" {
			if diffs := compareEditedTrafficFile(t, seedDir, pyFiles[name], goFiles[name]); len(diffs) > 0 {
				failed++
				t.Errorf("DIFF traffic/%s: %s", name, strings.Join(diffs, "; "))
			} else {
				t.Log("same traffic/agent-edit.json → bound native refusal preserves seed bytes")
			}
			continue
		}
		same := pyFiles[name] == goFiles[name]
		if name == "agent-traffic.json" {
			same = samePolicyJSON([]byte(pyFiles[name]), []byte(goFiles[name]))
		}
		if !same {
			failed++
			t.Errorf("DIFF traffic/%s:\n  python %s\n  go     %s", name, pyFiles[name], goFiles[name])
		} else {
			t.Logf("same traffic/%s (%d bytes)", name, len(pyFiles[name]))
		}
	}
	return failed
}
