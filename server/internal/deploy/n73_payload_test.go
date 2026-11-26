package deploy

import (
	"bytes"
	"testing"
)

func TestN73CommentedFlowRuleSourceIsNotEmpty(t *testing.T) {
	for _, prefix := range []string{"# header\n", "\ufeff\r\n# header\r\n---\r\n"} {
		source := prefix + "{payload: [\"+.example.com\", other.example]}\n"
		got, err := ConvertMosDNSRules([]byte(source), "domain_set", false)
		want := []byte("domain:example.com\nfull:other.example")
		if err != nil || len(got) == 0 || !bytes.Equal(got, want) {
			t.Fatalf("commented source: %q %v", got, err)
		}
	}
}
