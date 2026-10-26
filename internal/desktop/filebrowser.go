package desktop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"sshclient/internal/approval"
	"sshclient/internal/client"
	"sshclient/internal/sshconn"
	"sshclient/internal/transfer"
)

const (
	localEndpoint          = "@local"
	followTerminalEndpoint = "@terminal"
)

type directoryBrowser struct {
	dir, address string
	cache        map[string][]sshconn.File
	open         map[string]*bool
	chosen       ui.Selection[string]
	table        ui.ListState
	selected     int
	anchor       int
	seq          uint64
	tree         *directoryTreeIndex
	filesByName  map[string]int
	loading      map[string]*directoryLoad
	loadError    map[string]string
}
type directoryLoad struct {
	ctx      context.Context
	cancel   context.CancelFunc
	navigate bool
	seq      uint64
}
type filePane struct {
	id        uint64
	endpoint  string
	states    map[string]*directoryBrowser
	treeWidth float32
	follow    bool
}

func newFilePane(endpoint string, follow bool) *filePane {
	return &filePane{endpoint: endpoint, states: map[string]*directoryBrowser{}, treeWidth: 180, follow: follow}
}
func (p *filePane) browser() *directoryBrowser {
	if p.states == nil {
		p.states = map[string]*directoryBrowser{}
	}
	b := p.states[p.endpoint]
	if b == nil {
		dir := "/"
		if p.endpoint == localEndpoint {
			dir, _ = os.UserHomeDir()
			if dir == "" {
				dir = string(filepath.Separator)
			}
		}
		b = &directoryBrowser{dir: dir, address: dir, cache: map[string][]sshconn.File{}, open: map[string]*bool{}, selected: -1, anchor: -1}
		p.states[p.endpoint] = b
		b.reveal(p.endpoint == localEndpoint, dir)
	}
	return b
}
func (b *directoryBrowser) reveal(local bool, dir string) {
	for {
		if b.open[dir] == nil {
			v := true
			b.open[dir] = &v
		} else {
			*b.open[dir] = true
		}
		parent := parentDir(local, dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	b.treeLocation(local)
}
func parentDir(local bool, p string) string {
	if local {
		return filepath.Dir(p)
	}
	return path.Dir(p)
}
func joinPath(local bool, p, n string) string {
	if local {
		return filepath.Join(p, n)
	}
	return path.Join(p, n)
}
func baseName(local bool, p string) string {
	if local {
		return filepath.Base(p)
	}
	return path.Base(p)
}
func (a *app) ensurePanes() {
	if a.directorySlots == nil {
		a.directorySlots = make(chan struct{}, 2)
	}
	if a.localFilePane == nil {
		a.localFilePane = newFilePane(localEndpoint, false)
	}
	if a.primaryFilePane == nil {
		a.primaryFilePane = newFilePane(a.activeFileEndpoint(), true)
	}
	if a.edit == nil {
		a.edit = &editor.State{Buffer: editor.NewBuffer("")}
	}
}

func (a *app) activeFileEndpoint() string {
	if a.active >= 0 && a.active < len(a.sessions) && a.sessions[a.active].host != "" {
		return a.sessions[a.active].host
	}
	return localEndpoint
}

func (a *app) setFileEndpoint(p *filePane, endpoint string, follow bool) {
	if p.endpoint != endpoint {
		cancelPaneLoads(p)
		p.endpoint = endpoint
	}
	p.follow = follow
}

func (a *app) syncFollowingFilePanes() {
	for _, p := range a.allFilePanes() {
		if p != nil && p.follow {
			a.setFileEndpoint(p, a.activeFileEndpoint(), true)
		}
	}
}

func (a *app) chooseFileEndpoint(p *filePane, choice string) {
	a.activeFilePane = p
	if choice == followTerminalEndpoint {
		a.setFileEndpoint(p, a.activeFileEndpoint(), true)
	} else {
		a.setFileEndpoint(p, choice, false)
	}
}
func (a *app) initializeWorkspace() {
	a.ensurePanes()
	if a.server != nil {
		a.snippets = a.server.Vault.Snippets()
	}
	if a.primaryFilePane.endpoint == "" {
		a.primaryFilePane.endpoint = localEndpoint
	}
	if a.primaryFilePane.endpoint == localEndpoint {
		a.loadDirectory(a.primaryFilePane, a.primaryFilePane.browser().dir, true)
	}
}
func sortedFiles(fs []sshconn.File) []sshconn.File {
	valid := fs[:0]
	for _, f := range fs {
		if f.Name != "" && f.Name != "." && f.Name != ".." && !strings.ContainsAny(f.Name, "/\\\x00") {
			valid = append(valid, f)
		}
	}
	fs = valid
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Directory != fs[j].Directory {
			return fs[i].Directory
		}
		return strings.ToLower(fs[i].Name) < strings.ToLower(fs[j].Name)
	})
	return fs
}
func localDirectory(dir string) ([]sshconn.File, error) {
	return localDirectoryContext(context.Background(), dir)
}
func localDirectoryContext(ctx context.Context, dir string) ([]sshconn.File, error) {
	file, e := os.Open(dir)
	if e != nil {
		return nil, e
	}
	defer file.Close()
	out := make([]sshconn.File, 0, 256)
	for {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		items, readErr := file.ReadDir(256)
		for _, item := range items {
			info, e := item.Info()
			if e != nil {
				continue
			}
			out = append(out, sshconn.File{Name: item.Name(), Directory: info.IsDir(), Size: info.Size(), Mode: info.Mode().String(), Modified: info.ModTime()})
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	return sortedFiles(out), nil
}
func canonicalDirectory(local bool, p string) (string, error) {
	if local {
		return filepath.Abs(p)
	}
	if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\x00\r\n") {
		return "", errors.New(tr(nil, "请输入远程绝对目录路径"))
	}
	return path.Clean(p), nil
}
func (a *app) applyDirectory(b *directoryBrowser, local bool, dir string, fs []sshconn.File, seq uint64, navigate bool) {
	b.cache[dir] = fs
	b.indexDirectory(local, dir)
	if navigate && seq == b.seq {
		b.dir = dir
		b.address = dir
		b.chosen.Clear()
		b.selected = -1
		b.anchor = -1
		b.reveal(local, dir)
	}
}
func (a *app) loadDirectory(p *filePane, dir string, navigate bool) {
	endpoint, b := p.endpoint, p.browser()
	local := endpoint == localEndpoint
	dir, e := canonicalDirectory(local, dir)
	if e != nil {
		a.status = e.Error()
		return
	}
	if a.win == nil && a.dispatch == nil {
		return
	}
	if a.directorySlots == nil {
		a.directorySlots = make(chan struct{}, 2)
	}
	if navigate {
		b.seq++
	}
	if b.loading == nil {
		b.loading = map[string]*directoryLoad{}
		b.loadError = map[string]string{}
	}
	if pending := b.loading[dir]; pending != nil {
		if navigate {
			pending.navigate = true
			pending.seq = b.seq
		}
		return
	}
	if navigate {
		for other, pending := range b.loading {
			if pending.navigate {
				pending.cancel()
				delete(b.loading, other)
			}
		}
	}
	parent := a.operationContext()
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	request := &directoryLoad{ctx: ctx, cancel: cancel, navigate: navigate, seq: b.seq}
	b.loading[dir] = request
	delete(b.loadError, dir)
	gen, slots := a.generation, a.directorySlots
	finish := func(fs []sshconn.File, e error) {
		if b.loading[dir] != request {
			return
		}
		delete(b.loading, dir)
		defer cancel()
		if gen != a.generation || ctx.Err() != nil {
			return
		}
		if e != nil {
			b.loadError[dir] = e.Error()
			a.status = e.Error()
			return
		}
		a.applyDirectory(b, local, dir, fs, request.seq, request.navigate)

	}
	if local {
		go func() {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				return
			}
			fs, e := localDirectoryContext(ctx, dir)
			a.postUI(func() { finish(fs, e) })
		}()
		return
	}
	a.beginContext(ctx, approval.Operation{Host: endpoint, Kind: "list", Path: dir}, func(s *client.Stream) (func(), error) {
		defer s.Close()
		_, fs, e := s.Copy(io.Discard, io.Discard)
		if e == nil {
			fs = sortedFiles(fs)
		}
		return func() { finish(fs, nil) }, e
	}, func(e error) { finish(nil, e) })
}

func (a *app) navigateDirectory(p *filePane, dir string) {
	b := p.browser()
	dir, e := canonicalDirectory(p.endpoint == localEndpoint, dir)
	if e != nil {
		a.status = e.Error()
		return
	}
	if _, ok := b.cache[dir]; ok {
		for name, pending := range b.loading {
			if pending.navigate {
				pending.cancel()
				delete(b.loading, name)
			}
		}
		b.seq++
		b.dir = dir
		b.address = dir
		b.chosen.Clear()
		b.selected = -1
		b.anchor = -1
		b.reveal(p.endpoint == localEndpoint, dir)
		return
	}
	b.address = dir
	a.loadDirectory(p, dir, true)
}
func (a *app) endpointName(endpoint string) string {
	if endpoint == localEndpoint {
		return tr(nil, "本机")
	}
	return a.hostName(endpoint)
}
func chosenFiles(b *directoryBrowser) []sshconn.File {
	fs := b.cache[b.dir]
	if len(b.filesByName) != len(fs) {
		b.indexFiles()
	}
	out := make([]sshconn.File, 0, b.chosen.Len())
	for name := range b.chosen.All() {
		if i, ok := b.filesByName[name]; ok && i < len(fs) {
			out = append(out, fs[i])
		}
	}
	sort.Slice(out, func(i, j int) bool { return b.filesByName[out[i].Name] < b.filesByName[out[j].Name] })
	return out
}
func treeChildren(b *directoryBrowser, local bool, dir string) []string {
	return b.directoryChildren(dir)
}
func (a *app) openPaneFile(p *filePane) {
	b := p.browser()
	a.openEntries(p, b.dir, chosenFiles(b))
}
func (a *app) installEditor(endpoint, name, text, hash string) {
	buffer := editor.NewBuffer(text)
	buffer.SetLanguage(editor.LanguageFor(name))
	buffer.MarkSaved()
	a.edit = &editor.State{Buffer: buffer}
	a.editPath = name
	a.editHash = hash
	a.editHost = endpoint
	a.editLocal = endpoint == localEndpoint
	a.editShown = true
	a.ensureTerminalGroups()
	if a.editorTerminalGroup != nil {
		a.editorTerminalGroup.editorSelected = false
	}
	a.editorTerminalGroup = a.activeTerminalGroup
	a.editorTerminalGroup.editorSelected = true
	a.contentFocus = true
	a.fileExpanded = false
	a.page = 1
	a.saveAs = name + ".edited"
	a.status = ""
}
func validText(data []byte) error {
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return errors.New(tr(nil, "仅编辑不含 NUL 的 UTF-8 文本，请下载该文件"))
	}
	return nil
}
func textHash(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func (a *app) readEditor(endpoint, name string) {
	gen := a.generation
	a.editorSequence++
	seq := a.editorSequence
	buffer, version := a.edit.Buffer, a.edit.Buffer.Version()
	install := func(text, hash string) {
		if a.editorSequence != seq {
			return
		}
		open := func(string) { a.installEditor(endpoint, name, text, hash) }
		if a.edit.Buffer != buffer || buffer.Version() != version {
			a.ask(tr(nil, "切换到已读取的文件？"), a.endpointName(endpoint)+" · "+name, tr(nil, "读取期间编辑器发生了变化。继续会替换当前编辑内容。"), "", "", tr(nil, "打开文件"), open)
		} else {
			open("")
		}
	}
	if endpoint == localEndpoint {
		go func() {
			data, e := readLocalText(name)
			if len(data) > 4<<20 {
				e = errors.New(tr(nil, "文本超过 4 MiB，请使用本地编辑器"))
			}
			if e == nil {
				e = validText(data)
			}
			a.postUI(func() {
				if gen != a.generation {
					return
				}
				if e != nil {
					a.status = e.Error()
				} else {
					install(string(data), textHash(data))
				}
			})
		}()
		return
	}
	a.begin(approval.Operation{Host: endpoint, Kind: "get", Path: name}, func(s *client.Stream) (func(), error) {
		defer s.Close()
		b := &limitBuffer{limit: 4 << 20}
		_, _, e := s.Copy(b, io.Discard)
		if e != nil {
			return nil, e
		}
		if e = validText(b.Bytes()); e != nil {
			return nil, e
		}
		text, hash := b.String(), textHash(b.Bytes())
		return func() { install(text, hash) }, nil
	})
}
func (a *app) saveEditor() {
	if a.edit == nil || a.editPath == "" || !a.edit.Buffer.Modified() || a.editorSaving == a.edit || a.busy {
		return
	}
	if a.editLocal {
		a.saveLocalEditor(a.editPath, false)
		return
	}
	a.replaceText()
}
func (a *app) saveEditorAs() {
	endpoint, original := a.editHost, a.editPath
	a.ask(tr(nil, "另存为"), a.endpointName(endpoint), tr(nil, "保存到新路径；已有文件不会被覆盖。"), tr(nil, "完整目标路径"), original+".edited", tr(nil, "保存"), func(value string) {
		if endpoint != a.editHost || original != a.editPath {
			a.status = tr(nil, "编辑目标已变化，请重新另存为")
			return
		}
		if a.editLocal {
			a.saveLocalEditor(value, true)
		} else {
			a.saveAs = value
			a.upload(true)
		}
	})
}
func saveLocalText(name string, content []byte, original string, create bool) error {
	if !filepath.IsAbs(name) {
		return errors.New(tr(nil, "请填写本机绝对路径"))
	}
	var mode os.FileMode = 0600
	if !create {
		info, e := os.Lstat(name)
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return errors.New(tr(nil, "请选择普通文件；不覆盖符号链接"))
		}
		mode = info.Mode().Perm()
		old, e := readLocalText(name)
		if e != nil {
			return e
		}
		if textHash(old) != original {
			return errors.New(tr(nil, "文件已被外部修改，请重新读取或另存为"))
		}
	}
	f, e := os.CreateTemp(filepath.Dir(name), ".ssh-edit-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(content)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if create {
		return os.Link(tmp, name)
	}
	old, e := readLocalText(name)
	if e != nil {
		return e
	}
	if textHash(old) != original {
		return errors.New(tr(nil, "文件在保存前发生变化，请另存为"))
	}
	return os.Rename(tmp, name)
}
func (a *app) saveLocalEditor(name string, create bool) {
	content := []byte(a.edit.Buffer.String())
	doc, old, gen, buffer := a.editPath, a.editHash, a.generation, a.edit.Buffer
	a.work(func() (func(), error) {
		e := saveLocalText(name, content, old, create)
		return func() {
			if a.generation != gen || a.editPath != doc || a.edit.Buffer != buffer {
				return
			}
			a.editPath = name
			a.editHash = textHash(content)
			if buffer.String() == string(content) {
				buffer.MarkSaved()
			}
			a.status = tr(nil, "已保存本机文件 · ") + name
		}, e
	})
}
func (a *app) nativeTerminal(c *ui.Context, s *session) {
	view := terminal.View(c, s.term).FillWidth().Grow(1).MinHeight(0)
	active := a.terminalLayout == nil || s == a.currentTerminal()
	if a.renderingTerminalGroup != nil {
		active = !a.editShown && s == a.currentTerminal() && a.renderingTerminalGroup == a.activeTerminalGroup
	}
	if active {
		view.AutoFocus()
	}
	// A layout toggle can change the parent layout during this frame. Keep
	// the focus request for the replacement tree rather than the outgoing one.
	if a.contentFocus && active && a.contentLayoutReady() {
		view.Focus()
		a.contentFocus = false
	}
	if view.FocusWithin() && !active && !a.applyingTerminalFocus && !a.contentFocus {
		a.activateTerminal(s, false)
	}
}

func (a *app) fileOperation(p *filePane, kind string) {
	a.fileOperationAt(p, p.browser().dir, chosenFiles(p.browser()), kind)
}
func (a *app) fileOperationAt(p *filePane, dir string, fs []sshconn.File, kind string) {
	b, endpoint := p.browser(), p.endpoint
	local := endpoint == localEndpoint
	target := dir
	if len(fs) == 1 && kind != "new" && kind != "mkdir" {
		target = joinPath(local, dir, fs[0].Name)
	}
	if kind != "new" && kind != "mkdir" && len(fs) != 1 {
		return
	}
	if kind == "chmod" {
		a.openFileProperties(p, target)
		return
	}
	if kind != "new" && kind != "mkdir" && parentDir(local, target) == target {
		a.status = tr(nil, "不能修改或删除根目录")
		return
	}
	title, message, field, value, accept := "", "", "", "", tr(nil, "确认")
	switch kind {
	case "new":
		title = tr(nil, "新建文本文件")
		field = tr(nil, "文件名")
	case "mkdir":
		title = tr(nil, "新建文件夹")
		field = tr(nil, "文件夹名称")
	case "rename":
		title = tr(nil, "重命名 / 移动")
		field = tr(nil, "完整目标路径")
		value = target
	case "remove":
		title = tr(nil, "删除文件或空文件夹？")
		message = tr(nil, "删除后无法从客户端恢复；目录必须为空。")
		accept = tr(nil, "删除")
	case "chmod":
		title = tr(nil, "权限 / 属性")
		field = tr(nil, "权限（八进制）")
		value = permissionValue(fs[0])
		message = fmt.Sprintf("%s · %s · %s", fs[0].Mode, size(uint64(max(fs[0].Size, 0))), fs[0].Modified.Local().Format("2006-01-02 15:04"))
	}
	a.ask(title, a.endpointName(endpoint)+" · "+target, message, field, value, accept, func(value string) {
		name, dest, mode := target, "", uint32(0)
		if kind == "new" || kind == "mkdir" {
			if value == "" || value == "." || value == ".." || strings.ContainsAny(value, "/\\\x00\r\n") {
				a.status = tr(nil, "请输入单个有效名称")
				return
			}
			name = joinPath(local, dir, value)
		}
		if kind == "rename" {
			var e error
			dest, e = canonicalDirectory(local, value)
			if e != nil {
				a.status = e.Error()
				return
			}
		}
		if kind == "chmod" {
			v, e := strconv.ParseUint(value, 8, 32)
			if e != nil || v > 0777 {
				a.status = tr(nil, "请输入 000 到 777 的权限")
				return
			}
			mode = uint32(v)
		}
		complete := func() {
			isDir := len(fs) == 1 && fs[0].Directory
			a.fileMutationCompleted(endpoint, b, dir, kind, name, dest, mode, isDir)
			b.chosen.Clear()
			b.selected = -1
			a.status = ""
		}
		if local {
			gen := a.generation
			a.work(func() (func(), error) {
				e := mutateLocal(kind, name, dest, mode)
				return func() {
					if gen == a.generation {
						complete()
						if p.endpoint == endpoint && b.dir == dir {
							a.loadDirectory(p, dir, true)
						}
					}
				}, e
			})
			return
		}
		op := approval.Operation{Host: endpoint, Kind: kind, Path: name, Destination: dest, Mode: mode}
		if kind == "new" {
			op.Kind = "put"
			op.ContentHash = textHash(nil)
		}
		a.begin(op, func(s *client.Stream) (func(), error) {
			defer s.Close()
			if kind == "new" {
				if e := s.End(); e != nil {
					return nil, e
				}
			}
			_, _, e := s.Copy(io.Discard, io.Discard)
			return complete, e
		})
	})
	a.prompt.danger = kind == "remove"
	a.prompt.validate = func(value string) error { return validateFileInput(local, kind, value) }
}
func updateMutationCache(b *directoryBrowser, local bool, dir, kind, name, dest string, mode uint32) {
	fs := append([]sshconn.File(nil), b.cache[dir]...)
	base := baseName(local, name)
	if kind == "new" || kind == "mkdir" {
		fs = append(fs, sshconn.File{Name: base, Directory: kind == "mkdir", Modified: time.Now()})
	}
	for i := 0; i < len(fs); i++ {
		if fs[i].Name != base {
			continue
		}
		switch kind {
		case "remove":
			fs = append(fs[:i], fs[i+1:]...)
			i--
		case "rename":
			f := fs[i]
			f.Name = baseName(local, dest)
			fs = append(fs[:i], fs[i+1:]...)
			i--
			parent := parentDir(local, dest)
			if parent == dir {
				fs = append(fs, f)
				kind = "renamed"
			} else if _, loaded := b.cache[parent]; loaded {
				b.cache[parent] = sortedFiles(append(b.cache[parent], f))
				b.indexDirectory(local, parent)
			}
		case "chmod":
			prefix := "-"
			if fs[i].Directory {
				prefix = "d"
			}
			bits := ""
			for shift := 6; shift >= 0; shift -= 3 {
				n := (mode >> shift) & 7
				for j, ch := range "rwx" {
					if n&(4>>j) != 0 {
						bits += string(ch)
					} else {
						bits += "-"
					}
				}
			}
			fs[i].Mode = prefix + bits
		}
	}
	b.cache[dir] = sortedFiles(fs)
	b.indexDirectory(local, dir)
}

func mutateLocal(kind, name, dest string, mode uint32) error {
	switch kind {
	case "new":
		f, e := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		return f.Close()
	case "mkdir":
		return os.Mkdir(name, 0700)
	case "remove":
		return os.Remove(name)
	case "rename":
		if _, e := os.Lstat(dest); e == nil {
			return errors.New(tr(nil, "目标已存在，请使用新名称"))
		} else if !os.IsNotExist(e) {
			return e
		}
		return os.Rename(name, dest)
	case "chmod":
		st, e := os.Lstat(name)
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return errors.New(tr(nil, "不修改符号链接权限"))
		}
		return os.Chmod(name, os.FileMode(mode))
	default:
		return errors.New(tr(nil, "未知文件操作"))
	}
}
func (a *app) canTransfer(from, to *filePane) bool {
	if from == nil || to == nil {
		return false
	}
	if (from.endpoint == localEndpoint) == (to.endpoint == localEndpoint) {
		return false
	}
	if from.endpoint == "" || to.endpoint == "" {
		return false
	}
	return len(chosenFiles(from.browser())) > 0
}
func (a *app) transferBetween(from, to *filePane) {
	if !a.canTransfer(from, to) {
		return
	}
	source, destination := from.browser(), to.browser()
	a.transferEntries(from.endpoint, source.dir, chosenFiles(source), to.endpoint, destination.dir)
}
func (a *app) transferEntries(fromEndpoint, sourceDir string, entries []sshconn.File, toEndpoint, destinationDir string) {
	if fromEndpoint == "" || toEndpoint == "" || (fromEndpoint == localEndpoint) == (toEndpoint == localEndpoint) || len(entries) == 0 {
		return
	}
	upload := fromEndpoint == localEndpoint
	for _, f := range entries {
		if f.Directory {
			a.status = tr(nil, "目录可浏览；传输时请选择其中的文件（支持多选）")
			return
		}
		if strings.ContainsAny(f.Name, "/\\\x00") {
			a.status = tr(nil, "文件名称无效")
			return
		}
	}
	names := []string{}
	for _, f := range entries {
		names = append(names, reviewText(f.Name))
	}
	a.ask(tr(nil, "传输文件"), a.endpointName(fromEndpoint)+" · "+sourceDir+"\n→ "+a.endpointName(toEndpoint)+" · "+destinationDir, strings.Join(names, "\n")+tr(nil, "\n\n目标已存在时跳过，不覆盖。"), "", "", tr(nil, "加入队列"), func(string) {
		host, direction := toEndpoint, "upload"
		if !upload {
			host = fromEndpoint
			direction = "download"
		}
		gen := a.generation
		a.work(func() (func(), error) {
			ids := []string{}
			for _, f := range entries {
				local, remote := filepath.Join(sourceDir, f.Name), path.Join(destinationDir, f.Name)
				if !upload {
					local = filepath.Join(destinationDir, f.Name)
					remote = path.Join(sourceDir, f.Name)
				}
				j, e := a.transfers.Add(host, direction, local, remote)
				if e != nil {
					return func() {
						a.status = fmt.Sprintf(tr(nil, "已建立 %d 项，后续失败: %s"), len(ids), e)
						a.jobs = a.transfers.List()
					}, nil
				}
				ids = append(ids, j.ID)
			}
			return func() {
				if a.generation != gen {
					return
				}
				a.jobs = a.transfers.List()
				for _, id := range ids {
					a.runTransfer(id)
				}
				a.status = ""
			}, nil
		})
	})
}

func (a *app) transferArrived(job transfer.Job) {
	endpoint, local, name := job.Host, false, job.Remote
	if job.Direction == "download" {
		endpoint, local, name = localEndpoint, true, job.Local
	}
	dir, base := parentDir(local, name), baseName(local, name)
	for _, pane := range a.allFilePanes() {
		if pane == nil {
			continue
		}
		b := pane.states[endpoint]
		if b == nil {
			continue
		}
		files, loaded := b.cache[dir]
		if !loaded {
			continue
		}
		files = append([]sshconn.File(nil), files...)
		found := false
		for i := range files {
			if files[i].Name == base {
				files[i].Size = job.Size
				found = true
				break
			}
		}
		if !found {
			files = append(files, sshconn.File{Name: base, Size: job.Size, Modified: time.Now()})
		}
		b.cache[dir] = sortedFiles(files)
		b.indexDirectory(local, dir)
	}
}

func readLocalText(name string) ([]byte, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New(tr(nil, "只能编辑普通文件；符号链接请打开目标文件"))
	}
	if info.Size() > 4<<20 {
		return nil, errors.New(tr(nil, "文本超过 4 MiB，请使用本地编辑器"))
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !actual.Mode().IsRegular() || !os.SameFile(info, actual) {
		return nil, errors.New(tr(nil, "文件类型已变化，请重新打开"))
	}
	return io.ReadAll(io.LimitReader(file, (4<<20)+1))
}
func permissionValue(f sshconn.File) string {
	if len(f.Mode) < 10 {
		if f.Directory {
			return "755"
		}
		return "644"
	}
	mode := uint32(0)
	for i, ch := range f.Mode[1:10] {
		if ch == 'r' || ch == 'w' || ch == 'x' || ch == 's' || ch == 't' {
			mode |= 1 << uint(8-i)
		}
	}
	return fmt.Sprintf("%03o", mode)
}
func validateFileInput(local bool, kind, value string) error {
	switch kind {
	case "new", "mkdir":
		if strings.TrimSpace(value) == "" || value == "." || value == ".." || strings.ContainsAny(value, "/\\\x00\r\n") {
			return errors.New(tr(nil, "请输入有效名称，不要包含路径分隔符"))
		}
	case "rename":
		if strings.TrimSpace(value) == "" {
			return errors.New(tr(nil, "请输入完整目标路径"))
		}
		_, err := canonicalDirectory(local, value)
		return err
	case "chmod":
		n, err := strconv.ParseUint(value, 8, 32)
		if err != nil || n > 0777 {
			return errors.New(tr(nil, "请输入 000 到 777 的八进制权限"))
		}
	}
	return nil
}
