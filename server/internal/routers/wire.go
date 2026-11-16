package routers

import (
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// Wire registers the hooks this package owns (listed in internal/server
// wirePackages): the desensitized export of routers. Later modules add the
// job lock, the rule-sync task slot and loop here.
func Wire() {
	repo.RegisterHooks(repo.Hooks{RedactRouters: redactHook})
}

// redactHook adapts RedactRouters to repo's jsonx document (until repo's S pass).
func redactHook(routers any) any {
	if document, err := jsonx.Compact(routers); err == nil {
		if redacted, err := RedactRouters(document); err == nil {
			if value, err := jsonx.Parse(redacted); err == nil {
				return value
			}
		}
	}
	return routers
}
