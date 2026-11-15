package convert

// The node-conversion group of backend/converters/mihomo.py (IndentDumper,
// apply_github_proxy_domain, mihomo_used_ports, mihomo_listen_ports, the
// proxy YAML cache, _parse_structured_proxy_string, node_conversions,
// convert_node_to_mihomo) and backend/utils/proxy_utils.py. The rest of
// mihomo.py (generation) is module C18.

import (
	"container/list"
	"context"
	"math/big"
	"sort"
	"strings"
	"sync"

	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/yamlx"
)

var mihomoLog = logx.Get("backend.converters.mihomo")

// IndentDumperOptions is Dumper=IndentDumper: the only change from
// yaml.Dumper is that list items are indented under their key. Pass the
// other yaml.dump arguments of the call site in opts (yamlx.IndentDump is the
// common allow_unicode/sort_keys=False/indent=2 preset).
func IndentDumperOptions(opts yamlx.DumpOptions) yamlx.DumpOptions {
	opts.IndentSequences = true
	return opts
}

// FixProxyFields is proxy_utils.fix_proxy_fields: fill in fields Mihomo
// requires (VLESS encryption: none, reality-opts short-id) in place.
func FixProxyFields(proxy *jsonx.Object) {
	if proxy == nil || proxy.Len() == 0 || !jsonx.Equal(proxy.Value("type"), "vless") {
		return
	}
	encryption := proxy.GetOr("encryption", "")
	if encryption == nil || jsonx.Equal(encryption, "") || jsonx.Equal(encryption, "zero") {
		proxy.Set("encryption", "none")
	}
	if reality, ok := proxy.Value("reality-opts").(*jsonx.Object); ok && reality != nil && !reality.Has("short-id") {
		reality.Set("short-id", "")
	}
}

var githubDomains = map[string]bool{
	"github.com": true, "raw.githubusercontent.com": true, "gist.githubusercontent.com": true, "api.github.com": true,
}

// ApplyGithubProxyDomain is apply_github_proxy_domain: prefix a GitHub URL
// with system_config.github_proxy_domain ("proxy/original"). Errors are the
// exceptions Python raises (a system_config that is not an object, a URL
// urlsplit refuses).
func ApplyGithubProxyDomain(url string, config *jsonx.Object) (string, error) {
	if url == "" || config == nil || config.Len() == 0 {
		return url, nil
	}
	system, err := asObject(config.GetOr("system_config", jsonx.NewObject()))
	if err != nil {
		return "", err
	}
	proxyURL := ""
	if configured, ok := system.GetOr("github_proxy_domain", "").(string); ok {
		proxyURL = pyStrip(configured)
	}
	if proxyURL == "" {
		return url, nil
	}
	parts, err := urlsplit(url)
	if err != nil {
		return "", err
	}
	if host := parts.hostname(); host == "" || !githubDomains[host] {
		return url, nil
	}
	if !strings.HasPrefix(proxyURL, "http://") && !strings.HasPrefix(proxyURL, "https://") {
		proxyURL = "https://" + proxyURL
	}
	if !strings.HasSuffix(proxyURL, "/") {
		proxyURL += "/"
	}
	if strings.HasPrefix(url, proxyURL) {
		return url, nil
	}
	return proxyURL + url, nil
}

// Region probe names (mihomo.py constants shared with C18).
const (
	RegionProbeGroup       = "ConfigFlow-Region-Probe"
	RegionProbeListener    = "configflow-region-probe"
	DefaultRegionProbePort = 17999
)

// InboundPortKeys is _INBOUND_PORT_KEYS.
var InboundPortKeys = []string{"port", "socks-port", "mixed-port", "redir-port", "tproxy-port"}

// portValue is isinstance(v, int) as a port key (bools included, as in
// Python: True is port 1).
func portValue(v any) (jsonx.Number, bool) {
	switch value := v.(type) {
	case bool:
		if value {
			return jsonx.Int(1), true
		}
		return jsonx.Int(0), true
	case jsonx.Number:
		if value.IsInt() {
			n, _ := value.BigInt()
			return jsonx.BigInt(n), true
		}
	}
	return "", false
}

// hashable is the TypeError a set or dict raises for an unhashable value.
func hashable(v any) error {
	switch v.(type) {
	case []any:
		return pyError("TypeError", "unhashable type: 'list'")
	case *jsonx.Object:
		return pyError("TypeError", "unhashable type: 'dict'")
	}
	return nil
}

// MihomoUsedPorts is mihomo_used_ports: the inbound ports the configuration
// already occupies, keyed by their canonical integer text (jsonx.Int(p)).
// An unhashable port value is Python's TypeError.
func MihomoUsedPorts(config *jsonx.Object) (map[jsonx.Number]bool, error) {
	var values []any
	for _, key := range InboundPortKeys {
		values = append(values, config.Value(key))
	}
	if listeners, ok := config.Value("listeners").([]any); ok {
		for _, v := range listeners {
			item, ok := v.(*jsonx.Object)
			if !ok || item == nil || jsonx.Equal(item.Value("name"), RegionProbeListener) {
				continue
			}
			values = append(values, item.Value("port"))
		}
	}
	// A set keeps the first of equal members (7890.0 hides a later 7890,
	// True a later 1); only the int members are ports.
	first := map[string]any{}
	var order []string
	for _, v := range values {
		if err := hashable(v); err != nil {
			return nil, err
		}
		key := setKey(v)
		if _, ok := first[key]; !ok {
			first[key] = v
			order = append(order, key)
		}
	}
	used := map[jsonx.Number]bool{}
	for _, key := range order {
		if port, ok := portValue(first[key]); ok {
			used[port] = true
		}
	}
	return used, nil
}

// setKey groups values the way Python's == does for set members: numbers by
// value (1 == 1.0 == True), other values by type and text.
func setKey(v any) string {
	switch value := v.(type) {
	case bool:
		if value {
			return "n1"
		}
		return "n0"
	case jsonx.Number:
		if value.IsInt() {
			n, _ := value.BigInt()
			return "n" + n.String()
		}
		f := value.Float64()
		if f == f && f-f == 0 {
			if b, accuracy := new(big.Float).SetFloat64(f).Int(nil); accuracy == big.Exact {
				return "n" + b.String()
			}
		}
		return "f" + string(value)
	case string:
		return "s" + value
	case nil:
		return "none"
	}
	return "o" + jsonx.Repr(v)
}

// PortOwner is one entry of MihomoListenPorts.
type PortOwner struct {
	Port  jsonx.Number
	Owner string
}

// MihomoListenPorts is mihomo_listen_ports: {port: setting} for the inbound
// ports, listeners and external-controller. Order: inbound keys in key order
// (a port used twice keeps its first position and the last key, like a dict
// comprehension), then listener ports in ascending order (Python iterates a
// set there, whose order is not reproduced), then the controller.
func MihomoListenPorts(config *jsonx.Object) ([]PortOwner, error) {
	var owners []PortOwner
	index := map[jsonx.Number]int{}
	set := func(port jsonx.Number, owner string, overwrite bool) {
		if i, ok := index[port]; ok {
			if overwrite {
				owners[i].Owner = owner
			}
			return
		}
		index[port] = len(owners)
		owners = append(owners, PortOwner{port, owner})
	}
	for _, key := range InboundPortKeys {
		if port, ok := portValue(config.Value(key)); ok {
			set(port, key, true)
		}
	}
	used, err := MihomoUsedPorts(config)
	if err != nil {
		return nil, err
	}
	ports := make([]jsonx.Number, 0, len(used))
	for port := range used {
		if _, ok := index[port]; !ok {
			ports = append(ports, port)
		}
	}
	sortPorts(ports)
	for _, port := range ports {
		set(port, "listeners", false)
	}
	if controller, ok := config.Value("external-controller").(string); ok {
		text := controller
		if i := strings.LastIndex(controller, ":"); i >= 0 {
			text = controller[i+1:]
		}
		if pyIsDigit(text) {
			value := new(big.Int)
			for _, r := range text {
				d, ok := decimalValue(r)
				if !ok {
					return nil, pyError("ValueError", "invalid literal for int() with base 10: "+jsonx.Repr(text))
				}
				value.Mul(value, big.NewInt(10))
				value.Add(value, big.NewInt(int64(d)))
			}
			set(jsonx.BigInt(value), "external-controller", false)
		}
	}
	return owners, nil
}

// sortPorts sorts integer port keys by value.
func sortPorts(ports []jsonx.Number) {
	sort.Slice(ports, func(i, j int) bool {
		a, _ := ports[i].BigInt()
		b, _ := ports[j].BigInt()
		return a.Cmp(b) < 0
	})
}

// ---- proxy YAML cache ----

// Only the YAML branch is cached: URI/JSON already have cheap fast paths.
var (
	proxyYAMLCacheSize      = 128
	proxyYAMLMaxInputBytes  = 64 * 1024
	proxyYAMLMaxResultBytes = 64 * 1024
	proxyYAMLMaxObjects     = 2048
	proxyYAMLMaxDepth       = 32

	proxyYAMLMu    sync.Mutex
	proxyYAMLOrder = list.New() // front = least recently used; values are *proxyYAMLEntry
	proxyYAMLIndex = map[string]*list.Element{}

	// safeLoad is yaml.safe_load (a variable so tests can count calls).
	safeLoad = yamlx.SafeLoad
)

type proxyYAMLEntry struct {
	text string
	data *jsonx.Object
}

// sizeOf approximates sys.getsizeof for the cache budget (CPython 3.11 on
// 64-bit); the budget only decides whether a result is cached.
func sizeOf(v any) int {
	switch value := v.(type) {
	case nil:
		return 16
	case bool:
		return 28
	case string:
		if isASCII(value) {
			return 49 + len(value)
		}
		return 74 + 2*len([]rune(value))
	case jsonx.Number:
		if value.IsInt() {
			return 28
		}
		return 24
	case []any:
		return 56 + 8*len(value)
	case *jsonx.Object:
		n := value.Len()
		switch {
		case n == 0:
			return 64
		case n <= 5:
			return 232
		case n <= 10:
			return 360
		case n <= 21:
			return 640
		default:
			return 1176 + 48*(n-42)
		}
	}
	return 0
}

// cacheableProxyYAML is _cacheable_proxy_yaml: bound ordinary result trees;
// shared containers (aliases) and special YAML types bypass the cache.
func cacheableProxyYAML(data any) bool {
	type item struct {
		value any
		depth int
	}
	seenObjects := map[*jsonx.Object]bool{}
	seenLists := map[*any]bool{}
	pending := []item{{data, 0}}
	count, size := 0, 0
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if current.depth > proxyYAMLMaxDepth {
			return false
		}
		switch value := current.value.(type) {
		case *jsonx.Object:
			if seenObjects[value] {
				return false
			}
			seenObjects[value] = true
			for key, child := range value.All() {
				pending = append(pending, item{key, current.depth + 1}, item{child, current.depth + 1})
			}
		case []any:
			if len(value) > 0 {
				if seenLists[&value[0]] {
					return false
				}
				seenLists[&value[0]] = true
			}
			for _, child := range value {
				pending = append(pending, item{child, current.depth + 1})
			}
		case nil, bool, string, jsonx.Number:
		default:
			return false
		}
		count++
		size += sizeOf(current.value)
		if count > proxyYAMLMaxObjects || size > proxyYAMLMaxResultBytes {
			return false
		}
	}
	return true
}

// loadProxyYAML is _load_proxy_yaml: yaml.safe_load with a bounded LRU
// cache of ordinary proxy dicts. Cached results are private deep copies.
func loadProxyYAML(text string) (any, error) {
	if len(text) > proxyYAMLMaxInputBytes {
		return safeLoad(text)
	}
	proxyYAMLMu.Lock()
	if element, ok := proxyYAMLIndex[text]; ok {
		proxyYAMLOrder.MoveToBack(element)
		data := element.Value.(*proxyYAMLEntry).data
		proxyYAMLMu.Unlock()
		return data.DeepCopy(), nil
	}
	data, err := safeLoad(text)
	if err != nil {
		proxyYAMLMu.Unlock()
		return nil, err
	}
	object, ok := data.(*jsonx.Object)
	if !ok || object == nil || !pyTruthy(object.Value("type")) || !cacheableProxyYAML(object) {
		proxyYAMLMu.Unlock()
		return data, nil
	}
	proxyYAMLIndex[text] = proxyYAMLOrder.PushBack(&proxyYAMLEntry{text, object})
	for proxyYAMLOrder.Len() > proxyYAMLCacheSize {
		oldest := proxyYAMLOrder.Front()
		proxyYAMLOrder.Remove(oldest)
		delete(proxyYAMLIndex, oldest.Value.(*proxyYAMLEntry).text)
	}
	proxyYAMLMu.Unlock()
	return object.DeepCopy(), nil
}

// pyTruthy is bool(v), including the YAML-only values (bytes from !!binary).
func pyTruthy(v any) bool {
	if b, ok := v.([]byte); ok {
		return len(b) > 0
	}
	return jsonx.Truthy(v)
}

// ParseStructuredProxyString is _parse_structured_proxy_string: a JSON or
// YAML proxy string with a "type" is parsed locally; nil means Sub-Store has
// to convert it (URI links, base64).
func ParseStructuredProxyString(proxyString string) *jsonx.Object {
	stripped := pyStrip(proxyString)
	firstLine, _, _ := strings.Cut(stripped, "\n")
	if strings.Contains(firstLine, "://") {
		before, _, _ := strings.Cut(stripped, "://")
		if !strings.HasSuffix(before, ":") {
			first := pyStrip(firstLine)
			if !strings.HasSuffix(first, ":") && strings.Contains(first, "://") {
				scheme, _, _ := strings.Cut(first, "://")
				if !strings.Contains(scheme, " ") && !strings.Contains(scheme, ":") {
					return nil
				}
			}
		}
	}
	if strings.HasPrefix(stripped, "{") {
		if data, err := jsonx.ParseString(stripped); err == nil {
			if object, ok := data.(*jsonx.Object); ok && object != nil && pyTruthy(object.Value("type")) {
				return object
			}
		}
	}
	if data, err := loadProxyYAML(stripped); err == nil {
		if object, ok := data.(*jsonx.Object); ok && object != nil && pyTruthy(object.Value("type")) {
			return object
		}
	}
	return nil
}

// ---- node_conversions / convert_node_to_mihomo ----

// ProxyStringConverter is the part of the Sub-Store client (C13) that node
// conversion needs: sub_store_client.convert_proxy_string(s) with the
// default ClashMeta target. nil means the conversion failed (None).
type ProxyStringConverter interface {
	ConvertProxyString(ctx context.Context, proxyString string) *jsonx.Object
}

// ConversionResults records Sub-Store conversions of URI node strings (the
// `results` dict of node_conversions). A nil value records a failed
// conversion. Safe for concurrent use.
type ConversionResults struct {
	mu    sync.Mutex
	keys  []string
	items map[string]*jsonx.Object
}

// NewConversionResults returns an empty record.
func NewConversionResults() *ConversionResults {
	return &ConversionResults{items: map[string]*jsonx.Object{}}
}

// Get returns a deep copy of the recorded conversion of proxyString.
func (r *ConversionResults) Get(proxyString string) (*jsonx.Object, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.items[proxyString]
	return value.DeepCopy(), ok
}

// Keys lists the recorded strings in insertion order.
func (r *ConversionResults) Keys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.keys...)
}

// Len is len(results).
func (r *ConversionResults) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.keys)
}

func (r *ConversionResults) set(proxyString string, value *jsonx.Object) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[proxyString]; !ok {
		r.keys = append(r.keys, proxyString)
	}
	r.items[proxyString] = value.DeepCopy()
}

type conversionScope struct {
	results *ConversionResults
	reuse   bool
}

type conversionScopeKey struct{}

// WithNodeConversions is the node_conversions(results, reuse=...) context
// manager: conversions made with the returned context are recorded into
// results; with reuse a string already in results is answered from it
// instead of asking Sub-Store again. The scope ends with the context (the
// parent context keeps its own scope, as the thread-local was restored).
func WithNodeConversions(ctx context.Context, results *ConversionResults, reuse bool) context.Context {
	return context.WithValue(ctx, conversionScopeKey{}, &conversionScope{results, reuse})
}

// ConvertNodeToMihomo is convert_node_to_mihomo: JSON/YAML node strings are
// parsed locally, URI/base64 strings converted by Sub-Store, structured
// nodes flattened. nil (no error) is Python's None. Errors are the
// exceptions Python raises on malformed nodes.
func ConvertNodeToMihomo(ctx context.Context, node *jsonx.Object, converter ProxyStringConverter) (*jsonx.Object, error) {
	outerName := node.GetOr("name", "")
	if value := node.Value("proxy_string"); jsonx.Truthy(value) {
		proxyString, ok := value.(string)
		if !ok {
			return nil, attributeError(value, "strip")
		}
		if parsed := ParseStructuredProxyString(proxyString); parsed != nil && parsed.Len() > 0 {
			mihomoLog.Info("节点 '" + jsonx.Str(outerName) + "' 为 JSON/YAML 格式，本地解析")
			parsed.Set("name", outerName)
			FixProxyFields(parsed)
			return parsed, nil
		}
		scope, _ := ctx.Value(conversionScopeKey{}).(*conversionScope)
		var proxy *jsonx.Object
		reused := false
		if scope != nil && scope.reuse {
			proxy, reused = scope.results.Get(proxyString)
		}
		if !reused {
			mihomoLog.Info("节点 '" + jsonx.Str(outerName) + "' 为 URI/base64 格式，使用 Sub-Store 的转换结果")
			proxy = converter.ConvertProxyString(ctx, proxyString)
			if scope != nil {
				scope.results.set(proxyString, proxy)
			}
		}
		if proxy != nil && proxy.Len() > 0 {
			proxy.Set("name", outerName)
			FixProxyFields(proxy)
			return proxy, nil
		}
		return nil, nil
	}
	rawType := node.GetOr("type", "")
	typeText, ok := rawType.(string)
	if !ok {
		return nil, attributeError(rawType, "lower")
	}
	nodeType := pyLower(typeText)
	if nodeType == "" {
		return nil, nil
	}
	base := jsonx.Obj("name", outerName, "type", nodeType, "server", node.GetOr("server", ""),
		"port", node.GetOr("port", jsonx.Int(0)))
	switch params := node.GetOr("params", jsonx.NewObject()).(type) {
	case *jsonx.Object:
		for key, value := range params.All() {
			base.Set(key, value)
		}
	case []any:
		// dict.update accepts an iterable of key/value pairs.
		for i, item := range params {
			pair, ok := item.([]any)
			if !ok {
				return nil, pyError("TypeError", "cannot convert dictionary update sequence element #"+
					jsonx.Str(jsonx.Int(i))+" to a sequence")
			}
			if len(pair) != 2 {
				return nil, pyError("ValueError", "dictionary update sequence element #"+jsonx.Str(jsonx.Int(i))+
					" has length "+jsonx.Str(jsonx.Int(len(pair)))+"; 2 is required")
			}
			key, ok := pair[0].(string)
			if !ok {
				if err := hashable(pair[0]); err != nil {
					return nil, err
				}
				return nil, pyError("TypeError", "non-string key")
			}
			base.Set(key, pair[1])
		}
	case string:
		if params != "" {
			return nil, pyError("ValueError", "dictionary update sequence element #0 has length 1; 2 is required")
		}
	default:
		return nil, pyError("TypeError", "'"+pyTypeName(params)+"' object is not iterable")
	}
	FixProxyFields(base)
	return base, nil
}
