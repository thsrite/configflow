package repo

// The compat helpers against the real Python functions. The expectations in
// testdata/compat_golden.json are written by testdata/compat_gen.py:
//
//	rt --back server/internal/repo/testdata/compat_golden.json -- bash -lc \
//	  'cd server/internal/repo && ~/cf-test/.venv/bin/python testdata/compat_gen.py testdata/compat_golden.json'

import (
	"errors"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/rules"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"
)

// compatCall runs the Go counterpart of one golden function.
func compatCall(t *testing.T, fn string, input any) (any, error) {
	t.Helper()
	object, _ := input.(*jsonx.Object)
	switch fn {
	case "validate_shapes":
		return nil, validateShapes(object.GetObject("profile"), object.GetBool("require_ids"))
	case "validate_dialers":
		return nil, validateDialers(object)
	case "raw_dialer":
		return rawDialer(object)
	case "parse_proxy_string":
		if parsed := parseStructuredProxyString(input.(string)); parsed != nil {
			return goldenDefault(parsed), nil
		}
		return nil, nil
	case "validate_health":
		return nil, validateHealth(object.Value("values"), object.GetBool("provider"))
	case "validate_refresh_interval":
		if err := validateRefreshInterval(input); err != nil {
			return nil, err
		}
		return input, nil // Python returns the value
	case "normalize_notes":
		result, err := normalizeNotes(input)
		if err != nil {
			return nil, err
		}
		return result, nil
	case "normalize_client_range":
		result, err := normalizeClientRange(input)
		if err != nil {
			return nil, err
		}
		return result, nil
	case "validate_bindings":
		return nil, validateBindings(input)
	case "normalize_action":
		result, err := normalizeAction(object.Value("value"), object.GetBool("parked"))
		if err != nil {
			return nil, err
		}
		return result, nil
	case "hydrate_ruleset":
		item := object.GetObject("item").DeepCopy()
		profileID, _ := object.Value("profile_id").(string)
		if err := hydrateRuleset(item, object.GetObject("library"), profileID); err != nil {
			return nil, err
		}
		return item, nil
	case "sanitize_rule_name":
		return sanitizeRuleNameOf(input)
	case "rule_cache_filename":
		return rules.RuleCacheFilename(object), nil
	}
	t.Fatalf("unknown golden function %q", fn)
	return nil, nil
}

func compatText(t *testing.T, value any) string {
	t.Helper()
	text, err := jsonx.Compact(value)
	if err != nil {
		t.Fatalf("encode %v: %v", value, err)
	}
	return string(text)
}

func TestCompatGolden(t *testing.T) {
	data, err := os.ReadFile("testdata/compat_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	cases := parsed.(*jsonx.Object).GetList("cases")
	if len(cases) < 150 {
		t.Fatalf("only %d golden cases", len(cases))
	}
	intended := map[string]bool{ClassValueError: true, ClassDialerReference: true, ClassOxidnsSettings: true}
	counts := map[string]int{}
	for index, item := range cases {
		record := item.(*jsonx.Object)
		fn := record.GetString("fn")
		counts[fn]++
		input := record.Value("input")
		label := fn + " #" + jsonx.Repr(jsonx.Int(index))
		brief := compatText(t, input)
		if len(brief) > 300 {
			brief = brief[:300] + "…"
		}
		// The Python inputs are deep-copied per case; the Go helpers must not
		// modify theirs either (hydrate_ruleset works on a copy above).
		before := compatText(t, input)
		got, err := compatCall(t, fn, input)
		if after := compatText(t, input); after != before {
			t.Errorf("%s modified its input %s", label, brief)
		}
		if !record.GetBool("raised") {
			if err != nil && refusedByNetURL(input) {
				continue // accepted N2 difference: a URL net/url refuses
			}
			if err != nil {
				t.Errorf("%s: input %s: unexpected error %v", label, brief, err)
				continue
			}
			if want, have := compatText(t, record.Value("ok")), compatText(t, got); want != have {
				t.Errorf("%s: input %s:\n  python %s\n  go     %s", label, brief, want, have)
			}
			continue
		}
		class, message := record.GetString("error_class"), record.GetString("error")
		var helper *HelperError
		if err == nil || !errors.As(err, &helper) {
			t.Errorf("%s: input %s: got (%v, %v), python raised %s: %s", label, brief, got, err, class, message)
			continue
		}
		if intended[class] {
			if helper.Class != class || helper.Message != message {
				t.Errorf("%s: input %s:\n  python %s: %s\n  go     %s: %s", label, brief, class, message, helper.Class, helper.Message)
			}
		} else if intended[helper.Class] {
			// An unintended Python exception (TypeError, KeyError, ...): only the family must match.
			t.Errorf("%s: input %s: python raised %s (%s), go %s: %s", label, brief, class, message, helper.Class, helper.Message)
		}
	}
	for _, fn := range []string{"validate_shapes", "validate_dialers", "raw_dialer", "parse_proxy_string",
		"validate_health", "validate_refresh_interval", "normalize_notes", "normalize_client_range",
		"validate_bindings", "normalize_action", "hydrate_ruleset", "sanitize_rule_name", "rule_cache_filename"} {
		if counts[fn] == 0 {
			t.Errorf("no golden cases for %s", fn)
		}
	}
}

// goldenDefault is json.dumps(value, default=lambda o: [str(o)]) of the
// golden generator for the YAML values jsonx has no JSON form for.
func goldenDefault(v any) any {
	switch value := v.(type) {
	case *jsonx.Object:
		out := jsonx.NewObject()
		for k, child := range value.All() {
			out.Set(k, goldenDefault(child))
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			out[i] = goldenDefault(child)
		}
		return out
	case yamlx.Timestamp:
		return []any{value.String()}
	case []byte:
		return []any{"b" + jsonx.Repr(string(value))}
	}
	return v
}

// A YAML timestamp is a datetime in Python: truthy as a type, not a string
// as a dialer. The golden writes it as [text], which is what Go returns.
func TestCompatProxyStringTimestamp(t *testing.T) {
	parsed := parseStructuredProxyString("type: ss\ndialer-proxy: 2024-01-02")
	if parsed == nil {
		t.Fatal("not parsed")
	}
	if _, isString := parsed.Value("dialer-proxy").(string); isString {
		t.Fatal("a timestamp must not be a string")
	}
	profile := jsonx.Obj("nodes", []any{jsonx.Obj("id", "a", "name", "a", "proxy_string", "type: ss\ndialer-proxy: 2024-01-02")})
	err := validateDialers(profile)
	if !isHelperClass(err, ClassDialerReference) || err.Error() != "原始 dialer-proxy 必须是非空名称" {
		t.Fatalf("got %v", err)
	}
	if parseStructuredProxyString("type: 2024-13-45") != nil {
		t.Fatal("an impossible date must make the string unparsable")
	}
	// bytes and self-referencing containers are truthy non-strings in Python too
	for _, text := range []string{"type: !!binary YWJj", "a: &a [*a]\ntype: x", "type: &t [*t]", "type: ss\nself: &s {me: *s}"} {
		if parseStructuredProxyString(text) == nil {
			t.Errorf("%q: not parsed", text)
		}
	}
	if parseStructuredProxyString("type: !!binary '***'") != nil {
		t.Error("invalid base64 must make the string unparsable")
	}
}

// The parser is replaceable by the mihomo converter package.
func TestCompatProxyStringOverride(t *testing.T) {
	previous := parseStructuredProxyString
	defer func() { parseStructuredProxyString = previous }()
	parseStructuredProxyString = func(string) *jsonx.Object { return jsonx.Obj("type", "x", "dialer-proxy", "via") }
	value, err := rawDialer(jsonx.Obj("proxy_string", "anything"))
	if err != nil || value != "via" {
		t.Fatalf("got %v, %v", value, err)
	}
}

// refusedByNetURL reports whether v holds a URL string ("://") that net/url
// refuses although urllib.parse accepted it (control characters, an
// IPvFuture literal, a percent-encoded host, ...); since N2 such URLs are
// rejected.
func refusedByNetURL(v any) bool {
	switch v := v.(type) {
	case string:
		if !strings.Contains(v, "://") {
			return false
		}
		_, err := weburl.Parse(v)
		return err != nil
	case []any:
		for _, item := range v {
			if refusedByNetURL(item) {
				return true
			}
		}
	case *jsonx.Object:
		for _, key := range v.Keys() {
			if refusedByNetURL(v.Value(key)) {
				return true
			}
		}
	}
	return false
}
