package yamlxtest

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestFormerNativeEmptyDocument(t *testing.T) {
	for _, source := range []string{"", "# comment\n", "  \n"} {
		document, err := FormerNativeDocument(source)
		if err != nil || document.Kind != yaml.DocumentNode || len(document.Content) != 0 {
			t.Fatalf("old empty stream contract changed: %#v %v", document, err)
		}
	}
}
