package upgrades

import (
	"context"
	"errors"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"configflow/internal/model"
)

// TestReviewN810NullAgentVersionStillStartsUpgrade: registration stores an
// explicit null version as sent; Python started the update with previous
// version None. It is not damaged operation evidence, so Start must not
// demand an offline repair. A present non-null non-string stays refused.
func TestReviewN810NullAgentVersionStillStartsUpgrade(t *testing.T) {
	tests := []struct {
		raw     string
		corrupt bool
	}{
		{`null`, false},
		{`1.5`, true},
		{`["1.0"]`, true},
	}
	for _, test := range tests {
		t.Run(test.raw, func(t *testing.T) {
			e := newUpgradeEnv(t)
			if err := e.m.EditAgents(e.ctx, "", func(_ context.Context, rows *[]model.Agent) error {
				return (*rows)[0].SetJSON("version", []byte(test.raw))
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(SetRequestForTest(func(_ context.Context, _ *model.Agent, method, _ string, _ []byte) (*agents.Response, error) {
				if method == "GET" {
					return AgentResponse(nil, 404), nil
				}
				return AgentResponse(jsonx.Obj(), 202), nil
			}))
			state, err := StartModel(e.ctx, e.m, e.ident)
			var corrupt *CorruptStateError
			if test.corrupt {
				if !errors.As(err, &corrupt) || corrupt.Field != "version" {
					t.Fatal("non-string version accepted", state, err)
				}
				return
			}
			if err != nil || state == nil || state.Status != "queued" || state.PreviousVersion != "" {
				t.Fatal("null registration version blocked update", state, err)
			}
		})
	}
}

// TestReviewN810CorruptFollowWarningInterval: the refused-follow warning
// repeats once the interval has passed, separately per damaged field.
func TestReviewN810CorruptFollowWarningInterval(t *testing.T) {
	ResetAddressState()
	t.Cleanup(ResetAddressState)
	a := &model.Agent{}
	start := time.Unix(1_800_000_000, 0)
	logged := func(field string, at time.Time) bool {
		t.Helper()
		key := corruptKey{"agent-1", field}
		previous, seen := corruptWarned[key]
		warnCorruptFollow(a, "agent-1", "192.0.2.77", &CorruptStateError{Field: field}, at)
		return !seen || !corruptWarned[key].Equal(previous)
	}
	tests := []struct {
		name  string
		field string
		at    time.Time
		want  bool
	}{
		{"first", "oxidns_ha.enabled", start, true},
		{"within interval", "oxidns_ha.enabled", start.Add(CorruptWarnInterval - time.Second), false},
		{"other field", agents.HAField + ".status", start.Add(time.Second), true},
		{"after interval", "oxidns_ha.enabled", start.Add(CorruptWarnInterval), true},
	}
	for _, test := range tests {
		if got := logged(test.field, test.at); got != test.want {
			t.Fatalf("%s: warned=%v, want %v", test.name, got, test.want)
		}
	}
}
