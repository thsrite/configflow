package repo

import (
	"encoding/json"
	"reflect"
	"testing"

	"configflow/internal/model"
)

// TestAgentBindingsTypedMatchesEncoded compares the member-wise binding read
// with the encoded-registry read it replaced, for every member shape the
// projection distinguishes.
func TestAgentBindingsTypedMatchesEncoded(t *testing.T) {
	tests := []struct {
		name   string
		agents string
	}{
		{"plain", `[{"id":"a","name":"A","host":"192.0.2.1","port":8080,"service_type":"oxidns","profile_id":"p1"}]`},
		{"defaults", `[{"id":"b"},{"id":"c","name":"","service_type":"","profile_id":""},{"id":"d","name":null,"profile_id":null}]`},
		{"odd types", `[{"id":7,"name":0,"host":null,"port":"9090","service_type":false,"profile_id":"p2"},{"id":"e","profile_id":5},{"id":"f","profile_id":true}]`},
		{"unknown and history", `[{"id":"g","future":{"x":1},"deployments":{"one":{"status":"failed"}},"profile_id":"p1"},{"id":"h","port":8080.0,"profile_id":"p1"}]`},
		{"empty", `[]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var system model.System
			if err := system.SetJSON("agents", json.RawMessage(tt.agents)); err != nil {
				t.Fatal(err)
			}
			id := "p1"
			system.SystemConfig.DefaultProfileID = &id
			document := &model.Document{System: system}
			typed, err := agentBindingRecords(document)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := encodedAgentBindingRecords(document)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(typed, encoded) {
				t.Fatalf("typed %s\nencoded %s", mustJSON(t, typed), mustJSON(t, encoded))
			}
		})
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
