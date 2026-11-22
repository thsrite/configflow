package rulesync

import (
	"context"
	"errors"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/model"
)

// pairRefusal stands in for the HA owner's user refusal (dnsha.Error).
type pairRefusal struct{ message string }

func (e *pairRefusal) Error() string { return e.message }

// Lab L3-D3: an automatic sync of an HA member that the pair owner refuses
// states the owner's reason instead of the generic failure.
func TestPairRefusalReasonReachesSyncState(t *testing.T) {
	const reason = "主备配置基线不同，请先完整发布一次主备配置，再自动同步规则"
	restore := SetPairFailureMessage(func(_ context.Context, _ *agents.Manager, e error) (string, bool) {
		var refusal *pairRefusal
		if errors.As(e, &refusal) {
			return refusal.message, true
		}
		return "", false
	})
	defer restore()
	tests := []struct {
		name  string
		cause error
		want  string
	}{
		{"pair-refusal", &pairRefusal{reason}, reason},
		{"other-error", errors.New("synthetic internal failure"), msgFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newWorkerFixture(t, "mihomo")
			if _, e := f.s.Configure(f.ctx, "device", []byte(`{"library":false,"rules":true}`)); e != nil {
				t.Fatal(e)
			}
			f.s.prepare = func(context.Context, deploy.Target, *deploy.Source, Baseline, Settings, string, bool) (*deploy.Bundle, error) {
				return nil, tt.cause
			}
			_, e := f.s.m.Repository().UpdateProfile(f.ctx, "default", func(_ context.Context, p *model.Profile) error {
				for i := range p.RuleConfigs {
					if p.RuleConfigs[i].ID == "single" {
						p.RuleConfigs[i].Value = new("new-inline.example.com")
					}
				}
				return nil
			})
			if e != nil {
				t.Fatal(e)
			}
			for range 4 {
				// Run reports the cause after recording the failure state.
				if e = f.s.Run(f.ctx, "device"); e != nil && !errors.Is(e, tt.cause) {
					t.Fatal(e)
				}
				if f.state(t).Status == "failed" {
					break
				}
				f.now = f.now.Add(PollInterval + time.Second)
			}
			if state := f.state(t); state.Status != "failed" || state.Message != tt.want {
				t.Fatalf("state %s %q, want failed %q", state.Status, state.Message, tt.want)
			}
		})
	}
}
