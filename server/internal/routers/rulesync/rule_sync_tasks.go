package rulesync

import (
	"configflow/internal/routers"
	"configflow/internal/routers/device"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

// CaptureManual returns a routers.ManualBaselineCapturer bound to s.
func (s *RuleSyncService) CaptureManual(ctx context.Context, profile string, config json.RawMessage, r *spec.Router, d routers.Driver, desired *spec.Desired) error {
	if s == nil || s.Repo == nil {
		return errors.New("routers/rulesync: repository is nil")
	}
	return s.Capture(ctx, profile, config, r, d, desired)
}

// Loop returns the periodic loop specification registered by server assembly on
// the application Runtime.
func (s *RuleSyncService) Loop() lifecycle.Periodic {
	return lifecycle.Periodic{
		Name:       "router-rule-sync",
		Start:      lifecycle.AtBoot,
		FirstDelay: 30 * time.Second,
		Interval:   s.pauseDuration,
		Wake:       &s.pacer.wake,
		OnStart:    func(context.Context) { s.pacer.wake.Clear(); s.pacer.pause.Store(0) },
		Round: func(ctx context.Context) error {
			if s == nil || s.Repo == nil {
				return errors.New("routers/rulesync: repository is nil")
			}
			return s.RunRound(ctx)
		},
	}
}

// TaskRunner returns the tasks.RunFunc for tasks.SlotRouters registered by
// server assembly on the application tasks.Registry.
func (s *RuleSyncService) TaskRunner() tasks.RunFunc {
	return func(ctx context.Context, _ string, resource string) error {
		profile, id, _ := strings.Cut(resource, ":")
		if s == nil || s.Repo == nil || s.Agents == nil {
			return errors.New("routers/rulesync: service dependencies are nil")
		}
		err := s.StartJob(ctx, profile, id)
		var re *device.RouterError
		if errors.As(err, &re) {
			return &tasks.Conflict{Message: re.Error()}
		}
		return err
	}
}

// TaskRows builds the scheduled-task rows for tasks.SlotRouters.
func (s *RuleSyncService) TaskRows(ctx context.Context, statuses taskstatus.Snapshot) ([]*tasks.Task, error) {
	if s == nil || s.Repo == nil {
		return nil, errors.New("routers/rulesync: repository is nil")
	}
	db := s.Repo
	profiles, err := spec.RouterProfiles(ctx, db)
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
			status := device.OrText(receipt.Status, "waiting")
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
			restored := r.StateRestoredAt() != ""
			var due *time.Time
			if r.AutoRuleSync && ready && !restored && status != "paused" && receipt.CheckedAt != nil {
				if checked, ok := taskstatus.ParseTimestamp(*receipt.CheckedAt); ok {
					next := checked.Add(time.Duration(interval) * time.Second)
					due = &next
				}
			}
			hint, message := "", ""
			switch {
			case !r.AutoRuleSync:
				hint = messageSyncDisabledHint
			case restored:
				hint = messageSyncRestored
			case !ready:
				hint = messageSyncFirstHint
			case status == "running" || status == "queued" || status == "paused":
				hint = messageSyncPendingHint
			}
			if restored {
				message = messageSyncRestored
			} else if !ready {
				message = messageSyncWaitingState
			} else if status == "paused" {
				message = messageSyncPausedState
			}
			row := &tasks.Task{Key: key, Category: tasks.SlotRouters, Name: device.OrText(r.Name, r.ID), ResourceID: r.ID, ProfileID: new(profile.ID), ProfileName: new(device.OrText(profile.Name, profile.ID)), Enabled: r.AutoRuleSync, IntervalSeconds: device.JSON(interval), Editable: true, CanRun: hint == "", RunHint: hint, Running: status == "running" || status == "queued", StateMessage: message, Link: new("/routers"), HistoryData: history}
			row.NextRunAt = tasks.Timestamp(due)
			rows = append(rows, row)
		}
	}
	return rows, nil
}
