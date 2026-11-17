package mosdns

import (
	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
	"errors"
	"testing"
)

func TestN73YAMLOnlyValuesRemainNodes(t *testing.T) {
	for _, text := range []string{"s: !!set {a}", "{1: x, extra: !!omap [{a: 1}]}", "? [a,b]\n: c"} {
		config := jsonx.Obj("mosdns", jsonx.Obj("custom_config", text))
		output, err := GenerateMosDNSConfig(config, "")
		if err != nil {
			t.Fatal(err)
		}
		source, err := yamlx.Parse([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		got, err := yamlx.Parse([]byte(output))
		if err != nil {
			t.Fatal(err)
		}
		for key, value := range yamlx.AllPairs(source) {
			found := false
			for k, v := range yamlx.AllPairs(got) {
				if yamlx.Equal(k, key) && yamlx.Equal(v, value) {
					found = true
					break
				}
			}
			if !found {
				t.Fatal("free-form key/value was lost")
			}
		}
	}
	_, err := GenerateMosDNSConfig(jsonx.Obj("mosdns", jsonx.Obj("custom_config", "r: &r [*r]")), "")
	if !errors.Is(err, yamlx.ErrAliasRecursion) {
		t.Fatalf("recursive user graph: %v", err)
	}
	config := jsonx.Obj("mosdns", jsonx.Obj("custom_config", "plugins: {1: x}\nextra: 1"))
	if _, err := GenerateMosDNSConfig(config, ""); err != nil {
		t.Fatal(err)
	}
}
