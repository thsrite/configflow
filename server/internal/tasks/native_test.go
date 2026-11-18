package tasks

import (
	"bytes"
	"encoding/json"
	"testing"
)

func FuzzTaskRowRoundTrip(f *testing.F) {
	for _, seed := range []string{`{"key":"subscription:s","category":"subscription","name":"feed","enabled":false,"run_count":1,"failure_count":0,"future":{"large":9007199254740993123456789}}`, `{}`, `{"key":"x","last_status":null,"notifications_enabled":false}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		var row Task
		if json.Unmarshal([]byte(input), &row) != nil {
			return
		}
		first, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		var again Task
		if err = json.Unmarshal(first, &again); err != nil {
			t.Fatal(err)
		}
		second, err := json.Marshal(again)
		if err != nil || !bytes.Equal(first, second) {
			t.Fatalf("unstable row: %s -> %s: %v", first, second, err)
		}
	})
}
func TestTypedRowDefaultsAndOwnerFields(t *testing.T) {
	var row Task
	if err := json.Unmarshal([]byte(`{"key":"x","future":{"big":9007199254740993123456789}}`), &row); err != nil {
		t.Fatal(err)
	}
	if !row.Enabled || row.Key != "x" {
		t.Fatal("row defaults", row)
	}
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["future"]) != `{"big":9007199254740993123456789}` || string(fields["interval_seconds"]) != "null" || string(fields["can_run"]) != "false" {
		t.Fatalf("row shape: %s", data)
	}
}
