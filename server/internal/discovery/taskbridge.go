package discovery

import (
	"context"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

func wireTasks() {
	tasks.SetRows(tasks.SlotDomains, domainRows)
	tasks.SetRunner(tasks.SlotDomains, func(ctx context.Context, key, resource string) error {
		ctx = context.WithoutCancel(ctx)
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
		worker := repo.WithRepository(context.Background(), r)
		return tasks.StartBackground(worker, key, func(ctx context.Context) error { _, err := RunAutoTask(ctx, resource, "manual"); return err })
	})
}
func domainRows(ctx context.Context, statuses *jsonx.Object) ([]*jsonx.Object, error) {
	profiles, err := listProfiles(ctx)
	if err != nil {
		return nil, err
	}
	out := []*jsonx.Object{}
	for _, p := range profiles {
		key := "domain_discovery:" + p.ID
		c, err := loadConfig(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		entry, exists := statuses.Get(key)
		if !c.Settings.AutoProbe && !exists {
			continue
		}
		e, _ := entry.(*jsonx.Object)
		var due *time.Time
		if last, ok := taskstatus.ParseTime(e.Value("last_run_at")); ok && c.Settings.AutoProbe {
			due = ptr(last.Add(600 * time.Second))
		}
		hint := ""
		if !c.Settings.AutoProbe {
			hint = msgAutoDisabled
		}
		name := p.Name
		if name == "" {
			name = p.ID
		}
		fields := []any{"resource_id", p.ID, "profile_id", p.ID, "profile_name", name, "enabled", c.Settings.AutoProbe, "interval_seconds", 600, "can_run", c.Settings.AutoProbe, "run_hint", hint, "running", tasks.IsBackground(key), "next_run_at", tasks.ISO(due), "link", "/domain-discovery"}
		fields = append(fields, tasks.Spread(tasks.History(e))...)
		out = append(out, tasks.Row(key, tasks.SlotDomains, msgAutoName, fields...))
	}
	return out, nil
}
