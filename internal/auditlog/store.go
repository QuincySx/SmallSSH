package auditlog

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

const (
	MaxFileBytes    = 8 << 20
	MaxPageSize     = 100
	maxLineBytes    = 512 << 10
	maxScannedLines = 4096
)

var ErrCursor = errors.New("历史分页已过期，请重新加载历史")

// Store retains the current file and all rotated segments. The broker holds the
// data-directory lock; this mutex makes append, rotation and page snapshots atomic.
type Store struct {
	mu       sync.Mutex
	path     string
	f        *os.File
	maxBytes int64
	// head is the digest of the last record written, and what the next record
	// names as its Prev.
	head []byte
	// Called under mu before an archive can be evicted. The callback must not
	// call back into Store; its durable checkpoint must succeed before rotation.
	checkpoint func(string) error
	summaries  map[string]cachedSummary
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, maxBytes: MaxFileBytes}
	paths, err := s.paths()
	if err != nil {
		return nil, err
	}
	for _, name := range paths {
		f, err := openPrivate(name, name == path)
		if errors.Is(err, os.ErrNotExist) && name != path {
			continue
		}
		if err != nil {
			return nil, err
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
	// A record that was edited or removed in the middle is refused here rather
	// than trusted: the broker stops instead of writing into a log it cannot vouch
	// for.
	if _, err = s.Verify(); err != nil {
		f.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	f := s.f
	s.f = nil
	return f.Close()
}

// SetRotationCheckpoint coordinates retention with the encrypted vault anchor.
// A failed checkpoint leaves both retained files untouched and rejects append.
func (s *Store) SetRotationCheckpoint(checkpoint func(string) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpoint = checkpoint
}

// Checkpoint commits the current head under the same lock as append/rotation,
// so a delayed close cannot replace a newer retention checkpoint with an old one.
func (s *Store) Checkpoint() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checkpointLocked()
}

func (s *Store) checkpointLocked() error {
	if s.checkpoint != nil && len(s.head) != 0 {
		return s.checkpoint(hex.EncodeToString(s.head))
	}
	return nil
}

func (s *Store) Append(v Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return errors.New("历史文件不可用")
	}
	// Seal the record to the one before it before it is written.
	v.RecordHash = ""
	v.Prev = hex.EncodeToString(s.head)
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > maxLineBytes {
		return errors.New("历史事件超过上限")
	}
	// The chain commits to JSON only, never a separator repairing a crash tail.
	digest := sha256.Sum256(b)
	st, err := s.f.Stat()
	if err != nil {
		return err
	}
	// A crash may leave a partial tail. Budget its separator before deciding
	// whether the next record fits, including at the exact retention boundary.
	separator := int64(0)
	if st.Size() > 0 {
		var last [1]byte
		if _, err = s.f.ReadAt(last[:], st.Size()-1); err != nil {
			return err
		}
		if last[0] != '\n' {
			separator = 1
		}
	}
	if st.Size()+separator+int64(len(b))+1 > s.maxBytes {
		if err = s.rotate(); err != nil {
			return err
		}
		separator = 0 // The new current file has no tail to separate.
	}
	if separator != 0 {
		b = append([]byte{'\n'}, b...)
	}
	// Windows opens with explicit handle security flags; seek under the same
	// writer lock so both platform implementations append at the current end.
	if _, err = s.f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	line := append(b, '\n')
	if _, err = s.f.Write(line); err != nil {
		return err
	}
	if err = s.f.Sync(); err != nil {
		return err
	}
	s.head = digest[:]
	s.updateSummaries(v, hex.EncodeToString(digest[:]))
	return nil
}

func (s *Store) rotate() error {
	// Inspect the destination before replacing it: never follow symlinks or
	// silently replace another kind of file in the private directory.
	if archive, err := openPrivate(s.path+".1", false); err == nil {
		archive.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := s.checkpointLocked(); err != nil {
		return fmt.Errorf("轮转前保存审计检查点失败: %w", err)
	}
	if err := s.retainArchive(); err != nil {
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
	Filter  string     `json:"q,omitempty"`
}
type source struct {
	path string
	id   string
	size int64
}

func (s *Store) sources() ([]source, error) {
	paths, err := s.paths()
	if err != nil {
		return nil, err
	}
	var sources []source
	for i := len(paths) - 1; i >= 0; i-- {
		f, err := openPrivate(paths[i], false)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, err
		}
		id, err := fileIdentity(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		// Hash only the stable first line, without reading half a megabyte
		// from every segment for every page.
		hash := sha256.New()
		reader := bufio.NewReaderSize(f, 4096)
		remaining := maxLineBytes
		for remaining > 0 {
			part, readErr := reader.ReadSlice('\n')
			if len(part) > remaining {
				part = part[:remaining]
			}
			hash.Write(part)
			remaining -= len(part)
			if readErr != nil && !errors.Is(readErr, bufio.ErrBufferFull) && !errors.Is(readErr, io.EOF) {
				f.Close()
				return nil, readErr
			}
			if !errors.Is(readErr, bufio.ErrBufferFull) {
				break
			}
		}
		f.Close()
		id += ":" + hex.EncodeToString(hash.Sum(nil))
		if st.Size() > 0 {
			sources = append(sources, source{path: paths[i], id: id, size: st.Size()})
		}
	}
	return sources, nil
}

// History snapshots a file identity and byte position. Rotation preserves that
// identity; a page continues through older segments without an unbounded cursor
// or keeping every archive open. Filters are bound to the opaque cursor.
func (s *Store) History(q Query) (Page, error) {
	page := Page{Events: []Event{}}
	limit := q.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > MaxPageSize || len(q.Before) > 2048 || len(q.Search) > 256 || len(q.Application) > 1024 || len(q.Related) > 128 || len(q.Result) > 64 || !q.From.IsZero() && !q.To.IsZero() && !q.From.Before(q.To) {
		return page, errors.New("历史分页参数无效")
	}
	switch q.Category {
	case "", "authorization", "operation", "ssh", "system":
	default:
		return page, errors.New("审计分类无效")
	}
	binding := q
	binding.Before = ""
	binding.Limit = 0
	binding.Summary = false
	binding.Raw = false
	bindingJSON, _ := json.Marshal(binding)
	digest := sha256.Sum256(bindingJSON)
	filter := hex.EncodeToString(digest[:])
	c := cursor{Version: 2, Filter: filter}
	if q.Before != "" {
		b, err := base64.RawURLEncoding.DecodeString(q.Before)
		if err != nil || json.Unmarshal(b, &c) != nil || c.Version != 1 && c.Version != 2 || len(c.Files) < 1 || len(c.Files) > 2 || c.Version == 2 && c.Filter != filter {
			return page, ErrCursor
		}
		if c.Version == 1 && (q.Category != "" || q.Search != "" || q.Application != "" || q.Related != "" || q.Result != "" || !q.From.IsZero() || !q.To.IsZero()) {
			return page, ErrCursor
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, summaryCached := s.summaries[filter]
	if q.Raw && q.Before == "" || q.Summary && !summaryCached {
		if _, err := s.verifyLocked(); err != nil {
			return page, err
		}
	}
	sources, err := s.sources()
	if err != nil {
		return page, err
	}
	if q.Summary {
		cached, ok := s.summaries[filter]
		if !ok {
			summary, err := s.summarize(q, sources)
			if err != nil {
				return page, err
			}
			if len(s.summaries) >= 8 {
				s.summaries = nil
			}
			if s.summaries == nil {
				s.summaries = make(map[string]cachedSummary)
			}
			cached = cachedSummary{query: binding, value: summary}
			s.summaries[filter] = cached
		}
		page.Summary = cached.value.snapshot()
	}

	if len(sources) == 0 {
		return page, nil
	}
	index, end := 0, sources[0].size
	if q.Before != "" {
		index = -1
		for i, src := range sources {
			if src.id == c.Files[0].File {
				index = i
				end = c.Files[0].End
				break
			}
		}
		if index < 0 || end < 0 || end > sources[index].size {
			return page, ErrCursor
		}
		// Validate legacy cursors too, rather than accepting a fabricated second file.
		for _, pos := range c.Files[1:] {
			valid := false
			for _, src := range sources {
				valid = valid || src.id == pos.File && pos.End >= 0 && pos.End <= src.size
			}
			if !valid {
				return page, ErrCursor
			}
		}
	}
	scanned, budget := 0, 0
	for index < len(sources) && len(page.Events) < limit && scanned < maxScannedLines {
		src := sources[index]
		if end == 0 {
			index++
			if index < len(sources) {
				end = sources[index].size
			}
			continue
		}
		f, err := openPrivate(src.path, false)
		if err != nil {
			return page, err
		}
		reader := &reverseReader{f: f, size: src.size, start: -1}
		for end > 0 && len(page.Events) < limit && scanned < maxScannedLines {
			line, start, err := previousLine(reader, end)
			if err != nil {
				f.Close()
				return page, err
			}
			scanned++
			var v Event
			if len(line) == 0 || json.Unmarshal(line, &v) != nil || v.Time.IsZero() || v.Event == "" || !q.matches(v) {
				end = start
				continue
			}
			hash := sha256.Sum256(line)
			v = bounded(v)
			v.RecordHash = hex.EncodeToString(hash[:])
			encoded, _ := json.Marshal(v)
			cost := len(encoded) + 2
			if q.Raw {
				cost += len(line) + 2
			}
			if budget+cost > 600<<10 {
				if len(page.Events) == 0 {
					f.Close()
					return page, errors.New("单条审计记录超过导出帧上限")
				}
				break
			}
			budget += cost
			end = start
			page.Events = append(page.Events, v)
			if q.Raw {
				page.Raw = append(page.Raw, append(json.RawMessage(nil), line...))
			}
		}
		f.Close()
		if budget >= 590<<10 {
			break
		}
		if end > 0 {
			break
		}
	}
	for index < len(sources) && end == 0 {
		index++
		if index < len(sources) {
			end = sources[index].size
		}
	}
	if index < len(sources) {
		c = cursor{Version: 2, Filter: filter, Files: []position{{File: sources[index].id, End: end}}}
		b, _ := json.Marshal(c)
		page.Next = base64.RawURLEncoding.EncodeToString(b)
	}
	return page, nil
}

func (s *Store) summarize(q Query, sources []source) (Summary, error) {
	result := Summary{Head: hex.EncodeToString(s.head)}
	for _, src := range sources {
		f, err := openPrivate(src.path, false)
		if err != nil {
			return result, err
		}
		reader := bufio.NewReaderSize(f, maxLineBytes+1)
		for {
			line, err := readAuditLine(reader)
			var v Event
			if len(line) > 0 && json.Unmarshal(line, &v) == nil && q.matches(v) {
				result.add(v)
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					f.Close()
					return result, err
				}
				break
			}
		}
		f.Close()
	}
	return result, nil
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
