package webdav

import (
	"context"

	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/tasks"
)

func wireTasks() func() {
	return tasks.SetRunner(tasks.SlotBackup, func(ctx context.Context, key, _ string) error {
		ctx = lifecycle.Detach(ctx)
		if err := ctx.Err(); err != nil {
			return err
		}
		return tasks.StartBackground(ctx, key, func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			r, err := repo.GetRepository(ctx)
			if err != nil {
				return err
			}
			snapshot, err := (repositorySource{r}).Snapshot(ctx)
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			_, err = Run(ctx, snapshot, false)
			return err
		})
	})
}
