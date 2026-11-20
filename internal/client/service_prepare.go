package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"sshclient/internal/localipc"
	"sshclient/internal/ownercap"
	"sshclient/internal/protocol"
)

const ownerPrepareDefaultLifetime = time.Hour
const ownerPrepareMinLifetime = time.Minute
const ownerPrepareMaxLifetime = 8 * time.Hour
const ownerPreparedPendingLifetime = 30 * time.Second

// UnlockSource is the minimal owner material needed to prepare a service-owner
// session. It deliberately does not expose the vault's profile operations.
type UnlockSource interface {
	OwnerProof
	UnlockKey() ([]byte, error)
}

type preparedOwnerState uint8

const (
	preparedOwnerPrepared preparedOwnerState = iota
	preparedOwnerCommitting
	preparedOwnerTransferred
	preparedOwnerClosed
)

// PreparedOwner owns a verified control connection and an unbound in-memory
// signer until it is closed or handed to a committed owner session.
type PreparedOwner struct {
	mu             sync.Mutex
	state          preparedOwnerState
	wire           *localipc.Wire
	signer         *ownercap.Signer
	epoch          string
	challenge      string
	generation     uint64
	minor          int
	binding        string
	pendingID      string
	lifetime       time.Duration
	authCtx        context.Context
	stopAuthCancel func() bool
	expiry         *time.Timer
	done           chan struct{}
}

// Done is closed when the prepared connection is closed or transferred. It
// says nothing about root authorization or whether an owner session exists.
func (p *PreparedOwner) Done() <-chan struct{} {
	if p == nil {
		return nil
	}
	return p.done
}

// Close releases the pending control connection, signer, auth-context watcher,
// and pending timer. It is idempotent and never closes a transferred session.
func (p *PreparedOwner) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.state == preparedOwnerClosed || p.state == preparedOwnerTransferred {
		p.mu.Unlock()
		return
	}
	p.state = preparedOwnerClosed
	wire := p.wire
	signer := p.signer
	stopAuthCancel := p.stopAuthCancel
	expiry := p.expiry
	done := p.done
	p.wire = nil
	p.signer = nil
	p.stopAuthCancel = nil
	p.expiry = nil
	p.authCtx = nil
	p.mu.Unlock()

	if stopAuthCancel != nil {
		stopAuthCancel()
	}
	if expiry != nil {
		expiry.Stop()
	}
	if wire != nil {
		_ = wire.Close()
	}
	if signer != nil {
		signer.Close()
	}
	if done != nil {
		close(done)
	}
}

// Commit exchanges the prepared owner proof for one committed owner session.
func (p *PreparedOwner) Commit(ctx context.Context) (*OwnerSession, error) {
	if p == nil {
		return nil, errors.New("所有者准备状态无效")
	}
	if ctx == nil {
		return nil, errors.New("提交所有者会话的上下文无效")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.mu.Lock()
	if p.state != preparedOwnerPrepared {
		p.mu.Unlock()
		return nil, errors.New("所有者准备状态已失效")
	}
	if p.wire == nil ||
		p.signer == nil ||
		p.authCtx == nil ||
		p.expiry == nil ||
		p.stopAuthCancel == nil ||
		p.authCtx.Err() != nil ||
		!p.stopAuthCancel() ||
		!p.expiry.Stop() {
		p.mu.Unlock()
		p.Close()
		return nil, errors.New("所有者准备状态已失效")
	}
	p.state = preparedOwnerCommitting
	p.stopAuthCancel = nil
	p.expiry = nil
	p.authCtx = nil

	wire := p.wire
	signer := p.signer
	epoch := p.epoch
	minor := p.minor
	generation := p.generation
	pendingID := p.pendingID
	lifetime := p.lifetime
	binding := p.binding
	done := p.done
	p.mu.Unlock()

	stopCommitCancel := context.AfterFunc(ctx, p.Close)
	success := false
	defer func() {
		if !success {
			stopCommitCancel()
			p.Close()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	commit := protocol.ServiceMessage{
		Action:     "commitOwner",
		Major:      protocol.ServiceProtocolMajor,
		Minor:      minor,
		Epoch:      epoch,
		Generation: generation,
		ID:         pendingID,
	}
	if err := serviceSend(ctx, wire, commit); err != nil {
		return nil, serviceCallError(ctx, err)
	}

	var response protocol.ServiceMessage
	defer clearServiceMessageSecrets(&response)
	if err := serviceReceive(ctx, wire, &response); err != nil {
		return nil, serviceCallError(ctx, err)
	}
	now := time.Now()
	hasSecrets := len(response.UnlockKey) != 0 || len(response.Proof) != 0 || len(response.QuickUnlock) != 0
	clearServiceMessageSecrets(&response)

	ownerInfo := response.OwnerInfo
	brokerKeyValid := ownerInfo != nil && len(ownerInfo.BrokerKey) == 71 && ownerInfo.BrokerKey[:7] == "sha256:"
	for index := 7; brokerKeyValid && index < len(ownerInfo.BrokerKey); index++ {
		character := ownerInfo.BrokerKey[index]
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			brokerKeyValid = false
		}
	}
	if response.Action != "ownerCommitted" ||
		response.Major != protocol.ServiceProtocolMajor ||
		response.Minor != minor ||
		response.Epoch != epoch ||
		response.Generation != generation ||
		response.Error != "" ||
		response.ErrorCode != "" ||
		response.OwnerPrepare != nil ||
		response.Challenge != "" ||
		response.ID != "" ||
		response.Status != nil ||
		hasSecrets ||
		ownerInfo == nil ||
		!isLowerHex32(ownerInfo.ID) ||
		ownerInfo.Epoch != epoch ||
		!ownerInfo.Expires.After(now) ||
		ownerInfo.Expires.After(now.Add(lifetime+5*time.Second)) ||
		!brokerKeyValid {
		return nil, errors.New("本地服务返回的所有者提交响应无效")
	}

	info := ownercap.SessionInfo{
		ID:      ownerInfo.ID,
		Epoch:   ownerInfo.Epoch,
		Expires: ownerInfo.Expires,
	}
	if err := signer.Bind(info); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, errors.New("绑定所有者会话签名器失败")
	}

	p.mu.Lock()
	if p.state != preparedOwnerCommitting || ctx.Err() != nil || !stopCommitCancel() {
		p.mu.Unlock()
		p.Close()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("提交所有者会话已失效")
	}
	p.wire = nil
	p.signer = nil
	p.state = preparedOwnerTransferred
	p.mu.Unlock()
	if done != nil {
		close(done)
	}

	session, err := newOwnerSession(wire, signer, info, ownerInfo.BrokerKey, minor, generation, binding)
	if err != nil {
		_ = wire.Close()
		signer.Close()
		return nil, err
	}
	success = true
	return session, nil
}

// PrepareOwner verifies the service executable and protocol, then prepares a
// pending owner session without committing it or changing the service state.
func (c ServiceClient) PrepareOwner(ctx context.Context, source UnlockSource, lifetime time.Duration) (*PreparedOwner, error) {
	if ctx == nil {
		return nil, errors.New("准备所有者会话的上下文无效")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !HasOwnerProof(source) {
		return nil, errors.New("所有者解锁源无效")
	}
	lifetime, err := normalizeOwnerLifetime(lifetime)
	if err != nil {
		return nil, err
	}

	wire, err := localipc.ConnectExecutable(ctx, filepath.Join(c.Dir, "service"), c.Executable)
	if err != nil {
		return nil, serviceCallError(ctx, err)
	}
	wire.WriteTimeout = 0
	prepared := &PreparedOwner{
		state:   preparedOwnerPrepared,
		wire:    wire,
		authCtx: ctx,
		done:    make(chan struct{}),
	}
	prepared.mu.Lock()
	prepared.stopAuthCancel = context.AfterFunc(ctx, prepared.Close)
	prepared.mu.Unlock()
	success := false
	defer func() {
		if !success {
			prepared.Close()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	hello, err := exchangeServiceHello(ctx, wire)
	if err != nil {
		return nil, err
	}
	binding, err := localipc.ChannelBinding(wire.Conn)
	if err != nil {
		return nil, serviceCallError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	signer, err := ownercap.NewSigner()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	prepared.mu.Lock()
	if prepared.state != preparedOwnerPrepared {
		prepared.mu.Unlock()
		signer.Close()
		return nil, preparedOwnerPrepareFailure(ctx)
	}
	prepared.signer = signer
	prepared.mu.Unlock()
	publicKey := signer.PublicKey()
	defer clear(publicKey)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rawKey, err := source.UnlockKey()
	defer clear(rawKey)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, errors.New("读取所有者解锁密钥失败")
	}
	if len(rawKey) != 32 {
		return nil, errors.New("所有者解锁密钥长度无效")
	}
	unlockKey := append([]byte(nil), rawKey...)
	clear(rawKey)
	defer clear(unlockKey)
	digest := sha256.Sum256(unlockKey)
	keyHash := hex.EncodeToString(digest[:])
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ownerPrepare := protocol.OwnerPrepare{
		Epoch:           hello.Epoch,
		Challenge:       hello.Challenge,
		Binding:         binding,
		Generation:      hello.Generation,
		PublicKey:       publicKey,
		LifetimeSeconds: int(lifetime / time.Second),
		KeyHash:         keyHash,
	}
	payload, err := protocol.ServiceOwnerPreparePayload(ownerPrepare)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, errors.New("生成所有者准备数据失败")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	proof, err := source.Proof(payload)
	defer clear(proof)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, errors.New("生成所有者证明失败")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	message := protocol.ServiceMessage{
		Action:       "prepareOwner",
		Major:        protocol.ServiceProtocolMajor,
		Minor:        hello.Minor,
		Epoch:        hello.Epoch,
		Generation:   hello.Generation,
		Challenge:    hello.Challenge,
		OwnerPrepare: &ownerPrepare,
		UnlockKey:    unlockKey,
		Proof:        proof,
	}
	sendErr := serviceSend(ctx, wire, message)
	clearServiceMessageSecrets(&message)
	if sendErr != nil {
		return nil, serviceCallError(ctx, sendErr)
	}

	var response protocol.ServiceMessage
	defer clearServiceMessageSecrets(&response)
	if err := serviceReceive(ctx, wire, &response); err != nil {
		return nil, serviceCallError(ctx, err)
	}
	hasSecrets := len(response.UnlockKey) != 0 || len(response.Proof) != 0 || len(response.QuickUnlock) != 0
	clearServiceMessageSecrets(&response)
	if response.ErrorCode != "" || response.Error != "" {
		return nil, errors.New("本地服务拒绝了所有者准备请求")
	}
	if response.Action != "ownerPrepared" ||
		response.Major != protocol.ServiceProtocolMajor ||
		response.Minor != hello.Minor ||
		response.Epoch != hello.Epoch ||
		response.Generation != hello.Generation ||
		!isLowerHex32(response.ID) ||
		hasSecrets ||
		response.OwnerInfo != nil ||
		response.OwnerPrepare != nil {
		return nil, errors.New("本地服务返回的所有者准备响应无效")
	}
	if err := wire.Conn.SetDeadline(time.Time{}); err != nil {
		return nil, serviceCallError(ctx, err)
	}

	prepared.mu.Lock()
	if prepared.state != preparedOwnerPrepared || ctx.Err() != nil {
		prepared.mu.Unlock()
		return nil, preparedOwnerPrepareFailure(ctx)
	}
	prepared.epoch = hello.Epoch
	prepared.challenge = hello.Challenge
	prepared.generation = hello.Generation
	prepared.minor = hello.Minor
	prepared.binding = binding
	prepared.pendingID = response.ID
	prepared.lifetime = lifetime
	prepared.expiry = time.AfterFunc(ownerPreparedPendingLifetime, prepared.Close)
	success = true
	prepared.mu.Unlock()
	if err := ctx.Err(); err != nil {
		prepared.Close()
		return nil, err
	}
	select {
	case <-prepared.done:
		return nil, preparedOwnerPrepareFailure(ctx)
	default:
	}
	return prepared, nil
}

func normalizeOwnerLifetime(lifetime time.Duration) (time.Duration, error) {
	if lifetime == 0 {
		return ownerPrepareDefaultLifetime, nil
	}
	if lifetime < ownerPrepareMinLifetime || lifetime > ownerPrepareMaxLifetime || lifetime%time.Second != 0 {
		return 0, errors.New("所有者会话期限必须是60秒到8小时的整数秒")
	}
	return lifetime, nil
}

func exchangeServiceHello(ctx context.Context, wire *localipc.Wire) (protocol.ServiceMessage, error) {
	if err := serviceSend(ctx, wire, protocol.ServiceMessage{
		Action: "hello",
		Major:  protocol.ServiceProtocolMajor,
		Minor:  protocol.ServiceProtocolMinor,
	}); err != nil {
		return protocol.ServiceMessage{}, serviceCallError(ctx, err)
	}
	var hello protocol.ServiceMessage
	defer clearServiceMessageSecrets(&hello)
	if err := serviceReceive(ctx, wire, &hello); err != nil {
		return protocol.ServiceMessage{}, serviceCallError(ctx, err)
	}
	hasSecrets := len(hello.UnlockKey) != 0 || len(hello.Proof) != 0 || len(hello.QuickUnlock) != 0
	clearServiceMessageSecrets(&hello)
	if err := serviceResponseError(hello); err != nil {
		return protocol.ServiceMessage{}, err
	}
	if hello.Action != "hello" ||
		hello.Major != protocol.ServiceProtocolMajor ||
		hello.Minor < 1 ||
		hello.Minor > protocol.ServiceProtocolMinor ||
		!isLowerHex32(hello.Epoch) ||
		!isLowerHex32(hello.Challenge) ||
		hello.Generation == 0 ||
		hasSecrets {
		return protocol.ServiceMessage{}, errors.New("本地服务返回了不兼容的版本协商响应")
	}
	return hello, nil
}

func serviceSend(ctx context.Context, wire *localipc.Wire, message protocol.ServiceMessage) error {
	if err := setServiceExchangeDeadline(ctx, wire); err != nil {
		return err
	}
	return wire.Send(message)
}

func serviceReceive(ctx context.Context, wire *localipc.Wire, message *protocol.ServiceMessage) error {
	if err := setServiceExchangeDeadline(ctx, wire); err != nil {
		return err
	}
	return wire.Receive(message)
}

func setServiceExchangeDeadline(ctx context.Context, wire *localipc.Wire) error {
	deadline := time.Now().Add(serviceCallTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	return wire.Conn.SetDeadline(deadline)
}

func clearServiceMessageSecrets(message *protocol.ServiceMessage) {
	if message == nil {
		return
	}
	clear(message.UnlockKey)
	message.UnlockKey = nil
	clear(message.Proof)
	message.Proof = nil
	clear(message.QuickUnlock)
	message.QuickUnlock = nil
}

func isLowerHex32(value string) bool {
	if len(value) != 32 {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func preparedOwnerPrepareFailure(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.New("所有者准备已失效")
}
