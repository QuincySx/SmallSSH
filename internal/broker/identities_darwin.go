//go:build darwin

package broker

import "sshclient/internal/systemauth"

// A packaged daemon publishes the standard agent socket when the vault opens.
// The native process remains lazy: no Keychain or authentication UI at startup.
// Headless test executables have no packaged native helper beside them.
func (s *Server) startPackagedIdentityAgent() {
	if _, err := systemauth.IdentityAgentPath(); err == nil {
		_, _ = s.identityController()
	}
}
