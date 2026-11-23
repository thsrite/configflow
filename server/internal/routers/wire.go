package routers

import (
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
)

// Wire installs native notifications and the shared publication lock.
func Wire() {
	repo.RegisterHooks(repo.Hooks{RecordHooks: repo.RecordHooks{RedactRoutersJSON: func(raw json.RawMessage) (json.RawMessage, error) { return spec.RedactRouters(raw) }, JobLockRecord: jobLockHook}})
	notifications.SetInputs(notifications.Inputs{Summarize: pendingSummary})
}

func pendingSummary(report *notifications.RouterReport, plan json.RawMessage) (*notifications.RouterSummary, error) {
	raw, err := report.JSON()
	if err != nil {
		return nil, err
	}
	summary, err := SummarizeJSON(raw, plan)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		return nil, err
	}
	var result notifications.RouterSummary
	if err = json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
func recordPendingNotice(ctx context.Context, db *repo.Repository, profileID string, config json.RawMessage, r *Router, report *Report, plan *Plan) {
	work := lifecycle.Detach(repo.WithRepository(ctx, db))
	if work.Err() != nil {
		return
	}
	notifications.Safely(func() error {
		raw, err := spec.ReleaseRouterRecord(r)
		if err != nil {
			return err
		}
		var router model.Router
		if err = json.Unmarshal(raw, &router); err != nil {
			return err
		}
		raw, err = json.Marshal(report)
		if err != nil {
			return err
		}
		var native notifications.RouterReport
		if err = json.Unmarshal(raw, &native); err != nil {
			return err
		}
		raw, err = json.Marshal(plan)
		if err != nil {
			return err
		}
		if err = work.Err(); err != nil {
			return err
		}
		return notifications.SaveRouter(work, profileID, config, &router, &native, raw, "")
	})
}
