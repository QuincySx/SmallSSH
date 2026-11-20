// This fixture is linked, never executed by tools proxy-probe. It keeps JSON
// decoding, lifecycle management and TCP dialing reachable for the size probe.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("compile-only fixture: target argument and outbound JSON on stdin required")
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 64<<10+1))
	if err != nil {
		return err
	}
	if len(raw) > 64<<10 {
		return fmt.Errorf("outbound JSON exceeds 64 KiB")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, closeRoute, err := dial(ctx, raw, os.Args[1])
	if err != nil {
		return err
	}
	defer closeRoute()
	defer conn.Close()
	_, err = fmt.Fprintln(os.Stdout, conn.RemoteAddr())
	return err
}
