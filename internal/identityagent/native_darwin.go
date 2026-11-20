//go:build darwin

package identityagent

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"sync"
	"time"

	"sshclient/internal/identitymodel"
	"sshclient/internal/systemauth"
)

type nativeBackend struct {
	audit           func(identitymodel.AuditEvent) error
	dir             string
	mu              sync.Mutex
	started, closed bool
	sequence        uint64
	pending         map[uint64]chan reply
	send            chan command
	ctx             context.Context
	cancel          context.CancelFunc
	process         *exec.Cmd
	done            chan struct{}
	failure         error
}

func newBackend(dir string) backend {
	ctx, cancel := context.WithCancel(context.Background())
	return &nativeBackend{dir: dir, ctx: ctx, cancel: cancel, pending: map[uint64]chan reply{}, send: make(chan command, 64), done: make(chan struct{})}
}
func (b *nativeBackend) setAudit(observer func(identitymodel.AuditEvent) error) { b.audit = observer }

func (b *nativeBackend) startLocked() error {
	if b.closed {
		return errors.New("密钥代理已关闭")
	}
	if b.started {
		return b.failure
	}
	helper, err := systemauth.IdentityAgentPath()
	if err != nil {
		return err
	}
	namespace := sha256.Sum256([]byte(b.dir))
	cmd := exec.CommandContext(b.ctx, helper, "--namespace", hex.EncodeToString(namespace[:]))
	input, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return err
	}
	if err = cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return err
	}
	b.started = true
	b.process = cmd
	go func() {
		encoder := json.NewEncoder(input)
		for {
			select {
			case <-b.ctx.Done():
				input.Close()
				return
			case c := <-b.send:
				if encoder.Encode(c) != nil {
					b.cancel()
					return
				}
			}
		}
	}()
	go func() {
		defer close(b.done)
		defer input.Close()
		scan := bufio.NewScanner(output)
		scan.Buffer(make([]byte, 4096), 1<<20)
		for scan.Scan() {
			var r reply
			if json.Unmarshal(scan.Bytes(), &r) != nil {
				break
			}
			if r.Audit != nil {
				if b.audit != nil && b.audit(*r.Audit) != nil {
					b.cancel()
					break
				}
				continue
			}
			b.mu.Lock()
			wait := b.pending[r.ID]
			delete(b.pending, r.ID)
			b.mu.Unlock()
			if wait != nil {
				wait <- r
			}
		}
		b.cancel()
		_ = cmd.Wait()
		b.mu.Lock()
		b.closed = true
		b.failure = errors.New("原生密钥代理已停止，请重新解锁工作台")
		for id, wait := range b.pending {
			wait <- reply{Error: b.failure.Error()}
			delete(b.pending, id)
		}
		b.mu.Unlock()
	}()
	return nil
}
func (b *nativeBackend) call(ctx context.Context, c command) (reply, error) {
	if err := ctx.Err(); err != nil {
		return reply{}, err
	}
	b.mu.Lock()
	if err := b.startLocked(); err != nil {
		b.mu.Unlock()
		return reply{}, err
	}
	b.sequence++
	c.ID = b.sequence
	wait := make(chan reply, 1)
	b.pending[c.ID] = wait
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.pending, c.ID); b.mu.Unlock() }()
	select {
	case b.send <- c:
	case <-ctx.Done():
		return reply{}, ctx.Err()
	case <-b.ctx.Done():
		return reply{}, io.ErrClosedPipe
	}
	select {
	case r := <-wait:
		if r.Error != "" {
			return r, errors.New(r.Error)
		}
		return r, nil
	case <-ctx.Done():
		select {
		case b.send <- command{ID: c.ID, Action: "cancel"}:
		default:
			b.cancel()
		}
		return reply{}, ctx.Err()
	case <-b.ctx.Done():
		return reply{}, io.ErrClosedPipe
	}
}
func (b *nativeBackend) Close() error {
	b.mu.Lock()
	active := b.started && !b.closed
	b.mu.Unlock()
	if active {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, _ = b.call(ctx, command{Action: "clear"})
		cancel()
	}
	b.mu.Lock()
	b.closed = true
	started := b.started
	b.cancel()
	b.mu.Unlock()
	if started {
		<-b.done
	}
	return nil
}
