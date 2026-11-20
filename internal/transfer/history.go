package transfer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"sshclient/internal/localipc"
)

// RemoveCompleted forgets a completed top-level job. It never uses the job's
// local or remote target: only private transfer metadata is removed.
func (m *Manager) RemoveCompleted(id string) error { return m.remove(id, true) }

// Discard forgets a top-level job that is not running: queued, paused, failed
// or completed. For an unfinished download it also deletes the private partial
// files this job created next to its target; finished files, upload sources and
// anything on the server are never touched. A running job must be stopped
// first (Cancel does both).
func (m *Manager) Discard(id string) error { return m.remove(id, false) }

// Cancel stops a job wherever it runs (this process or another frontend) and
// then discards it. It waits for the run to release the job before touching
// any file, so a transfer never writes into a partial file being deleted.
func (m *Manager) Cancel(ctx context.Context, id string) error {
	if !validID.MatchString(id) {
		return errors.New("无效传输 ID")
	}
	if _, err := os.Lstat(m.jobDir(id)); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	m.Pause(id)
	for {
		release, err := m.lockJob(ctx, id)
		if err == nil {
			release()
			break
		}
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("任务仍在运行，未能取消，请稍后重试")
		case <-time.After(100 * time.Millisecond):
		}
	}
	return m.Discard(id)
}

// ClearFailed discards every failed top-level job, as Discard does.
func (m *Manager) ClearFailed() error {
	var result error
	for _, job := range m.List() {
		if job.State == "error" {
			result = errors.Join(result, m.Discard(job.ID))
		}
	}
	return result
}

func (m *Manager) ClearCompleted() error {
	var result error
	for _, job := range m.List() {
		if job.State == "done" {
			result = errors.Join(result, m.RemoveCompleted(job.ID))
		}
	}
	return result
}

func (m *Manager) remove(id string, completedOnly bool) error {
	if !validID.MatchString(id) {
		return errors.New("无效传输 ID")
	}
	metadataRelease, err := m.lockMetadata(context.Background())
	if err != nil {
		return err
	}
	defer metadataRelease()
	store, err := os.OpenRoot(m.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer store.Close()
	st, err := store.Lstat(id)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("传输记录目录无效")
	}
	root, err := store.OpenRoot(id)
	if err != nil {
		return err
	}
	defer root.Close()
	release, err := localipc.Acquire(m.jobDir(id))
	if err != nil {
		return errors.New("该任务正在运行，请先暂停或取消")
	}
	defer func() {
		if release != nil {
			release()
		}
	}()
	f, err := root.Open("state.json")
	if errors.Is(err, os.ErrNotExist) {
		if metadataTombstone(root) {
			root.Close()
			release()
			release = nil
			return store.RemoveAll(id)
		}
		return nil
	}
	if err != nil {
		return err
	}
	j, err := readJob(f, id)
	f.Close()
	if err != nil {
		return err
	}
	if j.ParentID != "" {
		return errors.New("目录内文件随所属下载任务移除")
	}
	if completedOnly && j.State != "done" {
		return errors.New("只能移除已完成的任务记录")
	}
	// Holding the job lock means no run owns it, so a stale running state is a
	// crashed run and its partial data is as discardable as a paused one.
	if j.State != "done" && j.Direction == "download" {
		if err := m.removeDownloadPartials(j, root); err != nil {
			return err
		}
	}
	if j.Directory {
		// Child state and the manifest live here; downloaded files live outside
		// this metadata tree and stay where they are.
		if err := root.RemoveAll("children"); err != nil {
			return err
		}
		if err := root.Remove("manifest.json"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := root.Remove("pause.request"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// State is removed last so an earlier cleanup failure remains retryable.
	if err := root.Remove("state.json"); err != nil {
		return err
	}
	// Windows cannot remove an open lock file. The store coordinator prevents
	// another run from opening a replacement lock while these handles close.
	root.Close()
	release()
	release = nil
	return store.RemoveAll(id)
}

// removeDownloadPartials deletes only the hidden ".name.ssh-part-ID" files a
// download created, through an os.Root at the target's directory so a path
// can never escape it. A partial that is not a private regular file is left
// alone and reported, and the record is kept so the user can look.
func (m *Manager) removeDownloadPartials(j Job, meta *os.Root) error {
	if !j.Directory {
		return removePartial(filepath.Dir(j.Local), partialName(filepath.Base(j.Local), j.ID))
	}
	target, err := os.OpenRoot(j.Local)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer target.Close()
	children, err := meta.OpenRoot("children")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer children.Close()
	entries, err := os.ReadDir(filepath.Join(m.jobDir(j.ID), "children"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var result error
	for _, entry := range entries {
		if !entry.IsDir() || !validID.MatchString(entry.Name()) {
			continue
		}
		f, err := children.Open(filepath.Join(entry.Name(), "state.json"))
		if err != nil {
			continue
		}
		child, err := readJob(f, entry.Name())
		f.Close()
		if err != nil || child.ParentID != j.ID || child.State == "done" {
			continue
		}
		rel, err := filepath.Rel(j.Local, child.Local)
		if err != nil || !filepath.IsLocal(rel) {
			result = errors.Join(result, errors.New("目录内传输记录路径越界，未删除部分文件"))
			continue
		}
		result = errors.Join(result, removePartialIn(target, filepath.Join(filepath.Dir(rel), partialName(filepath.Base(rel), child.ID))))
	}
	// The ownership marker names this job; drop it with the job.
	marker := ".smallssh-download-" + j.ID
	if st, err := target.Lstat(marker); err == nil && st.Mode().IsRegular() && st.Size() == int64(len(j.ID)) {
		result = errors.Join(result, target.Remove(marker))
	}
	return result
}

func partialName(base, id string) string { return "." + base + ".ssh-part-" + id }

func removePartial(dir, name string) error {
	root, err := os.OpenRoot(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	return removePartialIn(root, name)
}

func removePartialIn(root *os.Root, name string) error {
	st, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || (runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0) {
		return errors.New("部分文件类型或权限异常，未删除：" + name)
	}
	return root.Remove(name)
}
