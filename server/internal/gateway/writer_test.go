package gateway

import (
	"testing"

	"configflow/internal/yamlx"
)

func TestRulePayloadNativeWriter(t *testing.T) {
	for _, payload := range [][]string{nil, {}, {"+.example.com", "2001:db8::/32"}, {"0o17", "1e3", "yes", "2001-01-01", "tab\tvalue"}} {
		out, err := dumpPayload(payload)
		if err != nil {
			t.Fatal(err)
		}
		n, err := yamlx.Parse([]byte(out))
		if err != nil {
			t.Fatal(err)
		}
		items := yamlx.Seq()
		for _, item := range payload {
			items.Content = append(items.Content, yamlx.Str(item))
		}
		if !yamlx.Equal(n, yamlx.Map(yamlx.Str("payload"), items)) {
			t.Fatalf("rule payload types/fields changed: %s", out)
		}
	}
}
