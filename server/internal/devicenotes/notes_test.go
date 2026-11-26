package devicenotes

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestParseNotesValidation(t *testing.T) {
	tests := []struct{ name, raw, message string }{
		{"array", `[]`, NotesMessage},
		{"null", `null`, NotesMessage},
		{"bad address", `{"bad":"name"}`, AddressMessage},
		{"null name", `{"192.0.2.1":null}`, NameMessage},
		{"numeric name", `{"192.0.2.1":1}`, NameMessage},
		{"newline", `{"192.0.2.1":"one\ntwo"}`, NameMessage},
		{"DEL", `{"192.0.2.1":"one\u007ftwo"}`, NameMessage},
		{"duplicate canonical", `{"192.0.2.1":"first","::ffff:192.0.2.1":"second"}`, "设备备注地址重复：192.0.2.1"},
		{"trailing document", `{} {}`, NotesMessage},
		{"valid", `{"192.0.2.129/24":"  测试😀  ","2001:db8::1/128":""}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notes, err := Parse(json.RawMessage(tt.raw))
			if tt.message != "" {
				if err == nil || err.Error() != tt.message {
					t.Fatalf("got %v, want %s", err, tt.message)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if notes["192.0.2.0/24"] != "测试😀" || notes["2001:db8::1"] != "" {
				t.Fatal(notes)
			}
		})
	}
}

func TestLegacyNamesAndPolicyPlan(t *testing.T) {
	state, err := New(json.RawMessage(`{"192.0.2.1":"","192.0.2.2":"New","192.0.2.3":"Outside"}`), json.RawMessage(`[
	{"ip":"192.0.2.1","name":"Hidden","mode":"smart"},
	{"ip":"192.0.2.2","name":"Legacy","mode":"direct"},
	{"ip":"192.0.2.4","name":"Keep removed","mode":"smart"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state.ReadNames(), Names{"192.0.2.2": "New", "192.0.2.3": "Outside", "192.0.2.4": "Keep removed"}) {
		t.Fatal(state.ReadNames())
	}
	rows, notes, err := state.Prepare([]Policy{{"192.0.2.1", "", "smart"}, {"192.0.2.3", "", "direct"}})
	if err != nil {
		t.Fatal(err)
	}
	if rows[1].Name != "Outside" || notes["192.0.2.1"] != "" || notes["192.0.2.4"] != "Keep removed" {
		t.Fatal(rows, notes)
	}
	rows[0].Name = "changed"
	notes["192.0.2.1"] = "changed"
	if state.StoredNames()["192.0.2.1"] != "" {
		t.Fatal("plan aliases the original snapshot")
	}
	before, err := state.Revision()
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 64 {
		t.Fatal(before)
	}
	cleared, err := state.Set("192.0.2.2", "")
	if err != nil || cleared["192.0.2.2"] != "" {
		t.Fatal(cleared, err)
	}
	var zero State
	if _, err := zero.Set("192.0.2.8", "New"); err != nil {
		t.Fatal(err)
	}
}

func TestNameLimitsAndLegacyDELRepair(t *testing.T) {
	if _, err := NormalizeName(strings.Repeat("界", 80)); err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeName(strings.Repeat("界", 81)); err == nil {
		t.Fatal("overlong name accepted")
	}
	rows, err := ParsePolicies(json.RawMessage(`[{"ip":"::ffff:192.0.2.1","name":" old\u007f name ","mode":"smart"}]`))
	if err != nil || rows[0].Name != "old name" || rows[0].IP != "192.0.2.1" {
		t.Fatal(rows, err)
	}
	if !SameRoutes(rows, []Policy{{"192.0.2.1", "renamed", "smart"}}) {
		t.Fatal("display name affects routing identity")
	}
}

func FuzzStoredNotes(f *testing.F) {
	for _, seed := range []string{`{}`, `null`, `{"192.0.2.1":"测试"}`, `{"192.0.2.1":"","::ffff:192.0.2.1":"name"}`, `{"x":[[[[]]]]}`, `{"192.0.2.1":"line\u2028sep\u2029end\\u2028"}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 1<<20 {
			t.Skip()
		}
		notes, err := Parse(raw)
		if err != nil {
			return
		}
		encoded, err := json.Marshal(notes)
		if err != nil {
			t.Fatal(err)
		}
		again, err := Parse(encoded)
		if err != nil || !reflect.DeepEqual(again, notes) {
			t.Fatal("normalized notes do not round trip", err)
		}
		rows := make([]Policy, 0, len(notes))
		for ip := range notes {
			rows = append(rows, Policy{IP: ip, Mode: "smart"})
		}
		first, err := (State{notes: notes, policies: rows}).Revision()
		if err != nil {
			t.Fatal(err)
		}
		second, err := (State{notes: again, policies: rows}).Revision()
		if err != nil || first != second {
			t.Fatal("name revision changed after round trip", err)
		}
	})
}
