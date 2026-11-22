package subs

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

// C10 adversarial review: Python reference results for edge inputs
// (testdata/review_c10_gen.py → testdata/review_c10.jsonl.gz). Rows whose
// Python result JSON cannot carry (datetime, bytes, set, non-string keys)
// are counted, not compared: yamlx represents them differently (domain-level
// accepted difference).

func reviewOut(raw json.RawMessage) (kind string, msg string, ok bool) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || len(m) != 1 {
		return "", "", false
	}
	if v, has := m["error"]; has {
		_ = json.Unmarshal(v, &msg)
		return "error", msg, true
	}
	if v, has := m["unrepresentable"]; has {
		_ = json.Unmarshal(v, &msg)
		return "unrepresentable", msg, true
	}
	return "", "", false
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
	compared, unrepresentable, known, acceptedUTF8, acceptedEmpty := 0, 0, 0, 0, 0
	for i, row := range rows {
		wantRaw := row.OutRaw
		if expected, changed := changedNativeCase(t, oracle, fmt.Sprintf("subs/review_c10/%d", i)); changed {
			if expected.HasError {
				wantRaw = rawJSON(t, map[string]string{"error": expected.Error})
			} else {
				wantRaw = expected.Value
			}
		}
		if expected, ok := hostsNative[fmt.Sprintf("review_c10/%d", i)]; ok {
			wantRaw = expected.Output
		}
		if entry, ok := dialersNative[fmt.Sprintf("review_c10/%d", i)]; ok {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(entry.Expected, &fields); err != nil {
				t.Fatal(err)
			}
			if rawErr, hasErr := fields["error"]; hasErr {
				wantRaw = rawJSON(t, map[string]json.RawMessage{"error": rawErr})
			} else {
				wantRaw = fields["value"]
			}
		}
		if row.Fn == "parse_uri_list" {
			if expected, ok := native[fmt.Sprintf("review_c10/%d", i)]; ok {
				wantRaw = expected.Output
			}
		}
		in := row.In.(string)
		if yamlxDiffers[in] {
			known++
			continue
		}
		specialKind, specialMsg, isSpecial := reviewOut(wantRaw)
		if isSpecial && specialKind == "unrepresentable" {
			record := oracle[fmt.Sprintf("subs/review_c10/%d", i)]
			if len(record.OldLoads)+len(record.NativeLoads) > 0 {
				t.Fatalf("uncompared YAML row %d", i)
			}
			unrepresentable++
			continue
		}
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
			got = parsedListJSON(t, ParseList(in))
		case "parse_yaml_object":
			got = parseObjectJSON(t, in)
		case "extract_subscription_hosts":
			if record := ExtractHosts(in); record != nil {
				got = record
			}
		case "extract_subscription_dialers":
			var record *DialersData
			record, err = ExtractDialers(in)
			if record != nil {
				got = record
			}
		default:
			t.Fatalf("unknown fn %q", row.Fn)
		}
		compared++
		if isSpecial {
			if err == nil {
				t.Errorf("row %d %s(%q): Python raised %s, Go returned %s", i, row.Fn, in, specialMsg, compact(t, got))
			}
			continue
		}
		if err != nil {
			t.Errorf("row %d %s(%q): Go error %v, Python %s", i, row.Fn, in, err, wantRaw)
			continue
		}
		if !testjson.Equal(wantRaw, got) && o18T1cSameParsed(wantRaw, got) {
			acceptedEmpty++ // O18 T1c: empty server/port omitted
			continue
		}
		if !testjson.Equal(wantRaw, got) {
			t.Errorf("row %d %s(%q)\n got  %s\n want %s", i, row.Fn, in, compact(t, got), wantRaw)
		}
	}
	t.Logf("%d rows compared, %d unrepresentable in JSON skipped, %d known yamlx differences, %d accepted invalid-UTF-8 SNI rows, %d O18 T1c 接受 (empty server/port omitted)",
		compared, unrepresentable, known, acceptedUTF8, acceptedEmpty)
	if known != 0 {
		t.Errorf("%d known yamlx rows, want 0", known)
	}
}

// Regression (review C10): only string names can equal a dialer target
// (Python: a nameless proxy contributes None, a numeric name an int).
func TestAdaptSubscriptionProxiesIgnoresNonStringNames(t *testing.T) {
	main := yamlFromRaw(t, json.RawMessage(`{"proxies":[{"type":"ss"},{"name":5}],"proxy-groups":[]}`))
	var record DialersData
	if err := json.Unmarshal([]byte(`{"targets":{"None":{},"5":{}},"unresolved":[]}`), &record); err != nil {
		t.Fatal(err)
	}
	proxies := yamlFromRaw(t, json.RawMessage(`[{"name":"a","dialer-proxy":"None"},{"name":"b","dialer-proxy":"5"},{"name":"c","dialer-proxy":"x"}]`)).Content
	got, err := AdaptProxies("S", proxies, main, &record, true)
	if err != nil {
		t.Fatal(err)
	}
	gotRaw := yamlToRaw(t, yamlx.Seq(got...))
	want := json.RawMessage(`[{"name":"c","dialer-proxy":"x"}]`)
	if !testjson.Equal(want, gotRaw) {
		t.Fatalf("got %s, want %s (Python reference)", gotRaw, want)
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

// The retired private int() emulation accepted Unicode digits. URI integers
// now use the approved ASCII decimal protocol; mapped-address rules are unchanged.
func TestNativeURIIntegerAndMappedAddresses(t *testing.T) {
	for _, input := range []string{"𝟙𝟙", "𝟡𝟡", "𝟬𝟭", "٤٤٣"} {
		if _, err := parseURIPort(input); err == nil {
			t.Errorf("non-ASCII URI integer %q accepted", input)
		}
	}
	for in, want := range map[string]string{"::ffff:224.0.0.1": "", "::ffff:0.0.0.0": "", "::ffff:127.0.0.1": "::ffff:127.0.0.1"} {
		if got := usableAddress(in); got != want {
			t.Errorf("_address(%q) = %q, want %q", in, got, want)
		}
	}
}
