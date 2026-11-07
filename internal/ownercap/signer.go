package ownercap

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"sync"

	"sshclient/internal/localipc"
)

var (
	errSignerClosed   = errors.New("管理会话签名器已关闭")
	errSignerBound    = errors.New("管理会话签名器已绑定")
	errSignerUnbound  = errors.New("管理会话签名器尚未绑定")
	errSessionExpired = errors.New("管理会话已到期")
)

type Signer struct {
	mu      sync.Mutex
	private ed25519.PrivateKey
	public  ed25519.PublicKey
	bound   bool
	session SessionInfo
	closed  bool
}

func NewSigner() (*Signer, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Signer{
		private: private,
		public:  append(ed25519.PublicKey(nil), public...),
	}, nil
}

func (s *Signer) PublicKey() ed25519.PublicKey {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append(ed25519.PublicKey(nil), s.public...)
}

func (s *Signer) Bind(session SessionInfo) error {
	if s == nil {
		return errSignerClosed
	}
	if !validEntropyHex(session.ID) || !validEntropyHex(session.Epoch) || session.Expires.IsZero() {
		return errors.New("管理会话元数据无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errSignerClosed
	}
	if s.bound {
		return errSignerBound
	}
	s.session = session
	s.bound = true
	return nil
}

func (s *Signer) Proof(payload []byte) ([]byte, error) {
	if s == nil {
		return nil, errSignerClosed
	}
	if len(payload) > localipc.MaxFrame {
		return nil, errors.New("管理会话证明载荷过大")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || len(s.private) != ed25519.PrivateKeySize {
		return nil, errSignerClosed
	}
	if !s.bound {
		return nil, errSignerUnbound
	}
	if !now().Before(s.session.Expires) {
		return nil, errSessionExpired
	}
	signature := ed25519.Sign(s.private, signingInput(s.session.Epoch, s.session.ID, payload))
	return encodeProof(s.session.ID, signature), nil
}

func (s *Signer) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	clear(s.private)
	s.private = nil
	s.closed = true
}
