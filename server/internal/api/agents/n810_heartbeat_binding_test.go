package agents

import (
	"encoding/json"
	"testing"
)

func TestN810HeartbeatMachineVersionAndNumberBoundaries(t *testing.T) {
	for _, test := range []struct {
		body  string
		valid bool
	}{
		{`{"version":"1.8.61-go+build"}`, true},
		{`{"version":""}`, false},
		{`{"version":"１２.0"}`, false},
		{`{"version":"1.0 bad"}`, false},
		{`{"Version": "1.0.0"}`, false},
		{`{"system_metrics":{"network":{"bytes_sent":18446744073709551615}}}`, true},
		{`{"system_metrics":{"network":{"bytes_sent":18446744073709551616}}}`, false},
		{`{"system_metrics":{"cpu":{"core_count":true}}}`, false},
	} {
		got, err := ValidateHeartbeatPayload(json.RawMessage(test.body))
		if err != nil || got != test.valid {
			t.Fatalf("%s: valid=%v err=%v", test.body, got, err)
		}
	}
}
func FuzzN810HeartbeatBinding(f *testing.F) {
	for _, body := range []string{`{}`, `null`, `{"version":"1.0.0-go"}`, `{"system_metrics":{"cpu":{"usage_percent":12.5}}}`, `{"system_metrics":{"cpu":{"core_count":18446744073709551616}}}`} {
		f.Add(body)
	}
	f.Fuzz(func(t *testing.T, body string) {
		valid, err := ValidateHeartbeatPayload(json.RawMessage(body))
		if err != nil || !valid {
			return
		}
		var object map[string]json.RawMessage
		if json.Unmarshal([]byte(body), &object) != nil || object == nil {
			t.Fatal("nonobject admitted")
		}
		for key := range object {
			if !heartbeatFields[key] {
				t.Fatalf("unknown member %q", key)
			}
		}
	})
}
