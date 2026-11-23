package routers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

func wireRuleSync() {
	CaptureManual = func(ctx context.Context, profile string, config json.RawMessage, r *Router, d Driver, desired *Desired) error {
		service, err := newSyncService(ctx)
		if err != nil {
			return err
		}
		return service.Capture(ctx, profile, config, r, d, desired)
	}
	tasks.SetRowBuilder(tasks.SlotRouters, ruleSyncTasks)
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
	lifecycle.RegisterPeriodic(lifecycle.Periodic{Name: "router-rule-sync", Start: lifecycle.AtBoot, FirstDelay: 30 * time.Second, Interval: ruleSyncPause, Wake: &ruleSyncLoop.wake, Round: func(ctx context.Context) error {
		service, err := newSyncService(ctx)
		if err != nil {
			return err
		}
		return service.RunRound(ctx)
	}})
}
func ruleSyncTasks(ctx context.Context, statuses taskstatus.Snapshot) ([]*tasks.Task, error) {
	db, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	profiles, err := routerProfiles(ctx, db)
	if err != nil {
		return nil, err
	}
	rows := []*tasks.Task{}
	store := RuleSyncStore{DataDir: db.DataDir()}
	for _, profile := range profiles {
		for _, r := range profile.Routers {
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
			history, err := tasks.ProjectHistory(statuses.Entry(key))
			if err != nil {
				return nil, err
			}
			if receipt.CheckedAt != nil && *receipt.CheckedAt != "" {
				history.LastRunAt = new(*receipt.CheckedAt)
			}
			switch status {
			case "synced":
				history.LastStatus = new("success")
			case "failed", "paused":
				history.LastStatus = new("failed")
			case "running", "queued":
			default:
				history.LastStatus = nil
			}
			history.LastError = new("")
			if status == "failed" || status == "paused" {
				history.LastError = new(receipt.Message)
			}
			var due *time.Time
			if r.AutoRuleSync && ready && status != "paused" && receipt.CheckedAt != nil {
				if checked, ok := taskstatus.ParseTimestamp(*receipt.CheckedAt); ok {
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
			row := &tasks.Task{Key: key, Category: tasks.SlotRouters, Name: orText(r.Name, r.ID), ResourceID: r.ID, ProfileID: new(profile.ID), ProfileName: new(orText(profile.Name, profile.ID)), Enabled: r.AutoRuleSync, IntervalSeconds: mustPolicyJSON(interval), Editable: true, CanRun: hint == "", RunHint: hint, Running: status == "running" || status == "queued", StateMessage: message, Link: new("/routers"), HistoryData: history}
			row.NextRunAt = tasks.Timestamp(due)
			rows = append(rows, row)
		}
	}
	return rows, nil
}
