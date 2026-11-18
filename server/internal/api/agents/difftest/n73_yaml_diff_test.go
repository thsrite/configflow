package difftest

import (
	"testing"

	"configflow/internal/installer/installertest"
	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

func equalComposeYAML(left, right string) bool {
	a, err := yamlx.Parse([]byte(left))
	if err != nil {
		return false
	}
	b, err := yamlx.Parse([]byte(installertest.BootstrapBaseline(right)))
	return err == nil && yamlx.Equal(a, b)
}

func equalGuideComposeLayout(expected, actual []byte) []byte {
	left, err := jsonx.Parse(expected)
	if err != nil {
		return expected
	}
	right, err := jsonx.Parse(actual)
	if err != nil {
		return expected
	}
	a, aok := left.(*jsonx.Object)
	b, bok := right.(*jsonx.Object)
	if !aok || !bok {
		return expected
	}
	x, xok := a.Value("compose").(string)
	y, yok := b.Value("compose").(string)
	if xok && yok && equalComposeYAML(x, y) {
		a.Set("compose", y)
	}
	leftBundle, lok := a.Value("compose_bundle").(*jsonx.Object)
	rightBundle, rok := b.Value("compose_bundle").(*jsonx.Object)
	if lok && rok {
		leftArchive, leftYAML, leftErr := yamlxtest.ComposeArchive(leftBundle.GetString("content_base64"))
		rightArchive, rightYAML, rightErr := yamlxtest.ComposeArchive(rightBundle.GetString("content_base64"))
		if leftErr == nil && rightErr == nil && equalComposeYAML(leftYAML, rightYAML) && jsonx.Equal(leftArchive, rightArchive) {
			leftBundle.Set("content_base64", rightBundle.GetString("content_base64"))
		}
	}
	result, err := jsonx.Response(a)
	if err != nil {
		return expected
	}
	return result
}

func TestN73GuideComposeComparisonKeepsEveryOtherValue(t *testing.T) {
	expected := []byte(`{"success":true,"compose":"services: {agent: {image: synthetic, port: 7890}}\n","install_script":"echo synthetic"}`)
	for _, test := range []struct {
		name, compose, script string
		success, want         bool
	}{
		{"layout", "services:\n  agent:\n    image: synthetic\n    port: 7890\n", "echo synthetic", true, true},
		{"value", "services: {agent: {image: synthetic, port: 7891}}\n", "echo synthetic", true, false},
		{"type", "services: {agent: {image: synthetic, port: '7890'}}\n", "echo synthetic", true, false},
		{"script", "services: {agent: {image: synthetic, port: 7890}}\n", "echo changed", true, false},
		{"success", "services: {agent: {image: synthetic, port: 7890}}\n", "echo synthetic", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual, _ := jsonx.Response(jsonx.Obj("success", test.success, "compose", test.compose, "install_script", test.script))
			a, err := jsonx.Parse(equalGuideComposeLayout(expected, actual))
			if err != nil {
				t.Fatal(err)
			}
			b, err := jsonx.Parse(actual)
			if err != nil {
				t.Fatal(err)
			}
			if got := jsonx.Equal(a, b); got != test.want {
				t.Fatalf("equal=%v want %v", got, test.want)
			}
		})
	}
}

func TestNativeComposeHTTPComparison(t *testing.T) {
	for _, test := range []struct {
		name, before, after string
		equal               bool
	}{
		{"layout", "value: [one, two]\n", "value:\n  - one\n  - two\n", true},
		{"value", "value: one", "value: two", false},
		{"timestamp tag", "value: !!timestamp 2001-01-01", "value: '2001-01-01'", false},
		{"binary tag", "value: !!binary YQ==", "value: 'YQ=='", false},
		{"sequence order", "value: [one, two]", "value: [two, one]", false},
		{"invalid", "value: [", "value: []", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			makeResponse := func(text string) response {
				return response{method: "GET", status: 200, header: map[string][]string{"Content-Type": {"text/yaml; charset=utf-8"}}, body: []byte(text)}
			}
			before, after := makeResponse(test.before), makeResponse(test.after)
			if equal := len(compareInstaller(before, after, nil)) == 0; equal != test.equal {
				t.Fatalf("equal=%v want %v", equal, test.equal)
			}
			if test.equal {
				after.header.Set("Cache-Control", "private")
				if len(compareInstaller(before, after, nil)) == 0 {
					t.Fatal("YAML layout comparison ignored a changed header")
				}
			}
		})
	}
}
