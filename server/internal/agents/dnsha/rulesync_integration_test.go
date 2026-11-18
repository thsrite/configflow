package dnsha

import (
	"context"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/rulesync"
)

func TestBothRuleWorkersAdvanceExactlyOnePair(t *testing.T) {
	f := fixtureNew(t, Options{}, true, nil)
	f.publish(t, "primary", "rule-worker-pair", true)
	calls := 0
	restore := rulesync.SetPairAdvance(func(ctx context.Context, m *agents.Manager, id string) {
		calls++
		_, _ = ForManager(m).Advance(ctx, id)
	})
	defer restore()
	sync := rulesync.New(f.m, nil, rulesync.Options{})
	for range 12 {
		for _, id := range []string{"secondary", "primary"} {
			if e := sync.Run(f.ctx, id); e != nil {
				t.Fatal(e)
			}
		}
		a, e := f.s.get(f.ctx, "primary")
		if e != nil {
			t.Fatal(e)
		}
		if a.OxiDNSHAPublication.Status == "succeeded" {
			if calls == 0 || len(f.uploads()) != 2 {
				t.Fatal("rule workers duplicated pair publication", calls, len(f.uploads()))
			}
			return
		}
	}
	t.Fatal("rule workers did not resume pair")
}
