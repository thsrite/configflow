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

var pairFailure struct {
	sync.RWMutex
	fn func(context.Context, *agents.Manager, error) (string, bool)
}

// SetPairFailureMessage installs A18's translation of an HA pair refusal into
// the user message shown by the manual rule update, so automatic syncs of an
// HA member state the same reason.
func SetPairFailureMessage(fn func(context.Context, *agents.Manager, error) (string, bool)) (restore func()) {
	pairFailure.Lock()
	previous := pairFailure.fn
	pairFailure.fn = fn
	pairFailure.Unlock()
	return func() { pairFailure.Lock(); pairFailure.fn = previous; pairFailure.Unlock() }
}
func pairFailureMessage(ctx context.Context, m *agents.Manager, cause error) (string, bool) {
	pairFailure.RLock()
	fn := pairFailure.fn
	pairFailure.RUnlock()
	if fn == nil {
		return "", false
	}
	return fn(ctx, m, cause)
}

// PinnedWithheld restores C32's best-effort projection against each eligible
// device's successful receipt, resolving an absent profile to the real default.
func PinnedWithheld(ctx context.Context, r *repo.Repository, receipts *deploy.Receipts, profileID string, ruleIDs []string) (bool, error) {
	if len(ruleIDs) == 0 {
		return false, nil
	}
	if profileID == "" {
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
		if !isOxi(record) || !AgentSettings(record).Any() {
			continue
		}
		baseline, e := receipts.LoadAgent(ctx, record, false)
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
		_, e = ProjectRuleChanges(base, current, AgentSettings(record), "oxidns", held)
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
