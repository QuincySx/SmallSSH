package desktop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/desktop/components"
	"sshclient/internal/servernotes"
	"sshclient/internal/vault"
)

type serverNotesState struct {
	target                                         vault.Summary
	original, draft                                vault.ServerNote
	error                                          string
	scanning, saving, expanded, discard, detached  bool
	ctx                                            context.Context
	cancel, scanCancel                             context.CancelFunc
	generation                                     int
	scanSequence                                   uint64
	summarySource, detailsSource, summary, details string
}

func (s *serverNotesState) snapshotText() (string, string) {
	if s.summarySource != s.draft.Discovered || s.detailsSource != s.draft.Details {
		s.summarySource, s.detailsSource = s.draft.Discovered, s.draft.Details
		s.summary, s.details = servernotes.SavedPresentation(s.draft.Discovered, s.draft.Details)
		s.summary, s.details = notesDisplayText(s.summary), notesDisplayText(s.details)
	}
	return s.summary, s.details
}

// Report line breaks are trusted structure. Preserve them while still making
// other controls and bidi characters visible, as in the authorization review.
func notesDisplayText(value string) string {
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		lines[i] = reviewText(line)
	}
	return strings.Join(lines, "\n")
}

const notesScanInterval = time.Minute

func (a *app) notesScanWait(target vault.Summary) time.Duration {
	return time.Until(a.notesScannedAt[vault.NoteTarget(target)].Add(notesScanInterval))
}

func (a *app) openServerNotes(id string) {
	if a.serverNotes != nil {
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		a.status = err.Error()
		return
	}
	var target vault.Summary
	for _, p := range a.connections {
		if p.ID == id {
			target = p
			break
		}
	}
	if target.ID == "" {
		a.status = tr(nil, "请先选择服务器")
		return
	}
	note, err := owner.ServerNote(id)
	if err != nil {
		a.status = err.Error()
		return
	}
	ctx, cancel := context.WithCancel(a.operationContext())
	a.serverNotes = &serverNotesState{target: target, original: note, draft: note, ctx: ctx, cancel: cancel, generation: a.generation, detached: a.toolsWindowFocused()}
}

func (a *app) closeServerNotes() {
	if s := a.serverNotes; s != nil {
		if s.cancel != nil {
			s.cancel()
		}
		s.draft, s.original = vault.ServerNote{}, vault.ServerNote{}
		s.summarySource, s.detailsSource, s.summary, s.details = "", "", "", ""
		a.serverNotes = nil
	}
}

func (a *app) notesCurrent(s *serverNotesState) bool {
	return a.online && a.serverNotes == s && s.generation == a.generation && s.ctx.Err() == nil
}

func (a *app) cancelNotesScan(s *serverNotesState) {
	if s.scanCancel != nil {
		s.scanCancel()
	}
	s.scanSequence++
	s.scanCancel = nil
	s.scanning = false
	s.error = ""
}

func (a *app) requestCloseNotes(s *serverNotesState) {
	if s.saving {
		return
	}
	if s.scanCancel != nil {
		a.cancelNotesScan(s)
	}
	if s.discard {
		s.discard = false
		return
	}
	if s.draft != s.original {
		s.discard = true
		return
	}
	a.closeServerNotes()
}

func (a *app) serverNotesDialog(c *ui.Context) {
	s := a.serverNotes
	if s == nil || !a.online || s.detached != a.renderingToolsWindow {
		return
	}
	open := true
	title := tr(c, "服务器备注")
	if s.discard {
		title = tr(c, "放弃未保存的备注？")
	}
	height := float32(0)
	_, availableHeight := c.Size()
	if !s.discard && (s.expanded || availableHeight < 600) {
		height = 480
	}
	components.Dialog(c, &open, components.DialogOptions{Title: title, Target: s.target.Name, Width: 560, Height: height,
		Close: func() { a.requestCloseNotes(s) }, CloseDisabled: s.saving}, func() {
		if s.discard {
			ui.Text(c, tr(c, "手写备注和新生成的概览尚未保存。")).FillWidth()
			return
		}
		ui.TextArea(c, &s.draft.Manual).FillWidth().Height(88).Placeholder(tr(c, "用途、维护事项，或提醒以后的自己…")).Label(tr(c, "服务器备注输入")).Disabled(s.saving)
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spacer(c).Grow(1)
			label := tr(c, "检测服务和域名")
			if s.draft.Discovered != "" {
				label = tr(c, "重新检测")
			}
			if components.Button(c, label, components.ButtonOptions{Variant: components.Quiet, Busy: s.scanning, Disabled: s.saving || a.notesScanWait(s.target) > 0}).Tooltip(tr(c, "按需检测；同一服务器每分钟最多一次")).Clicked() {
				a.scanServerNotes(s)
			}
			if s.scanning && components.Button(c, tr(c, "取消检测")).Clicked() && s.scanCancel != nil {
				a.cancelNotesScan(s)
			}
		})
		if s.draft.Discovered != "" {
			if s.draft.Target != vault.NoteTarget(s.target) {
				ui.Text(c, tr(c, "连接资料已变化；以下是旧目标的检测结果。")).FillWidth().FontSize(12).TextColor(c.Theme().Warning)
			}
			summary, details := s.snapshotText()
			ui.Text(c, summary).FillWidth().FontSize(13).Selectable().Label(tr(c, "服务器服务和域名"))
			label := tr(c, "检测详情 · ") + s.draft.CheckedAt.Local().Format("01-02 15:04")
			if details != "" {
				ui.Collapsible(c, label, &s.expanded, func() {
					ui.Text(c, details).FillWidth().FontSize(12).Selectable().Label(tr(c, "服务器检测详情"))
				})
			}
		}
		if s.error != "" {
			ui.Text(c, reviewText(s.error)).FillWidth().FontSize(12).TextColor(c.Theme().Danger)
		}
	}, func() {
		if s.discard {
			if components.Button(c, tr(c, "继续编辑")).Clicked() {
				s.discard = false
			}
			if components.Button(c, tr(c, "放弃更改"), components.ButtonOptions{Variant: components.Destructive}).Clicked() {
				a.closeServerNotes()
			}
			return
		}
		if components.Button(c, tr(c, "关闭")).Disabled(s.saving).Clicked() {
			a.requestCloseNotes(s)
		}
		if components.Button(c, tr(c, "保存备注"), components.ButtonOptions{Variant: components.Primary, Busy: s.saving, Disabled: s.scanning || s.draft == s.original}).Clicked() {
			a.saveServerNotes(s)
		}
	})
	if !open {
		a.requestCloseNotes(s)
	}
}

func (a *app) saveServerNotes(s *serverNotesState) {
	if !a.notesCurrent(s) || s.saving || s.scanning {
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		s.error = err.Error()
		return
	}
	note, dir := s.draft, a.dir
	s.saving, s.error = true, ""
	go func() {
		err := owner.SaveServerNote(broker.VaultPath(dir), s.target.ID, note)
		a.postUI(func() {
			if !a.notesCurrent(s) {
				return
			}
			s.saving = false
			if err != nil {
				s.error = err.Error()
				return
			}
			a.closeServerNotes()
		})
	}()
}

func (a *app) scanServerNotes(s *serverNotesState) {
	if !a.notesCurrent(s) || s.scanning || s.saving || a.notesScanWait(s.target) > 0 {
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		s.error = err.Error()
		return
	}
	if a.notesScannedAt == nil {
		a.notesScannedAt = make(map[string]time.Time)
	}
	// Keep the cooldown outside the dialog so closing, cancelling or reopening
	// it cannot create another SSH request. This is not an automatic timer.
	for key, at := range a.notesScannedAt {
		if time.Since(at) >= notesScanInterval {
			delete(a.notesScannedAt, key)
		}
	}
	a.notesScannedAt[vault.NoteTarget(s.target)] = time.Now()
	ctx, cancel := context.WithTimeout(s.ctx, 25*time.Second)
	s.scanCancel = cancel
	s.scanning, s.error = true, ""
	s.scanSequence++
	sequence, lookup := s.scanSequence, a.notesDNS
	target, cli := s.target, a.client
	op := approval.Operation{Host: target.ID, Kind: "exec", Command: servernotes.Command,
		Endpoint: target.User + "@" + net.JoinHostPort(target.Host, strconv.Itoa(target.Port)), HostFingerprint: target.Fingerprint}
	go func() {
		defer cancel()
		var report servernotes.Report
		stream, err := cli.StartWithOwner(ctx, op, owner)
		if err == nil {
			out := &limitBuffer{limit: 2 << 20}
			code, _, readErr := stream.Copy(out, io.Discard)
			stream.Close()
			err = readErr
			if err == nil && code != 0 {
				err = fmt.Errorf(tr(nil, "只读检测退出，状态码 %d"), code)
			}
			if err == nil {
				report = servernotes.Parse(out.String(), target.Host)
				if !report.Complete {
					err = errors.New(tr(nil, "检测结果不完整，已保留之前的概览"))
				} else {
					dnsCtx, stop := context.WithTimeout(ctx, 8*time.Second)
					report.VerifyDNS(dnsCtx, target.Host, lookup)
					stop()
				}
			}
		}
		if ctx.Err() != nil {
			err = errors.New(tr(nil, "检测已取消或超时，已保留之前的概览"))
		}
		text, details := report.Text(), report.Details()
		checked := time.Now().UTC()
		a.postUI(func() {
			if !a.notesCurrent(s) || s.scanSequence != sequence {
				return
			}
			s.scanCancel = nil
			s.scanning = false
			if err != nil {
				s.error = err.Error()
				return
			}
			s.draft.Discovered, s.draft.CheckedAt, s.draft.Target = text, checked, vault.NoteTarget(target)
			s.draft.Details = details
			s.expanded = false
		})
	}()
}
