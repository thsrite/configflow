package agents

import (
	"configflow/internal/store"
)

// setAgentViewCache switches the cache and returns a function restoring the
// previous state (tests and measurements).
func setAgentViewCache(on bool) (restore func()) {
	previous := views.enabled.Swap(on)
	views.reset()
	return func() { views.enabled.Store(previous); views.reset() }
}

func (v *agentViews) reset() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.repository, v.head, v.env = nil, store.Head{}, ""
	v.sourceRepo, v.sourceHead = nil, store.Head{}
	v.entries, v.sources = nil, nil
}
