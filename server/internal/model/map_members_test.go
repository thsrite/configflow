package model

import (
	"encoding/json"
	"testing"
)

func TestMapMembersPreserveTopLevelValidityAndOriginalTokens(t *testing.T) {
	var records Map[Deployment]
	if err := json.Unmarshal([]byte(`{"scalar":7,"empty":{},"nested":{"profile_id":7},"null":null}`), &records); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		key, raw string
		valid    bool
	}{
		{"scalar", `7`, false}, {"empty", `{}`, true}, {"nested", `{"profile_id":7}`, true}, {"null", `null`, false},
	} {
		if got := records.EntryValid(tt.key); got != tt.valid {
			t.Fatalf("key=%s valid=%v want=%v", tt.key, got, tt.valid)
		}
		raw, present, err := records.MemberJSON(tt.key)
		if err != nil || !present || string(raw) != tt.raw {
			t.Fatalf("key=%s raw=%s err=%v", tt.key, raw, err)
		}
		raw[0] = 'x'
		again, _, _ := records.MemberJSON(tt.key)
		if string(again) != tt.raw {
			t.Fatal("caller mutated original map entry")
		}
	}
	records.Set("scalar", Deployment{})
	if records.EntryValid("scalar") {
		t.Fatal("zero assignment authorized an unchanged invalid token")
	}
	records.Set("scalar", Deployment{Status: "preparing"})
	if !records.EntryValid("scalar") {
		t.Fatal("explicit nonzero repair still invalid")
	}
	records.Delete("null")
	records.Set("null", Deployment{})
	if !records.EntryValid("null") {
		t.Fatal("explicit new empty object is not valid")
	}
	var children Map[string]
	if err := json.Unmarshal([]byte(`{"text":"7","number":7}`), &children); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ key, raw string }{{"text", `"7"`}, {"number", `7`}} {
		raw, present, err := children.MemberJSON(tt.key)
		if err != nil || !present || string(raw) != tt.raw {
			t.Fatal(tt.key, string(raw), err)
		}
	}
}

func FuzzDeploymentMapMemberReads(f *testing.F) {
	f.Add([]byte(`{"text":{"profile_id":"7"},"number":7,"empty":{},"null":null}`))
	f.Add([]byte(`{"task":{"profile_id":7,"status":["bad"],"future":{"x":2.00}}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 32<<10 {
			t.Skip()
		}
		var original map[string]json.RawMessage
		if json.Unmarshal(data, &original) != nil {
			return
		}
		var records Map[Deployment]
		if json.Unmarshal(data, &records) != nil {
			return
		}
		before, err := Marshal(records)
		if err != nil {
			t.Fatal(err)
		}
		for key, want := range original {
			raw, present, err := records.MemberJSON(key)
			if err != nil || !present || string(raw) != string(want) {
				t.Fatalf("key=%s raw=%s want=%s err=%v", key, raw, want, err)
			}
			if len(raw) > 0 {
				raw[0] = 'x'
			}
			again, present, err := records.MemberJSON(key)
			if err != nil || !present || string(again) != string(want) {
				t.Fatal("returned bytes alias source")
			}
		}
		after, err := Marshal(records)
		if err != nil || string(before) != string(after) {
			t.Fatal("reads mutated map", err)
		}
	})
}
