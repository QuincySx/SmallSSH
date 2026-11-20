package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"sshclient/internal/approval"
	"sshclient/internal/client"
	"sshclient/internal/connectcfg"
)

const maxDirectoryFiles = 10000
const maxDirectoryFolders = 2048

type directoryFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}
type directoryManifest struct {
	Version int             `json:"version"`
	Folders []string        `json:"folders"`
	Files   []directoryFile `json:"files"`
}

// AddDownload reserves a persistent file or directory download. All remote
// reads remain structured SFTP operations; this never executes an archive tool.
func (m *Manager) AddDownload(host, local, remote, endpoint, fingerprint string, directory bool, route ...connectcfg.RouteSelection) (Job, error) {
	if !path.IsAbs(remote) || path.Clean(remote) != remote || strings.ContainsAny(remote, "\x00\r\n") {
		return Job{}, errors.New("远程目录需为规范的绝对路径")
	}
	j, err := m.Add(host, "download", local, remote, route...)
	if err != nil {
		return j, err
	}
	release, err := m.lockJob(context.Background(), j.ID)
	if err != nil {
		return j, err
	}
	defer release()
	j.Directory, j.Endpoint, j.HostFingerprint = directory, endpoint, fingerprint
	err = m.save(j)
	return j, err
}

// Restrict names to portable single components, including on a case-insensitive
// target. Remote data cannot provide local paths or overwrite task metadata.
func directoryName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".smallssh-download-") || strings.TrimRight(name, " .") != name || strings.ContainsAny(name, "/\\:*?\"<>|") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	base := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return false
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9' {
		return false
	}
	return true
}

func (m *Manager) directoryStream(ctx context.Context, j Job, kind, remote, label string, owner client.OwnerProof) (*client.Stream, error) {
	op := approval.Operation{Host: j.Host, Endpoint: j.Endpoint, HostFingerprint: j.HostFingerprint, Kind: kind, Path: remote, Client: label}
	if client.HasOwnerProof(owner) {
		return m.client.StartWithOwner(ctx, op, owner)
	}
	r, ticket, err := m.client.Request(ctx, op, 15*time.Minute)
	if err != nil {
		return nil, err
	}
	if err = j.checkTarget(r.Operation); err != nil {
		return nil, err
	}
	if err = m.client.Wait(ctx, r.ID, ticket); err != nil {
		return nil, err
	}
	return m.client.Start(ctx, r.ID, ticket, 0, 0)
}

func (m *Manager) scanDirectory(ctx context.Context, j *Job, label string, owner client.OwnerProof, update func(bool) error) (directoryManifest, error) {
	manifest := directoryManifest{Version: 1}
	stream, err := m.directoryStream(ctx, *j, "file-info", j.Remote, label, owner)
	if err != nil {
		return manifest, err
	}
	info, err := stream.ReadFileInfo()
	stream.Close()
	if err != nil {
		return manifest, err
	}
	if os.FileMode(info.Type) != os.ModeDir {
		return manifest, errors.New("远程源不是普通目录；不会跟随符号链接")
	}
	queue := []string{""}
	seen := map[string]bool{}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return manifest, err
		}
		rel := queue[0]
		queue = queue[1:]
		stream, err := m.directoryStream(ctx, *j, "list", path.Join(j.Remote, rel), label, owner)
		if err != nil {
			return manifest, err
		}
		// Receive bounded batches directly: a hostile listing cannot allocate an
		// unbounded slice before the entry/depth limits have been checked.
		for {
			message, receiveErr := stream.Receive()
			if receiveErr != nil {
				stream.Close()
				return manifest, receiveErr
			}
			for _, file := range message.Files {
				child := path.Join(rel, file.Name)
				key := strings.ToLower(child)
				if !directoryName(file.Name) || seen[key] || strings.Count(child, "/") >= 64 {
					stream.Close()
					return manifest, fmt.Errorf("目录包含不安全、重名或过深的路径: %q", child)
				}
				seen[key] = true
				if file.Directory && strings.HasPrefix(file.Mode, "d") {
					manifest.Folders = append(manifest.Folders, child)
					queue = append(queue, child)
				} else if !file.Directory && strings.HasPrefix(file.Mode, "-") && file.Size >= 0 {
					manifest.Files = append(manifest.Files, directoryFile{child, file.Size})
				} else {
					stream.Close()
					return manifest, fmt.Errorf("目录包含符号链接或特殊文件，请单独处理: %q", child)
				}
				if len(manifest.Files) > maxDirectoryFiles || len(manifest.Folders) > maxDirectoryFolders {
					stream.Close()
					return manifest, errors.New("目录超过单任务上限（10000 文件 / 2048 文件夹），请分批下载")
				}
			}
			if message.Action == "done" {
				break
			}
		}
		stream.Close()
		j.FilesTotal = len(manifest.Files)
		if err = update(false); err != nil {
			return manifest, err
		}
	}
	return manifest, nil
}

func validateManifest(manifest directoryManifest) error {
	if manifest.Version != 1 || len(manifest.Files) > maxDirectoryFiles || len(manifest.Folders) > maxDirectoryFolders {
		return errors.New("目录传输清单无效")
	}
	seen := map[string]bool{}
	valid := func(rel string) bool {
		if rel == "" || path.IsAbs(rel) || path.Clean(rel) != rel || strings.Count(rel, "/") >= 64 {
			return false
		}
		for _, component := range strings.Split(rel, "/") {
			if !directoryName(component) {
				return false
			}
		}
		key := strings.ToLower(rel)
		if seen[key] {
			return false
		}
		seen[key] = true
		return true
	}
	for _, rel := range manifest.Folders {
		if !valid(rel) {
			return errors.New("目录传输清单路径无效")
		}
	}
	for _, file := range manifest.Files {
		if !valid(file.Path) || file.Size < 0 {
			return errors.New("目录传输清单文件无效")
		}
	}
	return nil
}

func (m *Manager) directoryManifest(ctx context.Context, j *Job, label string, owner client.OwnerProof, update func(bool) error) (directoryManifest, error) {
	name := filepath.Join(m.jobDir(j.ID), "manifest.json")
	manifest := directoryManifest{}
	f, err := os.Open(name)
	if err == nil {
		defer f.Close()
		decoder := json.NewDecoder(io.LimitReader(f, 4<<20))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&manifest); err != nil {
			return manifest, err
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			return manifest, errors.New("目录传输清单过长或包含多余内容")
		}
		return manifest, validateManifest(manifest)
	}
	if !os.IsNotExist(err) {
		return manifest, err
	}
	manifest, err = m.scanDirectory(ctx, j, label, owner, update)
	if err != nil {
		return manifest, err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return manifest, err
	}
	if len(data) >= 4<<20 {
		return manifest, errors.New("目录传输清单超过 4 MiB，请分批下载")
	}
	f, err = os.CreateTemp(m.jobDir(j.ID), ".manifest-")
	if err != nil {
		return manifest, err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), name)
	}
	return manifest, err
}

func (m *Manager) downloadDirectory(ctx context.Context, j *Job, label string, owner client.OwnerProof, update func(bool) error) error {
	j.State, j.Phase, j.Error, j.Speed = "running", "scanning", "", 0
	if err := update(true); err != nil {
		return err
	}
	manifest, err := m.directoryManifest(ctx, j, label, owner, update)
	if err != nil {
		return err
	}
	j.Size, j.Bytes, j.FilesDone, j.FilesTotal = 0, 0, 0, len(manifest.Files)
	for _, file := range manifest.Files {
		if file.Size > math.MaxInt64-j.Size {
			return errors.New("目录总大小超出范围")
		}
		j.Size += file.Size
	}
	marker := ".smallssh-download-" + j.ID
	if err = os.Mkdir(j.Local, 0700); err == nil {
		root, openErr := os.OpenRoot(j.Local)
		if openErr != nil {
			return openErr
		}
		f, createErr := root.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if createErr == nil {
			_, createErr = f.WriteString(j.ID)
			closeErr := f.Close()
			if createErr == nil {
				createErr = closeErr
			}
		}
		root.Close()
		if createErr != nil {
			return createErr
		}
	} else if !os.IsExist(err) {
		return err
	}
	st, err := os.Lstat(j.Local)
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("目录下载目标类型已变化")
	}
	root, err := os.OpenRoot(j.Local)
	if err != nil {
		return err
	}
	defer root.Close()
	markerInfo, err := root.Lstat(marker)
	if err != nil || !markerInfo.Mode().IsRegular() || markerInfo.Size() != int64(len(j.ID)) {
		return errors.New("本地目录不属于此任务，不会合并或覆盖")
	}
	f, err := root.Open(marker)
	if err != nil {
		return errors.New("本地目录已存在且不属于此任务，不会合并或覆盖")
	}
	data, err := io.ReadAll(io.LimitReader(f, 128))
	f.Close()
	if err != nil || string(data) != j.ID {
		return errors.New("目录续传标记无效，不会写入此目录")
	}
	for _, rel := range manifest.Folders {
		if err := root.MkdirAll(filepath.FromSlash(rel), 0700); err != nil {
			return err
		}
	}
	j.Phase = "downloading"
	if err = update(true); err != nil {
		return err
	}
	// Keep child state below its parent so listing the desktop queue remains
	// proportional to visible jobs, rather than every file in a large folder.
	children := NewWithClient(m.client)
	children.dir = filepath.Join(m.jobDir(j.ID), "children")
	for _, file := range manifest.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		digest := sha256.Sum256([]byte(j.ID + "\x00" + file.Path))
		id := hex.EncodeToString(digest[:12])
		child, readErr := children.read(id)
		local, remote := filepath.Join(j.Local, filepath.FromSlash(file.Path)), path.Join(j.Remote, file.Path)
		if os.IsNotExist(readErr) {
			child = Job{ID: id, ParentID: j.ID, Host: j.Host, Route: j.Route, Direction: "download", Local: local, Remote: remote, Endpoint: j.Endpoint, HostFingerprint: j.HostFingerprint, Size: file.Size, State: "queued", Created: time.Now().UTC()}
			if err = children.save(child); err != nil {
				return err
			}
		} else if readErr != nil {
			return readErr
		}
		if child.ParentID != j.ID || child.Local != local || child.Remote != remote || child.Size != file.Size || child.Endpoint != j.Endpoint || child.HostFingerprint != j.HostFingerprint {
			return errors.New("目录内传输记录与当前清单不一致")
		}
		if child.State == "done" {
			name := filepath.FromSlash(file.Path)
			st, statErr := root.Lstat(name)
			if os.IsNotExist(statErr) {
				child.State, child.Bytes = "queued", 0
				if err = children.save(child); err != nil {
					return err
				}
			} else {
				if statErr != nil {
					return statErr
				}
				if !st.Mode().IsRegular() || st.Size() != child.Size {
					return errors.New("已下载文件被修改，不会覆盖")
				}
				f, err := root.Open(name)
				if err != nil {
					return err
				}
				h := sha256.New()
				_, err = io.Copy(h, f)
				f.Close()
				if err != nil {
					return err
				}
				if hex.EncodeToString(h.Sum(nil)) != child.Hash {
					return errors.New("已下载文件内容已变化，不会覆盖")
				}
			}
		}
		base := j.Bytes
		err = children.run(ctx, id, label, owner, runOptions{root: root, base: j.Local, progress: func(current Job) {
			j.Bytes, j.Speed, j.RequestID = base+current.Bytes, current.Speed, current.RequestID
			_ = update(false)
		}})
		if err != nil {
			return fmt.Errorf("%s: %w", file.Path, err)
		}
		j.Bytes, j.Speed = base+file.Size, 0
		j.FilesDone++
		if err = update(true); err != nil {
			return err
		}
	}
	j.Phase = ""
	j.State = "done"
	// Persist completion before removing the marker. A crash can leave an
	// extra marker, but cannot make an unfinished group merge into a new root.
	if err := update(true); err != nil {
		return err
	}
	_ = root.Remove(marker)
	return nil
}
