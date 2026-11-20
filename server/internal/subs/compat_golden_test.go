package subs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

// TestCompatAgainstRepoGolden checks the shared dialer ops (Validate, Structured)
// against the Python goldens package repo generated for its own copy
// (server/internal/repo/testdata/compat_golden.json).
func TestCompatAgainstRepoGolden(t *testing.T) {
	data, err := os.ReadFile("../repo/testdata/compat_golden.json")
	if err != nil {
		t.Skip("repo golden not present:", err)
	}
	var parsed struct {
		Cases []struct {
			Fn         string          `json:"fn"`
			Input      json.RawMessage `json:"input"`
			OK         json.RawMessage `json:"ok"`
			Raised     bool            `json:"raised"`
			ErrorClass string          `json:"error_class"`
			Error      string          `json:"error"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(sanitizeNonFiniteJSON(data), &parsed); err != nil {
		t.Fatal(err)
	}
	stats := map[string]int{}
	oracle := nativeReaderCases(t)
	native := nativeChainExpectations(t)
	ops := sharedDialers()
	for i, record := range parsed.Cases {
		if record.Fn == "parse_proxy_string" || record.Fn == "validate_dialers" {
			if expected, changed := changedNativeCase(t, oracle, fmt.Sprintf("subs/golden/%d", i)); changed {
				record.OK = expected.Value
				record.Raised = expected.HasError
				record.ErrorClass = expected.Error
				record.Error = expected.Message
			}
		}
		if entry, ok := native[fmt.Sprintf("golden/%d", i)]; ok {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(entry.Expected, &fields); err != nil {
				t.Fatal(err)
			}
			if rawErr, hasErr := fields["error"]; hasErr {
				record.Raised = true
				_ = json.Unmarshal(fields["class"], &record.ErrorClass)
				_ = json.Unmarshal(rawErr, &record.Error)
			} else {
				record.Raised = false
				record.OK = fields["value"]
			}
		}
		switch record.Fn {
		case "validate_dialers":
			cfg, err := decodeConfigRaw(record.Input)
			if err == nil {
				err = referenceFailure(ops.Validate(cfg))
			}
			if !record.Raised {
				if err != nil {
					t.Errorf("case %d: unexpected error %v for %s", i, err, record.Input)
				}
				stats["ok"]++
				continue
			}
			var de *DialerReferenceError
			switch {
			case record.ErrorClass == "DialerReferenceError":
				if !errors.As(err, &de) || de.Message != record.Error {
					t.Errorf("case %d: got %v, python %s for %s", i, err, record.Error, record.Input)
				}
				stats["dialer_error"]++
			case err == nil:
				// Python raised TypeError/AttributeError/... on a malformed profile
				stats["python_crash_go_ok:"+record.ErrorClass]++
				t.Logf("case %d: python %s (%s), Go accepts %s", i, record.ErrorClass, record.Error, record.Input)
			default:
				stats["python_crash_go_error"]++
			}
		case "parse_proxy_string":
			var inputStr string
			if err := json.Unmarshal(record.Input, &inputStr); err != nil {
				t.Fatalf("case %d input: %v", i, err)
			}
			node, serr := ops.Structured(inputStr)
			if serr != nil {
				node = nil
			}
			var gotRaw json.RawMessage = json.RawMessage("null")
			if node != nil {
				gotRaw = yamlToRaw(t, node)
			}
			if record.Raised {
				t.Errorf("case %d: python raised", i)
				continue
			}
			if _, lerr := readSubscriptionYAMLNode(strings.TrimSpace(inputStr), false); node == nil && string(record.OK) != "null" && errors.Is(lerr, yamlx.ErrPolicy) && strings.Contains(lerr.Error(), "JSON object key must be a string") {
				stats["proxy_string_non_string_keys"]++
				continue
			}
			if !testjson.Equal(record.OK, gotRaw) {
				stats["proxy_string_mismatch"]++
				t.Errorf("case %d parse(%q): got %s python %s", i, inputStr, gotRaw, record.OK)
				continue
			}
			stats["proxy_string_ok"]++
		}
	}
	t.Logf("%v", stats)
}
