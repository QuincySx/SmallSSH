package systemauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sshclient/internal/vault"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeEmbeddedBridge struct {
	mu                               sync.Mutex
	t                                *testing.T
	starts, moves, cancels, releases int
	active                           bool
	zeroToken                        bool
	window                           uintptr
	rect                             EmbeddedRect
	input, output, result            []byte
	size                             int32
	onStart, onPoll                  func()
}

func (b *fakeEmbeddedBridge) Start(window uintptr, input []byte, rect EmbeddedRect) uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.starts++
	b.window, b.rect, b.input = window, rect, append([]byte(nil), input...)
	if b.onStart != nil {
		b.onStart()
	}
	if b.zeroToken {
		return 0
	}
	if b.active {
		b.t.Error("duplicate active native token")
	}
	b.active = true
	return 7
}
func (b *fakeEmbeddedBridge) requireToken(token uint64) {
	if token != 7 || !b.active {
		b.t.Errorf("invalid or released native token: %d", token)
	}
}
func (b *fakeEmbeddedBridge) Move(token uint64, rect EmbeddedRect) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.requireToken(token)
	b.moves++
	b.rect = rect
}
func (b *fakeEmbeddedBridge) Cancel(token uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.requireToken(token)
	b.cancels++
}
func (b *fakeEmbeddedBridge) Poll(token uint64, output []byte) int32 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.requireToken(token)
	b.output = output
	copy(output, b.result)
	if b.onPoll != nil {
		b.onPoll()
	}
	if b.size != 0 {
		return b.size
	}
	return int32(len(b.result))
}
func (b *fakeEmbeddedBridge) Release(token uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.requireToken(token)
	b.releases++
	b.active = false
}

func fakePresentation() *EmbeddedPresentation {
	p := NewEmbeddedPresentation(func(fn func()) { fn() })
	p.SetFrame(42, EmbeddedRect{10, 20, 240, 90})
	return p
}

func fakeReply(key []byte) []byte {
	b, _ := json.Marshal(map[string]any{"key": key, "supported": true, "touchID": true})
	return b
}

func TestEmbeddedRejectsMissingPresentationAndCancelledStart(t *testing.T) {
	for _, name := range []string{"nil", "unattached", "closed", "invalid-frame", "cancelled", "dropped-dispatch", "cancelled-dispatch"} {
		t.Run(name, func(t *testing.T) {
			p := fakePresentation()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch name {
			case "nil":
				p = nil
			case "unattached":
				p = NewEmbeddedPresentation(func(fn func()) { fn() })
			case "closed":
				p.Close()
			case "invalid-frame":
				p.SetFrame(42, EmbeddedRect{Width: math.Inf(1), Height: 20})
			case "cancelled":
				cancel()
			case "dropped-dispatch":
				p.runOnMain = func(func()) {}
			case "cancelled-dispatch":
				p.runOnMain = func(fn func()) { cancel(); fn() }
			}
			b := &fakeEmbeddedBridge{t: t}
			key, err := runEmbedded(ctx, p, b, []byte(`{"action":"unwrap"}`))
			if err == nil || len(key) != 0 || b.starts != 0 || b.releases != 0 {
				t.Fatalf("invalid start accepted: err=%v starts=%d releases=%d", err, b.starts, b.releases)
			}
			switch name {
			case "nil", "unattached", "closed", "invalid-frame":
				if !IsAuthenticationNotReady(err) {
					t.Fatal("missing presentation was not classified as not ready", err)
				}
			case "cancelled", "cancelled-dispatch":
				if !IsAuthenticationCancelled(err) {
					t.Fatal("cancelled start lost cancellation reason", err)
				}
			}
		})
	}
}

func TestEmbeddedResponseBoundsCancellationAndTokenLifetime(t *testing.T) {
	for _, name := range []string{"success", "zero-token", "cancel-during-start", "late-poll-success", "cancel-on-release", "close-on-release", "drop-on-release", "invalid-json", "trailing-json", "short-key", "long-key", "error-with-key", "negative-size", "oversized-size"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := fakePresentation()
			want := bytes.Repeat([]byte{37}, 32)
			b := &fakeEmbeddedBridge{t: t, result: fakeReply(want)}
			switch name {
			case "zero-token":
				b.zeroToken = true
			case "cancel-during-start":
				b.onStart = cancel
			case "late-poll-success":
				b.onPoll = cancel
			case "cancel-on-release", "close-on-release", "drop-on-release":
				calls := 0
				p.runOnMain = func(fn func()) {
					calls++
					if calls == 2 {
						if name == "drop-on-release" {
							return
						}
						if name == "close-on-release" {
							p.Close()
						} else {
							cancel()
						}
					}
					fn()
				}
			case "invalid-json":
				b.result = []byte(`{"key":`)
			case "trailing-json":
				b.result = append(b.result, []byte(` {}`)...)
			case "short-key":
				b.result = fakeReply(want[:31])
			case "long-key":
				b.result = fakeReply(append(want, 1))
			case "error-with-key":
				b.result, _ = json.Marshal(map[string]any{"key": want, "error": "cancelled"})
			case "negative-size":
				b.size = -65537
			case "oversized-size":
				b.size = 65537
			}
			key, err := runEmbedded(ctx, p, b, []byte(`{"action":"unwrap"}`))
			defer wipe(key)
			if name == "success" {
				if err != nil || !bytes.Equal(key, want) || b.cancels != 0 {
					t.Fatalf("successful verification failed: %v", err)
				}
			} else if err == nil || key != nil {
				t.Fatalf("invalid or cancelled response accepted: %v", err)
			}
			if b.starts != 1 || b.active {
				t.Fatalf("native lifecycle: starts=%d active=%v", b.starts, b.active)
			}
			if name == "zero-token" {
				if b.releases != 0 || b.cancels != 0 {
					t.Fatal("zero token passed to cleanup")
				}
			} else if b.releases != 1 {
				t.Fatalf("token released %d times", b.releases)
			}
			if !bytes.Equal(b.output, make([]byte, len(b.output))) {
				t.Fatal("native output buffer retained key bytes")
			}
		})
	}
}

func TestEmbeddedUsesLatestHostAndRejectsConcurrentSecondToken(t *testing.T) {
	p := fakePresentation()
	latest := EmbeddedRect{4, 8, 200, 80}
	calls := 0
	var uiMu sync.Mutex
	p.runOnMain = func(fn func()) {
		uiMu.Lock()
		defer uiMu.Unlock()
		calls++
		if calls == 1 {
			p.SetFrame(77, latest)
		}
		fn()
	}
	started := make(chan struct{})
	b := &fakeEmbeddedBridge{t: t, onStart: func() { close(started) }}
	done := make(chan error, 1)
	go func() {
		_, err := runEmbedded(context.Background(), p, b, []byte(`{"action":"unwrap"}`))
		done <- err
	}()
	<-started
	if key, err := runEmbedded(context.Background(), p, b, []byte(`{"action":"unwrap"}`)); err == nil || key != nil {
		t.Fatal("concurrent verification generated a second token")
	}
	b.mu.Lock()
	if b.starts != 1 || b.window != 77 || b.rect != latest {
		t.Errorf("start used stale host: starts=%d window=%d rect=%+v", b.starts, b.window, b.rect)
	}
	b.mu.Unlock()
	p.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("closed active session succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("closed session did not stop")
	}
}

func TestEmbeddedCancelAllowsImmediateRetry(t *testing.T) {
	p := fakePresentation()
	started := make(chan struct{})
	oldBridge := &fakeEmbeddedBridge{t: t, onStart: func() { close(started) }}
	done := make(chan error, 1)
	go func() {
		key, err := runEmbedded(context.Background(), p, oldBridge, []byte(`{"action":"unwrap"}`))
		wipe(key)
		done <- err
	}()
	<-started
	p.Cancel()
	if !p.Ready() {
		t.Fatal("cancel detached the reusable presentation")
	}
	want := bytes.Repeat([]byte{61}, 32)
	newBridge := &fakeEmbeddedBridge{t: t, result: fakeReply(want)}
	key, err := runEmbedded(context.Background(), p, newBridge, []byte(`{"action":"unwrap"}`))
	defer wipe(key)
	if err != nil || !bytes.Equal(key, want) {
		t.Fatal("immediate retry failed", err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled attempt returned a key")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled attempt did not finish")
	}
	oldBridge.mu.Lock()
	defer oldBridge.mu.Unlock()
	if oldBridge.active || oldBridge.cancels != 1 || oldBridge.releases != 1 || newBridge.starts != 1 || newBridge.releases != 1 {
		t.Fatal("retry did not isolate native token lifetimes")
	}
}

func TestEmbeddedLateSuccessAfterCancelCannotClearRetry(t *testing.T) {
	p := fakePresentation()
	oldBridge := &fakeEmbeddedBridge{t: t, result: fakeReply(bytes.Repeat([]byte{17}, 32))}
	newBridge := &fakeEmbeddedBridge{t: t}
	var retry *embeddedSession
	calls := 0
	p.runOnMain = func(fn func()) {
		calls++
		if calls == 2 {
			// The old attempt already received a valid key, but its completion
			// callback has not run. Cancel and start the retry first.
			p.Cancel()
			var err error
			retry, err = startEmbedded(context.Background(), p, newBridge, []byte(`{"action":"unwrap"}`))
			if err != nil {
				t.Fatal("retry could not start before old completion", err)
			}
		}
		fn()
	}
	key, err := runEmbedded(context.Background(), p, oldBridge, []byte(`{"action":"unwrap"}`))
	defer wipe(key)
	if err == nil || key != nil {
		t.Fatal("late success from the cancelled attempt escaped")
	}
	p.mu.Lock()
	active := p.active
	p.mu.Unlock()
	if retry == nil || active != retry || !newBridge.active || newBridge.releases != 0 || newBridge.cancels != 0 {
		t.Fatal("old completion changed the new active session")
	}
	p.Cancel()
	if newBridge.active || newBridge.cancels != 1 || newBridge.releases != 1 || !p.Ready() {
		t.Fatal("retry token cleanup did not preserve the presentation")
	}
}

func TestEmbeddedFrameChangesAndCloseCancelPendingSession(t *testing.T) {
	for _, action := range []string{"close", "detach", "replace", "context"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := fakePresentation()
			started := make(chan struct{})
			b := &fakeEmbeddedBridge{t: t, onStart: func() { close(started) }}
			done := make(chan error, 1)
			go func() {
				_, err := runEmbedded(ctx, p, b, []byte(`{"action":"unwrap"}`))
				done <- err
			}()
			<-started
			moved := EmbeddedRect{20, 30, 280, 100}
			p.SetFrame(42, moved)
			b.mu.Lock()
			if b.rect != moved || b.moves != 1 || b.window != 42 {
				t.Errorf("native view did not follow current frame: %+v", b.rect)
			}
			b.mu.Unlock()
			switch action {
			case "close":
				p.Close()
			case "detach":
				p.SetFrame(0, EmbeddedRect{})
			case "replace":
				p.SetFrame(43, moved)
			case "context":
				cancel()
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("cancelled presentation accepted")
				}
				if !IsAuthenticationCancelled(err) || IsAuthenticationUnavailable(err) {
					t.Fatal("presentation cancellation became a hardware failure", err)
				}
			case <-time.After(time.Second):
				t.Fatal("cancellation did not close native session")
			}
			b.mu.Lock()
			defer b.mu.Unlock()
			if b.active || b.cancels != 1 || b.releases != 1 {
				t.Fatalf("pending token leaked: active=%v cancels=%d releases=%d", b.active, b.cancels, b.releases)
			}
		})
	}
}

func TestEmbeddedRequestValidation(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`{}`), json.RawMessage(`null`), json.RawMessage(`""`), json.RawMessage(`"bad base64!"`), json.RawMessage(strings.Repeat("x", embeddedMessageLimit+1))} {
		if input, err := embeddedInput(raw, "reason"); err == nil || input != nil {
			t.Fatal("invalid encrypted record accepted")
		}
	}
	if _, err := embeddedInput(json.RawMessage(`"AQ=="`), strings.Repeat("x", embeddedMessageLimit)); err == nil {
		t.Fatal("oversized native request accepted")
	}
	input, err := embeddedInput(json.RawMessage(`"AQ=="`), "approve")
	if err != nil {
		t.Fatal(err)
	}
	defer wipe(input)
	var decoded struct {
		Action, Reason string
		Blob           []byte
	}
	if err := json.Unmarshal(input, &decoded); err != nil || decoded.Action != "unwrap" || decoded.Reason != "approve" || !bytes.Equal(decoded.Blob, []byte{1}) {
		t.Fatal("native request changed opaque data", err)
	}
	b := &fakeEmbeddedBridge{t: t}
	for _, raw := range [][]byte{nil, []byte(`broken`), bytes.Repeat([]byte(" "), embeddedMessageLimit+1)} {
		if _, err := runEmbedded(context.Background(), fakePresentation(), b, raw); err == nil {
			t.Fatal("invalid request reached native bridge")
		}
	}
	if b.starts != 0 {
		t.Fatal("native bridge called for invalid request")
	}
}

type fakeEmbeddedBackend struct {
	fakeBackend
	bridge *fakeEmbeddedBridge
	calls  int
}

func (b *fakeEmbeddedBackend) UnwrapEmbedded(ctx context.Context, _ json.RawMessage, _ string, p *EmbeddedPresentation) ([]byte, error) {
	b.calls++
	return runEmbedded(ctx, p, b.bridge, []byte(`{"action":"unwrap"}`))
}

func TestEmbeddedServicePreservesVaultChecksAndPlatformFallback(t *testing.T) {
	dir := t.TempDir()
	pass := []byte("isolated fake embedded master password")
	v, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	if err = vault.Save(filepath.Join(dir, "vault.json"), v, true); err != nil {
		t.Fatal(err)
	}
	v.Close()
	b := &fakeEmbeddedBackend{bridge: &fakeEmbeddedBridge{t: t}}
	s := &Service{backend: b}
	if err = s.Enroll(context.Background(), dir, pass); err != nil {
		t.Fatal(err)
	}
	defer wipe(b.key)
	if !s.SupportsEmbedded() {
		t.Fatal("embedded backend not recognized")
	}
	if session, err := s.OpenEmbedded(context.Background(), dir, "approve", nil); err == nil {
		session.Close()
		t.Fatal("missing presentation accepted")
	}
	if b.calls != 0 || b.bridge.starts != 0 {
		t.Fatal("missing presentation reached native backend")
	}
	b.bridge.result = fakeReply(b.key)
	for i := 0; i < 2; i++ {
		session, err := s.OpenEmbedded(context.Background(), dir, "approve", fakePresentation())
		if err != nil {
			t.Fatal(err)
		}
		session.Close()
	}
	if b.calls != 2 || b.bridge.starts != 2 {
		t.Fatal("embedded verification cached")
	}
	wrong := append([]byte(nil), b.key...)
	wrong[0] ^= 1
	b.bridge.result = fakeReply(wrong)
	if session, err := s.OpenEmbedded(context.Background(), dir, "approve", fakePresentation()); err == nil {
		session.Close()
		t.Fatal("wrong native key bypassed vault hash")
	}
	originalRecord, err := os.ReadFile(path(dir))
	if err != nil {
		t.Fatal(err)
	}
	record, err := readRecord(dir)
	if err != nil {
		t.Fatal(err)
	}
	wrongHash := sha256.Sum256(wrong)
	record.VaultHash = hex.EncodeToString(wrongHash[:])
	modifiedRecord, _ := json.Marshal(record)
	if err = os.WriteFile(path(dir), modifiedRecord, 0600); err != nil {
		t.Fatal(err)
	}
	if session, err := s.OpenEmbedded(context.Background(), dir, "approve", fakePresentation()); err == nil {
		session.Close()
		t.Fatal("native key with a matching record hash bypassed vault encryption")
	}
	wipe(wrong)
	if err = os.WriteFile(path(dir), originalRecord, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.bridge.result, b.bridge.onPoll = fakeReply(b.key), cancel
	if session, err := s.OpenEmbedded(ctx, dir, "approve", fakePresentation()); err == nil {
		session.Close()
		t.Fatal("late native success opened vault")
	}
	cancel()
	fallback := &fakeBackend{key: append([]byte(nil), b.key...)}
	defer wipe(fallback.key)
	s = &Service{backend: fallback}
	if s.SupportsEmbedded() {
		t.Fatal("ordinary backend claims embedded support")
	}
	session, err := s.OpenEmbedded(context.Background(), dir, "approve", nil)
	if err != nil {
		t.Fatal("non-embedded platform fallback failed", err)
	}
	session.Close()
	if fallback.calls != 1 {
		t.Fatal("ordinary owner verification bypassed")
	}
}
