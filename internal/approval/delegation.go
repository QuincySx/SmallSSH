package approval

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// GrantScope never contains a provider session or an SSH credential. PublicKey
// identifies possession of a client key, not a verified application brand.
type GrantScope struct {
	ReadRoot        string `json:"readRoot,omitempty"`
	WriteRoot       string `json:"writeRoot,omitempty"`
	PublicKey       string `json:"publicKey"`
	Permission      string `json:"permission"`
	LifetimeSeconds int    `json:"lifetimeSeconds"`
	IdleSeconds     int    `json:"idleSeconds"`
}

func (s GrantScope) Validate() error {
	for _, root := range []string{s.ReadRoot, s.WriteRoot} {
		if root != "" && (len(root) > 4096 || !utf8.ValidString(root) || !path.IsAbs(root) || path.Clean(root) != root || strings.ContainsFunc(root, unicode.IsControl)) {
			return errors.New("授权根目录需为规范绝对路径")
		}
	}
	if s.Permission == "read" && s.WriteRoot != "" {
		return errors.New("只读授权不能包含写入根目录")
	}
	key, err := base64.RawURLEncoding.DecodeString(s.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize || (s.Permission != "read" && s.Permission != "full") || s.LifetimeSeconds < 60 || s.LifetimeSeconds > 8*60*60 || s.IdleSeconds < 60 || s.IdleSeconds > 30*60 || s.IdleSeconds > s.LifetimeSeconds {
		return errors.New("会话授权需指定客户端公钥、只读或完整权限、1分钟至8小时期限和不超过30分钟的闲置期限")
	}
	return nil
}

func (s GrantScope) PairingCode() string {
	key, err := base64.RawURLEncoding.DecodeString(s.PublicKey)
	if err != nil {
		return "无效公钥"
	}
	// A readable fingerprint is displayed independently on both sides before
	// human approval. It must never be replaced with the caller's own label.
	return pairingFingerprint(key)
}

type DelegationChallenge struct {
	ID         string    `json:"id"`
	ParentID   string    `json:"parentId"`
	Operation  Operation `json:"operation"`
	TTLSeconds int       `json:"ttlSeconds"`
	Expires    time.Time `json:"expires"`
}

func DelegationProofMessage(c DelegationChallenge) []byte {
	b, _ := json.Marshal(c)
	return append([]byte("ssh-client/delegation/v1\x00"), b...)
}

func grantAllows(parent Request, op Operation) bool {
	if parent.Operation.Kind != "grant" || parent.Status != "approved" || op.Grant != (GrantScope{}) || op.Kind == "grant" || op.Host != parent.Operation.Host || op.Endpoint != parent.Operation.Endpoint || op.HostFingerprint != parent.Operation.HostFingerprint {
		return false
	}
	if scope := parent.Operation.Grant; scope.ReadRoot != "" || scope.WriteRoot != "" {
		return scope.allowsPath(op)
	}
	if parent.Operation.Grant.Permission == "full" {
		return true
	}
	switch op.Kind {
	case "monitor", "list", "file-info", "get", "get-resume", "docker-logs":
		return true
	}
	return false
}

func WithinRoot(root, target string) bool {
	return root != "" && path.IsAbs(target) && path.Clean(target) == target && !strings.ContainsFunc(target, unicode.IsControl) && (target == root || strings.HasPrefix(target, strings.TrimSuffix(root, "/")+"/"))
}

func (s GrantScope) allowsPath(op Operation) bool {
	switch op.Kind {
	case "list", "file-info", "get", "get-resume":
		return WithinRoot(s.ReadRoot, op.Path)
	case "put", "replace", "mkdir", "remove", "rename":
		return s.Permission == "full" && op.Path != s.WriteRoot && WithinRoot(s.WriteRoot, op.Path) && (op.Kind != "rename" || op.Destination != s.WriteRoot && WithinRoot(s.WriteRoot, op.Destination))
	}
	// Arbitrary commands, existing-inode writes and status/log reads cannot be
	// confined to a file root over SFTP. They require a separate broad grant.
	return false
}

func (m *Manager) DelegationChallenge(parentID string, op Operation, ttl time.Duration) (DelegationChallenge, error) {
	if err := validateFields(op); err != nil {
		return DelegationChallenge{}, err
	}
	if len(op.Command) > 32768 || len(op.Path) > 4096 || len(op.Destination) > 4096 || len(op.Client) > 128 || len(op.Host) > 128 {
		return DelegationChallenge{}, errors.New("子授权操作字段超过上限")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	parent := m.pending[parentID]
	if parent == nil && m.dormant[parentID] != nil && m.dormant[parentID].request.Status == "expired" {
		candidate := m.dormant[parentID].request
		candidate.Status = "approved"
		if ttl < time.Second || ttl > 5*time.Minute || !grantAllows(candidate, op) {
			return DelegationChallenge{}, ErrDenied
		}
		return DelegationChallenge{}, ErrSessionExpired
	}
	if parent != nil && parent.request.Status == "pending" && parent.request.Renewal {
		candidate := parent.request
		candidate.Status = "approved"
		if ttl < time.Second || ttl > 5*time.Minute || !grantAllows(candidate, op) {
			return DelegationChallenge{}, ErrDenied
		}
		return DelegationChallenge{}, ErrSessionRenewing
	}
	if parent == nil || !grantAllows(parent.request, op) || ttl < time.Second || ttl > 5*time.Minute {
		return DelegationChallenge{}, ErrDenied
	}
	if m.delegations == nil {
		m.delegations = make(map[string]DelegationChallenge)
	}
	for id, c := range m.delegations {
		if !m.now().Before(c.Expires) {
			delete(m.delegations, id)
		}
	}
	if len(m.delegations) >= 128 {
		return DelegationChallenge{}, errors.New("待签名授权过多")
	}
	c := DelegationChallenge{ID: token(), ParentID: parentID, Operation: op, TTLSeconds: int(ttl / time.Second), Expires: m.now().Add(30 * time.Second).UTC()}
	m.delegations[c.ID] = c
	return c, nil
}

// MintDelegated consumes a signed, one-use challenge. The final locked check
// closes the revoke/create race; an unapproved child is never returned.
func (m *Manager) MintDelegated(id string, signature []byte) (Request, string, error) {
	m.mu.Lock()
	m.prune()
	c, ok := m.delegations[id]
	delete(m.delegations, id)
	parent := m.pending[c.ParentID]
	if !ok || !m.now().Before(c.Expires) || parent == nil || !grantAllows(parent.request, c.Operation) {
		m.mu.Unlock()
		return Request{}, "", ErrDenied
	}
	key, _ := base64.RawURLEncoding.DecodeString(parent.request.Operation.Grant.PublicKey)
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, DelegationProofMessage(c), signature) {
		m.mu.Unlock()
		return Request{}, "", ErrDenied
	}
	ttl := min(time.Duration(c.TTLSeconds)*time.Second, parent.request.Expires.Sub(m.now()))
	m.mu.Unlock()
	r, t, err := m.Create(c.Operation, ttl)
	if err != nil {
		return Request{}, "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	parent = m.pending[c.ParentID]
	child := m.pending[r.ID]
	if parent == nil || child == nil || !grantAllows(parent.request, c.Operation) {
		delete(m.pending, r.ID)
		return Request{}, "", ErrDenied
	}
	child.request.ParentID = c.ParentID
	child.request.Status = "approved"
	child.request.Expires = minTime(child.request.Expires, parent.request.Expires)
	parent.lastUsed = m.now()
	return child.request, t, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (m *Manager) cancelChildren(parent string) {
	for _, e := range m.pending {
		if e.request.ParentID == parent {
			e.request.Status = "revoked"
			if e.cancel != nil {
				e.cancel()
			}
		}
	}
	for id, c := range m.delegations {
		if c.ParentID == parent {
			delete(m.delegations, id)
		}
	}
}

// DelegationOperation supplies only the immutable operation metadata needed by
// broker audit when a signed challenge is rejected. It grants no authority.
func (m *Manager) DelegationOperation(id string) (Operation, string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.delegations[id]
	return c.Operation, c.ParentID, ok
}

// Sweep is called by the broker independently of incoming requests, so an idle
// parent also cancels tasks when the requesting client stops making calls.
func (m *Manager) Sweep() { m.mu.Lock(); defer m.mu.Unlock(); m.prune() }
