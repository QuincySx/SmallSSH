package vault

// SaveAuditHead commits the anchor with the current vault contents under one
// lock. Concurrent profile/workspace saves cannot publish an older anchor or
// have their changes overwritten by a separately sealed checkpoint snapshot.
func (s *Session) SaveAuditHead(file, head string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrUnlock
	}
	copy := &Session{header: s.header, key: s.key, routes: s.routes, profiles: s.profiles, snippets: s.snippets, notes: s.notes, auditHead: head}
	if err := Save(file, copy, false); err != nil {
		return err
	}
	s.auditHead = head
	return nil
}
