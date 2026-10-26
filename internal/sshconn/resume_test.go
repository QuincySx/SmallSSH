package sshconn_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sshclient/internal/sshconn"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
	"testing"
)

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func TestResumeInterruptionPrefixesAndNoOverwrite(t *testing.T) {
	fixture := testserver.New(t)
	ctx := context.Background()
	c, e := sshconn.Dial(ctx, fixture.Profile())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	dir := t.TempDir()
	target := testserver.RemotePath(filepath.Join(dir, "resume.bin"))
	id := vault.ShortID()
	body := bytes.Repeat([]byte("content integrity\n"), 200000)
	hash := digest(body)
	cut := int64(700001)
	u, e := c.PrepareUpload(ctx, target, id, hash, int64(len(body)))
	if e != nil {
		t.Fatal(e)
	}
	if e = u.Finish(bytes.NewReader(body[:cut])); e == nil {
		t.Fatal("short upload succeeded")
	}
	u.Close()
	if _, e = os.Stat(testserver.LocalPath(target)); !os.IsNotExist(e) {
		t.Fatal("partial committed")
	}
	u, e = c.PrepareUpload(ctx, target, id, hash, int64(len(body)))
	if e != nil {
		t.Fatal(e)
	}
	if u.Meta.Offset != cut || u.Meta.PrefixHash != digest(body[:cut]) {
		t.Fatal(u.Meta)
	}
	if e = u.Finish(bytes.NewReader(body[cut:])); e != nil {
		t.Fatal(e)
	}
	u.Close()
	got, _ := os.ReadFile(testserver.LocalPath(target))
	if !bytes.Equal(got, body) {
		t.Fatal("mixed upload")
	}
	u, e = c.PrepareUpload(ctx, target, id, hash, int64(len(body)))
	if e != nil {
		t.Fatal(e)
	}
	if u.Meta.Offset != int64(len(body)) {
		t.Fatal("commit retry not idempotent")
	}
	if e = u.Finish(bytes.NewReader(nil)); e != nil {
		t.Fatal(e)
	}
	u.Close()
	if _, e = c.PrepareUpload(ctx, target, vault.ShortID(), digest([]byte("other")), 5); e == nil {
		t.Fatal("overwrite accepted")
	}
	d, e := c.PrepareDownload(ctx, target, cut, digest(body[:cut]), hash)
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e = d.Copy(&out); e != nil {
		t.Fatal(e)
	}
	d.Close()
	if !bytes.Equal(out.Bytes(), body[cut:]) {
		t.Fatal("bad resumed download")
	}
	if _, e = c.PrepareDownload(ctx, target, cut, digest([]byte("corrupted")), hash); e == nil {
		t.Fatal("corrupt prefix accepted")
	}
	changed := append([]byte(nil), body...)
	changed[len(changed)-1] ^= 1
	os.WriteFile(testserver.LocalPath(target), changed, 0600)
	if _, e = c.PrepareDownload(ctx, target, cut, digest(body[:cut]), hash); e == nil {
		t.Fatal("changed source accepted")
	}
	_ = io.Discard
}
