package deploy

import "configflow/internal/agents"

func previewsFor(m *agents.Manager) *Previews {
	return Default().PreviewsFor(m)
}

// LoadPreview reads the Manager-owned immutable artifact store.
func LoadPreview(m *agents.Manager, id, agentID string) (*Preview, error) {
	return Default().LoadPreview(m, id, agentID)
}
