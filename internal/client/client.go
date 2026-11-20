// Package client is the shared CLI/desktop frontend to the local protocol.
package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sshclient/internal/approval"
	"sshclient/internal/fileattrs"
	"sshclient/internal/localipc"
	"sshclient/internal/monitor"
	"sshclient/internal/protocol"
	"sshclient/internal/sshdata"
	"sync"
	"time"
)

type Client struct {
	Dir               string
	BrokerKey         string
	Owner             OwnerProof
	Delegation        *Delegation
	OnSessionRenewal  func(approval.Request)
	ServiceExecutable string // optional trusted companion path for unlock notices
}

func (c Client) Call(ctx context.Context, q protocol.Message) (protocol.Message, error) {
	if q.Action == "request" || q.Action == "ownerRequest" {
		return c.request(ctx, q)
	}
	if q.Ticket != "" && (q.Action == "status" || q.Action == "sessionStatus" || q.Action == "withdraw") {
		if h := c.channel(q.ID, q.Ticket); h != nil {
			r, err := h.call(ctx, q)
			if q.Action == "withdraw" {
				h.close()
			}
			return r, err
		}
		return protocol.Message{}, approval.ErrDenied
	}
	if HasOwnerProof(c.Owner) && protocol.OwnerQueryAction(q.Action) && q.Challenge == "" && q.Ticket == "" {
		return c.CallWithOwner(ctx, q, c.Owner)
	}
	return c.call(ctx, q)
}

func (c Client) call(ctx context.Context, q protocol.Message) (protocol.Message, error) {
	w, e := c.connect(ctx)
	if e != nil {
		return protocol.Message{}, e
	}
	defer w.Conn.Close()
	stop := context.AfterFunc(ctx, func() { w.Conn.Close() })
	defer stop()
	timeout := 15 * time.Second
	if q.Action == "workspaceWrite" && q.WorkspaceChange != nil && q.WorkspaceChange.Kind == "ssh-identity-change" {
		timeout = 2 * time.Minute
	}
	w.Conn.SetDeadline(time.Now().Add(timeout))
	if e = w.Send(q); e != nil {
		return protocol.Message{}, e
	}
	var r protocol.Message
	e = w.Receive(&r)
	if e == nil {
		e = responseError(r)
	}
	return r, e
}

func responseError(r protocol.Message) error {
	var e error
	if r.Error != "" {
		e = errors.New(r.Error)
		switch r.ErrorCode {
		case protocol.ErrorUploadTargetExists:
			e = sshdata.ErrUploadTargetExists
		case "session_expired":
			e = approval.ErrSessionExpired
		case "session_renewing":
			e = approval.ErrSessionRenewing
		case "operation_review_pending":
			e = approval.ErrReviewPending
		case "automation_policy":
			// Keep the refusal recognisable: a caller may be able to ask for a
			// narrower, allowed operation instead of failing outright.
			e = fmt.Errorf("%w: %s", ErrAutomationPolicy, r.Error)
		}
	}
	return e
}

// ErrAutomationPolicy marks a refusal by the owner's persistent automation
// policy, which no approval can override.
var ErrAutomationPolicy = errors.New("自动化策略拒绝")

// ErrOperationDenied means the owner declined this operation, not its session.
var ErrOperationDenied = errors.New("本次操作已拒绝")

func (c Client) Request(ctx context.Context, op approval.Operation, ttl time.Duration) (approval.Request, string, error) {
	if c.Delegation != nil {
		return c.RequestDelegated(ctx, op, ttl, c.Delegation)
	}
	r, e := c.request(ctx, protocol.Message{Action: "request", Operation: op, TTL: int(ttl / time.Second)})
	if e != nil {
		return approval.Request{}, "", e
	}
	if r.Request == nil {
		return approval.Request{}, "", errors.New("代理未返回授权请求")
	}
	return *r.Request, r.Ticket, nil
}
func (c Client) Wait(ctx context.Context, id, ticket string) error {
	t := time.NewTicker(350 * time.Millisecond)
	defer t.Stop()
	for {
		r, e := c.Call(ctx, protocol.Message{Action: "status", ID: id, Ticket: ticket})
		if e != nil {
			return e
		}
		if r.Request == nil {
			return approval.ErrDenied
		}
		switch r.Request.Status {
		case "approved":
			return nil
		case "pending":
		case "denied":
			if h := c.channel(id, ticket); h != nil {
				h.close()
			}
			return fmt.Errorf("%w: %w", ErrOperationDenied, approval.ErrDenied)
		default:
			if h := c.channel(id, ticket); h != nil {
				h.close()
			}
			return approval.ErrDenied
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

type Stream struct {
	ListenAddress string
	RequestID     string
	Operation     approval.Operation
	Meta          *sshdata.TransferMeta
	wire          *localipc.Wire
	stop          func() bool
	once          sync.Once
}

func (c Client) Start(ctx context.Context, id, ticket string, cols, rows int) (*Stream, error) {
	h := c.channel(id, ticket)
	if h == nil {
		return nil, approval.ErrDenied
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	status, err := h.callLocked(ctx, protocol.Message{Action: "status", ID: id, Ticket: ticket})
	if err != nil {
		return nil, err
	}
	if status.Request == nil || status.Request.Status != "approved" || status.Request.Operation.Kind == "grant" {
		return nil, approval.ErrDenied
	}
	if !requestChannels.CompareAndDelete(id, h) {
		return nil, approval.ErrDenied
	}
	h.taken = true
	if status.Request.OwnerSession {
		h.registration.Lock()
		if h.expiry != nil {
			h.expiry.Stop()
		}
		h.registration.Unlock()
	}
	w := h.wire
	stop := context.AfterFunc(ctx, h.close)
	var e error
	if e = w.Send(protocol.Message{Action: "run", ID: id, Ticket: ticket, Cols: cols, Rows: rows}); e != nil {
		stop()
		h.close()
		return nil, e
	}
	var r protocol.Message
	if e = w.Receive(&r); e != nil || r.Action != "ready" {
		stop()
		h.close()
		if e == nil {
			e = responseError(r)
			if e == nil {
				e = errors.New("代理未准备好执行操作")
			}
		}
		return nil, e
	}
	return &Stream{wire: w, stop: func() bool { stopped := stop(); h.close(); return stopped }, Meta: r.Transfer, RequestID: id, ListenAddress: r.ListenAddress}, nil
}
func (s *Stream) Close() error { s.once.Do(func() { s.stop(); s.wire.Conn.Close() }); return nil }
func (s *Stream) Write(p []byte) (int, error) {
	n := 0
	for len(p) > 0 {
		k := min(48<<10, len(p))
		if e := s.wire.Send(protocol.Message{Action: "data", Data: p[:k]}); e != nil {
			return n, e
		}
		n += k
		p = p[k:]
	}
	return n, nil
}
func (s *Stream) End() error { return s.wire.Send(protocol.Message{Action: "end"}) }

// SetMonitorActive tells the broker whether this session's server status is still
// on screen. The broker probes only while some subscriber shows it, so a
// collapsed panel or an inactive tab costs nothing on the server.
func (s *Stream) SetMonitorActive(active bool) error {
	return s.wire.Send(protocol.Message{Action: "monitor", MonitorActive: active})
}

func (s *Stream) Resize(cols, rows int) error {
	return s.wire.Send(protocol.Message{Action: "resize", Cols: cols, Rows: rows})
}
func (s *Stream) Receive() (protocol.Message, error) {
	var r protocol.Message
	e := s.wire.Receive(&r)
	if e == nil {
		e = responseError(r)
	}
	return r, e
}
func (s *Stream) Copy(out, stderr io.Writer) (int, []sshdata.File, error) {
	var files []sshdata.File
	for {
		m, e := s.Receive()
		if e != nil {
			return 0, files, e
		}
		switch m.Action {
		case "stdout", "data":
			if _, e = out.Write(m.Data); e != nil {
				return 0, files, e
			}
		case "stderr":
			if _, e = stderr.Write(m.Data); e != nil {
				return 0, files, e
			}
		case "files":
			files = append(files, m.Files...)
		case "done":
			return m.ExitCode, files, nil
		}
	}
}

// ReadFileInfo retains observed metadata even when a later write step fails.
func (s *Stream) ReadFileInfo() (fileattrs.Info, error) {
	var info fileattrs.Info
	for {
		m, err := s.Receive()
		if err != nil {
			return info, err
		}
		if m.Action == "file-info" && m.FileInfo != nil {
			info = *m.FileInfo
		}
		if m.Action == "done" {
			if !info.Valid {
				return info, errors.New("代理未返回文件属性")
			}
			return info, nil
		}
	}
}

// TerminalConn adapts a framed broker stream to Ghostty's io.ReadWriteCloser.
type TerminalConn struct {
	*Stream
	pending []byte
	// Callbacks run on the terminal reader goroutine and must not block it.
	OnStats        func(*monitor.Stats)
	OnMonitorError func(string)
}

func (t *TerminalConn) Read(p []byte) (int, error) {
	for len(t.pending) == 0 {
		m, e := t.Receive()
		if e != nil {
			return 0, e
		}
		if m.Action == "done" {
			return 0, io.EOF
		}
		if m.Action == "stats" {
			if t.OnStats != nil && m.Stats != nil {
				t.OnStats(m.Stats)
			}
			continue
		}
		if m.Action == "monitor-error" {
			if t.OnMonitorError != nil {
				t.OnMonitorError(m.MonitorError)
			}
			continue
		}
		t.pending = m.Data
	}
	n := copy(p, t.pending)
	t.pending = t.pending[n:]
	return n, nil
}
