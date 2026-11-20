package dnsha

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

// TestReviewN811PairRuleTaskEnvelope pins the oxidns_ha_publish.rules task
// members: a failed task always has code and message (also an empty journal
// message), other states have neither.
func TestReviewN811PairRuleTaskEnvelope(t *testing.T) {
	tests := []struct {
		name, status, phase string
		journal             *model.HAJournal
		want                string
	}{
		{"failed empty message", "failed", "done", &model.HAJournal{ID: "j1"},
			`{"id":"ha-j1","status":"failed","phase":"done","downloaded":0,"download_total":0,"reloaded":0,"reload_total":0,"code":"RULE_UPDATE_FAILED","message":""}`},
		{"failed message", "failed", "done", &model.HAJournal{ID: "j1", Message: "同步失败"},
			`{"id":"ha-j1","status":"failed","phase":"done","downloaded":0,"download_total":0,"reloaded":0,"reload_total":0,"code":"RULE_UPDATE_FAILED","message":"同步失败"}`},
		{"running", "running", "reloading", &model.HAJournal{ID: "j1", Message: "ignored"},
			`{"id":"ha-j1","status":"running","phase":"reloading","downloaded":0,"download_total":0,"reloaded":0,"reload_total":0}`},
		{"idle", "idle", "idle", nil,
			`{"id":"ha-","status":"idle","phase":"idle","downloaded":0,"download_total":0,"reloaded":0,"reload_total":0}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := "ha-"
			if tc.journal != nil {
				id += tc.journal.ID
			}
			result := rulesResult(id, tc.status, tc.phase, tc.journal)
			raw, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var envelope struct {
				Success bool            `json:"success"`
				Task    json.RawMessage `json:"task"`
				HA      json.RawMessage `json:"ha_publication"`
			}
			if err := json.Unmarshal(raw, &envelope); err != nil {
				t.Fatal(err)
			}
			if !envelope.Success || string(envelope.Task) != tc.want {
				t.Fatalf("task = %s, want %s", envelope.Task, tc.want)
			}
			if (tc.journal != nil) != (envelope.HA != nil) {
				t.Fatalf("ha_publication presence = %s", envelope.HA)
			}
		})
	}
}
