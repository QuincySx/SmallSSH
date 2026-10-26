// Package dockerlog bounds volatile log output and strips terminal control
// sequences. Log text is never executed or persisted by this package.
package dockerlog

import (
	"bytes"
	"strings"
	"sync"
)

type Buffer struct {
	mu                sync.Mutex
	data              []byte
	state             byte
	line              int
	lines             int
	dropping, trimmed bool
	version           uint64
}

func (b *Buffer) Version() uint64 { b.mu.Lock(); defer b.mu.Unlock(); return b.version }

func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, v := range p {
		switch b.state {
		case 1:
			if v == '[' {
				b.state = 2
			} else if v == ']' || v == 'P' || v == '_' || v == '^' {
				b.state = 3
			} else {
				b.state = 0
			}
			continue
		case 2:
			if v >= 0x40 && v <= 0x7e {
				b.state = 0
			}
			continue
		case 3:
			if v == 7 {
				b.state = 0
			} else if v == 27 {
				b.state = 4
			}
			continue
		case 4:
			if v == '\\' {
				b.state = 0
			} else {
				b.state = 3
			}
			continue
		}
		if v == 27 {
			b.state = 1
			continue
		}
		if v == '\r' {
			continue
		}
		if v == '\n' {
			b.data = append(b.data, v)
			b.lines++
			b.line = 0
			b.dropping = false
			continue
		}
		if v < 32 && v != '\t' || v == 127 {
			continue
		}
		if b.dropping {
			continue
		}
		if b.line >= 8192 {
			b.data = append(b.data, []byte(" …[此行过长]")...)
			b.dropping = true
			b.trimmed = true
			continue
		}
		b.data = append(b.data, v)
		b.line++
	}
	if len(b.data) > 512<<10 {
		cut := len(b.data) - (384 << 10)
		if n := bytes.IndexByte(b.data[cut:], '\n'); n >= 0 {
			cut += n + 1
		}
		b.lines -= bytes.Count(b.data[:cut], []byte{'\n'})
		b.data = append([]byte(nil), b.data[cut:]...)
		b.trimmed = true
	}
	if b.lines > 4000 {
		cut := 0
		for n := 0; n < b.lines-3000; n++ {
			cut += bytes.IndexByte(b.data[cut:], '\n') + 1
		}
		b.lines = 3000
		b.data = append([]byte(nil), b.data[cut:]...)
		b.trimmed = true
	}
	b.version++
	return len(p), nil
}
func (b *Buffer) Snapshot() ([]string, uint64, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	text := strings.ToValidUTF8(string(b.data), "�")
	if text == "" {
		return nil, b.version, b.trimmed
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n"), b.version, b.trimmed
}
