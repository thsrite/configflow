package rules

import (
	"bufio"
	"compress/gzip"
	"configflow/internal/pystr"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/logx"
)

// Review C08: references made by CPython 3.11.17 with the production modules
// on fuzzed inputs (testdata/review_c08_gen.py → review_c08.jsonl.gz): a
// regex grammar fuzzer (validity, error text, search spans with groups, with
// and without IGNORECASE), rule_value_error on mutated kinds and values,
// split_rule_payload, parse_rule_line in every dialect, ipaddress parsing,
// domain checks, match_query/match_domain, RuleConfigMatcher over fuzzed
// text and YAML rule sets, and str.upper/str.lower of every code point.

var reviewC08Records map[string][]*jsonx.Object

func reviewC08(t *testing.T) map[string][]*jsonx.Object {
	t.Helper()
	if reviewC08Records != nil {
		return reviewC08Records
	}
	file, err := os.Open("testdata/review_c08.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	records := map[string][]*jsonx.Object{}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(nil, 256<<20)
	for scanner.Scan() {
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		record := value.(*jsonx.Object)
		records[record.GetString("kind")] = append(records[record.GetString("kind")], record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	reviewC08Records = records
	return records
}

// pyCompileOutcome renders a parse error the way the generator records it.
func pyCompileOutcome(err error) (key, text string) {
	switch e := err.(type) {
	case nil:
		return "", ""
	case errRecursion:
		return "exc", "RecursionError"
	case errOverflow:
		return "exc", "OverflowError: " + e.Error()
	case *reError:
		return "error", e.Error()
	}
	return "exc", fmt.Sprintf("%T: %v", err, err)
}

func TestReviewC08Regex(t *testing.T) {
	compiled, failed, searches, spans, re2, backtrack := 0, 0, 0, 0, 0, 0
	for _, record := range reviewC08(t)["rx"] {
		pattern, ignore := record.GetString("pattern"), record.GetBool("ignore")
		flags := 0
		if ignore {
			flags = flagIgnoreCase
		}
		_, err := parsePyRegex(pattern, flags)
		key, text := pyCompileOutcome(err)
		wantKey, wantText := "", ""
		if v, ok := record.Get("error"); ok {
			wantKey, wantText = "error", v.(string)
		} else if v, ok := record.Get("exc"); ok {
			wantKey, wantText = "exc", v.(string)
		}
		if key != wantKey || text != wantText {
			t.Errorf("re.compile(%q, ignore=%v): got %s %q, want %s %q", pattern, ignore, key, text, wantKey, wantText)
			continue
		}
		if err != nil {
			failed++
			continue
		}
		re, err := CompilePyRegex(pattern, ignore)
		if err != nil {
			t.Errorf("re.compile(%q, ignore=%v): not runnable: %v", pattern, ignore, err)
			continue
		}
		compiled++
		if re.Engine() == "re2" {
			re2++
		} else {
			backtrack++
		}
		subjects, expected := record.GetList("subjects"), record.GetList("spans")
		for i, s := range subjects {
			subject := s.(string)
			want := expected[i]
			searches++
			if got := re.Search(subject); got != (want != nil) {
				t.Errorf("re.search(%q, %q, ignore=%v) [%s] = %v, want %v", pattern, subject, ignore, re.Engine(), got, want != nil)
				continue
			}
			m, err := re.Match(subject)
			if err != nil {
				t.Errorf("re.search(%q, %q): %v", pattern, subject, err)
				continue
			}
			var got any
			if m != nil {
				list := []any{jsonx.Int(m.spans[0]), jsonx.Int(m.spans[1])}
				for g := 1; g <= m.Groups(); g++ {
					a, b := m.Span(g)
					list = append(list, []any{jsonx.Int(a), jsonx.Int(b)})
				}
				got = list
			}
			spans++
			if compact(t, got) != compact(t, want) {
				t.Errorf("re.search(%q, %q, ignore=%v).spans = %s, want %s", pattern, subject, ignore, compact(t, got), compact(t, want))
			}
		}
	}
	t.Logf("%d patterns compiled (%d RE2, %d backtracking), %d rejected like Python; %d searches and %d span sets equal", compiled, re2, backtrack, failed, searches, spans)
}

func TestReviewC08RegexRuleValueError(t *testing.T) {
	compared, raised := 0, 0
	for _, record := range reviewC08(t)["rxerr"] {
		pattern := record.GetString("pattern")
		want, _ := record.Value("result").(string)
		if strings.HasPrefix(want, "__exception__") {
			// Python lets the exception escape (HTTP 500); Go reports it.
			raised++
			continue
		}
		if got := RuleValueError("DOMAIN-REGEX", pattern); got != want {
			t.Errorf("rule_value_error('DOMAIN-REGEX', %q) = %q, want %q", pattern, got, want)
		}
		compared++
	}
	t.Logf("%d equal, %d where Python raises", compared, raised)
}

func TestReviewC08Syntax(t *testing.T) {
	compared, raised := 0, 0
	for _, record := range reviewC08(t)["syn"] {
		want, _ := record.Value("result").(string)
		if strings.HasPrefix(want, "__exception__") {
			raised++
			continue
		}
		kind, value := record.Value("rule_kind"), record.Value("value")
		if got := RuleValueError(kind, value); got != want {
			t.Errorf("rule_value_error(%s, %s) = %q, want %q", jsonx.Repr(kind), jsonx.Repr(value), got, want)
		}
		compared++
	}
	for _, record := range reviewC08(t)["split"] {
		kind, payload, params := SplitRulePayload(record.GetString("text"))
		got := []any{kind, payload, jsonx.ListOf(params)}
		if compact(t, got) != compact(t, record.Value("result")) {
			t.Errorf("split_rule_payload(%q) = %s, want %s", record.GetString("text"), compact(t, got), compact(t, record.Value("result")))
		}
		compared++
	}
	t.Logf("%d equal, %d where Python raises", compared, raised)
}

func TestReviewC08Lines(t *testing.T) {
	for _, record := range reviewC08(t)["line"] {
		line := record.GetString("line")
		kind, value, ok := ParseRuleLine(line)
		var got any
		if ok {
			got = []any{kind, value}
		}
		if compact(t, got) != compact(t, record.Value("result")) {
			t.Errorf("parse_rule_line(%q) = %s, want %s", line, compact(t, got), compact(t, record.Value("result")))
		}
	}
}

func TestReviewC08IP(t *testing.T) {
	for _, record := range reviewC08(t)["ip"] {
		text := record.GetString("text")
		var address any
		if ip, ok := parseIPAddress(text); ok {
			var zone any
			if ip.zone != "" {
				zone = ip.zone
			}
			address = []any{jsonx.Int(ip.ver), bigString(ip.v), zone}
		}
		if compact(t, address) != compact(t, record.Value("address")) {
			t.Errorf("ip_address(%q) = %s, want %s", text, compact(t, address), compact(t, record.Value("address")))
		}
		var network any
		if net, ok := parseIPNetwork(text); ok {
			network = []any{jsonx.Int(net.ver), bigString(net.base), jsonx.Int(net.prefix), net.String()}
		}
		if compact(t, network) != compact(t, record.Value("network")) {
			t.Errorf("ip_network(%q) = %s, want %s", text, compact(t, network), compact(t, record.Value("network")))
		}
		want, _ := record.Value("cidr").(string)
		if got := CIDRError(text); got != want {
			t.Errorf("cidr_error(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestReviewC08Domains(t *testing.T) {
	for _, record := range reviewC08(t)["dom"] {
		text := record.GetString("text")
		want, _ := record.Value("error").(string)
		if got := DomainValueError(text); got != want {
			t.Errorf("domain_value_error(%q) = %q, want %q", text, got, want)
		}
		if got := IsValidDomain(text, false); got != record.GetBool("strict") {
			t.Errorf("is_valid_domain(%q) = %v", text, got)
		}
		if got := IsValidDomain(text, true); got != record.GetBool("dns") {
			t.Errorf("is_valid_domain(%q, allow_dns_labels=True) = %v", text, got)
		}
		if got := NormalizeMatchQuery(text); got != record.GetString("normalized") {
			t.Errorf("normalize_match_query(%q) = %q, want %q", text, got, record.GetString("normalized"))
		}
	}
	for _, record := range reviewC08(t)["mq"] {
		query, kind, value := record.GetString("query"), record.GetString("rule_type"), record.GetString("value")
		if want, ok := record.Value("match_query").(bool); ok {
			if got := MatchQuery(query, kind, value); got != want {
				t.Errorf("match_query(%q, %q, %q) = %v, want %v", query, kind, value, got, want)
			}
		}
		if want, ok := record.Value("match_domain").(bool); ok {
			if got := MatchDomain(query, kind, value); got != want {
				t.Errorf("match_domain(%q, %q, %q) = %v, want %v", query, kind, value, got, want)
			}
		}
	}
}

func TestReviewC08Matcher(t *testing.T) {
	pairs := 0
	for n, record := range reviewC08(t)["match"] {
		contents := record.GetObject("contents")
		load := func(item, _ *jsonx.Object) (string, error) {
			id, _ := item.Value("id").(string)
			return contents.GetString(id), nil
		}
		matcher := NewRuleConfigMatcher(record.GetList("configs"), nil, load, nil, nil)
		queries, results := record.GetList("queries"), record.GetList("results")
		for i, query := range queries {
			var got any
			if actual := matcher.Match(query.(string)); actual != nil {
				got = actual
			}
			pairs++
			if compact(t, got) != compact(t, results[i]) {
				t.Errorf("case %d query %q:\n got  %s\n want %s\n contents %s", n, query, compact(t, got), compact(t, results[i]), compact(t, contents))
			}
		}
	}
	t.Logf("%d (rule sets, query) pairs equal", pairs)
}

// TestReviewC08Case: str.upper(), str.lower() and str.isidentifier() of every
// code point (internal/pystr, CPython 3.11 tables), which split_rule_payload,
// parse_rule_line, the matcher and the regex group-name check rely on.
func TestReviewC08Case(t *testing.T) {
	records := reviewC08(t)["upper"]
	if len(records) != 1 {
		t.Fatal("missing case table")
	}
	table := func(key string) map[rune]string {
		out := map[rune]string{}
		for _, pair := range records[0].GetList(key) {
			p := pair.([]any)
			cp, _ := jsonx.AsInt(p[0])
			out[rune(cp)] = p[1].(string)
		}
		return out
	}
	set := func(key string) map[rune]bool {
		out := map[rune]bool{}
		for _, v := range records[0].GetList(key) {
			cp, _ := jsonx.AsInt(v)
			out[rune(cp)] = true
		}
		return out
	}
	upper, lower, start, cont := table("upper"), table("lower"), set("ident_start"), set("ident_continue")
	bad := 0
	report := func(format string, args ...any) {
		if bad++; bad <= 20 {
			t.Errorf(format, args...)
		}
	}
	for r := rune(0); r <= 0x10ffff; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		c := string(r)
		want, ok := upper[r]
		if !ok {
			want = c
		}
		if got := pystr.Upper(c); got != want {
			report("str.upper(U+%04X) = %q, want %q", r, got, want)
		}
		if want, ok = lower[r]; !ok {
			want = c
		}
		if got := pystr.Lower(c); got != want {
			report("str.lower(U+%04X) = %q, want %q", r, got, want)
		}
		if got := pystr.IsIdentifier(c); got != start[r] {
			report("U+%04X.isidentifier() = %v", r, got)
		}
		if got := pystr.IsIdentifier("a" + c); got != cont[r] {
			report("('a' + U+%04X).isidentifier() = %v", r, got)
		}
	}
	if bad > 0 {
		t.Errorf("%d differences", bad)
	}
}

// TestReviewC08YAMLPayload: _iter_ruleset_lines(text, 'yaml') with Python's
// CSafeLoader (libyaml) on token-soup documents (tabs, byte order marks, flow
// colons, escapes, tags, anchors, multi-line scalars, line break styles).
func TestReviewC08YAMLPayload(t *testing.T) {
	compared, nonEmpty := 0, 0
	for _, record := range reviewC08(t)["yaml"] {
		text := record.GetString("text")
		want := record.Value("result")
		if s, ok := want.(string); ok && strings.HasPrefix(s, "__exception__") {
			t.Errorf("Python raised on %q: %s", text, s)
			continue
		}
		got := []any{}
		for line := range iterRulesetLines(text, "yaml") {
			got = append(got, []any{line.value, line.source})
		}
		if len(got) > 0 {
			nonEmpty++
		}
		compared++
		if compact(t, got) != compact(t, want) {
			t.Errorf("_iter_ruleset_lines(%q):\n got  %s\n want %s", text, compact(t, got), compact(t, want))
		}
	}
	t.Logf("%d YAML documents equal (%d with entries)", compared, nonEmpty)
}

type pathError struct{}

func (pathError) Error() string       { return "open /secret/x: no such file" }
func (pathError) PythonClass() string { return "FileNotFoundError" }

// TestReviewC08MatcherLogLines: without an injected logger the matcher logs
// like rule_matcher's module logger: the load failure with
// safe_exception_details (Python class name, no message) and the skip warning.
func TestReviewC08MatcherLogLines(t *testing.T) {
	var buffer strings.Builder
	root, err := logx.Setup(logx.Config{Level: logx.Debug, Console: &buffer})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		root.Close()
		_, _ = logx.Setup(logx.Config{Level: logx.Critical})
	}()
	configs := []any{jsonx.Obj("id", "s", "itemType", "ruleset", "name", "规则 A", "behavior", "domain")}
	failing := func(*jsonx.Object, *jsonx.Object) (string, error) { return "", pathError{} }
	if got := NewRuleConfigMatcher(configs, nil, failing, nil, nil).Match("example.com"); got != nil {
		t.Fatalf("matched %v", got)
	}
	empty := func(*jsonx.Object, *jsonx.Object) (string, error) { return "", nil }
	NewRuleConfigMatcher([]any{jsonx.Obj("id", "s", "itemType", "ruleset", "name", jsonx.Int(5))}, nil, empty, nil, nil).Match("example.com")
	want := []string{
		` - backend.utils.rule_matcher - ERROR - Error processing ruleset "规则 A": exception_type=FileNotFoundError`,
		` - backend.utils.rule_matcher - WARNING - Skipping ruleset "规则 A": unable to fetch content`,
		` - backend.utils.rule_matcher - WARNING - Skipping ruleset "5": unable to fetch content`,
	}
	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	if len(lines) != len(want) {
		t.Fatalf("log lines:\n%s", buffer.String())
	}
	for i, line := range lines {
		if !strings.HasSuffix(line, want[i]) {
			t.Errorf("line %d = %q, want suffix %q", i, line, want[i])
		}
	}
}
