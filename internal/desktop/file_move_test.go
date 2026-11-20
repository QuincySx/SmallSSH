package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/connectcfg"
	"sshclient/internal/sshdata"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func dragFile(t *testing.T, tt *ui.Tester, source, target string) {
	t.Helper()
	from, sourceOK := tt.Find(source)
	to, targetOK := tt.Find(target)
	if !sourceOK || !targetOK {
		t.Fatal("missing drag source or destination", source, sourceOK, target, targetOK, tt.Texts())
	}
	x, y := from.X+min(40, from.W/2), from.Y+from.H/2
	tx, ty := to.X+min(40, to.W/2), to.Y+to.H/2
	tt.Press(x, y)
	tt.Move(x+12, y)
	tt.Move(tx, ty)
	tt.Release(tx, ty)
}

func TestFileDragMovesSelectionIntoFolderAndBackThroughTree(t *testing.T) {
	h := newConnectionFlowHarness(t)
	profile := h.fixture.Profile()
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: profile}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	for _, local := range []bool{true, false} {
		mode := "SFTP"
		if local {
			mode = "local"
		}
		t.Run(mode, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "source-folder")
			child := filepath.Join(root, "target-folder")
			if err := os.MkdirAll(child, 0700); err != nil {
				t.Fatal(err)
			}
			for name, content := range map[string]string{
				filepath.Join(root, "notes.txt"):    "move me",
				filepath.Join(root, "same.txt"):     "keep at source",
				filepath.Join(child, "same.txt"):    "keep at target",
				filepath.Join(child, "outside.txt"): "move to parent",
			} {
				if err := os.WriteFile(name, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			parentDir, childDir, endpoint := root, child, localEndpoint
			if !local {
				parentDir, childDir, endpoint = testserver.RemotePath(root), testserver.RemotePath(child), profile.ID
			}
			p := newFilePane(endpoint, false)
			p.treeHidden = true
			b := p.browser()
			h.a.applyDirectory(b, local, parentDir, []sshdata.File{
				{Name: "target-folder", Directory: true}, {Name: "notes.txt"}, {Name: "same.txt"},
			}, b.seq, true)
			// The destination may already show the moved entry if it was
			// refreshed before the operation's UI callback.
			h.a.applyDirectory(b, local, childDir, []sshdata.File{{Name: "notes.txt"}, {Name: "outside.txt"}, {Name: "same.txt"}}, b.seq, false)
			b.chosen.Add("notes.txt")
			b.chosen.Add("same.txt")
			h.ui = ui.NewTester(func(c *ui.Context) { h.a.browserPane(c, p) }, 950, 540)
			dragFile(t, h.ui, "notes.txt", "target-folder")
			h.wait(func() bool { return !h.a.busy })
			if b.chosen.Len() != 1 || !b.chosen.Has("same.txt") || !strings.Contains(b.moveFeedback, "已移动 1/2 项") {
				t.Fatal("partial move lost selection or feedback", b.moveFeedback, b.chosen.Len())
			}
			if len(b.cache[parentDir]) != 2 || len(b.cache[childDir]) != 3 {
				t.Fatal("move failed to reconcile the file lists", b.cache[parentDir], b.cache[childDir])
			}
			assertMovedFile(t, filepath.Join(child, "notes.txt"), "move me")
			assertMovedFile(t, filepath.Join(root, "same.txt"), "keep at source")
			assertMovedFile(t, filepath.Join(child, "same.txt"), "keep at target")
			if _, err := os.Stat(filepath.Join(root, "notes.txt")); !os.IsNotExist(err) {
				t.Fatal("source still exists after moving", err)
			}
			p.treeHidden = false
			b.showDirectory(local, childDir)
			h.ui.Frame()
			dragFile(t, h.ui, "outside.txt", "source-folder")
			h.wait(func() bool { return !h.a.busy })
			assertMovedFile(t, filepath.Join(root, "outside.txt"), "move to parent")
			if _, err := os.Stat(filepath.Join(child, "outside.txt")); !os.IsNotExist(err) {
				t.Fatal("parent drop left the source in the child directory", err)
			}
			p.treeHidden = true
			h.ui.Frame()
			dragFile(t, h.ui, "notes.txt", "上一级")
			h.wait(func() bool { return !h.a.busy })
			assertMovedFile(t, filepath.Join(root, "notes.txt"), "move me")
		})
	}
}

func assertMovedFile(t *testing.T, name, want string) {
	t.Helper()
	content, err := os.ReadFile(name)
	if err != nil || string(content) != want {
		t.Fatalf("%s: contents %q, want %q (%v)", name, content, want, err)
	}
}

func TestFileDragRejectsDifferentRoutesAndFolderCycles(t *testing.T) {
	a := fileFollowDemo()
	p := newFilePane("host", false)
	b := p.browser()
	b.showDirectory(false, "/srv")
	d := fileDrag{endpoint: p.endpoint, dir: b.dir, file: sshdata.File{Name: "assets", Directory: true}, browser: b}
	for _, target := range []string{"/srv", "/srv/assets", "/srv/assets/nested"} {
		if a.canMoveDroppedFiles(d, p, target) {
			t.Fatal("accepted a no-op or cyclic directory move", target)
		}
	}
	if !a.canMoveDroppedFiles(d, p, "/srv/other") {
		t.Fatal("rejected a valid move")
	}
	p.route = connectcfg.RouteSelection{Mode: "direct"}
	if a.canMoveDroppedFiles(d, p, "/srv/other") {
		t.Fatal("accepted a move to a pane using a different route")
	}
}
