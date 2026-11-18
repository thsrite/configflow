package jsonresp

import (
	"bytes"
	"testing"
)

func TestCanonicalIdentityEncoding(t *testing.T) {
	expected := []byte(`{"a":{"big":9007199254740993123456789,"name":"中文"},"b":1e400}`)
	for _, input := range []string{`{"b":1e400,"a":{"name":"\u4e2d\u6587","big":9007199254740993123456789}}`, string(expected)} {
		data, err := Canonical([]byte(input))
		if err != nil || !bytes.Equal(data, expected) {
			t.Fatalf("%s %v", data, err)
		}
	}
	for _, input := range []string{`{} {}`, `NaN`, `Infinity`, `-Infinity`} {
		if _, err := Canonical([]byte(input)); err == nil {
			t.Fatal("invalid input accepted", input)
		}
	}
}
