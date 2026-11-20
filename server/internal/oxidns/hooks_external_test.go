package oxidns_test

// The boundary rows ran Python with the real rule_ip_presence hook; the Go
// tests register the routers' implementation on a private temporary
// repository (never DATA_DIR). External test package: ippresence imports
// oxidns.

import (
	"context"
	"os"
	"sync"

	"configflow/internal/ippresence"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
)

func init() {
	var (
		once       sync.Once
		repository *repo.Repository
		openErr    error
	)
	oxidns.SetNativeClassicalIPLines(func(ctx context.Context, item model.RuleConfig, library []model.RuleLibraryItem) (*bool, error) {
		once.Do(func() {
			dir, err := os.MkdirTemp("", "oxidns-boundary-")
			if err != nil {
				openErr = err
				return
			}
			repository, openErr = repo.Open(dir, repo.Options{})
		})
		if openErr != nil {
			return nil, openErr
		}
		return ippresence.PeekTypedClassicalIPLines(repo.WithRepository(ctx, repository), item, library)
	})
}
