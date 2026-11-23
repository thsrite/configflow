package repo

import "sync"

// Hooks are the calls the repository makes into packages that depend on it
// (routers, WebDAV backup, export redaction, node deletion). Those packages
// register their implementation once at wiring time (main, or a test); the
// repository never imports them.
//
// A nil field has the default written at the field. Defaults that could
// leak data or skip a required plan fail instead of doing nothing.
type Hooks struct {
	// RecordHooks supplies native collaborators.
	RecordHooks

	// DefaultServerDomain is what load_config() stores as
	// system_config.server_domain when it is empty and the process is not in
	// a container: "http://<local ip>:<backend port>". Default: not set.
	DefaultServerDomain func() string
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
	if h.JobLockRecord != nil {
		hooks.JobLockRecord = h.JobLockRecord
	}
	if h.ConfigurationCommittedRecords != nil {
		hooks.ConfigurationCommittedRecords = h.ConfigurationCommittedRecords
	}
	if h.SanitizeExternalDocument != nil {
		hooks.SanitizeExternalDocument = h.SanitizeExternalDocument
	}
	if h.RedactOxidnsJSON != nil {
		hooks.RedactOxidnsJSON = h.RedactOxidnsJSON
	}
	if h.RedactRoutersJSON != nil {
		hooks.RedactRoutersJSON = h.RedactRoutersJSON
	}
	if h.DefaultServerDomain != nil {
		hooks.DefaultServerDomain = h.DefaultServerDomain
	}
}

// SetHooks replaces all hooks and returns the previous set, for tests:
//
//	defer repo.SetHooks(repo.SetHooks(repo.Hooks{RecordHooks: repo.RecordHooks{JobLockRecord: fake}}))
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

func recordHooks() RecordHooks {
	return currentHooks().RecordHooks
}
