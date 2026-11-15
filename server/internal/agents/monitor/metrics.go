package monitor

// Port of backend/agents/oxidns_metrics.py: normalize the bounded, read-only
// OxiDNS Prometheus metrics snapshot. Observations are jsonx documents
// ({"servers": [...], "caches": [...], "upstreams": [...]}, rows are
// *jsonx.Object with jsonx.Number or nil values) so that they are stored in
// the dashboard state exactly as Python's json.dumps writes them.

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"configflow/internal/jsonx"
)

// Limits of parse_metrics (variables so tests can lower them like the
// Python tests' monkeypatch).
var (
	MaxInputBytes = 2 * 1024 * 1024
	MaxSamples    = 10_000
	MaxLineLength = 16 * 1024
)

// MaxLabels is MAX_LABELS.
const MaxLabels = 32

type fieldMap struct{ metric, field string }

var serverFields = []fieldMap{
	{"server_request_total", "requests"},
	{"server_failed_total", "failed"},
	{"server_inflight", "inflight"},
	{"server_latency_sum_ms", "latency_sum_ms"},
	{"server_latency_count", "latency_count"},
}

var cacheFields = []fieldMap{
	{"cache_lookup_total", "lookups"},
	{"cache_miss_total", "misses"},
	{"cache_expired_total", "expired"},
	{"cache_entry_count", "entries"},
}

var upstreamFields = []fieldMap{
	{"forward_upstream_query_total", "requests"},
	{"forward_upstream_error_total", "failed"},
	{"forward_upstream_latency_sum_ms", "latency_sum_ms"},
	{"forward_upstream_latency_count", "latency_count"},
}

func lookupField(fields []fieldMap, metric string) (string, bool) {
	for _, f := range fields {
		if f.metric == metric {
			return f.field, true
		}
	}
	return "", false
}

func fieldNames(fields []fieldMap) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = f.field
	}
	return out
}

var cacheRowFields = []string{"lookups", "fresh_hits", "stale_hits", "misses", "expired", "entries"}

// pySpaceClass is re's \s for str patterns (str.isspace, Unicode 14).
const pySpaceClass = `\t\n\x0b\x0c\r\x1c-\x20\x85\xa0\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}`

// sampleRE is _SAMPLE with Python's Unicode \S and \d.
var sampleRE = regexp.MustCompile(`^([a-zA-Z_:][a-zA-Z0-9_:]*)(?:\{(.*)\})?[ \t]+([^` + pySpaceClass + `]+)(?:[ \t]+[+-]?[` + pyDigitClass() + `]+)?[ \t]*$`)

// pyDigitClass is the contents of a character class matching re's \d for
// str patterns (str.isdecimal of CPython 3.11).
func pyDigitClass() string {
	var b strings.Builder
	for _, start := range pyDecimalStarts {
		b.WriteString(fmt.Sprintf(`\x{%x}-\x{%x}`, start, start+9))
	}
	return b.String()
}

var labelRE = regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_]*)[ \t]*=[ \t]*"((?:[^"\\]|\\["\\n])*)"`)

var escapeRE = regexp.MustCompile(`\\(["\\n])`)

// numberRE is _NUMBER on text whose Unicode decimal digits were replaced by
// ASCII ones.
var numberRE = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

var integerRE = regexp.MustCompile(`^[+-]?[0-9]+$`)

// labels is _labels: nil for an invalid label set.
func labels(text string, present bool) *jsonx.Object {
	result := jsonx.NewObject()
	if !present {
		return result
	}
	position := 0
	skip := func() {
		for position < len(text) && (text[position] == ' ' || text[position] == '\t') {
			position++
		}
	}
	for position < len(text) {
		skip()
		if position == len(text) {
			break
		}
		match := labelRE.FindStringSubmatchIndex(text[position:])
		if match == nil {
			return nil
		}
		name := text[position+match[2] : position+match[3]]
		value := text[position+match[4] : position+match[5]]
		if result.Has(name) || result.Len() >= MaxLabels {
			return nil
		}
		result.Set(name, escapeRE.ReplaceAllStringFunc(value, func(m string) string {
			if m[1] == 'n' {
				return "\n"
			}
			return m[1:]
		}))
		position += match[1]
		skip()
		if position == len(text) {
			break
		}
		if text[position] != ',' {
			return nil
		}
		position++
	}
	return result
}

// toASCIIDigits replaces the Unicode decimal digits Python's \d, float()
// and int() accept with ASCII ones; digits counts them.
func toASCIIDigits(text string) (ascii string, digits int) {
	var b strings.Builder
	for _, r := range text {
		if v := pyDecimalValue(r); v >= 0 {
			b.WriteByte(byte('0' + v))
			digits++
		} else {
			b.WriteRune(r)
		}
	}
	return b.String(), digits
}

// maxStrDigits is sys.get_int_max_str_digits() (int() of a longer text
// raises ValueError).
const maxStrDigits = 4300

// metricNumber is _number: nil for anything that is not a finite,
// non-negative number; integer texts stay exact ints.
func metricNumber(text string) any {
	ascii, digits := toASCIIDigits(text)
	if !numberRE.MatchString(ascii) {
		return nil
	}
	value, err := strconv.ParseFloat(ascii, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return nil
	}
	if math.IsInf(value, 0) || math.IsNaN(value) || value < 0 {
		return nil
	}
	if integerRE.MatchString(ascii) {
		if digits > maxStrDigits {
			return nil
		}
		i, ok := new(big.Int).SetString(ascii, 10)
		if !ok {
			return nil
		}
		return jsonx.BigInt(i)
	}
	return jsonx.Float(value)
}

// pySum is _sum: nil when a value is unknown, the sum overflows or is not
// finite.
func pySum(values []any) any {
	if len(values) == 0 {
		return nil
	}
	total := intNum(0)
	for _, value := range values {
		if value == nil {
			return nil
		}
	}
	for _, value := range values {
		n, ok := asNum(value)
		if !ok {
			return nil
		}
		var err error
		if total, err = total.add(n); err != nil {
			return nil
		}
	}
	finite, err := total.isFinite()
	if err != nil || !finite {
		return nil
	}
	return total.value()
}

// divide is _divide.
func divide(numerator, denominator any, percentage bool) any {
	if numerator == nil || denominator == nil {
		return nil
	}
	n, okN := asNum(numerator)
	d, okD := asNum(denominator)
	if !okN || !okD || d.equal(intNum(0)) {
		return nil
	}
	if percentage && d.less(n) {
		return nil
	}
	result, err := n.trueDiv(d)
	if err != nil {
		return nil
	}
	if percentage {
		result *= 100
	}
	if math.IsInf(result, 0) || math.IsNaN(result) {
		return nil
	}
	return jsonx.Float(result)
}

// observe is _observe.
func observe(group *jsonx.Object, field string, value any) {
	if group.Has(field) && !pyNumberEqual(group.Value(field), value) {
		group.Set(field, nil)
	} else {
		group.Set(field, value)
	}
}

type groupKey struct{ a, b string }

type groups struct {
	keys   []groupKey
	values map[groupKey]*jsonx.Object
}

func newGroups() *groups { return &groups{values: map[groupKey]*jsonx.Object{}} }

func (g *groups) get(key groupKey) *jsonx.Object {
	if v, ok := g.values[key]; ok {
		return v
	}
	v := jsonx.NewObject()
	g.values[key] = v
	g.keys = append(g.keys, key)
	return v
}

func (g *groups) sorted() []groupKey {
	keys := append([]groupKey(nil), g.keys...)
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].a != keys[j].a {
			return keys[i].a < keys[j].a
		}
		return keys[i].b < keys[j].b
	})
	return keys
}

// MetricsError is the ValueError of parse_metrics.
type MetricsError struct{ Message string }

func (e *MetricsError) Error() string { return e.Message }

// ParseMetrics is parse_metrics: JSON-safe raw observations; oversized input
// is a *MetricsError.
func ParseMetrics(text string) (*jsonx.Object, error) {
	if len(text) > MaxInputBytes {
		return nil, &MetricsError{"OxiDNS metrics exceed size limit"}
	}
	servers, caches, upstreams := newGroups(), newGroups(), newGroups()
	sampleCount := 0
	for _, raw := range pySplitLines(text) {
		if runeLen(raw) > MaxLineLength {
			return nil, &MetricsError{"OxiDNS metrics line exceeds size limit"}
		}
		line := pyStrip(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		sampleCount++
		if sampleCount > MaxSamples {
			return nil, &MetricsError{"OxiDNS metrics exceed sample limit"}
		}
		name, labelText, hasLabels, valueText, ok := parseSample(line)
		if !ok {
			continue
		}
		family := strings.TrimPrefix(name, "oxidns_")
		serverField, isServer := lookupField(serverFields, family)
		upstreamField, isUpstream := lookupField(upstreamFields, family)
		cacheField, isCache := lookupField(cacheFields, family)
		if !isServer && !isUpstream && !isCache && family != "cache_hit_total" {
			continue
		}
		l := labels(labelText, hasLabels)
		if l == nil || l.Len() == 0 || l.GetString("plugin_tag") == "" {
			continue
		}
		tag := l.GetString("plugin_tag")
		value := metricNumber(valueText)
		switch {
		case isServer:
			protocol := l.GetString("protocol")
			if protocol == "" {
				continue
			}
			observe(servers.get(groupKey{tag, protocol}), serverField, value)
		case isUpstream:
			upstream := l.GetString("upstream")
			if upstream == "" {
				continue
			}
			observe(upstreams.get(groupKey{tag, upstream}), upstreamField, value)
		case family == "cache_hit_total":
			kind, _ := l.Value("kind").(string)
			if kind != "fresh" && kind != "stale" {
				continue
			}
			observe(caches.get(groupKey{a: tag}), kind+"_hits", value)
		default:
			observe(caches.get(groupKey{a: tag}), cacheField, value)
		}
	}
	serverRows := []any{}
	for _, key := range servers.sorted() {
		values := servers.values[key]
		row := jsonx.Obj("tag", key.a, "protocol", key.b)
		for _, field := range fieldNames(serverFields) {
			row.Set(field, values.Value(field))
		}
		serverRows = append(serverRows, row)
	}
	cacheRows := []any{}
	for _, key := range caches.sorted() {
		values := caches.values[key]
		row := jsonx.Obj("tag", key.a)
		for _, field := range cacheRowFields {
			row.Set(field, values.Value(field))
		}
		cacheRows = append(cacheRows, row)
	}
	upstreamRows := []any{}
	for _, key := range upstreams.sorted() {
		values := upstreams.values[key]
		row := jsonx.Obj("tag", key.a, "upstream", key.b)
		for _, field := range fieldNames(upstreamFields) {
			row.Set(field, values.Value(field))
		}
		upstreamRows = append(upstreamRows, row)
	}
	return jsonx.Obj("servers", serverRows, "caches", cacheRows, "upstreams", upstreamRows), nil
}

// parseSample is _SAMPLE.fullmatch(line).
func parseSample(line string) (name, labelText string, hasLabels bool, value string, ok bool) {
	m := sampleRE.FindStringSubmatchIndex(line)
	if m == nil {
		return "", "", false, "", false
	}
	if m[4] >= 0 {
		labelText, hasLabels = line[m[4]:m[5]], true
	}
	return line[m[2]:m[3]], labelText, hasLabels, line[m[6]:m[7]], true
}

// counters are _COUNTERS (gauges inflight and entries are not reset).
var counters = []struct {
	kind   string
	fields []string
	labels []string
}{
	{"servers", []string{"requests", "failed", "latency_sum_ms", "latency_count"}, []string{"tag", "protocol"}},
	{"caches", []string{"lookups", "fresh_hits", "stale_hits", "misses", "expired"}, []string{"tag"}},
	{"upstreams", []string{"requests", "failed", "latency_sum_ms", "latency_count"}, []string{"tag", "upstream"}},
}

// identity is tuple(row.get(key) for key in labels), as a comparable key.
func identity(row *jsonx.Object, labels []string) string {
	parts := make([]any, len(labels))
	for i, key := range labels {
		parts[i] = row.Value(key)
	}
	text, _ := jsonx.Compact(parts)
	return string(text)
}

func rowsOf(document *jsonx.Object, kind string) []*jsonx.Object {
	var out []*jsonx.Object
	for _, item := range document.GetList(kind) {
		if row, ok := item.(*jsonx.Object); ok {
			out = append(out, row)
		}
	}
	return out
}

// restarted is any(row.get(f) is not None and old.get(f) is not None and
// row[f] < old[f]).
func restarted(row, old *jsonx.Object, fields []string) bool {
	for _, field := range fields {
		a, okA := asNum(row.Value(field))
		b, okB := asNum(old.Value(field))
		if okA && okB && a.less(b) {
			return true
		}
	}
	return false
}

// MetricsCountersRestarted is metrics_counters_restarted.
func MetricsCountersRestarted(current, baseline *jsonx.Object) bool {
	for _, c := range counters {
		previous := map[string]*jsonx.Object{}
		for _, row := range rowsOf(baseline, c.kind) {
			previous[identity(row, c.labels)] = row
		}
		for _, row := range rowsOf(current, c.kind) {
			if old := previous[identity(row, c.labels)]; old != nil && restarted(row, old, c.fields) {
				return true
			}
		}
	}
	return false
}

// ApplyMetricsBaseline is apply_metrics_baseline: the counters since the
// baseline (inputs untouched) and whether the result is partial.
func ApplyMetricsBaseline(current, baseline *jsonx.Object) (*jsonx.Object, bool, error) {
	adjusted, partial := jsonx.NewObject(), false
	for _, c := range counters {
		previous := map[string]*jsonx.Object{}
		for _, row := range rowsOf(baseline, c.kind) {
			previous[identity(row, c.labels)] = row
		}
		rows := []any{}
		seen := map[string]bool{}
		for _, row := range rowsOf(current, c.kind) {
			id := identity(row, c.labels)
			seen[id] = true
			old := previous[id]
			output := row.Clone()
			switch {
			case old == nil:
				partial = true
				for _, field := range c.fields {
					output.Set(field, nil)
				}
			case restarted(row, old, c.fields):
				partial = true
			default:
				for _, field := range c.fields {
					value, before := row.Value(field), old.Value(field)
					var result any
					if value != nil && before != nil {
						a, okA := asNum(value)
						b, okB := asNum(before)
						if !okA || !okB {
							return nil, false, errors.New("TypeError: unsupported operand type(s) for -")
						}
						diff, err := a.sub(b)
						if err != nil {
							return nil, false, err
						}
						result = diff.value()
					}
					output.Set(field, result)
					if result == nil {
						partial = true
					}
				}
			}
			rows = append(rows, output)
		}
		adjusted.Set(c.kind, rows)
		for id := range previous {
			if !seen[id] {
				partial = true
			}
		}
	}
	return adjusted, partial, nil
}

// ZeroBaseline is zero_baseline.
func ZeroBaseline(snapshot *jsonx.Object) *jsonx.Object {
	result := jsonx.NewObject()
	for _, c := range counters {
		rows := []any{}
		for _, row := range rowsOf(snapshot, c.kind) {
			out := row.Clone()
			for _, field := range c.fields {
				out.Set(field, jsonx.Int(0))
			}
			rows = append(rows, out)
		}
		result.Set(c.kind, rows)
	}
	return result
}

// AddObservations is add_observations: counters carried across a
// publication, retired series with zero gauges.
func AddObservations(current, carried *jsonx.Object) *jsonx.Object {
	result := jsonx.NewObject()
	for _, c := range counters {
		type keyed struct {
			parts []string
			row   *jsonx.Object
		}
		live, old := map[string]keyed{}, map[string]keyed{}
		var order []string
		index := func(target map[string]keyed, rows []*jsonx.Object) {
			for _, row := range rows {
				parts := make([]string, len(c.labels))
				for i, key := range c.labels {
					parts[i], _ = row.Value(key).(string)
				}
				id := identity(row, c.labels)
				if _, ok := live[id]; !ok {
					if _, ok := old[id]; !ok {
						order = append(order, id)
					}
				}
				target[id] = keyed{parts, row}
			}
		}
		index(live, rowsOf(current, c.kind))
		index(old, rowsOf(carried, c.kind))
		partsOf := func(id string) []string {
			if k, ok := live[id]; ok {
				return k.parts
			}
			return old[id].parts
		}
		sort.SliceStable(order, func(i, j int) bool {
			a, b := partsOf(order[i]), partsOf(order[j])
			for k := range a {
				if a[k] != b[k] {
					return a[k] < b[k]
				}
			}
			return false
		})
		rows := []any{}
		for _, id := range order {
			l, inLive := live[id]
			o, inOld := old[id]
			var row *jsonx.Object
			if inLive {
				row = l.row.Clone()
			} else {
				row = o.row.Clone()
			}
			if inLive && inOld {
				for _, field := range c.fields {
					row.Set(field, pySum([]any{l.row.Value(field), o.row.Value(field)}))
				}
			}
			if !inLive {
				for _, gauge := range []string{"inflight", "entries"} {
					if row.Has(gauge) {
						row.Set(gauge, jsonx.Int(0))
					}
				}
			}
			rows = append(rows, row)
		}
		result.Set(c.kind, rows)
	}
	return result
}

func pick(values *jsonx.Object, fields ...string) *jsonx.Object {
	out := jsonx.NewObject()
	for _, field := range fields {
		out.Set(field, values.Value(field))
	}
	return out
}

// SummarizeObservations is summarize_observations: weighted dashboard ratios
// from raw or reset-adjusted counters.
func SummarizeObservations(snapshot *jsonx.Object) *jsonx.Object {
	snapServers := rowsOf(snapshot, "servers")
	servers := []any{}
	var requests, failed, inflight, latencySums, latencyCounts []any
	for _, values := range snapServers {
		row := pick(values, "tag", "protocol", "requests", "failed", "inflight")
		row.Set("avg_latency_ms", divide(values.Value("latency_sum_ms"), values.Value("latency_count"), false))
		servers = append(servers, row)
		requests = append(requests, row.Value("requests"))
		failed = append(failed, row.Value("failed"))
		inflight = append(inflight, row.Value("inflight"))
		latencySums = append(latencySums, values.Value("latency_sum_ms"))
		latencyCounts = append(latencyCounts, values.Value("latency_count"))
	}
	caches := []any{}
	var hits, lookups []any
	inconsistent := false
	for _, values := range rowsOf(snapshot, "caches") {
		h := pySum([]any{values.Value("fresh_hits"), values.Value("stale_hits")})
		row := pick(values, "tag", "lookups", "fresh_hits", "stale_hits", "misses", "expired", "entries")
		row.Set("hits", h)
		row.Set("hit_rate", divide(h, values.Value("lookups"), true))
		caches = append(caches, row)
		hits = append(hits, h)
		lookups = append(lookups, row.Value("lookups"))
		if h != nil && row.Value("lookups") != nil {
			a, okA := asNum(h)
			b, okB := asNum(row.Value("lookups"))
			if okA && okB && b.less(a) {
				inconsistent = true
			}
		}
	}
	cacheHitRate := divide(pySum(hits), pySum(lookups), true)
	if inconsistent {
		cacheHitRate = nil
	}
	upstreams := []*jsonx.Object{}
	for _, values := range rowsOf(snapshot, "upstreams") {
		row := pick(values, "tag", "upstream", "requests", "failed")
		row.Set("avg_latency_ms", divide(values.Value("latency_sum_ms"), values.Value("latency_count"), false))
		upstreams = append(upstreams, row)
	}
	// Unknown counts sort last, then by descending count, tag, upstream.
	sort.SliceStable(upstreams, func(i, j int) bool {
		a, b := upstreams[i], upstreams[j]
		an, bn := a.Value("requests") == nil, b.Value("requests") == nil
		if an != bn {
			return !an
		}
		if !an {
			x, _ := asNum(a.Value("requests"))
			y, _ := asNum(b.Value("requests"))
			if c := x.cmp(y); c != 0 {
				return c > 0
			}
		}
		at, bt := jsonx.Str(a.Value("tag")), jsonx.Str(b.Value("tag"))
		if at != bt {
			return at < bt
		}
		return jsonx.Str(a.Value("upstream")) < jsonx.Str(b.Value("upstream"))
	})
	upstreamList := make([]any, len(upstreams))
	for i, row := range upstreams {
		upstreamList[i] = row
	}
	return jsonx.Obj(
		"metrics", jsonx.Obj(
			"requests_total", pySum(requests),
			"failed_total", pySum(failed),
			"avg_latency_ms", divide(pySum(latencySums), pySum(latencyCounts), false),
			"inflight", pySum(inflight),
			"cache_hit_rate", cacheHitRate,
		),
		"caches", caches,
		"servers", servers,
		"upstreams", upstreamList,
	)
}

// SummarizeMetrics is summarize_metrics.
func SummarizeMetrics(text string) (*jsonx.Object, error) {
	observations, err := ParseMetrics(text)
	if err != nil {
		return nil, err
	}
	return SummarizeObservations(observations), nil
}
