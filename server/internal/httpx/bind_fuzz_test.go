package httpx

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func FuzzBindNativeJSON(f *testing.F) {
	for _, text := range []string{`{"n":6e1,"n":60.0}`, `{"n":9223372036854775808}`, `{"x":{"a":1},"x":{"b":2}}`, `["a,b:c",null,true]`, `{"中":"\u0036\ud800"}`, `{"n":NaN}`, "\xef\xbb\xbf{}", "{} {}"} {
		f.Add([]byte(text))
	}
	f.Fuzz(func(t *testing.T, source []byte) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader(source))
		req.Header.Set("Content-Type", "application/json")
		c := &Ctx{Request: req}
		var got any
		err := c.Bind(&got)
		text, textErr := decodeJSONBytes(source)
		if textErr != nil || !json.Valid(text) {
			if err == nil {
				t.Fatal("invalid JSON accepted")
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		d := json.NewDecoder(bytes.NewReader(text))
		d.UseNumber()
		var want any
		if err := d.Decode(&want); err != nil {
			t.Fatal(err)
		}
		a, aErr := json.Marshal(got)
		b, bErr := json.Marshal(want)
		if aErr != nil || bErr != nil || !bytes.Equal(a, b) {
			t.Fatalf("native value changed: %s != %s: %v %v", a, b, aErr, bErr)
		}
	})
}
