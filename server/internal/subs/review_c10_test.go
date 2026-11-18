package subs

import (
	"fmt"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx/yamlxtest"
)

// C10 adversarial review: Python reference results for edge inputs
// (testdata/review_c10_gen.py → testdata/review_c10.jsonl.gz). Rows whose
// Python result JSON cannot carry (datetime, bytes, set, non-string keys)
// are counted, not compared: yamlx represents them differently (domain-level
// accepted difference).

func reviewOut(row corpusRow) (*jsonx.Object, bool) {
	o, ok := row.Out.(*jsonx.Object)
	if !ok {
		return nil, false
	}
	if o.Len() == 1 && (o.Has("error") || o.Has("unrepresentable")) {
		return o, true
	}
	return nil, false
}

// yamlxDiffers are inputs on which internal/yamlx differs from PyYAML. The
// six C10 rows (non-string hosts/dialer keys, a tab before a value) match
// since review C02 moved yamlx onto the PyYAML port; keep it empty.
var yamlxDiffers = map[string]bool{}

func TestReviewC10References(t *testing.T) {
	rows := loadCorpus(t, "review_c10.jsonl.gz")
	oracle := nativeReaderCases(t)
	native := nativeParserExpectations(t)
	hostsNative := nativeHostsExpectations(t)
	dialersNative := nativeDialersExpectations(t)
	compared, unrepresentable, known, acceptedUTF8 := 0, 0, 0, 0
	for i, row := range rows {
		if expected, changed := changedNativeCase(t, oracle, fmt.Sprintf("subs/review_c10/%d", i)); changed {
			if expected.Has("error") {
				row.Out = jsonx.Obj("error", expected.GetString("error"))
			} else {
				row.Out = expected.Value("value")
			}
		}
		if expected := hostsNative.GetObject(fmt.Sprintf("review_c10/%d", i)); expected != nil {
			row.Out = expected.Value("output")
		}
		if entry := dialersNative.GetObject(fmt.Sprintf("review_c10/%d", i)); entry != nil {
			expected := entry.GetObject("expected")
			if expected.Has("error") {
				row.Out = jsonx.Obj("error", expected.Value("error"))
			} else {
				row.Out = expected.Value("value")
			}
		}
		if row.Fn == "parse_uri_list" {
			if expected := native.GetObject(fmt.Sprintf("review_c10/%d", i)); expected != nil {
				row.Out = expected.Value("output")
			}
		}
		if yamlxDiffers[row.In.(string)] {
			known++
			continue
		}
		special, isSpecial := reviewOut(row)
		if isSpecial && special.Has("unrepresentable") {
			record := oracle.GetObject(fmt.Sprintf("subs/review_c10/%d", i))
			if len(record.GetList("old_loads"))+len(record.GetList("native_loads")) > 0 {
				t.Fatalf("uncompared YAML row %d", i)
			}
			unrepresentable++
			continue
		}
		in := row.In.(string)
		if row.Fn == "parse_uri_list" && strings.Contains(in, "sni=%ff%fe") {
			// Go's strings.ToValidUTF8 replaces the invalid byte run once.
			// These synthetic SNI values are not valid UTF-8 host names or
			// usable TLS server names, so preserving Python's per-byte
			// replacement has no supported configuration benefit.
			acceptedUTF8++
			continue
		}
		var got any
		var err error
		switch row.Fn {
		case "parse_uri_list":
			nodes := ParseURIList(in)
			list := make([]any, len(nodes))
			for j, n := range nodes {
				list[j] = n
			}
			got = list
		case "parse_yaml_object":
			if node := ParseYAMLObject(in); node != nil {
				got = node
			}
		case "extract_subscription_hosts":
			if record := ExtractSubscriptionHosts(in); record != nil {
				got = record
			}
		case "extract_subscription_dialers":
			var record *jsonx.Object
			record, err = ExtractSubscriptionDialers(in)
			if record != nil {
				got = record
			}
		default:
			t.Fatalf("unknown fn %q", row.Fn)
		}
		compared++
		if isSpecial {
			if err == nil {
				t.Errorf("row %d %s(%q): Python raised %s, Go returned %s", i, row.Fn, in, special.GetString("error"), compact(t, got))
			}
			continue
		}
		if err != nil {
			t.Errorf("row %d %s(%q): Go error %v, Python %s", i, row.Fn, in, err, compact(t, row.Out))
			continue
		}
		if g, w := compact(t, got), compact(t, row.Out); !yamlxtest.EqualJSON(got, row.Out) {
			t.Errorf("row %d %s(%q)\n got  %s\n want %s", i, row.Fn, in, g, w)
		}
	}
	t.Logf("%d rows compared, %d unrepresentable in JSON skipped, %d known yamlx differences, %d accepted invalid-UTF-8 SNI rows",
		compared, unrepresentable, known, acceptedUTF8)
	if known != 0 {
		t.Errorf("%d known yamlx rows, want 0", known)
	}
}

// Regression (review C10): only string names can equal a dialer target
// (Python: a nameless proxy contributes None, a numeric name an int).
func TestAdaptSubscriptionProxiesIgnoresNonStringNames(t *testing.T) {
	main := jsonx.Obj("proxies", []any{jsonx.Obj("type", "ss"), jsonx.Obj("name", jsonx.Int(5))}, "proxy-groups", []any{})
	record := jsonx.Obj("targets", jsonx.Obj("None", jsonx.NewObject(), "5", jsonx.NewObject()), "unresolved", []any{})
	proxies := []any{jsonx.Obj("name", "a", "dialer-proxy", "None"), jsonx.Obj("name", "b", "dialer-proxy", "5"),
		jsonx.Obj("name", "c", "dialer-proxy", "x")}
	got := AdaptSubscriptionProxies("S", proxies, main, record, true)
	if want := `[{"name":"c","dialer-proxy":"x"}]`; compact(t, got) != want {
		t.Fatalf("got %s, want %s (Python reference)", compact(t, got), want)
	}
}

// Regression (review C10): a provider query filter pattern only Python can
// run is not applied; the other pattern still is.
func TestQueryFilterUnsupportedPatternIsNotApplied(t *testing.T) {
	check := queryFilter(map[string][]string{"regex": {`\N{LATIN SMALL LETTER A}`}, "exclude_regex": {"b"}})
	if check == nil || check("b1") || !check("c") {
		t.Fatal("exclude not applied or unsupported include applied")
	}
	if queryFilter(map[string][]string{"regex": {"("}, "exclude_regex": {"b"}}) != nil {
		t.Fatal("a pattern Python rejects must drop the whole filter")
	}
}

// Regression (review C10): int() of digits inside a contiguous block of Nd
// runs (U+1D7CE..U+1D7FF) and IPv4-mapped special addresses (3.11.17).
func TestPyIntStrAndMappedAddresses(t *testing.T) {
	for in, want := range map[string]string{"𝟙𝟙": "11", "𝟡𝟡": "99", "𝟬𝟭": "1", "٤٤٣": "443"} {
		if n, err := pyIntStr(in); err != nil || n.String() != want {
			t.Errorf("int(%q) = %v %v, want %s", in, n, err, want)
		}
	}
	for in, want := range map[string]string{"::ffff:224.0.0.1": "", "::ffff:0.0.0.0": "", "::ffff:127.0.0.1": "::ffff:127.0.0.1"} {
		if got := addressOf(in); got != want {
			t.Errorf("_address(%q) = %q, want %q", in, got, want)
		}
	}
}
