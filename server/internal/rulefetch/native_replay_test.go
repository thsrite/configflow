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
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

type nativeReplayCase struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Native   string `json:"native"`
}

func nativeReplayDifference(t *testing.T, suite, fn string, args any, got, want string) string {
	t.Helper()
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
		return jsonx.ParseString(s)
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
	if jsonx.Equal(g, w) {
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
	case *jsonx.Object:
		b, ok := w.(*jsonx.Object)
		if !ok {
			return false
		}
		diagnostic := diagnostics && yamlDiagnostic(a.GetString("message")) && yamlDiagnostic(b.GetString("message"))
		if a.Len() != b.Len() {
			return false
		}
		for k, v := range a.All() {
			if diagnostic && (k == "message" || k == "line" || k == "before") {
				continue
			}
			if diagnostics && k == "text" && yamlDiagnostic(a.GetString("text")) && yamlDiagnostic(b.GetString("text")) {
				continue
			}
			if diagnostics && k == "text" && a.Has("errors") && a.Value("errors") != nil && b.Has("errors") {
				continue
			}
			bv, exists := b.Get(k)
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
				if o, ok := value.(*jsonx.Object); ok {
					value = o.Value("content")
				}
			}
		}
	case *jsonx.Object:
		value = a.Value("content")
	}
	if o, ok := value.(*jsonx.Object); ok {
		if text, ok := o.Value("__bytes__").(string); ok {
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
	v, err := jsonx.ParseString(text)
	if err != nil {
		return false
	}
	var has func(any) bool
	has = func(v any) bool {
		switch v := v.(type) {
		case *jsonx.Object:
			for _, key := range []string{"text", "message"} {
				if strings.Contains(v.GetString(key), diagnostic) {
					return true
				}
			}
			for _, child := range v.All() {
				if has(child) {
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
	v, err := jsonx.ParseString(strings.TrimPrefix(strings.TrimPrefix(text, "error "), "ok "))
	if err != nil {
		return false
	}
	switch v := v.(type) {
	case *jsonx.Object:
		if class := v.GetString("class"); class != "" {
			return class == "ProfileValidationError" || class == "RemoteRuleValidationError"
		}
		return v.GetString("content") == source && len(v.GetList("errors")) > 0
	case []any:
		if len(v) != 2 || v[0] != source {
			return false
		}
		warnings, ok := v[1].([]any)
		return ok && len(warnings) > 0
	}
	return false
}
