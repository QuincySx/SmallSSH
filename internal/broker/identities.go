package broker

import (
	"context"
	"errors"
	"runtime"
	"sshclient/internal/identityagent"
	"sshclient/internal/identitymodel"
	"sshclient/internal/vault"
)

type identityController interface {
	State(context.Context) (identitymodel.State, error)
	Change(context.Context, identitymodel.Change) error
	Close()
}

func (s *Server) identityController() (identityController, error) {
	s.identityMu.Lock()
	defer s.identityMu.Unlock()
	if s.ctx.Err() != nil {
		return nil, errors.New("工作台已锁定")
	}
	if s.identityAgent == nil {
		if runtime.GOOS != "darwin" {
			return nil, errors.New("安全隔区 SSH 密钥当前仅支持 macOS")
		}
		a, err := identityagent.OpenWithAudit(s.dir, s.auditSignature, s.auditIdentityAuthorization)
		if err != nil {
			return nil, err
		}
		s.identityAgent = a
	}
	return s.identityAgent, nil
}
func (s *Server) identityState() (identitymodel.State, error) {
	if runtime.GOOS != "darwin" {
		return identitymodel.State{}, nil
	}
	controller, err := s.identityController()
	if err != nil {
		return identitymodel.State{}, err
	}
	return controller.State(s.ctx)
}
func (s *Server) closeIdentityAgent() {
	s.identityMu.Lock()
	controller := s.identityAgent
	s.identityAgent = nil
	s.identityMu.Unlock()
	if controller != nil {
		controller.Close()
	}
}

func (s *Server) resolveIdentitySockets(route []vault.Profile) error {
	for i := range route {
		if route[i].Auth != "managed" {
			continue
		}
		if _, err := s.identityController(); err != nil {
			return err
		}
		route[i].AgentSocket = identitymodel.SocketPath(s.dir)
	}
	return nil
}
