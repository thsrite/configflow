package monitor

// OxiDNS Prometheus snapshots and dashboard arithmetic use typed observations.

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"configflow/internal/model"
	"encoding/json"
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
func labels(text string, present bool) map[string]string {
	result := map[string]string{}
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
		if hasLabel(result, name) || len(result) >= MaxLabels {
			return nil
		}
		result[name] = escapeRE.ReplaceAllStringFunc(value, func(m string) string {
			if m[1] == 'n' {
				return "\n"
			}
			return m[1:]
		})
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
func metricNumberNative(text string) *MetricNumber {
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
		return &MetricNumber{num: num{i: i}}
	}
	return &MetricNumber{num: num{isFloat: true, f: value}}
}

// Observations are raw counters/gauges owned by one monitoring snapshot.
type Observations struct {
	Servers   []ServerObservation   `json:"servers"`
	Caches    []CacheObservation    `json:"caches"`
	Upstreams []UpstreamObservation `json:"upstreams"`
}

// ServerObservation holds one labeled servers metric series.
type ServerObservation struct {
	present      [6]bool
	Tag          string        `json:"tag"`
	Protocol     string        `json:"protocol"`
	Requests     *MetricNumber `json:"requests"`
	Failed       *MetricNumber `json:"failed"`
	Inflight     *MetricNumber `json:"inflight"`
	LatencySumMS *MetricNumber `json:"latency_sum_ms"`
	LatencyCount *MetricNumber `json:"latency_count"`
}

// CacheObservation holds one labeled caches metric series.
type CacheObservation struct {
	present   [6]bool
	Tag       string        `json:"tag"`
	Lookups   *MetricNumber `json:"lookups"`
	FreshHits *MetricNumber `json:"fresh_hits"`
	StaleHits *MetricNumber `json:"stale_hits"`
	Misses    *MetricNumber `json:"misses"`
	Expired   *MetricNumber `json:"expired"`
	Entries   *MetricNumber `json:"entries"`
}

// UpstreamObservation holds one labeled upstreams metric series.
type UpstreamObservation struct {
	present      [6]bool
	Tag          string        `json:"tag"`
	Upstream     string        `json:"upstream"`
	Requests     *MetricNumber `json:"requests"`
	Failed       *MetricNumber `json:"failed"`
	LatencySumMS *MetricNumber `json:"latency_sum_ms"`
	LatencyCount *MetricNumber `json:"latency_count"`
}
type groupKey struct{ a, b string }
type metricSeries struct {
	key     groupKey
	values  [6]*MetricNumber
	present [6]bool
}

var counterIndices = [3][]int{{0, 1, 3, 4}, {0, 1, 2, 3, 4}, {0, 1, 2, 3}}

func seriesOf(o *Observations, kind int) []metricSeries {
	if o == nil {
		return nil
	}
	var out []metricSeries
	switch kind {
	case 0:
		for _, row := range o.Servers {
			out = append(out, metricSeries{key: groupKey{row.Tag, row.Protocol}, values: [6]*MetricNumber{row.Requests, row.Failed, row.Inflight, row.LatencySumMS, row.LatencyCount}, present: row.present})
		}
	case 1:
		for _, row := range o.Caches {
			out = append(out, metricSeries{key: groupKey{row.Tag, ""}, values: [6]*MetricNumber{row.Lookups, row.FreshHits, row.StaleHits, row.Misses, row.Expired, row.Entries}, present: row.present})
		}
	case 2:
		for _, row := range o.Upstreams {
			out = append(out, metricSeries{key: groupKey{row.Tag, row.Upstream}, values: [6]*MetricNumber{row.Requests, row.Failed, row.LatencySumMS, row.LatencyCount}, present: row.present})
		}
	}
	return out
}
func observationsOf(kinds [3][]metricSeries) *Observations {
	out := &Observations{Servers: []ServerObservation{}, Caches: []CacheObservation{}, Upstreams: []UpstreamObservation{}}
	for _, row := range kinds[0] {
		out.Servers = append(out.Servers, ServerObservation{present: row.present, Tag: row.key.a, Protocol: row.key.b, Requests: row.values[0], Failed: row.values[1], Inflight: row.values[2], LatencySumMS: row.values[3], LatencyCount: row.values[4]})
	}
	for _, row := range kinds[1] {
		out.Caches = append(out.Caches, CacheObservation{present: row.present, Tag: row.key.a, Lookups: row.values[0], FreshHits: row.values[1], StaleHits: row.values[2], Misses: row.values[3], Expired: row.values[4], Entries: row.values[5]})
	}
	for _, row := range kinds[2] {
		out.Upstreams = append(out.Upstreams, UpstreamObservation{present: row.present, Tag: row.key.a, Upstream: row.key.b, Requests: row.values[0], Failed: row.values[1], LatencySumMS: row.values[2], LatencyCount: row.values[3]})
	}
	return out
}
func hasLabel(labels map[string]string, key string) bool { _, ok := labels[key]; return ok }

// MetricsError reports an oversized metrics snapshot.
type MetricsError struct{ Message string }

func (e *MetricsError) Error() string { return e.Message }

// ReadMetrics normalizes a bounded Prometheus snapshot into owned typed observations.
func ReadMetrics(text string) (*Observations, error) {
	if len(text) > MaxInputBytes {
		return nil, &MetricsError{"OxiDNS metrics exceed size limit"}
	}
	groups := [3]map[groupKey]*metricSeries{{}, {}, {}}
	observeMetric := func(kind int, key groupKey, field string, value *MetricNumber) {
		names := [][]fieldMap{serverFields, cacheFields, upstreamFields}
		index := -1
		if kind == 1 {
			for i, name := range cacheRowFields {
				if name == field {
					index = i
				}
			}
		} else {
			for i, name := range names[kind] {
				if name.field == field {
					index = i
				}
			}
		}
		if index < 0 {
			return
		}
		row := groups[kind][key]
		if row == nil {
			row = &metricSeries{key: key}
			groups[kind][key] = row
		}
		if row.present[index] && !metricEqual(row.values[index], value) {
			row.values[index] = nil
		} else {
			row.values[index] = value
		}
		row.present[index] = true
	}
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
		if l == nil || len(l) == 0 || l["plugin_tag"] == "" {
			continue
		}
		tag := l["plugin_tag"]
		value := metricNumberNative(valueText)
		switch {
		case isServer:
			protocol := l["protocol"]
			if protocol == "" {
				continue
			}
			observeMetric(0, groupKey{tag, protocol}, serverField, value)
		case isUpstream:
			upstream := l["upstream"]
			if upstream == "" {
				continue
			}
			observeMetric(2, groupKey{tag, upstream}, upstreamField, value)
		case family == "cache_hit_total":
			kind := l["kind"]
			if kind != "fresh" && kind != "stale" {
				continue
			}
			observeMetric(1, groupKey{a: tag}, kind+"_hits", value)
		default:
			observeMetric(1, groupKey{a: tag}, cacheField, value)
		}
	}
	var rows [3][]metricSeries
	for kind, groups := range groups {
		for _, row := range groups {
			for i := range row.present {
				row.present[i] = true
			}
			rows[kind] = append(rows[kind], *row)
		}
		sortSeries(rows[kind])
	}
	return observationsOf(rows), nil
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

func sortSeries(rows []metricSeries) {
	sort.Slice(rows, func(i, j int) bool { a, b := rows[i].key, rows[j].key; return a.a < b.a || a.a == b.a && a.b < b.b })
}
func resetSeries(current, old metricSeries, fields []int) bool {
	for _, field := range fields {
		a, b := current.values[field], old.values[field]
		if a != nil && b != nil && a.num.less(b.num) {
			return true
		}
	}
	return false
}

// CountersRestarted detects any decreasing counter with the same labels.
func CountersRestarted(current, baseline *Observations) bool {
	for kind, fields := range counterIndices {
		previous := map[groupKey]metricSeries{}
		for _, row := range seriesOf(baseline, kind) {
			previous[row.key] = row
		}
		for _, row := range seriesOf(current, kind) {
			if old, ok := previous[row.key]; ok && resetSeries(row, old, fields) {
				return true
			}
		}
	}
	return false
}

// ApplyBaseline subtracts one statistics boundary without mutating its inputs.
func ApplyBaseline(current, baseline *Observations) (*Observations, bool, error) {
	var rows [3][]metricSeries
	partial := false
	for kind, fields := range counterIndices {
		previous := map[groupKey]metricSeries{}
		for _, row := range seriesOf(baseline, kind) {
			previous[row.key] = row
		}
		seen := map[groupKey]bool{}
		for _, row := range seriesOf(current, kind) {
			seen[row.key] = true
			old, exists := previous[row.key]
			switch {
			case !exists:
				partial = true
				for _, field := range fields {
					row.values[field] = nil
					row.present[field] = true
				}
			case resetSeries(row, old, fields):
				partial = true
			default:
				for _, field := range fields {
					a, b := row.values[field], old.values[field]
					if a == nil || b == nil {
						row.values[field] = nil
						row.present[field] = true
						partial = true
						continue
					}
					value, err := a.num.sub(b.num)
					if err != nil {
						return nil, false, err
					}
					row.values[field] = &MetricNumber{num: value}
					row.present[field] = true
				}
			}
			rows[kind] = append(rows[kind], row)
		}
		for key := range previous {
			if !seen[key] {
				partial = true
			}
		}
	}
	return observationsOf(rows), partial, nil
}

// ZeroObservations retains labels and gauges while setting each counter to zero.
func ZeroObservations(current *Observations) *Observations {
	var rows [3][]metricSeries
	for kind, fields := range counterIndices {
		rows[kind] = seriesOf(current, kind)
		for i := range rows[kind] {
			for _, field := range fields {
				rows[kind][i].values[field] = metricInt(0)
				rows[kind][i].present[field] = true
			}
		}
	}
	return observationsOf(rows)
}

// CarryObservations adds counters carried across a publication boundary.
func CarryObservations(current, carried *Observations) *Observations {
	var rows [3][]metricSeries
	for kind, fields := range counterIndices {
		live := map[groupKey]metricSeries{}
		old := map[groupKey]metricSeries{}
		for _, row := range seriesOf(current, kind) {
			live[row.key] = row
		}
		for _, row := range seriesOf(carried, kind) {
			old[row.key] = row
		}
		for key, row := range live {
			if before, ok := old[key]; ok {
				for _, field := range fields {
					row.values[field] = metricSum(row.values[field], before.values[field])
					row.present[field] = true
				}
			}
			rows[kind] = append(rows[kind], row)
		}
		for key, row := range old {
			if _, exists := live[key]; exists {
				continue
			}
			if kind == 0 {
				row.values[2] = metricInt(0)
			} else if kind == 1 {
				row.values[5] = metricInt(0)
			}
			rows[kind] = append(rows[kind], row)
		}
		sortSeries(rows[kind])
	}
	return observationsOf(rows)
}

// MetricTotals holds weighted aggregate metrics; nil values are unknown.
type MetricTotals struct {
	RequestsTotal *MetricNumber `json:"requests_total"`
	FailedTotal   *MetricNumber `json:"failed_total"`
	AvgLatencyMS  *MetricNumber `json:"avg_latency_ms"`
	Inflight      *MetricNumber `json:"inflight"`
	CacheHitRate  *MetricNumber `json:"cache_hit_rate"`
}

// ServerSummary exposes only the server fields shown in the dashboard.
type ServerSummary struct {
	Tag          string        `json:"tag"`
	Protocol     string        `json:"protocol"`
	Requests     *MetricNumber `json:"requests"`
	Failed       *MetricNumber `json:"failed"`
	Inflight     *MetricNumber `json:"inflight"`
	AvgLatencyMS *MetricNumber `json:"avg_latency_ms"`
}

// CacheSummary adds hit counts and hit rate to one cache observation.
type CacheSummary struct {
	Tag       string        `json:"tag"`
	Lookups   *MetricNumber `json:"lookups"`
	FreshHits *MetricNumber `json:"fresh_hits"`
	StaleHits *MetricNumber `json:"stale_hits"`
	Misses    *MetricNumber `json:"misses"`
	Expired   *MetricNumber `json:"expired"`
	Entries   *MetricNumber `json:"entries"`
	Hits      *MetricNumber `json:"hits"`
	HitRate   *MetricNumber `json:"hit_rate"`
}

// UpstreamSummary holds the weighted metrics of one forwarder.
type UpstreamSummary struct {
	Tag          string        `json:"tag"`
	Upstream     string        `json:"upstream"`
	Requests     *MetricNumber `json:"requests"`
	Failed       *MetricNumber `json:"failed"`
	AvgLatencyMS *MetricNumber `json:"avg_latency_ms"`
}

// MetricSummary is the public dashboard projection of an observations snapshot.
type MetricSummary struct {
	Metrics   MetricTotals      `json:"metrics"`
	Servers   []ServerSummary   `json:"servers"`
	Caches    []CacheSummary    `json:"caches"`
	Upstreams []UpstreamSummary `json:"upstreams"`
}

// Summarize computes weighted dashboard ratios and stable forwarder order.
func Summarize(current *Observations) *MetricSummary {
	out := &MetricSummary{Servers: []ServerSummary{}, Caches: []CacheSummary{}, Upstreams: []UpstreamSummary{}}
	if current == nil {
		return out
	}
	var requests, failed, inflight, sums, counts, hits, lookups []*MetricNumber
	for _, row := range current.Servers {
		out.Servers = append(out.Servers, ServerSummary{row.Tag, row.Protocol, row.Requests, row.Failed, row.Inflight, metricRatio(row.LatencySumMS, row.LatencyCount, false)})
		requests = append(requests, row.Requests)
		failed = append(failed, row.Failed)
		inflight = append(inflight, row.Inflight)
		sums = append(sums, row.LatencySumMS)
		counts = append(counts, row.LatencyCount)
	}
	inconsistent := false
	for _, row := range current.Caches {
		hit := metricSum(row.FreshHits, row.StaleHits)
		out.Caches = append(out.Caches, CacheSummary{row.Tag, row.Lookups, row.FreshHits, row.StaleHits, row.Misses, row.Expired, row.Entries, hit, metricRatio(hit, row.Lookups, true)})
		hits = append(hits, hit)
		lookups = append(lookups, row.Lookups)
		if hit != nil && row.Lookups != nil && row.Lookups.num.less(hit.num) {
			inconsistent = true
		}
	}
	rate := metricRatio(metricSum(hits...), metricSum(lookups...), true)
	if inconsistent {
		rate = nil
	}
	out.Metrics = MetricTotals{metricSum(requests...), metricSum(failed...), metricRatio(metricSum(sums...), metricSum(counts...), false), metricSum(inflight...), rate}
	for _, row := range current.Upstreams {
		out.Upstreams = append(out.Upstreams, UpstreamSummary{row.Tag, row.Upstream, row.Requests, row.Failed, metricRatio(row.LatencySumMS, row.LatencyCount, false)})
	}
	sort.SliceStable(out.Upstreams, func(i, j int) bool {
		a, b := out.Upstreams[i], out.Upstreams[j]
		if (a.Requests == nil) != (b.Requests == nil) {
			return a.Requests != nil
		}
		if a.Requests != nil {
			if c := a.Requests.num.cmp(b.Requests.num); c != 0 {
				return c > 0
			}
		}
		return a.Tag < b.Tag || a.Tag == b.Tag && a.Upstream < b.Upstream
	})
	return out
}

// UnmarshalJSON decodes observation families by exact schema keys.
func (o *Observations) UnmarshalJSON(raw []byte) error {
	var fields model.Fields
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	*o = Observations{}
	for _, family := range []struct {
		name   string
		target any
	}{{"servers", &o.Servers}, {"caches", &o.Caches}, {"upstreams", &o.Upstreams}} {
		if value, present := fields.Get(family.name); present {
			if err := json.Unmarshal(value, family.target); err != nil {
				return err
			}
		}
	}
	return nil
}
func (row *ServerObservation) UnmarshalJSON(raw []byte) error {
	var fields model.Fields
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	*row = ServerObservation{}
	if value, present := fields.Get("tag"); present {
		if err := json.Unmarshal(value, &row.Tag); err != nil {
			return err
		}
	}
	if value, present := fields.Get("protocol"); present {
		if err := json.Unmarshal(value, &row.Protocol); err != nil {
			return err
		}
	}
	if value, present := fields.Get("requests"); present {
		row.present[0] = true
		if err := json.Unmarshal(value, &row.Requests); err != nil {
			return err
		}
	}
	if value, present := fields.Get("failed"); present {
		row.present[1] = true
		if err := json.Unmarshal(value, &row.Failed); err != nil {
			return err
		}
	}
	if value, present := fields.Get("inflight"); present {
		row.present[2] = true
		if err := json.Unmarshal(value, &row.Inflight); err != nil {
			return err
		}
	}
	if value, present := fields.Get("latency_sum_ms"); present {
		row.present[3] = true
		if err := json.Unmarshal(value, &row.LatencySumMS); err != nil {
			return err
		}
	}
	if value, present := fields.Get("latency_count"); present {
		row.present[4] = true
		if err := json.Unmarshal(value, &row.LatencyCount); err != nil {
			return err
		}
	}
	return nil
}
func (row *CacheObservation) UnmarshalJSON(raw []byte) error {
	var fields model.Fields
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	*row = CacheObservation{}
	if value, present := fields.Get("tag"); present {
		if err := json.Unmarshal(value, &row.Tag); err != nil {
			return err
		}
	}
	if value, present := fields.Get("lookups"); present {
		row.present[0] = true
		if err := json.Unmarshal(value, &row.Lookups); err != nil {
			return err
		}
	}
	if value, present := fields.Get("fresh_hits"); present {
		row.present[1] = true
		if err := json.Unmarshal(value, &row.FreshHits); err != nil {
			return err
		}
	}
	if value, present := fields.Get("stale_hits"); present {
		row.present[2] = true
		if err := json.Unmarshal(value, &row.StaleHits); err != nil {
			return err
		}
	}
	if value, present := fields.Get("misses"); present {
		row.present[3] = true
		if err := json.Unmarshal(value, &row.Misses); err != nil {
			return err
		}
	}
	if value, present := fields.Get("expired"); present {
		row.present[4] = true
		if err := json.Unmarshal(value, &row.Expired); err != nil {
			return err
		}
	}
	if value, present := fields.Get("entries"); present {
		row.present[5] = true
		if err := json.Unmarshal(value, &row.Entries); err != nil {
			return err
		}
	}
	return nil
}
func (row *UpstreamObservation) UnmarshalJSON(raw []byte) error {
	var fields model.Fields
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	*row = UpstreamObservation{}
	if value, present := fields.Get("tag"); present {
		if err := json.Unmarshal(value, &row.Tag); err != nil {
			return err
		}
	}
	if value, present := fields.Get("upstream"); present {
		if err := json.Unmarshal(value, &row.Upstream); err != nil {
			return err
		}
	}
	if value, present := fields.Get("requests"); present {
		row.present[0] = true
		if err := json.Unmarshal(value, &row.Requests); err != nil {
			return err
		}
	}
	if value, present := fields.Get("failed"); present {
		row.present[1] = true
		if err := json.Unmarshal(value, &row.Failed); err != nil {
			return err
		}
	}
	if value, present := fields.Get("latency_sum_ms"); present {
		row.present[2] = true
		if err := json.Unmarshal(value, &row.LatencySumMS); err != nil {
			return err
		}
	}
	if value, present := fields.Get("latency_count"); present {
		row.present[3] = true
		if err := json.Unmarshal(value, &row.LatencyCount); err != nil {
			return err
		}
	}
	return nil
}

func (row ServerObservation) MarshalJSON() ([]byte, error) {
	var fields model.Fields
	raw, err := json.Marshal(row.Tag)
	if err != nil {
		return nil, err
	}
	_ = fields.Set("tag", raw)
	raw, err = json.Marshal(row.Protocol)
	if err != nil {
		return nil, err
	}
	_ = fields.Set("protocol", raw)
	if row.present[0] {
		raw, err = json.Marshal(row.Requests)
		if err != nil {
			return nil, err
		}
		_ = fields.Set("requests", raw)
	}
	if row.present[1] {
		raw, err = json.Marshal(row.Failed)
		if err != nil {
			return nil, err
		}
		_ = fields.Set("failed", raw)
	}
	if row.present[2] {
		raw, err = json.Marshal(row.Inflight)
		if err != nil {
			return nil, err
		}
		_ = fields.Set("inflight", raw)
	}
	if row.present[3] {
		raw, err = json.Marshal(row.LatencySumMS)
		if err != nil {
			return nil, err
		}
		_ = fields.Set("latency_sum_ms", raw)
	}
	if row.present[4] {
		raw, err = json.Marshal(row.LatencyCount)
		if err != nil {
			return nil, err
		}
		_ = fields.Set("latency_count", raw)
	}
	return json.Marshal(fields)
}

func (row CacheObservation) MarshalJSON() ([]byte, error) {
	var fields model.Fields
	raw, err := json.Marshal(row.Tag)
	if err != nil {
		return nil, err
	}
	_ = fields.Set("tag", raw)
	if row.present[0] {
		raw, err = json.Marshal(row.Lookups)
		if err != nil {
			return nil, err
		}
		_ = fields.Set("lookups", raw)
	}
	if row.present[1] {
		raw, err = json.Marshal(row.FreshHits)
		if err != nil {
			return nil, err
		}
		_ = fields.Set("fresh_hits", raw)
	}
	if row.present[2] {
		raw, err = json.Marshal(row.StaleHits)
		if err != nil {
			return nil, err
		}
		_ = fields.Set("stale_hits", raw)
	}
	if row.present[3] {
		raw, err = json.Marshal(row.Misses)
		if err != nil {
			return nil, err
		}
		_ = fields.Set("misses", raw)
	}
	if row.present[4] {
		raw, err = json.Marshal(row.Expired)
		if err != nil {
			return nil, err
		}
		_ = fields.Set("expired", raw)
	}
	if row.present[5] {
		raw, err = json.Marshal(row.Entries)
		if err != nil {
			return nil, err
		}
		_ = fields.Set("entries", raw)
	}
	return json.Marshal(fields)
}

func (row UpstreamObservation) MarshalJSON() ([]byte, error) {
	var fields model.Fields
	raw, err := json.Marshal(row.Tag)
	if err != nil {
		return nil, err
	}
	_ = fields.Set("tag", raw)
	raw, err = json.Marshal(row.Upstream)
	if err != nil {
		return nil, err
	}
	_ = fields.Set("upstream", raw)
	if row.present[0] {
		raw, err = json.Marshal(row.Requests)
		if err != nil {
			return nil, err
		}
		_ = fields.Set("requests", raw)
	}
	if row.present[1] {
		raw, err = json.Marshal(row.Failed)
		if err != nil {
			return nil, err
		}
		_ = fields.Set("failed", raw)
	}
	if row.present[2] {
		raw, err = json.Marshal(row.LatencySumMS)
		if err != nil {
			return nil, err
		}
		_ = fields.Set("latency_sum_ms", raw)
	}
	if row.present[3] {
		raw, err = json.Marshal(row.LatencyCount)
		if err != nil {
			return nil, err
		}
		_ = fields.Set("latency_count", raw)
	}
	return json.Marshal(fields)
}
