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
	wire, err := localipc.ConnectExecutable(ctx, filepath.Join(c.Dir, "service"), c.Executable)
	if err != nil {
		return protocol.ServiceStatus{}, err
	}
	defer wire.Close()
	stop := context.AfterFunc(ctx, func() { _ = wire.Close() })
	defer stop()
	if err := wire.Conn.SetDeadline(time.Now().Add(serviceCallTimeout)); err != nil {
		return protocol.ServiceStatus{}, err
	}
	if err := wire.Send(protocol.ServiceMessage{
		Action: "hello", Major: protocol.ServiceProtocolMajor, Minor: protocol.ServiceProtocolMinor,
	}); err != nil {
		return protocol.ServiceStatus{}, serviceCallError(ctx, err)
	}
	var hello protocol.ServiceMessage
	if err := wire.Receive(&hello); err != nil {
		return protocol.ServiceStatus{}, serviceCallError(ctx, err)
	}
	if err := serviceResponseError(hello); err != nil {
		return protocol.ServiceStatus{}, err
	}
	if hello.Action != "hello" || hello.Major != protocol.ServiceProtocolMajor || hello.Minor < 0 || hello.Epoch == "" {
		return protocol.ServiceStatus{}, errors.New("本地服务返回了不兼容的版本协商响应")
	}
	minor := hello.Minor
	if minor > protocol.ServiceProtocolMinor {
		minor = protocol.ServiceProtocolMinor
	}
	if err := wire.Conn.SetDeadline(time.Now().Add(serviceCallTimeout)); err != nil {
		return protocol.ServiceStatus{}, err
	}
	if err := wire.Send(protocol.ServiceMessage{
		Action: "status", Major: protocol.ServiceProtocolMajor, Minor: minor, Epoch: hello.Epoch,
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
	if response.Action != "status" || response.Epoch != hello.Epoch || response.Status == nil {
		return protocol.ServiceStatus{}, errors.New("本地服务状态响应无效")
	}
	status := *response.Status
	switch status.State {
	case protocol.ServiceStateLocked, protocol.ServiceStateUnlocked, protocol.ServiceStateStopping:
	default:
		return protocol.ServiceStatus{}, errors.New("本地服务返回了无效状态")
	}
	if status.ProtocolMajor != protocol.ServiceProtocolMajor || status.ProtocolMinor != minor {
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
