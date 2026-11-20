package desktop

import "sshclient/internal/broker"

// In-process brokers are confined to generated headless fixtures.
func testDesktopSession(s *broker.Server) *desktopSession {
	if s == nil {
		return nil
	}
	return &desktopSession{owner: s.Vault, pin: s.PublicKeyPin(), seal: s.SealAudit, close: s.Close, abandon: s.Close}
}
func testDesktopOpen(s *broker.Server, err error) (*desktopSession, error) {
	return testDesktopSession(s), err
}
