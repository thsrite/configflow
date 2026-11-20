package routersplit

import (
	"testing"

	"configflow/internal/testgolden"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"

	"gopkg.in/yaml.v3"
)

type nativeOracle struct{ lines *testgolden.Lines }

func newNativeOracle(t *testing.T) *nativeOracle {
	t.Helper()
	return &nativeOracle{lines: testgolden.NewLines(t, "server/internal/routersplit/testdata/n73_reference.py")}
}

func rowYAML(row recordedRow) (string, bool) {
	switch row.Fn {
	case "full_tun_base_yaml":
		value, ok := row.Args.value("text").(string)
		return value, ok
	case "base_is_full_tun", "check_generation", "mode_from_context", "router_split_state":
		config, _ := row.Args.value("config").(*recObj)
		mihomo, _ := config.value("mihomo").(*recObj)
		value, ok := mihomo.value("custom_config").(string)
		return value, ok
	}
	return "", false
}

func (o *nativeOracle) compare(t *testing.T, row recordedRow, output any, actualErr error) (string, bool) {
	t.Helper()
	source, ok := rowYAML(row)
	if !ok {
		return "", false
	}
	class, err := yamlxtest.Classify(source)
	if err != nil {
		t.Logf("N7-3 unclassified %s %.80q: %v", row.Fn, source, err)
		return "", false
	}
	if class == yamlxtest.Equal || class == yamlxtest.Diagnostics {
		return "", false
	}
	normalized, parseErr := yamlxtest.ReferenceText(source)
	if parseErr != nil {
		normalized = "["
	} // Python's existing invalid-document path.
	args := row.Args.clone()
	if row.Fn == "full_tun_base_yaml" {
		args.set("text", normalized)
	} else {
		args.obj("config").obj("mihomo").set("custom_config", normalized)
	}
	data, err := encodeCompactBytes(newRecObj("fn", row.Fn, "args", args))
	if err != nil {
		t.Fatal(err)
	}
	answerBytes := o.lines.Call(t, data, nil)
	value, err := parseRec(answerBytes)
	if err != nil {
		t.Fatal(err)
	}
	answer := value.(*recObj)
	if expected := answer.obj("err"); expected != nil {
		return class, compareError(expected, actualErr) == ""
	}
	if actualErr != nil {
		return class, false
	}
	expected := answer.value("out")
	if row.Fn == "full_tun_base_yaml" {
		got, gok := output.(string)
		want, wok := expected.(string)
		return class, gok && wok && yamlxtest.SemanticEqual(got, want)
	}
	return class, testjson.Equal(expected, output, testjson.CompareOptions{DistinguishNumberLiterals: true})
}

// nativeFullTunInvariant covers native non-string-key maps where Python folds
// bool/int keys together. It verifies every source pair outside tun unchanged.
func nativeFullTunInvariant(source, output string) bool {
	input, err := yamlx.Parse([]byte(source))
	if err != nil {
		return false
	}
	result, err := yamlx.Parse([]byte(output))
	if err != nil {
		return false
	}
	for key, value := range yamlx.AllPairs(input) {
		key = yamlx.Resolve(key)
		if key.Kind == yaml.ScalarNode && key.ShortTag() == "!!str" && key.Value == "tun" {
			continue
		}
		found := false
		for resultKey, resultValue := range yamlx.AllPairs(result) {
			if yamlx.Equal(key, resultKey) && yamlx.Equal(value, resultValue) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if !fullTunNodeShape(result) || len(nodeTunFilters(yamlx.Get(result, "tun"))) != 0 {
		return false
	}
	rootCount := 0
	for range yamlx.AllPairs(input) {
		rootCount++
	}
	if yamlx.Get(input, "tun") == nil {
		rootCount++
	}
	resultCount := 0
	for range yamlx.AllPairs(result) {
		resultCount++
	}
	if resultCount != rootCount {
		return false
	}
	originalTun := yamlx.Resolve(yamlx.Get(input, "tun"))
	resultTun := yamlx.Resolve(yamlx.Get(result, "tun"))
	forced := map[string]bool{"enable": true, "auto-route": true, "auto-redirect": true, "auto-detect-interface": true, "dns-hijack": true}
	expectedFields := map[string]bool{"enable": true, "auto-route": true, "auto-redirect": true, "auto-detect-interface": true, "dns-hijack": true, "stack": true}
	preservedComplex := 0
	for key, value := range yamlx.AllPairs(originalTun) {
		resolved := yamlx.Resolve(key)
		if resolved.Kind != yaml.ScalarNode || resolved.ShortTag() != "!!str" {
			preservedComplex++
			continue
		}
		removed := false
		for _, name := range RouteKeysRemoved {
			removed = removed || resolved.Value == name
		}
		if removed {
			continue
		}
		expectedFields[resolved.Value] = true
		if !forced[resolved.Value] && !yamlx.Equal(value, yamlx.Get(resultTun, resolved.Value)) {
			return false
		}
	}
	tunCount := 0
	for range yamlx.AllPairs(resultTun) {
		tunCount++
	}
	return tunCount == len(expectedFields)+preservedComplex
}
