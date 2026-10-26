package fileattrs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

type testInfo struct {
	mode     os.FileMode
	uid, gid uint32
}

func (f testInfo) Name() string       { return "example" }
func (f testInfo) Size() int64        { return 8 }
func (f testInfo) Mode() os.FileMode  { return f.mode }
func (f testInfo) ModTime() time.Time { return time.Unix(10, 0) }
func (f testInfo) IsDir() bool        { return f.mode.IsDir() }
func (f testInfo) Sys() any           { return &sftp.FileStat{UID: f.uid, GID: f.gid} }

type testHandle struct {
	info     testInfo
	chmodErr error
	changes  int
}

func (f *testHandle) Stat() (os.FileInfo, error) { return f.info, nil }
func (f *testHandle) Chown(uid, gid int) error {
	f.changes++
	f.info.uid, f.info.gid = uint32(uid), uint32(gid)
	f.info.mode &^= os.ModeSetuid | os.ModeSetgid
	return nil
}
func (f *testHandle) Chmod(mode os.FileMode) error {
	f.changes++
	if f.chmodErr != nil {
		return f.chmodErr
	}
	f.info.mode = mode | f.info.mode&os.ModeType
	return nil
}

func TestAttributeChangePreservesOtherOwnerAndPostChownSpecialBits(t *testing.T) {
	f := &testHandle{info: testInfo{mode: os.ModeSetuid | 0750, uid: 1000, gid: 100}}
	c := Change{Expected: FromStat(f.info), SetUID: true, UID: 0, SetMode: true, Mode: 0640}
	info, err := Apply(f, c)
	if err != nil || info.UID != 0 || info.GID != 100 || info.Mode != 0640 {
		t.Fatal(info, err)
	}
	if f.changes != 2 {
		t.Fatal("missing ownership or permission operation")
	}
	f = &testHandle{info: testInfo{mode: os.ModeSetgid | 0750, uid: 1000, gid: 100}}
	info, err = Apply(f, Change{Expected: FromStat(f.info), SetMode: true, Mode: 0000})
	if err != nil || info.Mode != 02000 {
		t.Fatal("zero permissions or existing special bits lost", info, err)
	}
}

func TestAttributeChangeRejectsStaleMetadataAndLinksAndReportsPartialFailure(t *testing.T) {
	f := &testHandle{info: testInfo{mode: 0644, uid: 1000, gid: 100}}
	c := Change{Expected: FromStat(f.info), SetMode: true, Mode: 0600}
	f.info.mode = 0640
	if _, err := Apply(f, c); err == nil || f.changes != 0 {
		t.Fatal("stale file modified", err)
	}
	f.info.mode = os.ModeSymlink | 0777
	c.Expected = FromStat(f.info)
	if _, err := Apply(f, c); err == nil || f.changes != 0 {
		t.Fatal("symlink followed", err)
	}
	f.info.mode = 0644
	f.chmodErr = errors.New("permission denied")
	c = Change{Expected: FromStat(f.info), SetUID: true, UID: 0, SetMode: true, Mode: 0600}
	info, err := Apply(f, c)
	if err == nil || info.UID != 0 || info.Mode != 0644 || !strings.Contains(err.Error(), "不会回滚") {
		t.Fatal("partial change was hidden", info, err)
	}
}

func TestAccountsAreBoundedAndOnlyExposeNamesAndIDs(t *testing.T) {
	accounts, err := Accounts(strings.NewReader("root:secret-hash:0:0:private:/root:/bin/sh\nservice:x:1001:100\nbad name:x:12:3\ninvalid:x:-1:0\nroot:other:44:44\n"))
	if err != nil || len(accounts) != 2 {
		t.Fatal(accounts, err)
	}
	data, _ := json.Marshal(accounts)
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "private") {
		t.Fatal("account parser leaked other fields")
	}
	if id, err := ResolveAccount("root", accounts); err != nil || id != 0 {
		t.Fatal(id, err)
	}
	if id, err := ResolveAccount("12345", accounts); err != nil || id != 12345 {
		t.Fatal(id, err)
	}
	for _, value := range []string{"missing", "-1", "4294967295", "4294967296", "100;id"} {
		if _, err := ResolveAccount(value, accounts); err == nil {
			t.Fatal("invalid ID accepted", value)
		}
	}
	_, err = Accounts(strings.NewReader(strings.Repeat("x", 1<<20+2)))
	if err == nil {
		t.Fatal("unbounded account line accepted")
	}
}

func TestLocalAttributeRoundTripAndSymlinkTargetProtection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission assertion")
	}
	name := filepath.Join(t.TempDir(), "file ; $(not-a-command).txt")
	if err := os.WriteFile(name, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := LocalInfo(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	change := Change{Expected: info.Snapshot, SetMode: true, Mode: 0640}
	after, err := SetLocalAttributes(context.Background(), name, change)
	if err != nil || after.Mode&0777 != 0640 {
		t.Fatal(after, err)
	}
	if _, err = SetLocalAttributes(context.Background(), name, change); err == nil {
		t.Fatal("old snapshot was reusable")
	}
	link := filepath.Join(filepath.Dir(name), "link")
	if err = os.Symlink(name, link); err != nil {
		t.Fatal(err)
	}
	info, err = LocalInfo(context.Background(), link)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = SetLocalAttributes(context.Background(), link, Change{Expected: info.Snapshot, SetMode: true, Mode: 0777}); err == nil {
		t.Fatal("changed link target")
	}
	st, _ := os.Stat(name)
	if st.Mode().Perm() != 0640 {
		t.Fatal("link target changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = SetLocalAttributes(ctx, name, Change{Expected: after.Snapshot, SetMode: true, Mode: 0600}); err == nil {
		t.Fatal("cancelled write ran")
	}
}
