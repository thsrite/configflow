package agents

// OnClose registers an in-memory dependent resource's cleanup. False means the
// Manager is already closed; callers must clean up their resource immediately.
func (m *Manager) OnClose(fn func()) bool {
	m.closeMu.Lock()
	defer m.closeMu.Unlock()
	if m.closed {
		return false
	}
	m.closeHooks = append(m.closeHooks, fn)
	return true
}

// Close releases Manager-owned in-memory dependents exactly once. It does not
// stop another Manager or change repository/device state.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.closeMu.Lock()
	if m.closed {
		m.closeMu.Unlock()
		return
	}
	m.closed = true
	hooks := m.closeHooks
	m.closeHooks = nil
	m.closeMu.Unlock()
	for _, fn := range hooks {
		func() {
			defer func() {
				if recover() != nil {
					logger.Error("Agent manager cleanup panicked")
				}
			}()
			fn()
		}()
	}
}
