package deploy

import (
	"configflow/internal/agents"
	"sync"
)

var previewOwners = struct {
	sync.Mutex
	items map[*agents.Manager]*Previews
}{items: map[*agents.Manager]*Previews{}}

func previewsFor(m *agents.Manager) *Previews {
	previewOwners.Lock()
	defer previewOwners.Unlock()
	if p := previewOwners.items[m]; p != nil {
		return p
	}
	p := NewPreviews(m.Repository().DataDir(), nil)
	if m.OnClose(func() { previewOwners.Lock(); delete(previewOwners.items, m); previewOwners.Unlock() }) {
		previewOwners.items[m] = p
	}
	return p
}

// LoadPreview reads the Manager-owned immutable artifact store.
func LoadPreview(m *agents.Manager, id, agentID string) (*Preview, error) {
	return previewsFor(m).Load(id, agentID)
}
