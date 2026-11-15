package rules

import (
	"compress/gzip"
	"configflow/internal/pystr"
	"errors"
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
	for i, name := range data.GetList("names") {
		items = append(items, jsonx.Obj("name", name, "extra", jsonx.Int(int64(i))))
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
		if compact(t, got) != compact(t, want.Value("kept")) {
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
		if got != want {
			t.Errorf("validate_filters(%s, %s) = %q, want %q", compact(t, group), compact(t, record.Value("original")), got, want)
		}
	}
}

func TestReviewC09LowerGroupProviderFilters(t *testing.T) {
	for _, entry := range loadReviewC09(t).GetList("lower") {
		record := entry.(*jsonx.Object)
		main := record.GetObject("main").DeepCopy()
		LowerGroupProviderFilters(record.GetObject("config").DeepCopy(), main)
		if compact(t, main) != compact(t, record.Value("result")) {
			t.Errorf("lower_group_provider_filters:\n got  %s\n want %s", compact(t, main), compact(t, record.Value("result")))
		}
	}
}

func TestReviewC09ResolveNewOrder(t *testing.T) {
	for _, entry := range loadReviewC09(t).GetList("reorder") {
		record := entry.(*jsonx.Object)
		items := jsonx.DeepCopy(record.Value("items")).([]any)
		body := record.GetObject("body").DeepCopy()
		ordered, missing := ResolveNewOrder(items, body, "rules")
		if missing == nil {
			missing = []any{}
		}
		got := jsonx.Obj("ordered", ordered, "missing", missing)
		if compact(t, got) != compact(t, record.Value("result")) {
			t.Errorf("resolve_new_order(%s) = %s, want %s", compact(t, body), compact(t, got), compact(t, record.Value("result")))
		}
	}
}

func TestReviewC09ValidateRulePolicies(t *testing.T) {
	for _, entry := range loadReviewC09(t).GetList("policies") {
		record := entry.(*jsonx.Object)
		var names []string
		for _, name := range record.GetList("names") {
			names = append(names, name.(string))
		}
		want := record.Value("result")
		if object, ok := want.(*jsonx.Object); ok {
			// Python TypeError (unhashable policy) → handled error in Go.
			if !strings.Contains(object.GetString("message"), "unhashable") {
				t.Errorf("unexpected Python exception %s", compact(t, object))
			}
			continue
		}
		got := ""
		if err := ValidateRulePolicies(record.GetObject("config"), names, record.GetString("format")); err != nil {
			got = err.Error()
		}
		if got != want {
			t.Errorf("validate_rule_policies(%s, %s) = %q, want %q", compact(t, record.Value("config")), record.GetString("format"), got, want)
		}
	}
}

func TestReviewC09DestinationNetwork(t *testing.T) {
	for _, entry := range loadReviewC09(t).GetList("destination") {
		record := entry.(*jsonx.Object)
		want := record.GetObject("result")
		got := jsonx.NewObject()
		if destination, err := ParseDestination(record.Value("value"), record.GetString("rule_type")); err != nil {
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

// Regression (review C09): Python's subnet_of compares network addresses
// with IPv6Address.__eq__, which includes the scope id.
func TestSubnetOfHonoursScopeIDLikePython(t *testing.T) {
	parse := func(text string) ipNet {
		network, ok := parseIPNetwork(text)
		if !ok {
			t.Fatalf("parse %q", text)
		}
		return network
	}
	cases := []struct {
		inner, outer string
		want         bool
	}{
		{"fe80::/64", "fe80::%eth0/64", false},
		{"fe80::1/128", "fe80::%eth0/64", true},
		{"fe80::/64", "fe80::1%eth0/64", true}, // host bits cleared: the scope is dropped
		{"fe80::/64", "fe80::/64", true},
		{"fe80::%eth0/64", "fe80::%eth0/64", true},
		{"192.0.2.0/24", "192.0.2.0/24", true},
		{"192.0.2.0/23", "192.0.2.0/24", false},
	}
	for _, c := range cases {
		if got := parse(c.inner).subnetOf(parse(c.outer)); got != c.want {
			t.Errorf("%s subnet_of %s = %v, want %v", c.inner, c.outer, got, c.want)
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
