package config

import (
	"encoding/json"
	"testing"

	"configflow/internal/httpx/httpxtest"
)

// O18 T1a (Q2-A): a whole-configuration import with a member of the wrong
// type that Python did not validate is refused with "<member> 类型不正确";
// it is no longer read leniently as its zero value and saved.
func TestImportRefusesWrongTypedMembers(t *testing.T) {
	tests := []struct {
		name, section, member, value, message string
	}{
		{"profile description", "profile", "description", `5`, "description 类型不正确"},
		{"profile created_at", "profile", "created_at", `[]`, "created_at 类型不正确"},
		{"system registration sessions", "system", "agent_registration_sessions", `{}`, "agent_registration_sessions 类型不正确"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := exportApp(t)
			raw, err := s.Repo.ExportAllJSON(s.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]json.RawMessage
			if err := json.Unmarshal(raw, &document); err != nil {
				t.Fatal(err)
			}
			// edit sets one member of a JSON object member of document.
			edit := func(object json.RawMessage, member, value string) json.RawMessage {
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(object, &fields); err != nil {
					t.Fatal(err)
				}
				fields[member] = json.RawMessage(value)
				data, err := json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				return data
			}
			if tt.section == "profile" {
				var profiles map[string]json.RawMessage
				if err := json.Unmarshal(document["profiles"], &profiles); err != nil {
					t.Fatal(err)
				}
				profiles["default"] = edit(profiles["default"], tt.member, tt.value)
				data, err := json.Marshal(profiles)
				if err != nil {
					t.Fatal(err)
				}
				document["profiles"] = data
			} else {
				document["system"] = edit(document["system"], tt.member, tt.value)
			}
			before, err := s.Repo.ExportAllJSON(s.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			got := s.Post("/api/config/import", httpxtest.JSON(document)).WantStatus(400)
			if message := got.Object().GetString("message"); message != tt.message {
				t.Fatalf("message %q, want %q; body %s", message, tt.message, got.Body)
			}
			after, err := s.Repo.ExportAllJSON(s.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("refused import changed the configuration")
			}
		})
	}
}
