package components

import (
	"embed"
	"github.com/egoist/mygo/ui"
)

//go:embed lucide/*.svg lucide/LICENSE
var lucideAssets embed.FS

func lucideIcon(name string) *ui.SVG {
	data, err := lucideAssets.ReadFile("lucide/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	return ui.MustParseSVG(data)
}

func lineIcon(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

// Shared vector actions keep the same shape and weight on all three platforms.
var (
	SearchIcon        = lucideIcon("search")
	PlusIcon          = lucideIcon("plus")
	CloseIcon         = lucideIcon("x")
	MoreIcon          = lucideIcon("ellipsis")
	SidebarIcon       = lucideIcon("panel-left")
	WorkspaceIcon     = lineIcon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 9h18M9 9v11"/>`)
	SettingsIcon      = lucideIcon("settings")
	ShieldIcon        = lineIcon(`<path d="m12 3 8 3v6c0 4-4 7-8 9-4-2-8-5-8-9V6z"/><path d="m8.5 12 2.3 2.3 4.7-4.7"/>`)
	PendingIcon       = lineIcon(`<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>`)
	ActiveIcon        = lineIcon(`<circle cx="12" cy="12" r="9"/><path d="m8 12 3 3 5-6"/>`)
	HistoryIcon       = lineIcon(`<path d="M3 5v5h5M4.3 9a8 8 0 1 1-.3 6M12 7v5l3 2"/>`)
	LockIcon          = lineIcon(`<rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3m-4 5v2"/>`)
	FingerprintIcon   = lineIcon(`<path d="M4.5 7.5a8.5 8.5 0 0 1 15 1M3.5 12a8.5 8.5 0 0 1 .2-1.8M20.5 12c0 3.5-.5 6-1.5 8M6.5 16c.5-1.2.5-2.5.5-4a5 5 0 0 1 10 0c0 4-.5 7-1.7 10M4 19c1-1 1.5-2 1.7-3M9.5 20c1-2.5 1-5.5 1-8a1.5 1.5 0 0 1 3 0c0 4-.3 7-1 10M7.5 22l.5-1M14.5 5.5a6.5 6.5 0 0 0-9 6c0 .8 0 1.5-.1 2"/>`)
	BottomPanelIcon   = lucideIcon("panel-bottom")
	RightPanelIcon    = lucideIcon("panel-right")
	ExpandPanelIcon   = lucideIcon("maximize-2")
	RestorePanelIcon  = lucideIcon("minimize-2")
	DetachPanelIcon   = lineIcon(`<path d="M7 4H3v17h17v-4"/><rect x="8" y="3" width="13" height="13" rx="1"/>`)
	TerminalIcon      = lineIcon(`<path d="m4 17 6-5-6-5m8 12h8"/>`)
	SplitTerminalIcon = lineIcon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M12 4v16m-6-9 2 1-2 1m9-2 2 1-2 1"/>`)
	TransfersIcon     = lucideIcon("download")
	FollowIcon        = lineIcon(`<path d="M10 13a5 5 0 0 0 7.07 0l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71M14 11a5 5 0 0 0-7.07 0l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>`)
	RefreshIcon       = lucideIcon("refresh-cw")
	ChevronRightIcon  = lucideIcon("chevron-right")
	ChevronDownIcon   = lucideIcon("chevron-down")
	FolderIcon        = lucideIcon("folder")
	FolderOpenIcon    = lineIcon(`<path d="M3 17V7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v1M3 19l3-8h16l-3 8z" fill="currentColor" fill-opacity=".08"/>`)
	FileIcon          = lucideIcon("file-text")
	CodeFileIcon      = lineIcon(`<path d="M14 3H5v18h14V8zM14 3v5h5m-9 4-2 2 2 2m4-4 2 2-2 2"/>`)
	ImageFileIcon     = lineIcon(`<path d="M14 3H5v18h14V8zM14 3v5h5M7 18l4-4 2 2 2-2 2 4"/><circle cx="9" cy="11" r="1"/>`)
	ArchiveFileIcon   = lineIcon(`<path d="M14 3H5v18h14V8zM14 3v5h5M10 4v2m2 0v2m-2 0v2m2 0v2m-2 1h3v4h-3z"/>`)
	FaceIcon          = lineIcon(`<circle cx="12" cy="12" r="9"/><path d="M9 10h.01M15 10h.01M8.5 14.5a4.5 4.5 0 0 0 7 0"/>`)
	KeyIcon           = lineIcon(`<circle cx="8" cy="8" r="4"/><path d="m11 11 8 8m-3 0h3v-3"/>`)
	CheckCircleIcon   = lineIcon(`<circle cx="12" cy="12" r="9"/><path d="m8 12.5 2.5 2.5 5.5-6"/>`)
	EyeIcon           = lineIcon(`<path d="M2 12s3.5-6.5 10-6.5S22 12 22 12s-3.5 6.5-10 6.5S2 12 2 12z"/><circle cx="12" cy="12" r="2.8"/>`)
	EyeOffIcon        = lineIcon(`<path d="M3 5l18 14M10.6 6.1A9.7 9.7 0 0 1 12 6c6.5 0 10 6 10 6a17 17 0 0 1-3.2 3.8M6.4 8.2A16.5 16.5 0 0 0 2 12s3.5 6 10 6c1.3 0 2.5-.2 3.5-.6"/>`)
	SaveIcon          = lucideIcon("save")
	UploadIcon        = lucideIcon("upload")
	DownloadIcon      = lucideIcon("download")
	FolderPlusIcon    = lucideIcon("folder-plus")
	FolderTreeIcon    = lucideIcon("folder-tree")
	UpIcon            = lucideIcon("arrow-up")
)

func SVGButton(c *ui.Context, icon *ui.SVG, label string, tooltip ...string) ui.Element {
	button := Button(c, "", ButtonOptions{Variant: Quiet}).Size(32, 32).
		Padding(0).Label(label)
	tip := label
	if len(tooltip) > 0 {
		tip = tooltip[0]
	}
	if tip != "" {
		button.Tooltip(tip)
	}
	button.Children(func() { ui.Icon(c, icon).Size(18, 18) })
	return button
}
