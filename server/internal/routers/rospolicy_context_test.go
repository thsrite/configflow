package routers

import (
	"configflow/internal/routers/rospolicy"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestROSPolicyDesiredPassesContextToDefaultContentLoader(t *testing.T) {
	type contextKey struct{}
	ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), contextKey{}, "request-scope"), time.Minute)
	defer cancel()
	previous := spec.RulesetContent
	t.Cleanup(func() { spec.RulesetContent = previous })
	calls := 0
	spec.RulesetContent = func(received context.Context, _ json.RawMessage, item RuleSet, _ *LibraryRule) (string, error) {
		calls++
		if received != ctx || received.Value(contextKey{}) != "request-scope" {
			t.Fatal("policy inspection lost the caller's context")
		}
		deadline, ok := received.Deadline()
		want, _ := ctx.Deadline()
		if !ok || deadline != want {
			t.Fatal("policy inspection lost the caller's deadline")
		}
		if item.ID != "selected" {
			t.Fatal("unexpected ruleset", item.ID)
		}
		return "IP-CIDR,198.51.100.0/24,no-resolve\n", nil
	}
	config := json.RawMessage(`{"rule_configs":[{"id":"selected","itemType":"ruleset","name":"Synthetic","behavior":"classical","policy":"PROXY","enabled":true}]}`)
	router := &Router{RouterSettings: RouterSettings{ID: "synthetic-router", RosPolicyRuleSets: []string{"selected"}}}
	out, err := rospolicy.InspectROSPolicyDesired(ctx, config, router, &Desired{ProxyGateway: "192.0.2.1"}, nil)
	if err != nil || out == nil || calls != 1 {
		t.Fatalf("context-aware inspection: %+v, calls=%d, err=%v", out, calls, err)
	}
}
