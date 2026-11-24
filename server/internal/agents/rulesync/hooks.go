package rulesync

import (
	"configflow/internal/deploy"
	"configflow/internal/repo"
	"context"
)

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
