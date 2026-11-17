package aggregations

import (
	"encoding/json"
	"testing"

	"configflow/internal/yamlx"
)

func TestProviderJSONScalarTypes(t *testing.T) {
	special := "line\nnext\ttab # {} [] : $ <>&\u0085\u2028\u2029"
	encodedSpecial, err := json.Marshal(special)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, raw, tag, value string
	}{
		{"surrogate pair", `"\ud83c\udded"`, "!!str", "🇭"},
		{"numeric string", `"1e3"`, "!!str", "1e3"},
		{"octal-looking string", `"0o17"`, "!!str", "0o17"},
		{"bool-looking string", `"on"`, "!!str", "on"},
		{"boolean", `true`, "!!bool", "true"},
		{"large integer", `100000000000000000001`, "!!int", "100000000000000000001"},
		{"special characters", string(encodedSpecial), "!!str", special},
	} {
		t.Run(test.name, func(t *testing.T) {
			text, err := providerYAML([]Proxy{{"name": json.RawMessage(`"Synthetic"`), "extra": json.RawMessage(test.raw)}})
			if err != nil {
				t.Fatal(err)
			}
			root, err := yamlx.Parse([]byte(text))
			if err != nil {
				t.Fatal(err)
			}
			proxies := yamlx.Get(root, "proxies")
			if proxies == nil || len(proxies.Content) != 1 {
				t.Fatal("typed proxy list changed")
			}
			value := yamlx.Get(proxies.Content[0], "extra")
			if value == nil || value.ShortTag() != test.tag || value.Value != test.value {
				t.Fatalf("JSON user value changed type or content: %#v", value)
			}
		})
	}
}
