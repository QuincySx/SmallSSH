//go:build !windows

package main

import (
	"context"
	"golang.org/x/term"
	"os"
	"os/signal"
	"sshclient/internal/client"
	"syscall"
)

func openTTY() (*os.File, error) { return os.OpenFile("/dev/tty", os.O_RDWR, 0) }
func watchResize(ctx context.Context, s *client.Stream) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ch:
				cols, rows, e := term.GetSize(int(os.Stdin.Fd()))
				if e == nil {
					s.Resize(cols, rows)
				}
			}
		}
	}()
	return func() { signal.Stop(ch); close(done) }
}
