package desktop

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

func TestEditorWindowCloseAndStaleCallbacks(t *testing.T) {
	a := &app{}
	var windows []*fakeToolsWindow
	var requests []func()
	a.newEditorWindow = func(options mygo.WindowOptions, request, closed func()) toolsWindow {
		w := &fakeToolsWindow{options: options, onClosed: closed}
		windows = append(windows, w)
		requests = append(requests, request)
		return w
	}
	a.installEditor(localEndpoint, "/tmp/first.txt", "first", "")
	if len(windows) != 1 || !windows[0].options.Hidden || windows[0].shown != 1 || a.editShown {
		t.Fatal("document did not open in an independent window")
	}
	old := windows[0]
	a.edit.Buffer.Set("changed")
	requests[0]()
	if !a.prompt.open || a.editPath == "" || old.hidden != 0 {
		t.Fatal("dirty close did not request confirmation")
	}
	a.prompt.open = false
	a.edit.Buffer.MarkSaved()
	requests[0]()
	if a.editPath != "" || old.hidden != 1 {
		t.Fatal("clean close request did not close the document")
	}
	a.installEditor(localEndpoint, "/tmp/second.txt", "second", "")
	old.finishPendingUpdates()
	requests[0]()
	if len(windows) != 2 || a.editorWin != windows[1] || a.editPath != "/tmp/second.txt" {
		t.Fatal("stale window callback closed replacement document")
	}
}

func TestEditorWindowDefersCreationAndCancelsPending(t *testing.T) {
	var queued []func()
	a := &app{dispatch: func(fn func()) { queued = append(queued, fn) }}
	created := 0
	a.newEditorWindow = func(options mygo.WindowOptions, request, closed func()) toolsWindow {
		created++
		return &fakeToolsWindow{options: options, onClosed: closed}
	}
	a.installEditor(localEndpoint, "/tmp/pending.txt", "", "")
	if len(queued) != 1 || created != 0 || !a.editorWindowPending {
		t.Fatal("window creation was not deferred")
	}
	a.closeEditor()
	queued[0]()
	if created != 0 || a.editorWindowPending {
		t.Fatal("closed document created a delayed window")
	}
}

func TestLocalEditorSavePreservesCRLF(t *testing.T) {
	file := filepath.Join(t.TempDir(), "lines.txt")
	original := "one\r\ntwo\r\n"
	if err := os.WriteFile(file, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 4)
	a := &app{ctx: context.Background(), dispatch: func(fn func()) { updates <- fn }}
	a.installEditor(localEndpoint, file, original, textHash([]byte(original)))
	a.edit.Buffer.Set("changed\ntwo\n")
	a.saveLocalEditor(file, false)
	if a.editorSaving != a.edit {
		t.Fatal("local save did not protect the document while in flight")
	}
	select {
	case fn := <-updates:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("save did not complete")
	}
	got, err := os.ReadFile(file)
	if err != nil || string(got) != "changed\r\ntwo\r\n" || a.edit.Buffer.Modified() || a.editorSaving != nil {
		t.Fatalf("saved bytes %q, error %v, modified %v", got, err, a.edit.Buffer.Modified())
	}
}
