package mosdns

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/testgolden"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"

	"gopkg.in/yaml.v3"
)

func mosDNSSources(fn string, args []any) []string {
	if len(args) == 0 {
		return nil
	}
	switch fn {
	case "generate":
		return yamlxtest.TextFields(args[0], "custom_config", "local_dns", "remote_dns", "fallback_dns")
	case "upstreams", "convert":
		if text, ok := args[0].(string); ok {
			return []string{text}
		}
	}
	return nil
}

func sameMosDNSOutput(fn string, want, got any) bool {
	if jsonx.Equal(want, got) {
		return true
	}
	if fn == "generate" {
		a, aok := want.(string)
		b, bok := got.(string)
		return aok && bok && yamlxtest.SemanticEqual(a, b)
	}
	if fn == "upstreams" {
		a, aok := want.(*jsonx.Object)
		b, bok := got.(*jsonx.Object)
		return aok && bok && a.Len() == 1 && b.Len() == 1 && yamlxtest.SemanticEqual(a.GetString("dump"), b.GetString("dump"))
	}
	return false
}

// nativeMosDNSOutcome permits only an independently classified input change;
// all generated values are compared against the actual Python converter.
func nativeMosDNSOutcome(t *testing.T, oracle *goldenOracle, row *jsonx.Object, got any, actualErr error) bool {
	t.Helper()
	fn := row.GetString("fn")
	args := row.GetList("args")
	changed := false
	for _, source := range mosDNSSources(fn, args) {
		_, parseErr := yamlx.Parse([]byte(source))
		if rejectYAML(parseErr) {
			var rejection *UnsupportedYAMLError
			return errors.As(actualErr, &rejection) && errors.Is(actualErr, parseErrKind(parseErr)) && actualErr.Error() == fmt.Sprintf(msgCustomYAML, parseErr)
		}
		class, err := yamlxtest.Classify(source)
		if err != nil {
			return false
		}
		changed = changed || class != yamlxtest.Equal && class != yamlxtest.Diagnostics
	}
	if !changed && (actualErr != nil || row.Has("err") || fn != "generate" && fn != "upstreams") {
		return false
	}
	args = jsonx.DeepCopy(args).([]any)
	normalize := func(source string) string {
		text, err := yamlxtest.ReferenceText(source)
		if err != nil {
			return source
		}
		return text
	}
	switch fn {
	case "generate":
		config, _ := args[0].(*jsonx.Object)
		settings, _ := config.Value("mosdns").(*jsonx.Object)
		for _, key := range []string{"custom_config", "local_dns", "remote_dns", "fallback_dns"} {
			if text, ok := settings.Value(key).(string); ok {
				if key == "custom_config" {
					settings.Set(key, normalize(text))
				} else {
					node, err := yamlx.Parse([]byte(text))
					if err == nil && nativeDNSContainer(node) {
						settings.Set(key, normalize(text))
					}
				}
			}
		}
	case "convert":
		args[0] = normalize(args[0].(string))
	case "upstreams":
		source := args[0].(string)
		node, err := yamlx.Parse([]byte(source))
		if err == nil && nativeDNSContainer(node) {
			args[0] = normalize(source)
		}
	}
	request := jsonx.Obj("fn", fn, "args", args)
	if actualErr == nil && (fn == "generate" || fn == "upstreams") {
		actualText := ""
		if fn == "generate" {
			actualText, _ = got.(string)
		} else if object, ok := got.(*jsonx.Object); ok {
			actualText = object.GetString("dump")
		}
		normalized, err := yamlxtest.ReferenceText(actualText)
		if err != nil {
			return false
		}
		request.Set("actual_yaml", normalized)
	}
	answer := oracle.Call(t, request)
	if expected := answer.GetList("err"); expected != nil {
		if actualErr == nil {
			t.Logf("native-policy oracle refusal: %v", expected)
			return false
		}
		// Native validation retains Chinese messages; internal Python exceptions
		// are not reconstructed. Both sides must still refuse the same input.
		if strings.ContainsFunc(expected[1].(string), func(r rune) bool { return r >= 0x4e00 && r <= 0x9fff }) {
			return actualErr.Error() == expected[1]
		}
		return true
	}
	if request.Has("actual_yaml") {
		return actualErr == nil && answer.GetBool("yaml_equal")
	}
	return actualErr == nil && sameMosDNSOutput(fn, answer.Value("out"), got)
}

func parseErrKind(err error) error {
	for _, kind := range []error{yamlx.ErrAliasRecursion, yamlx.ErrAliasBudget, yamlx.ErrPolicy} {
		if errors.Is(err, kind) {
			return kind
		}
	}
	return yamlx.ErrSyntax
}

func nativeDNSContainer(node *yamlx.Node) bool {
	node = yamlx.Resolve(node)
	if node.Kind == yaml.MappingNode {
		return yamlx.Get(node, "addr") != nil
	}
	if node.Kind == yaml.SequenceNode {
		for _, entry := range node.Content {
			entry = yamlx.Resolve(entry)
			if textScalar(entry) || entry.Kind == yaml.MappingNode && yamlx.Get(entry, "addr") != nil {
				return true
			}
		}
	}
	return false
}

// goldenOracle retains the JSON-lines reference boundary without running Python
// in ordinary tests. Live checks use the exact same script and request bytes.
type goldenOracle struct{ lines *testgolden.Lines }

func newGoldenOracle(t *testing.T, script string) *goldenOracle {
	t.Helper()
	return &goldenOracle{lines: testgolden.NewLines(t, script)}
}
func (o *goldenOracle) Call(t *testing.T, request any) *jsonx.Object {
	t.Helper()
	input, err := jsonx.Compact(request)
	if err != nil {
		t.Fatal(err)
	}
	output := o.lines.Call(t, input, nil)
	value, err := jsonx.Parse(output)
	if err != nil {
		t.Fatal(err)
	}
	return value.(*jsonx.Object)
}
