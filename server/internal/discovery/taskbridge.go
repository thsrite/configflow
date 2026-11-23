package discovery

import (
	"context"
	"encoding/json"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
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
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	type profileProbe struct {
		id, name  string
		autoProbe bool
	}
	profiles := []profileProbe{}
	if err := r.View(ctx, func(d *model.Document) error {
		for _, p := range d.Profiles {
			profiles = append(profiles, profileProbe{
				id:        p.Key(),
				name:      p.Name,
				autoProbe: p.DomainDiscovery.AutoProbe != nil && *p.DomainDiscovery.AutoProbe,
			})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	out := []*tasks.Task{}
	for _, p := range profiles {
		key := "domain_discovery:" + p.id
		_, exists := statuses[key]
		if !p.autoProbe && !exists {
			continue
		}
		e := statuses.Entry(key)
		var due *time.Time
		if e.LastRunAt != nil {
			if last, ok := taskstatus.ParseTimestamp(*e.LastRunAt); ok && p.autoProbe {
				due = new(last.Add(600 * time.Second))
			}
		}
		hint := ""
		if !p.autoProbe {
			hint = msgAutoDisabled
		}
		name := p.name
		if name == "" {
			name = p.id
		}
		history, err := tasks.ProjectHistory(e)
		if err != nil {
			return nil, err
		}
		out = append(out, &tasks.Task{Key: key, Category: tasks.SlotDomains, Name: msgAutoName, ResourceID: p.id, ProfileID: new(p.id), ProfileName: new(name), Enabled: p.autoProbe, IntervalSeconds: json.RawMessage("600"), CanRun: p.autoProbe, RunHint: hint, Running: tasks.IsBackground(key), NextRunAt: tasks.Timestamp(due), Link: new("/domain-discovery"), HistoryData: history})
	}
	return out, nil
}
