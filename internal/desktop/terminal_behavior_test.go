//go:build terminal_integration

package desktop

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

type terminalRecorder struct {
	reader *io.PipeReader
	writer *io.PipeWriter
	mu     sync.Mutex
	input  bytes.Buffer
}

func newTerminalRecorder() *terminalRecorder {
	r, w := io.Pipe()
	return &terminalRecorder{reader: r, writer: w}
}
func (c *terminalRecorder) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *terminalRecorder) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.input.Write(p)
}
func (c *terminalRecorder) Close() error { c.writer.Close(); return c.reader.Close() }
func (c *terminalRecorder) take() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := c.input.String()
	c.input.Reset()
	return v
}
func expectTerminalInput(t *testing.T, c *terminalRecorder, want string) {
	t.Helper()
	got := ""
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got += c.take()
		if len(got) >= len(want) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if got != want {
		t.Fatalf("input bytes: got %q want %q", got, want)
	}
}

func TestTerminalKeysUnicodeAndBracketedPaste(t *testing.T) {
	c := newTerminalRecorder()
	term, e := terminal.New(terminal.Options{Conn: c, OptionAsAlt: true, Scrollback: 32 << 20, ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
	if e != nil {
		t.Fatal(e)
	}
	defer term.Close()
	a := &app{}
	s := &session{term: term}
	tt := ui.NewTester(func(ctx *ui.Context) { a.nativeTerminal(ctx, s) }, 800, 400)
	if e = tt.Click("Terminal"); e != nil {
		t.Fatal(e)
	}
	for _, k := range []ui.Key{ui.KeyUp, ui.KeyDown, ui.KeyLeft, ui.KeyRight} {
		tt.Key(0, k)
	}
	expectTerminalInput(t, c, "\x1b[A\x1b[B\x1b[D\x1b[C")
	term.Feed([]byte("\x1b[?1h"))
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyLeft)
	expectTerminalInput(t, c, "\x1bOA\x1bOD")
	term.Feed([]byte("\x1b[?1l"))
	for _, k := range []ui.Key{ui.KeyC, ui.KeyR, ui.KeyA, ui.KeyZ, ui.KeyJ} {
		tt.Key(ui.Ctrl, k)
	}
	expectTerminalInput(t, c, "\x03\x12\x01\x1a\n")
	for _, k := range []ui.Key{ui.KeyHome, ui.KeyEnd, ui.KeyDelete, ui.KeyBackspace, ui.KeyTab, ui.KeyEscape} {
		tt.Key(0, k)
	}
	expectTerminalInput(t, c, "\x1b[H\x1b[F\x1b[3~\x7f\t\x1b")
	text := "中文🙂 e\u0301 \\ '$HOME' `literal`"
	tt.Type(text)
	expectTerminalInput(t, c, text)
	term.Feed([]byte("\x1b[?2004h"))
	paste := "first line\nsecond\t'$x'\\"
	tt.SetClipboard(paste)
	mods := ui.Ctrl | ui.Shift
	if runtime.GOOS == "darwin" {
		mods = ui.Super
	}
	tt.Key(mods, ui.KeyV)
	expectTerminalInput(t, c, "\x1b[200~"+paste+"\x1b[201~")
	// UTF-8 may cross SSH packet boundaries; VT parsing keeps the character whole.
	for _, b := range []byte("\x1b[31m分包中文🙂\x1b[0m\r\n") {
		term.Feed([]byte{b})
	}
	if !strings.Contains(term.Text(), "分包中文🙂") || strings.Contains(term.Text(), "[31m") {
		t.Fatal(term.Text())
	}
	term.Feed([]byte("\x1b[?1049hALT_SCREEN\x1b[?1049l"))
	if strings.Contains(term.Text(), "ALT_SCREEN") || !strings.Contains(term.Text(), "分包中文🙂") {
		t.Fatal("alternate screen leaked into normal scrollback", term.Text())
	}
}

func TestRemoteStyleInteractiveShellUsesExistingHistory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX PTY history test; Windows terminal byte tests remain enabled")
	}
	dir := t.TempDir()
	history := filepath.Join(dir, "history")
	if e := os.WriteFile(history, []byte("printf 'saved-history-ok\\n'\n"), 0600); e != nil {
		t.Fatal(e)
	}
	term, e := terminal.New(terminal.Options{Command: []string{"/bin/bash", "--noprofile", "--norc", "-i"}, Dir: dir, Env: []string{"HISTFILE=" + history, "HISTSIZE=1000", "PS1=qa> ", "PROMPT_COMMAND=", "LC_ALL=en_US.UTF-8"}, ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
	if e != nil {
		t.Fatal(e)
	}
	defer term.Close()
	tt := ui.NewTester(func(c *ui.Context) { terminal.View(c, term).Fill().AutoFocus() }, 800, 400)
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(term.Text(), "qa>") {
		if time.Now().After(deadline) {
			t.Fatal(term.Text())
		}
		time.Sleep(5 * time.Millisecond)
	}
	tt.Click("Terminal")
	tt.Key(0, ui.KeyUp)
	deadline = time.Now().Add(2 * time.Second)
	for !strings.Contains(term.Text(), "saved-history-ok") {
		if time.Now().After(deadline) {
			t.Fatal("shell did not load history", term.Text())
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Enter executes the recalled command in the shell itself.
	tt.Key(0, ui.KeyEnter)
	deadline = time.Now().Add(2 * time.Second)
	for !strings.Contains(term.Text(), "\nsaved-history-ok") {
		if time.Now().After(deadline) {
			t.Fatal(term.Text())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got, _ := os.ReadFile(history); string(got) != "printf 'saved-history-ok\\n'\n" {
		t.Fatal("history changed before shell exit")
	}
	before := strings.Count(term.Text(), "\nsaved-history-ok")
	tt.Key(ui.Ctrl, ui.KeyR)
	tt.Type("saved-history")
	tt.Key(0, ui.KeyEnter)
	deadline = time.Now().Add(2 * time.Second)
	for strings.Count(term.Text(), "\nsaved-history-ok") <= before {
		if time.Now().After(deadline) {
			t.Fatal("Ctrl+R history search did not execute recalled command", term.Text())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func BenchmarkTerminalOutputAndFrame(b *testing.B) {
	c := newTerminalRecorder()
	term, e := terminal.New(terminal.Options{Conn: c, Scrollback: 32 << 20})
	if e != nil {
		b.Fatal(e)
	}
	defer term.Close()
	tt := ui.NewTester(func(ctx *ui.Context) { terminal.View(ctx, term).Fill() }, 1000, 650)
	chunk := bytes.Repeat([]byte("\x1b[32m正常 output 12345\x1b[0m\r\n"), 128)
	b.SetBytes(int64(len(chunk)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		term.Feed(chunk)
		tt.Frame()
	}
}

func TestClickingTerminalTabImmediatelyRoutesTypingToThatSession(t *testing.T) {
	first, second := newTerminalRecorder(), newTerminalRecorder()
	one, err := terminal.New(terminal.Options{Conn: first, ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()
	two, err := terminal.New(terminal.Options{Conn: second, ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
	if err != nil {
		t.Fatal(err)
	}
	defer two.Close()
	a := &app{online: true, page: 1, toolsHidden: true, sidebarWidth: 190, groups: map[string]*bool{}, sessions: []*session{{name: "First", host: "one", term: one}, {name: "Second", host: "two", term: two}}}
	tt := ui.NewTester(a.view, 1000, 760)
	if err = tt.Click("Second"); err != nil {
		t.Fatal(err)
	}
	tt.Type("second session")
	expectTerminalInput(t, second, "second session")
	if first.take() != "" {
		t.Fatal("typing went to previous session")
	}
	if err = tt.Click("First"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyUp)
	expectTerminalInput(t, first, "\x1b[A")
}

func TestSidebarToggleKeepsLiveTerminalBufferAndTyping(t *testing.T) {
	recorder := newTerminalRecorder()
	term, err := terminal.New(terminal.Options{Conn: recorder, ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	a := &app{online: true, page: 1, toolsHidden: true, sidebarWidth: 190, groups: map[string]*bool{}, sessions: []*session{{name: "Shell", host: "one", term: term}}}
	tt := ui.NewTester(a.view, 1000, 760)
	term.Feed([]byte("preserved terminal output\r\n$ "))
	tt.Frame()
	for _, label := range []string{"收窄连接侧栏", "展开连接侧栏"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.Type("keep typing")
		expectTerminalInput(t, recorder, "keep typing")
		if len(a.sessions) != 1 || a.sessions[0].term != term || !strings.Contains(term.Text(), "preserved terminal output") {
			t.Fatal("sidebar replaced the terminal or lost its scrollback")
		}
	}
	if err := tt.Click("收窄连接侧栏"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("恢复默认布局"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("工作台"); err != nil {
		t.Fatal(err)
	}
	tt.Type("standard typing")
	expectTerminalInput(t, recorder, "standard typing")
	for _, label := range []string{"隐藏工具面板", "显示工具面板"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.Type("panel typing")
		expectTerminalInput(t, recorder, "panel typing")
		if a.sessions[0].term != term || !strings.Contains(term.Text(), "preserved terminal output") {
			t.Fatal("tool toggle replaced the terminal or lost output")
		}
	}
	for _, label := range []string{"放到右侧", "放到底部"} {
		if err := tt.Click("工具面板布局"); err != nil {
			t.Fatal(err)
		}
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.Type("dock typing")
		expectTerminalInput(t, recorder, "dock typing")
		if a.sessions[0].term != term || !strings.Contains(term.Text(), "preserved terminal output") {
			t.Fatal("docking replaced the terminal or lost output")
		}
	}
}

func TestWorkspaceNavigationAndMenusLeaveOrdinaryTerminalKeysAlone(t *testing.T) {
	recorder := newTerminalRecorder()
	term, err := terminal.New(terminal.Options{Conn: recorder, ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	a := &app{online: true, page: 1, toolsHidden: true, sidebarWidth: 190, groups: map[string]*bool{}, sessions: []*session{{name: "Shell", host: "one", term: term}}}
	tt := ui.NewTester(a.view, 560, 760)
	for _, label := range []string{"授权中心", "设置", "工作台"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []ui.Key{ui.KeyUp, ui.KeyDown, ui.KeyLeft, ui.KeyRight} {
		tt.Key(0, key)
	}
	tt.Key(ui.Ctrl, ui.KeyC)
	tt.Key(ui.Ctrl, ui.KeyR)
	tt.Key(0, ui.KeyTab)
	tt.Type("terminal input")
	expectTerminalInput(t, recorder, "\x1b[A\x1b[B\x1b[D\x1b[C\x03\x12\tterminal input")
	if err := tt.Click("Terminal"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Ctrl, ui.KeyC)
	tt.Key(0, ui.KeyUp)
	expectTerminalInput(t, recorder, "\x03\x1b[A")
}
