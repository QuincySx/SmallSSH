package client

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
)

const serviceCallTimeout = 10 * time.Second

type ServiceClient struct {
	Dir        string
	Executable string
}

func (c ServiceClient) Status(ctx context.Context) (protocol.ServiceStatus, error) {
	reader, err := c.OpenStatusReader(ctx)
	if err != nil {
		return protocol.ServiceStatus{}, err
	}
	defer reader.Close()
	return reader.Status(ctx)
}

// RequestUnlock asks the locked service to notify the desktop. The result
// says whether the service is locked. This call carries no vault or SSH data.
func (c ServiceClient) RequestUnlock(ctx context.Context) (bool, error) {
	reader, err := c.OpenStatusReader(ctx)
	if err != nil {
		return false, err
	}
	defer reader.Close()
	if reader.minor < protocol.ServiceUnlockNoticeMinor {
		return false, errors.New("当前本地服务不支持解锁提示")
	}
	status, err := reader.status(ctx, protocol.ServiceActionRequestUnlock)
	return status.State == protocol.ServiceStateLocked, err
}

// ServiceStatusReader reuses one verified connection for serial status queries.
// Close it after an error or when the caller no longer needs status updates.
type ServiceStatusReader struct {
	wire       *localipc.Wire
	epoch      string
	minor      int
	generation uint64
}

func (c ServiceClient) OpenStatusReader(ctx context.Context) (*ServiceStatusReader, error) {
	wire, err := localipc.ConnectExecutable(ctx, filepath.Join(c.Dir, "service"), c.Executable)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = wire.Close()
		}
	}()
	stop := context.AfterFunc(ctx, func() { _ = wire.Close() })
	defer stop()
	if err := wire.Conn.SetDeadline(time.Now().Add(serviceCallTimeout)); err != nil {
		return nil, err
	}
	if err := wire.Send(protocol.ServiceMessage{
		Action: "hello", Major: protocol.ServiceProtocolMajor, Minor: protocol.ServiceProtocolMinor,
	}); err != nil {
		return nil, serviceCallError(ctx, err)
	}
	var hello protocol.ServiceMessage
	if err := wire.Receive(&hello); err != nil {
		return nil, serviceCallError(ctx, err)
	}
	if err := serviceResponseError(hello); err != nil {
		return nil, err
	}
	if hello.Action != "hello" || hello.Major != protocol.ServiceProtocolMajor || hello.Minor < 0 || hello.Epoch == "" {
		return nil, errors.New("本地服务返回了不兼容的版本协商响应")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	keep = true
	return &ServiceStatusReader{wire: wire, epoch: hello.Epoch, minor: min(hello.Minor, protocol.ServiceProtocolMinor), generation: hello.Generation}, nil
}

func (r *ServiceStatusReader) Close() {
	if r != nil {
		_ = r.wire.Close()
	}
}

func (r *ServiceStatusReader) Status(ctx context.Context) (protocol.ServiceStatus, error) {
	return r.status(ctx, "status")
}

func (r *ServiceStatusReader) status(ctx context.Context, action string) (protocol.ServiceStatus, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ServiceStatus{}, err
	}
	wire := r.wire
	stop := context.AfterFunc(ctx, func() { _ = wire.Close() })
	defer stop()
	if err := wire.Conn.SetDeadline(time.Now().Add(serviceCallTimeout)); err != nil {
		return protocol.ServiceStatus{}, err
	}
	if err := wire.Send(protocol.ServiceMessage{
		Action: action, Major: protocol.ServiceProtocolMajor, Minor: r.minor, Epoch: r.epoch, Generation: r.generation,
	}); err != nil {
		return protocol.ServiceStatus{}, serviceCallError(ctx, err)
	}
	var response protocol.ServiceMessage
	if err := wire.Receive(&response); err != nil {
		return protocol.ServiceStatus{}, serviceCallError(ctx, err)
	}
	if err := serviceResponseError(response); err != nil {
		return protocol.ServiceStatus{}, err
	}
	if response.Action != action || response.Epoch != r.epoch || response.Status == nil {
		return protocol.ServiceStatus{}, errors.New("本地服务状态响应无效")
	}
	status := *response.Status
	switch status.State {
	case protocol.ServiceStateLocked, protocol.ServiceStateUnlocked, protocol.ServiceStateStopping:
	default:
		return protocol.ServiceStatus{}, errors.New("本地服务返回了无效状态")
	}
	if status.ProtocolMajor != protocol.ServiceProtocolMajor || status.ProtocolMinor != r.minor {
		return protocol.ServiceStatus{}, errors.New("本地服务状态版本不一致")
	}
	return status, nil
}

func serviceCallError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

func serviceResponseError(message protocol.ServiceMessage) error {
	if message.ErrorCode == "" && message.Error == "" {
		return nil
	}
	if message.ErrorCode != "" && message.Error != "" {
		return fmt.Errorf("本地服务 %s: %s", message.ErrorCode, message.Error)
	}
	if message.ErrorCode != "" {
		return errors.New("本地服务错误: " + message.ErrorCode)
	}
	return errors.New(message.Error)
}
