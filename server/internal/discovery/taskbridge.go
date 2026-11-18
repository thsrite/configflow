package discovery

import (
	"context"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"encoding/json"
)

func wireTasks() {
	tasks.SetRowBuilder(tasks.SlotDomains, domainRows)
	tasks.SetRunner(tasks.SlotDomains, func(ctx context.Context, key, resource string) error {
		ctx = lifecycle.Detach(ctx)
		c, err := loadConfig(ctx, resource)
		if err != nil {
			return err
		}
		if !c.Settings.AutoProbe {
			return &tasks.Invalid{Message: msgAutoDisabled}
		}
		r, err := repo.GetRepository(ctx)
		if err != nil {
			return err
		}
		worker := repo.WithRepository(ctx, r)
		return tasks.StartBackground(worker, key, func(ctx context.Context) error { _, err := RunAutoTask(ctx, resource, "manual"); return err })
	})
}
func domainRows(ctx context.Context, statuses taskstatus.Snapshot) ([]*tasks.Task, error) {
	profiles, err := listProfiles(ctx)
	if err != nil {
		return nil, err
	}
	out := []*tasks.Task{}
	for _, p := range profiles {
		key := "domain_discovery:" + p.ID
		c, err := loadConfig(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		_, exists := statuses[key]
		if !c.Settings.AutoProbe && !exists {
			continue
		}
		e := statuses.Entry(key)
		var due *time.Time
		if e.LastRunAt != nil {
			if last, ok := taskstatus.ParseTimestamp(*e.LastRunAt); ok && c.Settings.AutoProbe {
				due = ptr(last.Add(600 * time.Second))
			}
		}
		hint := ""
		if !c.Settings.AutoProbe {
			hint = msgAutoDisabled
		}
		name := p.Name
		if name == "" {
			name = p.ID
		}
		history, err := tasks.ProjectHistory(e)
		if err != nil {
			return nil, err
		}
		out = append(out, &tasks.Task{Key: key, Category: tasks.SlotDomains, Name: msgAutoName, ResourceID: p.ID, ProfileID: ptr(p.ID), ProfileName: ptr(name), Enabled: c.Settings.AutoProbe, IntervalSeconds: json.RawMessage("600"), CanRun: c.Settings.AutoProbe, RunHint: hint, Running: tasks.IsBackground(key), NextRunAt: tasks.Timestamp(due), Link: ptr("/domain-discovery"), HistoryData: history})
	}
	return out, nil
}
