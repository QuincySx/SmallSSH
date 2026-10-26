package components

import "github.com/egoist/mygo/ui"

func lineIcon(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

// Shared vector actions keep the same shape and weight on all three platforms.
var (
	SearchIcon        = lineIcon(`<circle cx="10.5" cy="10.5" r="6.5"/><path d="m15.5 15.5 5 5"/>`)
	PlusIcon          = lineIcon(`<path d="M12 5v14M5 12h14"/>`)
	CloseIcon         = lineIcon(`<path d="m6 6 12 12M18 6 6 18"/>`)
	MoreIcon          = lineIcon(`<circle cx="12" cy="5" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="12" cy="19" r="1"/>`)
	SidebarIcon       = lineIcon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16M6 8v1m0 3v1m0 3v1"/>`)
	WorkspaceIcon     = lineIcon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 9h18M9 9v11"/>`)
	ShieldIcon        = lineIcon(`<path d="m12 3 8 3v6c0 4-4 7-8 9-4-2-8-5-8-9V6z"/><path d="m8.5 12 2.3 2.3 4.7-4.7"/>`)
	PendingIcon       = lineIcon(`<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>`)
	ActiveIcon        = lineIcon(`<circle cx="12" cy="12" r="9"/><path d="m8 12 3 3 5-6"/>`)
	HistoryIcon       = lineIcon(`<path d="M3 5v5h5M4.3 9a8 8 0 1 1-.3 6M12 7v5l3 2"/>`)
	LockIcon          = lineIcon(`<rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3m-4 5v2"/>`)
	FingerprintIcon   = lineIcon(`<path d="M4.5 7.5a8.5 8.5 0 0 1 15 1M3.5 12a8.5 8.5 0 0 1 .2-1.8M20.5 12c0 3.5-.5 6-1.5 8M6.5 16c.5-1.2.5-2.5.5-4a5 5 0 0 1 10 0c0 4-.5 7-1.7 10M4 19c1-1 1.5-2 1.7-3M9.5 20c1-2.5 1-5.5 1-8a1.5 1.5 0 0 1 3 0c0 4-.3 7-1 10M7.5 22l.5-1M14.5 5.5a6.5 6.5 0 0 0-9 6c0 .8 0 1.5-.1 2"/>`)
	BottomPanelIcon   = lineIcon(`<path d="M3 14h18v6H3z" fill="currentColor" fill-opacity=".2" stroke="none"/><rect x="3" y="4" width="18" height="16" rx="1"/><path d="M3 14h18"/>`)
	RightPanelIcon    = lineIcon(`<path d="M15 4h6v16h-6z" fill="currentColor" fill-opacity=".2" stroke="none"/><rect x="3" y="4" width="18" height="16" rx="1"/><path d="M15 4v16"/>`)
	ExpandPanelIcon   = lineIcon(`<path d="M9 3H3v6m12-6h6v6M3 15v6h6m12-6v6h-6"/>`)
	RestorePanelIcon  = lineIcon(`<path d="M3 9h6V3m6 0v6h6M3 15h6v6m6 0v-6h6"/>`)
	DetachPanelIcon   = lineIcon(`<path d="M7 4H3v17h17v-4"/><rect x="8" y="3" width="13" height="13" rx="1"/>`)
	SplitTerminalIcon = lineIcon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M12 4v16m-6-9 2 1-2 1m9-2 2 1-2 1"/>`)
	TransfersIcon     = lineIcon(`<path d="M8 3v12m-4-4 4 4 4-4M16 15V3m-4 4 4-4 4 4M4 19v2h16v-2"/>`)
	FollowIcon        = lineIcon(`<path d="M10 13a5 5 0 0 0 7.07 0l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71M14 11a5 5 0 0 0-7.07 0l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>`)
	RefreshIcon       = lineIcon(`<path d="M20 7v5h-5M4 17v-5h5M6.1 6.1a8 8 0 0 1 13.4 3M4.5 14.9a8 8 0 0 0 13.4 3"/>`)
	ChevronRightIcon  = lineIcon(`<path d="m9 6 6 6-6 6"/>`)
	ChevronDownIcon   = lineIcon(`<path d="m6 9 6 6 6-6"/>`)
	FolderIcon        = lineIcon(`<path d="M3 7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v10H3z" fill="currentColor" fill-opacity=".08"/><path d="M3 10h18"/>`)
	FolderOpenIcon    = lineIcon(`<path d="M3 17V7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v1M3 19l3-8h16l-3 8z" fill="currentColor" fill-opacity=".08"/>`)
	FileIcon          = lineIcon(`<path d="M14 3H5v18h14V8zM14 3v5h5M8 12h8M8 16h6"/>`)
	CodeFileIcon      = lineIcon(`<path d="M14 3H5v18h14V8zM14 3v5h5m-9 4-2 2 2 2m4-4 2 2-2 2"/>`)
	ImageFileIcon     = lineIcon(`<path d="M14 3H5v18h14V8zM14 3v5h5M7 18l4-4 2 2 2-2 2 4"/><circle cx="9" cy="11" r="1"/>`)
	ArchiveFileIcon   = lineIcon(`<path d="M14 3H5v18h14V8zM14 3v5h5M10 4v2m2 0v2m-2 0v2m2 0v2m-2 1h3v4h-3z"/>`)
)

func SVGButton(c *ui.Context, icon *ui.SVG, label string) ui.Element {
	button := Button(c, "", ButtonOptions{Variant: Quiet}).Size(30, 30).
		Padding(0).Label(label).Tooltip(label)
	button.Children(func() { ui.Icon(c, icon).Size(17, 17) })
	return button
}
