package repo

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

// HR O18 T1b: a stored Agent without a port shows port null in profile
// bindings, as Python's agent.get('port') did (T1b made it 0); a stored port
// is a number.
func TestReviewO18T1bBindingMissingPortIsNull(t *testing.T) {
	tests := []struct {
		name  string
		agent model.Agent
		want  string
	}{
		{"missing", model.Agent{ID: "a"}, `{"id":"a","name":"a","host":"","port":null,"service_type":"mihomo"}`},
		{"stored", model.Agent{ID: "a", Name: "A", Host: "192.0.2.1", Port: 8080, ServiceType: "oxidns"}, `{"id":"a","name":"A","host":"192.0.2.1","port":8080,"service_type":"oxidns"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binding, err := AgentBindingRecord(tt.agent)
			if err != nil {
				t.Fatal(err)
			}
			got, err := json.Marshal(binding)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("binding %s, want %s", got, tt.want)
			}
		})
	}
}
