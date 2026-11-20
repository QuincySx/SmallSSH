// Package desktop uses MyGo's native UI and Ghostty terminal. It never dials SSH
// directly: human and AI operations share the same broker and grants.
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
	"runtime"
	"sort"
	"sshclient/internal/desktop/components"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"sshclient/internal/approval"
	"sshclient/internal/client"
	"sshclient/internal/connectcfg"
	"sshclient/internal/desktopcontrol"
	"sshclient/internal/localipc"
	"sshclient/internal/monitor"
	"sshclient/internal/protocol"
	"sshclient/internal/servernotes"
	"sshclient/internal/snippet"
	"sshclient/internal/sshdata"
	"sshclient/internal/systemauth"
	"sshclient/internal/transfer"
	"sshclient/internal/vault"
)

type session struct {
	connectionState    string
	connectionError    string
	connectionCancel   context.CancelFunc
	connectionMismatch bool
	group              *terminalGroup
	host               string
	name               string
	containerName      string
	term               *terminal.Terminal
	stream             *client.Stream
	requestID          string
	stats              *monitor.Stats
	history            statusHistory
	statusSource       *serverStatusState
	monitorStatus      string
	// monitorActive mirrors what the broker was last told: whether this
	// session's server status is on screen.
	monitorActive bool
	closed        bool
	management    *serverManagementUI
	target        approval.Operation
}
type terminalOpenRequest struct {
	group   *terminalGroup
	opened  func(*session)
	pending *session
}

type app struct {
	routes                                                        []vault.RouteSummary
	routeError                                                    string
	tunnels                                                       tunnelManager
	vnc                                                           vncManager
	newVNCWindow                                                  toolsWindowFactory
	newVNCView                                                    func(mygo.WebViewOptions) (vncEmbeddedView, error)
	startup                                                       startupState
	presence                                                      presenceState
	connection                                                    connectionEditor
	dispatch                                                      func(func())
	keyPicker                                                     func() ([]string, error)
	directorySlots                                                chan struct{}
	localFilePane, primaryFilePane                                *filePane
	fileLayout                                                    *fileLayoutNode
	activeFilePane                                                *filePane
	filePaneSequence                                              uint64
	fileExpanded, editShown, editLocal                            bool
	toolsHidden, toolsRight                                       bool
	workspaceCanvas                                               bool
	revealedWorkspaceTab                                          workspaceTab
	workspaceHeaderTabsWidth                                      float32
	toolsEdge                                                     string
	toolsLayoutOpen                                               bool
	settingsSection                                               string
	settingsWin                                                   toolsWindow
	newSettingsWindow                                             toolsWindowFactory
	settingsOpen, settingsWindowPending, renderingSettingsWindow  bool
	settingsWindowSequence                                        uint64
	settingsWindowReady                                           bool
	editorWin                                                     toolsWindow
	newEditorWindow                                               editorWindowFactory
	editorWindowSequence                                          uint64
	editorWindowPending, renderingEditorWindow, editorWindowFocus bool
	filesTabOpen, filesTabSelected                                bool
	workspaceTools                                                map[int]*workspaceToolTab
	renderingWorkspaceTool                                        *workspaceToolTab
	workspaceToolBounds                                           ui.Rect
	workspaceRevision                                             uint64

	behavior                                 behaviorPreferences
	languageChoice                           string
	toolsDetached, renderingToolsWindow      bool
	toolsWin                                 toolsWindow
	newToolsWindow                           toolsWindowFactory
	toolsWindowSequence                      uint64
	toolsWindowPending                       bool
	toolsPublished                           toolsSharedState
	snippetDetached, transfersDetached       bool
	terminalShare, terminalWidthShare        float32
	diagnosticTab                            int
	statusMetric, statusRange, statusNetwork string
	processSort, serviceSort                 ui.SortOrder
	processSearch                            string
	managementFallback                       *serverManagementUI
	serviceSearch                            string
	serviceTable                             ui.ListState
	snippetGrid                              ui.GridState
	snippetSelected                          int
	menu                                     *mygo.Menu
	snippets                                 []snippet.Command
	snippetLoad                              uint64
	snippetSearch, snippetGroup              string
	snippetForm                              snippet.Command
	snippetDraft                             uint64
	snippetError                             string
	snippetOpen                              bool
	snippetHistoryWrites                     chan func()
	prompt                                   promptState
	editorSequence                           uint64
	editorRead                               *fileReadState
	authCancel                               context.CancelFunc
	authSequence                             uint64
	approvalTab                              int
	approvalHistory                          approvalHistoryState
	auth                                     *systemauth.Service
	authStatus                               systemauth.Status
	quickUnlock                              quickUnlockStore // nil: the local service; tests substitute
	legacyAuthChecked                        bool
	identities                               identitySettingsState
	transfers                                *transfer.Manager
	jobs                                     []transfer.Job
	dockerJobs                               []*dockerJob
	activityTab                              int
	activityHistory                          [2]int
	transferHistoryBusy                      bool
	transferHistoryError                     string
	transferCancelling                       map[string]bool
	uploadConflicts                          []uploadConflict
	transferJobsVersion                      atomic.Uint64
	transfersOpen                            bool
	connections                              []vault.Summary
	connectionOrderVersion                   atomic.Uint64
	pollWake                                 chan struct{}
	search                                   string
	groups                                   map[string]*bool
	connectionRailList                       ui.ListState
	connectionRailCache                      connectionRailModel
	sidebarWidth                             float32
	sidebarHidden                            bool
	layoutRendered                           workspaceLayout
	dock                                     int
	fileTable, processTable, transferTable   ui.ListState
	activityTable                            ui.ListState
	fileSelected                             int
	details                                  *vault.Summary

	win                           *mygo.Window
	dir                           string
	client                        client.Client
	server                        *desktopSession
	ctx                           context.Context
	cancel                        context.CancelFunc
	sessionCtx                    context.Context
	sessionCancel                 context.CancelFunc
	online, busy, create          bool
	generation                    int
	epoch                         atomic.Uint64
	queryOwner                    atomic.Pointer[desktopSession]
	unlockService                 client.ServiceClient
	lastUnlockNotice              time.Time
	pass, confirm, status         string
	unlockError                   string
	unlockState                   *unlockState
	unlockCleanup                 <-chan struct{}
	page                          int
	hosts                         []string
	selected                      string
	form                          vault.Profile
	formNote, formNoteOriginal    vault.ServerNote
	keyFile, ownerPass            string
	port                          string
	listedPath                    string
	requests                      []approval.Request
	selectedRequest               string
	approvalReviewKey             string
	approvalPasswordMode          bool
	inlineApproval, popupApproval *approvalPromptState
	mainWindowReady               bool
	applicationActive             bool
	// zoom is the main window's content zoom; 0 means unset, which draws at 100%.
	zoom      float32
	idleWatch sync.Once
	// idleUnlockedAt is when this unlock began, used by the idle lock. It belongs
	// to the UI thread.
	idleUnlockedAt                time.Time
	approvalTerms                 map[string]approvalTerms
	approvalAutoAttempted         map[string]bool
	sessions                      []*session
	serverStatuses                map[string]*serverStatusState
	terminalLayout                *terminalLayoutNode
	singleTerminalGroup           *terminalGroup
	activeTerminalGroup           *terminalGroup
	editorTerminalGroup           *terminalGroup
	terminalGroupSequence         uint64
	renderingTerminalGroups       bool
	pendingTerminalMutation       func()
	renderingTerminalGroup        *terminalGroup
	lastActiveTerminal            *session
	applyingTerminalFocus         bool
	connecting                    map[string]context.CancelFunc
	pendingShellOpens             map[string][]terminalOpenRequest
	active                        int
	contentFocus                  bool
	remotePath, localFile, saveAs string
	files                         []sshdata.File
	edit                          *editor.State
	editorSaving                  *editor.State
	editPath                      string
	editHash, editHost            string
	editRoute                     connectcfg.RouteSelection
	approvalWin                   *mygo.Window
	approvalID                    string
	approvalNonce                 string
	notified                      map[string]bool
	serverNotes                   *serverNotesState
	notesDNS                      servernotes.Lookup
	notesScannedAt                map[string]time.Time
}

func Run(dir string) error {
	return RunWithOptions(dir, Options{})
}

func RunWithOptions(dir string, opts Options) error {
	a := &app{dir: dir, client: client.Client{Dir: dir}, remotePath: "/", port: "22", form: vault.Profile{Port: 22, Auth: "password"}, edit: &editor.State{Buffer: editor.NewBuffer("")}}
	defer a.destroyPresence()
	a.pollWake = make(chan struct{}, 1)
	a.startup = newStartup(opts.Background)
	mygo.App.SetActivationPolicy(a.startup.policy())
	a.notified = make(map[string]bool)
	a.groups = make(map[string]*bool)
	a.sidebarWidth = 190
	a.terminalShare = 72
	a.terminalWidthShare = 65
	a.snippetSelected = -1
	a.fileSelected = -1
	a.dock = toolCommands
	a.loadLayout()
	a.loadBehavior()
	a.transfers = transfer.New(dir)
	a.create = !vault.Exists(dir)
	a.ctx, a.cancel = context.WithCancel(context.Background())
	defer a.cancel()
	// Cmd-Q stops the run loop instead of closing the window, so this is the only
	// cleanup on that path: without it the broker never writes its closing record
	// or its audit anchor.
	defer func() {
		if a.server != nil {
			a.server.Close()
		}
	}()
	var control *desktopcontrol.Server
	var startupErr error
	defer func() {
		if control != nil {
			control.Close()
		}
	}()
	mygo.App.OnDidBecomeActive(func() {
		a.startup.activations++
		a.applicationActive = true
		// Both automatic surfaces wait for activation, so both are re-queued
		// here. The approval prompt is parented to the main window and becomes
		// key before the application is active.
		a.queueUnlockAuthentication()
		a.queueInlineApproval()
	})
	mygo.App.OnDidResignActive(func() { a.applicationActive = false })
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.beginPresenceQuit() })
	mygo.App.OnActivate(func(visible bool) { a.presenceActivate(visible) })
	offLocale := mygo.App.OnLocaleChanged(func(string) { a.refreshLanguage() })
	defer offLocale()
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(a.mainWindowOptions(opts.Background, runtime.GOOS))
		a.installMenu()
		a.installPresence()
		a.win.OnClose(func(e *mygo.CloseEvent) { a.presenceClose(e.PreventDefault) })
		a.win.OnClosed(func() { a.shutdown() })
		a.win.OnReadyToShow(func() { a.mainWindowReady = true; a.queueInlineApproval(); a.queueUnlockAuthentication() })
		a.win.OnFocus(func() { a.queueInlineApproval(); a.queueUnlockAuthentication() })
		a.win.OnShow(func() { a.queueInlineApproval(); a.queueUnlockAuthentication(); a.syncMonitorVisibility() })
		a.win.OnHide(func() { a.hideApprovalAuthentication(); a.cancelUnlockAttempt(); a.syncMonitorVisibility() })
		mygo.Power.OnLockScreen(func() { a.lock() })
		mygo.Power.OnSuspend(func() { a.lock() })
		a.auth = systemauth.New(a.win.NativeHandle())
		a.refreshAuth()
		if local, err := client.NewLocalService(a.dir); err == nil {
			a.unlockService = local.Client()
			if !a.create && !opts.Background {
				go func() {
					ctx, cancel := context.WithTimeout(a.ctx, 12*time.Second)
					defer cancel()
					_ = local.Ensure(ctx)
				}()
			}
		}
		if opts.Background {
			control, startupErr = desktopcontrol.Start(a.ctx, a.dir, a.handleDesktopControl)
			if startupErr != nil {
				mygo.App.Quit()
				return
			}
		}
		go a.poll()
	})
	if err := mygo.App.Run(); err != nil {
		return err
	}
	return startupErr
}

// Relabel existing windows without opening them or changing terminal focus.
func (a *app) refreshLanguage() {
	a.refreshPresenceLabels()
	if a.win != nil {
		a.win.SetTitle(applicationName)
		a.installMenu()
	}
	if a.toolsWin != nil {
		a.toolsWin.SetTitle(a.toolsWindowTitle())
	}
	if a.settingsWin != nil {
		a.settingsWin.SetTitle(tr(nil, "设置"))
	}
	a.refreshEditorWindowTitle()
	if a.approvalWin != nil {
		title := tr(nil, "访问授权")
		for _, r := range a.requests {
			if r.ID == a.approvalID {
				title = a.approvalWindowTitle(r)
				break
			}
		}
		a.approvalWin.SetTitle(title)
		a.approvalWin.Invalidate()
	}
	a.invalidateWindows()
}

func (a *app) shutdown() {
	a.closeVNCs()
	a.closeTunnels()
	a.destroyPresence()
	a.approvalTerms, a.approvalAutoAttempted = nil, nil
	a.cancelEditorRead()
	a.closeEditorWindow()
	a.closeSettingsWindow()
	a.filesTabOpen, a.filesTabSelected = false, false
	a.workspaceTools, a.renderingWorkspaceTool = nil, nil
	a.stopSnippetHistoryWrites()
	a.disposeUnlockState()
	a.closeApprovalAuthentication()
	a.generation++
	a.clearApprovalHistory()
	a.clearServerStatuses()
	a.closeServerNotes()
	a.cancelFileProperties()
	a.cancelDockerWork()
	a.closeToolsWindow()
	if a.sessionCancel != nil {
		a.sessionCancel()
	}
	a.resetConnectionEditor()
	_ = a.saveLayout()
	a.cancelDirectoryLoads()
	if a.authCancel != nil {
		a.authCancel()
	}
	a.cancel()
	if a.transfers != nil {
		a.transfers.PauseAll()
	}
	if a.server != nil {
		a.server.Close()
	}
	for _, s := range a.sessions {
		if s.term != nil {
			s.term.Close()
		}
	}
}

// isActive reports whether this application is the one in front. The backend is
// asked directly once it runs, so activation cannot drift from our own record of
// the events it sent. Before App.Run (headless tests) the events are all there is.
func (a *app) isActive() bool {
	if !mygo.App.IsReady() {
		return a.applicationActive
	}
	return mygo.App.IsActive()
}

func (a *app) lock() {
	a.closeVNCs()
	a.closeTunnels()
	a.idleUnlockedAt = time.Time{}
	a.approvalTerms, a.approvalAutoAttempted = nil, nil
	a.cancelEditorRead()
	a.closeEditorWindow()
	a.closeSettingsWindow()
	a.filesTabOpen, a.filesTabSelected = false, false
	a.workspaceTools, a.renderingWorkspaceTool = nil, nil
	a.stopSnippetHistoryWrites()
	a.queryOwner.Store(nil)
	a.disposeUnlockState()
	a.closeApprovalAuthentication()
	a.closeServerNotes()
	a.cancelFileProperties()
	a.cancelDockerWork()
	a.closeToolsWindow()
	a.toolsDetached = false
	a.moveToolDialogsToMain()
	for _, cancel := range a.connecting {
		cancel()
	}
	a.connecting = nil
	a.pendingShellOpens = nil
	if a.sessionCancel != nil {
		a.sessionCancel()
		a.sessionCancel = nil
	}
	a.sessionCtx = nil
	a.busy = false
	a.resetConnectionEditor()
	a.cancelDirectoryLoads()
	if a.authCancel != nil {
		a.authCancel()
	}
	if a.transfers != nil {
		a.transfers.PauseAll()
	}
	a.connections = nil
	a.routes = nil
	a.routeError = ""
	a.connectionRailCache = connectionRailModel{}
	a.connectionRailList = ui.ListState{}
	a.details = nil
	a.generation++
	a.epoch.Add(1)
	a.online = false
	a.pass = ""
	a.ownerPass = ""
	a.confirm = ""
	a.unlockError = ""
	a.form.Password = ""
	a.form.PrivateKey = ""
	a.form.KeyPassphrase = ""
	a.requests = nil
	a.clearApprovalHistory()
	a.hosts = nil
	a.files = nil
	a.edit = &editor.State{Buffer: editor.NewBuffer("")}
	a.editPath = ""
	a.editHash = ""
	a.editHost = ""
	a.editShown = false
	a.editorSaving = nil
	a.contentFocus = false
	a.fileLayout = nil
	a.activeFilePane = nil
	a.filePaneSequence = 0
	a.localFilePane = nil
	a.primaryFilePane = nil
	a.snippets = nil
	a.snippetLoad++
	a.snippetSelected = -1
	a.snippetGrid = ui.GridState{}
	a.snippetForm = snippet.Command{}
	a.snippetDraft++
	a.snippetError = ""
	a.snippetGroup, a.snippetSearch = "", ""
	a.snippetOpen = false
	a.formNote, a.formNoteOriginal = vault.ServerNote{}, vault.ServerNote{}
	a.transfersOpen = false
	a.activityHistory = [2]int{}
	a.transferHistoryBusy, a.transferHistoryError = false, ""
	a.transferCancelling = nil
	a.uploadConflicts = nil
	a.transferJobsVersion.Add(1)
	a.toolsLayoutOpen = false
	a.toolsHidden = false
	a.diagnosticTab = 0
	a.statusMetric, a.statusNetwork, a.processSearch = "", "", ""
	a.serviceSearch = ""
	a.prompt = promptState{}
	a.notified = make(map[string]bool)
	if a.approvalWin != nil {
		a.approvalWin.Close()
		a.approvalWin = nil
	}
	for _, s := range a.sessions {
		if s.term != nil {
			s.term.Close()
		}
	}
	a.clearServerStatuses()
	a.sessions = nil
	a.terminalLayout, a.lastActiveTerminal = nil, nil
	a.singleTerminalGroup, a.activeTerminalGroup, a.editorTerminalGroup = nil, nil, nil
	a.pendingTerminalMutation = nil
	a.terminalGroupSequence = 0
	s := a.server
	a.server = nil
	lockStatus := tr(nil, "已锁定，连接与授权已撤销")
	if s != nil {
		s.revoke()
		lockStatus = tr(nil, "正在锁定连接服务…")
		expected, gen := lockStatus, a.generation
		done := make(chan struct{})
		a.unlockCleanup = done
		go func() {
			defer close(done)
			err := s.Lock()
			a.postUI(func() {
				if !a.online && a.generation == gen && a.status == expected {
					if err == nil {
						a.status = tr(nil, "已锁定，连接与授权已撤销")
					} else {
						a.status = tr(nil, "界面已锁定，连接服务未确认锁定")
						a.unlockError = a.status
					}
				}
				// The lock screen offers quick unlock only if the service
				// still holds this executable's envelope.
				if !a.online && a.generation == gen {
					a.refreshAuth()
				}
			})
		}()
	}
	a.status = lockStatus
	if a.win != nil {
		a.win.Invalidate()
	}
}

// All app state changes run on the UI thread; blocking work runs separately.
func (a *app) work(fn func() (func(), error)) {
	if a.busy {
		return
	}
	a.busy = true
	gen, wasOnline := a.generation, a.online
	go func() {
		done, e := fn()
		a.postUI(func() {
			if gen != a.generation {
				// An obsolete unlock result owns a broker that its guarded callback closes.
				if !wasOnline && done != nil {
					done()
				}
				return
			}
			a.busy = false
			if e != nil {
				a.status = e.Error()
				if !a.online {
					a.unlockError = e.Error()
				}
			} else if done != nil {
				done()
			}
		})
	}()
}
func (a *app) unlock() {
	if !a.create {
		a.beginVaultUnlock(false)
		return
	}
	a.unlockError = ""
	pass := []byte(a.pass)
	confirm := a.confirm
	create := a.create
	gen := a.generation
	dir := a.dir
	seal := a.quickUnlockSealer() // a new vault is opened with its password
	a.pass = ""
	a.confirm = ""
	a.work(func() (func(), error) {
		defer wipe(pass)
		if create {
			if string(pass) != confirm {
				return nil, errors.New(tr(nil, "两次主密码不一致"))
			}
			release, e := localipc.Acquire(dir)
			if e != nil {
				return nil, e
			}
			v, e := vault.New(pass)
			if e == nil {
				e = vault.Save(vault.Path(dir), v, true)
				v.Close()
			}
			release()
			if e != nil {
				return nil, e
			}
		}
		source, e := vault.Read(vault.Path(dir), pass)
		if e != nil {
			return nil, e
		}
		s, e := openDesktopSession(a.ctx, dir, source, seal)
		if e != nil {
			return nil, e
		}
		return func() {
			if gen != a.generation || a.ctx.Err() != nil {
				go s.Abandon()
				return
			}
			a.server = s
			a.client.BrokerKey = s.PublicKeyPin()
			a.online = true
			a.page = 1
			a.create = false
			a.generation++
			a.epoch.Add(1)
			a.startUnlockedSession()
			a.status = ""
			a.initializeWorkspace()
		}, nil
	})
}
func (a *app) poll() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	var reader *client.ServiceStatusReader
	defer func() { reader.Close() }()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-t.C:
		case <-a.pollWake:
		}
		epoch := a.epoch.Load()
		owner := a.queryOwner.Load()
		if owner == nil {
			reader = a.pollUnlockNotice(epoch, reader)
			continue
		}
		reader.Close()
		reader = nil
		ctx, cancel := context.WithTimeout(a.ctx, 2*time.Second)
		cli := client.Client{Dir: a.dir, BrokerKey: owner.PublicKeyPin()}
		orderVersion := a.connectionOrderVersion.Load()
		h, e := cli.CallWithOwner(ctx, protocol.Message{Action: "hosts"}, owner)
		a.postUI(func() { a.applyConnectionSnapshot(epoch, orderVersion, h, e) })

		p, err := cli.CallWithOwner(ctx, protocol.Message{Action: "pending"}, owner)
		cancel()
		if err != nil {
			continue
		}
		var jobs []transfer.Job
		jobsVersion := a.transferJobsVersion.Load()
		if a.transfers != nil {
			jobs = a.transfers.List()
		}
		sort.Slice(p.Requests, func(i, j int) bool { return p.Requests[i].Expires.Before(p.Requests[j].Expires) })
		a.postUI(func() {
			if !a.online || a.epoch.Load() != epoch {
				return
			}
			if a.transferJobsVersion.Load() == jobsVersion {
				a.jobs = jobs
			}
			a.requests = p.Requests
			a.pruneApprovalTerms()
			if a.approvalCenterVisible() && a.approvalTab == 2 {
				a.refreshApprovalHistoryIfDue()
			}
			if a.approvalCenterVisible() && a.approvalTab == 1 && !a.identities.busy && time.Since(a.identities.lastAttempt) > 10*time.Second {
				a.refreshIdentities(nil)
			}
			if a.approvalWin != nil {
				pending := false
				var current approval.Request
				for _, r := range p.Requests {
					if r.ID == a.approvalID && r.Nonce == a.approvalNonce && r.Status == "pending" {
						pending, current = true, r
					}
				}
				if !pending {
					old := a.approvalWin
					a.approvalWin = nil
					a.approvalID, a.approvalNonce = "", ""
					old.Close()
				} else {
					a.approvalWin.SetTitle(a.approvalWindowTitle(current))
				}
			}
			if r, ok := a.nextApprovalPopup(time.Now()); ok {
				a.showApproval(r)
			}
			if a.selectedRequest == "" {
				for _, r := range a.requests {
					if r.Status == "pending" {
						a.selectedRequest = r.ID
						break
					}
				}
			}
		})
	}
}
func hostID(s string) string { return strings.SplitN(s, " · ", 2)[0] }
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func (a *app) view(c *ui.Context) {
	// What this frame shows decides whether the broker keeps sampling servers
	// nobody is looking at.
	defer a.syncMonitorVisibility()
	// Browser-like content zoom: MyGo lays the view out in a window 1/z its size
	// and draws it z times larger, so text and glyphs stay sharp.
	c.SetZoom(a.viewZoom())
	defer a.queueUnlockAuthentication()
	defer a.queueInlineApproval()
	defer a.publishToolsState()
	components.ApplyTheme(c, !a.online)
	if a.online && a.vnc.fullscreen != nil && a.vnc.fullscreen.view != nil {
		ui.WebView(c, a.vnc.fullscreen.view).Fill().Label(tr(c, "VNC 远程桌面内容"))
		return
	}
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Background).Draw(func(p *ui.Painter, r ui.Rect) {
		if a.online && a.workspaceNavigationPage() == 1 {
			inset := components.WorkspaceInset
			p.Fill(ui.Rect{X: r.X + inset, Y: r.Y + inset, W: components.SidebarWidth(c, a.sidebarWidth, a.sidebarHidden), H: max(0, r.H-2*inset)}, t.Surface, components.PanelRadius)
		}
	}).Children(func() {

		if !a.online {
			lockedWindowChrome(c)
			a.lockedView(c)
		} else {
			a.workspaceHeader(c)
			a.workspaceView(c)
		}
		if a.online {
			a.workspaceStatusNotice(c)
		}
		if a.online && a.page == 0 && !(a.prompt.open && a.prompt.connectionIdentity) {
			a.connectionView(c)
		}
		if !a.snippetDetached || !a.toolsDetached || a.toolsHidden {
			a.snippetDialog(c)
		}
		a.tunnelsView(c)
		a.vncDialog(c)
		a.serverNotesDialog(c)
		a.promptForHost(c, dialogHostMain)
	})
}
func field(c *ui.Context, label string, value *string, secret bool) {
	ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).Width(170)
		v := ui.TextInput(c, value).Grow(1).Label(label)
		if secret {
			v.Password()
		}
	})
}

// Human gestures use the process-bound owner capability. External requests never
// enter this path and retain separate fresh verification.
func (a *app) operationContext() context.Context {
	ctx := context.Background()
	if a.sessionCtx != nil {
		ctx = a.sessionCtx
	} else if a.ctx != nil {
		ctx = a.ctx
	}
	if source := a.serverStateSession(); source != nil {
		ctx = client.WithRoute(ctx, source.host, source.target.Route)
	}
	return ctx
}
func (a *app) startUnlockedSession() {
	a.idleUnlockedAt = time.Now()
	if a.server != nil {
		a.connectionRailCache.loading = true
		a.queryOwner.Store(a.server)
		a.wakePoll()
	}
	if a.sessionCancel != nil {
		a.sessionCancel()
	}
	a.sessionCtx, a.sessionCancel = context.WithCancel(a.ctx)
	if server := a.server; server != nil && server.done != nil {
		ctx, gen := a.sessionCtx, a.generation
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-server.done:
			}
			a.postUI(func() {
				if a.server == server && a.generation == gen {
					a.lock()
					a.status = tr(nil, "连接服务会话已结束，请重新解锁")
					a.unlockError = a.status
				}
			})
		}()
	}

}
func (a *app) begin(op approval.Operation, execute func(*client.Stream) (func(), error)) {
	a.beginContext(a.operationContext(), op, execute, nil)
}
func (a *app) beginContext(ctx context.Context, op approval.Operation, execute func(*client.Stream) (func(), error), failed func(error)) {
	report := func(err error) {
		if failed != nil {
			failed(err)
		} else {
			a.status = err.Error()
		}
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		report(err)
		return
	}
	if op.Host == "" {
		report(errors.New(tr(nil, "请先选择连接")))
		return
	}
	cli, gen := a.client, a.generation
	go func() {
		stream, err := cli.StartWithOwner(ctx, op, owner)
		if err != nil {
			a.postUI(func() {
				if gen == a.generation && ctx.Err() == nil {
					report(err)
				}
			})
			return
		}
		done, err := execute(stream)
		stream.Close()
		a.postUI(func() {
			if gen != a.generation || ctx.Err() != nil {
				return
			}
			if err != nil {
				report(err)
			} else if done != nil {
				done()
			}
		})
	}()
}
func (a *app) openShell()               { a.openShellMode(false) }
func (a *app) openShellMode(force bool) { a.openShellAfter(force, nil) }
func (a *app) openShellAfter(force bool, opened func(*session)) {
	a.openShellForHostAfter(a.selected, force, opened)
}

func (a *app) openShellForHostAfter(host string, force bool, opened func(*session), route ...connectcfg.RouteSelection) {
	a.filesTabSelected = false
	a.ensureTerminalGroups()
	if len(route) != 0 && route[0].Temporary() {
		if _, err := a.unlockedOwner(); err != nil {
			a.status = err.Error()
			return
		}
		pending := a.reserveTerminal(host, a.activeTerminalGroup, opened == nil)
		pending.target.Route = route[0]
		a.openShellInGroup(host, true, pending.group, opened, pending)
		return
	}
	a.openShellInGroup(host, force, a.activeTerminalGroup, opened)
}

func (a *app) openShellInGroup(host string, force bool, targetGroup *terminalGroup, opened func(*session), reserved ...*session) {
	var replace *session
	if !force {
		for _, s := range a.sessions {
			if s.host == host && s.target.Kind != "docker-terminal" && !s.target.Route.Temporary() {
				if !s.closed {
					a.page = 1
					a.activateTerminal(s, true)
					return
				}
				replace = s
			}
		}
	}

	owner, err := a.unlockedOwner()
	if err != nil {
		a.status = err.Error()
		return
	}
	if host == "" {
		a.status = tr(nil, "请先选择连接")
		return
	}
	var pending *session
	if len(reserved) != 0 {
		pending = reserved[0]
	}
	if pending == nil {
		pending = a.reserveTerminal(host, targetGroup, opened == nil)
	}
	if !a.hasSession(pending) {
		return
	}
	if a.connecting[host] != nil {
		pending.connectionState = "queued"
		if a.pendingShellOpens == nil {
			a.pendingShellOpens = make(map[string][]terminalOpenRequest)
		}
		a.pendingShellOpens[host] = append(a.pendingShellOpens[host], terminalOpenRequest{group: targetGroup, opened: opened, pending: pending})
		pending.connectionCancel = func() {
			requests := a.pendingShellOpens[host]
			for i, request := range requests {
				if request.pending == pending {
					requests = append(requests[:i], requests[i+1:]...)
					break
				}
			}
			if len(requests) == 0 {
				delete(a.pendingShellOpens, host)
			} else {
				a.pendingShellOpens[host] = requests
			}
		}
		return
	}
	pending.connectionState, pending.connectionError, pending.closed = "connecting", "", false
	ctx, cancel := context.WithCancel(client.WithRoute(a.operationContext(), host, pending.target.Route))
	pending.connectionCancel = cancel
	gen, cli := a.generation, a.client
	if a.connecting == nil {
		a.connecting = map[string]context.CancelFunc{}
	}
	// Keep the in-flight entry until its completion runs. A new request cannot
	// replace it while a canceled result is still waiting on the UI queue.
	a.connecting[host] = func() {
		cancel()
		a.failPendingTerminals(host, tr(nil, "连接已取消"))
	}
	a.status = tr(nil, "正在连接 ") + a.hostName(host) + "…"
	var target vault.Summary
	for _, profile := range a.connections {
		if profile.ID == host {
			target = profile
			break
		}
	}
	steps, trustErr := a.connectionTrustPlan(host, pending.target.Route)
	if target.ID != "" && (trustErr != nil || len(steps) > 0) {
		a.readFirstHostIdentity(ctx, target, pending, opened, gen)
		return
	}
	go func() {
		stream, err := cli.ConnectWithOwner(ctx, host, owner)
		a.postUI(func() {
			if gen == a.generation {
				delete(a.connecting, host)
			}
			if gen != a.generation || !a.online || ctx.Err() != nil || !a.hasSession(pending) {
				cancel()
				if a.hasSession(pending) {
					pending.closed = true
					pending.connectionState = "error"
					pending.connectionError = tr(nil, "连接已取消")
					if errors.Is(ctx.Err(), context.DeadlineExceeded) {
						pending.connectionError = tr(nil, "连接超时，请检查网络后重试")
					}
				}
				if stream != nil {
					stream.Close()
				}
				if gen == a.generation {
					if a.online {
						// Cancel cleared earlier requests. Any remaining request
						// arrived after cancellation and still needs its own PTY.
						a.openNextShell(host)
					} else {
						delete(a.pendingShellOpens, host)
					}
				}
				return
			}
			if err != nil {
				cancel()
				pending.closed = true
				pending.connectionState, pending.connectionError = "error", connectionError(err)
				a.failPendingTerminals(host, pending.connectionError)
				if errors.Is(err, context.Canceled) {
					a.status = ""
					return
				}
				if isHostKeyMismatch(err) {
					pending.connectionMismatch = true
					return
				}
				a.status = connectionError(err)
				return
			}
			attached := a.attachTerminalInGroup(stream, host, cancel, false, pending.group, pending)
			if a.status == tr(nil, "正在连接 ")+a.hostName(host)+"…" {
				a.status = ""
			}
			if replace != nil && attached != nil {
				a.replaceClosedTerminal(replace, attached)
			}
			if attached == nil {
				a.failPendingTerminals(host, a.status)
				return
			}
			if opened != nil {
				a.status = ""
				opened(attached)
			}
			a.openNextShell(host)
		})
	}()
}

func (a *app) openNextShell(host string) {
	if pending := a.pendingShellOpens[host]; len(pending) > 0 {
		next := pending[0]
		if len(pending) == 1 {
			delete(a.pendingShellOpens, host)
		} else {
			a.pendingShellOpens[host] = pending[1:]
		}
		a.openShellInGroup(host, true, next.group, next.opened, next.pending)
	}
}

func (a *app) attachTerminal(stream *client.Stream, host string, finish context.CancelFunc, activate ...bool) *session {
	a.ensureTerminalGroups()
	return a.attachTerminalInGroup(stream, host, finish, len(activate) == 0 || activate[0], a.activeTerminalGroup)
}

func (a *app) attachTerminalInGroup(stream *client.Stream, host string, finish context.CancelFunc, activate bool, targetGroup *terminalGroup, reserved ...*session) *session {
	gen := a.generation
	if a.notified == nil {
		a.notified = map[string]bool{}
	}
	a.notified[stream.RequestID] = true
	// The broker starts collecting as soon as the session subscribes; the first
	// frame decides whether anything shows it.
	var sess *session
	if len(reserved) != 0 {
		sess = reserved[0]
	}
	if sess == nil {
		sess = &session{group: targetGroup, host: host}
	}
	sess.target, sess.requestID, sess.stream = stream.Operation, stream.RequestID, stream
	sess.monitorStatus, sess.monitorActive = "正在读取服务器状态…", true
	conn := &client.TerminalConn{
		Stream:         stream,
		OnStats:        func(stats *monitor.Stats) { a.updateServerStatus(sess, gen, stats, "自动更新") },
		OnMonitorError: func(message string) { a.updateServerStatus(sess, gen, nil, message) },
	}
	lightColors, darkColors := workspaceTerminalColors()
	term, e := terminal.New(terminal.Options{Conn: conn, Theme: lightColors, DarkTheme: darkColors, Padding: workspaceTerminalPadding(), Scrollback: 32 << 20, OptionAsAlt: true, ClipboardRead: terminal.ClipboardDeny, NoClipboardWrite: true})
	if e != nil {
		stream.Close()
		a.status = e.Error()
		if a.hasSession(sess) {
			sess.closed, sess.connectionState, sess.connectionError = true, "error", e.Error()
		}
		if finish != nil {
			finish()
		}
		return nil
	}
	name := host
	for _, d := range a.connections {
		if d.ID == host {
			name = d.Name
			break
		}
	}
	base := name
	for n := 2; ; n++ {
		collision := false
		for _, existing := range a.sessions {
			if existing != sess && existing.name == name {
				collision = true
				break
			}
		}
		if !collision {
			break
		}
		name = fmt.Sprintf("%s (%d)", base, n)
	}
	if sess.name == "" {
		sess.name = name
	}
	previousTerm := sess.term
	sess.term, sess.closed = term, false
	sess.connectionState, sess.connectionError, sess.connectionCancel = "", "", nil
	if previousTerm != nil {
		previousTerm.Close()
	}
	if !a.hasSession(sess) {
		a.sessions = append(a.sessions, sess)
	}
	if a.page == 1 && activate {
		a.active = len(a.sessions) - 1
		// A fresh connection starts on the commands page; files and server
		// status stay available but are opened on demand.
		a.dock = toolCommands
		a.syncFollowingFilePanes()
		a.contentFocus = true
		a.editShown = false
		a.status = ""
	}
	go func() {
		<-term.Done()
		if finish != nil {
			finish()
		}
		a.postUI(func() {
			if gen == a.generation && a.hasSession(sess) && sess.term == term {
				a.detachServerStatus(sess)
				sess.closed = true
				sess.monitorStatus = "已断开，保留最后状态"
			}
		})
	}()
	return sess
}
func (a *app) decide(id string, allow, revoke bool) {
	pass := []byte(a.ownerPass)
	a.ownerPass = ""
	reviewed, ok := a.reviewedApproval(id)
	if !ok {
		wipe(pass)
		a.status = tr(nil, "授权请求已变化，请重新查看")
		return
	}
	cli := a.client
	ctx := a.operationContext()
	owner, ownerErr := a.unlockedOwner()
	a.work(func() (func(), error) {
		defer wipe(pass)
		var e error
		if !allow || revoke {
			if ownerErr != nil {
				return nil, ownerErr
			}
			e = cli.DecideReviewedWithOwner(ctx, reviewed, allow, revoke, owner)
		} else {
			e = cli.DecideReviewed(ctx, reviewed, allow, revoke, pass)
		}
		return func() {
			a.markApprovalHandled(reviewed)
			a.status = tr(nil, "授权决定已处理")
			if revoke {
				for i, r := range a.requests {
					if r.ID == reviewed.ID && r.Nonce == reviewed.Nonce || r.ParentID == reviewed.ID {
						a.requests[i].Status = "revoked"
					}
				}
			}
		}, e
	})
}

type limitBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New(tr(nil, "文本超过4 MiB，请直接下载"))
	}
	return b.Buffer.Write(p)
}
func (a *app) replaceText() {
	document := a.edit
	content := []byte(editorBufferText(a.edit.Buffer))
	h := sha256.Sum256(content)
	hash := hex.EncodeToString(h[:])
	p := a.editPath
	host := a.editHost
	oldHash := a.editHash
	a.editorSaving = document
	a.beginContext(client.WithRoute(a.operationContext(), host, a.editRoute), approval.Operation{Host: host, Kind: "replace", Path: p, ContentHash: hash, OriginalHash: oldHash}, func(s *client.Stream) (func(), error) {
		defer s.Close()
		if _, e := s.Write(content); e != nil {
			return nil, e
		}
		if e := s.End(); e != nil {
			return nil, e
		}
		var backup bytes.Buffer
		_, _, e := s.Copy(&backup, io.Discard)
		return func() {
			if a.editorSaving == document {
				a.editorSaving = nil
			}
			if a.edit == document && a.editHost == host && a.editPath == p {
				a.editHash = hash
				if editorBufferText(a.edit.Buffer) == string(content) {
					a.edit.Buffer.MarkSaved()
					a.refreshEditorWindowTitle()
				}
			}
			a.status = tr(nil, "已保存 · ") + a.hostName(host) + " · " + p + tr(nil, "；旧副本: ") + backup.String()
		}, e
	}, func(err error) {
		if a.editorSaving == document {
			a.editorSaving = nil
		}
		a.status = err.Error()
	})
}
func (a *app) download() {
	files, e := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: a.win, Title: tr(nil, "下载到本机")})
	if e != nil {
		a.status = e.Error()
		return
	}
	if files == "" {
		return
	}
	a.addTransfer("download", files, a.remotePath)
}
func (a *app) upload(fromEditor bool) {
	if !fromEditor {
		a.addTransfer("upload", a.localFile, a.remotePath)
		return
	}
	cliPath, remote, host := a.localFile, a.remotePath, a.selected
	document := a.edit
	if document == nil || a.editorSaving == document {
		return
	}
	a.editorSaving = document
	clearSaving := func() {
		if a.editorSaving == document {
			a.editorSaving = nil
		}
	}
	gen := a.generation
	ctx := a.operationContext()
	var content []byte
	if fromEditor {
		content = []byte(editorBufferText(a.edit.Buffer))
		remote = a.saveAs
		host = a.editHost
		ctx = client.WithRoute(ctx, host, a.editRoute)
	}
	go func() {
		var source io.ReadSeeker
		var f *os.File
		var e error
		if fromEditor {
			source = bytes.NewReader(content)
		} else {
			f, e = os.Open(cliPath)
			if e != nil {
				a.postUI(func() { clearSaving(); a.status = e.Error() })
				return
			}
			st, e := f.Stat()
			if e != nil || !st.Mode().IsRegular() {
				f.Close()
				a.postUI(func() { a.status = tr(nil, "请选择普通文件") })
				return
			}
			source = f
		}
		hash := sha256.New()
		if _, e = io.Copy(hash, source); e != nil {
			if f != nil {
				f.Close()
			}
			a.postUI(func() { clearSaving(); a.status = e.Error() })
			return
		}
		if f != nil {
			f.Close()
		}
		fingerprint := hex.EncodeToString(hash.Sum(nil))
		a.postUI(func() {
			if gen != a.generation || ctx.Err() != nil || !a.online {
				clearSaving()
				return
			}
			a.beginContext(ctx, approval.Operation{Host: host, Kind: "put", Path: remote, ContentHash: fingerprint}, func(s *client.Stream) (func(), error) {
				defer s.Close()
				var source io.Reader = bytes.NewReader(content)
				if !fromEditor {
					input, e := os.Open(cliPath)
					if e != nil {
						return nil, e
					}
					defer input.Close()
					source = input
				}
				if _, e := io.Copy(s, source); e != nil {
					return nil, e
				}
				if e := s.End(); e != nil {
					return nil, e
				}
				_, _, e := s.Copy(io.Discard, io.Discard)
				return func() {
					clearSaving()
					if fromEditor && a.edit == document && a.editHost == host {
						a.editPath = remote
						a.refreshEditorWindowTitle()
						a.editHash = fingerprint
						if editorBufferText(a.edit.Buffer) == string(content) {
							a.edit.Buffer.MarkSaved()
						}
					}
					a.status = tr(nil, "已另存为 · ") + a.hostName(host) + " · " + remote
				}, e
			}, func(err error) { clearSaving(); a.status = err.Error() })
		})
	}()
}
