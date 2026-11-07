// Package terminalcodec translates terminal text, leaving file-transfer bytes alone.
package terminalcodec

import (
	"errors"
	"io"
	"sshclient/internal/connectcfg"
	"sync"
	"unicode/utf8"
)

type converter interface {
	convert([]byte) ([]byte, error)
	Close() error
}
type choice struct {
	sync.Mutex
	mode string
}

func (c *choice) current() string { c.Lock(); defer c.Unlock(); return c.mode }
func (c *choice) selectMode(mode string) string {
	c.Lock()
	defer c.Unlock()
	if c.mode == "" {
		c.mode = mode
	}
	return c.mode
}

type reader struct {
	source          io.Reader
	state           *choice
	pending, output []byte
	failure         error
	codec           converter
	mu              sync.Mutex
	closed          bool
}

func (r *reader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	for len(r.output) == 0 {
		if len(r.pending) > 0 {
			mode := r.state.current()
			if mode == "" {
				n := 0
				for n < len(r.pending) && r.pending[n] < 128 {
					n++
				}
				if n > 0 {
					r.output = append(r.output, r.pending[:n]...)
					r.pending = r.pending[n:]
					break
				}
				if utf8.FullRune(r.pending) || r.failure != nil {
					mode = "utf-8"
					if value, n := utf8.DecodeRune(r.pending); value == utf8.RuneError && n == 1 {
						mode = "gb18030"
					}
					mode = r.state.selectMode(mode)
				}
			}
			if mode == "utf-8" {
				r.output = r.pending
				r.pending = nil
				break
			}
			if mode != "" {
				n := gbPrefix(r.pending, mode == "gb18030")
				if n > 0 {
					if r.codec == nil {
						var e error
						r.codec, e = newConverter(mode, "utf-8")
						if e != nil {
							return 0, e
						}
					}
					output, e := r.codec.convert(r.pending[:n])
					if e != nil {
						return 0, e
					}
					r.pending = r.pending[n:]
					r.output = output
					break
				}
				if r.failure != nil {
					return 0, io.ErrUnexpectedEOF
				}
			}
		}
		if r.failure != nil {
			return 0, r.failure
		}
		var b [4096]byte
		n, e := r.source.Read(b[:])
		r.pending = append(r.pending, b[:n]...)
		r.failure = e
		if n == 0 && e == nil {
			return 0, nil
		}
	}
	n := copy(p, r.output)
	r.output = r.output[n:]
	return n, nil
}

// GB encodings are stateless. Retain at most one partial two/four-byte unit.
func gbPrefix(b []byte, four bool) int {
	n := 0
	for n < len(b) {
		width := 1
		if b[n] >= 128 {
			width = 2
			if four && n+1 < len(b) && b[n+1] >= '0' && b[n+1] <= '9' {
				width = 4
			}
		}
		if n+width > len(b) {
			break
		}
		n += width
	}
	return n
}
func (r *reader) Close() error {
	if closer, ok := r.source.(io.Closer); ok {
		closer.Close()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.codec != nil {
		return r.codec.Close()
	}
	return nil
}

type writer struct {
	target  io.Writer
	state   *choice
	erase   byte
	pending []byte
	codec   converter
	mu      sync.Mutex
	closed  bool
}

func (w *writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, io.ErrClosedPipe
	}
	b := append([]byte(nil), p...)
	for i, v := range b {
		if v == 127 {
			b[i] = w.erase
		}
	}
	mode := w.state.current()
	if mode == "" {
		for _, v := range b {
			if v >= 128 {
				mode = w.state.selectMode("utf-8")
				break
			}
		}
	}
	if mode == "" || mode == "utf-8" {
		n, e := writeAll(w.target, b)
		return n, e
	}
	w.pending = append(w.pending, b...)
	n := 0
	for n < len(w.pending) {
		if !utf8.FullRune(w.pending[n:]) {
			break
		}
		v, size := utf8.DecodeRune(w.pending[n:])
		if v == utf8.RuneError && size == 1 {
			return 0, errors.New("终端输入不是有效 UTF-8")
		}
		n += size
	}
	if n == 0 {
		return len(p), nil
	}
	if w.codec == nil {
		var e error
		w.codec, e = newConverter("utf-8", mode)
		if e != nil {
			return 0, e
		}
	}
	output, e := w.codec.convert(w.pending[:n])
	if e != nil {
		return 0, e
	}
	w.pending = w.pending[n:]
	if _, e = writeAll(w.target, output); e != nil {
		return 0, e
	}
	return len(p), nil
}
func writeAll(w io.Writer, p []byte) (int, error) {
	n := 0
	for len(p) > 0 {
		m, e := w.Write(p)
		n += m
		p = p[m:]
		if e != nil {
			return n, e
		}
		if m == 0 {
			return n, io.ErrShortWrite
		}
	}
	return n, nil
}
func (w *writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	if w.codec != nil {
		return w.codec.Close()
	}
	return nil
}

// Auto prefers valid UTF-8 at the first non-ASCII output, otherwise GB18030.
// Ambiguous legacy data requires an explicit saved encoding; no detector can
// infer an encoding from ASCII alone or distinguish every valid byte sequence.
func New(output io.Reader, input io.Writer, settings connectcfg.Terminal) (io.ReadCloser, io.WriteCloser, error) {
	if e := settings.Validate(); e != nil {
		return nil, nil, e
	}
	mode := settings.Encoding
	if mode == "auto" {
		mode = ""
	}
	state := &choice{mode: mode}
	return &reader{source: output, state: state}, &writer{target: input, state: state, erase: settings.Erase()}, nil
}
