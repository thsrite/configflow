package routersplit

import (
	"encoding/json"
	"os"
	"testing"

	"configflow/internal/model"
	"configflow/internal/testjson"
)

func n89SessionPolicy(t *testing.T, input json.RawMessage, old string, actual *Context) {
	t.Helper()
	raw, err := os.ReadFile("testdata/n89_session_expectation.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected struct {
		Input    json.RawMessage `json:"input"`
		Original json.RawMessage `json:"original"`
		Native   json.RawMessage `json:"native"`
	}
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	if !testjson.Equal(expected.Input, input) {
		t.Fatalf("independent session input changed: %s", testjson.Diff(expected.Input, input))
	}
	if !testjson.Equal(expected.Original, json.RawMessage(old)) {
		t.Fatalf("original Python session answer changed: %s", testjson.Diff(expected.Original, json.RawMessage(old)))
	}
	got := struct {
		SessionGoal       *string       `json:"session_goal"`
		SessionID         *model.String `json:"session_id"`
		SessionCompleted  bool          `json:"session_completed"`
		HasGatewaySession bool          `json:"has_gateway_session"`
		MihomoAgentIDs    []string      `json:"mihomo_agent_ids"`
	}{actual.SessionGoal, actual.SessionID, actual.SessionCompleted, actual.HasGatewaySession, actual.MihomoAgentIDs}
	if !testjson.Equal(expected.Native, got) {
		t.Fatalf("R5 completion policy differs: %s", testjson.Diff(expected.Native, got))
	}
}
