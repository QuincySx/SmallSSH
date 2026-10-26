package main

import (
	"context"
	"golang.org/x/term"
	"os"
	"sshclient/internal/client"
	"time"
)

func openTTY() (*os.File, error) { return os.OpenFile("CONIN$", os.O_RDWR, 0) }
func watchResize(ctx context.Context, s *client.Stream) func() {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		cols, rows := 0, 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-t.C:
				c, r, e := term.GetSize(int(os.Stdin.Fd()))
				if e == nil && (c != cols || r != rows) {
					cols, rows = c, r
					s.Resize(c, r)
				}
			}
		}
	}()
	return func() { close(done) }
}
