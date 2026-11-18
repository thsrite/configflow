package routers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

func wireRuleSync() {
	// O3-6b has not landed in this base. This is the one transitional lock shared
	// by sync, R15 callers of JobLock, and the repository's commit guard.
	repo.RegisterHooks(repo.Hooks{JobLock: func(ctx context.Context, profile, id string, raw *jsonx.Object) (func(), bool) {
		var r *Router
		if raw != nil {
			var v Router
			if json.Unmarshal(mustPolicyJSON(raw), &v) != nil {
				return nil, false
			}
			r = &v
		}
		_, release, ok := JobLock(ctx, profile, id, r)
		return release, ok
	}})
	CaptureManual = func(ctx context.Context, profile string, config json.RawMessage, r *Router, d Driver, desired *Desired) error {
		service, err := newSyncService(ctx)
		if err != nil {
			return err
		}
		return service.Capture(ctx, profile, config, r, d, desired)
	}
	tasks.SetRows(tasks.SlotRouters, ruleSyncRows)
	tasks.SetRunner(tasks.SlotRouters, func(ctx context.Context, _ string, resource string) error {
		profile, id, _ := strings.Cut(resource, ":")
		service, err := newSyncService(lifecycle.Detach(ctx))
		if err != nil {
			return err
		}
		err = service.StartJob(ctx, profile, id)
		var re *RouterError
		if errors.As(err, &re) {
			return &tasks.Conflict{Message: re.Error()}
		}
		return err
	})
	lifecycle.RegisterPeriodic(lifecycle.Periodic{Name: "router-rule-sync", Start: lifecycle.AtBoot, FirstDelay: 30 * time.Second, Interval: func(context.Context) time.Duration { return 30 * time.Second }, Round: func(ctx context.Context) error {
		service, err := newSyncService(ctx)
		if err != nil {
			return err
		}
		return service.RunRound(ctx)
	}})
}
func ruleSyncRows(ctx context.Context, statuses *jsonx.Object) ([]*jsonx.Object, error) {
	db, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	profiles, err := db.ListProfiles(ctx)
	if err != nil {
		return nil, err
	}
	rows := []*jsonx.Object{}
	store := RuleSyncStore{DataDir: db.DataDir()}
	for _, value := range profiles {
		var profile pendingProfile
		if err = json.Unmarshal(mustPolicyJSON(value), &profile); err != nil {
			return nil, err
		}
		config, err := db.GetProfile(ctx, profile.ID)
		if err != nil {
			return nil, err
		}
		for _, r := range routersOf(mustPolicyJSON(config)) {
			if r.Type != "routeros" {
				continue
			}
			key := syncTaskKey(profile.ID, r.ID)
			receipt, err := store.Read(ctx, profile.ID, r.ID)
			if err != nil {
				return nil, err
			}
			status := orText(receipt.Status, "waiting")
			ready := receipt.Baseline != nil
			interval := r.RuleSyncInterval
			if interval == 0 {
				interval = 60
			}
			interval *= 60
			history, err := tasks.History(statuses.GetObject(key))
			if err != nil {
				return nil, err
			}
			if receipt.CheckedAt != nil && *receipt.CheckedAt != "" {
				history.Set("last_run_at", *receipt.CheckedAt)
			}
			switch status {
			case "synced":
				history.Set("last_status", "success")
			case "failed", "paused":
				history.Set("last_status", "failed")
			case "running", "queued":
			default:
				history.Set("last_status", nil)
			}
			history.Set("last_error", "")
			if status == "failed" || status == "paused" {
				history.Set("last_error", receipt.Message)
			}
			var due *time.Time
			if r.AutoRuleSync && ready && status != "paused" && receipt.CheckedAt != nil {
				if checked, ok := taskstatus.ParseTime(*receipt.CheckedAt); ok {
					next := checked.Add(time.Duration(interval) * time.Second)
					due = &next
				}
			}
			hint, message := "", ""
			switch {
			case !r.AutoRuleSync:
				hint = messageSyncDisabledHint
			case !ready:
				hint = messageSyncFirstHint
			case status == "running" || status == "queued" || status == "paused":
				hint = messageSyncPendingHint
			}
			if !ready {
				message = messageSyncWaitingState
			} else if status == "paused" {
				message = messageSyncPausedState
			}
			fields := []any{"resource_id", r.ID, "profile_id", profile.ID, "profile_name", orText(profile.Name, profile.ID), "enabled", r.AutoRuleSync, "interval_seconds", interval, "editable", true, "can_run", hint == "", "run_hint", hint, "running", status == "running" || status == "queued", "next_run_at", tasks.ISO(due), "state_message", message, "link", "/routers"}
			fields = append(fields, tasks.Spread(history)...)
			rows = append(rows, tasks.Row(key, tasks.SlotRouters, orText(r.Name, r.ID), fields...))
		}
	}
	return rows, nil
}
