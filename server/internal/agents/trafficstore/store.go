// Package trafficstore is the port of backend/agents/traffic_store.py
// (A05): validation and storage of the Mihomo traffic summaries Agents
// report for domain discovery. One JSON file per Agent under
// DATA_DIR/traffic, bucketed by day; only per-domain counters are kept (no
// URL paths, client IPs or processes).
//
// Python created a store object per request and serialised the
// read-modify-write cycles with a module-level lock. The lock and signed
// file cache are shared here as well, keyed by absolute path.
//
// Domain discovery (C37) reads this package (Get, Load, LoadStatus,
// ParseRouteKey); it must therefore not import internal/discovery.
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

	"time"
	"unicode/utf8"

	"bytes"
	"configflow/internal/jsonstore"
	"encoding/json"
	"encoding/json/jsontext"

	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
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

// StoreState describes whether an Agent's traffic file can accept new reports.
type StoreState string

const (
	// StoreStateOK indicates the traffic store is healthy or not yet created.
	StoreStateOK StoreState = "ok"
	// StoreStateDamaged indicates the traffic file is corrupted and writes are refused.
	StoreStateDamaged StoreState = "damaged"
)

// StatusSnapshot pairs an Agent's collection metadata with its traffic-store health.
type StatusSnapshot struct {
	Status json.RawMessage
	State  StoreState
}

// ---------- status cache ----------

type cacheEntry struct {
	path      string
	signature jsonstore.Signature
	status    json.RawMessage
	idle      *idleHistory
	document  *Document
}

type damagedEntry struct {
	path      string
	signature jsonstore.Signature
	dayKey    string
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
	// damagedCache remembers damaged file signatures so status polls stay cheap
	// without changing statusCache's cached-path contract.
	damagedCache = list.New()
	damagedIndex = map[string]*list.Element{}
)

// ResetCache empties the status and damaged-state caches (tests: the autouse fixture).
func ResetCache() {
	mu.Lock()
	defer mu.Unlock()
	statusCache = list.New()
	statusIndex = map[string]*list.Element{}
	damagedCache = list.New()
	damagedIndex = map[string]*list.Element{}
}

func cachePop(path string) {
	if element, ok := statusIndex[path]; ok {
		statusCache.Remove(element)
		delete(statusIndex, path)
	}
}

func damagedPop(path string) {
	if element, ok := damagedIndex[path]; ok {
		damagedCache.Remove(element)
		delete(damagedIndex, path)
	}
}

func rememberDamaged(path string, signature *jsonstore.Signature, dayKey string, status json.RawMessage) {
	damagedPop(path)
	if signature == nil {
		return
	}
	current := statusSignature(path)
	if current == nil || *current != *signature {
		return
	}
	var cached json.RawMessage
	if cacheableStatus(status) {
		cached = bytes.Clone(status)
	}
	entry := &damagedEntry{path: path, signature: *signature, dayKey: dayKey, status: cached}
	damagedIndex[path] = damagedCache.PushBack(entry)
	for damagedCache.Len() > statusCacheLimit {
		oldest := damagedCache.Front()
		damagedCache.Remove(oldest)
		delete(damagedIndex, oldest.Value.(*damagedEntry).path)
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
func rememberStatus(path string, signature *jsonstore.Signature, status json.RawMessage) *cacheEntry {
	if signature == nil || !cacheableStatus(status) {
		cachePop(path)
		return nil
	}
	current := statusSignature(path)
	if current == nil || *current != *signature {
		cachePop(path)
		return nil
	}
	if e := statusIndex[path]; e != nil {
		entry := e.Value.(*cacheEntry)
		if entry.signature == *signature && bytes.Equal(entry.status, status) {
			statusCache.MoveToBack(e)
			return entry
		}
		cachePop(path)
	}
	entry := &cacheEntry{path: path, signature: *signature, status: bytes.Clone(status)}
	statusIndex[path] = statusCache.PushBack(entry)
	for statusCache.Len() > statusCacheLimit {
		oldest := statusCache.Front()
		statusCache.Remove(oldest)
		delete(statusIndex, oldest.Value.(*cacheEntry).path)
	}
	return entry
}

// Status contains bounded collection metadata. Opaque reads remain available for legacy files.
type Status struct {
	Value         string          `json:"value"`
	At            string          `json:"at"`
	MihomoVersion json.RawMessage `json:"mihomo_version"`
}

func member(fields model.Fields, name string) json.RawMessage { raw, _ := fields.Get(name); return raw }

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

// NativeStore owns native JSON persistence and a bounded cache of signed writes.
type NativeStore struct {
	files     *jsonstore.Store
	readJSON  func(string) (json.RawMessage, error)
	writeJSON func(context.Context, string, json.RawMessage) (*jsonstore.Signature, error)
}

// NewNative opens the domain-discovery store without legacy JSON conversions.
func NewNative(dataDir string) *NativeStore {
	s := &NativeStore{files: jsonstore.New(dataDir)}
	s.readJSON = func(id string) (json.RawMessage, error) {
		raw, err := os.ReadFile(s.Path(id))
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		// A missing file is nil; an existing empty file is corrupt, not missing.
		if len(raw) == 0 {
			return json.RawMessage{}, nil
		}
		return raw, nil
	}
	s.writeJSON = func(ctx context.Context, id string, raw json.RawMessage) (*jsonstore.Signature, error) {
		return s.files.WriteEncoded(ctx, id, raw, true)
	}
	return s
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
	revision   DocumentRevision
	fields     records
	days       records
	bucket     *mergeBucket
	storeState StoreState
}

// MarshalJSON encodes the existing traffic file format without discarding extension members.
func (d Document) MarshalJSON() ([]byte, error) {
	fields := d.fields.clone()
	fields.setEncoded("days", nil)
	return json.Marshal(json.RawMessage(fields.encodeObject("days", &d.days)))
}

func emptyDocument() Document {
	var f records
	f.setEncoded("status", json.RawMessage("null"))
	return Document{fields: f}
}

func (d Document) clone() Document {
	d.fields, d.days = d.fields.clone(), d.days.clone()
	return d
}

func (s *NativeStore) read(id, path string, signature *jsonstore.Signature, forMerge bool) (Document, error) {
	if e := statusIndex[path]; e != nil && signature != nil {
		cached := e.Value.(*cacheEntry)
		if cached.signature == *signature && cached.document != nil {
			statusCache.MoveToBack(e)
			return cached.document.clone(), nil
		}
	}
	raw, err := s.readJSON(id)
	if err != nil {
		cachePop(path)
		return Document{}, err
	}
	if raw == nil {
		cachePop(path)
		return emptyDocument(), nil
	}
	raw = bytes.TrimSpace(raw)
	// The root decoder verifies UTF-8 and unique names throughout the
	// document. Nested indexes can then borrow spans of the validated bytes.
	var reader recordReader
	f, err := reader.index(raw)
	if err != nil {
		cachePop(path)
		return Document{}, invalidData("invalid traffic days")
	}
	days := recordMember(f, "days")
	if len(days) == 0 || string(days) == "null" {
		cachePop(path)
		return emptyDocument(), nil
	}
	if forMerge {
		// The returned status may inherit the exact previous version literal.
		// Compact only the other fields, as json.Marshal would at publication.
		for key, v := range f.values {
			if key == "status" {
				continue
			}
			encoded, err := jsontext.AppendFormat(make([]byte, 0, len(v)), v, jsontext.AllowDuplicateNames(true), jsontext.PreserveRawStrings(true), jsontext.EscapeForJS(true), jsontext.EscapeForHTML(true))
			if err != nil {
				return Document{}, err
			}
			f.values[key] = encoded
		}
		days = recordMember(f, "days")
	}
	d, err := reader.read(days)
	if err != nil {
		cachePop(path)
		return Document{}, invalidData("invalid traffic days")
	}
	rememberStatus(path, signature, recordMember(f, "status"))
	f.setEncoded("days", nil)
	return Document{fields: f, days: d}, nil
}
func prune(d *Document, at time.Time) {
	oldest := at.AddDate(0, 0, -(RetentionDays - 1)).Format("2006-01-02")
	for key := range d.days.values {
		if key < oldest {
			d.days.delete(key)
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
		return f, invalidData("invalid traffic object")
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

// mergeBucket retains the last successfully written day's member order and
// host index. A merge edits private maps until its atomic publication succeeds.
type mergeBucket struct {
	key        string
	day, hosts records
}

func (d Document) openBucket(reader *recordReader, key string, r *Report) (records, records, error) {
	var day, hosts records
	cached := d.bucket != nil && d.bucket.key == key
	if cached {
		day = d.bucket.day.clone()
	} else {
		raw, ok := d.days.get(key)
		if !ok {
			raw = []byte(`{"ip_only_conns":0,"dropped":0,"hosts":{}}`)
		}
		var err error
		day, err = reader.read(raw)
		if err != nil {
			return records{}, records{}, invalidData("invalid traffic object")
		}
	}
	if err := incRecord(&day, "ip_only_conns", r.IPOnlyConns); err != nil {
		return records{}, records{}, err
	}
	if err := incRecord(&day, "dropped", r.Dropped); err != nil {
		return records{}, records{}, err
	}
	if cached {
		return day, d.bucket.hosts.clone(), nil
	}
	hosts, err := reader.read(recordMember(day, "hosts"))
	return day, hosts, err
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

func validateStatusFields(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	f, err := object(raw)
	if err != nil {
		return err
	}
	for _, key := range []string{"value", "at", "mihomo_version"} {
		valueRaw, exists := f.Get(key)
		if exists && string(valueRaw) != "null" {
			var value string
			if jsondecode.Unmarshal(valueRaw, &value) != nil {
				return invalidData("invalid traffic status member")
			}
		}
	}
	return nil
}

func validateDayBucketHeader(days records, dayKey string) error {
	rawDay, exists := days.get(dayKey)
	if !exists {
		return nil
	}
	day, err := object(rawDay)
	if err != nil {
		return err
	}
	for _, key := range []string{"ip_only_conns", "dropped"} {
		if value, ok := day.Get(key); ok {
			if _, err := parseCounter(value); err != nil {
				return err
			}
		}
	}
	hostsRaw := bytes.TrimSpace(member(day, "hosts"))
	if len(hostsRaw) == 0 || hostsRaw[0] != '{' {
		return invalidData("invalid traffic collection")
	}
	return nil
}

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
		return nil, invalidData("missing traffic report")
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
	path := s.absPath(id)
	preSig := statusSignature(path)
	if len(r.Items) == 0 && r.IPOnlyConns.Sign() == 0 && r.Dropped.Sign() == 0 {
		if status, err := refreshIdleReport(ctx, path, preSig, r, dayKey, stamp); status != nil || err != nil {
			return status, err
		}
	}
	d, err := s.read(id, path, preSig, true)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			rememberDamaged(path, preSig, "", nil)
		}
		return nil, err
	}
	previous := d.StatusJSON()
	if err := validateStatusFields(previous); err != nil {
		cachePop(path)
		rememberDamaged(path, preSig, "", nil)
		return nil, err
	}
	markDayDamaged := func() {
		cachePop(path)
		rememberDamaged(path, preSig, dayKey, previous)
	}
	version := r.MihomoVersion
	if len(version) == 0 || string(version) == "null" || string(version) == `""` {
		version = json.RawMessage(`""`)
		if len(previous) > 0 && string(previous) != "null" {
			f, err := object(previous)
			if err != nil {
				cachePop(path)
				rememberDamaged(path, preSig, "", nil)
				return nil, err
			}
			if raw, ok := f.Get("mihomo_version"); ok {
				version = raw
			}
		}
	}
	status := &Status{Value: r.Status, At: stamp, MihomoVersion: version}
	var reader recordReader
	day, hosts, err := d.openBucket(&reader, dayKey, r)
	if err != nil {
		markDayDamaged()
		return nil, err
	}
	stampJSON, _ := json.Marshal(stamp)
	modified := map[string]*hostMerge{}
	var modifiedOrder []string
	for _, item := range r.Items {
		for _, count := range item.Counts {
			if count == nil {
				return nil, invalidData("missing cleaned item counter")
			}
		}
		if item.Rule == nil {
			if item.rulePresent {
				return nil, invalidData("null rule text")
			}
			return nil, missingMember("rule")
		}
		host := modified[item.Host]
		if host == nil {
			host, err = reader.openHost(hosts, item.Host, stampJSON)
			if err != nil {
				markDayDamaged()
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
				markDayDamaged()
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
		stats, err := reader.read(statsRaw)
		if err != nil {
			markDayDamaged()
			return nil, err
		}
		for i, key := range countFields {
			if err = incRecord(&stats, key, item.Counts[i]); err != nil {
				markDayDamaged()
				return nil, err
			}
		}
		kinds, err := reader.read(recordMember(stats, "fail_kinds"))
		if err != nil {
			markDayDamaged()
			return nil, err
		}
		for key, n := range item.FailKinds {
			value := recordMember(kinds, key)
			if len(value) == 0 {
				value = []byte("0")
			}
			value, err = addNumber(value, n)
			if err != nil {
				markDayDamaged()
				return nil, err
			}
			kinds.setEncoded(key, value)
		}
		stats.setEncoded("fail_kinds", kinds.encode())
		routes.setEncoded(routeKey, stats.encode())
	}
	for _, name := range modifiedOrder {
		host := modified[name]
		host.entry.setEncoded("ports", host.ports.encode())
		host.entry.setEncoded("by_route", host.routes.encode())
		hosts.setEncoded(name, host.entry.encode())
	}
	if hosts.len() > MaxHostsPerDay {
		keys := hosts.keys()
		weights := map[string]*big.Rat{}
		for _, key := range keys {
			w, err := weight(recordMember(hosts, key))
			if err != nil {
				markDayDamaged()
				return nil, err
			}
			weights[key] = w
		}
		sort.SliceStable(keys, func(i, j int) bool { return weights[keys[i]].Cmp(weights[keys[j]]) > 0 })
		for _, key := range keys[MaxHostsPerDay:] {
			hosts.delete(key)
			if err = incRecord(&day, "dropped", big.NewInt(1)); err != nil {
				markDayDamaged()
				return nil, err
			}
		}
	}
	hosts.compactOrder()
	day.setEncoded("hosts", nil)
	d.days.setEncoded(dayKey, day.encodeObject("hosts", &hosts))
	statusRaw, err := json.Marshal(status)
	if err != nil {
		return nil, err
	}
	d.fields.setEncoded("status", statusRaw)
	d.fields.setEncoded("days", nil)
	prune(&d, current)
	d.days.compactOrder()
	raw := d.fields.encodeObject("days", &d.days)
	if !json.Valid(raw) {
		return nil, model.ErrJSON
	}
	cachePop(path)
	damagedPop(path)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	signature, err := s.writeJSON(ctx, id, raw)
	if err != nil {
		return nil, err
	}
	if entry := rememberStatus(path, signature, statusRaw); entry != nil && entry.signature.Ino != 0 {
		d.bucket = nil
		if d.days.has(dayKey) {
			d.bucket = &mergeBucket{dayKey, day, hosts}
		}
		if jsontext.Value(statusRaw).IsValid() {
			entry.document = &d
		}
		entry.idle = newIdleHistory(raw, statusRaw, dayKey)
	}
	return status, nil
}

// InspectStatus returns bounded status metadata and read-only store health using
// file-signature caches so repeated UI polls avoid re-parsing unchanged files.
func (s *NativeStore) InspectStatus(id string) StatusSnapshot {
	mu.Lock()
	defer mu.Unlock()
	path := s.absPath(id)
	signature := statusSignature(path)
	if signature == nil {
		cachePop(path)
		damagedPop(path)
		return StatusSnapshot{State: StoreStateOK}
	}
	today := now().Format("2006-01-02")
	if e, ok := damagedIndex[path]; ok {
		entry := e.Value.(*damagedEntry)
		if entry.signature == *signature && (entry.dayKey == "" || entry.dayKey == today) {
			damagedCache.MoveToBack(e)
			return StatusSnapshot{Status: bytes.Clone(entry.status), State: StoreStateDamaged}
		}
		damagedPop(path)
	}
	if e, ok := statusIndex[path]; ok && e.Value.(*cacheEntry).signature == *signature {
		statusCache.MoveToBack(e)
		return StatusSnapshot{Status: bytes.Clone(e.Value.(*cacheEntry).status), State: StoreStateOK}
	}
	cachePop(path)
	d, err := s.read(id, path, signature, false)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			rememberDamaged(path, signature, "", nil)
			return StatusSnapshot{State: StoreStateDamaged}
		}
		return StatusSnapshot{State: StoreStateOK}
	}
	statusRaw := d.StatusJSON()
	if validateStatusFields(statusRaw) != nil {
		cachePop(path)
		rememberDamaged(path, signature, "", nil)
		return StatusSnapshot{State: StoreStateDamaged}
	}
	if validateDayBucketHeader(d.days, today) != nil {
		cachePop(path)
		rememberDamaged(path, signature, today, statusRaw)
		return StatusSnapshot{Status: bytes.Clone(statusRaw), State: StoreStateDamaged}
	}
	return StatusSnapshot{Status: bytes.Clone(statusRaw), State: StoreStateOK}
}

// StatusJSON returns an independent copy of the collection metadata.
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
	d, err := s.read(id, path, signature, false)
	if err != nil {
		return nil
	}
	return d.StatusJSON()
}

// DocumentRevision identifies the file and retention day used by LoadDocument.
type DocumentRevision struct {
	path      string
	day       string
	signature jsonstore.Signature
}

// Revision can be kept independently of the decoded document.
func (d Document) Revision() DocumentRevision { return d.revision }

// Current also checks whether another reader or writer has since marked the file damaged.
func (r DocumentRevision) Current() bool {
	if r.path == "" || r.day != now().Format("2006-01-02") {
		return false
	}
	mu.Lock()
	defer mu.Unlock()
	sig := statusSignature(r.path)
	if sig == nil || *sig != r.signature {
		return false
	}
	if elem := damagedIndex[r.path]; elem != nil {
		entry := elem.Value.(*damagedEntry)
		if entry.signature == r.signature && (entry.dayKey == "" || entry.dayKey == r.day) {
			return false
		}
	}
	return true
}

// LoadDocument reads and prunes the returned copy without writing the file.
func (s *NativeStore) LoadDocument(id string, at *time.Time) Document {
	current := now()
	if at != nil {
		current = *at
	}
	today := current.Format("2006-01-02")
	path := s.absPath(id)

	mu.Lock()
	preSig := statusSignature(path)
	priorDamaged := false
	if preSig != nil {
		if e, ok := damagedIndex[path]; ok {
			entry := e.Value.(*damagedEntry)
			if entry.signature == *preSig && (entry.dayKey == "" || entry.dayKey == today) {
				priorDamaged = true
			}
		}
	}
	d, err := s.read(id, path, preSig, false)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			rememberDamaged(path, preSig, "", nil)
			mu.Unlock()
			out := emptyDocument()
			out.storeState = StoreStateDamaged
			return out
		}
		mu.Unlock()
		return emptyDocument()
	}
	statusRaw := d.StatusJSON()
	statusInvalid := validateStatusFields(statusRaw) != nil
	if statusInvalid {
		cachePop(path)
		rememberDamaged(path, preSig, "", nil)
	}
	mu.Unlock()

	d.storeState = StoreStateOK
	if priorDamaged {
		d.storeState = StoreStateDamaged
	}
	if statusInvalid {
		d.storeState = StoreStateDamaged
		d.fields.setEncoded("status", json.RawMessage("null"))
	}
	prune(&d, current)
	for _, date := range d.days.keys() {
		raw := recordMember(d.days, date)
		day, ok := readableDay(raw)
		if !ok {
			if date == today {
				d.storeState = StoreStateDamaged
				mu.Lock()
				cachePop(path)
				rememberDamaged(path, preSig, today, statusRaw)
				mu.Unlock()
			}
			d.days.delete(date)
			warnStore(path, date)
			continue
		}
		if day != nil {
			if err := recordPut(&d.days, date, day); err != nil {
				d.days.delete(date)
			}
		}
	}
	if d.storeState == StoreStateOK && preSig != nil {
		d.revision = DocumentRevision{path: path, day: today, signature: *preSig}
	}
	d.bucket = nil
	return d
}

// Delete invalidates metadata and removes the per-Agent file while the work lifetime is live.
func (s *NativeStore) Delete(ctx context.Context, id string) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	path := s.absPath(id)
	cachePop(path)
	damagedPop(path)
	return s.files.Remove(ctx, id)
}
