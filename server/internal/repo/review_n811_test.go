package repo

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"configflow/internal/model"
)

// reviewN811System builds a system section whose single Agent carries a
// deployment history of the given number of entries (synthetic values only).
func reviewN811System(t *testing.T, deployments int) *model.System {
	t.Helper()
	history := make([]string, 0, deployments)
	payload := strings.Repeat("x", 1024)
	for i := range deployments {
		history = append(history, fmt.Sprintf(`"d%d":{"id":"d%d","status":"success","config":%q}`, i, i, payload))
	}
	raw := `{"system_config":{"rule_proxy_token":"synthetic-rule-token","retired_rule_proxy_tokens":["synthetic-retired"]},` +
		`"backup":{},"agents":[{"id":"agent-1","name":"Agent","token":"synthetic-agent-token","deployments":{` +
		strings.Join(history, ",") + `}}],"_revision":1}`
	var system model.System
	if err := json.Unmarshal([]byte(raw), &system); err != nil {
		t.Fatal(err)
	}
	return &system
}

func reviewN811TokenBytes(system *model.System) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range 20 {
		sanitizationTokensModel(system)
	}
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / 20
}

// TestReviewN811SanitizationTokensSkipAgentHistory guards the hot redaction
// path: collecting tokens reads single members and never encodes an Agent's
// deployment history (its cost must not grow with the history size).
func TestReviewN811SanitizationTokensSkipAgentHistory(t *testing.T) {
	small, large := reviewN811System(t, 1), reviewN811System(t, 2000)
	ruleProxy, agents := sanitizationTokensModel(large)
	for _, token := range []string{"synthetic-rule-token", "synthetic-retired"} {
		if !ruleProxy.Has(token) {
			t.Fatalf("rule proxy token %q missing", token)
		}
	}
	if !agents.Has("synthetic-agent-token") || len(agents) != 1 {
		t.Fatalf("agent tokens = %v", agents)
	}
	smallBytes, largeBytes := reviewN811TokenBytes(small), reviewN811TokenBytes(large)
	// The history is ~2 MB; encoding it even once per call would exceed this bound.
	if largeBytes > smallBytes+64*1024 {
		t.Fatalf("token collection allocates %d bytes with a large history (%d with a small one)", largeBytes, smallBytes)
	}
}
