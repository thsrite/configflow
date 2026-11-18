package repo

import (
	"context"
	"sync"

	"configflow/internal/jsonx"
)

// Hooks are the calls the Python repository makes into packages that depend
// on it (routers, WebDAV backup, export redaction, node deletion). Those
// packages register their implementation once at wiring time (main, or a
// test); the repository never imports them.
//
// A nil field has the default written at the field. Defaults that could
// leak data or skip a required plan fail instead of doing nothing.
type Hooks struct {
	// JobLock is routers/rule_sync_store.job_lock(profile_id, router_id,
	// router): a non-blocking attempt to take the publication lock of the
	// device behind a router record. acquired=false rejects the commit
	// ("路由器正在发布或同步…"). release is called after the commit (for
	// DeleteProfile: after the profile's files were moved and the document
	// committed); it is never called when acquired is false.
	//
	// It is called inside the write transaction, under .config.lock, after
	// validation and immediately before the SQLite commit; it must not wait
	// and must not call back into the repository.
	// Default: reject changed connections/manifests until routers is wired.
	JobLock func(ctx context.Context, profileID, routerID string, router *jsonx.Object) (release func(), acquired bool)

	// ConfigurationCommitted is utils/webdav_backup.configuration_committed.
	// It is called after a write transaction committed and .config.lock was
	// released, only when the committed system.backup.auto_backup is truthy.
	// previous is the generation the transaction started from (shared, never
	// mutate), current the transaction's document. It must not return an
	// error; failures are its own to log.
	// Default: nothing.
	ConfigurationCommitted func(repository *Repository, previous, current *jsonx.Object)

	// PlanNodeDeletion is common/node_deletion.plan_node_deletion(document,
	// node_ids) → (preview, candidate document). Errors pass through
	// PreviewNodeDeletion / DeleteNodesWithReferences unchanged.
	// Default: an error (the node deletion package is not wired).
	PlanNodeDeletion func(document *jsonx.Object, nodeIDs any) (preview, candidate *jsonx.Object, err error)

	// Redaction used by ExportAll(desensitize=true):
	// config_export.sanitize_external_payload(result, system_config),
	// config_export.redact_oxidns_settings, routers/store.redact_routers and
	// mihomo_easytier.redact_settings. Each returns the redacted value.
	// Default: ExportAll(true) fails rather than export secrets.
	SanitizeExternalPayload func(ctx context.Context, payload *jsonx.Object, systemConfig *jsonx.Object) (*jsonx.Object, error)
	RedactOxidnsSettings    func(settings any) any
	RedactRouters           func(routers any) any
	RedactEasytierSettings  func(settings any) any

	// DefaultServerDomain is what load_config() stores as
	// system_config.server_domain when it is empty and the process is not in
	// a container: "http://<local ip>:<backend port>". Default: not set.
	DefaultServerDomain func() string
	// InitAgentManager is common/agent_manager.init_agent_manager, called by
	// LoadConfig. Default: nothing.
	InitAgentManager func()
}

var (
	hooksMu sync.RWMutex
	hooks   Hooks
)

// RegisterHooks merges the non-nil fields of h into the process-wide hooks.
// Packages call it from their wiring function; a later registration of the
// same field replaces the earlier one.
func RegisterHooks(h Hooks) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	if h.JobLock != nil {
		hooks.JobLock = h.JobLock
	}
	if h.ConfigurationCommitted != nil {
		hooks.ConfigurationCommitted = h.ConfigurationCommitted
	}
	if h.PlanNodeDeletion != nil {
		hooks.PlanNodeDeletion = h.PlanNodeDeletion
	}
	if h.SanitizeExternalPayload != nil {
		hooks.SanitizeExternalPayload = h.SanitizeExternalPayload
	}
	if h.RedactOxidnsSettings != nil {
		hooks.RedactOxidnsSettings = h.RedactOxidnsSettings
	}
	if h.RedactRouters != nil {
		hooks.RedactRouters = h.RedactRouters
	}
	if h.RedactEasytierSettings != nil {
		hooks.RedactEasytierSettings = h.RedactEasytierSettings
	}
	if h.DefaultServerDomain != nil {
		hooks.DefaultServerDomain = h.DefaultServerDomain
	}
	if h.InitAgentManager != nil {
		hooks.InitAgentManager = h.InitAgentManager
	}
}

// SetHooks replaces all hooks and returns the previous set, for tests:
//
//	defer repo.SetHooks(repo.SetHooks(repo.Hooks{JobLock: fake}))
func SetHooks(h Hooks) Hooks {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	previous := hooks
	hooks = h
	return previous
}

func currentHooks() Hooks {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	return hooks
}
