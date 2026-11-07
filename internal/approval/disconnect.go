package approval

// Disconnect removes both active authority and the metadata that could renew
// it. Only the broker's actual connection lifecycle calls this method.
func (m *Manager) Disconnect(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.pending[id]; e != nil {
		if e.request.Status == "pending" || e.request.Status == "approved" || e.request.Status == "running" {
			e.request.Status = "revoked"
			if e.cancel != nil {
				e.cancel()
			}
		}
	}
	if e := m.dormant[id]; e != nil {
		e.request.Status = "revoked"
	}
	m.cancelChildren(id)
	for key, challenge := range m.renewals {
		if challenge.SessionID == id {
			delete(m.renewals, key)
		}
	}
}
