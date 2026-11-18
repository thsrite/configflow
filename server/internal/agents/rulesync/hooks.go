package rulesync

import (
	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/repo"
	"context"
	"sync"
)

var pairAdvance struct {
	sync.RWMutex
	fn func(context.Context, *agents.Manager, string)
}

// SetPairAdvance installs A18's active publication reconciliation hook.
func SetPairAdvance(fn func(context.Context, *agents.Manager, string)) (restore func()) {
	pairAdvance.Lock()
	previous := pairAdvance.fn
	pairAdvance.fn = fn
	pairAdvance.Unlock()
	return func() { pairAdvance.Lock(); pairAdvance.fn = previous; pairAdvance.Unlock() }
}
func advancePair(ctx context.Context, m *agents.Manager, id string) error {
	pairAdvance.RLock()
	fn := pairAdvance.fn
	pairAdvance.RUnlock()
	if fn == nil {
		return nil
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	fn(ctx, m, id)
	return ctx.Err()
}

// PinnedWithheld restores C32's best-effort projection against each eligible
// device's successful receipt, resolving an absent profile to the real default.
func PinnedWithheld(ctx context.Context, r *repo.Repository, receipts *deploy.Receipts, profileID string, ruleIDs []string) (bool, error) {
	if len(ruleIDs) == 0 {
		return false, nil
	}
	if profileID == "" || profileID == "default" {
		id, e := r.DefaultProfileID(ctx)
		if e != nil {
			return false, e
		}
		profileID = id
	}
	records, e := profileAgents(ctx, r, profileID)
	if e != nil {
		return false, e
	}
	if receipts == nil {
		receipts = deploy.NewReceipts(r.DataDir())
	}
	ids := map[string]bool{}
	for _, id := range ruleIDs {
		ids[id] = true
	}
	var current *deploy.Source
	for _, record := range records {
		if !isOxi(record) || !SettingsFor(record).Any() {
			continue
		}
		baseline, e := receipts.Load(ctx, record, false)
		if e != nil {
			return false, e
		}
		if baseline == nil {
			continue
		}
		if current == nil {
			current, e = repositorySource(ctx, r, profileID)
			if e != nil {
				return false, e
			}
		}
		base, e := deploy.SourceFromJSON(baseline.Source, nil)
		if e != nil {
			return false, e
		}
		held := map[string]bool{}
		_, e = ProjectRuleChanges(base, current, SettingsFor(record), "oxidns", held)
		if e != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			continue
		}
		for id := range held {
			if ids[id] {
				return true, nil
			}
		}
	}
	return false, nil
}
