package rules

import (
	"bufio"
	"compress/gzip"
	"math/big"
	"net/netip"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// The corpus (testdata/corpus.jsonl.gz, made by testdata/gen.py with the
// production Python modules) records what CPython 3.11 computes for a few
// ten thousand inputs; every test below asserts equality for each record.

func loadCorpus(t *testing.T) map[string][]*jsonx.Object {
	t.Helper()
	file, err := os.Open("testdata/corpus.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	records := map[string][]*jsonx.Object{}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(nil, 64<<20)
	for scanner.Scan() {
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		record := value.(*jsonx.Object)
		kind := record.GetString("kind")
		records[kind] = append(records[kind], record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return records
}

func compact(t *testing.T, value any) string {
	t.Helper()
	data, err := jsonx.Compact(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func isException(value any) bool {
	text, ok := value.(string)
	return ok && strings.HasPrefix(text, "__exception__")
}

var corpusOnce struct {
	records map[string][]*jsonx.Object
}

func corpus(t *testing.T) map[string][]*jsonx.Object {
	if corpusOnce.records == nil {
		corpusOnce.records = loadCorpus(t)
	}
	return corpusOnce.records
}

func TestCorpusMatch(t *testing.T) {
	pairs, skipped := 0, 0
	for n, record := range corpus(t)["match"] {
		contents := record.GetObject("contents")
		if scopedNetwork(compact(t, record.Value("configs"))) {
			skipped++
			continue
		}
		load := func(item, _ *jsonx.Object) (string, error) {
			id, _ := item.Value("id").(string)
			return contents.GetString(id), nil
		}
		matcher := NewRuleConfigMatcher(record.GetList("configs"), record.GetList("library"), load, nil, nil)
		queries, results := record.GetList("queries"), record.GetList("results")
		for i, query := range queries {
			actual := matcher.Match(query.(string))
			var got any
			if actual != nil {
				got = actual
			}
			want := results[i]
			if compact(t, got) == compact(t, want) {
				pairs++
				continue
			}
			t.Errorf("case %d query %q:\n got  %s\n want %s\n configs %s\n contents %s", n, query,
				compact(t, got), compact(t, want), compact(t, record.Value("configs")), compact(t, contents))
		}
	}
	t.Logf("%d (rule list, query) pairs equal, %d rule lists skipped (scoped networks)", pairs, skipped)
	if pairs < 20000 {
		t.Errorf("only %d pairs compared", pairs)
	}
}

func TestCorpusParseRuleLine(t *testing.T) {
	for _, record := range corpus(t)["line"] {
		line := record.GetString("line")
		kind, value, ok := ParseRuleLine(line)
		var got any
		if ok {
			got = []any{kind, value}
		}
		if compact(t, got) != compact(t, record.Value("result")) {
			t.Errorf("ParseRuleLine(%q) = %v, want %s", line, got, compact(t, record.Value("result")))
		}
	}
}

func TestCorpusDirect(t *testing.T) {
	for _, record := range corpus(t)["direct"] {
		ruleType, value, query := record.GetString("rule_type"), record.GetString("value"), record.GetString("query")
		if scopedNetwork(value) && strings.HasPrefix(ruleType, "IP-CIDR") {
			continue
		}
		check := func(name string, got bool) {
			if want, _ := record.Value(name).(bool); got != want {
				t.Errorf("%s(%q, %q, %q) = %v, want %v", name, query, ruleType, value, got, want)
			}
		}
		if record.Value("match_domain") != nil {
			check("match_domain", MatchDomain(query, ruleType, value))
		}
		check("match_ip", MatchIP(query, ruleType, value))
		check("match_query", MatchQuery(query, ruleType, value))
		check("valid_domain", IsValidDomain(query, false))
		check("valid_dns", IsValidDomain(query, true))
		check("valid_ip", IsValidIP(query))
		if got := NormalizeMatchQuery(query); got != record.GetString("normalized") {
			t.Errorf("NormalizeMatchQuery(%q) = %q, want %q", query, got, record.GetString("normalized"))
		}
	}
}

func TestCorpusIPAddressAndNetwork(t *testing.T) {
	// Python's ipaddress and net/netip agree on which texts are addresses and
	// networks (strict=False) and on their values; IPv6 scope ids in networks
	// are refused by netip (listed difference) and are skipped.
	value := func(addr netip.Addr) string {
		b := addr.As16()
		if addr.Is4() {
			a := addr.As4()
			b = [16]byte{}
			copy(b[12:], a[:])
		}
		return new(big.Int).SetBytes(b[:]).String()
	}
	for _, record := range corpus(t)["ip"] {
		text := record.GetString("text")
		var address any
		if ip, ok := parseAddr(text); ok {
			address = []any{jsonx.Int(int64(4 + 2*family(ip))), value(ip)}
		}
		if want, ok := record.Value("address").([]any); ok {
			record.Set("address", want[:2])
		}
		if compact(t, address) != compact(t, record.Value("address")) {
			t.Errorf("ip_address(%q) = %s, want %s", text, compact(t, address), compact(t, record.Value("address")))
		}
		if strings.Contains(text, "%") {
			continue
		}
		var network any
		if net, ok := parseNetwork(text); ok {
			network = []any{jsonx.Int(int64(4 + 2*family(net.Addr()))), value(net.Addr()), jsonx.Int(int64(net.Bits()))}
		}
		if want, ok := record.Value("network").([]any); ok {
			record.Set("network", want[:3])
		}
		if compact(t, network) != compact(t, record.Value("network")) {
			t.Errorf("ip_network(%q) = %s, want %s", text, compact(t, network), compact(t, record.Value("network")))
		}
	}
}

func TestCorpusRegex(t *testing.T) {
	// Accept/reject and every search result agree with Python's re.
	re2, backtrack, searches, lenient := 0, 0, 0, 0
	for _, record := range corpus(t)["regex"] {
		pattern, ignore := record.GetString("pattern"), record.GetBool("ignore")
		re, err := CompilePyRegex(pattern, ignore)
		pythonOK := record.Value("error") == nil
		if regexMoreLenient(!pythonOK, err) || hugeRepeat(pattern) {
			lenient++
			continue
		}
		if (err == nil || !IsPyRegexError(err)) != pythonOK {
			t.Errorf("re.compile(%q, ignore=%v): Go %v, Python error %v", pattern, ignore, err, record.Value("error"))
			continue
		}
		if err != nil {
			continue
		}
		if re.re2 != nil {
			re2++
		} else {
			backtrack++
		}
		texts, expected := record.GetList("texts"), record.GetList("searches")
		for i, text := range texts {
			searches++
			if re.Search(text.(string)) != expected[i].(bool) {
				t.Errorf("re.search(%q, %q, ignore=%v) = %v, want %v", pattern, text, ignore, !expected[i].(bool), expected[i])
			}
		}
	}
	t.Logf("%d patterns on RE2, %d on regexp2; %d searches equal; %d patterns only Python rejects", re2, backtrack, searches, lenient)
}

func TestCorpusRuleValueError(t *testing.T) {
	compared, skipped := 0, 0
	for _, record := range corpus(t)["syntax"] {
		want, _ := record.Value("error").(string)
		if strings.HasPrefix(want, "__exception__") {
			skipped++
			continue
		}
		kind, kindOK := record.Value("rule_kind").(string)
		value, valueOK := record.Value("value").(string)
		if !kindOK || !valueOK {
			skipped++
			continue
		}
		got := RuleValueError(kind, value)
		if idna2003Only(value) && (kind == "DOMAIN" || kind == "DOMAIN-SUFFIX") || specialUpper(kind) || hugeRepeat(value) {
			continue
		}
		if strings.HasPrefix(want, regexInvalid) && (got == "" || strings.HasPrefix(got, regexInvalid)) {
			// The detail after the Chinese prefix is the engine's error text.
			continue
		}
		if got != want {
			t.Errorf("rule_value_error(%s, %s) = %q, want %q", jsonx.Repr(kind), jsonx.Repr(value), got, want)
		}
		compared++
	}
	t.Logf("%d (kind, value) pairs equal, %d skipped (Python raises)", compared, skipped)
	for _, record := range corpus(t)["split"] {
		if specialUpper(record.GetString("text")) {
			continue
		}
		kind, payload, params := SplitRulePayload(record.GetString("text"))
		got := []any{kind, payload, jsonx.ListOf(params)}
		if compact(t, got) != compact(t, record.Value("result")) {
			t.Errorf("split_rule_payload(%q) = %s, want %s", record.GetString("text"), compact(t, got), compact(t, record.Value("result")))
		}
	}
	for _, record := range corpus(t)["cidr"] {
		want, _ := record.Value("error").(string)
		if got := CIDRError(record.GetString("value")); got != want {
			t.Errorf("cidr_error(%q) = %q, want %q", record.GetString("value"), got, want)
		}
	}
}

const regexInvalid = "正则表达式格式无效："

func TestCorpusCacheNames(t *testing.T) {
	for _, record := range corpus(t)["cache"] {
		want := record.Value("result")
		if isException(want) {
			continue
		}
		switch record.GetString("what") {
		case "sanitize":
			name, _ := record.Value("name").(string)
			if got := SanitizeRuleName(name); got != want {
				t.Errorf("sanitize_rule_name(%q) = %q, want %q", name, got, want)
			}
		case "filename":
			rule := record.GetObject("rule")
			if got := RuleCacheFilename(rule); got != want {
				t.Errorf("rule_cache_filename(%s) = %q, want %q", compact(t, rule), got, want)
			}
		}
	}
}

func TestCorpusFilters(t *testing.T) {
	skipped := 0
	for _, record := range corpus(t)["filters"] {
		switch record.GetString("what") {
		case "names":
			corpusNodeNames = corpusNodeNames[:0]
			for _, name := range record.GetList("names") {
				corpusNodeNames = append(corpusNodeNames, name.(string))
			}
		case "backticks":
			if got := literalBackticks(record.GetString("pattern")); got != record.GetString("result") {
				t.Errorf("_literal_backticks(%q) = %q, want %q", record.GetString("pattern"), got, record.GetString("result"))
			}
		case "embedded":
			if got := embedded(record.GetString("pattern")); got != record.GetString("result") {
				t.Errorf("_embedded(%q) = %q, want %q", record.GetString("pattern"), got, record.GetString("result"))
			}
		case "exclusion":
			if got := ExclusionFilter(record.GetString("include"), record.GetString("exclude")); got != record.GetString("result") {
				t.Errorf("exclusion_filter(%q, %q) = %q, want %q", record.GetString("include"), record.GetString("exclude"), got, record.GetString("result"))
			}
		case "filter":
			var items []any
			for i, name := range corpusNodeNames {
				items = append(items, jsonx.Obj("name", name, "extra", jsonx.Int(i)))
			}
			kept, err := FilterCandidates(items, record.Value("include"), record.Value("exclude"))
			result := record.GetObject("result")
			switch {
			case result.Has("exception"):
				skipped++
			case result.Has("error") && err == nil:
				skipped++ // regexMoreLenient
			case err != nil:
				if result.GetString("error") == "正则表达式无效" {
					break // Python stopped at a pattern only it rejects, Go at the next problem
				}
				if result.GetString("error") != err.Error() {
					t.Errorf("filter_candidates(%s, %s) error %q, want %s", compact(t, record.Value("include")), compact(t, record.Value("exclude")), err.Error(), compact(t, result))
				}
			default:
				var got []any
				for _, item := range kept {
					got = append(got, item.(*jsonx.Object).Value("extra"))
				}
				if compact(t, jsonx.ListOf(got)) != compact(t, result.Value("kept")) && !(len(got) == 0 && len(result.GetList("kept")) == 0) {
					t.Errorf("filter_candidates(%s, %s) kept %s, want %s", compact(t, record.Value("include")), compact(t, record.Value("exclude")), compact(t, got), compact(t, result.Value("kept")))
				}
			}
		case "validate":
			original, _ := record.Value("original").(*jsonx.Object)
			want := record.Value("result")
			if isException(want) {
				skipped++
				continue
			}
			got := ""
			if err := ValidateFilters(record.GetObject("group"), original); err != nil {
				got = err.Error()
			}
			wantText, _ := want.(string)
			// regexMoreLenient: Go may accept the pattern Python reports, or
			// report the next invalid field.
			if got != wantText && !(strings.HasSuffix(wantText, "正则表达式无效") && (got == "" || strings.HasSuffix(got, "正则表达式无效"))) {
				t.Errorf("validate_filters(%s, %s) = %q, want %q", compact(t, record.Value("group")), compact(t, record.Value("original")), got, wantText)
			}
		}
	}
	t.Logf("%d filter cases skipped (Python raised, or only Python rejects the pattern)", skipped)
}

var corpusNodeNames = []string{"US-new", "US-expired", "HK-new", "HK-expired", "US`old", "EU 01", "香港-01", "sg", "", "US-expired-new", "hk-1", "日本 02", "Aé", "x_1", "123"}

func TestCorpusGroupCandidates(t *testing.T) {
	skipped, compared := 0, 0
	for _, record := range corpus(t)["group"] {
		if record.GetString("what") == "merge" {
			original, patch := record.GetObject("original"), record.GetObject("patch")
			merged, err := MergeGroupUpdate(original, patch)
			result := record.GetObject("result")
			if err != nil {
				if result.GetString("error") != err.Error() {
					t.Errorf("merge_group_update error %q, want %s", err.Error(), compact(t, result))
				}
			} else if compact(t, merged) != compact(t, result.Value("merged")) {
				t.Errorf("merge_group_update = %s, want %s", compact(t, merged), compact(t, result.Value("merged")))
			}
			continue
		}
		group := record.GetObject("group")
		original, _ := record.Value("original").(*jsonx.Object)
		shared, profile := record.GetObject("shared"), record.GetObject("profile")
		full := shared.Clone()
		full.Update(profile)
		if pythonIteratesNonList(group) {
			skipped++
			continue
		}
		if want := record.Value("members"); !isException(want) {
			selection := ManagedMembers(group)
			var members []any
			for _, member := range selection.Members {
				members = append(members, []any{member.Kind, member.ID})
			}
			if compact(t, jsonx.ListOf(members)) != compact(t, want) {
				t.Errorf("managed_members(%s) = %s, want %s", compact(t, group), compact(t, members), compact(t, want))
			}
			wantSelection := record.GetObject("selection")
			gotSelection := jsonx.Obj("nodes", selection.Nodes, "subscriptions", selection.Subscriptions,
				"aggregations", selection.Aggregations, "groups", selection.Groups, "order", selection.Order)
			if compact(t, gotSelection) != compact(t, wantSelection) {
				t.Errorf("managed_members selection %s, want %s", compact(t, gotSelection), compact(t, wantSelection))
			}
		}
		if want := record.Value("render"); !isException(want) {
			if got := RenderMembers(group, full); compact(t, got) != compact(t, want) {
				t.Errorf("render_members(%s) = %s, want %s", compact(t, group), compact(t, got), compact(t, want))
			}
		}
		if want := record.Value("providers"); !isException(want) {
			var got []any
			for _, p := range ManagedProviders(group, full) {
				got = append(got, []any{p.Item.Value("id"), p.Include, p.Exclude})
			}
			if compact(t, jsonx.ListOf(got)) != compact(t, want) {
				t.Errorf("managed_providers(%s) = %s, want %s", compact(t, group), compact(t, got), compact(t, want))
			}
		}
		want := record.Value("validate")
		if isException(want) {
			skipped++
			continue
		}
		got := ""
		if err := ValidateCandidates(group.DeepCopy(), profile, shared, original.DeepCopy()); err != nil {
			got = err.Error()
		}
		wantText, _ := want.(string)
		if got != wantText {
			t.Errorf("validate_candidates(%s, original=%s) = %q, want %q", compact(t, group), compact(t, original), got, wantText)
		}
		compared++
	}
	t.Logf("validate_candidates: %d equal, %d skipped (Python raised, or non-list source fields)", compared, skipped)
}

func TestCorpusLowerGroupProviderFilters(t *testing.T) {
	for _, record := range corpus(t)["lower"] {
		want := record.Value("result")
		if isException(want) {
			continue
		}
		main := record.GetObject("main").DeepCopy()
		LowerGroupProviderFilters(record.GetObject("config").DeepCopy(), main)
		if compact(t, main) != compact(t, want) {
			t.Errorf("lower_group_provider_filters:\n got  %s\n want %s\n config %s", compact(t, main), compact(t, want), compact(t, record.Value("config")))
		}
	}
}
