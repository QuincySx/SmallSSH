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

// This one persistent lock protects the job-lock namespace, not transfer IO.
// A job can be acquired only while this lock is held; removal releases its job
// handle and removes the directory before allowing another acquisition.
func (m *Manager) lockMetadata(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := localipc.Prepare(m.dir); err != nil {
		return nil, err
	}
	name := filepath.Join(m.dir, ".metadata.lock")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err == nil {
		var entry os.FileInfo
		entry, err = os.Lstat(name)
		if err == nil && (!st.Mode().IsRegular() || !os.SameFile(st, entry) || runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0) {
			err = errors.New("传输元数据锁类型或权限无效")
		}
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	for {
		locked, err := tryMetadataLock(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		if locked {
			return func() { unlockMetadata(f); f.Close() }, nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			f.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (m *Manager) lockJob(ctx context.Context, id string) (func(), error) {
	release, err := m.lockMetadata(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	// Acquire creates its directory. Check first while namespace changes are
	// excluded, so a stale Run/List/Cancel cannot recreate a removed job.
	st, err := os.Lstat(m.jobDir(id))
	if err != nil {
		return nil, err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("传输记录目录无效")
	}
	return localipc.Acquire(m.jobDir(id))
}

// Older versions retained vault.lock after clearing state. Reclaim only such
// empty metadata; unrecognised contents and active job locks are left alone.
func (m *Manager) reclaimTombstone(id string) {
	release, err := m.lockMetadata(context.Background())
	if err != nil {
		return
	}
	defer release()
	store, err := os.OpenRoot(m.dir)
	if err != nil {
		return
	}
	defer store.Close()
	st, err := store.Lstat(id)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return
	}
	jobRelease, err := localipc.Acquire(m.jobDir(id))
	if err != nil {
		return
	}
	defer func() {
		if jobRelease != nil {
			jobRelease()
		}
	}()
	root, err := store.OpenRoot(id)
	if err != nil {
		return
	}
	defer root.Close()
	if !metadataTombstone(root) {
		return
	}
	root.Close()
	jobRelease()
	jobRelease = nil
	_ = store.RemoveAll(id)
}

func metadataTombstone(root *os.Root) bool {
	f, err := root.Open(".")
	if err != nil {
		return false
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Name() != "vault.lock" && entry.Name() != "pause.request" {
			return false
		}
	}
	return true
}
