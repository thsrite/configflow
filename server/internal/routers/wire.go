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

// redactHook adapts RedactRouters to repo's jsonx document (repo keeps jsonx
// until its S pass).
func redactHook(routers any) any {
	document, err := jsonx.Compact(routers)
	if err != nil {
		return routers
	}
	redacted, err := RedactRouters(document)
	if err != nil {
		return routers
	}
	value, err := jsonx.Parse(redacted)
	if err != nil {
		return routers
	}
	return value
}
