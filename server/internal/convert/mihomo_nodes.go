package convert

// The node-conversion group of backend/converters/mihomo.py (IndentDumper,
// apply_github_proxy_domain, mihomo_used_ports, mihomo_listen_ports, the
// proxy YAML cache, _parse_structured_proxy_string, node_conversions,
// convert_node_to_mihomo) and backend/utils/proxy_utils.py. The rest of
// mihomo.py (generation) is module C18.

import (
	"container/list"
	"context"
	"errors"
	"math"
	"math/big"
	"sort"
	"strings"
	"sync"

	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/pystr"
	"configflow/internal/weburl"
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
// that cannot be parsed).
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
		proxyURL = pystr.Strip(configured)
	}
	if proxyURL == "" {
		return url, nil
	}
	parts, err := weburl.Parse(url)
	if err != nil {
		if errors.Is(err, weburl.ErrAmbiguousHost) || strings.ContainsAny(url, "[]") && !strings.Contains(url, "[v") {
			return "", pyError("ValueError", err.Error()) // a broken IPv6 literal or a spoofable host
		}
		return url, nil // not a usable URL, so not a GitHub one
	}
	if host := weburl.Hostname(parts); host == "" || !githubDomains[host] {
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
// Python: True is port 1); the result is the canonical integer text.
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

// usedPorts is mihomo_used_ports: the inbound ports the configuration
// already occupies, as Python holds them (True stays a bool) and in the
// iteration order of the resulting CPython set (see pySet).
func usedPorts(config *jsonx.Object) ([]any, error) {
	used := newPySet()
	for _, key := range InboundPortKeys {
		if err := used.add(config.Value(key)); err != nil {
			return nil, err
		}
	}
	if listeners, ok := config.Value("listeners").([]any); ok {
		ports := newPySet()
		for _, v := range listeners {
			item, ok := v.(*jsonx.Object)
			if !ok || item == nil || jsonx.Equal(item.Value("name"), RegionProbeListener) {
				continue
			}
			if err := ports.add(item.Value("port")); err != nil {
				return nil, err
			}
		}
		used.update(ports)
	}
	ints := newPySet()
	for _, v := range used.members() {
		if _, ok := portValue(v); ok {
			_ = ints.add(v)
		}
	}
	return ints.members(), nil
}

// MihomoUsedPorts is mihomo_used_ports: the inbound ports the configuration
// already occupies, keyed by their canonical integer text (jsonx.Int(p)).
// An unhashable port value is Python's TypeError.
func MihomoUsedPorts(config *jsonx.Object) (map[jsonx.Number]bool, error) {
	values, err := usedPorts(config)
	if err != nil {
		return nil, err
	}
	used := map[jsonx.Number]bool{}
	for _, v := range values {
		port, _ := portValue(v)
		used[port] = true
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
	// Port is the canonical integer text of the key.
	Port jsonx.Number
	// Value is the key as Python holds it (a bool for `port: true`), for
	// messages: f'{port}' writes True.
	Value any
	Owner string
}

// MihomoListenPorts is mihomo_listen_ports: {port: setting} for the inbound
// ports, listeners and external-controller, in Python's dict order: inbound
// keys in key order (a port used twice keeps its first position and key
// object and gets the last setting), then the listener ports in the
// iteration order of mihomo_used_ports' set, then the controller.
func MihomoListenPorts(config *jsonx.Object) ([]PortOwner, error) {
	var owners []PortOwner
	index := map[jsonx.Number]int{}
	set := func(value any, owner string, overwrite bool) {
		port, _ := portValue(value)
		if i, ok := index[port]; ok {
			if overwrite {
				owners[i].Owner = owner
			}
			return
		}
		index[port] = len(owners)
		owners = append(owners, PortOwner{port, value, owner})
	}
	for _, key := range InboundPortKeys {
		if v := config.Value(key); isPort(v) {
			set(v, key, true)
		}
	}
	used, err := usedPorts(config)
	if err != nil {
		return nil, err
	}
	for _, v := range used {
		set(v, "listeners", false)
	}
	if controller, ok := config.Value("external-controller").(string); ok {
		text := controller
		if i := strings.LastIndex(controller, ":"); i >= 0 {
			text = controller[i+1:]
		}
		if pyIsDigit(text) {
			value, err := pystr.Int(text, 10)
			if err != nil {
				return nil, pyError("ValueError", err.Error())
			}
			set(jsonx.BigInt(value), "external-controller", false)
		}
	}
	return owners, nil
}

func isPort(v any) bool {
	_, ok := portValue(v)
	return ok
}

// sortPorts sorts integer port keys by value.
func sortPorts(ports []jsonx.Number) {
	sort.Slice(ports, func(i, j int) bool {
		a, _ := ports[i].BigInt()
		b, _ := ports[j].BigInt()
		return a.Cmp(b) < 0
	})
}

// ---- CPython 3.11 set iteration order (Objects/setobject.c) ----

// pySet reproduces the slot layout of a CPython set so that iterating it
// gives Python's order: open addressing with LINEAR_PROBES linear probes,
// then perturbed probing, resized when fill*5 >= mask*3. Numbers and bools
// use CPython's hashes. Members whose hash Python randomises per process or
// derives from an address (str, None, NaN) count towards the fill and the
// resizes but take no slot, so ints are placed as Python places them unless
// such a member happened to take an int's slot first.
type pySet struct {
	table    []pySetEntry
	fill     int
	phantoms map[string]bool
}

type pySetEntry struct {
	key  any
	hash uint64
	full bool
}

const (
	pySetMinSize      = 8
	pySetLinearProbes = 9
	pySetPerturbShift = 5
	pyHashBits        = 61
	pyHashModulus     = uint64(1)<<pyHashBits - 1
	pyHashInf         = 314159
)

func newPySet() *pySet {
	return &pySet{table: make([]pySetEntry, pySetMinSize), phantoms: map[string]bool{}}
}

func (s *pySet) mask() uint64 { return uint64(len(s.table) - 1) }

// add is set.add(v); an unhashable v is Python's TypeError.
func (s *pySet) add(v any) error {
	if err := hashable(v); err != nil {
		return err
	}
	hash, known := pyHash(v)
	if !known {
		key := setKey(v)
		if s.phantoms[key] {
			return nil
		}
		s.phantoms[key] = true
		s.grow()
		return nil
	}
	s.addEntry(v, hash)
	return nil
}

func (s *pySet) addEntry(v any, hash uint64) {
	mask := s.mask()
	i := hash & mask
	perturb := hash
	key := setKey(v)
	for {
		probes := 0
		if i+pySetLinearProbes <= mask {
			probes = pySetLinearProbes
		}
		for j := i; ; j++ {
			entry := &s.table[j]
			if !entry.full {
				entry.key, entry.hash, entry.full = v, hash, true
				s.grow()
				return
			}
			if entry.hash == hash && setKey(entry.key) == key {
				return
			}
			if probes == 0 {
				break
			}
			probes--
		}
		perturb >>= pySetPerturbShift
		i = (i*5 + 1 + perturb) & mask
	}
}

// used is the number of members, phantoms included.
func (s *pySet) used() int {
	n := len(s.phantoms)
	for _, e := range s.table {
		if e.full {
			n++
		}
	}
	return n
}

// grow counts one new member and resizes like set_add_entry.
func (s *pySet) grow() {
	s.fill++
	if uint64(s.fill)*5 < s.mask()*3 {
		return
	}
	used := s.used()
	if used > 50000 {
		s.resize(used * 2)
	} else {
		s.resize(used * 4)
	}
}

// resize is set_table_resize: the smallest power of two above minused,
// members reinserted in slot order.
func (s *pySet) resize(minused int) {
	size := pySetMinSize
	for size <= minused {
		size <<= 1
	}
	old := s.table
	s.table = make([]pySetEntry, size)
	s.fill = len(s.phantoms)
	for _, e := range old {
		if e.full {
			s.insertClean(e)
			s.fill++
		}
	}
}

func (s *pySet) insertClean(e pySetEntry) {
	mask := s.mask()
	i := e.hash & mask
	perturb := e.hash
	for {
		if !s.table[i].full {
			s.table[i] = e
			return
		}
		if i+pySetLinearProbes <= mask {
			for j := uint64(1); j <= pySetLinearProbes; j++ {
				if !s.table[i+j].full {
					s.table[i+j] = e
					return
				}
			}
		}
		perturb >>= pySetPerturbShift
		i = (i*5 + 1 + perturb) & mask
	}
}

// update is set |= other (set_merge with normal insertions: the target is
// never empty here).
func (s *pySet) update(other *pySet) {
	otherUsed := other.used()
	if otherUsed == 0 {
		return
	}
	if uint64(s.fill+otherUsed)*5 >= s.mask()*3 {
		s.resize((s.used() + otherUsed) * 2)
	}
	for key := range other.phantoms {
		if !s.phantoms[key] {
			s.phantoms[key] = true
			s.grow()
		}
	}
	for _, e := range other.table {
		if e.full {
			s.addEntry(e.key, e.hash)
		}
	}
}

// members lists the slotted members in iteration order.
func (s *pySet) members() []any {
	var out []any
	for _, e := range s.table {
		if e.full {
			out = append(out, e.key)
		}
	}
	return out
}

// pyHash is hash(v) for bools, ints and floats (as size_t); known is false
// for values whose hash differs between processes.
func pyHash(v any) (hash uint64, known bool) {
	switch value := v.(type) {
	case bool:
		if value {
			return 1, true
		}
		return 0, true
	case jsonx.Number:
		if value.IsInt() {
			n, _ := value.BigInt()
			return pyIntHash(n), true
		}
		f := value.Float64()
		if f != f {
			return 0, false
		}
		return pyFloatHash(f), true
	}
	return 0, false
}

func pyIntHash(n *big.Int) uint64 {
	x := new(big.Int).Abs(n)
	x.Mod(x, new(big.Int).SetUint64(pyHashModulus))
	h := int64(x.Uint64())
	if n.Sign() < 0 {
		h = -h
	}
	if h == -1 {
		h = -2
	}
	return uint64(h)
}

// pyFloatHash is _Py_HashDouble for a finite or infinite v.
func pyFloatHash(v float64) uint64 {
	if math.IsInf(v, 0) {
		if v > 0 {
			return pyHashInf
		}
		inf := int64(pyHashInf)
		return uint64(-inf)
	}
	m, e := math.Frexp(v)
	sign := int64(1)
	if m < 0 {
		sign, m = -1, -m
	}
	var x uint64
	for m != 0 {
		x = ((x << 28) & pyHashModulus) | x>>(pyHashBits-28)
		m *= 268435456.0
		e -= 28
		y := uint64(m)
		m -= float64(y)
		x += y
		if x >= pyHashModulus {
			x -= pyHashModulus
		}
	}
	if e >= 0 {
		e %= pyHashBits
	} else {
		e = pyHashBits - 1 - ((-1 - e) % pyHashBits)
	}
	x = ((x << uint(e)) & pyHashModulus) | x>>uint(pyHashBits-e)
	x = uint64(int64(x) * sign)
	if x == ^uint64(0) {
		x = ^uint64(1)
	}
	return x
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

	// safeLoad is yaml.safe_load (a variable so tests can count calls);
	// a mapping with keys other than strings is a *yamlx.Mapping.
	safeLoad = func(text string) (any, error) {
		return yamlx.LoadWith(text, yamlx.LoadOptions{KeepNonStringKeys: true})
	}
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
// Mappings with keys other than strings (`1: a`, `yes: a`) are never cached
// (as in Python) and come back with the keys as json.dumps writes them, the
// closest a jsonx object gets to Python's dict (see stringKeys).
func loadProxyYAML(text string) (any, error) {
	if len(text) > proxyYAMLMaxInputBytes {
		data, err := safeLoad(text)
		if err != nil {
			return nil, err
		}
		data, _ = stringKeys(data)
		return data, nil
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
	data, converted := stringKeys(data)
	object, ok := data.(*jsonx.Object)
	if converted || !ok || object == nil || !pyTruthy(object.Value("type")) || !cacheableProxyYAML(object) {
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

// stringKeys replaces every *yamlx.Mapping (a dict with a key that is not a
// string) in a loaded value by a jsonx object whose keys are the text
// json.dumps writes for them (1 -> "1", 1.5 -> "1.5", true -> "true", null ->
// "null"); converted reports whether there was one. Without one the value is
// returned as loaded (shared alias containers stay shared).
func stringKeys(value any) (result any, converted bool) {
	if !containsMapping(value, map[any]bool{}) {
		return value, false
	}
	return rebuildStringKeys(value, map[any]any{}), true
}

// containerID identifies a loaded container (aliases share it).
func containerID(value any) (any, bool) {
	switch v := value.(type) {
	case *jsonx.Object:
		return v, v != nil
	case *yamlx.Mapping:
		return v, v != nil
	case []any:
		if len(v) > 0 {
			return &v[0], true
		}
	}
	return nil, false
}

func containsMapping(value any, seen map[any]bool) bool {
	id, ok := containerID(value)
	if !ok {
		return false
	}
	if seen[id] {
		return false
	}
	seen[id] = true
	switch v := value.(type) {
	case *yamlx.Mapping:
		return true
	case *jsonx.Object:
		for _, child := range v.All() {
			if containsMapping(child, seen) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if containsMapping(child, seen) {
				return true
			}
		}
	}
	return false
}

func rebuildStringKeys(value any, memo map[any]any) any {
	id, ok := containerID(value)
	if !ok {
		return value
	}
	if done, ok := memo[id]; ok {
		return done
	}
	switch v := value.(type) {
	case *yamlx.Mapping:
		out := jsonx.NewObject()
		memo[id] = out
		for _, pair := range v.Pairs {
			out.Set(jsonKeyText(pair.Key), rebuildStringKeys(pair.Value, memo))
		}
		return out
	case *jsonx.Object:
		out := jsonx.NewObject()
		memo[id] = out
		for key, child := range v.All() {
			out.Set(key, rebuildStringKeys(child, memo))
		}
		return out
	case []any:
		out := make([]any, len(v))
		memo[id] = out
		for i, child := range v {
			out[i] = rebuildStringKeys(child, memo)
		}
		return out
	}
	return value
}

// jsonKeyText is the text json.dumps writes for a dict key. bytes and dates
// (which json.dumps refuses) use str().
func jsonKeyText(key any) string {
	switch k := key.(type) {
	case string:
		return k
	case nil:
		return "null"
	case bool:
		if k {
			return "true"
		}
		return "false"
	case jsonx.Number:
		return string(k)
	case yamlx.Timestamp:
		return k.String()
	case []byte:
		return jsonx.Repr(string(k))
	}
	return jsonx.Str(key)
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
// to convert it (URI links, base64). Keys that are not strings are written
// as json.dumps writes them (see loadProxyYAML); a YAML document jsonx cannot
// hold at all (a recursive alias, a non-empty !!set/!!omap/!!pairs) is nil.
func ParseStructuredProxyString(proxyString string) *jsonx.Object {
	return parseStructured(proxyString, false)
}

// InspectStructuredProxyString is _parse_structured_proxy_string for callers
// that only read fields (raw_dialer, provider delivery, the repository's
// import rewrite): a YAML document with a recursive alias, which Python
// parses but jsonx cannot hold, is read by the loose loader instead, which
// stands in a truthy one-element list for the recursive container.
func InspectStructuredProxyString(proxyString string) *jsonx.Object {
	return parseStructured(proxyString, true)
}

func parseStructured(proxyString string, loose bool) *jsonx.Object {
	stripped := pystr.Strip(proxyString)
	firstLine, _, _ := strings.Cut(stripped, "\n")
	if strings.Contains(firstLine, "://") {
		before, _, _ := strings.Cut(stripped, "://")
		if !strings.HasSuffix(before, ":") {
			first := pystr.Strip(firstLine)
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
	data, err := loadProxyYAML(stripped)
	var unrepresentable *yamlx.UnrepresentableError
	if err != nil && loose && errors.As(err, &unrepresentable) && unrepresentable.Kind == "recursive" {
		data, err = loadProxyYAMLLoose(stripped)
	}
	if err == nil {
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
	nodeType := pystr.Lower(typeText)
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
		// dict.update accepts an iterable of key/value pairs: any sequence of
		// length 2 (a two-character string, a dict with two keys). A key that
		// is not a string is written as json.dumps writes it (stringKeys).
		for i, item := range params {
			var pair []any
			switch element := item.(type) {
			case []any:
				pair = element
			case string:
				for _, r := range element {
					pair = append(pair, string(r))
				}
			case *jsonx.Object:
				pair = jsonx.ListOf(element.Keys())
			default:
				return nil, pyError("TypeError", "cannot convert dictionary update sequence element #"+
					jsonx.Str(jsonx.Int(i))+" to a sequence")
			}
			if len(pair) != 2 {
				return nil, pyError("ValueError", "dictionary update sequence element #"+jsonx.Str(jsonx.Int(i))+
					" has length "+jsonx.Str(jsonx.Int(len(pair)))+"; 2 is required")
			}
			if err := hashable(pair[0]); err != nil {
				return nil, err
			}
			base.Set(jsonKeyText(pair[0]), pair[1])
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
