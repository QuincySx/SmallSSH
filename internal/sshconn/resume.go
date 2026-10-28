package sshconn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/pkg/sftp"
	"hash"
	"io"
	"os"
	"path"
	"regexp"
)

type TransferMeta struct {
	Size       int64  `json:"size"`
	Offset     int64  `json:"offset"`
	Hash       string `json:"hash"`
	PrefixHash string `json:"prefixHash"`
}

var transferID = regexp.MustCompile(`^[A-Za-z0-9_-]{24}$`)

type UploadSession struct {
	Meta         TransferMeta
	s            *sftp.Client
	file         *sftp.File
	part, target string
	hash         hash.Hash
	complete     bool
	stop         func() bool
}

func (c *Client) PrepareUpload(ctx context.Context, p, id, expected string, size int64) (*UploadSession, error) {
	return c.prepareUpload(ctx, p, id, expected, size, false)
}

func (c *Client) PrepareNewUpload(ctx context.Context, p, id, expected string, size int64) (*UploadSession, error) {
	return c.prepareUpload(ctx, p, id, expected, size, true)
}

func (c *Client) prepareUpload(ctx context.Context, p, id, expected string, size int64, createOnly bool) (*UploadSession, error) {
	if e := remotePath(p); e != nil {
		return nil, e
	}
	if !transferID.MatchString(id) || size < 0 || !validHash(expected) {
		return nil, errors.New("续传标识、大小或文件指纹无效")
	}
	s, e := sftp.NewClient(c.SSH)
	if e != nil {
		return nil, e
	}
	u := &UploadSession{s: s, target: p, part: path.Join(path.Dir(p), ".ssh-part-"+id), hash: sha256.New(), Meta: TransferMeta{Size: size, Hash: expected}}
	u.stop = context.AfterFunc(ctx, func() { s.Close() })
	success := false
	defer func() {
		if !success {
			u.Close()
		}
	}()
	if info, e := s.Lstat(p); e == nil {
		if createOnly {
			return nil, errors.New("新上传目标已存在，不会覆盖或复用")
		}
		if !info.Mode().IsRegular() || info.Size() != size {
			return nil, errors.New("目标已存在且不同，续传不会覆盖")
		}
		f, e := s.Open(p)
		if e != nil {
			return nil, e
		}
		_, e = io.Copy(u.hash, f)
		f.Close()
		if e != nil {
			return nil, e
		}
		if hex.EncodeToString(u.hash.Sum(nil)) != expected {
			return nil, errors.New("目标已存在且内容不同")
		}
		u.Meta.Offset = size
		u.Meta.PrefixHash = expected
		u.complete = true
		success = true
		return u, nil
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	var info os.FileInfo
	if !createOnly {
		info, e = s.Lstat(u.part)
	} else {
		e = os.ErrNotExist
	}
	if os.IsNotExist(e) {
		u.file, e = s.OpenFile(u.part, os.O_RDWR|os.O_CREATE|os.O_EXCL)
		if e == nil {
			e = u.file.Chmod(0600)
		}
	} else if e == nil {
		if !info.Mode().IsRegular() || info.Size() > size {
			return nil, errors.New("远程部分文件类型或大小不匹配")
		}
		u.file, e = s.OpenFile(u.part, os.O_RDWR)
	}
	if e != nil {
		return nil, e
	}
	st, e := u.file.Stat()
	if e != nil {
		return nil, e
	}
	u.Meta.Offset = st.Size()
	if _, e = io.Copy(u.hash, u.file); e != nil {
		return nil, e
	}
	u.Meta.PrefixHash = hex.EncodeToString(u.hash.Sum(nil))
	if _, e = u.file.Seek(u.Meta.Offset, io.SeekStart); e != nil {
		return nil, e
	}
	success = true
	return u, nil
}
func validHash(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func (u *UploadSession) Finish(r io.Reader) error {
	if u.complete {
		var b [1]byte
		n, e := r.Read(b[:])
		if n != 0 || e != io.EOF {
			return errors.New("已完成文件不接受额外数据")
		}
		return nil
	}
	remaining := u.Meta.Size - u.Meta.Offset
	n, e := io.Copy(u.file, io.TeeReader(io.LimitReader(r, remaining), u.hash))
	if e != nil {
		return e
	}
	if n != remaining {
		return fmt.Errorf("传输中断，已保留 %d 字节供续传", u.Meta.Offset+n)
	}
	var extra [1]byte
	nextra, e := r.Read(extra[:])
	if nextra != 0 || e != io.EOF {
		return errors.New("收到超出批准大小的数据")
	}
	if hex.EncodeToString(u.hash.Sum(nil)) != u.Meta.Hash {
		return errors.New("上传校验失败，部分文件已保留")
	}
	if e = u.file.Close(); e != nil {
		return e
	}
	if e = u.s.Link(u.part, u.target); e != nil {
		return fmt.Errorf("无覆盖提交失败，部分文件已保留: %w", e)
	}
	return u.s.Remove(u.part)
}
func (u *UploadSession) Close() {
	if u.stop != nil {
		u.stop()
	}
	if u.file != nil {
		u.file.Close()
	}
	if u.s != nil {
		u.s.Close()
	}
}

type DownloadSession struct {
	Meta TransferMeta
	s    *sftp.Client
	file *sftp.File
	stop func() bool
}

func (c *Client) PrepareDownload(ctx context.Context, p string, offset int64, prefix, expected string) (*DownloadSession, error) {
	if e := remotePath(p); e != nil {
		return nil, e
	}
	if offset < 0 || (offset > 0 && !validHash(prefix)) || (expected != "" && !validHash(expected)) {
		return nil, errors.New("下载续传参数无效")
	}
	s, e := sftp.NewClient(c.SSH)
	if e != nil {
		return nil, e
	}
	d := &DownloadSession{s: s}
	d.stop = context.AfterFunc(ctx, func() { s.Close() })
	success := false
	defer func() {
		if !success {
			d.Close()
		}
	}()
	d.file, e = s.Open(p)
	if e != nil {
		return nil, e
	}
	info, e := d.file.Stat()
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() || offset > info.Size() {
		return nil, errors.New("文件类型或续传位置不匹配")
	}
	h := sha256.New()
	if _, e = io.CopyN(h, d.file, offset); e != nil {
		return nil, e
	}
	ph := hex.EncodeToString(h.Sum(nil))
	if offset > 0 && ph != prefix {
		return nil, errors.New("下载部分内容与远端前缀不同，拒绝混合文件")
	}
	n, e := io.Copy(h, d.file)
	if e != nil {
		return nil, e
	}
	if n+offset != info.Size() {
		return nil, errors.New("远程文件在校验时发生变化")
	}
	full := hex.EncodeToString(h.Sum(nil))
	if expected != "" && full != expected {
		return nil, errors.New("远程文件内容已变化，请新建下载任务")
	}
	d.Meta = TransferMeta{Size: info.Size(), Offset: offset, Hash: full, PrefixHash: ph}
	if _, e = d.file.Seek(offset, io.SeekStart); e != nil {
		return nil, e
	}
	success = true
	return d, nil
}
func (d *DownloadSession) Copy(w io.Writer) error {
	n, e := io.Copy(w, io.LimitReader(d.file, d.Meta.Size-d.Meta.Offset))
	if e == nil && n != d.Meta.Size-d.Meta.Offset {
		return io.ErrUnexpectedEOF
	}
	return e
}
func (d *DownloadSession) Close() {
	if d.stop != nil {
		d.stop()
	}
	if d.file != nil {
		d.file.Close()
	}
	if d.s != nil {
		d.s.Close()
	}
}
