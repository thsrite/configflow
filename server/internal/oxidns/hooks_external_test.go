package oxidns_test

// The boundary rows ran Python with the real rule_ip_presence hook; the Go
// tests bind a private temporary repository (never DATA_DIR) so direct
// ippresence lookups never touch the user's DATA_DIR.

import (
	"os"

	"configflow/internal/repo"
)

func init() {
	dir, err := os.MkdirTemp("", "oxidns-boundary-")
	if err != nil {
		panic(err)
	}
	repository, err := repo.Open(dir, repo.Options{})
	if err != nil {
		panic(err)
	}
	repo.SetRepository(repository)
}
