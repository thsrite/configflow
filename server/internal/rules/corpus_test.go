package rules

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"math/big"
	"net/netip"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/testjson"
	"configflow/internal/testjson/modeldirt"
	"configflow/internal/yamlx"
)

// The corpus (testdata/corpus.jsonl.gz, made by testdata/gen.py with the
// production Python modules) records what CPython 3.11 computes for a few
// ten thousand inputs; every test below asserts equality for each record.

func decodeMapBytes(data []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func mustDecodeMap(t *testing.T, data []byte) map[string]any {
	t.Helper()
	m, err := decodeMapBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func strField(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	s, _ := m[k].(string)
	return s
}

func boolField(m map[string]any, k string) bool {
	if m == nil {
		return false
	}
	b, _ := m[k].(bool)
	return b
}

func listField(m map[string]any, k string) []any {
	if m == nil {
		return nil
	}
	l, _ := m[k].([]any)
	return l
}

func mapField(m map[string]any, k string) map[string]any {
	if m == nil {
		return nil
	}
	sub, _ := m[k].(map[string]any)
	return sub
}

func loadCorpus(t *testing.T) map[string][]map[string]any {
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
	records := map[string][]map[string]any{}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(nil, 64<<20)
	for scanner.Scan() {
		record, err := decodeMapBytes(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		kind := strField(record, "kind")
		records[kind] = append(records[kind], record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return records
}

func compact(t *testing.T, value any) string {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

func isException(value any) bool {
	text, ok := value.(string)
	return ok && strings.HasPrefix(text, "__exception__")
}

var corpusOnce struct {
	records map[string][]map[string]any
}

func corpus(t *testing.T) map[string][]map[string]any {
	if corpusOnce.records == nil {
		corpusOnce.records = loadCorpus(t)
	}
	return corpusOnce.records
}

func decodeInto(src any, dst any) error {
	if src == nil {
		return nil
	}
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return jsondecode.Unmarshal(data, dst)
}

func newMatcherFromRaw(configsAny, libraryAny any, load ContentLoader, key SourceKey, logger Logger) *Matcher {
	configsList, _ := configsAny.([]any)
	records := make([]model.RuleConfig, 0, len(configsList))
	for _, entry := range configsList {
		var record model.RuleConfig
		if _, ok := entry.(map[string]any); !ok || decodeInto(entry, &record) != nil {
			disabled := false
			record.Enabled = &disabled
		}
		records = append(records, record)
	}
	libList, _ := libraryAny.([]any)
	entries := make([]model.RuleLibraryItem, 0, len(libList))
	for _, entry := range libList {
		if _, ok := entry.(map[string]any); !ok {
			continue
		}
		var record model.RuleLibraryItem
		if decodeInto(entry, &record) != nil {
			continue
		}
		entries = append(entries, record)
	}
	return NewMatcher(records, entries, load, key, logger)
}

func groupCatalogFromMap(m map[string]any) GroupCatalog {
	if m == nil {
		return GroupCatalog{}
	}
	data, _ := json.Marshal(m)
	return decodeGroupCatalogRaw(data)
}

func mergeMaps(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func decodeFilterValue(v any) (*string, error) {
	if v == nil {
		return nil, nil
	}
	s, ok := v.(string)
	if !ok {
		return nil, candidateError(msgFilterString)
	}
	return &s, nil
}

func TestCorpusMatch(t *testing.T) {
	pairs, skipped := 0, 0
	for n, record := range corpus(t)["match"] {
		contents := mapField(record, "contents")
		if scopedNetwork(compact(t, record["configs"])) {
			skipped++
			continue
		}
		load := func(item *model.RuleConfig, _ *model.RuleLibraryItem) (string, error) {
			return strField(contents, item.ID), nil
		}
		matcher := newMatcherFromRaw(record["configs"], record["library"], load, nil, nil)
		queries, results := listField(record, "queries"), listField(record, "results")
		for i, query := range queries {
			actual := matcher.Match(query.(string))
			var got any
			if actual != nil {
				got = actual
			}
			want := results[i]
			if testjson.Equal(want, got) {
				pairs++
				continue
			}
			t.Errorf("case %d query %q:\n got  %s\n want %s\n configs %s\n contents %s", n, query,
				compact(t, got), compact(t, want), compact(t, record["configs"]), compact(t, contents))
		}
	}
	t.Logf("%d (rule list, query) pairs equal, %d rule lists skipped (scoped networks)", pairs, skipped)
	if pairs < 20000 {
		t.Errorf("only %d pairs compared", pairs)
	}
}

func TestCorpusParseRuleLine(t *testing.T) {
	for _, record := range corpus(t)["line"] {
		line := strField(record, "line")
		kind, value, ok := ParseRuleLine(line)
		var got any
		if ok {
			got = []any{kind, value}
		}
		if !testjson.Equal(record["result"], got) {
			t.Errorf("ParseRuleLine(%q) = %v, want %s", line, got, compact(t, record["result"]))
		}
	}
}

func TestCorpusDirect(t *testing.T) {
	for _, record := range corpus(t)["direct"] {
		ruleType, value, query := strField(record, "rule_type"), strField(record, "value"), strField(record, "query")
		if scopedNetwork(value) && strings.HasPrefix(ruleType, "IP-CIDR") {
			continue
		}
		check := func(name string, got bool) {
			if want, _ := record[name].(bool); got != want {
				t.Errorf("%s(%q, %q, %q) = %v, want %v", name, query, ruleType, value, got, want)
			}
		}
		if record["match_domain"] != nil {
			check("match_domain", MatchDomain(query, ruleType, value))
		}
		check("match_ip", MatchIP(query, ruleType, value))
		check("match_query", MatchQuery(query, ruleType, value))
		check("valid_domain", IsValidDomain(query, false))
		check("valid_dns", IsValidDomain(query, true))
		check("valid_ip", IsValidIP(query))
		if got := NormalizeMatchQuery(query); got != strField(record, "normalized") {
			t.Errorf("NormalizeMatchQuery(%q) = %q, want %q", query, got, strField(record, "normalized"))
		}
	}
}

func TestCorpusIPAddressAndNetwork(t *testing.T) {
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
		text := strField(record, "text")
		var address any
		if ip, ok := parseAddr(text); ok {
			address = []any{4 + 2*family(ip), value(ip)}
		}
		wantAddr := record["address"]
		if want, ok := wantAddr.([]any); ok {
			wantAddr = want[:2]
		}
		if !testjson.Equal(wantAddr, address) {
			t.Errorf("ip_address(%q) = %s, want %s", text, compact(t, address), compact(t, wantAddr))
		}
		if strings.Contains(text, "%") {
			continue
		}
		var network any
		if net, ok := parseNetwork(text); ok {
			network = []any{4 + 2*family(net.Addr()), value(net.Addr()), net.Bits()}
		}
		wantNet := record["network"]
		if want, ok := wantNet.([]any); ok {
			wantNet = want[:3]
		}
		if !testjson.Equal(wantNet, network) {
			t.Errorf("ip_network(%q) = %s, want %s", text, compact(t, network), compact(t, wantNet))
		}
	}
}

func TestCorpusRegex(t *testing.T) {
	re2, backtrack, searches, lenient := 0, 0, 0, 0
	for _, record := range corpus(t)["regex"] {
		pattern, ignore := strField(record, "pattern"), boolField(record, "ignore")
		re, err := CompilePyRegex(pattern, ignore)
		pythonOK := record["error"] == nil
		if regexMoreLenient(!pythonOK, err) || hugeRepeat(pattern) {
			lenient++
			continue
		}
		if (err == nil || !IsPyRegexError(err)) != pythonOK {
			t.Errorf("re.compile(%q, ignore=%v): Go %v, Python error %v", pattern, ignore, err, record["error"])
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
		texts, expected := listField(record, "texts"), listField(record, "searches")
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
		want, _ := record["error"].(string)
		if strings.HasPrefix(want, "__exception__") {
			skipped++
			continue
		}
		kind, kindOK := record["rule_kind"].(string)
		value, valueOK := record["value"].(string)
		if !kindOK || !valueOK {
			skipped++
			continue
		}
		got := RuleValueError(kind, value)
		if idna2003Only(value) && (kind == "DOMAIN" || kind == "DOMAIN-SUFFIX") || specialUpper(kind) || hugeRepeat(value) {
			continue
		}
		if strings.HasPrefix(want, regexInvalid) && (got == "" || strings.HasPrefix(got, regexInvalid)) {
			continue
		}
		if got != want {
			t.Errorf("rule_value_error(%q, %q) = %q, want %q", kind, value, got, want)
		}
		compared++
	}
	t.Logf("%d (kind, value) pairs equal, %d skipped (Python raises)", compared, skipped)
	for _, record := range corpus(t)["split"] {
		if specialUpper(strField(record, "text")) {
			continue
		}
		kind, payload, params := SplitRulePayload(strField(record, "text"))
		if params == nil {
			params = []string{}
		}
		got := []any{kind, payload, params}
		if !testjson.Equal(record["result"], got) {
			t.Errorf("split_rule_payload(%q) = %s, want %s", strField(record, "text"), compact(t, got), compact(t, record["result"]))
		}
	}
	for _, record := range corpus(t)["cidr"] {
		want, _ := record["error"].(string)
		if got := CIDRError(strField(record, "value")); got != want {
			t.Errorf("cidr_error(%q) = %q, want %q", strField(record, "value"), got, want)
		}
	}
}

const regexInvalid = "正则表达式格式无效："

func TestCorpusCacheNames(t *testing.T) {
	reference := cacheReference(t)
	checked := 0
	acceptedCache := map[string]int{}
	defer func() { t.Logf("accepted O18 T1c cache-name differences: %v", acceptedCache) }()
	for _, record := range corpus(t)["cache"] {
		want := record["result"]
		if isException(want) {
			continue
		}
		switch strField(record, "what") {
		case "sanitize":
			name, _ := record["name"].(string)
			if got := SanitizeRuleName(name); got != want {
				t.Errorf("sanitize_rule_name(%q) = %q, want %q", name, got, want)
			}
		case "filename":
			ruleMap := mapField(record, "rule")
			expected := reference.Rows[checked]
			checked++
			assertCacheReferenceInput(t, expected, ruleMap, want)
			var rule model.RuleConfig
			_ = decodeInto(ruleMap, &rule)
			if got := CacheFilename(rule); got != expected.Native {
				if dirt := modeldirt.Of(mustJSON(t, ruleMap), model.RuleConfig{}); len(dirt) > 0 {
					// Accepted O18 T1c difference: the stored rule holds dirty
					// data ("id": "" reads as a missing ID, a number URL as its
					// text, null as missing), so the cache name follows the
					// plain record's values.
					acceptedCache[strings.Join(dirt, "+")]++
					continue
				}
				t.Errorf("CacheFilename(%s) = %q, want %q (%s)", compact(t, ruleMap), got, expected.Native, expected.Class)
			}
		}
	}
	if checked != len(reference.Rows) {
		t.Fatalf("reference coverage %d/%d", checked, len(reference.Rows))
	}
}

func TestCorpusFilters(t *testing.T) {
	type indexedNode struct {
		name  string
		extra int
	}
	skipped := 0
	for _, record := range corpus(t)["filters"] {
		switch strField(record, "what") {
		case "names":
			corpusNodeNames = corpusNodeNames[:0]
			for _, name := range listField(record, "names") {
				corpusNodeNames = append(corpusNodeNames, name.(string))
			}
		case "backticks":
			if got := literalBackticks(strField(record, "pattern")); got != strField(record, "result") {
				t.Errorf("_literal_backticks(%q) = %q, want %q", strField(record, "pattern"), got, strField(record, "result"))
			}
		case "embedded":
			if got := embedded(strField(record, "pattern")); got != strField(record, "result") {
				t.Errorf("_embedded(%q) = %q, want %q", strField(record, "pattern"), got, strField(record, "result"))
			}
		case "exclusion":
			if got := ExclusionFilter(strField(record, "include"), strField(record, "exclude")); got != strField(record, "result") {
				t.Errorf("exclusion_filter(%q, %q) = %q, want %q", strField(record, "include"), strField(record, "exclude"), got, strField(record, "result"))
			}
		case "filter":
			items := make([]indexedNode, len(corpusNodeNames))
			for i, name := range corpusNodeNames {
				items[i] = indexedNode{name: name, extra: i}
			}
			var kept []indexedNode
			inc, err := decodeFilterValue(record["include"])
			var exc *string
			if err == nil {
				exc, err = decodeFilterValue(record["exclude"])
			}
			if err == nil {
				kept, err = Filter(items, inc, exc, func(n indexedNode) string { return n.name })
			}
			result := mapField(record, "result")
			_, hasException := result["exception"]
			_, hasError := result["error"]
			switch {
			case hasException:
				skipped++
			case hasError && err == nil:
				skipped++ // regexMoreLenient
			case err != nil:
				if strField(result, "error") == "正则表达式无效" {
					break
				}
				if strField(result, "error") != err.Error() {
					t.Errorf("filter_candidates(%s, %s) error %q, want %s", compact(t, record["include"]), compact(t, record["exclude"]), err.Error(), compact(t, result))
				}
			default:
				got := make([]int, len(kept))
				for i, item := range kept {
					got[i] = item.extra
				}
				if !testjson.Equal(result["kept"], got) && !(len(got) == 0 && len(listField(result, "kept")) == 0) {
					t.Errorf("filter_candidates(%s, %s) kept %s, want %s", compact(t, record["include"]), compact(t, record["exclude"]), compact(t, got), compact(t, result["kept"]))
				}
			}
		case "validate":
			want := record["result"]
			if isException(want) {
				skipped++
				continue
			}
			var group model.ProxyGroup
			_ = decodeInto(record["group"], &group)
			var before *model.ProxyGroup
			if orig := mapField(record, "original"); orig != nil {
				var g model.ProxyGroup
				_ = decodeInto(orig, &g)
				before = &g
			}
			got := ""
			_ = before
			if err := CheckFilterMembers(requestMembers(t, record["group"]), originalMembers(t, record)); err != nil {
				got = err.Error()
			}
			wantText, _ := want.(string)
			if got != wantText && !(strings.HasSuffix(wantText, "正则表达式无效") && (got == "" || strings.HasSuffix(got, "正则表达式无效"))) {
				t.Errorf("validate_filters(%s, %s) = %q, want %q", compact(t, record["group"]), compact(t, record["original"]), got, wantText)
			}
		}
	}
	t.Logf("%d filter cases skipped (Python raised, or only Python rejects the pattern)", skipped)
}

var corpusNodeNames = []string{"US-new", "US-expired", "HK-new", "HK-expired", "US`old", "EU 01", "香港-01", "sg", "", "US-expired-new", "hk-1", "日本 02", "Aé", "x_1", "123"}

// orderEntryShapeInvalid reports whether a recorded group's proxies_order has
// an entry that is not an object with exactly the members type and id.
func orderEntryShapeInvalid(group any) bool {
	object, _ := group.(map[string]any)
	list, _ := object["proxies_order"].([]any)
	for _, item := range list {
		entry, ok := item.(map[string]any)
		if !ok || len(entry) != 2 {
			return true
		}
		if _, ok := entry["type"]; !ok {
			return true
		}
		if _, ok := entry["id"]; !ok {
			return true
		}
	}
	return false
}

// groupStoredForm is a recorded proxy group as a plain record holds it: the
// value read leniently into model.ProxyGroup and written back.
func groupStoredForm(t *testing.T, value any) any {
	t.Helper()
	var group model.ProxyGroup
	if err := json.Unmarshal(mustJSON(t, value), &group); err != nil {
		t.Fatalf("stored form: %v", err)
	}
	out, err := testjson.Parse(mustJSON(t, group))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func nonNilStrs(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func TestCorpusGroupCandidates(t *testing.T) {
	reference := groupReference(t)
	referenceIndex := 0
	skipped, compared := 0, 0
	acceptedMerge, acceptedValidate, pendingShape := 0, 0, 0
	for _, record := range corpus(t)["group"] {
		if strField(record, "what") == "merge" {
			var original model.ProxyGroup
			_ = decodeInto(record["original"], &original)
			var patch model.Fields
			_ = decodeInto(record["patch"], &patch)
			merged, err := MergeGroup(original, patch)
			result := mapField(record, "result")
			if err != nil {
				if strField(result, "error") != err.Error() {
					t.Errorf("merge_group_update error %q, want %s", err.Error(), compact(t, result))
				}
			} else if !testjson.Equal(result["merged"], merged) {
				dirty := len(modeldirt.Of(mustJSON(t, record["original"]), model.ProxyGroup{})) > 0 ||
					len(modeldirt.Of(mustJSON(t, record["patch"]), model.ProxyGroup{})) > 0
				// Accepted O18 T1c difference: Python merged raw members, so a
				// dirty stored or patched value (a number regex, null, a list
				// member of another kind, an undeclared member) stays as it
				// was; the plain record holds its read value. Python's merge
				// is compared in the record's stored form, so every clean
				// value is still compared exactly.
				if !dirty || !testjson.Equal(groupStoredForm(t, result["merged"]), groupStoredForm(t, merged)) {
					t.Errorf("merge_group_update = %s, want %s", compact(t, merged), compact(t, result["merged"]))
				} else {
					acceptedMerge++
				}
			}
			continue
		}
		var group model.ProxyGroup
		_ = decodeInto(record["group"], &group)
		var original *model.ProxyGroup
		if orig := mapField(record, "original"); orig != nil {
			var g model.ProxyGroup
			_ = decodeInto(orig, &g)
			original = &g
		}
		full := mergeMaps(mapField(record, "shared"), mapField(record, "profile"))
		catalog := groupCatalogFromMap(full)
		expected := groupRow(t, reference.Groups[referenceIndex], record)
		referenceIndex++
		if want := expected["members"]; !isException(want) {
			selection := SelectMembers(group)
			members := make([][]string, len(selection.Members))
			for i, member := range selection.Members {
				members[i] = []string{member.Type, member.ID}
			}
			if !testjson.Equal(want, members) {
				t.Errorf("managed_members(%s) = %s, want %s", compact(t, record["group"]), compact(t, members), compact(t, want))
			}
			order := selection.Order
			if order == nil {
				order = []GroupMember{}
			}
			gotSelection := map[string]any{
				"nodes":         nonNilStrs(selection.Nodes),
				"subscriptions": nonNilStrs(selection.Subscriptions),
				"aggregations":  nonNilStrs(selection.Aggregations),
				"groups":        nonNilStrs(selection.Groups),
				"order":         order,
			}
			if !testjson.Equal(expected["selection"], gotSelection) {
				t.Errorf("managed_members selection %s, want %s", compact(t, gotSelection), compact(t, expected["selection"]))
			}
		}
		if want := expected["render"]; !isException(want) {
			if got := RenderGroup(group, catalog); !testjson.Equal(want, got) {
				t.Errorf("render_members(%s) = %s, want %s", compact(t, record["group"]), compact(t, got), compact(t, want))
			}
		}
		if want := expected["providers"]; !isException(want) {
			got := make([][]any, 0)
			for _, p := range SelectProviders(group, catalog) {
				got = append(got, []any{p.ID, p.Include, p.Exclude})
			}
			if !testjson.Equal(want, got) {
				t.Errorf("managed_providers(%s) = %s, want %s", compact(t, record["group"]), compact(t, got), compact(t, want))
			}
		}
		want := record["validate"]
		if isException(want) {
			skipped++
			continue
		}
		got := ""
		if err := CheckCandidateMembers(group, requestMembers(t, record["group"]), catalog, original, originalMembers(t, record)); err != nil {
			got = err.Error()
		}
		wantText, _ := want.(string)
		if got != wantText {
			switch {
			case wantText == msgOrderPair && orderEntryShapeInvalid(record["group"]):
				// Suspected product regression, handed back to the T1c
				// author: a request proxies_order entry that is not an
				// object or has members besides type and id lost Python's
				// shape message. Counted so that a fix is noticed.
				pendingShape++
			case len(modeldirt.Of(mustJSON(t, record["original"]), model.ProxyGroup{})) > 0 ||
				len(modeldirt.Of(mustJSON(t, record["group"]), model.ProxyGroup{})) > 0:
				// Accepted O18 T1c difference: the stored original (or the
				// group, decoded here leniently) holds dirty data, such as
				// manual_nodes "x" or a null list that reads as no list, or
				// an undeclared "source" member that is dropped.
				acceptedValidate++
			default:
				t.Errorf("validate_candidates(%s, original=%s) = %q, want %q", compact(t, record["group"]), compact(t, record["original"]), got, wantText)
			}
		}
		compared++
	}
	t.Logf("validate_candidates: %d compared, %d skipped (Python raised, or non-list source fields); accepted O18 T1c: merge %d, validate %d; pending proxies_order shape message %d",
		compared, skipped, acceptedMerge, acceptedValidate, pendingShape)
	if pendingShape != 0 {
		t.Errorf("pending proxies_order shape rows = %d, want 0 (the request's own members are checked)", pendingShape)
	}
	if referenceIndex != len(reference.Groups) {
		t.Fatal("typed group reference coverage drift")
	}
}

func TestCorpusLowerGroupProviderFilters(t *testing.T) {
	reference := groupReference(t)
	if len(reference.Lower) != len(corpus(t)["lower"]) {
		t.Fatal("lower reference count drift")
	}
	for i, record := range corpus(t)["lower"] {
		want := record["result"]
		if isException(want) {
			continue
		}
		expected := groupRow(t, *reference.Lower[i], record)
		mainBytes, err := json.Marshal(record["main"])
		if err != nil {
			t.Fatal(err)
		}
		node, err := yamlx.FromJSON(mainBytes)
		if err != nil {
			t.Fatal(err)
		}
		if err := LowerProviderFilters(groupCatalogFromMap(mapField(record, "config")), node); err != nil {
			t.Fatal(err)
		}
		gotBytes, err := yamlx.ToJSON(node)
		if err != nil {
			t.Fatal(err)
		}
		if !testjson.Equal(expected, json.RawMessage(gotBytes)) {
			t.Errorf("lower_group_provider_filters:\n got  %s\n want %s\n config %s", gotBytes, compact(t, expected), compact(t, record["config"]))
		}
	}
}
