package convert

import (
	"encoding/json"
	"fmt"
	"testing"

	"configflow/internal/yamlx"
)

func TestNativeMihomoScalarProjection(t *testing.T) {
	tests := []struct {
		name, source, wantJSON string
	}{
		{"bare merge root", "<<\n", `"<<"`},
		{"bare merge value", "key: <<\n", `{"key":"<<"}`},
		{"bare merge item", "[<<]\n", `["<<"]`},
		{"aliased merge scalar", "key: &scalar <<\nother: *scalar\n", `{"key":"<<","other":"<<"}`},
		{"actual merge key", "base: &base {key: value}\ncopy: {<<: *base}\n", `{"base":{"key":"value"},"copy":{"key":"value"}}`},
		{"native scalar values", "[~, null, true, 0x1, 1e3, =]\n", `[null,null,true,1,1e3,"="]`},
	}
	for _, value := range []string{"<<", "~", "null", "Null", "NULL", "true", "false", "yes", "on", "0x1", "0o17", "0b1", "017", "1_000", "1e3", ".inf", ".nan", "2001-01-01", "=", ""} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		quoted := string(encoded)
		tests = append(tests,
			struct{ name, source, wantJSON string }{"root " + quoted, quoted + "\n", quoted},
			struct{ name, source, wantJSON string }{"key " + quoted, quoted + ": value\n", fmt.Sprintf(`{%s:"value"}`, quoted)},
			struct{ name, source, wantJSON string }{"value " + quoted, "key: " + quoted + "\n", fmt.Sprintf(`{"key":%s}`, quoted)},
		)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, err := readProxyNode(tt.source)
			if err != nil {
				t.Fatal(err)
			}
			want, err := yamlx.FromJSON([]byte(tt.wantJSON))
			if err != nil {
				t.Fatal(err)
			}
			if !yamlx.Equal(input, want) {
				t.Fatalf("read projection changed scalar semantics: %#v; want %s", input, tt.wantJSON)
			}
			output, err := outputGraph(input)
			if err != nil {
				t.Fatal(err)
			}
			data, err := yamlx.Marshal(output)
			if err != nil {
				t.Fatal(err)
			}
			again, err := yamlx.Parse(data)
			if err != nil || !yamlx.Equal(again, want) {
				t.Fatalf("scalar round trip %q: %v; want %s", data, err, tt.wantJSON)
			}
		})
	}
}
