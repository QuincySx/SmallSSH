package broker_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sshclient/internal/approval"
	"sshclient/internal/fileattrs"
	"sshclient/internal/testserver"
	"testing"
)

func TestSFTPAttributesRoundTripRequiresApprovalAndRejectsStaleTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission assertion")
	}
	c, fixture := setup(t)
	name := filepath.Join(t.TempDir(), "config ; $(no-shell).txt")
	if err := os.WriteFile(name, []byte("unchanged bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	remote := testserver.RemotePath(name)
	r, ticket := request(t, c, approval.Operation{Host: "fixture", Kind: "file-info", Path: remote, Client: "attribute test"})
	approve(t, c, r)
	stream, err := c.Start(context.Background(), r.ID, ticket, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.ReadFileInfo()
	stream.Close()
	if err != nil || !info.Valid || info.Mode&0777 != 0600 {
		t.Fatal(info, err)
	}
	change := fileattrs.Change{Expected: info.Snapshot, SetMode: true, Mode: 0640, SetUID: true, UID: info.UID, SetGID: true, GID: info.GID}
	op := approval.Operation{Host: "fixture", Kind: "file-attributes", Path: remote, Client: "attribute test", Attributes: change}
	r, ticket = request(t, c, op)
	if stream, e := c.Start(context.Background(), r.ID, ticket, 0, 0); e == nil {
		stream.Close()
		t.Fatal("unapproved attribute change executed")
	}
	approve(t, c, r)
	stream, err = c.Start(context.Background(), r.ID, ticket, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	after, err := stream.ReadFileInfo()
	stream.Close()
	if err != nil || after.Mode&0777 != 0640 || after.UID != info.UID || after.GID != info.GID {
		t.Fatal(after, err)
	}
	data, _ := os.ReadFile(name)
	if string(data) != "unchanged bytes" {
		t.Fatal("attribute update touched file data")
	}
	r, ticket = request(t, c, op)
	approve(t, c, r)
	stream, err = c.Start(context.Background(), r.ID, ticket, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	current, err := stream.ReadFileInfo()
	stream.Close()
	if err == nil || !current.Valid || current.Mode&0777 != 0640 {
		t.Fatal("stale approved target changed", current, err)
	}
	folder := filepath.Join(filepath.Dir(name), "folder")
	if err = os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	r, ticket = request(t, c, approval.Operation{Host: "fixture", Kind: "file-info", Path: testserver.RemotePath(folder), Client: "attribute test"})
	approve(t, c, r)
	stream, err = c.Start(context.Background(), r.ID, ticket, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := stream.ReadFileInfo()
	stream.Close()
	if err != nil || directory.Type != uint32(os.ModeDir) {
		t.Fatal(directory, err)
	}
	r, ticket = request(t, c, approval.Operation{Host: "fixture", Kind: "file-attributes", Path: testserver.RemotePath(folder), Client: "attribute test",
		Attributes: fileattrs.Change{Expected: directory.Snapshot, SetMode: true, Mode: 0750}})
	approve(t, c, r)
	stream, err = c.Start(context.Background(), r.ID, ticket, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	directory, err = stream.ReadFileInfo()
	stream.Close()
	if err != nil || directory.Mode&0777 != 0750 {
		t.Fatal("SFTP directory attributes failed", directory, err)
	}
	select {
	case command := <-fixture.Started:
		t.Fatal("attribute operation executed a shell command", command)
	default:
	}
}
