package server

import (
	"encoding/json"

	"configflow/internal/configexport"
	"configflow/internal/repo"
	"configflow/internal/routers/spec"
)

// RepoOptions returns the repository Options wired by the server: export
// sanitization (OxiDNS + routers + external document) and default server
// domain resolution. RouterJobLock stays on routers.Wire until W9.
func RepoOptions() repo.Options {
	return repo.Options{
		Export: configexport.Exporter{
			RedactRouters: func(raw json.RawMessage) (json.RawMessage, error) {
				return spec.RedactRouters(raw)
			},
		},
		DefaultServerDomain: defaultServerDomain,
	}
}
