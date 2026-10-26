// Package localipc exposes private local transports, never a TCP authority port.
package localipc

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
)

const MaxFrame = 1 << 20

func DefaultDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		panic(err)
	}
	return filepath.Join(d, "ssh-client-workbench")
}

// Wire is length framed; malformed lengths are rejected before allocation.
// One reader and any number of writers may use it concurrently.
type Wire struct {
	Conn net.Conn
	mu   sync.Mutex
}

func (w *Wire) Send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > MaxFrame {
		return errors.New("本地消息超过上限")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	var h [4]byte
	binary.BigEndian.PutUint32(h[:], uint32(len(b)))
	if err = writeAll(w.Conn, h[:]); err != nil {
		return err
	}
	return writeAll(w.Conn, b)
}
func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, e := w.Write(b)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
func (w *Wire) Receive(v any) error {
	var h [4]byte
	if _, e := io.ReadFull(w.Conn, h[:]); e != nil {
		return e
	}
	n := binary.BigEndian.Uint32(h[:])
	if n == 0 || n > MaxFrame {
		return errors.New("无效本地消息长度")
	}
	b := make([]byte, n)
	if _, e := io.ReadFull(w.Conn, b); e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func Connect(ctx context.Context, dir string) (*Wire, error) {
	c, e := Dial(ctx, dir)
	if e != nil {
		return nil, e
	}
	return &Wire{Conn: c}, nil
}
