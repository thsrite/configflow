package server

import (
	"encoding/json"

	"configflow/internal/configexport"
	"configflow/internal/repo"
	"configflow/internal/routers"
	"configflow/internal/routers/spec"
)

// RepoOptions returns the repository Options wired by the server: export
// sanitization (OxiDNS + routers + external document), router publication job
// lock, and default server domain resolution.
func RepoOptions() repo.Options {
	return repo.Options{
		Export: configexport.Exporter{
			RedactRouters: func(raw json.RawMessage) (json.RawMessage, error) {
				return spec.RedactRouters(raw)
			},
		},
		RouterJobLock:       routers.JobLockRecord,
		DefaultServerDomain: defaultServerDomain,
	}
}
