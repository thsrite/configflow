package agents_test

import (
	"context"
	"testing"

	"configflow/internal/jsonx"
)

// TestReviewN810StoredMemberDefaults checks Python's agent.get(key, default):
// only a missing member takes the default; a stored null does not.
func TestReviewN810StoredMemberDefaults(t *testing.T) {
	e := newTrafficEnv(t, false)
	_, err := e.s.Repo.UpdateSystemTransaction(e.s.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", []any{
			jsonx.Obj("id", "legacy", "name", "Legacy", "profile_id", "default", "token", "synthetic-legacy-token", "host", "192.0.2.10", "port", 8080),
			jsonx.Obj("id", "nulls", "name", "Nulls", "service_type", nil, "status", nil, "profile_id", "default", "token", "synthetic-null-token", "host", "192.0.2.11", "port", 8080),
			jsonx.Obj("id", "dns", "name", "DNS", "service_type", "oxidns", "status", "online", "profile_id", "default", "token", "synthetic-dns-token", "host", "192.0.2.12", "port", 8080),
		})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	unsupported := jsonx.Obj("success", false, "message", "流量记录只支持 mihomo Agent")
	tests := []struct {
		name, path string
		status     int
		want       any
	}{
		{"missing status defaults", "/api/agents/legacy/status", 200, jsonx.Obj("success", true, "status", "unknown")},
		{"null status stays null", "/api/agents/nulls/status", 200, jsonx.Obj("success", true, "status", nil)},
		{"stored status", "/api/agents/dns/status", 200, jsonx.Obj("success", true, "status", "online")},
		{"missing service type is mihomo", "/api/agents/legacy/traffic-history", 200, nil},
		{"null service type is not mihomo", "/api/agents/nulls/traffic-history", 400, unsupported},
		{"oxidns is not mihomo", "/api/agents/dns/traffic-history", 400, unsupported},
	}
	// The server helper reports through the parent t, so the cases run inline.
	for _, tt := range tests {
		t.Log(tt.name)
		result := e.s.Get(tt.path).WantStatus(tt.status)
		if tt.want != nil {
			result.WantJSON(tt.want)
		}
	}
}
