package routers

import (
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	"context"
	"encoding/json"
)

// Wire registers the hooks this package owns (listed in internal/server
// wirePackages): desensitized router exports and pending notification summaries.
// Later modules add the job lock, the rule-sync task slot and loop here.
func Wire() {
	repo.RegisterHooks(repo.Hooks{RedactRouters: redactHook})
	notifications.SetSources(notifications.Sources{Summarize: pendingSummaryHook})
}

// redactHook adapts RedactRouters to repo's jsonx document (until repo's S pass).
func redactHook(routers any) any {
	if document, err := jsonx.Compact(routers); err == nil {
		if redacted, err := RedactRouters(document); err == nil {
			if value, err := jsonx.Parse(redacted); err == nil {
				return value
			}
		}
	}
	return routers
}

// pendingSummaryHook is the existing C07 legacy callback boundary. R13's
// summary business logic uses native projections; N8-7 owns its later migration.
func pendingSummaryHook(report, plan *jsonx.Object) *jsonx.Object {
	r, err := json.Marshal(report)
	if err != nil {
		return pendingUnknownHook()
	}
	p, err := json.Marshal(plan)
	if err != nil {
		return pendingUnknownHook()
	}
	summary, err := SummarizeJSON(r, p)
	if err != nil {
		return pendingUnknownHook()
	}
	b, err := json.Marshal(summary)
	if err != nil {
		return pendingUnknownHook()
	}
	var result jsonx.Object
	if err = json.Unmarshal(b, &result); err != nil {
		return pendingUnknownHook()
	}
	return &result
}
func pendingUnknownHook() *jsonx.Object {
	return jsonx.Obj("state", "unknown", "pending", 0, "items", []any{}, "message", messagePendingMalformed)
}
func recordPendingNotice(ctx context.Context, db *repo.Repository, profileID string, config json.RawMessage, r *Router, report *Report, plan *Plan) {
	work := lifecycle.Detach(repo.WithRepository(ctx, db))
	if work.Err() != nil {
		return
	}
	notifications.Safely(func() error {
		router, err := releaseRouterRecord(r)
		if err != nil {
			return err
		}
		reportData, err := json.Marshal(report)
		if err != nil {
			return err
		}
		planData, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		values := []json.RawMessage{config, router, reportData, planData}
		records := make([]*jsonx.Object, len(values))
		for i, data := range values {
			records[i] = jsonx.NewObject()
			if err = json.Unmarshal(data, records[i]); err != nil {
				return err
			}
		}
		if err = work.Err(); err != nil {
			return err
		}
		// C07 batch-1 RecordRouter calls TransactionContext. Do not duplicate its
		// leaf cancellation guard here or introduce a parallel JSON writer API.
		return notifications.RecordRouter(work, profileID, records[0], records[1], records[2], records[3], "")
	})
}
