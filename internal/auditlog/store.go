package auditlog

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	MaxFileBytes    = 8 << 20
	MaxPageSize     = 100
	maxLineBytes    = 8 << 10
	maxScannedLines = 4096
)

var ErrCursor = errors.New("历史分页已过期，请重新加载历史")

// Store retains at most one current file and one archive. The broker holds the
// data-directory lock; this mutex makes append, rotation and page snapshots atomic.
type Store struct {
	mu       sync.Mutex
	path     string
	f        *os.File
	maxBytes int64
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, maxBytes: MaxFileBytes}
	for _, name := range []string{path + ".1", path} {
		f, err := openPrivate(name, name == path)
		if errors.Is(err, os.ErrNotExist) && name != path {
			continue
		}
		if err != nil {
			return nil, err
		}
		st, err := f.Stat()
		if err == nil && st.Size() > s.maxBytes {
			err = s.trim(f, name)
		}
		f.Close()
		if err != nil {
			return nil, err
		}
	}
	f, err := openPrivate(path, true)
	if err != nil {
		return nil, err
	}
	s.f = f
	return s, nil
}

// trim preserves only complete recent lines when opening an oversized legacy log.
// Replacement is atomic, so a failed write cannot destroy the original log.
func (s *Store) trim(src *os.File, path string) error {
	st, err := src.Stat()
	if err != nil {
		return err
	}
	start := st.Size() - s.maxBytes
	b := make([]byte, s.maxBytes)
	if _, err = src.ReadAt(b, start); err != nil {
		return err
	}
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		b = b[i+1:]
	} else {
		b = nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".audit-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	return s.f.Close()
}

func (s *Store) Append(v Event) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > maxLineBytes {
		return errors.New("历史事件超过上限")
	}
	b = append(b, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return errors.New("历史文件不可用")
	}
	st, err := s.f.Stat()
	if err != nil {
		return err
	}
	if st.Size()+int64(len(b))+1 > s.maxBytes {
		if err = s.rotate(); err != nil {
			return err
		}
		st, err = s.f.Stat()
		if err != nil {
			return err
		}
	}
	// A crash may leave a partial tail. Separate it from the next valid record.
	if st.Size() > 0 {
		var last [1]byte
		if _, err = s.f.ReadAt(last[:], st.Size()-1); err != nil {
			return err
		}
		if last[0] != '\n' {
			b = append([]byte{'\n'}, b...)
		}
	}
	// Windows opens with explicit handle security flags; seek under the same
	// writer lock so both platform implementations append at the current end.
	if _, err = s.f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	if _, err = s.f.Write(b); err != nil {
		return err
	}
	return s.f.Sync()
}

func (s *Store) rotate() error {
	// Inspect the destination before replacing it: never follow symlinks or
	// silently replace another kind of file in the private directory.
	if archive, err := openPrivate(s.path+".1", false); err == nil {
		archive.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := s.f.Close(); err != nil {
		return err
	}
	if err := os.Rename(s.path, s.path+".1"); err != nil {
		s.f, _ = openPrivate(s.path, true)
		return err
	}
	f, err := openPrivate(s.path, true)
	if err != nil {
		s.f = nil
		return err
	}
	s.f = f
	return nil
}

type position struct {
	File string `json:"f"`
	End  int64  `json:"e"`
}
type cursor struct {
	Version int        `json:"v"`
	Files   []position `json:"f"`
}
type source struct {
	size   int64
	reader *reverseReader
}

// History returns newest first. A cursor snapshots byte positions and OS file
// identities, so appends and one rotation cannot duplicate or skip records.
// Once retention evicts a needed file, the cursor fails explicitly.
func (s *Store) History(q Query) (Page, error) {
	page := Page{Events: []Event{}}
	limit := q.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > MaxPageSize || len(q.Before) > 2048 {
		return page, errors.New("历史分页参数无效")
	}
	c := cursor{Version: 1}
	if q.Before != "" {
		b, err := base64.RawURLEncoding.DecodeString(q.Before)
		if err != nil || json.Unmarshal(b, &c) != nil || c.Version != 1 || len(c.Files) < 1 || len(c.Files) > 2 {
			return page, ErrCursor
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sources := make(map[string]source)
	for _, path := range []string{s.path, s.path + ".1"} {
		f, err := openPrivate(path, false)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return page, err
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return page, err
		}
		id, err := fileIdentity(f)
		if err != nil {
			return page, err
		}
		// Include a stable first-line digest to distinguish inode reuse after
		// an older archive has been evicted. Appends never alter this prefix.
		prefix := make([]byte, min(st.Size(), int64(maxLineBytes)))
		if _, err = f.ReadAt(prefix, 0); err != nil && err != io.EOF {
			return page, err
		}
		if i := bytes.IndexByte(prefix, '\n'); i >= 0 {
			prefix = prefix[:i+1]
		}
		digest := sha256.Sum256(prefix)
		id += fmt.Sprintf(":%x", digest[:])
		if st.Size() > s.maxBytes {
			return page, errors.New("历史文件超过上限")
		}
		sources[id] = source{size: st.Size(), reader: &reverseReader{f: f, size: st.Size(), start: -1}}
		if q.Before == "" && st.Size() > 0 {
			c.Files = append(c.Files, position{File: id, End: st.Size()})
		}
	}
	seen := make(map[string]bool)
	for _, pos := range c.Files {
		src, ok := sources[pos.File]
		if !ok || seen[pos.File] || pos.End < 0 || pos.End > src.size {
			return page, ErrCursor
		}
		seen[pos.File] = true
	}
	for scanned := 0; len(c.Files) > 0 && len(page.Events) < limit && scanned < maxScannedLines; {
		pos := &c.Files[0]
		if pos.End == 0 {
			c.Files = c.Files[1:]
			continue
		}
		line, start, err := previousLine(sources[pos.File].reader, pos.End)
		if err != nil {
			return page, err
		}
		pos.End = start
		scanned++
		var v Event
		if len(line) == 0 || json.Unmarshal(line, &v) != nil || v.Time.IsZero() || v.Event == "" {
			continue
		}
		page.Events = append(page.Events, bounded(v))
	}
	for len(c.Files) > 0 && c.Files[0].End == 0 {
		c.Files = c.Files[1:]
	}
	if len(c.Files) > 0 {
		b, _ := json.Marshal(c)
		page.Next = base64.RawURLEncoding.EncodeToString(b)
	}
	return page, nil
}

type reverseReader struct {
	f     *os.File
	size  int64
	start int64
	block []byte
}

// blockBefore caches each aligned block as records are consumed backwards.
// Even thousands of tiny damaged lines read each disk block only once per page.
func (r *reverseReader) blockBefore(end int64) ([]byte, error) {
	start := (end - 1) / 4096 * 4096
	if r.start != start {
		r.block = make([]byte, min(int64(4096), r.size-start))
		if _, err := r.f.ReadAt(r.block, start); err != nil {
			return nil, err
		}
		r.start = start
	}
	return r.block[:end-start], nil
}

// previousLine scans backwards with fixed allocations, including when a corrupt
// line is enormous. Unterminated and oversized records are skipped safely.
func previousLine(r *reverseReader, end int64) ([]byte, int64, error) {
	last, err := r.blockBefore(end)
	if err != nil {
		return nil, 0, err
	}
	complete := last[len(last)-1] == '\n'
	pos := end
	if complete {
		pos--
	}
	tail := make([]byte, 0, maxLineBytes)
	oversized := false
	for pos > 0 {
		chunk, err := r.blockBefore(pos)
		if err != nil {
			return nil, 0, err
		}
		start := pos - int64(len(chunk))
		i := bytes.LastIndexByte(chunk, '\n')
		part := chunk
		if i >= 0 {
			part = chunk[i+1:]
		}
		if len(part)+len(tail) > maxLineBytes {
			oversized = true
		}
		if !oversized && complete {
			tail = append(part, tail...)
		}
		if i >= 0 {
			if oversized || !complete {
				return nil, start + int64(i) + 1, nil
			}
			return tail, start + int64(i) + 1, nil
		}
		pos = start
	}
	if oversized || !complete {
		return nil, 0, nil
	}
	return tail, 0, nil
}
