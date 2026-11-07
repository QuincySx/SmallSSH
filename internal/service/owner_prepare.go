package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"time"

	"sshclient/internal/broker"
	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
	"sshclient/internal/vault"
)

const (
	serviceMaxPendingOwners     = 2
	servicePendingOwnerLifetime = 30 * time.Second
	serviceDefaultOwnerLifetime = time.Hour
	serviceMinOwnerLifetime     = time.Minute
	serviceMaxOwnerLifetime     = 8 * time.Hour
)

var errOwnerPrepareFailed = errors.New("owner prepare failed")

type preparedOwner struct {
	session    *vault.Session
	generation uint64
	publicKey  []byte
	lifetime   time.Duration
	id         string
	expiresAt  time.Time
	raw        net.Conn
	identity   *localipc.ProcessIdentity
	controlCtx context.Context
	challenge  string
	binding    string
	slotHeld   bool
	cleaned    bool
}

func randomServiceToken() (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}

func (h *Host) generationSnapshot() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.generation
}

func (h *Host) closingSnapshot() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closing || h.stopping
}

func (h *Host) prepareOwner(
	controlCtx context.Context,
	raw net.Conn,
	identity *localipc.ProcessIdentity,
	negotiatedMinor int,
	challenge string,
	binding string,
	message protocol.ServiceMessage,
) (*preparedOwner, error) {
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()

	generation := h.generationSnapshot()
	prepare := message.OwnerPrepare
	if prepare == nil || controlCtx.Err() != nil || h.ctx.Err() != nil || h.closingSnapshot() ||
		message.Major != protocol.ServiceProtocolMajor || message.Minor != negotiatedMinor ||
		message.Epoch != h.epoch || message.Generation != generation ||
		message.Challenge != challenge || prepare.Epoch != h.epoch ||
		prepare.Challenge != challenge || prepare.Generation != generation ||
		prepare.Binding != binding {
		return nil, errOwnerPrepareFailed
	}
	if len(prepare.PublicKey) != 32 || len(message.UnlockKey) != 32 {
		return nil, errOwnerPrepareFailed
	}

	ttlSeconds := prepare.LifetimeSeconds
	if ttlSeconds == 0 {
		ttlSeconds = int(serviceDefaultOwnerLifetime / time.Second)
	}
	if ttlSeconds < int(serviceMinOwnerLifetime/time.Second) ||
		ttlSeconds > int(serviceMaxOwnerLifetime/time.Second) {
		return nil, errOwnerPrepareFailed
	}
	keyDigest := sha256.Sum256(message.UnlockKey)
	if !hmac.Equal([]byte(prepare.KeyHash), []byte(hex.EncodeToString(keyDigest[:]))) {
		return nil, errOwnerPrepareFailed
	}
	if h.pendingOwners >= serviceMaxPendingOwners {
		return nil, errOwnerPrepareFailed
	}
	h.pendingOwners++
	reserved := true
	defer func() {
		if reserved {
			h.pendingOwners--
		}
	}()

	session, err := vault.ReadKey(broker.VaultPath(h.dir), message.UnlockKey)
	if err != nil {
		return nil, errOwnerPrepareFailed
	}
	defer func() {
		if session != nil {
			session.Close()
		}
	}()
	if controlCtx.Err() != nil || h.ctx.Err() != nil || h.closingSnapshot() {
		return nil, errOwnerPrepareFailed
	}

	payload, err := protocol.ServiceOwnerPreparePayload(*prepare)
	if err != nil {
		return nil, errOwnerPrepareFailed
	}
	expected, err := session.Proof(payload)
	if err != nil {
		return nil, errOwnerPrepareFailed
	}
	defer clear(expected)
	if !hmac.Equal(message.Proof, expected) {
		return nil, errOwnerPrepareFailed
	}
	if controlCtx.Err() != nil || h.ctx.Err() != nil || h.closingSnapshot() ||
		h.generationSnapshot() != generation || identity == nil || identity.Verify() != nil {
		return nil, errOwnerPrepareFailed
	}

	id, err := randomServiceToken()
	if err != nil {
		return nil, errOwnerPrepareFailed
	}
	if controlCtx.Err() != nil || h.ctx.Err() != nil || h.closingSnapshot() ||
		h.generationSnapshot() != generation || identity.Verify() != nil {
		return nil, errOwnerPrepareFailed
	}
	prepared := &preparedOwner{
		session:    session,
		generation: generation,
		publicKey:  append([]byte(nil), prepare.PublicKey...),
		lifetime:   time.Duration(ttlSeconds) * time.Second,
		id:         id,
		expiresAt:  time.Now().Add(servicePendingOwnerLifetime),
		raw:        raw,
		identity:   identity,
		controlCtx: controlCtx,
		challenge:  challenge,
		binding:    binding,
		slotHeld:   true,
	}
	if h.pending == nil {
		h.pending = make(map[*preparedOwner]struct{})
	}
	h.pending[prepared] = struct{}{}
	session = nil
	reserved = false
	return prepared, nil
}

func (h *Host) releasePending(pending *preparedOwner) {
	if pending == nil {
		return
	}
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	pending.cleanupLocked(h)
}

func (pending *preparedOwner) cleanupLocked(h *Host) {
	if pending == nil || pending.cleaned {
		return
	}
	pending.cleaned = true
	if pending.session != nil {
		pending.session.Close()
		pending.session = nil
	}
	if h.pending != nil {
		delete(h.pending, pending)
	}
	clear(pending.publicKey)
	pending.publicKey = nil
	if pending.slotHeld {
		pending.slotHeld = false
		if h.pendingOwners > 0 {
			h.pendingOwners--
		}
	}
}

func (h *Host) cleanupPendingLocked() {
	if h == nil {
		return
	}
	for pending := range h.pending {
		pending.cleanupLocked(h)
		if pending.raw != nil {
			_ = pending.raw.Close()
			pending.raw = nil
		}
	}
}
