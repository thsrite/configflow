package main

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
)

// rule is one entry of server/parity/proddiff-normalize.json.
type rule struct {
	Route           string
	Path            string
	HasPath         bool
	Reason          string
	Pattern         *regexp2.Regexp
	Tolerance       float64
	HasTolerance    bool
	Bracket         bool
	Unordered       bool
	TimestampFormat string
	Mode            string
}

var ruleFields = map[string]bool{"route": true, "path": true, "reason": true, "pattern": true, "tolerance": true,
	"bracket": true, "unordered": true, "timestamp_format": true, "mode": true}

// modes are the reference modes a rule may be limited to.
var modes = map[string]bool{"python": true, "go": true}

// errRule is the framework failure for an invalid normalization file.
var errRule = errors.New("normalization requires an exact route/path and reason")

var pd1GeneratedRoutes = func() map[string]bool {
	out := map[string]bool{}
	for _, prefix := range []string{"/api/generate", "/api/profiles/<profile_id>/generate"} {
		for _, service := range []string{"mihomo", "surge", "loon"} {
			out[prefix+"/"+service] = true
		}
	}
	return out
}()

// New read-only routes stamp only these request-owned fields; keep their wire formats strict.
const requestSecond = `[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}`

var requestClockFormats = map[[2]string]*regexp.Regexp{
	{"/api/agents/<agent_id>/ipv6-readiness", "refresh_error.at"}: regexp.MustCompile(`^` + requestSecond + `[+-][0-9]{2}:[0-9]{2}$`),
	{"/api/agents/<agent_id>/upstream-routers", "checked_at"}:     regexp.MustCompile(`^` + requestSecond + `[+-][0-9]{2}:[0-9]{2}$`),
	{"/api/client-devices/discovery", "checked_at"}:               regexp.MustCompile(`^` + requestSecond + `[+-][0-9]{2}:[0-9]{2}$`),
	{"/api/notifications", "checked_at"}:                          regexp.MustCompile(`^` + requestSecond + `\.[0-9]{6}\+00:00$`),
	{"/api/routers/<router_id>/policy", "generated_at"}:           regexp.MustCompile(`^` + requestSecond + `$`),
}

// loadRules reads and validates the normalization file; only rules for the
// given mode (or without a mode) are returned.
func loadRules(path string, routes map[string]bool, mode string) ([]rule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read normalization rules: %w", err)
	}
	raw, err := loadJSON(data)
	if err != nil {
		return nil, fmt.Errorf("parse normalization rules: %w", err)
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, errRule
	}
	var out []rule
	for _, item := range list {
		r, keep, err := validRule(item, routes, mode)
		if err != nil {
			return nil, err
		}
		if keep {
			out = append(out, r)
		}
	}
	return out, nil
}

func validRule(item any, routes map[string]bool, mode string) (rule, bool, error) {
	m, ok := item.(*Map)
	if !ok {
		return rule{}, false, errRule
	}
	for _, k := range m.Keys() {
		if s, ok := k.(string); !ok || !ruleFields[s] {
			return rule{}, false, errRule
		}
	}
	var r rule
	route, _ := m.Get("route")
	r.Route, ok = route.(string)
	if !ok || !routes[r.Route] {
		return rule{}, false, errRule
	}
	if reason, _ := m.Get("reason"); !truthy(reason) {
		return rule{}, false, errRule
	} else if s, ok := reason.(string); ok {
		r.Reason = s
	}
	pathValue, hasPath := m.Get("path")
	if hasPath {
		r.Path, ok = pathValue.(string)
		if !ok {
			return rule{}, false, errRule
		}
		r.HasPath = true
	}
	if strings.Contains(r.Route+r.Path, "*") {
		return rule{}, false, errRule
	}
	bracket, _ := m.Get("bracket")
	r.Bracket = truthy(bracket)
	if r.Bracket && hasPath || !r.Bracket && !truthy(pathValue) {
		return rule{}, false, errRule
	}
	modesSet := 0
	for _, key := range []string{"pattern", "tolerance", "unordered"} {
		if m.Has(key) {
			modesSet++
		}
	}
	if modesSet > 1 {
		return rule{}, false, errRule
	}
	unordered, _ := m.Get("unordered")
	r.Unordered = truthy(unordered)
	tolerance, hasTolerance := m.Get("tolerance")
	if hasTolerance {
		if !isNumber(tolerance) {
			return rule{}, false, errRule
		}
		r.Tolerance, r.HasTolerance = toFloat(tolerance), true
	}
	if format, ok := m.Get("timestamp_format"); ok {
		r.TimestampFormat, _ = format.(string)
		exactInt := func(want int64) bool { n, ok := tolerance.(int64); return ok && n == want }
		switch r.TimestampFormat {
		case "http-date":
			if r.Path != "header.last-modified" || !pd1GeneratedRoutes[r.Route] || !exactInt(600) {
				return rule{}, false, errRule
			}
		case "request-clock":
			if _, ok := requestClockFormats[[2]string{r.Route, r.Path}]; !ok || !hasPath || !exactInt(120) {
				return rule{}, false, errRule
			}
		default:
			return rule{}, false, errRule
		}
	}
	// "mode" limits a rule to one reference: Go-vs-Go-only runtime identities never loosen python mode.
	r.Mode = mode
	if value, ok := m.Get("mode"); ok {
		s, isString := value.(string)
		if !isString || !modes[s] {
			return rule{}, false, errRule
		}
		r.Mode = s
	}
	if pattern, ok := m.Get("pattern"); ok {
		text, isString := pattern.(string)
		if !isString {
			return rule{}, false, errRule
		}
		compiled, err := compilePattern(text)
		if err != nil {
			return rule{}, false, fmt.Errorf("compile normalization pattern: %w", err)
		}
		r.Pattern = compiled
	}
	return r, r.Mode == mode, nil
}

// compilePattern compiles a Python `re` pattern. regexp2 (already a server
// dependency) supports the look-arounds the rule file uses; RE2 does not.
func compilePattern(text string) (*regexp2.Regexp, error) {
	return regexp2.Compile(text, regexp2.None)
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case int64:
		return float64(x)
	case float64:
		return x
	case bool:
		return float64(boolInt(x))
	}
	if b, ok := bigOf(v); ok {
		f, _ := new(big.Float).SetInt(b).Float64()
		return f
	}
	return math.NaN()
}

// target addresses container[key]: a *Map entry or a list element.
type target struct {
	m     *Map
	list  []any
	key   any
	index int
}

func (t target) get() any {
	if t.m != nil {
		v, _ := t.m.Get(t.key)
		return v
	}
	return t.list[t.index]
}

func (t target) set(v any) {
	if t.m != nil {
		t.m.Set(t.key, v)
		return
	}
	t.list[t.index] = v
}

// sameKey compares the container keys of two targets.
func (t target) sameKey(o target) bool {
	if t.m != nil && o.m != nil {
		return pyEqual(t.key, o.key)
	}
	if t.m == nil && o.m == nil {
		return t.index == o.index
	}
	return false
}

// walk yields the targets addressed by a dotted path; `name[]` means every element.
func walk(root any, parts []string) []target {
	current := []any{root}
	for i, part := range parts {
		last := i == len(parts)-1
		every := strings.HasSuffix(part, "[]")
		name := strings.TrimSuffix(part, "[]")
		var following []target
		for _, value := range current {
			m, ok := value.(*Map)
			if !ok {
				continue
			}
			child, ok := m.Get(name)
			if !ok {
				continue
			}
			if every {
				if list, ok := child.([]any); ok {
					for j := range list {
						following = append(following, target{list: list, index: j})
					}
				}
			} else {
				following = append(following, target{m: m, key: name})
			}
		}
		if last {
			return following
		}
		current = current[:0:0]
		for _, t := range following {
			current = append(current, t.get())
		}
	}
	return nil
}

// applyRules normalizes the route's declared paths on both sides in place.
func applyRules(rules []rule, route string, a, b *side) {
	for _, r := range rules {
		if r.Route != route || !r.HasPath {
			continue
		}
		var leftRoot, rightRoot any
		var parts []string
		if strings.HasPrefix(r.Path, "header.") {
			leftRoot, rightRoot = a.header, b.header
			parts = []string{strings.TrimPrefix(r.Path, "header.")}
		} else {
			leftRoot, rightRoot = a.body, b.body
			parts = strings.Split(r.Path, ".")
		}
		if _, ok := leftRoot.(*Map); !ok {
			continue
		}
		if _, ok := rightRoot.(*Map); !ok {
			continue
		}
		left, right := walk(leftRoot, parts), walk(rightRoot, parts)
		if r.Pattern != nil {
			// Masking is a per-value transform: masked values only become equal
			// when everything outside the pattern is equal.
			for _, t := range append(left, right...) {
				if s, ok := t.get().(string); ok {
					if masked, err := r.Pattern.Replace(s, "<runtime>", -1, -1); err == nil {
						t.set(masked)
					}
				}
			}
			continue
		}
		if len(left) != len(right) {
			continue
		}
		for i := range left {
			lt, rt := left[i], right[i]
			if !lt.sameKey(rt) {
				continue
			}
			if r.Unordered {
				// Multiset comparison for lists whose order is not defined.
				ll, lok := lt.get().([]any)
				rl, rok := rt.get().([]any)
				if lok && rok {
					lt.set(sortedByJSON(ll))
					rt.set(sortedByJSON(rl))
				}
				continue
			}
			if l, rv, ok := normalizePair(r, lt.get(), rt.get()); ok {
				lt.set(l)
				rt.set(rv)
			}
		}
	}
}

func sortedByJSON(list []any) []any {
	type keyed struct {
		key  string
		item any
	}
	items := make([]keyed, len(list))
	for i, item := range list {
		items[i] = keyed{dumpJSON(item, dumpOptions{indent: -1, sortKeys: true}), item}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].key < items[j].key })
	out := make([]any, len(list))
	for i, item := range items {
		out[i] = item.item
	}
	return out
}

var digit = regexp.MustCompile(`[0-9]`)

// normalizePair returns the normalized pair, or ok=false when the rule's
// preconditions do not hold (the difference stays).
func normalizePair(r rule, left, right any) (any, any, bool) {
	if pyType(left) != pyType(right) {
		return nil, nil, false
	}
	switch left.(type) {
	case *Map, []any:
		return nil, nil, false
	}
	if r.HasTolerance && r.TimestampFormat == "http-date" {
		a, aok := httpDate(left)
		b, bok := httpDate(right)
		if !aok || !bok || math.Abs(a-b) > r.Tolerance {
			return nil, nil, false
		}
		return "<runtime-http-date>", "<runtime-http-date>", true
	}
	if r.TimestampFormat == "request-clock" {
		pattern := requestClockFormats[[2]string{r.Route, r.Path}]
		ls, lok := left.(string)
		rs, rok := right.(string)
		if pattern == nil || !lok || !rok || !pattern.MatchString(ls) || !pattern.MatchString(rs) {
			return nil, nil, false
		}
	}
	if r.HasTolerance {
		shape := func(v any) string {
			if s, ok := v.(string); ok {
				return digit.ReplaceAllString(s, "9")
			}
			return pyType(v)
		}
		a, aok := timestamp(left)
		b, bok := timestamp(right)
		if !aok || !bok || shape(left) != shape(right) || math.Abs(a-b) > r.Tolerance {
			return nil, nil, false
		}
	}
	return "<runtime>", "<runtime>", true
}

// timestamp reads epoch numbers and ISO 8601 text (datetime.fromisoformat).
func timestamp(v any) (float64, bool) {
	switch x := v.(type) {
	case int64, float64, *big.Int:
		return toFloat(x), true
	case string:
		t, ok := fromISOFormat(x)
		if !ok {
			return 0, false
		}
		return float64(t.UnixNano()) / 1e9, true
	}
	return 0, false
}

// isoPattern covers datetime.fromisoformat (Python 3.11) for the extended and
// basic calendar forms the server writes: date, optional time with any number
// of fractional digits, optional Z or numeric offset. Naive values read as UTC.
var isoPattern = regexp.MustCompile(`^([0-9]{4})-?([0-9]{2})-?([0-9]{2})(?:(?s:.)([0-9]{2})(?::?([0-9]{2})(?::?([0-9]{2})(?:[.,]([0-9]+))?)?)?(Z|[+-][0-9]{2}(?::?[0-9]{2}(?::?[0-9]{2}(?:\.[0-9]{1,6})?)?)?)?)?$`)

func fromISOFormat(s string) (time.Time, bool) {
	if !utf8.ValidString(s) {
		return time.Time{}, false
	}
	m := isoPattern.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	// The extended and basic date forms may not be mixed.
	if (len(s) >= 5 && s[4] == '-') != (len(s) >= 8 && s[7] == '-') {
		return time.Time{}, false
	}
	n := func(text string) int {
		v, _ := strconv.Atoi(text)
		return v
	}
	year, month, day := n(m[1]), n(m[2]), n(m[3])
	hour, minute, second := n(m[4]), n(m[5]), n(m[6])
	nanos := 0
	if frac := m[7]; frac != "" {
		if len(frac) > 9 {
			frac = frac[:9]
		}
		nanos = n(frac + strings.Repeat("0", 9-len(frac)))
		nanos = nanos / 1000 * 1000 // microsecond precision
	}
	if month < 1 || month > 12 || day < 1 || hour > 23 || minute > 59 || second > 59 {
		return time.Time{}, false
	}
	loc := time.UTC
	if zone := m[8]; zone != "" && zone != "Z" {
		digits := strings.ReplaceAll(zone[1:], ":", "")
		hh, mm, ss := n(digits[0:2]), 0, 0
		if len(digits) >= 4 {
			mm = n(digits[2:4])
		}
		if len(digits) >= 6 {
			ss = n(digits[4:6])
		}
		if hh > 23 || mm > 59 || ss > 59 {
			return time.Time{}, false
		}
		offset := hh*3600 + mm*60 + ss
		if zone[0] == '-' {
			offset = -offset
		}
		loc = time.FixedZone("", offset)
	}
	t := time.Date(year, time.Month(month), day, hour, minute, second, nanos, loc)
	if t.Day() != day || int(t.Month()) != month {
		return time.Time{}, false
	}
	return t, true
}

// httpDate accepts only the canonical IMF-fixdate a generated Last-Modified uses.
func httpDate(v any) (float64, bool) {
	s, ok := v.(string)
	if !ok {
		return 0, false
	}
	t, err := time.Parse(http.TimeFormat, s)
	if err != nil || t.UTC().Format(http.TimeFormat) != s {
		return 0, false
	}
	return float64(t.Unix()), true
}
