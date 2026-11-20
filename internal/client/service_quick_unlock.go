package client

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
)

// ErrQuickUnlockUnavailable means the service holds no envelope this
// executable may use: none was deposited since the service started, it
// expired, or another program deposited it. The master password is needed.
var ErrQuickUnlockUnavailable = errors.New("快速解锁不可用，请输入主密码")

// QuickUnlockStatus asks a running service whether it holds an envelope this
// executable deposited. It never starts the service: a service that is not
// running cannot hold one.
func (c ServiceClient) QuickUnlockStatus(ctx context.Context) (protocol.QuickUnlockState, error) {
	response, err := c.quickUnlockCall(ctx, protocol.ServiceMessage{Action: "quickUnlockStatus"}, "quickUnlockStatus")
	if err != nil {
		return protocol.QuickUnlockState{}, err
	}
	return *response.QuickUnlockState, nil
}

// QuickUnlockRecord returns the envelope this executable deposited. The caller
// owns the returned bytes and must clear them after unwrapping.
func (c ServiceClient) QuickUnlockRecord(ctx context.Context) ([]byte, error) {
	response, err := c.quickUnlockCall(ctx, protocol.ServiceMessage{Action: "quickUnlockRecord"}, "quickUnlockRecord")
	if err != nil {
		return nil, err
	}
	if len(response.QuickUnlock) == 0 || !response.QuickUnlockState.Available {
		clear(response.QuickUnlock)
		return nil, ErrQuickUnlockUnavailable
	}
	return response.QuickUnlock, nil
}

// ClearQuickUnlock drops the envelope this executable deposited, even while
// the vault is locked. It only removes authority.
func (c ServiceClient) ClearQuickUnlock(ctx context.Context) error {
	_, err := c.quickUnlockCall(ctx, protocol.ServiceMessage{Action: "clearQuickUnlock"}, "quickUnlockCleared")
	return err
}

func (c ServiceClient) quickUnlockCall(ctx context.Context, request protocol.ServiceMessage, expected string) (protocol.ServiceMessage, error) {
	// Connect to a running service only: one that is not running holds no envelope.
	wire, err := localipc.ConnectExecutable(ctx, filepath.Join(c.Dir, "service"), c.Executable)
	if err != nil {
		return protocol.ServiceMessage{}, err
	}
	defer wire.Close()
	stop := context.AfterFunc(ctx, func() { _ = wire.Close() })
	defer stop()
	if err := wire.Conn.SetDeadline(time.Now().Add(serviceCallTimeout)); err != nil {
		return protocol.ServiceMessage{}, err
	}
	hello, err := exchangeServiceHello(ctx, wire)
	if err != nil {
		return protocol.ServiceMessage{}, err
	}
	if hello.Minor < 2 {
		return protocol.ServiceMessage{}, ErrQuickUnlockUnavailable
	}
	request.Major, request.Minor, request.Epoch = protocol.ServiceProtocolMajor, hello.Minor, hello.Epoch
	if err := serviceSend(ctx, wire, request); err != nil {
		return protocol.ServiceMessage{}, serviceCallError(ctx, err)
	}
	var response protocol.ServiceMessage
	if err := serviceReceive(ctx, wire, &response); err != nil {
		return protocol.ServiceMessage{}, serviceCallError(ctx, err)
	}
	if response.ErrorCode == "quick_unlock_unavailable" {
		clearServiceMessageSecrets(&response)
		return protocol.ServiceMessage{}, ErrQuickUnlockUnavailable
	}
	if err := serviceResponseError(response); err != nil {
		clearServiceMessageSecrets(&response)
		return protocol.ServiceMessage{}, err
	}
	// Only the record reply may carry the envelope.
	if response.Action != expected || response.Epoch != hello.Epoch || response.QuickUnlockState == nil ||
		len(response.UnlockKey) != 0 || len(response.Proof) != 0 || expected != "quickUnlockRecord" && len(response.QuickUnlock) != 0 {
		clearServiceMessageSecrets(&response)
		return protocol.ServiceMessage{}, errors.New("本地服务快速解锁响应无效")
	}
	return response, nil
}

// DepositQuickUnlock hands the service a freshly wrapped envelope over this
// owner session's authenticated control connection. The service binds it to
// this executable and keeps it in memory only. record is not retained.
func (s *OwnerSession) DepositQuickUnlock(ctx context.Context, record []byte) (protocol.QuickUnlockState, error) {
	if s == nil {
		return protocol.QuickUnlockState{}, errOwnerSessionClosed
	}
	s.mu.Lock()
	minor, epoch, generation := s.minor, s.info.Epoch, s.generation
	s.mu.Unlock()
	if minor < 2 {
		return protocol.QuickUnlockState{}, ErrQuickUnlockUnavailable
	}
	request := protocol.ServiceMessage{
		Action: "depositQuickUnlock", Major: protocol.ServiceProtocolMajor, Minor: minor, Epoch: epoch,
		Generation: generation, QuickUnlock: record,
	}
	response, err := s.exchange(ctx, request, "quickUnlockDeposited", generation, false)
	if err != nil {
		return protocol.QuickUnlockState{}, err
	}
	if err := serviceResponseError(response); err != nil {
		return protocol.QuickUnlockState{}, err
	}
	if response.QuickUnlockState == nil || !response.QuickUnlockState.Available {
		return protocol.QuickUnlockState{}, errors.New("本地服务未保存快速解锁")
	}
	return *response.QuickUnlockState, nil
}
