package jsonresp

import (
	"encoding/json"
	"testing"
)

func TestEncodeUTF8KeepsLiteralEscapesAndValues(t *testing.T) {
	for _, value := range []string{"中<&>\u2028\u2029", `\u2028\u2029`, "\\\u2028\\\\\u2029", "\"\\\n\x00\u2028"} {
		data, err := EncodeUTF8(value)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		if err := json.Unmarshal(data, &got); err != nil || got != value {
			t.Fatalf("changed %q to %s: %v", value, data, err)
		}
	}
	data, err := EncodeUTF8("\u2028\u2029<&>")
	if err != nil || string(data) != "\"\u2028\u2029<&>\"" {
		t.Fatalf("not UTF-8 JSON data: %s %v", data, err)
	}
}
