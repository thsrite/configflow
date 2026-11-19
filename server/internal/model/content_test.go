package model

import "testing"

func TestJSONHasContentUsesNativeOmissionValues(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want bool
	}{
		{`null`, false}, {`false`, false}, {`true`, true}, {`""`, false}, {`"0"`, true},
		{`0`, false}, {`-0.00`, false}, {`0.5`, true}, {`1e999`, true}, {`1e-999`, false},
		{`[]`, false}, {`[null]`, true}, {`{}`, false}, {`{"x":false}`, true},
	} {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := JSONHasContent([]byte(tt.raw))
			if err != nil || got != tt.want {
				t.Fatalf("content=%v err=%v want=%v", got, err, tt.want)
			}
		})
	}
	for _, raw := range []string{`{"x":1,"x":2}`, `{"nested":{"x":1,"x":2}}`, `NaN`, `1 x`} {
		if _, err := JSONHasContent([]byte(raw)); err == nil {
			t.Fatalf("invalid JSON accepted: %s", raw)
		}
	}
}
