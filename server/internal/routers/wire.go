package routers

import "configflow/internal/repo"

// Wire registers the hooks the routers package owns (listed in
// internal/server wirePackages): repo.Hooks.RedactRouters for the
// desensitized export. Later modules add JobLock, the task slot/runner and
// the rule-sync loop here.
func Wire() {
	repo.RegisterHooks(repo.Hooks{RedactRouters: RedactRouters})
}
