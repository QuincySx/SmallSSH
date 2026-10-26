package systemauth

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"time"
)

const embeddedMessageLimit = 65536

// EmbeddedRect is a rectangle in window-content points, with a top-left origin.
type EmbeddedRect struct {
	X, Y, Width, Height float64
}

func (r EmbeddedRect) valid() bool {
	return !math.IsNaN(r.X) && !math.IsNaN(r.Y) && !math.IsNaN(r.Width) && !math.IsNaN(r.Height) &&
		!math.IsInf(r.X, 0) && !math.IsInf(r.Y, 0) && !math.IsInf(r.Width, 0) && !math.IsInf(r.Height, 0) &&
		r.Width > 0 && r.Height > 0
}

// EmbeddedPresentation tracks a live window and its biometric-view rectangle.
// runOnMain must run the callback on the UI thread and wait for it to return.
// The desktop supplies its UI dispatcher without coupling this package to it.
type EmbeddedPresentation struct {
	mu        sync.Mutex
	runOnMain func(func())
	window    uintptr
	rect      EmbeddedRect
	closed    bool
	active    *embeddedSession
}

func NewEmbeddedPresentation(runOnMain func(func())) *EmbeddedPresentation {
	return &EmbeddedPresentation{runOnMain: runOnMain}
}

func (p *EmbeddedPresentation) readyLocked() bool {
	return !p.closed && p.runOnMain != nil && p.window != 0 && p.rect.valid()
}

// Ready reports whether a current window and a usable layout frame are known.
func (p *EmbeddedPresentation) Ready() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.readyLocked()
}

// SetFrame updates the current host. Detaching or replacing the window cancels
// an active operation instead of using a saved, potentially stale native handle.
func (p *EmbeddedPresentation) SetFrame(window uintptr, rect EmbeddedRect) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.closed || (p.window == window && p.rect == rect) {
		p.mu.Unlock()
		return
	}
	p.window, p.rect = window, rect
	s := p.active
	if s != nil && (!p.readyLocked() || window != s.window) {
		s.mu.Lock()
		if !s.released {
			s.cancelLocked()
		}
		s.mu.Unlock()
	}
	p.mu.Unlock()
	if s != nil {
		s.updateFrame()
	}
}

// Close permanently detaches this presentation and cancels its native session.
func (p *EmbeddedPresentation) Close() {
	p.cancelActive(true)
}

// Cancel synchronously releases the current verification while keeping its
// window and frame ready for an immediate retry.
func (p *EmbeddedPresentation) Cancel() {
	p.cancelActive(false)
}

func (p *EmbeddedPresentation) cancelActive(closing bool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if closing {
		p.closed, p.window = true, 0
	}
	s := p.active
	if s != nil {
		s.mu.Lock()
		if !s.released {
			s.cancelLocked()
		}
		s.mu.Unlock()
	}
	p.mu.Unlock()
	if s != nil {
		s.finish(true)
	}
}

type embeddedBackend interface {
	UnwrapEmbedded(context.Context, json.RawMessage, string, *EmbeddedPresentation) ([]byte, error)
}

// UI methods are called only through the presentation's synchronous dispatcher.
// Poll alone runs in the background and must never present authentication UI.
type embeddedBridge interface {
	Start(uintptr, []byte, EmbeddedRect) uint64
	Move(uint64, EmbeddedRect)
	Cancel(uint64)
	Poll(uint64, []byte) int32
	Release(uint64)
}

type embeddedSession struct {
	mu        sync.Mutex
	ctx       context.Context
	p         *EmbeddedPresentation
	bridge    embeddedBridge
	window    uintptr
	token     uint64
	released  bool
	cancelled chan struct{}
	isCancel  bool
}

func (s *embeddedSession) cancelLocked() {
	if !s.isCancel {
		s.isCancel = true
		close(s.cancelled)
	}
}

func (s *embeddedSession) releaseLocked(cancel bool) {
	if s.released {
		return
	}
	if cancel || s.ctx.Err() != nil {
		s.cancelLocked()
	}
	if s.isCancel {
		s.bridge.Cancel(s.token)
	}
	s.bridge.Release(s.token)
	s.released, s.token = true, 0
	if s.p.active == s {
		s.p.active = nil
	}
}

func (s *embeddedSession) finish(cancel bool) bool {
	// Signal cancellation before waiting for the UI thread, so a concurrent poll
	// can never accept a late key while the close callback is queued.
	s.mu.Lock()
	if s.released {
		s.mu.Unlock()
		return true
	}
	if cancel {
		s.cancelLocked()
	}
	s.mu.Unlock()
	executed := false
	s.p.runOnMain(func() {
		executed = true
		s.p.mu.Lock()
		defer s.p.mu.Unlock()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.releaseLocked(cancel)
	})
	if !executed {
		s.mu.Lock()
		s.cancelLocked()
		s.mu.Unlock()
	}
	return executed
}

func (s *embeddedSession) updateFrame() {
	s.p.runOnMain(func() {
		s.p.mu.Lock()
		defer s.p.mu.Unlock()
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.released {
			return
		}
		if !s.p.readyLocked() || s.p.window != s.window || s.ctx.Err() != nil {
			s.releaseLocked(true)
			return
		}
		s.bridge.Move(s.token, s.p.rect)
	})
}

func startEmbedded(ctx context.Context, p *EmbeddedPresentation, bridge embeddedBridge, input []byte) (*embeddedSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !p.Ready() {
		return nil, &EmbeddedError{Code: "notReady", Message: "窗口内指纹验证区域尚未就绪"}
	}
	s := &embeddedSession{ctx: ctx, p: p, bridge: bridge, cancelled: make(chan struct{})}
	var err error
	executed := false
	p.runOnMain(func() {
		executed = true
		p.mu.Lock()
		defer p.mu.Unlock()
		if err = ctx.Err(); err != nil {
			return
		}
		if !p.readyLocked() {
			err = &EmbeddedError{Code: "notReady", Message: "窗口内指纹验证区域已关闭"}
			return
		}
		if p.active != nil {
			err = errors.New("窗口内指纹验证正在进行")
			return
		}
		s.window = p.window
		s.token = bridge.Start(p.window, input, p.rect)
		if s.token == 0 {
			err = errors.New("无法启动窗口内指纹验证")
			return
		}
		p.active = s
	})
	if !executed {
		return nil, errors.New("窗口内指纹验证的界面线程已停止")
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

func embeddedInput(raw json.RawMessage, reason string) ([]byte, error) {
	if len(raw) == 0 || len(raw) > embeddedMessageLimit {
		return nil, errors.New("系统解锁数据无效")
	}
	var blob []byte
	if err := json.Unmarshal(raw, &blob); err != nil || len(blob) == 0 {
		wipe(blob)
		return nil, errors.New("系统解锁数据无效")
	}
	defer wipe(blob)
	input, err := json.Marshal(struct {
		Action string `json:"action"`
		Blob   []byte `json:"blob"`
		Reason string `json:"reason"`
	}{"unwrap", blob, reason})
	if err != nil {
		return nil, err
	}
	if len(input) > embeddedMessageLimit {
		wipe(input)
		return nil, errors.New("认证组件请求过大")
	}
	return input, nil
}

func embeddedKey(output []byte) ([]byte, error) {
	var reply struct {
		Key          []byte          `json:"key"`
		Error        string          `json:"error"`
		ErrorCode    json.RawMessage `json:"errorCode"`
		NativeDomain string          `json:"nativeDomain"`
		NativeCode   int64           `json:"nativeCode"`
	}
	err := json.Unmarshal(output, &reply)
	if err == nil {
		if len(reply.ErrorCode) > 0 {
			var code string
			if json.Unmarshal(reply.ErrorCode, &code) != nil || code == "" {
				err = errors.New("认证组件错误类型无效")
			} else {
				err = &EmbeddedError{Code: code, Message: reply.Error, NativeDomain: reply.NativeDomain, NativeCode: reply.NativeCode}
			}
		} else if reply.Error != "" {
			err = errors.New(reply.Error)
		}
	}
	if err == nil && len(reply.Key) != 32 {
		err = errors.New("设备密钥长度错误")
	}
	if err != nil {
		wipe(reply.Key)
		return nil, err
	}
	return reply.Key, nil
}

func runEmbedded(ctx context.Context, p *EmbeddedPresentation, bridge embeddedBridge, input []byte) ([]byte, error) {
	if len(input) == 0 || len(input) > embeddedMessageLimit || !json.Valid(input) {
		return nil, errors.New("认证组件请求无效")
	}
	s, err := startEmbedded(ctx, p, bridge, input)
	if err != nil {
		return nil, err
	}
	defer s.finish(true)
	output := make([]byte, embeddedMessageLimit)
	defer wipe(output)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		select {
		case <-s.cancelled:
			return nil, &EmbeddedError{Code: "cancelled", Message: "窗口内指纹验证已取消"}
		default:
		}
		s.mu.Lock()
		var n int32
		if !s.released {
			n = bridge.Poll(s.token, output)
		}
		s.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		select {
		case <-s.cancelled:
			return nil, &EmbeddedError{Code: "cancelled", Message: "窗口内指纹验证已取消"}
		default:
		}
		if n < 0 || n > int32(len(output)) {
			return nil, errors.New("认证组件响应无效或过大")
		}
		if n > 0 {
			key, err := embeddedKey(output[:n])
			if err != nil {
				return nil, err
			}
			if !s.finish(false) {
				wipe(key)
				return nil, errors.New("窗口内指纹验证的界面线程已停止")
			}
			if ctx.Err() != nil {
				wipe(key)
				return nil, ctx.Err()
			}
			select {
			case <-s.cancelled:
				wipe(key)
				return nil, &EmbeddedError{Code: "cancelled", Message: "窗口内指纹验证已取消"}
			default:
				return key, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.cancelled:
			return nil, &EmbeddedError{Code: "cancelled", Message: "窗口内指纹验证已取消"}
		case <-ticker.C:
		}
	}
}
