package mosdns

import (
	"errors"
	"testing"

	"configflow/internal/jsonx"
)

// Custom base YAML that jsonx cannot hold is refused (Python would dump it
// with yaml.Dumper), but only after every error Python raises first.
func TestUnholdableCustomConfigIsRefusedAfterPythonErrors(t *testing.T) {
	for _, text := range []string{"{1: x, extra: y}", "extra: {1: x}", "s: !!set {a}", "r: &r [*r]"} {
		config := jsonx.Obj("mosdns", jsonx.Obj("custom_config", text))
		_, err := GenerateMosDNSConfig(config, "")
		var u *UnsupportedYAMLError
		if !errors.As(err, &u) {
			t.Errorf("%q: want UnsupportedYAMLError, got %v", text, err)
		}
		config.GetObject("mosdns").Set("cache_size", "abc")
		_, err = GenerateMosDNSConfig(config, "")
		if errClass(err) != "ValueError" {
			t.Errorf("%q: Python's ValueError must come first, got %v", text, err)
		}
	}
	// Non-string keys only inside a replaced section are fine.
	config := jsonx.Obj("mosdns", jsonx.Obj("custom_config", "plugins: {1: x}\nextra: 1"))
	if _, err := GenerateMosDNSConfig(config, ""); err != nil {
		t.Errorf("plugins with a non-string key is replaced, got %v", err)
	}
}
