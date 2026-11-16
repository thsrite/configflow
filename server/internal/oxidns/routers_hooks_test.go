package oxidns_test

// Added by routers R02: the replays of this package (recorded.jsonl.gz,
// corpus.jsonl.gz, c23/c24 files) run with the real
// rule_ip_presence.classical_ip_lines hook instead of C22's pending stub, so
// rows that reach it are compared instead of counted as pending. The hook
// gets a private temporary repository (created on first use) instead of the
// default one, which would open DATA_DIR or the working directory.

import (
	"context"
	"os"
	"sync"

	"configflow/internal/ippresence"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
)

func init() {
	var (
		once       sync.Once
		repository *repo.Repository
		openErr    error
	)
	oxidns.SetClassicalIPLines(func(ctx context.Context, item, config any) (any, error) {
		once.Do(func() {
			dir, err := os.MkdirTemp("", "oxidns-r02-hook-")
			if err != nil {
				openErr = err
				return
			}
			repository, openErr = repo.Open(dir, repo.Options{})
		})
		if openErr != nil {
			return nil, openErr
		}
		return ippresence.ClassicalIPLines(repo.WithRepository(ctx, repository), item, config)
	})
}
