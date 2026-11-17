package difftest

import (
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx/yamltest"
)

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
	if xok && yok && yamltest.SemanticEqual(x, y) {
		a.Set("compose", y)
	}
	leftBundle, lok := a.Value("compose_bundle").(*jsonx.Object)
	rightBundle, rok := b.Value("compose_bundle").(*jsonx.Object)
	if lok && rok {
		leftArchive, leftYAML, leftErr := yamltest.ComposeArchive(leftBundle.GetString("content_base64"))
		rightArchive, rightYAML, rightErr := yamltest.ComposeArchive(rightBundle.GetString("content_base64"))
		if leftErr == nil && rightErr == nil && yamltest.SemanticEqual(leftYAML, rightYAML) && jsonx.Equal(leftArchive, rightArchive) {
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
