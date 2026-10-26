// Package desktopcontrol provides a private, development-only UI control pipe.
// It has no SSH, credential, unlock or approval operations.
package desktopcontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"sshclient/internal/localipc"
)

type State struct {
	PID             int    `json:"pid"`
	Instance        string `json:"instance"`
	Ready           bool   `json:"ready"`
	Visible         bool   `json:"visible"`
	Focused         bool   `json:"focused"`
	Activations     int    `json:"activations"`
	BackgroundStart bool   `json:"backgroundStart"`
}

type Reply struct {
	State      State
	AfterReply func()
}

type Handler func(context.Context, string) (Reply, error)
type message struct {
	Action string `json:"action,omitempty"`
	State  State  `json:"state,omitempty"`
	Error  string `json:"error,omitempty"`
}

type Server struct {
	listener net.Listener
	cancel   context.CancelFunc
	release  func()
	once     sync.Once
	wg       sync.WaitGroup
}

func DefaultDir() string { return filepath.Join(localipc.DefaultDir(), "development") }

// Short Unix paths even when checkout/data paths are long. localipc checks
// directory ownership/permissions and restricts Windows pipes to the user SID.
func endpoint(dir string) (string, error) {
	p, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	userRoot, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(userRoot + "\x00" + filepath.Clean(p)))
	return filepath.Join(os.TempDir(), "sw-ui-"+hex.EncodeToString(sum[:12])), nil
}

func Start(parent context.Context, dir string, handle Handler) (*Server, error) {
	path, err := endpoint(dir)
	if err != nil {
		return nil, err
	}
	release, err := localipc.Acquire(path)
	if err != nil {
		return nil, err
	}
	ln, err := localipc.Listen(path)
	if err != nil {
		release()
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	s := &Server{listener: ln, cancel: cancel, release: release}
	s.wg.Go(func() {
		stop := context.AfterFunc(ctx, func() { ln.Close() })
		defer stop()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s.wg.Go(func() {
				defer c.Close()
				requestCtx, finish := context.WithTimeout(ctx, 3*time.Second)
				defer finish()
				stop := context.AfterFunc(requestCtx, func() { c.Close() })
				defer stop()
				c.SetDeadline(time.Now().Add(3 * time.Second))
				wire := &localipc.Wire{Conn: c}
				var q message
				if wire.Receive(&q) != nil {
					return
				}
				if q.Action != "status" && q.Action != "show" && q.Action != "quit" {
					_ = wire.Send(message{Error: "仅支持开发窗口的 status、show、quit"})
					return
				}
				result, err := handle(requestCtx, q.Action)
				if err != nil {
					_ = wire.Send(message{Error: err.Error()})
					return
				}
				if wire.Send(message{State: result.State}) == nil && result.AfterReply != nil {
					result.AfterReply()
				}
			})
		}
	})
	return s, nil
}

func (s *Server) Close() {
	s.once.Do(func() { s.cancel(); s.listener.Close(); s.wg.Wait(); s.release() })
}

func Command(parent context.Context, dir, action string) (State, error) {
	if action != "status" && action != "show" && action != "quit" {
		return State{}, errors.New("无效的开发窗口操作")
	}
	path, err := endpoint(dir)
	if err != nil {
		return State{}, err
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	wire, err := localipc.Connect(ctx, path)
	if err != nil {
		return State{}, err
	}
	defer wire.Conn.Close()
	stop := context.AfterFunc(ctx, func() { wire.Conn.Close() })
	defer stop()
	wire.Conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err = wire.Send(message{Action: action}); err != nil {
		return State{}, err
	}
	var result message
	if err = wire.Receive(&result); err != nil {
		return State{}, err
	}
	if result.Error != "" {
		return State{}, errors.New(result.Error)
	}
	return result.State, nil
}
