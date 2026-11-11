package routes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	oxRankingLimit = 10
	// rows= asks for more: ConfigFlow keeps every name of a short period to add
	// periods up exactly (the core ranks at most 10,000 records per request).
	oxRankingMaxRows = 2000
	// Each confirmed block branch costs one top_qnames request per snapshot.
	oxRankingBlockLimit = 32
)

// Generated rule-group providers; renamed duplicates gain further _N suffixes.
var oxRankingRuleGroup = regexp.MustCompile(`^rules_(domain|ip)_[0-9]+(_[0-9]+)*$`)

type oxUpstreamLabel struct {
	PluginTag string `json:"plugin_tag"`
	Upstream  string `json:"upstream"`
	Address   string `json:"address"`
}

type oxRankingConfig struct {
	upstreams []oxUpstreamLabel
	retention map[string]*uint64
	// Per recorder, the quick matcher tags whose match proves a block answer.
	blocked map[string][]string
}

// These labels describe configured upstreams, never the winner inferred from a
// query trace. Match OxiDNS 1.6.1 forward::upstream_metric_names exactly.
func oxMonitorRankingConfig(plugins []interface{}) oxRankingConfig {
	result := oxRankingConfig{upstreams: []oxUpstreamLabel{}, retention: map[string]*uint64{}, blocked: map[string][]string{}}
	byTag := map[string]map[string]interface{}{}
	duplicate := map[string]bool{}
	for _, item := range plugins {
		plugin, _ := item.(map[string]interface{})
		tag, _ := plugin["tag"].(string)
		if byTag[tag] != nil {
			duplicate[tag] = true
		}
		byTag[tag] = plugin
		args, _ := plugin["args"].(map[string]interface{})
		if plugin["type"] == "query_recorder" {
			if value, exists := args["retention_days"]; !exists {
				days := uint64(7)
				result.retention[tag] = &days
			} else if days, ok := value.(int); ok && days > 0 {
				days := uint64(days)
				result.retention[tag] = &days
			}
		}
		if plugin["type"] != "forward" || !oxMonitorSafeTag(tag) {
			continue
		}
		upstreams, _ := args["upstreams"].([]interface{})
		names := map[string]bool{}
		for index, raw := range upstreams {
			upstream, _ := raw.(map[string]interface{})
			address, _ := upstream["addr"].(string)
			name, named := upstream["tag"].(string)
			if !named {
				name = address
			}
			if names[name] {
				name += "#" + strconv.Itoa(index)
			}
			names[name] = true
			if address == "" || !oxMonitorLiteral(address) || !oxMonitorLiteral(name) || len(address) > 2048 || len(name) > 2048 {
				continue
			}
			// Addresses may contain DoH credentials or query tokens. Only the
			// server location belongs in this display-only metadata.
			display := address
			if parsed, err := url.Parse(address); err == nil && parsed.Host != "" {
				parsed.User, parsed.RawQuery, parsed.Fragment = nil, "", ""
				display = parsed.String()
			}
			result.upstreams = append(result.upstreams, oxUpstreamLabel{tag, name, display})
		}
	}
	for tag := range duplicate {
		delete(byTag, tag)
	}
	main := byTag["sequence_main"]
	rules, _ := main["args"].([]interface{})
	if main["type"] != "sequence" || !oxRankingDirectEntry(plugins) {
		return result
	}
	// A quick matcher match proves a block only for a generated terminal
	// branch. Arbitrary matcher matches, NXDOMAIN and IPv4 suppression do not.
	recorders := []string{}
	for index, raw := range rules {
		rule, ok := raw.(map[string]interface{})
		if !ok {
			break
		}
		exec, _ := rule["exec"].(string)
		matches := oxRankingMatches(rule["matches"])
		matcher := ""
		if len(rule) == 2 && reflect.DeepEqual(matches, []string{"question $adguard_rules"}) && exec == "goto adguard_reject" && oxRankingAdguard(byTag) {
			matcher = "question"
		} else if len(rule) == 2 && len(matches) == 1 && oxRankingRuleBlock(byTag, matches[0], exec) {
			// Rule DNS actions: reject (refused/nxdomain/nodata) or blackhole.
			matcher = strings.Fields(matches[0])[0]
		}
		if matcher != "" {
			for _, recorder := range recorders {
				result.blocked[recorder] = append(result.blocked[recorder], fmt.Sprintf("qs.match.sequence_main.%d.0.%s", index, matcher))
			}
			continue
		}
		if len(rule) == 1 && matches == nil {
			if strings.HasPrefix(exec, "$") && byTag[strings.TrimPrefix(exec, "$")]["type"] == "query_recorder" {
				recorders = append(recorders, strings.TrimPrefix(exec, "$"))
				continue
			}
			if exec == "metrics_collector metrics" || exec == "prefer_ipv4" || exec == "$custom_hosts" && byTag["custom_hosts"]["type"] == "hosts" || exec == "drop_resp" {
				continue
			}
			// A jumped sequence's own wrappers complete before it returns
			// (OxiDNS 1.6.1 traces), so it cannot rewrite a later block answer.
			if target := strings.TrimPrefix(exec, "jump "); target != exec && target != "sequence_main" && byTag[target]["type"] == "sequence" {
				continue
			}
		}
		// Other conditional terminal branches only end the queries they take,
		// including the NODATA answer for blocked query types.
		if len(rule) == 2 && len(matches) > 0 && (oxRankingTerminal(byTag, exec) ||
			len(matches) == 1 && strings.HasPrefix(matches[0], "qtype ") && exec == "black_hole nodata short_circuit=true") {
			continue
		}
		// Unknown wrapping executors could replace the block response on the
		// return path. Do not infer a blocked ranking for custom chains.
		break
	}
	return result
}

func oxRankingAdguard(byTag map[string]map[string]interface{}) bool {
	reject := byTag["adguard_reject"]
	wantReject := []interface{}{map[string]interface{}{"exec": "$adguard_block"}, map[string]interface{}{"exec": "accept"}}
	return byTag["adguard_rules"]["type"] == "adguard_rule" && reject["type"] == "sequence" && reflect.DeepEqual(reject["args"], wantReject) && oxRankingBlackHole(byTag["adguard_block"], false)
}

// A main-chain black hole must short-circuit, or later entries would replace
// its answer. Mode null is the quoted string: a bare YAML null means nxdomain.
func oxRankingBlackHole(plugin map[string]interface{}, shortCircuit bool) bool {
	args, ok := plugin["args"].(map[string]interface{})
	mode, _ := args["mode"].(string)
	want := 1
	if shortCircuit {
		want = 2
		if args["short_circuit"] != true {
			return false
		}
	}
	return plugin["type"] == "black_hole" && ok && len(args) == want && (mode == "nxdomain" || mode == "nodata" || mode == "null" || mode == "refused")
}

// reject, reject 3 and reject 0 all answer and stop as the builtin "reject".
func oxRankingBlock(byTag map[string]map[string]interface{}, exec string) bool {
	return exec == "reject" || strings.HasPrefix(exec, "reject ") && len(strings.Fields(exec)) == 2 ||
		exec == "$cf_dns_blackhole" && oxRankingBlackHole(byTag["cf_dns_blackhole"], true)
}

func oxRankingTerminal(byTag map[string]map[string]interface{}, exec string) bool {
	return strings.HasPrefix(exec, "goto ") || exec == "accept" || oxRankingBlock(byTag, exec)
}

// Only generated rule groups count: qname for domain groups, resp_ip for IP
// groups after the local probe. Pinned upstreams and lists are not blocks.
func oxRankingRuleBlock(byTag map[string]map[string]interface{}, match, exec string) bool {
	fields := strings.Fields(match)
	if len(fields) != 2 || !strings.HasPrefix(fields[1], "$") || !oxRankingBlock(byTag, exec) {
		return false
	}
	group := oxRankingRuleGroup.FindStringSubmatch(strings.TrimPrefix(fields[1], "$"))
	return group != nil && byTag[group[0]] != nil && (group[1] == "domain" && fields[0] == "qname" || group[1] == "ip" && fields[0] == "resp_ip")
}

// An outer sequence/executor can replace the response after the main chain
// returns. Recognize only direct built-in server entries and conservatively
// decline custom references rather than interpreting arbitrary plugin config.
func oxRankingDirectEntry(plugins []interface{}) bool {
	servers := 0
	for _, item := range plugins {
		plugin, _ := item.(map[string]interface{})
		if plugin["tag"] == "sequence_main" {
			continue
		}
		args, _ := plugin["args"].(map[string]interface{})
		kind, _ := plugin["type"].(string)
		server := kind == "udp_server" || kind == "tcp_server" || kind == "http_server" || kind == "quic_server"
		if server && args["entry"] == "sequence_main" {
			servers++
			for key, value := range args {
				if key != "entry" && oxRankingMentionsMain(value) {
					return false
				}
			}
		} else if oxRankingMentionsMain(plugin["args"]) {
			return false
		}
	}
	return servers > 0
}

func oxRankingMentionsMain(value interface{}) bool {
	switch value := value.(type) {
	case string:
		return strings.Contains(value, "sequence_main")
	case []interface{}:
		for _, item := range value {
			if oxRankingMentionsMain(item) {
				return true
			}
		}
	case map[string]interface{}:
		for _, item := range value {
			if oxRankingMentionsMain(item) {
				return true
			}
		}
	}
	return false
}

func oxRankingMatches(raw interface{}) []string {
	if value, ok := raw.(string); ok {
		return []string{value}
	}
	if values, ok := raw.([]interface{}); ok {
		result := []string{}
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil
			}
			result = append(result, text)
		}
		return result
	}
	return nil
}

func oxMonitorParseRankings(raw string) (string, url.Values, *oxMonitorError) {
	values, err := url.ParseQuery(raw)
	if err != nil || len(raw) > 8192 || !values.Has("since_ms") {
		return "", nil, oxMonitorBadQuery()
	}
	rows := oxRankingLimit
	for key, items := range values {
		if len(items) != 1 || key != "recorder" && key != "since_ms" && key != "until_ms" && key != "rows" {
			return "", nil, oxMonitorBadQuery()
		}
		if key == "rows" {
			n, err := strconv.Atoi(items[0])
			if err != nil || n < 1 || n > oxRankingMaxRows || strconv.Itoa(n) != items[0] {
				return "", nil, oxMonitorBadQuery()
			}
			rows = n
			continue
		}
		if key != "recorder" {
			if _, err := strconv.ParseInt(items[0], 10, 64); err != nil || strings.HasPrefix(items[0], "-") {
				return "", nil, oxMonitorBadQuery()
			}
		}
	}
	values.Del("rows")
	recorder, query, queryErr := oxMonitorParseQuery(values.Encode(), false)
	if queryErr != nil {
		return "", nil, queryErr
	}
	query.Set("limit", strconv.Itoa(rows))
	return recorder, query, nil
}

type oxRankingRow struct {
	Key   string  `json:"key"`
	Count uint64  `json:"count"`
	Share float64 `json:"share"`
}

type oxRankingGroup struct {
	SampleSize uint64         `json:"sample_size"`
	Rows       []oxRankingRow `json:"rows"`
	AtLimit    bool           `json:"at_limit"`
}

type oxMonitorRuntime struct {
	RunningVersion string  `json:"running_version"`
	LastStarted    *uint64 `json:"last_started_ms"`
	LastCompleted  *uint64 `json:"last_completed_ms"`
	LastSuccess    *uint64 `json:"last_success_ms"`
}

func (state oxMonitorRuntime) revision() string {
	data, _ := json.Marshal(state)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func (config *oxMonitorConfig) runtimeSnapshot(ctx context.Context, client *http.Client) (*oxMonitorRuntime, *oxMonitorError) {
	data, err := config.fetch(ctx, client, "/api/reload/status", nil, 64<<10, false)
	if err != nil {
		return nil, err
	}
	var status struct {
		Status     string `json:"status"`
		Pending    *bool  `json:"pending"`
		InProgress *bool  `json:"in_progress"`
		oxMonitorRuntime
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &status) != nil || json.Unmarshal(data, &fields) != nil || status.Pending == nil || status.InProgress == nil || fields["last_started_ms"] == nil || fields["last_completed_ms"] == nil || fields["last_success_ms"] == nil {
		return nil, oxMonitorFail(http.StatusBadGateway, "INVALID_RESPONSE", "OxiDNS running configuration status is unavailable")
	}
	if *status.Pending || *status.InProgress || status.RunningVersion != config.sourceRevision || status.Status != "idle" && status.Status != "ok" && status.Status != "failed" {
		return nil, oxMonitorFail(http.StatusConflict, "SNAPSHOT_CHANGED", "The running OxiDNS configuration is changing or differs from the configuration file")
	}
	return &status.oxMonitorRuntime, nil
}

func oxDecodeRanking(data []byte, clients bool, rows int) (oxRankingGroup, *oxMonitorError) {
	bad := oxMonitorFail(http.StatusBadGateway, "INVALID_RESPONSE", "OxiDNS API returned invalid ranking statistics")
	var core struct {
		OK         bool            `json:"ok"`
		SampleSize *uint64         `json:"sample_size"`
		Rows       *[]oxRankingRow `json:"rows"`
	}
	if json.Unmarshal(data, &core) != nil || !core.OK || core.SampleSize == nil || core.Rows == nil || *core.SampleSize > oxMonitorClientSampleLimit || len(*core.Rows) > rows {
		return oxRankingGroup{}, bad
	}
	result := oxRankingGroup{*core.SampleSize, *core.Rows, *core.SampleSize == oxMonitorClientSampleLimit}
	seen := map[string]bool{}
	var clientTotal uint64
	for index := range result.Rows {
		row := &result.Rows[index]
		// DNS can contain multiple questions; domain counts count question
		// occurrences and can exceed the number of recorded requests.
		max := result.SampleSize * 65535
		if clients {
			max = result.SampleSize
		}
		if row.Key == "" || len(row.Key) > 1024 || !oxMonitorLiteral(row.Key) || seen[row.Key] || row.Count == 0 || row.Count > max || clients && net.ParseIP(row.Key) == nil {
			return oxRankingGroup{}, bad
		}
		seen[row.Key] = true
		if clients {
			clientTotal += row.Count
		}
		row.Share = float64(row.Count) / float64(result.SampleSize)
	}
	if clients && clientTotal > result.SampleSize {
		return oxRankingGroup{}, bad
	}
	return result, nil
}

// Block branches are terminal, so each query is counted under one matcher and
// samples add up. A total beyond one request's sample cap is reported at the
// cap; per-domain counts then cover more than the reported sample.
func oxMergeRankings(groups []oxRankingGroup, rows int) oxRankingGroup {
	if len(groups) == 1 {
		return groups[0]
	}
	result := oxRankingGroup{Rows: []oxRankingRow{}}
	counts := map[string]uint64{}
	for _, group := range groups {
		result.SampleSize += group.SampleSize
		for _, row := range group.Rows {
			counts[row.Key] += row.Count
		}
	}
	if result.SampleSize > oxMonitorClientSampleLimit {
		result.SampleSize = oxMonitorClientSampleLimit
	}
	result.AtLimit = result.SampleSize == oxMonitorClientSampleLimit
	for key, count := range counts {
		result.Rows = append(result.Rows, oxRankingRow{key, count, float64(count) / float64(result.SampleSize)})
	}
	sort.Slice(result.Rows, func(i, j int) bool {
		a, b := result.Rows[i], result.Rows[j]
		return a.Count > b.Count || a.Count == b.Count && a.Key < b.Key
	})
	if len(result.Rows) > rows {
		result.Rows = result.Rows[:rows]
	}
	return result
}

func (config *oxMonitorConfig) verifySnapshot(ctx context.Context, client *http.Client, cfg *Config, before []byte, running *oxMonitorRuntime) *oxMonitorError {
	afterRuntime, err := config.runtimeSnapshot(ctx, client)
	if err != nil {
		return err
	}
	changed := oxMonitorFail(http.StatusConflict, "SNAPSHOT_CHANGED", "OxiDNS changed while statistics were being collected; refresh the dashboard")
	if !reflect.DeepEqual(running, afterRuntime) {
		return changed
	}
	after, err := config.fetch(ctx, client, "/api/health", nil, oxMonitorJSONLimit, true)
	if err != nil {
		return err
	}
	if oxSourceInstance(before) == "" || oxSourceInstance(before) != oxSourceInstance(after) {
		return changed
	}
	file, openErr := os.Open(cfg.ConfigPath)
	if openErr != nil {
		return changed
	}
	defer file.Close()
	data, readErr := io.ReadAll(io.LimitReader(file, oxMonitorJSONLimit+1))
	if readErr != nil || len(data) > oxMonitorJSONLimit {
		return changed
	}
	revision := sha256.Sum256(data)
	if hex.EncodeToString(revision[:]) != config.sourceRevision {
		return changed
	}
	return nil
}

func (config *oxMonitorConfig) writeRankings(w http.ResponseWriter, ctx context.Context, client *http.Client, cfg *Config, recorder string, query url.Values) {
	if recorder == "" && len(config.capabilities.QueryRecorders) > 0 {
		recorder = config.capabilities.QueryRecorders[0]
	}
	rows, rowsErr := strconv.Atoi(query.Get("limit"))
	if rowsErr != nil || rows < 1 || rows > oxRankingMaxRows {
		rows = oxRankingLimit
	}
	tag, err := config.recorder(recorder)
	if err != nil {
		oxMonitorWriteError(w, err)
		return
	}
	health, err := config.fetch(ctx, client, "/api/health", nil, oxMonitorJSONLimit, true)
	if err != nil {
		oxMonitorWriteError(w, err)
		return
	}
	running, err := config.runtimeSnapshot(ctx, client)
	if err != nil {
		oxMonitorWriteError(w, err)
		return
	}
	collectedAt := time.Now().UnixMilli()
	if !query.Has("until_ms") {
		query.Set("until_ms", strconv.FormatInt(collectedAt, 10))
	}
	fetch := func(kind string, params url.Values) (oxRankingGroup, *oxMonitorError) {
		// Validate the bounded top response independently: the existing client
		// picker requests every bucket and requires their sum to equal sample.
		data, fetchErr := config.fetch(ctx, client, "/api/plugins/"+tag+"/stats/"+kind, params, oxMonitorJSONLimit, false)
		if fetchErr != nil {
			return oxRankingGroup{}, fetchErr
		}
		return oxDecodeRanking(data, kind == "top_clients", rows)
	}
	clients, err := fetch("top_clients", query)
	if err != nil {
		oxMonitorWriteError(w, err)
		return
	}
	domains, err := fetch("top_qnames", query)
	if err != nil {
		oxMonitorWriteError(w, err)
		return
	}
	blocked := map[string]interface{}{"available": false, "reason": "当前配置未包含可确认的标准 AdGuard 拦截路径", "sample_size": 0, "rows": []oxRankingRow{}, "at_limit": false}
	matchers := config.rankings.blocked[tag]
	if len(matchers) > oxRankingBlockLimit {
		blocked["reason"] = "too_many_block_rules"
	} else if len(matchers) > 0 {
		groups := []oxRankingGroup{}
		adguard, rules := false, false
		for _, matcher := range matchers {
			// The core filters by one matcher tag per request.
			filtered := url.Values{}
			for key, items := range query {
				filtered[key] = append([]string{}, items...)
			}
			filtered.Set("matcher_tag", matcher)
			filtered.Set("status", "has_response")
			group, fetchErr := fetch("top_qnames", filtered)
			if fetchErr != nil {
				oxMonitorWriteError(w, fetchErr)
				return
			}
			groups = append(groups, group)
			adguard = adguard || strings.HasSuffix(matcher, ".question")
			rules = rules || !strings.HasSuffix(matcher, ".question")
		}
		reason := "rule_dns"
		if adguard && rules {
			reason = "standard_adguard_rule_dns"
		} else if adguard {
			reason = "standard_adguard"
		}
		group := oxMergeRankings(groups, rows)
		blocked = map[string]interface{}{"available": true, "reason": reason, "matcher_tags": matchers, "sample_size": group.SampleSize, "rows": group.Rows, "at_limit": group.AtLimit}
		if len(matchers) == 1 {
			blocked["matcher_tag"] = matchers[0]
		}
	}
	if err := config.verifySnapshot(ctx, client, cfg, health, running); err != nil {
		oxMonitorWriteError(w, err)
		return
	}
	JsonResponse(w, http.StatusOK, map[string]interface{}{"success": true, "recorder": tag, "collected_at_ms": collectedAt, "source_revision": config.sourceRevision, "runtime_revision": running.revision(), "instance_id": oxSourceInstance(health), "retention_days": config.rankings.retention[tag], "sample_limit": oxMonitorClientSampleLimit, "rows_limit": rows, "clients": clients, "domains": domains, "blocked": blocked})
}
