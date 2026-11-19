package agents

import (
	"bytes"
	"encoding/json"
	"testing"

	"golang.org/x/text/encoding"
	textunicode "golang.org/x/text/encoding/unicode"
	"golang.org/x/text/encoding/unicode/utf32"
)

func FuzzNativeUnicodeTransport(f *testing.F) {
	for _, value := range []string{"", "中文", "a\xff\xe4\xb8", "literal\\u2028\u2028", "𝟙\x00"} {
		f.Add([]byte(value))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		original, err := json.Marshal(string(data))
		if err != nil {
			t.Fatal(err)
		}
		encodings := []encoding.Encoding{textunicode.UTF8BOM, textunicode.UTF16(textunicode.LittleEndian, textunicode.UseBOM), textunicode.UTF16(textunicode.BigEndian, textunicode.UseBOM), utf32.UTF32(utf32.LittleEndian, utf32.UseBOM), utf32.UTF32(utf32.BigEndian, utf32.UseBOM)}
		for _, enc := range encodings {
			wire, err := enc.NewEncoder().Bytes(original)
			if err != nil {
				t.Fatal(err)
			}
			decoded, ok := DecodeJSONBytes(wire)
			if !ok || !bytes.Equal([]byte(decoded), original) {
				t.Fatalf("Unicode transport lost JSON bytes: encoding=%v", enc)
			}
		}
	})
}
