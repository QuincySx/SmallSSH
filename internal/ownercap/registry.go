package ownercap

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"sshclient/internal/localipc"
)

const (
	maxSessions     = 16
	defaultLifetime = time.Hour
	minLifetime     = time.Minute
	maxLifetime     = 8 * time.Hour
	idleLifetime    = 15 * time.Minute
)

var now = time.Now

type SessionInfo struct {
	ID      string    `json:"id"`
	Epoch   string    `json:"epoch"`
	Expires time.Time `json:"expires"`
}

type sessionEntry struct {
	info        SessionInfo
	identity    *localipc.ProcessIdentity
	public      ed25519.PublicKey
	ctx         context.Context
	idleExpires time.Time
	stop        func() bool
}

type Registry struct {
	mu       sync.Mutex
	epoch    string
	onRevoke func(string)
	sessions map[string]*sessionEntry
	closed   bool
}

func NewRegistry(epoch string, onRevoke func(string)) (*Registry, error) {
	if !validEntropyHex(epoch) {
		return nil, errors.New("宿主代次必须是32位小写随机十六进制")
	}
	return &Registry{
		epoch:    epoch,
		onRevoke: onRevoke,
		sessions: make(map[string]*sessionEntry),
	}, nil
}

func (r *Registry) Register(ctx context.Context, identity *localipc.ProcessIdentity, public ed25519.PublicKey, lifetime time.Duration) (SessionInfo, error) {
	if r == nil {
		return SessionInfo{}, errors.New("管理会话注册表为空")
	}
	if ctx == nil || ctx.Done() == nil {
		return SessionInfo{}, errors.New("管理会话必须绑定可取消的活连接")
	}
	if err := ctx.Err(); err != nil {
		return SessionInfo{}, err
	}
	if identity == nil || identity.Verify() != nil {
		return SessionInfo{}, localipc.ErrIdentityUnavailable
	}
	if len(public) != ed25519.PublicKeySize {
		return SessionInfo{}, errors.New("管理会话公钥长度无效")
	}
	publicCopy := append(ed25519.PublicKey(nil), public...)
	if lifetime == 0 {
		lifetime = defaultLifetime
	}
	if lifetime < minLifetime || lifetime > maxLifetime {
		return SessionInfo{}, errors.New("管理会话期限必须在1分钟到8小时之间")
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return SessionInfo{}, errors.New("管理会话注册表已关闭")
	}
	if len(r.sessions) >= maxSessions {
		r.mu.Unlock()
		return SessionInfo{}, errors.New("管理会话数量已达上限")
	}
	if err := ctx.Err(); err != nil {
		r.mu.Unlock()
		return SessionInfo{}, err
	}
	if err := identity.Verify(); err != nil {
		r.mu.Unlock()
		return SessionInfo{}, localipc.ErrIdentityUnavailable
	}
	id, err := r.newSessionIDLocked()
	if err != nil {
		r.mu.Unlock()
		return SessionInfo{}, err
	}
	startedAt := now()
	expires := startedAt.Add(lifetime)
	idleExpires := startedAt.Add(idleLifetime)
	if idleExpires.After(expires) {
		idleExpires = expires
	}
	entry := &sessionEntry{
		info:        SessionInfo{ID: id, Epoch: r.epoch, Expires: expires},
		identity:    identity,
		public:      publicCopy,
		ctx:         ctx,
		idleExpires: idleExpires,
	}
	entry.stop = context.AfterFunc(ctx, func() { r.Revoke(id) })
	r.sessions[id] = entry
	info := entry.info
	r.mu.Unlock()
	return info, nil
}

func (r *Registry) Verify(caller *localipc.ProcessIdentity, payload, proof []byte) (string, error) {
	if r == nil {
		return "", errors.New("管理会话注册表为空")
	}
	if len(payload) > localipc.MaxFrame {
		return "", errors.New("管理会话验证载荷过大")
	}
	sessionID, signature, err := decodeProof(proof)
	if err != nil {
		return "", err
	}
	if caller == nil || caller.Verify() != nil {
		return "", localipc.ErrIdentityUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "", errors.New("管理会话注册表已关闭")
	}
	entry := r.sessions[sessionID]
	if entry == nil {
		return "", errors.New("管理会话不存在")
	}
	current := now()
	if entry.ctx.Err() != nil || !current.Before(entry.info.Expires) || !current.Before(entry.idleExpires) {
		return "", errors.New("管理会话已失效")
	}
	if entry.identity == nil || entry.identity.Verify() != nil || !entry.identity.Same(caller) {
		return "", localipc.ErrIdentityUnavailable
	}
	if !ed25519.Verify(entry.public, signingInput(entry.info.Epoch, sessionID, payload), signature) {
		return "", errors.New("管理会话证明签名无效")
	}
	entry.idleExpires = current.Add(idleLifetime)
	if entry.idleExpires.After(entry.info.Expires) {
		entry.idleExpires = entry.info.Expires
	}
	return sessionID, nil
}

func (r *Registry) Revoke(id string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	entry := r.sessions[id]
	if entry != nil {
		delete(r.sessions, id)
	}
	r.mu.Unlock()
	if entry != nil {
		r.finishRevocations([]*sessionEntry{entry})
	}
}

func (r *Registry) RevokeAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	entries := r.takeAllLocked()
	r.mu.Unlock()
	r.finishRevocations(entries)
}

func (r *Registry) Sweep() {
	if r == nil {
		return
	}
	current := now()
	r.mu.Lock()
	var entries []*sessionEntry
	for id, entry := range r.sessions {
		if entry.ctx.Err() != nil || !current.Before(entry.info.Expires) || !current.Before(entry.idleExpires) || entry.identity == nil || entry.identity.Verify() != nil {
			delete(r.sessions, id)
			entries = append(entries, entry)
		}
	}
	r.mu.Unlock()
	r.finishRevocations(entries)
}

func (r *Registry) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	entries := r.takeAllLocked()
	r.mu.Unlock()
	r.finishRevocations(entries)
}

func (r *Registry) takeAllLocked() []*sessionEntry {
	entries := make([]*sessionEntry, 0, len(r.sessions))
	for id, entry := range r.sessions {
		delete(r.sessions, id)
		entries = append(entries, entry)
	}
	return entries
}

func (r *Registry) finishRevocations(entries []*sessionEntry) {
	for _, entry := range entries {
		if entry.stop != nil {
			entry.stop()
		}
	}
	for _, entry := range entries {
		if r.onRevoke != nil {
			r.onRevoke(entry.info.ID)
		}
	}
}

func (r *Registry) newSessionIDLocked() (string, error) {
	for {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", err
		}
		id := hex.EncodeToString(random[:])
		if !validEntropyHex(id) {
			continue
		}
		if _, exists := r.sessions[id]; !exists {
			return id, nil
		}
	}
}
