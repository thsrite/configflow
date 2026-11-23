package jsondecode_test

import (
	"testing"

	"configflow/internal/jsondecode"
)

func TestMemberRecordsPresenceAndType(t *testing.T) {
	type request struct {
		Name  jsondecode.Member[string]   `json:"name"`
		IDs   jsondecode.Member[[]string] `json:"ids"`
		Other jsondecode.Member[bool]     `json:"Other"`
	}
	tests := []struct {
		body              string
		present, null, ok bool
		value, or         string
	}{
		{`{}`, false, true, false, "", "fallback"},
		{`{"name":null}`, true, true, false, "", ""},
		{`{"name":"x"}`, true, false, true, "x", "x"},
		{`{"name":5}`, true, false, false, "", ""},
		{`{"NAME":"x"}`, false, true, false, "", "fallback"},
	}
	for _, tc := range tests {
		t.Run(tc.body, func(t *testing.T) {
			var got request
			if err := jsondecode.Unmarshal([]byte(tc.body), &got); err != nil {
				t.Fatal(err)
			}
			m := got.Name
			if m.Present() != tc.present || m.Null() != tc.null || m.OK != tc.ok || m.Value != tc.value || m.Or("fallback") != tc.or {
				t.Fatalf("member %+v", m)
			}
		})
	}
	var got request
	if err := jsondecode.Unmarshal([]byte(`{"ids":["a",1],"Other":true}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.IDs.OK || string(got.IDs.Raw) != `["a",1]` || !got.Other.OK || !got.Other.Value {
		t.Fatalf("members %+v", got)
	}
}
