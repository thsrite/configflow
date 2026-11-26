package profiles

import (
	"encoding/json"
	"testing"

	"configflow/internal/httpx/httpxtest"
)

// O18 T1a (Q2-A): saving profile members through the profile import route
// decodes them strictly. A member of the wrong type is refused with the
// message Python's validation gave for its shape, and nothing is saved; it is
// no longer read leniently as its zero value and stored.
func TestProfileImportRefusesWrongTypedMembers(t *testing.T) {
	tests := []struct {
		name, body, message string
	}{
		{"engine settings", `{"mosdns":5}`, "mosdns 参数必须是对象"},
		{"routers", `{"routers":[]}`, "routers 参数必须是包含 items 列表的对象"},
		{"null object", `{"surge":null}`, "surge 参数必须是对象"},
		{"wrapped config", `{"config":{"loon":"text"}}`, "loon 参数必须是对象"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := profileApp(t)
			before := state(t, s)
			got := s.Post("/api/profiles/default/import", httpxtest.JSON(json.RawMessage(tt.body))).WantStatus(400)
			if message := got.Object().GetString("message"); message != tt.message {
				t.Fatalf("message %q, want %q; body %s", message, tt.message, got.Body)
			}
			if state(t, s) != before {
				t.Fatal("refused import changed the configuration")
			}
		})
	}
}
