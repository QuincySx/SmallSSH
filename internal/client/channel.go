package client

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
)

// This registry contains live connections, never credentials that another
// process can reload. A value-copy of Client within this process may use them.
var requestChannels sync.Map

type requestChannel struct {
	wire                     *localipc.Wire
	dir, binding, id, ticket string
	ctx                      context.Context
	cancel                   context.CancelFunc
	mu                       sync.Mutex
	registration             sync.Mutex
	expiry                   *time.Timer
	taken                    bool // protected by mu; the wire now belongs to Stream
}

// brokerKeyProvider is an optional capability of owner sources. Proof-only
// sources must not be forced to expose a vault pin.
type brokerKeyProvider interface {
	BrokerPublicKeyPin() (string, error)
}

func (c Client) connect(ctx context.Context) (*localipc.Wire, error) {
	pin := c.BrokerKey
	if pin == "" && HasOwnerProof(c.Owner) {
		source, ok := c.Owner.(brokerKeyProvider)
		if ok {
			var err error
			pin, err = source.BrokerPublicKeyPin()
			if err != nil {
				return nil, err
			}
		}
	}
	return localipc.ConnectVerified(ctx, c.Dir, pin)
}

func (c Client) newChannel(ctx context.Context) (*requestChannel, error) {
	w, err := c.connect(ctx)
	if err != nil {
		return nil, err
	}
	binding, err := localipc.ChannelBinding(w.Conn)
	if err != nil {
		w.Conn.Close()
		return nil, err
	}
	dir, err := filepath.Abs(c.Dir)
	if err != nil {
		w.Conn.Close()
		return nil, err
	}
	channelCtx, cancel := context.WithCancel(ctx)
	h := &requestChannel{wire: w, dir: dir, binding: binding, ctx: channelCtx, cancel: cancel}
	context.AfterFunc(channelCtx, h.close)
	return h, nil
}

func (h *requestChannel) close() {
	h.cancel()
	_ = h.wire.Close()
	h.registration.Lock()
	defer h.registration.Unlock()
	if h.expiry != nil {
		h.expiry.Stop()
	}
	if h.id != "" {
		requestChannels.CompareAndDelete(h.id, h)
	}
}

func (h *requestChannel) call(ctx context.Context, q protocol.Message) (protocol.Message, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.callLocked(ctx, q)
}

func (h *requestChannel) callLocked(ctx context.Context, q protocol.Message) (protocol.Message, error) {
	if h.taken || h.ctx.Err() != nil {
		return protocol.Message{}, approval.ErrDenied
	}
	stop := context.AfterFunc(ctx, h.close)
	defer stop()
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	h.wire.Conn.SetDeadline(deadline)
	defer h.wire.Conn.SetDeadline(time.Time{})
	if err := h.wire.Send(q); err != nil {
		h.close()
		return protocol.Message{}, err
	}
	var r protocol.Message
	if err := h.wire.Receive(&r); err != nil {
		h.close()
		return r, err
	}
	return r, responseError(r)
}

func (c Client) channel(id, ticket string) *requestChannel {
	v, ok := requestChannels.Load(id)
	if !ok {
		return nil
	}
	h := v.(*requestChannel)
	h.registration.Lock()
	defer h.registration.Unlock()
	dir, err := filepath.Abs(c.Dir)
	if err != nil || h.dir != dir || h.ticket != ticket || h.ctx.Err() != nil {
		return nil
	}
	return h
}

func (h *requestChannel) register(r protocol.Message) error {
	h.registration.Lock()
	defer h.registration.Unlock()
	if h.ctx.Err() != nil {
		return approval.ErrDenied
	}
	if r.Request == nil || r.Ticket == "" || r.Channel != h.binding {
		return errors.New("授权未绑定到当前连接")
	}
	h.id, h.ticket = r.Request.ID, r.Ticket
	requestChannels.Store(h.id, h)
	if r.Request.Operation.Kind != "grant" {
		h.expiry = time.AfterFunc(time.Until(r.Request.Expires), h.close)
	}
	return nil
}

func (c Client) request(ctx context.Context, q protocol.Message) (protocol.Message, error) {
	h, err := c.newChannel(ctx)
	if err != nil {
		if q.Action == "request" && errors.Is(err, localipc.ErrBrokerUnavailable) {
			err = c.notifyVaultLocked(ctx, err)
		}
		return protocol.Message{}, err
	}
	r, err := h.call(ctx, q)
	if err == nil {
		err = h.register(r)
	}
	if err != nil {
		h.close()
	}
	return r, err
}

func (c Client) openOperation(ctx context.Context, d *Delegation) (*requestChannel, error) {
	h, err := c.newChannel(ctx)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(d.state.control.ctx, h.close)
	context.AfterFunc(h.ctx, func() { stop() })
	r, err := h.call(ctx, protocol.Message{Action: "openOperation"})
	if err == nil && (r.Action != "channel" || r.Channel != h.binding) {
		err = errors.New("代理未确认当前操作连接")
	}
	if err != nil {
		h.close()
		return nil, err
	}
	return h, nil
}
