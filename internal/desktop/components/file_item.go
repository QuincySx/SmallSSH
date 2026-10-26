package components

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// Shared metrics keep a directory row and a table row on the same rhythm.
const (
	FileRowHeight    = 26
	FileIconSize     = 16
	FileItemGap      = 6
	TreeIndent       = 14
	FileHeaderHeight = 36
)

// fileViewTheme is scoped to a tree/table. Selected rows remain readable in
// either appearance without giving secondary text a white-on-blue override.
func fileViewTheme(c *ui.Context) func() {
	original := c.Theme()
	t := *original
	t.Spacing, t.FontSize, t.Radius = FileRowHeight/8.0, BodyFont, 4
	amount := float32(.89)
	if t.Dark {
		amount = .76
	}
	t.Accent = original.Accent.Mix(original.Background, amount)
	t.AccentText = original.Text
	c.SetTheme(&t)
	return func() { c.SetTheme(original) }
}

// FileItemIcon is deliberately small: recognizable shapes, no platform font
// glyphs or per-extension assets. It never inspects a path or reads a file.
func FileItemIcon(name string, directory, expanded bool) *ui.SVG {
	if directory {
		if expanded {
			return FolderOpenIcon
		}
		return FolderIcon
	}
	name = strings.ToLower(name)
	at := strings.LastIndexByte(name, '.')
	if at >= 0 {
		switch name[at:] {
		case ".sh", ".bash", ".zsh", ".go", ".js", ".ts", ".jsx", ".tsx", ".py", ".rs", ".c", ".h", ".html", ".css", ".json", ".toml", ".yaml", ".yml", ".conf", ".ini", ".xml":
			return CodeFileIcon
		case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".svg", ".ico":
			return ImageFileIcon
		case ".zip", ".gz", ".bz2", ".xz", ".tar", ".7z", ".rar":
			return ArchiveFileIcon
		}
	}
	return FileIcon
}

type FileNameOptions struct {
	Expanded bool
	Selected bool
}

// FileName is the common icon/name cell for the tree and the file list.
// It stays passive so the row owns selection, context menus and drag/drop.
func FileName(c *ui.Context, f FileEntry, options ...FileNameOptions) ui.Element {
	var o FileNameOptions
	if len(options) > 0 {
		o = options[0]
	}
	return ui.Row(c).Grow(1).MinWidth(0).Gap(FileItemGap).AlignItems(ui.Center).Children(func() {
		color := c.Theme().TextMuted
		if o.Selected {
			color = c.Theme().Text
		}
		ui.Icon(c, FileItemIcon(f.Name, f.Directory, o.Expanded)).Size(FileIconSize, FileIconSize).Shrink(0).TextColor(color)
		name := ui.Text(c, f.Name).Grow(1).MinWidth(0).SingleLine().FontSize(BodyFont).Tooltip(f.Name)
		if strings.HasPrefix(f.Name, ".") && !o.Selected {
			name.TextColor(c.Theme().TextMuted)
		}
	})
}
