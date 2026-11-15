package health

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/pystr"
	"configflow/internal/repo"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/urlx"
)

// Node latency (backend/utils/node_latency.py): real proxy latency through
// the independent node-probe worker, with complete node credentials, never
// the running Mihomo controller.
//
// Worker protocol (version 4), for image package #40:
//   - argv: [<binary>] only; credentials never on the command line.
//   - stdin: json.dumps({url, timeout_ms, dns_servers, dns_bootstrap,
//     unified_delay, nodes: [<Mihomo proxy with name = source key>…]},
//     ensure_ascii=False) for latency; json.dumps({operation: "dns_check",
//     timeout_ms: 5000, dns_servers, dns_bootstrap}) for the DNS check.
//   - env: os.environ plus GOMAXPROCS=2 and GOMEMLIMIT=128MiB unless set.
//   - timeout: ceil(n/20) * timeout_ms/1000 + 5 s (latency), 10 s (check).
//   - stdout: {"version": 4, "results": [{name, latency: int>=1 | null,
//     error?, error_code?, …}]} or {"version": 4, "dns_check": {success,
//     addresses, latency_ms?, error_code?}}; return code 3 = invalid DNS
//     settings; stderr is never shown.

const (
	LatencyFile         = "node_latency.json"
	Method              = "proxy_http_v2"
	HistorySize         = 14
	MaxNodes            = 20
	MaxWorkers          = MaxNodes
	MaxConversionWorker = 5
	checkTimeout        = 10 * time.Second
)

var (
	latencyLog = logx.Get("backend.utils.node_latency")
	testLock   sync.Mutex
)

// LockForTest holds the test lock like a running test would (tests);
// release with the returned function.
func LockForTest() (release func()) {
	testLock.Lock()
	return testLock.Unlock
}

// DNSSettings is dns_settings().
func DNSSettings(ctx context.Context) (any, error) {
	settings, err := EffectiveSettings(ctx, nil)
	if err != nil {
		return nil, err
	}
	return settings.Value("dns_servers"), nil
}

// Settings is settings(): the probe URL, timeout, DNS and unified delay.
func Settings(ctx context.Context) (*jsonx.Object, error) {
	env := currentEnv
	invalid := &ProbeUnavailable{"节点测速设置无效，请检查测试地址及超时（500–10000 毫秒）"}
	url := pystr.Strip(env.getenv("CONFIGFLOW_NODE_TEST_URL", "https://www.gstatic.com/generate_204"))
	parsed, err := urlx.Split(url)
	if err != nil {
		return nil, invalid
	}
	username, _, password, _ := userInfo(parsed.Netloc)
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || username != "" || password != "" {
		return nil, invalid
	}
	if _, _, err := parsed.Port(); err != nil {
		return nil, invalid
	}
	timeout, err := pystr.Int(env.getenv("CONFIGFLOW_NODE_TEST_TIMEOUT_MS", "5000"), 10)
	if err != nil || !timeout.IsInt64() || timeout.Int64() < 500 || timeout.Int64() > 10000 {
		return nil, invalid
	}
	dns, err := EffectiveSettings(ctx, nil)
	if err != nil {
		if isValueError(err) {
			return nil, &ProbeUnavailable{err.Error()}
		}
		return nil, err
	}
	unified := pystr.Lower(env.getenv("CONFIGFLOW_NODE_TEST_UNIFIED_DELAY", "1"))
	out := jsonx.Obj("url", url, "timeout_ms", jsonx.Int(timeout.Int64()))
	out.Update(dns)
	out.Set("unified_delay", unified != "0" && unified != "false")
	return out, nil
}

func isValueError(err error) bool {
	switch err.(type) {
	case *ValueError:
		return true
	}
	return repo.IsValueError(err)
}

// candidate is one entry of collect_candidates (key → node).
type candidates struct {
	keys  []string
	nodes map[string]*jsonx.Object
}

func (c *candidates) add(node any, identity []any) error {
	object, ok := node.(*jsonx.Object)
	if !ok || object == nil || !jsonx.Truthy(object.Value("name")) {
		return nil
	}
	if value, isBool := object.Value("enabled").(bool); isBool && !value { // `is not False`
		return nil
	}
	data, err := jsonx.Encode(identity, jsonx.Options{Compact: true})
	if err != nil {
		return err
	}
	key := string(data)
	if _, seen := c.nodes[key]; !seen {
		c.keys = append(c.keys, key)
		c.nodes[key] = object
	}
	return nil
}

func (c *candidates) has(key string) bool { _, ok := c.nodes[key]; return ok }

// CollectCandidates is collect_candidates(): complete nodes keyed by
// source (`["node",id]`, `["subscription",sub_id,name]`), in order.
func CollectCandidates(ctx context.Context) ([]string, map[string]*jsonx.Object, error) {
	c, err := collectCandidates(ctx)
	if err != nil {
		return nil, nil, err
	}
	return c.keys, c.nodes, nil
}

func collectCandidates(ctx context.Context) (*candidates, error) {
	config, err := repo.GetSharedConfig(ctx)
	if err != nil {
		return nil, err
	}
	c := &candidates{nodes: map[string]*jsonx.Object{}}
	for _, node := range config.GetList("nodes") {
		if object, ok := node.(*jsonx.Object); ok && object != nil && jsonx.Truthy(object.Value("id")) {
			if err := c.add(object, []any{"node", object.Value("id")}); err != nil {
				return nil, err
			}
		}
	}
	for _, entry := range config.GetList("subscriptions") {
		sub, ok := entry.(*jsonx.Object)
		if !ok || sub == nil {
			return nil, &pyTypeError{"'" + typeName(entry) + "' object has no attribute 'get'"}
		}
		if value, isBool := sub.Value("enabled").(bool); (isBool && !value) || !jsonx.Truthy(sub.Value("id")) {
			continue
		}
		subID, ok := sub.Value("id").(string)
		if !ok {
			return nil, &pyTypeError{"'" + typeName(sub.Value("id")) + "' object has no attribute 'replace'"}
		}
		cache, err := substore.LoadSubscriptionCache(ctx, subID)
		if err != nil {
			return nil, err
		}
		var nodes []any
		if cache != nil {
			switch value := cache.GetOr("nodes", []any{}).(type) {
			case []any:
				nodes = value
			case bool, jsonx.Number:
				if jsonx.Truthy(value) {
					return nil, &pyTypeError{"'" + typeName(value) + "' object is not iterable"}
				}
			}
		}
		for _, node := range subs.FilterSubscriptionItems(sub, nodes) {
			if object, ok := node.(*jsonx.Object); ok && object != nil {
				if err := c.add(object, []any{"subscription", subID, object.Value("name")}); err != nil {
					return nil, err
				}
			}
		}
	}
	return c, nil
}

// proxyOf is _proxy_of(node): the complete Mihomo proxy, nil when the node
// cannot be converted (a malformed node must not stop the others).
func proxyOf(ctx context.Context, node *jsonx.Object, converter convert.ProxyStringConverter) (proxy *jsonx.Object) {
	defer func() {
		if recover() != nil {
			proxy = nil
		}
	}()
	node = node.DeepCopy()
	if jsonx.Truthy(node.Value("proxy_string")) || node.Has("params") {
		converted, err := convert.ConvertNodeToMihomo(ctx, node, converter)
		if err != nil {
			return nil
		}
		proxy = converted
	} else {
		proxy = node
		convert.FixProxyFields(proxy)
	}
	if proxy == nil || !jsonx.Truthy(proxy.Value("type")) {
		return nil
	}
	proxy.Set("name", node.Value("name"))
	return proxy
}

// needsSubStore is _needs_sub_store(node).
func needsSubStore(node *jsonx.Object) bool {
	raw, ok := node.Value("proxy_string").(string)
	return ok && raw != "" && convert.ParseStructuredProxyString(raw) == nil
}

// runProbe is _run_probe(proxies, options): ordered name → result.
func runProbe(ctx context.Context, proxies []*jsonx.Object, options *jsonx.Object) (*jsonx.Object, error) {
	env := currentEnv
	binary, err := env.probeBinary()
	if err != nil {
		return nil, err
	}
	payload := options.Clone()
	nodes := make([]any, len(proxies))
	for i, proxy := range proxies {
		nodes[i] = proxy
	}
	payload.Set("nodes", nodes)
	data, err := jsonx.Encode(payload, jsonx.Options{})
	if err != nil {
		return nil, err
	}
	if len(data) > 8*1024*1024 {
		return nil, valueError("本批节点配置过大，请减少每批节点数")
	}
	timeoutMS := options.Value("timeout_ms").(jsonx.Number).Float64()
	batches := float64((len(proxies) + MaxWorkers - 1) / MaxWorkers)
	budget := batches*timeoutMS/1000 + 5
	process, err := env.Runner.Run(ctx, Command{Argv: []string{binary}, Stdin: data,
		Timeout: time.Duration(budget * float64(time.Second)), Env: env.workerEnv()})
	if err == ErrTimeout {
		return nil, &ProbeUnavailable{"测速组件执行超时，已结束本批测试，请重试"}
	}
	if err != nil {
		return nil, &ProbeUnavailable{"测速组件无法启动，请检查可执行权限和 CPU 架构"}
	}
	stdout, err := decodeText(process.Stdout)
	if err != nil {
		return nil, err
	}
	if _, err := decodeText(process.Stderr); err != nil {
		return nil, err
	}
	// Worker stderr and the input are never exposed (node credentials).
	if process.ReturnCode == 3 {
		return nil, &ProbeUnavailable{"节点测速 DNS 设置无效，请检查服务器地址及引导 DNS"}
	}
	if process.ReturnCode != 0 {
		return nil, &ProbeUnavailable{"测速组件异常退出，请检查组件版本后重试"}
	}
	results, err := parseResults(stdout, proxies)
	if err != nil {
		if _, ok := err.(*ProbeUnavailable); ok {
			return nil, err
		}
		return nil, &ProbeUnavailable{"测速组件返回了无效结果，请检查组件版本"}
	}
	return results, nil
}

// parseResults validates the worker's answer; errors other than
// *ProbeUnavailable are Python's ValueError/TypeError/KeyError.
func parseResults(stdout string, proxies []*jsonx.Object) (*jsonx.Object, error) {
	parsed, err := jsonx.Parse([]byte(stdout))
	if err != nil {
		return nil, err
	}
	version, err := subscript(parsed, "version")
	if err != nil {
		return nil, err
	}
	if !jsonx.Equal(version, jsonx.Int(4)) {
		return nil, &ProbeUnavailable{versionMismatch}
	}
	resultsValue, err := subscript(parsed, "results")
	if err != nil {
		return nil, err
	}
	list, ok := resultsValue.([]any)
	if !ok {
		return nil, valueError("")
	}
	results := jsonx.NewObject()
	for _, entry := range list {
		object, ok := entry.(*jsonx.Object)
		if !ok || object == nil {
			return nil, &pyTypeError{"indices"}
		}
		name, ok := object.Get("name")
		if !ok {
			return nil, &pyTypeError{"KeyError name"}
		}
		switch name.(type) {
		case []any, *jsonx.Object:
			return nil, &pyTypeError{"unhashable"}
		}
		text, isString := name.(string)
		if !isString {
			return nil, valueError("") // never equals a proxy name
		}
		results.Set(text, object)
	}
	if results.Len() != len(uniqueNames(proxies)) {
		return nil, valueError("")
	}
	for _, name := range uniqueNames(proxies) {
		if !results.Has(name) {
			return nil, valueError("")
		}
	}
	for _, entry := range results.Values() {
		object := entry.(*jsonx.Object)
		latency, ok := object.Get("latency")
		if !ok {
			return nil, &pyTypeError{"KeyError latency"}
		}
		if latency != nil {
			number, isNumber := latency.(jsonx.Number)
			value, fits := number.Int64()
			if !isNumber || !number.IsInt() || (fits && value < 1) || strings.HasPrefix(number.String(), "-") {
				return nil, valueError("")
			}
		}
		if latency == nil && !jsonx.Truthy(object.Value("error")) {
			return nil, valueError("")
		}
	}
	return results, nil
}

func uniqueNames(proxies []*jsonx.Object) []string {
	var names []string
	seen := map[string]bool{}
	for _, proxy := range proxies {
		name := proxy.GetString("name")
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// LoadResults is load_results(): stored results of the current probe
// method; anything unreadable is empty.
func LoadResults(ctx context.Context) *jsonx.Object {
	out := jsonx.NewObject()
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return out
	}
	data, err := repository.ReadSharedJSON(ctx, LatencyFile)
	if err != nil || data == nil {
		return out
	}
	for name, entry := range data.All() {
		if object, ok := entry.(*jsonx.Object); ok && object != nil && jsonx.Equal(object.Value("method"), Method) {
			out.Set(name, object)
		}
	}
	return out
}

func saveResults(ctx context.Context, results *jsonx.Object) {
	repository, err := repo.GetRepository(ctx)
	if err == nil {
		_, err = repository.WriteSharedJSON(ctx, LatencyFile, results)
	}
	if err != nil {
		latencyLog.Error("写入节点延迟结果失败")
	}
}

// RunTests is run_tests(names, keys=keys): nil slices are None. One test
// at a time per process (ProbeBusy otherwise).
func RunTests(ctx context.Context, names, keys []string) (*jsonx.Object, error) {
	if !testLock.TryLock() {
		return nil, &ProbeBusy{"已有节点测速正在进行，请稍后重试"}
	}
	defer testLock.Unlock()
	return runTests(ctx, names, keys)
}

func dedupe(items []string) []string {
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

func runTests(ctx context.Context, names, keys []string) (*jsonx.Object, error) {
	env := currentEnv
	c, err := collectCandidates(ctx)
	if err != nil {
		return nil, err
	}
	if names != nil && keys != nil {
		return nil, valueError("names 与 keys 不能同时指定")
	}
	requestedIDs := keys
	if names != nil {
		requestedIDs = names
	}
	if names == nil && keys == nil {
		requestedIDs = c.keys
	}
	requested := dedupe(requestedIDs)
	if len(requested) > MaxNodes {
		return nil, valueError(fmt.Sprintf("单次最多测试 %d 个节点，请分批测试", MaxNodes))
	}
	// identities: requested id → source key (ordered).
	var identityIDs []string
	identity := map[string]string{}
	if names != nil {
		byName := map[string][]string{}
		for _, key := range c.keys {
			name := c.nodes[key].Value("name")
			switch name.(type) {
			case []any, *jsonx.Object:
				return nil, &pyTypeError{"unhashable type: '" + typeName(name) + "'"}
			}
			if text, ok := name.(string); ok {
				byName[text] = append(byName[text], key)
			}
		}
		for _, name := range requested {
			if len(byName[name]) > 1 {
				return nil, valueError("存在同名节点，请使用 keys 指定节点来源后重试")
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
			if c.has(key) {
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
		if needsSubStore(c.nodes[key]) {
			remote++
		}
	}
	if remote > MaxConversionWorker {
		return nil, valueError(fmt.Sprintf("单次最多测试 %d 个需要 Sub-Store 转换的节点，请分批测试", MaxConversionWorker))
	}
	options, err := Settings(ctx)
	if err != nil {
		return nil, err
	}
	measured := jsonx.NewObject()
	if len(wanted) > 0 {
		if _, err := env.probeBinary(); err != nil { // fail before conversion if missing
			return nil, err
		}
		converted := make([]*jsonx.Object, len(wanted))
		workers := MaxConversionWorker
		if len(wanted) < workers {
			workers = len(wanted)
		}
		jobs := make(chan int)
		var wg sync.WaitGroup
		converter := env.converter()
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					converted[i] = proxyOf(ctx, c.nodes[wanted[i]], converter)
				}
			}()
		}
		for i := range wanted {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		var proxies []*jsonx.Object
		for i, key := range wanted {
			if proxy := converted[i]; proxy != nil {
				// The worker correlates by name; source keys keep same-name
				// nodes apart.
				proxy.Set("name", key)
				proxies = append(proxies, proxy)
			} else {
				measured.Set(key, jsonx.Obj("latency", nil, "error_code", "config",
					"error", "无法解析完整节点配置，请检查格式或 Sub-Store 服务"))
			}
		}
		if len(proxies) > 0 {
			results, err := runProbe(ctx, proxies, options)
			if err != nil {
				return nil, err
			}
			measured.Update(results)
		}
	}

	stamp := isoUTC(env.Now())
	stored := LoadResults(ctx)
	for key, measurement := range measured.All() {
		m := measurement.(*jsonx.Object)
		entry, _ := stored.Value(key).(*jsonx.Object)
		if entry == nil {
			entry = jsonx.NewObject()
		}
		sameTest := jsonx.Equal(entry.Value("url"), options.Value("url")) &&
			jsonx.Equal(entry.Value("unified_delay"), options.Value("unified_delay")) &&
			jsonx.Equal(entry.GetOr("dns_servers", []any{}), options.Value("dns_servers")) &&
			jsonx.Equal(entry.GetOr("dns_bootstrap", []any{}), options.Value("dns_bootstrap"))
		history := []any{}
		if sameTest {
			previous, err := pyList(entry.Value("history"))
			if err != nil {
				return nil, err
			}
			if len(previous) > HistorySize-1 {
				previous = previous[len(previous)-(HistorySize-1):]
			}
			history = append(history, previous...)
		}
		history = append(history, m.Value("latency"))
		record := jsonx.NewObject()
		for field, value := range m.All() {
			if field != "name" {
				record.Set(field, value)
			}
		}
		record.Set("method", Method)
		record.Set("tested_at", stamp)
		record.Set("history", history)
		for field, value := range options.All() {
			record.Set(field, jsonx.DeepCopy(value))
		}
		stored.Set(key, record)
	}
	kept := jsonx.NewObject()
	for key, entry := range stored.All() {
		if c.has(key) {
			kept.Set(key, entry)
		}
	}
	saveResults(ctx, kept)
	results := jsonx.NewObject()
	for _, id := range identityIDs {
		results.Set(id, kept.Value(identity[id]))
	}
	missing := []any{}
	for _, id := range requested {
		if _, ok := identity[id]; !ok {
			missing = append(missing, id)
		}
	}
	return jsonx.Obj("tested_at", stamp, "results", results, "missing", missing, "settings", options), nil
}

// pyList is list(value or []).
func pyList(value any) ([]any, error) {
	if !jsonx.Truthy(value) {
		return nil, nil
	}
	switch v := value.(type) {
	case []any:
		return append([]any{}, v...), nil
	case string:
		var out []any
		for _, r := range v {
			out = append(out, string(r))
		}
		return out, nil
	case *jsonx.Object:
		var out []any
		for key := range v.All() {
			out = append(out, key)
		}
		return out, nil
	}
	return nil, &pyTypeError{"'" + typeName(value) + "' object is not iterable"}
}

// isoUTC is datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z').
func isoUTC(t time.Time) string {
	t = t.UTC()
	if t.Nanosecond()/1000 == 0 {
		return t.Format("2006-01-02T15:04:05") + "Z"
	}
	return t.Format("2006-01-02T15:04:05") + fmt.Sprintf(".%06d", t.Nanosecond()/1000) + "Z"
}
