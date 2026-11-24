package webdav

import (
	"context"
	"errors"

	"configflow/internal/lifecycle"
	"configflow/internal/tasks"
)

func (s *Service) registerTasks(tk *tasks.Registry) func() {
	return tk.SetRunner(tasks.SlotBackup, func(ctx context.Context, key, _ string) error {
		ctx = lifecycle.Detach(ctx)
		if err := ctx.Err(); err != nil {
			return err
		}
		return tk.StartBackground(ctx, key, func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			r := s.Repo
			if r == nil {
				return errors.New("webdav: repository is nil")
			}
			snapshot, err := (repositorySource{r}).Snapshot(ctx)
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			_, err = s.Run(ctx, snapshot, false)
			return err
		})
	})
}
