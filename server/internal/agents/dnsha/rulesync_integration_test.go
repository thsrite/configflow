package dnsha

import (
	"context"
	"testing"

	"configflow/internal/agents/rulesync"
)

func TestBothRuleWorkersAdvanceExactlyOnePair(t *testing.T) {
	f := fixtureNew(t, Options{}, true, nil)
	f.publish(t, "primary", "rule-worker-pair", true)
	calls := 0
	sync := rulesync.New(f.m, nil, rulesync.Options{
		Pair: rulesync.PairHooks{
			Advance: func(ctx context.Context, id string) {
				calls++
				_, _ = f.s.Advance(ctx, id)
			},
		},
	})
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
