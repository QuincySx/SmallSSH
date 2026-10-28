// Package client is the shared CLI/desktop frontend to the local broker.
package client

import (
	"context"
	"errors"
	"io"
	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/fileattrs"
	"sshclient/internal/localipc"
	"sshclient/internal/monitor"
	"sshclient/internal/sshconn"
	"sshclient/internal/vault"
	"sync"
	"time"
)

type Client struct {
	Dir              string
	Owner            *vault.Session
	Delegation       *Delegation
	OnSessionRenewal func(approval.Request)
}

func (c Client) Call(ctx context.Context, q broker.Message) (broker.Message, error) {
	if c.Owner != nil && broker.OwnerQueryAction(q.Action) && q.Challenge == "" && q.Ticket == "" {
		return c.CallWithOwner(ctx, q, c.Owner)
	}
	return c.call(ctx, q)
}

func (c Client) call(ctx context.Context, q broker.Message) (broker.Message, error) {
	w, e := localipc.Connect(ctx, c.Dir)
	if e != nil {
		return broker.Message{}, e
	}
	defer w.Conn.Close()
	stop := context.AfterFunc(ctx, func() { w.Conn.Close() })
	defer stop()
	w.Conn.SetDeadline(time.Now().Add(15 * time.Second))
	if e = w.Send(q); e != nil {
		return broker.Message{}, e
	}
	var r broker.Message
	e = w.Receive(&r)
	if e == nil && r.Error != "" {
		e = errors.New(r.Error)
		switch r.ErrorCode {
		case "session_expired":
			e = approval.ErrSessionExpired
		case "session_renewing":
			e = approval.ErrSessionRenewing
		}
	}
	return r, e
}
func (c Client) Request(ctx context.Context, op approval.Operation, ttl time.Duration) (approval.Request, string, error) {
	if c.Delegation != nil {
		return c.RequestDelegated(ctx, op, ttl, c.Delegation)
	}
	r, e := c.Call(ctx, broker.Message{Action: "request", Operation: op, TTL: int(ttl / time.Second)})
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
		r, e := c.Call(ctx, broker.Message{Action: "status", ID: id, Ticket: ticket})
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
		default:
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
	RequestID string
	Operation approval.Operation
	Meta      *sshconn.TransferMeta
	wire      *localipc.Wire
	stop      func() bool
	once      sync.Once
}

func (c Client) Start(ctx context.Context, id, ticket string, cols, rows int) (*Stream, error) {
	w, e := localipc.Connect(ctx, c.Dir)
	if e != nil {
		return nil, e
	}
	stop := context.AfterFunc(ctx, func() { w.Conn.Close() })
	if e = w.Send(broker.Message{Action: "run", ID: id, Ticket: ticket, Cols: cols, Rows: rows}); e != nil {
		stop()
		w.Conn.Close()
		return nil, e
	}
	var r broker.Message
	if e = w.Receive(&r); e != nil || r.Action != "ready" {
		stop()
		w.Conn.Close()
		if e == nil {
			e = errors.New(r.Error)
		}
		return nil, e
	}
	return &Stream{wire: w, stop: stop, Meta: r.Transfer, RequestID: id}, nil
}
func (s *Stream) Close() error { s.once.Do(func() { s.stop(); s.wire.Conn.Close() }); return nil }
func (s *Stream) Write(p []byte) (int, error) {
	n := 0
	for len(p) > 0 {
		k := min(48<<10, len(p))
		if e := s.wire.Send(broker.Message{Action: "data", Data: p[:k]}); e != nil {
			return n, e
		}
		n += k
		p = p[k:]
	}
	return n, nil
}
func (s *Stream) End() error { return s.wire.Send(broker.Message{Action: "end"}) }

// SetMonitorActive tells the broker whether this session's server status is still
// on screen. The broker probes only while some subscriber shows it, so a
// collapsed panel or an inactive tab costs nothing on the server.
func (s *Stream) SetMonitorActive(active bool) error {
	return s.wire.Send(broker.Message{Action: "monitor", MonitorActive: active})
}

func (s *Stream) Resize(cols, rows int) error {
	return s.wire.Send(broker.Message{Action: "resize", Cols: cols, Rows: rows})
}
func (s *Stream) Receive() (broker.Message, error) {
	var r broker.Message
	e := s.wire.Receive(&r)
	if e == nil && r.Error != "" {
		e = errors.New(r.Error)
	}
	return r, e
}
func (s *Stream) Copy(out, stderr io.Writer) (int, []sshconn.File, error) {
	var files []sshconn.File
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
