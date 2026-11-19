package jsonresp

import (
	"encoding/json"
	"math"
	"testing"
)

func TestDeviceJSONUsesProtocolBytes(t *testing.T) {
	tests := []struct{ raw, want string }{
		{`{"x":1.5,"y":"中"}`, `{"x": 1.5, "y": "\u4e2d"}`},
		{`{"content":"日志😀","n":2.00,"arr":[true,null,{"z":[]}]}`, `{"content": "\u65e5\u5fd7\ud83d\ude00", "n": 2.00, "arr": [true, null, {"z": []}]}`},
		{`{"literal":"\\u4e2d\n","url":"https://example.com/a?x=1&y=<tag>"}`, `{"literal": "\\u4e2d\n", "url": "https://example.com/a?x=1&y=<tag>"}`},
		{`[]`, `[]`}, {`{}`, `{}`}, {`null`, `null`}, {`"a,b:中"`, `"a,b:\u4e2d"`},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := EncodeDevice(json.RawMessage(tt.raw))
			if err != nil || string(got) != tt.want {
				t.Fatalf("got %s err=%v want %s", got, err, tt.want)
			}
		})
	}
	got, err := EncodeDevice(struct {
		Control string `json:"control"`
	}{"\x7f"})
	if err != nil || string(got) != `{"control": "\u007f"}` {
		t.Fatalf("DEL protocol byte changed: %s err=%v", got, err)
	}
	if _, err := EncodeDevice(math.NaN()); err == nil {
		t.Fatal("nonfinite device payload accepted")
	}
	if _, err := EncodeDevice(json.RawMessage(`{"x":NaN}`)); err == nil {
		t.Fatal("invalid raw JSON accepted")
	}
}

func FuzzDeviceJSONPreservesValues(f *testing.F) {
	f.Add([]byte(`{"x":2.00,"name":"日志😀","nested":[{},[]]}`))
	f.Add([]byte(`"\\u0000\n,中:😀"`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 32<<10 || !json.Valid(raw) {
			return
		}
		got, err := EncodeDevice(json.RawMessage(raw))
		if err != nil {
			return
		}
		// Native canonical encoding compares the complete value, keeping exact
		// number tokens; it does not normalize device request bytes.
		before, err := Canonical(raw)
		if err != nil {
			return
		}
		after, err := Canonical(got)
		if err != nil || string(before) != string(after) {
			t.Fatalf("wire encoding changed JSON: %s -> %s err=%v", raw, got, err)
		}
	})
}
