package rules

import (
	"compress/gzip"
	"configflow/internal/pystr"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// C09 adversarial review: Python reference results for edge inputs
// (testdata/review_c09_gen.py → testdata/review_c09.json.gz, CPython 3.11
// with the production modules).

func loadReviewC09(t *testing.T) *jsonx.Object {
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
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return value.(*jsonx.Object)
}

// unsupportedPattern reports a pattern of the domain-level accepted
// difference (\N{name}): Python runs it, Go does not apply the filter.
func unsupportedPattern(value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	_, err := CompilePyRegex(pystr.Strip(text), false)
	var unsupported *UnsupportedRegexError
	return errors.As(err, &unsupported)
}

func TestReviewC09FilterCandidates(t *testing.T) {
	data := loadReviewC09(t).GetObject("filters")
	var items []any
	turkish := map[string]bool{} // turkishI: these names are left out of the comparison
	for i, name := range data.GetList("names") {
		items = append(items, jsonx.Obj("name", name, "extra", jsonx.Int(int64(i))))
		if text, _ := name.(string); turkishI(text) {
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
	for _, entry := range data.GetList("cases") {
		record := entry.(*jsonx.Object)
		include, exclude := record.Value("include"), record.Value("exclude")
		want := record.GetObject("result")
		if want.Has("exception") {
			t.Errorf("Python raised for %s/%s: %s", compact(t, include), compact(t, exclude), compact(t, want))
			continue
		}
		if turkishI(compact(t, include) + compact(t, exclude)) {
			continue
		}
		kept, err := FilterCandidates(items, include, exclude)
		if unsupportedPattern(include) || unsupportedPattern(exclude) {
			// Accepted difference: the unsupported filter is not applied and the
			// request is accepted (Python applies it).
			if err != nil {
				t.Errorf("filter_candidates(%s, %s): unsupported pattern refused: %v", compact(t, include), compact(t, exclude), err)
			}
			continue
		}
		compared++
		if want.Has("error") {
			if regexMoreLenient(true, err) {
				continue
			}
			if err == nil || err.Error() != want.GetString("error") {
				t.Errorf("filter_candidates(%s, %s) error %v, want %q", compact(t, include), compact(t, exclude), err, want.GetString("error"))
			}
			continue
		}
		if err != nil {
			t.Errorf("filter_candidates(%s, %s) error %v, want %s", compact(t, include), compact(t, exclude), err, compact(t, want))
			continue
		}
		got := []any{}
		for _, item := range kept {
			got = append(got, item.(*jsonx.Object).Value("extra"))
		}
		if compact(t, withoutTurkish(got)) != compact(t, withoutTurkish(want.GetList("kept"))) {
			t.Errorf("filter_candidates(%s, %s) kept %s, want %s", compact(t, include), compact(t, exclude), compact(t, got), compact(t, want.Value("kept")))
		}
	}
	if compared < 200 {
		t.Fatalf("only %d cases compared", compared)
	}
}

func TestReviewC09ValidateFilters(t *testing.T) {
	for _, entry := range loadReviewC09(t).GetList("validate") {
		record := entry.(*jsonx.Object)
		group := record.GetObject("group")
		original, _ := record.Value("original").(*jsonx.Object)
		want := record.Value("result")
		if object, ok := want.(*jsonx.Object); ok {
			t.Errorf("Python raised for %s: %s", compact(t, group), compact(t, object))
			continue
		}
		got := ""
		if err := ValidateFilters(group, original); err != nil {
			got = err.Error()
		}
		if wantText, _ := want.(string); got == "" && strings.HasSuffix(wantText, "正则表达式无效") {
			continue // regexMoreLenient
		}
		if got != want {
			t.Errorf("validate_filters(%s, %s) = %q, want %q", compact(t, group), compact(t, record.Value("original")), got, want)
		}
	}
}

func TestReviewC09LowerGroupProviderFilters(t *testing.T) {
	reference := groupReference(t)
	rows := loadReviewC09(t).GetList("lower")
	if len(rows) != len(reference.ReviewLower) {
		t.Fatal("review lower count drift")
	}
	for i, entry := range rows {
		record := entry.(*jsonx.Object)
		expected := groupRow(t, reference.ReviewLower[i], record)
		main := record.GetObject("main").DeepCopy()
		LowerGroupProviderFilters(record.GetObject("config").DeepCopy(), main)
		if compact(t, main) != compact(t, expected) {
			t.Errorf("lower_group_provider_filters:\n got  %s\n want %s", compact(t, main), compact(t, expected))
		}
	}
}

func TestReviewC09ResolveNewOrder(t *testing.T) {
	for _, entry := range loadReviewC09(t).GetList("reorder") {
		record := entry.(*jsonx.Object)
		items := jsonx.DeepCopy(record.Value("items")).([]any)
		body := record.GetObject("body").DeepCopy()
		ordered, missing := ResolveNewOrder(items, body, "rules")
		got := jsonx.Obj("ordered", ordered, "missing", jsonx.ListOf(missing))
		if compact(t, got) != compact(t, record.Value("result")) {
			t.Errorf("resolve_new_order(%s) = %s, want %s", compact(t, body), compact(t, got), compact(t, record.Value("result")))
		}
	}
}

func TestReviewC09ValidateRulePolicies(t *testing.T) {
	reference := cacheReference(t)
	rows := loadReviewC09(t).GetList("policies")
	if len(rows) != len(reference.Policies) {
		t.Fatal("policy reference row count drift")
	}
	for i, entry := range rows {
		record := entry.(*jsonx.Object)
		expected := reference.Policies[i]
		input, err := jsonx.Parse(expected.Input)
		if err != nil {
			t.Fatal(err)
		}
		if !jsonx.Equal(input, record) {
			t.Fatalf("policy reference row %d drift", i)
		}
		if expected.Class != "R5" && expected.Class != "unchanged" {
			t.Fatalf("unknown policy class %q", expected.Class)
		}
		if expected.Class == "unchanged" && record.Value("result") != expected.Native {
			t.Fatal("unchanged policy expected drift")
		}
		var names []string
		for _, name := range record.GetList("names") {
			names = append(names, name.(string))
		}
		got := ""
		if err := ValidateRulePolicies(record.GetObject("config"), names, record.GetString("format")); err != nil {
			got = err.Error()
		}
		if got != expected.Native {
			t.Errorf("policy reference row %d (%s): got %q want %q", i, expected.Class, got, expected.Native)
		}
	}
}

func TestReviewC09DestinationNetwork(t *testing.T) {
	for _, entry := range loadReviewC09(t).GetList("destination") {
		record := entry.(*jsonx.Object)
		want := record.GetObject("result")
		got := jsonx.NewObject()
		value, isText := record.Value("value").(string)
		if !isText { // the route passes strings only
			continue
		}
		if destination, err := ParseDestination(value, record.GetString("rule_type")); err != nil {
			got.Set("exception", "ProfileValidationError")
			got.Set("message", err.Error())
		} else {
			got.Set("network", destination.String())
		}
		if compact(t, got) != compact(t, want) {
			t.Errorf("_destination_network(%s, %s) = %s, want %s", compact(t, record.Value("value")), record.GetString("rule_type"), compact(t, got), compact(t, want))
		}
	}
}

// Regression (review C09): a pattern Python runs but this engine cannot is
// not applied (domain-level accepted difference) instead of being refused.
func TestFilterCandidatesUnsupportedPatternIsNotApplied(t *testing.T) {
	items := []any{jsonx.Obj("name", "a"), jsonx.Obj("name", "b")}
	for _, pair := range [][2]any{{`\N{LATIN SMALL LETTER A}`, nil}, {nil, `\N{LATIN SMALL LETTER A}`}} {
		kept, err := FilterCandidates(items, pair[0], pair[1])
		if err != nil || len(kept) != 2 {
			t.Fatalf("FilterCandidates(%v, %v) = %v, %v", pair[0], pair[1], kept, err)
		}
	}
}
