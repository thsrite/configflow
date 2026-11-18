package health

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/pystr"
	"configflow/internal/repo"
	"configflow/internal/weburl"
)

const (
	// LatencyFile is the shared JSON filename persisting node latency results.
	LatencyFile = "node_latency.json"
	// Method identifies the current standalone worker probe format.
	Method = "proxy_http_v2"
	// HistorySize is the maximum number of latency samples retained per node.
	HistorySize = 14
	// MaxNodes is the maximum number of nodes allowed in a single test batch.
	MaxNodes = 20
	// MaxWorkers is the worker concurrency per probe batch.
	MaxWorkers = MaxNodes
	// MaxConversionWorker bounds concurrent Sub-Store conversions per test run.
	MaxConversionWorker = 5
	checkTimeout        = 10 * time.Second
)

var (
	latencyLog = logx.Get("backend.utils.node_latency")
	testLock   sync.Mutex
)

// LockForTest acquires the process-wide latency test mutex for test isolation
// and returns a release callback.
func LockForTest() (release func()) {
	testLock.Lock()
	return testLock.Unlock
}

// ProbeSettings holds the resolved URL, timeout, DNS, and delay settings for a probe batch.
type ProbeSettings struct {
	URL          string   `json:"url"`
	TimeoutMS    int64    `json:"timeout_ms"`
	DNSServers   []string `json:"dns_servers"`
	DNSBootstrap []string `json:"dns_bootstrap"`
	UnifiedDelay bool     `json:"unified_delay"`
}

// ResolveDNSSettings returns the active node-test DNS server list.
func ResolveDNSSettings(ctx context.Context) ([]string, error) {
	effective, err := EffectiveDNSSettings(ctx, nil)
	if err != nil {
		return nil, err
	}
	return effective.DNSServers, nil
}

func isValueError(err error) bool {
	var ve *ValueError
	if errors.As(err, &ve) {
		return true
	}
	return repo.IsValueError(err)
}

// ResolveSettings resolves ProbeSettings from environment variables and saved DNS config.
func ResolveSettings(ctx context.Context) (ProbeSettings, error) {
	env := CurrentProbeEnv()
	invalid := &ProbeUnavailable{Message: msgProbeInvalidSettings}
	url := pystr.Strip(env.getenv("CONFIGFLOW_NODE_TEST_URL", "https://www.gstatic.com/generate_204"))
	parsed, err := weburl.Parse(url)
	if err != nil {
		return ProbeSettings{}, invalid
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || weburl.HasCredentials(parsed) {
		return ProbeSettings{}, invalid
	}
	if _, _, err := weburl.Port(parsed); err != nil {
		return ProbeSettings{}, invalid
	}
	timeout, err := pystr.Int(env.getenv("CONFIGFLOW_NODE_TEST_TIMEOUT_MS", "5000"), 10)
	if err != nil || !timeout.IsInt64() || timeout.Int64() < 500 || timeout.Int64() > 10000 {
		return ProbeSettings{}, invalid
	}
	dns, err := EffectiveDNSSettings(ctx, nil)
	if err != nil {
		if isValueError(err) {
			return ProbeSettings{}, &ProbeUnavailable{Message: err.Error()}
		}
		return ProbeSettings{}, err
	}
	unified := pystr.Lower(env.getenv("CONFIGFLOW_NODE_TEST_UNIFIED_DELAY", "1"))
	return ProbeSettings{
		URL:          url,
		TimeoutMS:    timeout.Int64(),
		DNSServers:   dns.DNSServers,
		DNSBootstrap: dns.DNSBootstrap,
		UnifiedDelay: unified != "0" && unified != "false",
	}, nil
}

// CandidateNode represents an enabled manual or subscription node eligible for probing.
type CandidateNode struct {
	Key         string
	Name        string
	ProxyString string
	Raw         json.RawMessage
}

type candidateSet struct {
	list  []CandidateNode
	byKey map[string]CandidateNode
}

func (c *candidateSet) add(node CandidateNode) {
	if _, seen := c.byKey[node.Key]; seen {
		return
	}
	c.byKey[node.Key] = node
	c.list = append(c.list, node)
}

func (c *candidateSet) has(key string) bool {
	_, ok := c.byKey[key]
	return ok
}

func hasFieldIssue(issues []model.FieldIssue, field string) bool {
	for _, issue := range issues {
		if issue.Path == field {
			return true
		}
	}
	return false
}

// CollectCandidateNodes gathers enabled manual and cached subscription nodes
// keyed by canonical JSON source identifiers.
func CollectCandidateNodes(ctx context.Context) ([]CandidateNode, error) {
	set, err := collectCandidateSet(ctx)
	if err != nil {
		return nil, err
	}
	return set.list, nil
}

func collectCandidateSet(ctx context.Context) (*candidateSet, error) {
	env := CurrentProbeEnv()
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	shared, err := repository.Shared(ctx)
	if err != nil {
		return nil, err
	}
	out := &candidateSet{byKey: map[string]CandidateNode{}}

	for i := range shared.Nodes {
		node := &shared.Nodes[i]
		issues := model.Issues(node)
		rawNode, err := model.Marshal(node)
		if err != nil {
			return nil, fmt.Errorf("marshal shared node: %w", err)
		}
		var nodeObj rawObject
		if err := json.Unmarshal(rawNode, &nodeObj); err != nil {
			return nil, fmt.Errorf("parse shared node: %w", err)
		}
		if hasFieldIssue(issues, "id") {
			rawID, _ := nodeObj.Fields.Get("id")
			return nil, valueError(fmt.Sprintf(msgStoredNodeIDInvalid, compactRaw(rawID)))
		}
		if node.ID == "" {
			continue
		}
		if node.Enabled != nil && !*node.Enabled {
			continue
		}
		name := node.Name
		if hasFieldIssue(issues, "name") {
			rawName, _ := nodeObj.Fields.Get("name")
			decodedName, err := decodeNonNullText(bytes.TrimSpace(rawName))
			if err != nil {
				return nil, valueError(fmt.Sprintf(msgStoredNodeNameInvalid, node.ID, compactRaw(rawName)))
			}
			name = decodedName
			encodedName, err := jsonresp.Encode(name)
			if err == nil {
				_ = nodeObj.Fields.Set("name", encodedName)
				if updatedRaw, err := nodeObj.MarshalJSON(); err == nil {
					rawNode = updatedRaw
				}
			}
		}
		if name == "" {
			continue
		}
		keyBytes, err := jsonresp.Encode([]string{"node", node.ID})
		if err != nil {
			return nil, fmt.Errorf("encode manual node key: %w", err)
		}
		var proxyStr string
		if node.ProxyString != nil {
			proxyStr = *node.ProxyString
		}
		out.add(CandidateNode{
			Key:         string(keyBytes),
			Name:        name,
			ProxyString: proxyStr,
			Raw:         rawNode,
		})
	}

	for i := range shared.Subscriptions {
		sub := &shared.Subscriptions[i]
		if sub.Enabled != nil && !*sub.Enabled {
			continue
		}
		issues := model.Issues(sub)
		if hasFieldIssue(issues, "id") {
			rawSub, err := model.Marshal(sub)
			if err != nil {
				return nil, fmt.Errorf("marshal subscription: %w", err)
			}
			var subObj rawObject
			_ = json.Unmarshal(rawSub, &subObj)
			rawID, _ := subObj.Fields.Get("id")
			return nil, valueError(fmt.Sprintf(msgStoredSubIDInvalid, compactRaw(rawID)))
		}
		if sub.ID == "" {
			continue
		}
		if env.Collaborators.LoadSubscriptionCache == nil {
			continue
		}
		cacheRaw, err := env.Collaborators.LoadSubscriptionCache(ctx, sub.ID)
		if err != nil {
			return nil, err
		}
		var cachedNodes []json.RawMessage
		trimmedCache := bytes.TrimSpace(cacheRaw)
		if len(trimmedCache) > 0 && !bytes.Equal(trimmedCache, []byte("null")) {
			var cacheObj rawObject
			if err := json.Unmarshal(trimmedCache, &cacheObj); err == nil {
				if rawNodes, ok := cacheObj.Fields.Get("nodes"); ok {
					trimmedNodes := bytes.TrimSpace(rawNodes)
					if !bytes.Equal(trimmedNodes, []byte("null")) && !bytes.Equal(trimmedNodes, []byte("false")) && !bytes.Equal(trimmedNodes, []byte("0")) {
						if len(trimmedNodes) == 0 || trimmedNodes[0] != '[' {
							return nil, valueError(fmt.Sprintf(msgCachedNodesMustBeArray, sub.ID, compactRaw(rawNodes)))
						}
						if err := json.Unmarshal(trimmedNodes, &cachedNodes); err != nil {
							return nil, valueError(fmt.Sprintf(msgCachedNodesMustBeArray, sub.ID, compactRaw(rawNodes)))
						}
					}
				}
			}
		}
		filtered := cachedNodes
		if env.Collaborators.FilterSubscriptionNodes != nil {
			filtered, err = env.Collaborators.FilterSubscriptionNodes(*sub, cachedNodes)
			if err != nil {
				return nil, err
			}
		}
		for _, itemRaw := range filtered {
			var itemObj rawObject
			if err := json.Unmarshal(itemRaw, &itemObj); err != nil {
				continue
			}
			if rawEnabled, ok := itemObj.Fields.Get("enabled"); ok && bytes.Equal(bytes.TrimSpace(rawEnabled), []byte("false")) {
				continue
			}
			rawName, ok := itemObj.Fields.Get("name")
			if !ok || bytes.Equal(bytes.TrimSpace(rawName), []byte("null")) {
				continue
			}
			name, err := decodeNonNullText(bytes.TrimSpace(rawName))
			if err != nil {
				return nil, valueError(fmt.Sprintf(msgCachedNodeNameInvalid, sub.ID, compactRaw(rawName)))
			}
			if name == "" {
				continue
			}
			if rawName[0] != '"' {
				if encodedName, err := jsonresp.Encode(name); err == nil {
					_ = itemObj.Fields.Set("name", encodedName)
					if updatedItem, err := itemObj.MarshalJSON(); err == nil {
						itemRaw = updatedItem
					}
				}
			}
			var proxyStr strictString
			if rawPS, ok := itemObj.Fields.Get("proxy_string"); ok {
				_ = json.Unmarshal(rawPS, &proxyStr)
			}
			keyBytes, err := jsonresp.Encode([]string{"subscription", sub.ID, name})
			if err != nil {
				return nil, fmt.Errorf("encode subscription node key: %w", err)
			}
			out.add(CandidateNode{
				Key:         string(keyBytes),
				Name:        name,
				ProxyString: string(proxyStr),
				Raw:         itemRaw,
			})
		}
	}
	return out, nil
}

func needsRemoteConversion(env *ProbeEnv, node CandidateNode) bool {
	if node.ProxyString == "" || env.Collaborators.NeedsRemoteConversion == nil {
		return false
	}
	return env.Collaborators.NeedsRemoteConversion(node.ProxyString)
}

// LatencyEntry holds a stored or freshly measured node latency record,
// retaining worker extension fields and member order.
type LatencyEntry struct {
	Fields model.Fields
}

// UnmarshalJSON decodes LatencyEntry preserving member order and raw bytes.
func (e *LatencyEntry) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &e.Fields)
}

// MarshalJSON encodes LatencyEntry preserving member order and raw bytes.
func (e LatencyEntry) MarshalJSON() ([]byte, error) {
	return e.Fields.MarshalJSON()
}

func (e LatencyEntry) method() string {
	raw, ok := e.Fields.Get("method")
	if !ok {
		return ""
	}
	var m strictString
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	return string(m)
}

func (e LatencyEntry) sameTestSettings(settings ProbeSettings) bool {
	rawURL, ok := e.Fields.Get("url")
	if !ok {
		return false
	}
	var url strictString
	if json.Unmarshal(rawURL, &url) != nil || string(url) != settings.URL {
		return false
	}
	rawUnified, ok := e.Fields.Get("unified_delay")
	if !ok {
		return false
	}
	trimmedUnified := bytes.TrimSpace(rawUnified)
	var unified bool
	switch {
	case bytes.Equal(trimmedUnified, []byte("true")):
		unified = true
	case bytes.Equal(trimmedUnified, []byte("false")):
		unified = false
	default:
		return false
	}
	if unified != settings.UnifiedDelay {
		return false
	}
	servers, ok := e.stringListOrEmpty("dns_servers")
	if !ok || !slices.Equal(servers, settings.DNSServers) {
		return false
	}
	bootstrap, ok := e.stringListOrEmpty("dns_bootstrap")
	if !ok || !slices.Equal(bootstrap, settings.DNSBootstrap) {
		return false
	}
	return true
}

func (e LatencyEntry) stringListOrEmpty(field string) ([]string, bool) {
	raw, ok := e.Fields.Get(field)
	if !ok {
		return []string{}, true
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, false
	}
	var items []strictString
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return nil, false
	}
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = string(item)
	}
	return out, true
}

func (e LatencyEntry) history() []json.RawMessage {
	raw, ok := e.Fields.Get("history")
	if !ok {
		return nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return nil
	}
	return items
}

func runProbeFields(ctx context.Context, proxies []model.Fields, options model.Fields) (model.Map[LatencyEntry], error) {
	var empty model.Map[LatencyEntry]
	env := CurrentProbeEnv()
	binary, err := env.probeBinary()
	if err != nil {
		return empty, err
	}
	proxiesJSON, err := jsonresp.EncodeUTF8(proxies)
	if err != nil {
		return empty, err
	}
	payload := options
	if err := payload.Set("nodes", proxiesJSON); err != nil {
		return empty, err
	}
	data, err := jsonresp.EncodeUTF8(payload)
	if err != nil {
		return empty, err
	}
	if len(data) > 8*1024*1024 {
		return empty, valueError(msgBatchPayloadTooLarge)
	}
	var timeoutMS float64
	if rawTimeout, ok := options.Get("timeout_ms"); ok {
		_ = json.Unmarshal(rawTimeout, &timeoutMS)
	}
	batches := float64((len(proxies) + MaxWorkers - 1) / MaxWorkers)
	budget := batches*timeoutMS/1000 + 5
	process, err := env.Runner.Run(ctx, Command{
		Argv:    []string{binary},
		Stdin:   data,
		Timeout: time.Duration(budget * float64(time.Second)),
		Env:     env.workerEnv(),
	})
	if errors.Is(err, ErrTimeout) {
		return empty, &ProbeUnavailable{Message: msgProbeTimeout}
	}
	if err != nil {
		return empty, &ProbeUnavailable{Message: msgProbeStartFailed}
	}
	stdout, err := decodeText(process.Stdout)
	if err != nil {
		return empty, err
	}
	if _, err := decodeText(process.Stderr); err != nil {
		return empty, err
	}
	if process.ReturnCode == 3 {
		return empty, &ProbeUnavailable{Message: msgProbeBadDNS}
	}
	if process.ReturnCode != 0 {
		return empty, &ProbeUnavailable{Message: msgProbeExitFailed}
	}
	results, err := parseProbeResults(stdout, proxies)
	if err != nil {
		var unavailable *ProbeUnavailable
		if errors.As(err, &unavailable) {
			return empty, err
		}
		return empty, &ProbeUnavailable{Message: msgProbeInvalidOutput}
	}
	return results, nil
}

func isTruthyWorkerError(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("false")) {
		return false
	}
	if trimmed[0] == '"' {
		var s string
		return json.Unmarshal(trimmed, &s) == nil && s != ""
	}
	if trimmed[0] == '[' || trimmed[0] == '{' {
		return len(trimmed) > 2
	}
	var n float64
	if json.Unmarshal(trimmed, &n) == nil {
		return n != 0
	}
	return false
}

func uniqueProxyNames(proxies []model.Fields) []string {
	var names []string
	seen := map[string]bool{}
	for _, proxy := range proxies {
		rawName, ok := proxy.Get("name")
		if !ok {
			continue
		}
		var name strictString
		if json.Unmarshal(rawName, &name) != nil {
			continue
		}
		if !seen[string(name)] {
			seen[string(name)] = true
			names = append(names, string(name))
		}
	}
	return names
}

func parseProbeResults(stdout string, proxies []model.Fields) (model.Map[LatencyEntry], error) {
	var results model.Map[LatencyEntry]
	var top rawObject
	if err := json.Unmarshal([]byte(stdout), &top); err != nil {
		return results, err
	}
	if err := checkWorkerVersion(top); err != nil {
		return results, err
	}
	rawResults, ok := top.Fields.Get("results")
	if !ok || len(bytes.TrimSpace(rawResults)) == 0 || bytes.TrimSpace(rawResults)[0] != '[' {
		return results, errors.New("invalid results list")
	}
	var list []rawObject
	if err := json.Unmarshal(rawResults, &list); err != nil {
		return results, err
	}
	for _, entry := range list {
		rawName, ok := entry.Fields.Get("name")
		if !ok {
			return results, errors.New("missing result name")
		}
		var name strictString
		if err := json.Unmarshal(rawName, &name); err != nil {
			return results, errors.New("non-string result name")
		}
		results.Set(string(name), LatencyEntry{Fields: entry.Fields})
	}
	expectedNames := uniqueProxyNames(proxies)
	if results.Len() != len(expectedNames) {
		return results, errors.New("result count mismatch")
	}
	for _, name := range expectedNames {
		if _, found := results.Get(name); !found {
			return results, errors.New("missing expected proxy result")
		}
	}
	for _, entry := range results.All() {
		rawLatency, ok := entry.Fields.Get("latency")
		if !ok {
			return results, errors.New("missing latency")
		}
		trimmedLatency := bytes.TrimSpace(rawLatency)
		if bytes.Equal(trimmedLatency, []byte("null")) {
			rawErr, hasErr := entry.Fields.Get("error")
			if !hasErr || !isTruthyWorkerError(rawErr) {
				return results, errors.New("null latency without error")
			}
			continue
		}
		if _, valid := parsePositiveJSONInt(trimmedLatency); !valid {
			return results, errors.New("invalid positive latency")
		}
	}
	return results, nil
}

// LoadLatencyResults reads persisted node latency entries of the current probe Method.
func LoadLatencyResults(ctx context.Context) model.Map[LatencyEntry] {
	var out model.Map[LatencyEntry]
	env := CurrentProbeEnv()
	if env.Collaborators.ReadLatencyFile == nil {
		return out
	}
	raw, ok := env.Collaborators.ReadLatencyFile(ctx)
	if !ok {
		return out
	}
	var top rawObject
	if err := json.Unmarshal(raw, &top); err != nil {
		return out
	}
	for name, rawEntry := range top.Fields.All() {
		var entry LatencyEntry
		if err := json.Unmarshal(rawEntry, &entry); err != nil {
			continue
		}
		if entry.method() == Method {
			out.Set(name, entry)
		}
	}
	return out
}

func saveLatencyResults(ctx context.Context, results model.Map[LatencyEntry]) {
	env := CurrentProbeEnv()
	if env.Collaborators.WriteLatencyFile == nil {
		latencyLog.Error(msgSaveLatencyFailed)
		return
	}
	encoded, err := jsonresp.Encode(results)
	if err != nil {
		latencyLog.Error(msgSaveLatencyFailed)
		return
	}
	if err := env.Collaborators.WriteLatencyFile(ctx, encoded); err != nil {
		latencyLog.Error(msgSaveLatencyFailed)
	}
}

// TestOutcome is the API result of a RunLatencyTests execution.
type TestOutcome struct {
	TestedAt string                  `json:"tested_at"`
	Results  model.Map[LatencyEntry] `json:"results"`
	Missing  []string                `json:"missing"`
	Settings ProbeSettings           `json:"settings"`
}

// RunLatencyTests executes an exclusive node latency probe for names or keys
// (nil for both tests all enabled candidates).
func RunLatencyTests(ctx context.Context, names, keys []string) (TestOutcome, error) {
	ctx = lifecycle.Detach(ctx)
	if !testLock.TryLock() {
		return TestOutcome{}, &ProbeBusy{Message: msgProbeBusy}
	}
	defer testLock.Unlock()
	return runLatencyTestsLocked(ctx, names, keys)
}

func dedupeStrings(items []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

func buildFailedMeasurement() LatencyEntry {
	var fields model.Fields
	_ = fields.Set("latency", json.RawMessage("null"))
	_ = fields.Set("error_code", json.RawMessage(`"config"`))
	msgJSON, _ := jsonresp.Encode(msgNodeConversionFailed)
	_ = fields.Set("error", msgJSON)
	return LatencyEntry{Fields: fields}
}

func runLatencyTestsLocked(ctx context.Context, names, keys []string) (TestOutcome, error) {
	env := CurrentProbeEnv()
	candidates, err := collectCandidateSet(ctx)
	if err != nil {
		return TestOutcome{}, err
	}
	if names != nil && keys != nil {
		return TestOutcome{}, valueError(msgNamesAndKeysConflict)
	}
	var requestedIDs []string
	switch {
	case names != nil:
		requestedIDs = names
	case keys != nil:
		requestedIDs = keys
	default:
		requestedIDs = make([]string, len(candidates.list))
		for i, c := range candidates.list {
			requestedIDs[i] = c.Key
		}
	}
	requested := dedupeStrings(requestedIDs)
	if len(requested) > MaxNodes {
		return TestOutcome{}, valueError(fmt.Sprintf(msgMaxNodesExceeded, MaxNodes))
	}

	var identityIDs []string
	identity := map[string]string{}
	if names != nil {
		byName := map[string][]string{}
		for _, c := range candidates.list {
			byName[c.Name] = append(byName[c.Name], c.Key)
		}
		for _, name := range requested {
			if len(byName[name]) > 1 {
				return TestOutcome{}, valueError(msgDuplicateNodeName)
			}
		}
		for _, name := range requested {
			if found := byName[name]; len(found) > 0 {
				identityIDs = append(identityIDs, name)
				identity[name] = found[0]
			}
		}
	} else {
		for _, key := range requested {
			if candidates.has(key) {
				identityIDs = append(identityIDs, key)
				identity[key] = key
			}
		}
	}

	wanted := make([]string, len(identityIDs))
	for i, id := range identityIDs {
		wanted[i] = identity[id]
	}
	remote := 0
	for _, key := range wanted {
		if needsRemoteConversion(env, candidates.byKey[key]) {
			remote++
		}
	}
	if remote > MaxConversionWorker {
		return TestOutcome{}, valueError(fmt.Sprintf(msgMaxRemoteConversions, MaxConversionWorker))
	}

	settings, err := ResolveSettings(ctx)
	if err != nil {
		return TestOutcome{}, err
	}
	settingsBytes, err := jsonresp.Encode(settings)
	if err != nil {
		return TestOutcome{}, err
	}
	var settingsFields model.Fields
	if err := json.Unmarshal(settingsBytes, &settingsFields); err != nil {
		return TestOutcome{}, err
	}

	var measured model.Map[LatencyEntry]
	if len(wanted) > 0 {
		if _, err := env.probeBinary(); err != nil {
			return TestOutcome{}, err
		}
		type convResult struct {
			proxy model.Fields
			ok    bool
		}
		converted := make([]convResult, len(wanted))
		workers := MaxConversionWorker
		if len(wanted) < workers {
			workers = len(wanted)
		}
		jobs := make(chan int)
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					if recovered := recover(); recovered != nil {
						latencyLog.Errorf("node conversion worker recovered panic: %v", recovered)
					}
				}()
				for idx := range jobs {
					if env.Collaborators.ConvertNode != nil {
						proxy, ok := env.Collaborators.ConvertNode(ctx, candidates.byKey[wanted[idx]].Raw)
						converted[idx] = convResult{proxy: proxy, ok: ok}
					}
				}
			}()
		}
		for i := range wanted {
			jobs <- i
		}
		close(jobs)
		wg.Wait()

		var proxies []model.Fields
		for i, key := range wanted {
			if converted[i].ok {
				proxy := converted[i].proxy
				keyJSON, err := jsonresp.Encode(key)
				if err != nil {
					return TestOutcome{}, err
				}
				if err := proxy.Set("name", keyJSON); err != nil {
					return TestOutcome{}, err
				}
				proxies = append(proxies, proxy)
			} else {
				measured.Set(key, buildFailedMeasurement())
			}
		}
		if len(proxies) > 0 {
			batchResults, err := runProbeFields(ctx, proxies, settingsFields)
			if err != nil {
				return TestOutcome{}, err
			}
			for key, entry := range batchResults.All() {
				measured.Set(key, entry)
			}
		}
	}

	stamp := isoUTC(env.Now())
	stampJSON, err := jsonresp.Encode(stamp)
	if err != nil {
		return TestOutcome{}, err
	}
	methodJSON, err := jsonresp.Encode(Method)
	if err != nil {
		return TestOutcome{}, err
	}

	stored := LoadLatencyResults(ctx)
	for key, m := range measured.All() {
		prev, hasPrev := stored.Get(key)
		history := []json.RawMessage{}
		if hasPrev && prev.sameTestSettings(settings) {
			previous := prev.history()
			if len(previous) > HistorySize-1 {
				previous = previous[len(previous)-(HistorySize-1):]
			}
			history = append(history, previous...)
		}
		latencyRaw, ok := m.Fields.Get("latency")
		if !ok {
			latencyRaw = json.RawMessage("null")
		}
		history = append(history, latencyRaw)
		historyJSON, err := jsonresp.Encode(history)
		if err != nil {
			return TestOutcome{}, err
		}

		record := m.Fields
		record.Delete("name")
		_ = record.Set("method", methodJSON)
		_ = record.Set("tested_at", stampJSON)
		_ = record.Set("history", historyJSON)
		for field, value := range settingsFields.All() {
			_ = record.Set(field, value)
		}
		stored.Set(key, LatencyEntry{Fields: record})
	}

	var kept model.Map[LatencyEntry]
	for key, entry := range stored.All() {
		if candidates.has(key) {
			kept.Set(key, entry)
		}
	}
	saveLatencyResults(ctx, kept)

	var outcomeResults model.Map[LatencyEntry]
	for _, id := range identityIDs {
		if entry, ok := kept.Get(identity[id]); ok {
			outcomeResults.Set(id, entry)
		}
	}
	missing := []string{}
	for _, id := range requested {
		if _, ok := identity[id]; !ok {
			missing = append(missing, id)
		}
	}
	return TestOutcome{
		TestedAt: stamp,
		Results:  outcomeResults,
		Missing:  missing,
		Settings: settings,
	}, nil
}

func isoUTC(t time.Time) string {
	t = t.UTC()
	if t.Nanosecond()/1000 == 0 {
		return t.Format("2006-01-02T15:04:05") + "Z"
	}
	return t.Format("2006-01-02T15:04:05") + fmt.Sprintf(".%06d", t.Nanosecond()/1000) + "Z"
}
