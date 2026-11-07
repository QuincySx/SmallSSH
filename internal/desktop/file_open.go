package desktop

import (
	"context"
	"errors"
	"io"
	"path"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"sshclient/internal/approval"
	"sshclient/internal/client"
	"sshclient/internal/desktop/components"
	"sshclient/internal/sshconn"
)

type fileReadState struct {
	endpoint, name string
	loading        bool
	error          string
	cancel         context.CancelFunc
	editor         *editor.State
}

// The file can grow after readLocalText's size check. A full limited read of
// 4 MiB plus one byte is an oversized file, not a complete document.
func validateLocalEditorRead(data []byte, err error) error {
	if err != nil {
		return err
	}
	if len(data) > 4<<20 {
		return errors.New(tr(nil, "文本超过 4 MiB，请使用本地编辑器"))
	}
	return validText(data)
}

func (a *app) cancelEditorRead() {
	if a.editorRead != nil {
		a.editorRead.cancel()
		a.editorRead = nil
	}
}

// The document exists before any disk or SSH work begins. Completion only
// populates that document; it never selects a tab after the user moved away.
func (a *app) readEditor(endpoint, name string) {
	a.cancelEditorRead()
	a.editorSequence++
	seq, gen := a.editorSequence, a.generation
	a.installEditor(endpoint, name, "", "")
	ctx, cancel := context.WithTimeout(a.operationContext(), 30*time.Second)
	load := &fileReadState{endpoint: endpoint, name: name, loading: true, cancel: cancel, editor: a.edit}
	a.editorRead = load
	current := func() bool {
		return a.editorRead == load && a.editorSequence == seq && a.generation == gen && a.edit == load.editor
	}
	finish := func(data []byte, err error) {
		if !current() || !load.loading {
			return
		}
		load.loading = false
		defer cancel()
		if err != nil {
			load.error = err.Error()
			return
		}
		buffer := editor.NewBuffer(string(data))
		buffer.SetLanguage(editor.LanguageFor(name))
		buffer.MarkSaved()
		load.editor.Buffer = buffer
		a.editHash = textHash(data)
		a.editorRead = nil
	}
	go func() {
		<-ctx.Done()
		a.postUI(func() {
			if !current() || !load.loading {
				return
			}
			load.loading = false
			load.error = tr(nil, "读取已取消")
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				load.error = tr(nil, "读取超时，请重试或下载文件")
			}
		})
	}()
	if endpoint == localEndpoint {
		go func() {
			data, err := readLocalText(name)
			err = validateLocalEditorRead(data, err)
			a.postUI(func() { finish(data, err) })
		}()
		return
	}
	a.beginContext(ctx, approval.Operation{Host: endpoint, Kind: "get", Path: name}, func(stream *client.Stream) (func(), error) {
		defer stream.Close()
		data := &limitBuffer{limit: 4 << 20}
		_, _, err := stream.Copy(data, io.Discard)
		if err != nil {
			if data.Len() >= 4<<20 {
				err = errors.New(tr(nil, "文件超过 4 MiB，请下载后打开"))
			}
			return nil, err
		}
		if err := validText(data.Bytes()); err != nil {
			return nil, err
		}
		return func() { finish(data.Bytes(), nil) }, nil
	}, func(err error) { finish(nil, err) })
}

func (a *app) editorReadView(c *ui.Context, load *fileReadState) {
	ui.Column(c).Fill().MinHeight(0).Padding(24).Gap(12).Center().Label(tr(c, "文件加载状态")).Children(func() {
		if load.loading {
			ui.Spinner(c).Size(24, 24)
			ui.Text(c, tr(c, "正在读取文件…")).FontSize(15).Bold()
		} else {
			ui.Text(c, tr(c, "文件暂未打开")).FontSize(15).Bold()
			ui.Text(c, reviewText(load.error)).FillWidth().TextColor(c.Theme().Danger)
		}
		ui.Text(c, a.endpointName(load.endpoint)+" · "+reviewText(load.name)).FillWidth().FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Text(c, tr(c, "内置编辑器支持 4 MiB 以内、不含 NUL 的 UTF-8 文本；图片、PDF、压缩包和其他二进制文件请下载后打开。")).FillWidth().FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(8).Children(func() {
			if load.loading {
				if components.Button(c, tr(c, "取消读取")).Clicked() {
					load.loading, load.error = false, tr(nil, "读取已取消")
					load.cancel()
				}
			} else if components.Button(c, tr(c, "重试读取")).Clicked() {
				a.readEditor(load.endpoint, load.name)
			}
			if load.endpoint != localEndpoint && components.Button(c, tr(c, "下载此文件…")).Clicked() {
				a.promptDownload(load.endpoint, path.Dir(load.name), []sshconn.File{{Name: path.Base(load.name)}})
			}
		})
	})
}

// Keep feedback beside the originating list even when the editor is behind
// another window. The action only shows the existing document, without I/O.
func (a *app) fileOpenFeedback(c *ui.Context, p *filePane, dir string) {
	if a.editPath == "" || a.editHost != p.endpoint || parentDir(p.endpoint == localEndpoint, a.editPath) != dir {
		return
	}
	name := reviewText(baseName(p.endpoint == localEndpoint, a.editPath))
	label := tr(c, "已打开 · ") + name
	load := a.editorRead
	if load != nil {
		if load.loading {
			ui.Spinner(c).Size(14, 14).Shrink(0).Label(tr(c, "正在打开文件"))
			label = tr(c, "正在打开 · ") + name
		} else {
			label = tr(c, "打开失败 · ") + name
		}
	}
	message := ui.Text(c, label).SingleLine().MinWidth(0).Shrink(1).FontSize(11).TextColor(c.Theme().TextMuted).Tooltip(label)
	if load != nil && !load.loading {
		message.TextColor(c.Theme().Danger).Tooltip(reviewText(load.error))
	}
	if components.SVGButton(c, components.DetachPanelIcon, tr(c, "显示文件窗口")).Size(24, 24).Shrink(0).Clicked() {
		a.openEditorWindow()
	}
}
