package convert

import (
	"testing"

	"configflow/internal/yamlx"
)

func TestTypedNameOverrideExactKeys(t *testing.T) {
	for _, tt := range []struct {
		source, want string
		invalid      bool
	}{
		{`null`, "abc", false},
		{`{"additional-prefix":"p-","proxy-name":[{"pattern":"b","target":"<$1>"}]}`, "", true}, // group 1 is absent
		{`{"additional-prefix":"p-","proxy-name":[{"pattern":"(b)","target":"<$1>"}]}`, "p-a<b>c", false},
		{`{"Additional-Prefix":"p-","Proxy-Name":[{"pattern":"(b)","target":"x"}]}`, "abc", false},
		{`{"proxy-name":[{"Pattern":"b","target":"x"}]}`, "", true},
		{`{"proxy-name":[{"pattern":"b"}]}`, "", true},
		{`{"proxy-name":[{"pattern":5,"target":"x"}]}`, "", true},
	} {
		t.Run(tt.source, func(t *testing.T) {
			node, err := yamlx.FromJSON([]byte(tt.source))
			if err != nil {
				t.Fatal(err)
			}
			override, err := nameOverrideFromNode(node)
			var result string
			if err == nil {
				result, err = ProviderName("abc", override)
			}
			if tt.invalid {
				if err == nil || err.Error() != msgProviderNameRule {
					t.Fatalf("%q %v", result, err)
				}
			} else if err != nil || result != tt.want {
				t.Fatalf("%q %v; want %q", result, err, tt.want)
			}
		})
	}
}
