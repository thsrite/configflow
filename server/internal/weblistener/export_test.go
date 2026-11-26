package weblistener

import (
	"path/filepath"
)

func (m *Manager) lockPath() string { return filepath.Join(m.DataDir, "web-listener.lock") }
