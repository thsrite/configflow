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

	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"time"
	"unicode/utf8"

	"bytes"
	"configflow/internal/jsonstore"
	"encoding/json"

	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
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

// ---------- status cache ----------

type cacheEntry struct {
	path      string
	signature jsonstore.Signature
	status    json.RawMessage
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
func cacheableStatus(status json.RawMessage) bool {
	if len(status) == 0 || len(status) > statusMaxBytes {
		return false
	}
	var object map[string]json.RawMessage
	if jsondecode.Unmarshal(status, &object) != nil || object == nil {
		return false
	}
	for name, raw := range object {
		if !statusFields[name] {
			return false
		}
		if bytes.Equal(raw, []byte("null")) {
			continue
		}
		var value string
		if jsondecode.Unmarshal(raw, &value) != nil || utf8.RuneCountInString(value) > statusMaxBytes {
			return false
		}
	}
	return true
}

// rememberStatus is _remember_status(path, signature, status): reuse the
// bounded metadata only while its source file is still unchanged.
func rememberStatus(path string, signature *jsonstore.Signature, status json.RawMessage) {
	cachePop(path)
	if signature == nil || !cacheableStatus(status) {
		return
	}
	current := statusSignature(path)
	if current == nil || *current != *signature {
		return
	}
	entry := &cacheEntry{path: path, signature: *signature, status: bytes.Clone(status)}
	statusIndex[path] = statusCache.PushBack(entry)
	for statusCache.Len() > statusCacheLimit {
		oldest := statusCache.Front()
		statusCache.Remove(oldest)
		delete(statusIndex, oldest.Value.(*cacheEntry).path)
	}
}

// Status contains bounded collection metadata. Opaque reads remain available for legacy files.
type Status struct {
	Value         string          `json:"value"`
	At            string          `json:"at"`
	MihomoVersion json.RawMessage `json:"mihomo_version"`
}

// Report is a validated domain-discovery report; raw members preserve optional fields.
type Report struct {
	Status               string
	MihomoVersion        json.RawMessage
	IPOnlyConns, Dropped *big.Int
	Items                []ReportItem
	raw                  model.Fields
}

// ReportItem is one validated connection aggregate.
type ReportItem struct {
	Host              string
	Port              int64
	Rule, RulePayload *string
	rulePresent       bool
	Policy, Outlet    string
	Counts            [5]*big.Int
	FailKinds         map[string]*big.Int
	raw               json.RawMessage
}

// MarshalJSON retains the released cleaned-report shape, including omitted optional fields.
func (r Report) MarshalJSON() ([]byte, error) {
	fields := r.raw
	items := make([]json.RawMessage, len(r.Items))
	for i, item := range r.Items {
		items[i] = item.raw
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	if err = fields.Set("items", raw); err != nil {
		return nil, err
	}
	if err = fields.Set("dropped", json.RawMessage(r.Dropped.String())); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}
func bounded(raw json.RawMessage, limit int, required bool) bool {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return !required
	}
	var value string
	if jsondecode.Unmarshal(raw, &value) != nil || utf8.RuneCountInString(value) > limit || required && value == "" {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func member(fields model.Fields, name string) json.RawMessage { raw, _ := fields.Get(name); return raw }
func str(raw json.RawMessage) string {
	var text string
	_ = jsondecode.Unmarshal(raw, &text)
	return text
}
func countJSON(raw json.RawMessage, defaultZero bool) (*big.Int, bool) {
	if len(raw) == 0 && defaultZero {
		return new(big.Int), true
	}
	text := string(raw)
	if text == "" {
		return nil, false
	}
	for i, r := range text {
		if i == 0 && r == '-' {
			continue
		}
		if r < '0' || r > '9' {
			return nil, false
		}
	}
	n, ok := new(big.Int).SetString(text, 10)
	return n, ok && n.Sign() >= 0 && n.Cmp(maxCount) <= 0
}
func oneOf(raw json.RawMessage, choices []string) (bool, error) {
	if len(raw) > 0 && (raw[0] == '[' || raw[0] == '{') {
		kind := "list"
		if raw[0] == '{' {
			kind = "dict"
		}
		return false, &TypeError{Message: "unhashable type: '" + kind + "'"}
	}
	var value string
	if jsondecode.Unmarshal(raw, &value) != nil {
		return false, nil
	}
	for _, choice := range choices {
		if value == choice {
			return true, nil
		}
	}
	return false, nil
}
func within(f model.Fields, allowed map[string]bool) bool {
	for key := range f.All() {
		if !allowed[key] {
			return false
		}
	}
	return true
}

// DecodeReport validates exact Agent JSON keys and drops individual malformed items.
func DecodeReport(raw json.RawMessage) (*Report, error) {
	var f model.Fields
	if jsondecode.Unmarshal(raw, &f) != nil || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' || !within(f, reportFields) {
		return nil, nil
	}
	schema := member(f, "schema")
	var version float64
	if string(schema) == "true" {
		version = 1
	} else if jsondecode.Unmarshal(schema, &version) != nil {
		return nil, nil
	}
	if version != 1 {
		return nil, nil
	}
	known, err := oneOf(member(f, "status"), ReportStatuses)
	if err != nil || !known {
		return nil, err
	}
	for _, name := range []string{"mihomo_version", "window_start", "window_end"} {
		if !bounded(member(f, name), 64, false) {
			return nil, nil
		}
	}
	ip, ok := countJSON(member(f, "ip_only_conns"), true)
	if !ok {
		return nil, nil
	}
	dropped, ok := countJSON(member(f, "dropped"), true)
	if !ok {
		return nil, nil
	}
	items := []json.RawMessage{}
	if v := member(f, "items"); len(v) > 0 {
		if jsondecode.Unmarshal(v, &items) != nil || items == nil {
			return nil, nil
		}
	}
	if len(items) > ReportMaxItems {
		return nil, nil
	}
	report := &Report{Status: str(member(f, "status")), MihomoVersion: member(f, "mihomo_version"), IPOnlyConns: ip, Dropped: dropped, Items: []ReportItem{}, raw: f}
	for _, raw := range items {
		item, err := decodeItem(raw)
		if err != nil {
			return nil, err
		}
		if item == nil {
			report.Dropped.Add(report.Dropped, big.NewInt(1))
		} else {
			report.Items = append(report.Items, *item)
		}
	}
	return report, nil
}
func decodeItem(raw json.RawMessage) (*ReportItem, error) {
	var f model.Fields
	if jsondecode.Unmarshal(raw, &f) != nil || len(raw) == 0 || raw[0] != '{' || !within(f, itemFields) {
		return nil, nil
	}
	host := str(member(f, "host"))
	if host == "" || host != strings.ToLower(host) || rules.IsValidIP(host) || !rules.IsValidDomain(host, false) {
		return nil, nil
	}
	port, ok := countJSON(member(f, "port"), true)
	if !ok || !port.IsInt64() || port.Int64() > 65535 {
		return nil, nil
	}
	network := member(f, "network")
	if len(network) > 0 && str(network) != "tcp" && str(network) != "udp" {
		return nil, nil
	}
	known, err := oneOf(member(f, "outlet"), Outlets)
	if err != nil || !known {
		return nil, err
	}
	if !bounded(member(f, "rule"), 64, false) || !bounded(member(f, "rule_payload"), 256, false) || !bounded(member(f, "policy"), 128, true) {
		return nil, nil
	}
	item := &ReportItem{Host: host, Port: port.Int64(), Policy: str(member(f, "policy")), Outlet: str(member(f, "outlet")), raw: bytes.Clone(raw), FailKinds: map[string]*big.Int{}}
	item.rulePresent = f.Has("rule")
	if v := member(f, "rule"); len(v) > 0 && string(v) != "null" {
		text := str(v)
		item.Rule = &text
	}
	if v := member(f, "rule_payload"); len(v) > 0 && string(v) != "null" {
		text := str(v)
		item.RulePayload = &text
	}
	for i, name := range countFields {
		n, ok := countJSON(member(f, name), true)
		if !ok {
			return nil, nil
		}
		item.Counts[i] = n
	}
	kinds := model.Fields{}
	if v := member(f, "fail_kinds"); len(v) > 0 {
		if v[0] != '{' || jsondecode.Unmarshal(v, &kinds) != nil {
			return nil, nil
		}
	}
	for key, raw := range kinds.All() {
		if !set(FailKinds...)[key] {
			return nil, nil
		}
		n, ok := countJSON(raw, false)
		if !ok {
			return nil, nil
		}
		item.FailKinds[key] = n
	}
	return item, nil
}

// Route identifies one Mihomo rule decision.
type Route struct {
	Rule        string `json:"rule"`
	RulePayload string `json:"rule_payload"`
	Outlet      string `json:"outlet"`
	Policy      string `json:"policy"`
}

// RouteKey encodes a rule decision using the released on-disk separator.
func RouteKey(rule, rulePayload, outlet, policy string) string {
	return strings.Join([]string{rule, rulePayload, outlet, policy}, routeSeparator)
}

// ParseRouteKey decodes a persisted rule decision.
func ParseRouteKey(key string) (Route, bool) {
	p := strings.SplitN(key, routeSeparator, 4)
	if len(p) != 4 {
		return Route{}, false
	}
	return Route{p[0], p[1], p[2], p[3]}, true
}

// NativeStore owns native JSON persistence and a shared bounded status cache.
type NativeStore struct {
	files     *jsonstore.Store
	readJSON  func(string) json.RawMessage
	writeJSON func(context.Context, string, json.RawMessage) (*jsonstore.Signature, error)
}

// NewNative opens the domain-discovery store without legacy JSON conversions.
func NewNative(dataDir string) *NativeStore {
	s := &NativeStore{files: jsonstore.New(dataDir)}
	s.readJSON = func(id string) json.RawMessage { var raw json.RawMessage; s.files.Read(id, &raw); return raw }
	s.writeJSON = func(ctx context.Context, id string, raw json.RawMessage) (*jsonstore.Signature, error) {
		return s.files.WriteContext(ctx, id, raw, true)
	}
	return s
}

// GetNative opens the domain-discovery store in the current repository.
func GetNative(ctx context.Context) (*NativeStore, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	return NewNative(filepath.Join(r.DataDir(), "traffic")), nil
}

// Path returns the safe per-Agent file path.
func (s *NativeStore) Path(id string) string { return s.files.Path(id) }
func (s *NativeStore) absPath(id string) string {
	p, err := filepath.Abs(s.Path(id))
	if err != nil {
		return s.Path(id)
	}
	return p
}

// Document retains unknown persisted members; typed merge code opens only the records it updates.
type Document struct {
	fields model.Fields
	days   model.Fields
}

// MarshalJSON encodes the existing traffic file format without discarding extension members.
func (d Document) MarshalJSON() ([]byte, error) {
	f := d.fields
	raw, err := json.Marshal(d.days)
	if err != nil {
		return nil, err
	}
	if err = f.Set("days", raw); err != nil {
		return nil, err
	}
	return json.Marshal(f)
}
func emptyDocument() Document {
	var f model.Fields
	_ = f.Set("status", json.RawMessage("null"))
	return Document{fields: f}
}
func (s *NativeStore) read(id string) (Document, error) {
	path := s.absPath(id)
	signature := statusSignature(path)
	raw := s.readJSON(id)
	if len(raw) == 0 {
		cachePop(path)
		return emptyDocument(), nil
	}
	var f model.Fields
	if err := jsondecode.Unmarshal(raw, &f); err != nil {
		cachePop(path)
		return Document{}, err
	}
	days := member(f, "days")
	var d model.Fields
	if len(days) == 0 || string(days) == "null" {
		cachePop(path)
		return emptyDocument(), nil
	}
	if days[0] != '{' || jsondecode.Unmarshal(days, &d) != nil {
		cachePop(path)
		return Document{}, &TypeError{Message: "invalid traffic days"}
	}
	rememberStatus(path, signature, member(f, "status"))
	return Document{fields: f, days: d}, nil
}
func prune(d *Document, at time.Time) {
	oldest := at.AddDate(0, 0, -(RetentionDays - 1)).Format("2006-01-02")
	for key := range d.days.All() {
		if key < oldest {
			d.days.Delete(key)
		}
	}
}

// number adds exact integers and retains REAL counters written by earlier versions.
func addNumber(raw json.RawMessage, n *big.Int) (json.RawMessage, error) {
	value, err := parseCounter(raw)
	if err != nil {
		return nil, err
	}
	sum, err := value.add(storedCounter{integer: n})
	if err != nil {
		return nil, err
	}
	return sum.raw(), nil
}
func object(raw json.RawMessage) (model.Fields, error) {
	var f model.Fields
	if len(raw) == 0 || raw[0] != '{' || jsondecode.Unmarshal(raw, &f) != nil {
		return f, &TypeError{Message: "invalid traffic object"}
	}
	return f, nil
}
func put(f *model.Fields, name string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return f.Set(name, raw)
}
func inc(f *model.Fields, name string, n *big.Int) error {
	raw, err := addNumber(member(*f, name), n)
	if err != nil {
		return err
	}
	return f.Set(name, raw)
}
func defaultObject(f model.Fields, key string, raw string) (model.Fields, error) {
	value, exists := f.Get(key)
	if !exists {
		value = json.RawMessage(raw)
	}
	return object(value)
}
func weight(raw json.RawMessage) (*big.Rat, error) {
	entry, err := object(raw)
	if err != nil {
		return nil, err
	}
	routes, err := object(member(entry, "by_route"))
	if err != nil {
		return nil, err
	}
	total := storedCounter{integer: new(big.Int)}
	for _, raw := range routes.All() {
		stats, err := object(raw)
		if err != nil {
			return nil, err
		}
		conns, err := parseCounter(member(stats, "conns"))
		if err != nil {
			return nil, err
		}
		fails, err := parseCounter(member(stats, "fails"))
		if err != nil {
			return nil, err
		}
		sum, err := conns.add(fails)
		if err != nil {
			return nil, err
		}
		total, err = total.add(sum)
		if err != nil {
			return nil, err
		}
	}
	return total.rat(), nil
}

var now = time.Now

// SetNow replaces the store clock for tests.
func SetNow(fn func() time.Time) func() { old := now; now = fn; return func() { now = old } }

// Add merges a validated report atomically, preserving the work lifetime through file replacement.
func (s *NativeStore) Add(ctx context.Context, id string, r *Report, at *time.Time) (result *Status, resultErr error) {
	defer func() {
		if resultErr != nil && ctx.Err() == nil {
			warnStore(s.absPath(id), "bucket:"+func() string {
				if at != nil {
					return at.Format("2006-01-02")
				}
				return now().Format("2006-01-02")
			}())
		}
	}()
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.IPOnlyConns == nil || r.Dropped == nil {
		return nil, &TypeError{Message: "missing traffic report"}
	}
	current := now()
	if at != nil {
		current = *at
	}
	stamp := current.Format("2006-01-02T15:04:05")
	dayKey := current.Format("2006-01-02")
	mu.Lock()
	defer mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d, err := s.read(id)
	if err != nil {
		return nil, err
	}
	previous := member(d.fields, "status")
	if len(previous) > 0 && string(previous) != "null" {
		f, err := object(previous)
		if err != nil {
			return nil, err
		}
		for _, key := range []string{"value", "at", "mihomo_version"} {
			raw, exists := f.Get(key)
			if exists && string(raw) != "null" {
				var value string
				if jsondecode.Unmarshal(raw, &value) != nil {
					return nil, &TypeError{Message: "invalid traffic status member"}
				}
			}
		}
	}
	version := r.MihomoVersion
	if len(version) == 0 || string(version) == "null" || string(version) == `""` {
		version = json.RawMessage(`""`)
		previous := member(d.fields, "status")
		if len(previous) > 0 && string(previous) != "null" {
			f, err := object(previous)
			if err != nil {
				return nil, err
			}
			if raw, ok := f.Get("mihomo_version"); ok {
				version = raw
			}
		}
	}
	status := &Status{Value: r.Status, At: stamp, MihomoVersion: version}
	day, err := defaultObject(d.days, dayKey, `{"ip_only_conns":0,"dropped":0,"hosts":{}}`)
	if err != nil {
		return nil, err
	}
	if err = inc(&day, "ip_only_conns", r.IPOnlyConns); err != nil {
		return nil, err
	}
	if err = inc(&day, "dropped", r.Dropped); err != nil {
		return nil, err
	}
	hosts, err := readRecords(member(day, "hosts"))
	if err != nil {
		return nil, err
	}
	modified := map[string]*hostMerge{}
	var modifiedOrder []string
	for _, item := range r.Items {
		for _, count := range item.Counts {
			if count == nil {
				return nil, &TypeError{Message: "missing cleaned item counter"}
			}
		}
		if item.Rule == nil {
			if item.rulePresent {
				return nil, &TypeError{Message: "sequence item 0: expected str instance, NoneType found"}
			}
			return nil, &KeyError{Key: "rule"}
		}
		host := modified[item.Host]
		if host == nil {
			host, err = openHost(hosts, item.Host, stamp)
			if err != nil {
				return nil, err
			}
			modified[item.Host] = host
			modifiedOrder = append(modifiedOrder, item.Host)
		}
		ports := &host.ports
		key := strconv.FormatInt(item.Port, 10)
		value := recordMember(*ports, key)
		if len(value) == 0 {
			value = []byte("0")
		}
		for _, n := range []*big.Int{item.Counts[0], item.Counts[1]} {
			value, err = addNumber(value, n)
			if err != nil {
				return nil, err
			}
		}
		ports.setEncoded(key, value)
		routes := &host.routes
		payload := ""
		if item.RulePayload != nil {
			payload = *item.RulePayload
		}
		routeKey := RouteKey(*item.Rule, payload, item.Outlet, item.Policy)
		statsRaw, exists := routes.get(routeKey)
		if !exists {
			statsRaw = []byte(`{"conns":0,"fails":0,"zero_dl":0,"up":0,"down":0,"fail_kinds":{}}`)
		}
		stats, err := readRecords(statsRaw)
		if err != nil {
			return nil, err
		}
		for i, key := range countFields {
			if err = incRecord(&stats, key, item.Counts[i]); err != nil {
				return nil, err
			}
		}
		kinds, err := readRecords(recordMember(stats, "fail_kinds"))
		if err != nil {
			return nil, err
		}
		for key, n := range item.FailKinds {
			value := recordMember(kinds, key)
			if len(value) == 0 {
				value = []byte("0")
			}
			value, err = addNumber(value, n)
			if err != nil {
				return nil, err
			}
			kinds.setEncoded(key, value)
		}
		if err = recordPut(&stats, "fail_kinds", kinds); err != nil {
			return nil, err
		}
		if err = recordPut(routes, routeKey, stats); err != nil {
			return nil, err
		}
	}
	for _, name := range modifiedOrder {
		host := modified[name]
		if err = recordPut(&host.entry, "ports", host.ports); err != nil {
			return nil, err
		}
		if err = recordPut(&host.entry, "by_route", host.routes); err != nil {
			return nil, err
		}
		if err = recordPut(&hosts, name, host.entry); err != nil {
			return nil, err
		}
	}
	if hosts.len() > MaxHostsPerDay {
		keys := hosts.keys()
		weights := map[string]*big.Rat{}
		for _, key := range keys {
			w, err := weight(recordMember(hosts, key))
			if err != nil {
				return nil, err
			}
			weights[key] = w
		}
		sort.SliceStable(keys, func(i, j int) bool { return weights[keys[i]].Cmp(weights[keys[j]]) > 0 })
		for _, key := range keys[MaxHostsPerDay:] {
			hosts.delete(key)
			if err = inc(&day, "dropped", big.NewInt(1)); err != nil {
				return nil, err
			}
		}
	}
	if err = put(&day, "hosts", hosts); err != nil {
		return nil, err
	}
	if err = put(&d.days, dayKey, day); err != nil {
		return nil, err
	}
	if err = put(&d.fields, "status", status); err != nil {
		return nil, err
	}
	prune(&d, current)
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	path := s.absPath(id)
	cachePop(path)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	signature, err := s.writeJSON(ctx, id, raw)
	if err != nil {
		return nil, err
	}
	statusRaw, err := json.Marshal(status)
	if err != nil {
		return nil, err
	}
	rememberStatus(path, signature, statusRaw)
	return status, nil
}

// StatusJSON returns independent status metadata without retaining the full history.
func (s *NativeStore) StatusJSON(id string) json.RawMessage {
	mu.Lock()
	defer mu.Unlock()
	path := s.absPath(id)
	signature := statusSignature(path)
	if e, ok := statusIndex[path]; ok && signature != nil && e.Value.(*cacheEntry).signature == *signature {
		statusCache.MoveToBack(e)
		return bytes.Clone(e.Value.(*cacheEntry).status)
	}
	cachePop(path)
	d, err := s.read(id)
	if err != nil {
		return nil
	}
	return member(d.fields, "status")
}

// LoadDocument reads and prunes the returned copy without writing the file.
func (s *NativeStore) LoadDocument(id string, at *time.Time) Document {
	mu.Lock()
	d, err := s.read(id)
	mu.Unlock()
	if err != nil {
		return emptyDocument()
	}
	current := now()
	if at != nil {
		current = *at
	}
	prune(&d, current)
	for date, raw := range d.days.All() {
		day, ok := readableDay(raw)
		if !ok {
			d.days.Delete(date)
			warnStore(s.absPath(id), date)
			continue
		}
		if err := put(&d.days, date, day); err != nil {
			d.days.Delete(date)
		}
	}
	return d
}

// DeleteContext invalidates metadata and removes the per-Agent file while the work lifetime is live.
func (s *NativeStore) DeleteContext(ctx context.Context, id string) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	cachePop(s.absPath(id))
	return s.files.RemoveContext(ctx, id)
}

// CtimeNs returns the cache change-time signature for tests.
func CtimeNs(info os.FileInfo) int64 { return signatureOf(info).CtimeNs }

func decodeCleanedReport(raw json.RawMessage) (*Report, error) {
	var f model.Fields
	if err := jsondecode.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	if !f.Has("schema") {
		if err := f.Set("schema", json.RawMessage("1")); err != nil {
			return nil, err
		}
	}
	dropped := member(f, "dropped")
	if len(dropped) == 0 {
		dropped = []byte("0")
	}
	n, ok := new(big.Int).SetString(string(dropped), 10)
	if !ok || n.Sign() < 0 {
		return nil, &TypeError{Message: "invalid cleaned dropped counter"}
	}
	if err := f.Set("dropped", json.RawMessage("0")); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	report, err := DecodeReport(raw)
	if report != nil {
		report.Dropped = n
	}
	return report, err
}
