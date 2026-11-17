package pushdiff

import (
	"bytes"
	"encoding/json"
	"testing"

	"configflow/internal/yamlx"
)

// Changing the configuration writer must not invent semantic push changes or
// invalidate credential spans in the canonical patch rendered by A12.
func TestNativeWriterTransition(t *testing.T) {
	tests := []struct {
		name, source string
	}{
		{"indent-and-unicode", "proxies:\n- name: \"\\u9999\\u6E2F\"\n  type: http\n  port: 8080\n  password: synthetic-secret\nproxy-groups: []\n"},
		{"scalar-tags", "when: 2001-01-01 10:00:00\nlarge: 100000000000000000000\nbinary: !!binary YQ==\ncustom: !custom x\nnumber-like: '0777'\n"},
		{"aliases-and-merge", "base: &base {type: http, password: synthetic-secret}\nproxies:\n- <<: *base\n  name: Hong Kong\n"},
		{"multiline-secret", "password: \"synthetic-secret\\nsecond-line\\tvalue\\n\"\nmode: rule\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n, err := yamlx.Parse([]byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			out, err := yamlx.Marshal(n)
			if err != nil {
				t.Fatal(err)
			}
			again, err := yamlx.Parse(out)
			if err != nil || !yamlx.Equal(n, again) {
				t.Fatalf("writer changed semantics: %s: %v", out, err)
			}
			diff := Config([]byte(tc.source), out, "old.yaml", "new.yaml")
			if diff.Status != "formatting" || len(diff.Items) != 0 || diff.Omitted != 0 {
				t.Fatalf("writer change reported as semantic edit: %+v", diff)
			}
			if diff.Raw == nil || diff.Raw.Truncated || len(diff.Raw.Lines) != 0 {
				t.Fatalf("equivalent canonical output differs: %+v", diff.Raw)
			}
			// A real edit after the transition still produces marked credentials.
			yamlx.Set(again, "password", yamlx.Str("replacement-secret"))
			changed, err := yamlx.Marshal(again)
			if err != nil {
				t.Fatal(err)
			}
			diff = Config(out, changed, "old.yaml", "new.yaml")
			if diff.Status != "compared" || len(diff.Items) != 1 || diff.Items[0].Key != "password" {
				t.Fatalf("real edit missing: %+v", diff)
			}
			hidden, err := json.Marshal(WithoutSecrets(diff))
			if err != nil || bytes.Contains(hidden, []byte("replacement-secret")) || bytes.Contains(hidden, []byte("synthetic-secret")) {
				t.Fatalf("canonical credential spans lost: %s: %v", hidden, err)
			}
		})
	}
}
