package yamlx

import (
	"reflect"
	"testing"
)

func TestReadableUnicodeOnlyExpandsActualQuotedEscapes(t *testing.T) {
	source := "名称: \"\\U0001F1ED\\U0001F1F0 香港节点\"\n" +
		"plain: \\U0001F680\nsingle: '\\U0001F680'\n" +
		"mixed: \"\\\\U0001F680 \\\"\\U0001F680\\\"\"\n" +
		"block: |\n  \"\\U0001F680\"\n# \\U0001F680\n"
	want := "名称: \"🇭🇰 香港节点\"\n" +
		"plain: \\U0001F680\nsingle: '\\U0001F680'\n" +
		"mixed: \"\\\\U0001F680 \\\"🚀\\\"\"\n" +
		"block: |\n  \"\\U0001F680\"\n# \\U0001F680\n"
	got := ReadableUnicode(source)
	if got != want {
		t.Fatalf("unexpected output:\n%s", got)
	}
	var before, after map[string]string
	if err := Unmarshal([]byte(source), &before); err != nil {
		t.Fatal(err)
	}
	if err := Unmarshal([]byte(got), &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("decoded strings changed")
	}
}
