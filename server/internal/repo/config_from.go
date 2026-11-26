package repo

import "configflow/internal/model"

// ConfigFrom resolves a profile from a document already held by a snapshot or
// locked hook. It performs no repository read and acquires no lock. The
// result borrows the document (see "Borrowed reads" in config_borrow.go): callers
// must keep the document stable while they use it and must not modify what it
// shares. Agents are omitted, as in Config.
func ConfigFrom(document *model.Document, profileID string, roots ResourceRoots) (*model.Config, error) {
	return resolveConfig(document, profileID, roots, agentCopyNone, true)
}
