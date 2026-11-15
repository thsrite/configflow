// Package trafficstore is the port of backend/agents/traffic_store.py
// (A05): validation and storage of the Mihomo traffic summaries Agents
// report for domain discovery. One JSON file per Agent under
// DATA_DIR/traffic, bucketed by day; only per-domain counters are kept (no
// URL paths, client IPs or processes).
//
// Python created a store object per request and serialised the
// read-modify-write cycles with a module-level lock; the small status cache
// (load_status) is module level too, keyed by the absolute file path. Both
// are package level here.
//
// Domain discovery (C37) reads this package (Get, Load, LoadStatus,
// SplitRouteKey); it must therefore not import internal/discovery.
package trafficstore

import (
	"container/list"
	"context"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"configflow/internal/jsonstore"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/rules"
)

// Report limits and choices (module constants of traffic_store.py).
const (
	ReportSchema           = 1
	ReportMaxContentLength = 256 * 1024
	ReportMaxItems         = 2000
	RetentionDays          = 7

	routeSeparator          = "\t"
	defaultStatusCacheLimit = 128
	statusMaxBytes          = 1024
)

// MaxHostsPerDay is MAX_HOSTS_PER_DAY (a variable: tests lower it, as the
// Python test monkeypatches it).
var MaxHostsPerDay = 5000

var (
	// ReportStatuses, Outlets and FailKinds are REPORT_STATUSES, OUTLETS
	// and FAIL_KINDS.
	ReportStatuses = []string{"running", "no_controller", "auth_failed", "unreachable"}
	Outlets        = []string{"direct", "proxy", "reject"}
	FailKinds      = []string{"timeout", "reset", "refused", "eof", "dns", "other"}

	reportFields = set("schema", "status", "mihomo_version", "window_start", "window_end", "ip_only_conns", "dropped", "items")
	itemFields   = set("host", "port", "network", "rule", "rule_payload", "policy", "outlet",
		"conns", "fails", "fail_kinds", "zero_dl", "up", "down")
	statusFields = set("value", "at", "mihomo_version")
	countFields  = []string{"conns", "fails", "zero_dl", "up", "down"}
	maxCount     = new(big.Int).Lsh(big.NewInt(1), 53)
)

func set(values ...string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		out[value] = true
	}
	return out
}

// TypeError is Python's TypeError (e.g. an unhashable value tested for
// membership in a frozenset); routes answer it with the 500 page.
type TypeError struct{ Message string }

func (e *TypeError) Error() string { return e.Message }

// pyTypeName is type(value).__name__ for JSON values.
func pyTypeName(value any) string {
	switch v := value.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case []any:
		return "list"
	case *jsonx.Object:
		return "dict"
	case jsonx.Number:
		if v.IsInt() {
			return "int"
		}
		return "float"
	}
	return "object"
}

// inFrozenset is `value in frozenset(choices)`: list and dict values are
// unhashable and raise TypeError; other values compare with ==.
func inFrozenset(value any, choices []string) (bool, error) {
	switch value.(type) {
	case []any, *jsonx.Object:
		return false, &TypeError{Message: "unhashable type: '" + pyTypeName(value) + "'"}
	}
	for _, choice := range choices {
		if jsonx.Equal(value, choice) {
			return true, nil
		}
	}
	return false, nil
}

// keysWithin is `not set(mapping) - allowed`.
func keysWithin(object *jsonx.Object, allowed map[string]bool) bool {
	for _, key := range object.Keys() {
		if !allowed[key] {
			return false
		}
	}
	return true
}

// boundedText is _bounded_text(value, max_length, required=...).
func boundedText(value any, maxLength int, required bool) bool {
	if value == nil {
		return !required
	}
	text, ok := value.(string)
	if !ok || utf8.RuneCountInString(text) > maxLength {
		return false
	}
	if required && text == "" {
		return false
	}
	for _, r := range text {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

// count is _count(value): an int (not bool) in [0, 2**53].
func count(value any) bool {
	number, ok := value.(jsonx.Number)
	if !ok {
		return false
	}
	n, ok := number.BigInt()
	return ok && n.Sign() >= 0 && n.Cmp(maxCount) <= 0
}

func getOr(object *jsonx.Object, key string, fallback any) any {
	if value, ok := object.Get(key); ok {
		return value
	}
	return fallback
}

var zero = jsonx.Int(0)

// validItem is _valid_item(item).
func validItem(entry any) (bool, error) {
	item, ok := entry.(*jsonx.Object)
	if !ok || item == nil || !keysWithin(item, itemFields) {
		return false, nil
	}
	host, ok := item.Value("host").(string)
	if !ok || host != pyLowerASCIIOrSelf(host) || rules.IsValidIP(host) || !rules.IsValidDomain(host, false) {
		return false, nil
	}
	port, ok := getOr(item, "port", zero).(jsonx.Number)
	if !ok {
		return false, nil
	}
	if n, isInt := port.BigInt(); !isInt || n.Sign() < 0 || n.Cmp(big.NewInt(65535)) > 0 {
		return false, nil
	}
	network := getOr(item, "network", "tcp")
	if !jsonx.Equal(network, "tcp") && !jsonx.Equal(network, "udp") {
		return false, nil
	}
	known, err := inFrozenset(item.Value("outlet"), Outlets)
	if err != nil || !known {
		return false, err
	}
	// 全局 / 直连模式、或经指定了 proxy 的入站时，Mihomo 不走规则匹配，rule 为空
	if !(boundedText(item.Value("rule"), 64, false) && boundedText(item.Value("rule_payload"), 256, false) &&
		boundedText(item.Value("policy"), 128, true)) {
		return false, nil
	}
	for _, field := range countFields {
		if !count(getOr(item, field, zero)) {
			return false, nil
		}
	}
	failKinds, ok := getOr(item, "fail_kinds", jsonx.NewObject()).(*jsonx.Object)
	if !ok || failKinds == nil || !keysWithin(failKinds, set(FailKinds...)) {
		return false, nil
	}
	for _, value := range failKinds.Values() {
		if !count(value) {
			return false, nil
		}
	}
	return true, nil
}

// pyLowerASCIIOrSelf is host.lower() for the comparison host ==
// host.lower(): a host with non-ASCII characters never passes
// is_valid_domain, so only ASCII lowering decides the outcome.
func pyLowerASCIIOrSelf(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return s
		}
	}
	return strings.ToLower(s)
}

// CleanReport is clean_report(payload): the validated report, or nil when
// a top-level field is out of bounds. A single bad item is only dropped and
// counted in "dropped", so that one odd connection (e.g. an overlong logical
// rule) does not lose the whole 5-minute window. A list or dict "status" or
// item "outlet" raises TypeError (unhashable) as in Python.
func CleanReport(payload any) (*jsonx.Object, error) {
	report, ok := payload.(*jsonx.Object)
	if !ok || report == nil || !keysWithin(report, reportFields) {
		return nil, nil
	}
	if !jsonx.Equal(report.Value("schema"), jsonx.Int(ReportSchema)) {
		return nil, nil
	}
	known, err := inFrozenset(report.Value("status"), ReportStatuses)
	if err != nil || !known {
		return nil, err
	}
	for _, field := range []string{"mihomo_version", "window_start", "window_end"} {
		if !boundedText(report.Value(field), 64, false) {
			return nil, nil
		}
	}
	for _, field := range []string{"ip_only_conns", "dropped"} {
		if !count(getOr(report, field, zero)) {
			return nil, nil
		}
	}
	items, ok := getOr(report, "items", []any{}).([]any)
	if !ok || len(items) > ReportMaxItems {
		return nil, nil
	}
	valid := []any{}
	for _, item := range items {
		good, err := validItem(item)
		if err != nil {
			return nil, err
		}
		if good {
			valid = append(valid, item)
		}
	}
	dropped, _ := getOr(report, "dropped", zero).(jsonx.Number).BigInt()
	dropped.Add(dropped, big.NewInt(int64(len(items)-len(valid))))
	out := report.Clone()
	out.Set("items", valid)
	out.Set("dropped", jsonx.BigInt(dropped))
	return out, nil
}

// RouteKey is route_key(rule, rule_payload, outlet, policy).
func RouteKey(rule, rulePayload, outlet, policy string) string {
	return strings.Join([]string{rule, rulePayload, outlet, policy}, routeSeparator)
}

// SplitRouteKey is split_route_key(key). ok is false where Python's
// four-way unpacking raised ValueError (fewer than three separators).
func SplitRouteKey(key string) (route *jsonx.Object, ok bool) {
	parts := strings.SplitN(key, routeSeparator, 4)
	if len(parts) != 4 {
		return nil, false
	}
	return jsonx.Obj("rule", parts[0], "rule_payload", parts[1], "outlet", parts[2], "policy", parts[3]), true
}

func emptyStats() *jsonx.Object {
	return jsonx.Obj("conns", 0, "fails", 0, "fail_kinds", jsonx.NewObject(), "zero_dl", 0, "up", 0, "down", 0)
}

// ---------- status cache ----------

type cacheEntry struct {
	path      string
	signature jsonstore.Signature
	status    *jsonx.Object
}

var (
	// mu is _LOCK: store objects are made per request, so the lock that
	// serialises read-modify-write cycles is package level.
	mu sync.Mutex
	// statusCache is _STATUS_CACHE (an LRU OrderedDict by absolute path).
	statusCache      = list.New()
	statusIndex      = map[string]*list.Element{}
	statusCacheLimit = defaultStatusCacheLimit
)

// ResetCache empties the status cache (tests: the autouse fixture).
func ResetCache() {
	mu.Lock()
	defer mu.Unlock()
	statusCache = list.New()
	statusIndex = map[string]*list.Element{}
}

// SetCacheLimit changes _STATUS_CACHE_LIMIT and returns the old value
// (tests).
func SetCacheLimit(limit int) int {
	mu.Lock()
	defer mu.Unlock()
	old := statusCacheLimit
	statusCacheLimit = limit
	return old
}

// CachedPaths lists the cached paths, least recently used first (tests).
func CachedPaths() []string {
	mu.Lock()
	defer mu.Unlock()
	var out []string
	for element := statusCache.Front(); element != nil; element = element.Next() {
		out = append(out, element.Value.(*cacheEntry).path)
	}
	return out
}

// CachedStatus is the cached status of path (tests), nil when absent.
func CachedStatus(path string) *jsonx.Object {
	mu.Lock()
	defer mu.Unlock()
	if element, ok := statusIndex[path]; ok {
		return element.Value.(*cacheEntry).status.DeepCopy()
	}
	return nil
}

func cachePop(path string) {
	if element, ok := statusIndex[path]; ok {
		statusCache.Remove(element)
		delete(statusIndex, path)
	}
}

// statusSignature is _status_signature(path): nil when the file cannot be
// stat'ed. (Python skipped the cache on Windows, where ctime is the
// creation time; the server runs on Linux.)
var statusSignature = func(path string) *jsonstore.Signature {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	signature := signatureOf(info)
	return &signature
}

// cacheableStatus is _cacheable_status(status).
func cacheableStatus(status any) bool {
	object, ok := status.(*jsonx.Object)
	if !ok || object == nil || !keysWithin(object, statusFields) {
		return false
	}
	for _, value := range object.Values() {
		if value == nil {
			continue
		}
		text, ok := value.(string)
		if !ok || utf8.RuneCountInString(text) > statusMaxBytes {
			return false
		}
	}
	encoded, err := jsonx.Encode(object, jsonx.Options{})
	return err == nil && len(encoded) <= statusMaxBytes
}

// rememberStatus is _remember_status(path, signature, status): reuse the
// bounded metadata only while its source file is still unchanged.
func rememberStatus(path string, signature *jsonstore.Signature, status any) {
	cachePop(path)
	if signature == nil || !cacheableStatus(status) {
		return
	}
	current := statusSignature(path)
	if current == nil || *current != *signature {
		return
	}
	entry := &cacheEntry{path: path, signature: *signature, status: status.(*jsonx.Object).DeepCopy()}
	statusIndex[path] = statusCache.PushBack(entry)
	for statusCache.Len() > statusCacheLimit {
		oldest := statusCache.Front()
		statusCache.Remove(oldest)
		delete(statusIndex, oldest.Value.(*cacheEntry).path)
	}
}

// ---------- store ----------

// Store is TrafficStore: domain discovery data per Agent, bucketed by day
// and expiring on its own.
type Store struct {
	files *jsonstore.Store

	// ReadJSON and WriteJSON are _read_json and _write_json (verified
	// signature); tests replace them to count reads or play an external
	// writer.
	ReadJSON  func(agentID string) *jsonx.Object
	WriteJSON func(agentID string, data *jsonx.Object) (*jsonstore.Signature, error)
}

// New is TrafficStore(data_dir).
func New(dataDir string) *Store {
	s := &Store{files: jsonstore.New(dataDir)}
	s.ReadJSON = s.files.Read
	s.WriteJSON = func(agentID string, data *jsonx.Object) (*jsonstore.Signature, error) {
		return s.files.Write(agentID, data, true)
	}
	return s
}

// Get is get_traffic_store(): the store under the repository's data
// directory (DATA_DIR/traffic).
func Get(ctx context.Context) (*Store, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	return New(filepath.Join(repository.DataDir(), "traffic")), nil
}

// Path is _path(agent_id).
func (s *Store) Path(agentID string) string { return s.files.Path(agentID) }

func (s *Store) absPath(agentID string) string {
	path, err := filepath.Abs(s.Path(agentID))
	if err != nil {
		return s.Path(agentID)
	}
	return path
}

// read is _read(agent_id); the caller holds mu.
func (s *Store) read(agentID string) *jsonx.Object {
	path := s.absPath(agentID)
	signature := statusSignature(path)
	data := s.ReadJSON(agentID)
	if data == nil {
		cachePop(path)
		return jsonx.Obj("status", nil, "days", jsonx.NewObject())
	}
	if _, ok := data.Value("days").(*jsonx.Object); !ok {
		cachePop(path)
		return jsonx.Obj("status", nil, "days", jsonx.NewObject())
	}
	// A history request has already paid for JSON decoding. Let subsequent
	// status requests reuse its small metadata without retaining the history.
	rememberStatus(path, signature, data.Value("status"))
	return data
}

// isoDate is date.isoformat().
func isoDate(t time.Time) string { return t.Format("2006-01-02") }

// prune is _prune(data, now): drop days older than RETENTION_DAYS (naive
// local dates, compared as text).
func prune(data *jsonx.Object, now time.Time) {
	y, m, d := now.Date()
	oldest := isoDate(time.Date(y, m, d-(RetentionDays-1), 0, 0, 0, 0, time.UTC))
	days := data.GetObject("days")
	for _, day := range days.Keys() {
		if day < oldest {
			days.Delete(day)
		}
	}
}

// AttributeError is Python's AttributeError ('list' object has no attribute
// 'get' and the like) from a hand-edited file.
type AttributeError struct{ Message string }

func (e *AttributeError) Error() string { return e.Message }

// KeyError is Python's KeyError from a hand-edited file.
type KeyError struct{ Key string }

func (e *KeyError) Error() string { return jsonx.Repr(e.Key) }

// pyGetObject is `value.get(...)`-able: value must be a dict.
func asDict(value any) (*jsonx.Object, error) {
	object, ok := value.(*jsonx.Object)
	if !ok || object == nil {
		return nil, &AttributeError{Message: "'" + pyTypeName(value) + "' object has no attribute 'get'"}
	}
	return object, nil
}

// subscript is container[key] for a dict read from a file.
func subscript(container any, key string) (any, error) {
	object, ok := container.(*jsonx.Object)
	if !ok || object == nil {
		if container == nil {
			return nil, &TypeError{Message: "'NoneType' object is not subscriptable"}
		}
		return nil, &TypeError{Message: "'" + pyTypeName(container) + "' object is not subscriptable"}
	}
	value, present := object.Get(key)
	if !present {
		return nil, &KeyError{Key: key}
	}
	return value, nil
}

// addInt is `current + n` for a counter read from a file (int + int, float
// + int, bool + int as in Python).
func addInt(current any, n *big.Int) (any, error) {
	return pyAdd(current, jsonx.BigInt(n))
}

// pyNumber is a JSON value as a Python number: bool and int are ints, the
// rest of the numbers floats; ok is false for any other type.
func pyNumber(value any) (i *big.Int, f float64, isFloat, ok bool) {
	switch v := value.(type) {
	case bool:
		if v {
			return big.NewInt(1), 0, false, true
		}
		return new(big.Int), 0, false, true
	case jsonx.Number:
		if n, isInt := v.BigInt(); isInt {
			return n, 0, false, true
		}
		return nil, v.Float64(), true, true
	}
	return nil, 0, false, false
}

func intToFloat(i *big.Int) float64 {
	f, _ := new(big.Float).SetInt(i).Float64()
	return f
}

// pyAdd is a + b for numbers read from a file or reported: int + int is an
// int, anything with a float a float (float(int) correctly rounded). Other
// types raised TypeError in Python (str + int, None + int, ...; the text of
// str + str or list + list differs, but the outcome — the report fails — is
// the same).
func pyAdd(a, b any) (any, error) {
	ai, af, aFloat, aOK := pyNumber(a)
	bi, bf, bFloat, bOK := pyNumber(b)
	if !aOK || !bOK {
		return nil, &TypeError{Message: "unsupported operand type(s) for +: '" + pyTypeName(a) + "' and '" + pyTypeName(b) + "'"}
	}
	if !aFloat && !bFloat {
		return jsonx.BigInt(new(big.Int).Add(ai, bi)), nil
	}
	if !aFloat {
		af = intToFloat(ai)
	}
	if !bFloat {
		bf = intToFloat(bi)
	}
	return jsonx.Float(af + bf), nil
}

// pyCompare compares two numbers of pyAdd exactly (int against float as
// Python does). NaN weights only come from a hand-edited file: they
// compare equal here, Python's sort order with NaN keys is arbitrary.
func pyCompare(a, b any) int {
	exact := func(value any) *big.Float {
		i, f, isFloat, _ := pyNumber(value)
		if isFloat {
			if f != f {
				return new(big.Float)
			}
			return new(big.Float).SetFloat64(f)
		}
		return new(big.Float).SetInt(i)
	}
	return exact(a).Cmp(exact(b))
}

func bigOf(value any) *big.Int {
	if number, ok := value.(jsonx.Number); ok {
		if n, ok := number.BigInt(); ok {
			return n
		}
	}
	return new(big.Int)
}

// increment is target[key] += n.
func increment(target any, key string, n *big.Int) error {
	current, err := subscript(target, key)
	if err != nil {
		return err
	}
	sum, err := addInt(current, n)
	if err != nil {
		return err
	}
	target.(*jsonx.Object).Set(key, sum)
	return nil
}

// now is datetime.now() (naive local time; tests replace it).
var now = time.Now

// SetNow replaces the clock and returns a function restoring it (tests).
func SetNow(fn func() time.Time) func() {
	previous := now
	now = fn
	return func() { now = previous }
}

// isoSeconds is datetime.isoformat(timespec='seconds') of a naive time.
func isoSeconds(t time.Time) string { return t.Format("2006-01-02T15:04:05") }

// AddReport is add_report(agent_id, report, now): merge one cleaned report
// and return the saved status (a copy). at is nil for datetime.now().
func (s *Store) AddReport(agentID string, report *jsonx.Object, at *time.Time) (*jsonx.Object, error) {
	current := now()
	if at != nil {
		current = *at
	}
	timestamp := isoSeconds(current)
	mu.Lock()
	defer mu.Unlock()
	data := s.read(agentID)
	previousValue := data.Value("status")
	if !jsonx.Truthy(previousValue) {
		previousValue = jsonx.NewObject()
	}
	version := report.Value("mihomo_version")
	if !jsonx.Truthy(version) {
		// 连不上 Mihomo 时拿不到版本，沿用上次的值
		previous, err := asDict(previousValue)
		if err != nil {
			return nil, err
		}
		version = getOr(previous, "mihomo_version", "")
	}
	status := jsonx.Obj("value", report.Value("status"), "at", timestamp, "mihomo_version", version)
	data.Set("status", status)
	days := data.GetObject("days")
	dayValue := days.SetDefault(isoDate(current), jsonx.Obj("ip_only_conns", 0, "dropped", 0, "hosts", jsonx.NewObject()))
	if err := increment(dayValue, "ip_only_conns", bigOf(getOr(report, "ip_only_conns", zero))); err != nil {
		return nil, err
	}
	if err := increment(dayValue, "dropped", bigOf(getOr(report, "dropped", zero))); err != nil {
		return nil, err
	}
	day := dayValue.(*jsonx.Object)
	hostsValue, err := subscript(day, "hosts")
	if err != nil {
		return nil, err
	}
	hosts, _ := hostsValue.(*jsonx.Object)
	items, _ := getOr(report, "items", []any{}).([]any)
	if len(items) > 0 && hosts == nil {
		// hosts.setdefault(...) runs only for a report with items.
		return nil, &AttributeError{Message: "'" + pyTypeName(hostsValue) + "' object has no attribute 'setdefault'"}
	}
	for _, entryValue := range items {
		item := entryValue.(*jsonx.Object)
		host := item.GetString("host")
		entry, err := asSetdefaultTarget(hosts.SetDefault(host, jsonx.Obj("first_seen", timestamp, "last_seen", timestamp, "by_route", jsonx.NewObject())))
		if err != nil {
			return nil, err
		}
		entry.Set("last_seen", timestamp)
		// 端口用于主动探测时选择 http / https，按连接与失败次数计权
		ports, err := asSetdefaultTarget(entry.SetDefault("ports", jsonx.NewObject()))
		if err != nil {
			return nil, err
		}
		portKey := jsonx.Str(getOr(item, "port", zero))
		// ports.get(port_key, 0) + conns + fails, added left to right (a
		// float read from the file rounds after each addition).
		sum, err := addInt(getOr(ports, portKey, zero), bigOf(getOr(item, "conns", zero)))
		if err != nil {
			return nil, err
		}
		if sum, err = addInt(sum, bigOf(getOr(item, "fails", zero))); err != nil {
			return nil, err
		}
		ports.Set(portKey, sum)
		// item['rule'] is not checked by clean_report beyond its type: a
		// missing rule is a KeyError and None a TypeError in str.join.
		ruleValue, present := item.Get("rule")
		if !present {
			return nil, &KeyError{Key: "rule"}
		}
		rule, isString := ruleValue.(string)
		if !isString {
			return nil, &TypeError{Message: "sequence item 0: expected str instance, " + pyTypeName(ruleValue) + " found"}
		}
		rulePayload, _ := item.Value("rule_payload").(string)
		key := RouteKey(rule, rulePayload, item.GetString("outlet"), item.GetString("policy"))
		byRouteValue, err := subscript(entry, "by_route")
		if err != nil {
			return nil, err
		}
		byRoute, err := asSetdefaultTarget(byRouteValue)
		if err != nil {
			return nil, err
		}
		stats, err := asSetdefaultTarget(byRoute.SetDefault(key, emptyStats()))
		if err != nil {
			return nil, err
		}
		for _, field := range countFields {
			if err := increment(stats, field, bigOf(getOr(item, field, zero))); err != nil {
				return nil, err
			}
		}
		failKinds, _ := getOr(item, "fail_kinds", jsonx.NewObject()).(*jsonx.Object)
		statsKinds, err := subscript(stats, "fail_kinds")
		if err != nil {
			return nil, err
		}
		kinds, err := asDict(statsKinds)
		if err != nil {
			return nil, err
		}
		for kind, value := range failKinds.All() {
			sum, err := addInt(getOr(kinds, kind, zero), bigOf(value))
			if err != nil {
				return nil, err
			}
			kinds.Set(kind, sum)
		}
	}
	// len(hosts): a list or str left by a hand edit has a length too (and
	// is kept as it is); other values raised TypeError.
	var count int
	switch value := hostsValue.(type) {
	case *jsonx.Object:
		count = value.Len()
	case []any:
		count = len(value)
	case string:
		count = utf8.RuneCountInString(value)
	default:
		return nil, &TypeError{Message: "object of type '" + pyTypeName(hostsValue) + "' has no len()"}
	}
	if count > MaxHostsPerDay && hosts == nil {
		// sorted(hosts, key=lambda host: _host_weight(hosts[host])) on a
		// list or str: hosts[host] (or the weight of what it finds) fails.
		return nil, &TypeError{Message: pyTypeName(hostsValue) + " indices must be integers"}
	}
	if count > MaxHostsPerDay {
		ranked := hosts.Keys()
		weights := map[string]any{}
		for _, host := range ranked {
			weight, err := hostWeight(hosts.Value(host))
			if err != nil {
				return nil, err
			}
			weights[host] = weight
		}
		// sorted(..., reverse=True) is stable: equal weights keep their order.
		sort.SliceStable(ranked, func(i, j int) bool { return pyCompare(weights[ranked[i]], weights[ranked[j]]) > 0 })
		for _, host := range ranked[MaxHostsPerDay:] {
			hosts.Delete(host)
			if err := increment(day, "dropped", big.NewInt(1)); err != nil {
				return nil, err
			}
		}
	}
	prune(data, current)
	// Invalidate before writing, including a failed/partial write.
	path := s.absPath(agentID)
	cachePop(path)
	written, err := s.WriteJSON(agentID, data)
	if err != nil {
		return nil, err
	}
	// Do not decode the just-written history again merely to obtain
	// status. The descriptor signature belongs to our write, even if an
	// external process replaces the path before this cache is populated.
	rememberStatus(path, written, status)
	return status.DeepCopy(), nil
}

func asSetdefaultTarget(value any) (*jsonx.Object, error) {
	object, ok := value.(*jsonx.Object)
	if !ok || object == nil {
		return nil, &TypeError{Message: "'" + pyTypeName(value) + "' object does not support item assignment"}
	}
	return object, nil
}

// hostWeight is _host_weight(host_entry): connections plus failures over
// every route, sum(stats['conns'] + stats['fails'] for ...) with Python
// number semantics (a float in a hand-edited file makes the weight a float).
func hostWeight(entry any) (any, error) {
	byRoute, err := subscript(entry, "by_route")
	if err != nil {
		return nil, err
	}
	routes, err := asDict(byRoute)
	if err != nil {
		return nil, err
	}
	var total any = zero
	for _, stats := range routes.Values() {
		conns, err := subscript(stats, "conns")
		if err != nil {
			return nil, err
		}
		fails, err := subscript(stats, "fails")
		if err != nil {
			return nil, err
		}
		weight, err := pyAdd(conns, fails)
		if err != nil {
			return nil, err
		}
		if total, err = pyAdd(total, weight); err != nil {
			return nil, err
		}
	}
	return total, nil
}

// LoadStatus is load_status(agent_id): the collection metadata without
// keeping the full history. Only bounded status fields from a valid read
// or a successful write are cached; a replaced or edited file invalidates
// them and failures are never cached.
func (s *Store) LoadStatus(agentID string) any {
	path := s.absPath(agentID)
	mu.Lock()
	defer mu.Unlock()
	signature := statusSignature(path)
	if element, ok := statusIndex[path]; ok && signature != nil && element.Value.(*cacheEntry).signature == *signature {
		statusCache.MoveToBack(element)
		return element.Value.(*cacheEntry).status.DeepCopy()
	}
	cachePop(path)
	return s.read(agentID).Value("status")
}

// StatusOf adapts LoadStatus to subs.StatusLoader: the status when it is a
// dict, nil otherwise.
func (s *Store) StatusOf(agentID string) *jsonx.Object {
	status, _ := s.LoadStatus(agentID).(*jsonx.Object)
	return status
}

// Load is load(agent_id, now): the whole document, days older than the
// retention dropped (in the returned copy only). at nil = now.
func (s *Store) Load(agentID string, at *time.Time) *jsonx.Object {
	mu.Lock()
	data := s.read(agentID)
	mu.Unlock()
	current := now()
	if at != nil {
		current = *at
	}
	prune(data, current)
	return data
}

// Delete is delete(agent_id): forget the cached status and remove the file
// (a missing file is fine; other failures are returned as Python's OSError
// text).
func (s *Store) Delete(agentID string) error {
	mu.Lock()
	defer mu.Unlock()
	cachePop(s.absPath(agentID))
	path := s.Path(agentID)
	// os.remove is unlink: never removes a directory.
	if err := syscall.Unlink(path); err != nil && !errors.Is(err, syscall.ENOENT) {
		return &OSError{Errno: err.(syscall.Errno), Path: path}
	}
	return nil
}

// OSError is str(OSError) of a failed call on Path:
// "[Errno 21] Is a directory: '/data/traffic/a.json'".
type OSError struct {
	Errno syscall.Errno
	Path  string
}

func (e *OSError) Error() string {
	text := e.Errno.Error()
	if text != "" {
		text = strings.ToUpper(text[:1]) + text[1:]
	}
	return "[Errno " + strconv.Itoa(int(e.Errno)) + "] " + text + ": " + jsonx.Repr(e.Path)
}

func (e *OSError) Unwrap() error { return e.Errno }

// CtimeNs is st_ctime_ns of info (tests).
func CtimeNs(info os.FileInfo) int64 { return signatureOf(info).CtimeNs }
