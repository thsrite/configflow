package agents

import "testing"

func TestPublicAgentOmitsUnreadAndSecrets(t *testing.T) {
	record := map[string]any{
		"id":              "device",
		"token":           "secret",
		"applied_content": "secret",
		"dns_follow_job":  map[string]any{"private": true},
		"deployments":     testAgentHistoryNotRead,
	}
	public := publicAgent(record)
	if len(public) != 1 || mapStr(public, "id") != "device" {
		t.Fatalf("public: %#v", public)
	}
	if len(record) != 5 {
		t.Fatal("source changed")
	}
}
