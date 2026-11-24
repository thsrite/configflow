package oxidns

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"

	"configflow/internal/testgolden"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

// nativeReference projects only YAML reader input. The business answer is
// independently calculated by the real Python module; unknown fields, failure
// messages, issue order and the complete output remain subject to comparison.
func nativeReference(t *testing.T, original row) (row, bool) {
	t.Helper()
	switch original.Fn {
	case "normalize", "load_native_yaml", "native_document", "generate", "downloads", "custom_files", "unavailable_rulesets", "resource_references", "validate_resource_files", "read_common_settings", "apply_common_settings", "read_routing", "apply_routing", "resource_downloads", "prepare_editor":
	default:
		return row{}, false
	}
	var args []any
	data, err := json.Marshal(original.Args)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err = decoder.Decode(&args); err != nil {
		t.Fatal(err)
	}
	replacements := map[string]string{}
	normalize := func(source string) string {
		normalized, err := yamlxtest.ReferenceText(source)
		if err == nil && normalized != source {
			replacements[normalized] = source
			return normalized
		}
		return source
	}
	var visit func(any)
	visit = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			// Full native drafts are YAML. Template DNS lines may use a separate text
			// syntax, so they are intentionally outside this reader-only projection.
			for _, key := range []string{"native_config", "custom_config", "local_dns", "remote_dns", "fallback_dns"} {
				if text, ok := v[key].(string); ok {
					if key == "native_config" || key == "custom_config" {
						v[key] = normalize(text)
					} else if root, err := yamlx.Parse([]byte(text)); err == nil && referenceDNSContainer(root) {
						v[key] = normalize(text)
					}
				}
			}
			for _, child := range v {
				visit(child)
			}
		case []any:
			for _, child := range v {
				visit(child)
			}
		}
	}
	visit(args)
	if original.Fn == "load_native_yaml" && len(args) > 0 {
		if text, ok := args[0].(string); ok {
			args[0] = normalize(text)
		}
	}
	metadataChanged := projectReferenceMetadata(original.Fn, args)
	if len(replacements) == 0 && !metadataChanged {
		return row{}, false
	}
	input, err := json.Marshal(struct {
		Fn   string `json:"fn"`
		Args []any  `json:"args"`
	}{original.Fn, args})
	if err != nil {
		t.Fatal(err)
	}
	oracle := nativeOracle(t)
	output := oracle.Call(t, input, nil)
	// If Python retains a draft's source verbatim, restore that exact source in
	// its returned JSON. Generated/edited text is never substituted.
	var answer any
	decoder = json.NewDecoder(bytes.NewReader(output))
	decoder.UseNumber()
	if err = decoder.Decode(&answer); err != nil {
		t.Fatal(err)
	}
	var restore func(any) any
	restore = func(v any) any {
		switch v := v.(type) {
		case string:
			if source, ok := replacements[v]; ok {
				return source
			}
			return v
		case []any:
			for i, item := range v {
				v[i] = restore(item)
			}
			return v
		case map[string]any:
			for key, item := range v {
				v[key] = restore(item)
			}
			return v
		}
		return v
	}
	output, err = json.Marshal(restore(answer))
	if err != nil {
		t.Fatal(err)
	}
	var projected row
	if err = json.Unmarshal(output, &projected); err != nil {
		t.Fatal(err)
	}
	return projected, true
}

func compare(t *testing.T, r row, got *Node, err error) string {
	t.Helper()
	if nativeValidationExpectation(t, r, got, err) || nativeCapabilityExpectation(t, r, got, err) {
		return ""
	}
	if nativeRequestExpectation(t, r, err) {
		return ""
	}
	difference := compareFrozen(r, got, err)
	if difference == "" {
		return ""
	}
	if projected, changed := nativeReference(t, r); changed {
		if nativeDifference := compareFrozen(projected, got, err); nativeDifference == "" {
			return ""
		} else {
			return difference + "; native reference: " + nativeDifference
		}
	}
	return difference
}

var nativeOracles = struct {
	sync.Mutex
	byTest map[*testing.T]*testgolden.Lines
}{byTest: map[*testing.T]*testgolden.Lines{}}

func nativeOracle(t *testing.T) *testgolden.Lines {
	t.Helper()
	nativeOracles.Lock()
	defer nativeOracles.Unlock()
	if oracle := nativeOracles.byTest[t]; oracle != nil {
		return oracle
	}
	oracle := testgolden.NewLines(t)
	nativeOracles.byTest[t] = oracle
	t.Cleanup(func() { nativeOracles.Lock(); delete(nativeOracles.byTest, t); nativeOracles.Unlock() })
	return oracle
}

func referenceDNSContainer(root *Node) bool {
	if isMap(root) {
		return has(root, "addr")
	}
	if isSeq(root) {
		for _, item := range items(root) {
			if isStr(item) || isMap(item) && has(item, "addr") {
				return true
			}
		}
	}
	return false
}
