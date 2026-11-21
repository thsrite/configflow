package repo

import (
	"context"
	"encoding/json"
	"sync"
	"weak"

	"configflow/internal/model"
)

// configCredentials remembers, for one immutable snapshot document, the
// credential values found in the encoded ConfigWithOxiDNSAgents view of each
// profile. The weak pointer identifies the generation without keeping a
// replaced document alive.
type configCredentials struct {
	document weak.Pointer[model.Document]
	mu       sync.Mutex
	values   map[string][]string
}

// ConfigCredentials returns the credential values collect finds in the
// encoded ConfigWithOxiDNSAgents view of profileID. A borrowing read
// remembers them per generation and profile, so a cached generation resolves
// nothing; inside a write transaction or section updater they are collected
// from the working copy each time. collect must depend only on its input; the
// returned slice is shared and must not be modified.
func (r *Repository) ConfigCredentials(ctx context.Context, profileID string, collect func(json.RawMessage) []string) (values []string, err error) {
	err = r.borrow(ctx, func(document *model.Document, shared bool) error {
		values, err = r.configCredentials(document, shared, profileID, nil, collect)
		return err
	})
	return values, err
}

// ConfigWithCredentials is ConfigWithOxiDNSAgents together with
// ConfigCredentials for the same generation: the credential values belong to
// exactly the returned view. The view is borrowed like Config.
func (r *Repository) ConfigWithCredentials(ctx context.Context, profileID string, collect func(json.RawMessage) []string) (result *model.Config, values []string, err error) {
	err = r.borrow(ctx, func(document *model.Document, shared bool) error {
		if result, err = resolveConfig(document, profileID, ResourceRoots{}, agentCopyOxiDNS, shared); err != nil {
			return err
		}
		values, err = r.configCredentials(document, shared, profileID, result, collect)
		return err
	})
	return result, values, err
}

// ProfileWithCredentials is ConfigCredentials together with the stored
// profile of the same generation, borrowed like Profile. A cached generation
// resolves nothing; errors are those of ConfigWithCredentials.
func (r *Repository) ProfileWithCredentials(ctx context.Context, profileID string, collect func(json.RawMessage) []string) (result *model.Profile, values []string, err error) {
	err = r.borrow(ctx, func(document *model.Document, shared bool) error {
		if err := ValidateProfileID(profileID); err != nil {
			return err
		}
		stored := profileByKey(document, profileID)
		if stored == nil {
			return notFound(profileID)
		}
		if values, err = r.configCredentials(document, shared, profileID, nil, collect); err != nil {
			return err
		}
		result, err = lend(stored, shared)
		return err
	})
	return result, values, err
}

// configCredentials looks up or collects the values for one profile of
// document; config is the already resolved view, or nil.
func (r *Repository) configCredentials(document *model.Document, shared bool, profileID string, config *model.Config, collect func(json.RawMessage) []string) ([]string, error) {
	var cache *configCredentials
	if shared {
		cache = r.configCredentialsFor(document)
		cache.mu.Lock()
		values, ok := cache.values[profileID]
		cache.mu.Unlock()
		if ok {
			return values, nil
		}
	}
	if config == nil {
		var err error
		if config, err = resolveConfig(document, profileID, ResourceRoots{}, agentCopyOxiDNS, shared); err != nil {
			return nil, err
		}
	}
	data, err := model.Marshal(config)
	if err != nil {
		return nil, err
	}
	values := collect(data)
	if cache != nil {
		cache.mu.Lock()
		cache.values[profileID] = values
		cache.mu.Unlock()
	}
	return values, nil
}

// configCredentialsFor returns the cache of document's generation, replacing
// the cache of an older one.
func (r *Repository) configCredentialsFor(document *model.Document) *configCredentials {
	for {
		current := r.configCredentialsCache.Load()
		if current != nil && current.document.Value() == document {
			return current
		}
		next := &configCredentials{document: weak.Make(document), values: map[string][]string{}}
		if r.configCredentialsCache.CompareAndSwap(current, next) {
			return next
		}
	}
}
