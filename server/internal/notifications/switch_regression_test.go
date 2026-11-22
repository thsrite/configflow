package notifications

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
	"configflow/internal/tasks"
)

func TestTaskIncidentAgentEnabledSwitchTruthiness(t *testing.T) {
	cases := []struct {
		name, value string
		wantActive  bool
	}{
		{"absent", "", true},
		{"false", "false", false},
		{"null", "null", false},
		{"zero", "0", false},
		{"zero_float", "0.0", false},
		{"neg_zero", "-0", false},
		{"zero_exp", "0e0", false},
		{"empty_string", `""`, false},
		{"empty_array", `[]`, false},
		{"empty_object", `{}`, false},
		// The Agent's enabled switch is a device-write gate (O18 T1b
		// review): another type than a boolean is off.
		{"one", "1", false},
		{"yes", `"yes"`, false},
		{"true", "true", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"id":"a1","name":"Agent"}`
			if tc.value != "" {
				raw = `{"id":"a1","name":"Agent","enabled":` + tc.value + `}`
			}
			var agent model.Agent
			if err := json.Unmarshal([]byte(raw), &agent); err != nil {
				t.Fatal(err)
			}
			status := "failed"
			errMsg := "sync error"
			item, err := TaskIncident(&tasks.Task{
				Key:        "agent_rule_sync:a1",
				Category:   "agent_rule_sync",
				ResourceID: "a1",
				Enabled:    true,
				HistoryData: &tasks.HistoryData{
					LastStatus: &status,
					LastError:  &errMsg,
				},
			}, map[string]model.Agent{"a1": agent}, nil)
			if err != nil {
				t.Fatal(err)
			}
			wantState := "inactive"
			if tc.wantActive {
				wantState = "pending"
			}
			if item.State != wantState {
				t.Fatalf("TaskIncident(%s).State = %q, want %q", raw, item.State, wantState)
			}
		})
	}
}
