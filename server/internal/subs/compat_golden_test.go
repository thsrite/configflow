package subs

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

// TestCompatAgainstRepoGolden checks the private dialer_references copy of
// compat.go (ValidateDialers, the structured proxy-string parser) against the
// Python goldens package repo generated for its own copy
// (server/internal/repo/testdata/compat_golden.json).
func TestCompatAgainstRepoGolden(t *testing.T) {
	data, err := os.ReadFile("../repo/testdata/compat_golden.json")
	if err != nil {
		t.Skip("repo golden not present:", err)
	}
	parsed, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	stats := map[string]int{}
	oracle := nativeReaderCases(t)
	native := nativeChainExpectations(t)
	for i, item := range parsed.(*jsonx.Object).GetList("cases") {
		record := item.(*jsonx.Object)
		if record.GetString("fn") == "parse_proxy_string" || record.GetString("fn") == "validate_dialers" {
			if expected, changed := changedNativeCase(t, oracle, fmt.Sprintf("subs/golden/%d", i)); changed {
				record = record.Clone()
				record.Set("ok", expected.Value("value"))
				record.Set("raised", expected.Has("error"))
				record.Set("error_class", expected.GetString("error"))
				record.Set("error", expected.GetString("message"))
			}
		}
		if entry := native.GetObject(fmt.Sprintf("golden/%d", i)); entry != nil {
			record = record.Clone()
			expected := entry.GetObject("expected")
			if expected.Has("error") {
				record.Set("raised", true)
				record.Set("error_class", expected.Value("class"))
				record.Set("error", expected.Value("error"))
			} else {
				record.Set("raised", false)
				record.Set("ok", expected.Value("value"))
			}
		}
		input := record.Value("input")
		switch record.GetString("fn") {
		case "validate_dialers":
			profile, _ := input.(*jsonx.Object)
			err := ValidateDialers(profile.DeepCopy())
			if !record.GetBool("raised") {
				if err != nil {
					t.Errorf("case %d: unexpected error %v for %s", i, err, compact(t, input))
				}
				stats["ok"]++
				continue
			}
			var de *DialerReferenceError
			switch {
			case record.GetString("error_class") == "DialerReferenceError":
				if !errors.As(err, &de) || de.Message != record.GetString("error") {
					t.Errorf("case %d: got %v, python %s for %s", i, err, record.GetString("error"), compact(t, input))
				}
				stats["dialer_error"]++
			case err == nil:
				// Python raised TypeError/AttributeError/... on a malformed profile
				stats["python_crash_go_ok:"+record.GetString("error_class")]++
				t.Logf("case %d: python %s (%s), Go accepts %s", i, record.GetString("error_class"), record.GetString("error"), compact(t, input))
			default:
				stats["python_crash_go_error"]++
			}
		case "parse_proxy_string":
			got := parseStructuredProxyString(input.(string))
			var g any
			if got != nil {
				g = got
			}
			if record.GetBool("raised") {
				t.Errorf("case %d: python raised", i)
				continue
			}
			if _, lerr := readSubscriptionYAML(strings.TrimSpace(input.(string)), false); got == nil && record.Value("ok") != nil && errors.Is(lerr, yamlx.ErrPolicy) && strings.Contains(lerr.Error(), "JSON object key must be a string") {
				stats["proxy_string_non_string_keys"]++
				continue
			}
			if !yamlxtest.EqualJSON(g, record.Value("ok")) {
				stats["proxy_string_mismatch"]++
				t.Errorf("case %d parse(%q): got %s python %s", i, input, compact(t, g), compact(t, record.Value("ok")))
				continue
			}
			stats["proxy_string_ok"]++
		}
	}
	t.Logf("%v", stats)
}
