package rules

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

// C09 adversarial review: Python reference results for edge inputs
// (testdata/review_c09_gen.py → testdata/review_c09.json.gz, CPython 3.11
// with the production modules).

func loadReviewC09(t *testing.T) map[string]any {
	t.Helper()
	file, err := os.Open("testdata/review_c09.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return mustDecodeMap(t, data)
}

// unsupportedPattern reports a pattern of the domain-level accepted
// difference (\N{name}): Python runs it, Go does not apply the filter.
func unsupportedPattern(value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	_, err := CompilePyRegex(trimFilter(text), false)
	var unsupported *UnsupportedRegexError
	return errors.As(err, &unsupported)
}

func TestReviewC09FilterCandidates(t *testing.T) {
	type indexedItem struct {
		name  string
		extra int
	}
	data := mapField(loadReviewC09(t), "filters")
	names := listField(data, "names")
	items := make([]indexedItem, len(names))
	turkish := map[string]bool{} // turkishI: these names are left out of the comparison
	for i, name := range names {
		text, _ := name.(string)
		items[i] = indexedItem{name: text, extra: i}
		if turkishI(text) {
			turkish[fmt.Sprint(i)] = true
		}
	}
	withoutTurkish := func(list []any) []any {
		out := []any{}
		for _, v := range list {
			if !turkish[fmt.Sprint(v)] {
				out = append(out, v)
			}
		}
		return out
	}
	compared := 0
	for _, entry := range listField(data, "cases") {
		record := entry.(map[string]any)
		include, exclude := record["include"], record["exclude"]
		want := mapField(record, "result")
		if _, has := want["exception"]; has {
			t.Errorf("Python raised for %s/%s: %s", compact(t, include), compact(t, exclude), compact(t, want))
			continue
		}
		if turkishI(compact(t, include) + compact(t, exclude)) {
			continue
		}
		var kept []indexedItem
		inc, err := decodeFilterValue(include)
		var exc *string
		if err == nil {
			exc, err = decodeFilterValue(exclude)
		}
		if err == nil {
			kept, err = Filter(items, inc, exc, func(it indexedItem) string { return it.name })
		}
		if unsupportedPattern(include) || unsupportedPattern(exclude) {
			if err != nil {
				t.Errorf("filter_candidates(%s, %s): unsupported pattern refused: %v", compact(t, include), compact(t, exclude), err)
			}
			continue
		}
		compared++
		if _, hasErr := want["error"]; hasErr {
			if regexMoreLenient(true, err) {
				continue
			}
			if err == nil || err.Error() != strField(want, "error") {
				t.Errorf("filter_candidates(%s, %s) error %v, want %q", compact(t, include), compact(t, exclude), err, strField(want, "error"))
			}
			continue
		}
		if err != nil {
			t.Errorf("filter_candidates(%s, %s) error %v, want %s", compact(t, include), compact(t, exclude), err, compact(t, want))
			continue
		}
		got := make([]any, len(kept))
		for i, item := range kept {
			got[i] = item.extra
		}
		if !testjson.Equal(withoutTurkish(listField(want, "kept")), withoutTurkish(got)) {
			t.Errorf("filter_candidates(%s, %s) kept %s, want %s", compact(t, include), compact(t, exclude), compact(t, got), compact(t, want["kept"]))
		}
	}
	if compared < 200 {
		t.Fatalf("only %d cases compared", compared)
	}
}

func TestReviewC09ValidateFilters(t *testing.T) {
	for _, entry := range listField(loadReviewC09(t), "validate") {
		record := entry.(map[string]any)
		want := record["result"]
		if object, ok := want.(map[string]any); ok {
			t.Errorf("Python raised for %s: %s", compact(t, record["group"]), compact(t, object))
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
		if wantText, _ := want.(string); got == "" && strings.HasSuffix(wantText, "正则表达式无效") {
			continue // regexMoreLenient
		}
		if got != want {
			t.Errorf("validate_filters(%s, %s) = %q, want %q", compact(t, record["group"]), compact(t, record["original"]), got, want)
		}
	}
}

func TestReviewC09LowerGroupProviderFilters(t *testing.T) {
	reference := groupReference(t)
	rows := listField(loadReviewC09(t), "lower")
	if len(rows) != len(reference.ReviewLower) {
		t.Fatal("review lower count drift")
	}
	for i, entry := range rows {
		record := entry.(map[string]any)
		expected := groupRow(t, reference.ReviewLower[i], record)
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
			t.Errorf("lower_group_provider_filters:\n got  %s\n want %s", gotBytes, compact(t, expected))
		}
	}
}

func TestReviewC09ResolveNewOrder(t *testing.T) {
	for _, entry := range listField(loadReviewC09(t), "reorder") {
		record := entry.(map[string]any)
		items := append([]any(nil), listField(record, "items")...)
		bodyRaw, err := json.Marshal(record["body"])
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(bodyRaw, &body); err != nil {
			t.Fatal(err)
		}
		ordered, missing := resolveOrder(items, body, "rules")
		if missing == nil {
			missing = []string{}
		}
		got := map[string]any{"ordered": ordered, "missing": missing}
		if !testjson.Equal(record["result"], got) {
			t.Errorf("resolve_new_order(%s) = %s, want %s", compact(t, record["body"]), compact(t, got), compact(t, record["result"]))
		}
	}
}

func TestReviewC09ValidateRulePolicies(t *testing.T) {
	reference := cacheReference(t)
	rows := listField(loadReviewC09(t), "policies")
	if len(rows) != len(reference.Policies) {
		t.Fatal("policy reference row count drift")
	}
	for i, entry := range rows {
		record := entry.(map[string]any)
		expected := reference.Policies[i]
		if !testjson.Equal(expected.Input, record) {
			t.Fatalf("policy reference row %d drift", i)
		}
		if expected.Class != "R5" && expected.Class != "unchanged" {
			t.Fatalf("unknown policy class %q", expected.Class)
		}
		if expected.Class == "unchanged" && record["result"] != expected.Native {
			t.Fatal("unchanged policy expected drift")
		}
		var names []string
		for _, name := range listField(record, "names") {
			names = append(names, name.(string))
		}
		var configs []model.RuleConfig
		for _, rc := range listField(mapField(record, "config"), "rule_configs") {
			var rule model.RuleConfig
			if decodeInto(rc, &rule) == nil {
				configs = append(configs, rule)
			}
		}
		got := ""
		if err := ValidatePolicies(configs, names, strField(record, "format")); err != nil {
			got = err.Error()
		}
		want := expected.Native
		if expected.Class == "R5" && strings.Contains(strField(record, "result"), "'None'") {
			// An explicit null policy is still refused; the plain rule record
			// holds it as an empty reference, shown as '' (O18 T1c).
			want = strings.ReplaceAll(strField(record, "result"), "'None'", "''")
		}
		// A non-text stored policy is refused as an empty reference (O18 T1c).
		if got != want && got == regexp.MustCompile(`'[^']*'，请修改策略引用$`).ReplaceAllString(want, "''，请修改策略引用") {
			continue
		}
		if got != want {
			t.Errorf("policy reference row %d (%s): got %q want %q", i, expected.Class, got, want)
		}
	}
}

func TestReviewC09DestinationNetwork(t *testing.T) {
	for _, entry := range listField(loadReviewC09(t), "destination") {
		record := entry.(map[string]any)
		want := mapField(record, "result")
		got := map[string]any{}
		value, isText := record["value"].(string)
		if !isText { // the route passes strings only
			continue
		}
		if destination, err := ParseDestination(value, strField(record, "rule_type")); err != nil {
			got["exception"] = "ProfileValidationError"
			got["message"] = err.Error()
		} else {
			got["network"] = destination.String()
		}
		if !testjson.Equal(want, got) {
			t.Errorf("_destination_network(%s, %s) = %s, want %s", compact(t, record["value"]), strField(record, "rule_type"), compact(t, got), compact(t, want))
		}
	}
}

// Regression (review C09): a pattern Python runs but this engine cannot is
// not applied (domain-level accepted difference) instead of being refused.
func TestFilterCandidatesUnsupportedPatternIsNotApplied(t *testing.T) {
	items := []string{"a", "b"}
	for _, pair := range [][2]*string{{strp(`\N{LATIN SMALL LETTER A}`), nil}, {nil, strp(`\N{LATIN SMALL LETTER A}`)}} {
		kept, err := Filter(items, pair[0], pair[1], func(s string) string { return s })
		if err != nil || len(kept) != 2 {
			t.Fatalf("Filter(%v, %v) = %v, %v", pair[0], pair[1], kept, err)
		}
	}
}
