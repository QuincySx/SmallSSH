// Package transfer persists resumable jobs without storing credentials or grants.
package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/vault"
	"sync"
	"time"
)

type Job struct {
	ID        string    `json:"id"`
	Host      string    `json:"host"`
	Direction string    `json:"direction"`
	Local     string    `json:"local"`
	Remote    string    `json:"remote"`
	Hash      string    `json:"hash"`
	Size      int64     `json:"size"`
	Bytes     int64     `json:"bytes"`
	Speed     float64   `json:"speed"`
	State     string    `json:"state"`
	Error     string    `json:"error,omitempty"`
	RequestID string    `json:"requestId,omitempty"`
	Created   time.Time `json:"created"`
}
type Manager struct {
	dir    string
	client client.Client
	mu     sync.Mutex
	active map[string]context.CancelFunc
	live   map[string]Job
}

var validID = regexp.MustCompile(`^[A-Za-z0-9_-]{24}$`)

func New(dir string) *Manager {
	return NewWithClient(client.Client{Dir: dir})
}

// NewWithClient lets a CLI transfer use its explicitly selected delegated
// session. Desktop owner operations keep their existing proof path.
func NewWithClient(c client.Client) *Manager {
	return &Manager{dir: filepath.Join(c.Dir, "transfers"), client: c, active: map[string]context.CancelFunc{}, live: map[string]Job{}}
}
func (m *Manager) jobDir(id string) string { return filepath.Join(m.dir, id) }
func (m *Manager) save(j Job) error {
	d := m.jobDir(j.ID)
	if e := localipc.Prepare(d); e != nil {
		return e
	}
	b, e := json.Marshal(j)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(d, ".state-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(name, filepath.Join(d, "state.json"))
}
func (m *Manager) read(id string) (Job, error) {
	if !validID.MatchString(id) {
		return Job{}, errors.New("无效传输 ID")
	}
	f, e := os.Open(filepath.Join(m.jobDir(id), "state.json"))
	if e != nil {
		return Job{}, e
	}
	defer f.Close()
	var j Job
	if e = json.NewDecoder(io.LimitReader(f, 65537)).Decode(&j); e != nil {
		return j, e
	}
	if j.ID != id || (j.Direction != "upload" && j.Direction != "download") || !filepath.IsAbs(j.Local) || j.Size < 0 {
		return j, errors.New("传输记录无效")
	}
	return j, nil
}
func (m *Manager) Add(host, direction, local, remote string) (Job, error) {
	if host == "" || (direction != "upload" && direction != "download") {
		return Job{}, errors.New("请选择服务器和传输方向")
	}
	abs, e := filepath.Abs(local)
	if e != nil {
		return Job{}, e
	}
	j := Job{ID: vault.ShortID(), Host: host, Direction: direction, Local: abs, Remote: remote, State: "queued", Created: time.Now().UTC()}
	if direction == "upload" {
		f, e := os.Open(abs)
		if e != nil {
			return j, e
		}
		defer f.Close()
		st, e := f.Stat()
		if e != nil {
			return j, e
		}
		if !st.Mode().IsRegular() {
			return j, errors.New("只能上传普通文件")
		}
		j.Size = st.Size()
		h := sha256.New()
		if _, e = io.Copy(h, f); e != nil {
			return j, e
		}
		j.Hash = hex.EncodeToString(h.Sum(nil))
	} else {
		if _, e = os.Lstat(abs); e == nil {
			return j, errors.New("本地目标已存在，不会覆盖")
		}
		if !os.IsNotExist(e) {
			return j, e
		}
	}
	if e = localipc.Prepare(m.dir); e != nil {
		return j, e
	}
	if e = m.save(j); e != nil {
		return j, e
	}
	return j, nil
}
func (m *Manager) List() []Job {
	m.mu.Lock()
	live := make(map[string]Job, len(m.live))
	for id, job := range m.live {
		live[id] = job
	}
	m.mu.Unlock()
	out := []Job{}
	entries, _ := os.ReadDir(m.dir)
	for _, e := range entries {
		if !e.IsDir() || !validID.MatchString(e.Name()) {
			continue
		}
		j, err := m.read(e.Name())
		if err != nil {
			continue
		}
		if current, ok := live[j.ID]; ok {
			j = current
		} else if j.State == "running" || j.State == "waiting" {
			if release, e := localipc.Acquire(m.jobDir(j.ID)); e == nil {
				release()
				j.State = "paused"
				j.Speed = 0
				j.Error = "上次传输未完成，可继续"
			}
		}
		out = append(out, j)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}
func (m *Manager) Pause(id string) {
	if !validID.MatchString(id) {
		return
	}
	m.mu.Lock()
	stop := m.active[id]
	m.mu.Unlock()
	if stop != nil {
		stop()
		return
	}
	// Desktop and CLI may be separate processes. A private stop marker only
	// reduces authority; it carries no ticket and cannot approve an operation.
	_ = os.WriteFile(filepath.Join(m.jobDir(id), "pause.request"), nil, 0600)
}
func (m *Manager) PauseAll() {
	m.mu.Lock()
	stops := make([]context.CancelFunc, 0, len(m.active))
	for _, stop := range m.active {
		stops = append(stops, stop)
	}
	m.mu.Unlock()
	for _, stop := range stops {
		stop()
	}
}
func (m *Manager) Run(parent context.Context, id, label string) (err error) {
	return m.run(parent, id, label, nil)
}

// RunWithOwner is only used by a human-facing in-process frontend. The public
// CLI Run path never obtains an owner session and still waits for approval.
func (m *Manager) RunWithOwner(parent context.Context, id, label string, owner *vault.Session) error {
	if owner == nil {
		return errors.New("保险库已锁定")
	}
	return m.run(parent, id, label, owner)
}

// freshUploadFallback reports whether a refused resumed upload may be retried as
// a bounded first-time upload: never for the owner, never for a download.
func freshUploadFallback(kind string, owner *vault.Session, direction string, err error) bool {
	return kind == "put-resume" && owner == nil && direction == "upload" && errors.Is(err, client.ErrAutomationPolicy)
}

func (m *Manager) run(parent context.Context, id, label string, owner *vault.Session) (err error) {
	if !validID.MatchString(id) {
		return errors.New("无效传输 ID")
	}
	release, e := localipc.Acquire(m.jobDir(id))
	if e != nil {
		return errors.New("该任务正在另一处运行")
	}
	defer release()
	j, e := m.read(id)
	if e != nil {
		return e
	}
	if j.State == "done" {
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	m.mu.Lock()
	m.active[id] = cancel
	m.live[id] = j
	m.mu.Unlock()
	stopPath := filepath.Join(m.jobDir(id), "pause.request")
	_ = os.Remove(stopPath)
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-watchDone:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, e := os.Stat(stopPath); e == nil {
					_ = os.Remove(stopPath)
					cancel()
					return
				}
			}
		}
	}()
	defer func() { m.mu.Lock(); delete(m.active, id); delete(m.live, id); m.mu.Unlock() }()
	update := func(flush bool) error {
		m.mu.Lock()
		m.live[id] = j
		m.mu.Unlock()
		if flush {
			return m.save(j)
		}
		return nil
	}
	defer func() {
		j.Speed = 0
		if err != nil {
			j.Error = err.Error()
			j.State = "error"
			if ctx.Err() != nil {
				j.State = "paused"
				j.Error = "已暂停，部分文件已保留"
			}
		} else {
			j.State = "done"
			j.Bytes = j.Size
			j.Error = ""
		}
		if e := update(true); err == nil && e != nil {
			err = e
		}
	}()
	var local *os.File
	var offset int64
	prefix := ""
	var part string
	if j.Direction == "upload" {
		local, e = os.Open(j.Local)
		if e != nil {
			return e
		}
		defer local.Close()
		st, e := local.Stat()
		if e != nil {
			return e
		}
		if st.Size() != j.Size {
			return errors.New("本地源文件大小已变化，请新建任务")
		}
	} else {
		part = filepath.Join(filepath.Dir(j.Local), "."+filepath.Base(j.Local)+".ssh-part-"+j.ID)
		if st, statErr := os.Lstat(j.Local); statErr == nil {
			// Recover a crash after no-clobber commit but before the final state
			// write. Only an already complete, hash-matching target is accepted.
			if !st.Mode().IsRegular() || st.Size() != j.Size || j.Hash == "" {
				return errors.New("本地目标已存在，不会覆盖")
			}
			f, err := os.Open(j.Local)
			if err != nil {
				return err
			}
			h := sha256.New()
			_, err = io.Copy(h, f)
			_ = f.Close()
			if err != nil {
				return err
			}
			if hex.EncodeToString(h.Sum(nil)) != j.Hash {
				return errors.New("本地目标已存在且内容不同，不会覆盖")
			}
			if partial, err := os.Lstat(part); err == nil && os.SameFile(st, partial) {
				_ = os.Remove(part)
			}
			return nil
		} else if !os.IsNotExist(statErr) {
			return statErr
		}
		st, e := os.Lstat(part)
		if os.IsNotExist(e) {
			local, e = os.OpenFile(part, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		} else if e == nil {
			if !st.Mode().IsRegular() || (runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0) {
				return errors.New("本地部分文件类型或权限不安全")
			}
			local, e = os.OpenFile(part, os.O_RDWR, 0)
		}
		if e != nil {
			return e
		}
		defer local.Close()
		defer local.Sync()
		st, e = local.Stat()
		if e != nil {
			return e
		}
		offset = st.Size()
		h := sha256.New()
		if _, e = io.Copy(h, local); e != nil {
			return e
		}
		prefix = hex.EncodeToString(h.Sum(nil))
	}
	kind := "put"
	if owner != nil || j.Bytes > 0 {
		kind = "put-resume"
	}
	if j.Direction == "download" {
		kind = "get-resume"
	}
	op := approval.Operation{Host: j.Host, Kind: kind, Path: j.Remote, Client: label, TransferID: j.ID, Size: j.Size, Offset: offset, ContentHash: j.Hash, PrefixHash: prefix}
	var r approval.Request
	var ticket string
	if owner != nil {
		r, ticket, e = m.client.RequestWithOwner(ctx, op, owner)
	} else {
		r, ticket, e = m.client.Request(ctx, op, 15*time.Minute)
	}
	// The owner's policy can refuse a resumed write outright, because SFTP cannot
	// prove an existing remote partial is not a hard link into a protected tree.
	// Re-uploading from the beginning is bounded and allowed, so ask for that
	// instead of leaving the job refused forever.
	if e == nil || !freshUploadFallback(kind, owner, j.Direction, e) {
		if e != nil {
			return e
		}
	} else {
		j.Bytes = 0
		kind = "put"
		op.Kind, op.Offset, op.PrefixHash = "put", 0, ""
		op.TransferID = vault.ShortID()
		if r, ticket, e = m.client.Request(ctx, op, 15*time.Minute); e != nil {
			return e
		}
	}
	defer func() {
		if ctx.Err() != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, _ = m.client.Call(cleanup, broker.Message{Action: "withdraw", ID: r.ID, Ticket: ticket})
		}
	}()
	j.State = "waiting"
	j.RequestID = r.ID
	j.Error = ""
	if e = update(true); e != nil {
		return e
	}
	if owner != nil {
		if e = m.client.DecideWithOwner(ctx, r.ID, true, false, owner); e != nil {
			return e
		}
	}
	if e = m.client.Wait(ctx, r.ID, ticket); e != nil {
		return e
	}
	stream, e := m.client.Start(ctx, r.ID, ticket, 0, 0)
	if e != nil {
		return e
	}
	defer stream.Close()
	if stream.Meta == nil {
		return errors.New("代理没有返回续传校验信息")
	}
	meta := *stream.Meta
	if meta.Offset < 0 || meta.Size < 0 || meta.Offset > meta.Size {
		return errors.New("代理返回的续传位置无效")
	}
	if j.Direction == "upload" {
		if meta.Size != j.Size || meta.Hash != j.Hash {
			return errors.New("上传元数据与批准内容不同")
		}
		h := sha256.New()
		if _, e = io.CopyN(h, local, meta.Offset); e != nil {
			return e
		}
		if hex.EncodeToString(h.Sum(nil)) != meta.PrefixHash {
			return errors.New("远程部分文件与本地前缀不匹配，请新建任务")
		}
	} else {
		if meta.Offset != offset || (j.Hash != "" && meta.Hash != j.Hash) {
			return errors.New("远程源文件在两次传输之间发生变化")
		}
		j.Hash = meta.Hash
		j.Size = meta.Size
	}
	j.Bytes = meta.Offset
	j.State = "running"
	if e = update(true); e != nil {
		return e
	}
	start := time.Now()
	base := j.Bytes
	lastSave := start
	progress := func(n int) {
		j.Bytes += int64(n)
		j.Speed = float64(j.Bytes-base) / max(time.Since(start).Seconds(), 0.001)
		flush := time.Since(lastSave) > time.Second
		if flush {
			lastSave = time.Now()
		}
		_ = update(flush)
	}
	if j.Direction == "upload" {
		buf := make([]byte, 48<<10)
		for {
			n, re := local.Read(buf)
			if n > 0 {
				if _, e = stream.Write(buf[:n]); e != nil {
					return e
				}
				progress(n)
			}
			if re == io.EOF {
				break
			}
			if re != nil {
				return re
			}
		}
		if e = stream.End(); e != nil {
			return e
		}
		if _, _, e = stream.Copy(io.Discard, io.Discard); e != nil {
			return e
		}
	} else {
		if _, e = local.Seek(offset, io.SeekStart); e != nil {
			return e
		}
		writer := countWriter{local, progress, j.Size - offset}
		if _, _, e = stream.Copy(&writer, io.Discard); e != nil {
			return e
		}
		if j.Bytes != j.Size {
			return io.ErrUnexpectedEOF
		}
		if e = local.Sync(); e != nil {
			return e
		}
		if _, e = local.Seek(0, io.SeekStart); e != nil {
			return e
		}
		h := sha256.New()
		if _, e = io.Copy(h, local); e != nil {
			return e
		}
		if hex.EncodeToString(h.Sum(nil)) != j.Hash {
			return errors.New("下载完整性校验失败，部分文件已保留")
		}
		if e = local.Close(); e != nil {
			return e
		}
		if e = os.Link(part, j.Local); e != nil {
			return fmt.Errorf("本地无覆盖提交失败，部分文件保留: %w", e)
		}
		if e = os.Remove(part); e != nil {
			return e
		}
	}
	return nil
}

type countWriter struct {
	w         io.Writer
	progress  func(int)
	remaining int64
}

func (w *countWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, errors.New("下载超过批准的文件大小")
	}
	n, e := w.w.Write(p)
	w.remaining -= int64(n)
	w.progress(n)
	return n, e
}
