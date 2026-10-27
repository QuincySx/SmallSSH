package desktop

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"sshclient/internal/localipc"
)

// This file contains layout only: no host names, command text or credentials.
type layoutPreferences struct {
	Version            int     `json:"version"`
	Right              bool    `json:"right"`
	Hidden             bool    `json:"hidden"`
	Tab                int     `json:"tab"`
	TerminalShare      float32 `json:"terminalShare"`
	TerminalWidthShare float32 `json:"terminalWidthShare"`
	Sidebar            float32 `json:"sidebar"`
	SidebarHidden      bool    `json:"sidebarHidden,omitempty"`
	LocaleChoice       string  `json:"localeChoice,omitempty"`
	// Zoom is the content zoom of the main window, 1 for 100%. Layout only: it
	// says how large the interface is drawn, nothing about what is in it.
	Zoom float32 `json:"zoom,omitempty"`
}

func (a *app) loadLayout() {
	f, e := os.Open(filepath.Join(a.dir, "layout.json"))
	if e != nil {
		return
	}
	defer f.Close()
	var p layoutPreferences
	if json.NewDecoder(io.LimitReader(f, 8193)).Decode(&p) != nil || p.Version < 1 || p.Version > 3 {
		return
	}
	a.toolsRight, a.toolsHidden = p.Right, p.Hidden
	if p.Version == 1 {
		// The preview's previous three-column preset used a right dock. Move
		// that layout to the standard terminal-above-tools workspace once.
		a.toolsRight = false
	}
	a.sidebarHidden = p.SidebarHidden
	a.zoom = clampZoom(p.Zoom)
	a.languageChoice = languageChoiceOrSystem(p.LocaleChoice)
	if p.Tab >= 0 && p.Tab <= toolDiagnostics {
		a.dock = p.Tab
		if a.dock == toolTransfers {
			a.dock = toolFiles
		}
	}
	if p.TerminalShare >= 30 && p.TerminalShare <= 90 {
		a.terminalShare = p.TerminalShare
	}
	// Narrow windows can legitimately fit the dock with less than 35% left
	// for the terminal. Restore that fitted value instead of resetting it.
	if p.TerminalWidthShare > 0 && p.TerminalWidthShare <= 80 {
		a.terminalWidthShare = p.TerminalWidthShare
	}
	if p.Sidebar >= 140 && p.Sidebar <= 400 {
		a.sidebarWidth = p.Sidebar
	}
}

func (a *app) saveLayout() error {
	if e := localipc.Prepare(a.dir); e != nil {
		return e
	}
	p := layoutPreferences{Version: 3, Right: a.toolsRight, Hidden: a.toolsHidden, Tab: a.dock,
		TerminalShare: a.terminalShare, TerminalWidthShare: a.terminalWidthShare, Sidebar: a.sidebarWidth, SidebarHidden: a.sidebarHidden, LocaleChoice: a.languageChoice,
		Zoom: a.viewZoom()}
	f, e := os.CreateTemp(a.dir, ".layout-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	e = json.NewEncoder(f).Encode(p)
	closed := f.Close()
	if e != nil {
		return e
	}
	if closed != nil {
		return closed
	}
	return os.Rename(name, filepath.Join(a.dir, "layout.json"))
}
