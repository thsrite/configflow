package mosdns

import (
	"errors"
	"testing"

	"configflow/internal/jsonx"
)

// Custom base YAML values that jsonx cannot hold and that reach the output
// are refused (Python would dump them with yaml.Dumper), but only after every
// error Python raises first.
func TestUnholdableCustomConfigIsRefusedAfterPythonErrors(t *testing.T) {
	for _, text := range []string{"s: !!set {a}", "r: &r [*r]", "{1: x, extra: !!omap [{a: 1}]}"} {
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
