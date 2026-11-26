package rulefetch

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"configflow/internal/yamlx"
)

type nativeReplayCase struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Native   string `json:"native"`
}

func nativeReplayDifference(t *testing.T, suite, fn string, args any, got, want string) string {
	t.Helper()
	if hasRareLineBreak(args) {
		return "O17 line breaks"
	}
	if category := nativeHTMLDifference(t, suite, fn, args, got); category != "" {
		return category
	}
	source := nativeReplaySource(fn, args)
	_, parseErr := yamlx.Parse([]byte(source))
	if (errors.Is(parseErr, yamlx.ErrPolicy) || errors.Is(parseErr, yamlx.ErrAliasRecursion) || errors.Is(parseErr, yamlx.ErrAliasBudget)) && boundaryHasDiagnostic(got, parseErr.Error()) && boundaryRefusedSource(got, source) {
		if errors.Is(parseErr, yamlx.ErrDuplicateKey) {
			return "D1 duplicate"
		}
		if errors.Is(parseErr, yamlx.ErrAliasRecursion) || errors.Is(parseErr, yamlx.ErrAliasBudget) {
			return "alias refusal"
		}
		return "5.4 validation policy"
	}
	// The old oracle omitted English parser text. A native Chinese diagnostic
	// is now intentionally visible; retain the same typed refusal and exact cause.
	if errors.Is(parseErr, yamlx.ErrSyntax) && boundaryHasDiagnostic(got, parseErr.Error()) && boundaryRefusedSource(got, source) && sameFailureMetadata(got, want) {
		return "5.7 diagnostics"
	}
	if nativeReplayEquivalent(got, want, false) {
		return "D3 layout"
	}
	if nativeReplayEquivalent(got, want, true) {
		return "5.7 diagnostics"
	}
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(suite+"/"+fn+"/"+encode(args))))
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(got)))
	data, err := os.ReadFile("testdata/n73_native_differences.json")
	if err != nil {
		return ""
	}
	var cases []nativeReplayCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if c.ID == id {
			if c.Native != digest {
				t.Errorf("native replay changed: %s/%s %s", suite, fn, id)
				return ""
			}
			return c.Category
		}
	}
	return ""
}

// Replay compares boundary data structurally. Only YAML documents are compared
// as Nodes; rule values and every other response field retain exact comparison.
func nativeReplayEquivalent(got, want string, diagnostics bool) bool {
	if strings.HasPrefix(got, "error ") != strings.HasPrefix(want, "error ") || strings.HasPrefix(got, "ok ") != strings.HasPrefix(want, "ok ") {
		return false
	}
	parse := func(s string) (any, error) {
		for _, p := range []string{"error ", "ok "} {
			s = strings.TrimPrefix(s, p)
		}
		return parseOrderedJSON([]byte(s))
	}
	g, ge := parse(got)
	w, we := parse(want)
	if ge != nil || we != nil {
		return false
	}
	return nativeBoundaryEqual(g, w, diagnostics)
}

func yamlDiagnostic(s string) bool {
	for _, m := range []string{msgYAMLPayload, msgUniquePayload, msgRemotePayload, msgPayloadList, msgUnreadablePayload} {
		if strings.Contains(s, m) {
			return true
		}
	}
	for _, text := range []string{"yaml:", "while parsing", "while scanning", "unacceptable character", "expected a single", "expected alphabetic", "expected '<", "found character", "found undefined alias", "YAML 包含重复键", "YAML 格式无效", "YAML 只能包含一个文档", "while constructing", "规则集的 payload 格式无法读取"} {
		if strings.Contains(s, text) {
			return true
		}
	}
	return false
}

func nativeBoundaryEqual(g, w any, diagnostics bool) bool {
	if orderedEqual(g, w) {
		return true
	}
	switch a := g.(type) {
	case string:
		b, ok := w.(string)
		if !ok {
			return false
		}
		if !strings.Contains(a, "payload") || !strings.Contains(b, "payload") {
			return false
		}
		an, ae := yamlx.Parse([]byte(a))
		bn, be := yamlx.Parse([]byte(b))
		return ae == nil && be == nil && yamlx.Get(an, "payload") != nil && yamlx.Get(bn, "payload") != nil && yamlx.Equal(an, bn)
	case []any:
		b, ok := w.([]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for i, v := range a {
			if !nativeBoundaryEqual(v, b[i], diagnostics) {
				return false
			}
		}
		return true
	case *orderedObj:
		b, ok := w.(*orderedObj)
		if !ok {
			return false
		}
		diagnostic := diagnostics && yamlDiagnostic(a.str("message")) && yamlDiagnostic(b.str("message"))
		if a.len() != b.len() {
			return false
		}
		for _, kv := range a.items {
			k, v := kv.k, kv.v
			if diagnostic && (k == "message" || k == "line" || k == "before") {
				continue
			}
			if diagnostics && k == "text" && yamlDiagnostic(a.str("text")) && yamlDiagnostic(b.str("text")) {
				continue
			}
			if diagnostics && k == "text" && a.has("errors") && a.val("errors") != nil && b.has("errors") {
				continue
			}
			bv, exists := b.get(k)
			if !exists || !nativeBoundaryEqual(v, bv, diagnostics) {
				return false
			}
		}
		return true
	}
	return false
}

func nativeReplaySource(fn string, args any) string {
	var value any
	switch a := args.(type) {
	case []any:
		if len(a) > 0 {
			value = a[0]
			if fn == "validate_domain" {
				if o, ok := value.(*orderedObj); ok {
					value = o.val("content")
				}
			}
		}
	case *orderedObj:
		value = a.val("content")
	}
	if o, ok := value.(*orderedObj); ok {
		if text, ok := o.val("__bytes__").(string); ok {
			b, _ := hex.DecodeString(text)
			return string(b)
		}
	}
	text, _ := value.(string)
	if fn == "validate_remote_bytes" {
		b, _ := base64.StdEncoding.DecodeString(text)
		return string(b)
	}
	return text
}

func boundaryHasDiagnostic(text, diagnostic string) bool {
	text = strings.TrimPrefix(strings.TrimPrefix(text, "error "), "ok ")
	v, err := parseOrderedJSON([]byte(text))
	if err != nil {
		return false
	}
	var has func(any) bool
	has = func(v any) bool {
		switch v := v.(type) {
		case *orderedObj:
			for _, key := range []string{"text", "message"} {
				if strings.Contains(v.str(key), diagnostic) {
					return true
				}
			}
			for _, kv := range v.items {
				if has(kv.v) {
					return true
				}
			}
		case []any:
			for _, child := range v {
				if has(child) {
					return true
				}
			}
		}
		return false
	}
	return has(v)
}

func boundaryRefusedSource(text, source string) bool {
	v, err := parseOrderedJSON([]byte(strings.TrimPrefix(strings.TrimPrefix(text, "error "), "ok ")))
	if err != nil {
		return false
	}
	switch v := v.(type) {
	case *orderedObj:
		if class := v.str("class"); class != "" {
			return class == "ProfileValidationError" || class == "RemoteRuleValidationError"
		}
		return v.str("content") == source && len(v.list("errors")) > 0
	case []any:
		if len(v) != 2 || v[0] != source {
			return false
		}
		warnings, ok := v[1].([]any)
		return ok && len(warnings) > 0
	}
	return false
}

func sameFailureMetadata(got, want string) bool {
	decode := func(text string) *orderedObj {
		v, err := parseOrderedJSON([]byte(strings.TrimPrefix(strings.TrimPrefix(text, "error "), "ok ")))
		if err != nil {
			return nil
		}
		o, _ := v.(*orderedObj)
		return o
	}
	g, w := decode(got), decode(want)
	if g == nil || w == nil || g.str("class") == "" || g.str("class") != w.str("class") {
		return false
	}
	g.del("text")
	w.del("text")
	return encode(g) == encode(w)
}

var nativeHTMLCases = sync.OnceValues(func() (map[string]nativeReplayCase, error) {
	data, err := os.ReadFile("testdata/n811_native_html_differences.json")
	if err != nil {
		return nil, err
	}
	var rows []nativeReplayCase
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, err
	}
	cases := make(map[string]nativeReplayCase, len(rows))
	for _, row := range rows {
		if previous, ok := cases[row.ID]; ok && previous != row {
			return nil, fmt.Errorf("conflicting HTML difference %s", row.ID)
		}
		cases[row.ID] = row
	}
	return cases, nil
})

func nativeHTMLDifference(t *testing.T, suite, fn string, args any, got string) string {
	t.Helper()
	if !(suite == "review" && (fn == "unescape" || fn == "meta_content" || fn == "clean_text") || suite == "corpus" && fn == "page_identity") {
		return ""
	}
	cases, err := nativeHTMLCases()
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(suite+"/"+fn+"/"+encode(args))))
	row, ok := cases[id]
	if !ok {
		return ""
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(got)))
	if row.Native != digest {
		return ""
	}
	return row.Category
}

func TestN811HTMLReplayNegativeControl(t *testing.T) {
	args := []any{"&#1;"}
	if got := nativeHTMLDifference(t, "review", "unescape", args, encode("\x01")); got != "N8-11 native HTML" {
		t.Fatalf("native HTML case not checked: %q", got)
	}
	if got := nativeHTMLDifference(t, "review", "unescape", args, encode("wrong")); got != "" {
		t.Fatal("changed native HTML output bypassed replay")
	}
}
