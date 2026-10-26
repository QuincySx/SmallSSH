package components

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

// Navigation geometry is shared by full sidebars and their compact rails.
const (
	RailWidth              float32 = 48
	NavigationItemHeight   float32 = 40
	NavigationItemGap      float32 = 4
	NavigationIconSlotSize float32 = 36
	NavigationTextGap      float32 = 6
	NavigationCircleSize   float32 = 28
	NavigationGlyphSize    float32 = 16
)

// NavigationSidebar changes only its width between modes. The scroll viewport,
// focus group and item parents keep their identities and vertical geometry.
func NavigationSidebar(c *ui.Context, key, label string, expandedWidth float32, compact bool, children func()) ui.Element {
	width := expandedWidth
	if compact {
		width = RailWidth
	}
	c.Key(key)
	return ui.Column(c).Width(width).FillHeight().Shrink(0).MinHeight(0).
		Background(c.Theme().Surface).BorderWidth(0, 1, 0, 0).
		BorderColor(c.Theme().Border).Label(label).Children(func() {
		// Scroll reads its identity during construction, before its children.
		c.Key("navigation-sidebar-scroll:" + key)
		ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Label(label + tr(c, "滚动区")).Children(func() {
			c.Key("navigation-sidebar-items:" + key)
			ui.Column(c).FillWidth().Padding(10, 6, 10, 6).Gap(NavigationItemGap).
				FocusGroup(ui.Vertical).Children(children)
		})
	})
}

// NavigationItem keeps the icon in a fixed left slot. Expanding appends a text
// sibling without replacing the button, icon subtree or keyboard focus target.
func NavigationItem(c *ui.Context, key any, label string, selected, compact bool, content func()) ui.Element {
	t := c.Theme()
	c.Key(key)
	item := ui.ButtonBase(c).Height(NavigationItemHeight).Shrink(0).Padding(0).Radius(10).
		Justify(ui.Start).AlignItems(ui.Center).Label(label).Tooltip(label).
		Selected(selected).FocusRing(false)
	if compact {
		item.Width(NavigationIconSlotSize)
	} else {
		item.FillWidth()
	}
	background := ui.Transparent
	if selected {
		background = t.Accent.Alpha(.16)
	}
	if item.Pressed() {
		background = t.SurfacePressed
	} else if item.Hovered() {
		background = t.SurfaceHover
	}
	color := t.Text
	if selected {
		color = t.Accent
	}
	if compact {
		// Fade the existing artwork slightly for interaction states. Do not
		// add a second shape, outline or background around compact icons.
		opacity := float32(1)
		if selected {
			opacity = .90
		}
		if item.Hovered() || item.FocusVisible() {
			opacity -= .06
		}
		if item.Pressed() {
			opacity = .76
		}
		if item.FocusVisible() {
			color = t.Accent
		}
		item.Background(ui.Transparent).TextColor(color).Opacity(opacity)
	} else {
		item.Background(background).TextColor(color)
		// Paint over the fixed geometry so selection never moves the icon.
		item.DrawOver(func(p *ui.Painter, r ui.Rect) {
			if item.FocusVisible() {
				p.Stroke(r, t.Accent, 10, 2)
			} else if selected {
				p.Stroke(r, t.Accent.Alpha(.65), 10, 1)
			}
		})
	}
	return item.Children(func() {
		c.Key("navigation-item-icon-slot")
		ui.Box(c).Size(NavigationIconSlotSize, NavigationIconSlotSize).Shrink(0).
			Justify(ui.Center).AlignItems(ui.Center).Children(content)
		if !compact {
			c.Key("navigation-item-label")
			ui.Text(c, label).Grow(1).MinWidth(0).SingleLine().FontSize(13).MarginStart(NavigationTextGap).MarginEnd(8)
		}
	})
}

// NavigationIcon shares the same circular icon in expanded and compact modes.
func NavigationIcon(c *ui.Context, key any, label string, icon *ui.SVG, selected, compact bool) ui.Element {
	return NavigationItem(c, key, label, selected, compact, func() {
		background := c.Theme().TextMuted.Alpha(.10)
		if selected {
			background = c.Theme().Accent.Alpha(.12)
		}
		c.Key("navigation-item-icon")
		ui.Box(c).Size(NavigationCircleSize, NavigationCircleSize).Shrink(0).Radius(NavigationCircleSize / 2).Background(background).
			Label(label + tr(c, "图标")).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).Size(NavigationGlyphSize, NavigationGlyphSize)
		})
	})
}

// Compact wrappers retain the existing rail API and native button behavior.
func NavigationRail(c *ui.Context, label string, children func()) ui.Element {
	return NavigationSidebar(c, "navigation-rail:"+label, label, RailWidth, true, children)
}

func RailItem(c *ui.Context, key any, label string, selected bool, content func()) ui.Element {
	return NavigationItem(c, key, label, selected, true, content)
}

func RailIcon(c *ui.Context, key any, label string, icon *ui.SVG, selected bool) ui.Element {
	return NavigationIcon(c, key, label, icon, selected, true)
}

const connectionAvatarCacheLimit = 512

// Only digests are retained, never connection names or addresses. A fixed FIFO
// ring bounds the cache; the lock also prevents duplicate parsing on a miss.
var connectionAvatars = struct {
	sync.Mutex
	images map[[sha256.Size]byte]*ui.SVG
	order  [connectionAvatarCacheLimit][sha256.Size]byte
	next   int
}{images: make(map[[sha256.Size]byte]*ui.SVG)}

// ConnectionAvatar renders a stable local identicon. No network or image
// generation is involved, and repeated frames reuse an already parsed vector.
func ConnectionAvatar(c *ui.Context, id string) {
	ui.Box(c).Size(NavigationCircleSize, NavigationCircleSize).Shrink(0).
		Justify(ui.Center).AlignItems(ui.Center).Children(func() {
		ui.Image(c, connectionAvatarSVG(id)).Size(NavigationCircleSize, NavigationCircleSize).Shrink(0)
	})
}

func connectionAvatarSVG(id string) *ui.SVG {
	digest := sha256.Sum256([]byte(id))
	connectionAvatars.Lock()
	defer connectionAvatars.Unlock()
	if image, ok := connectionAvatars.images[digest]; ok {
		return image
	}
	image := ui.MustParseSVG([]byte(connectionAvatarMarkup(digest)))
	if len(connectionAvatars.images) == connectionAvatarCacheLimit {
		delete(connectionAvatars.images, connectionAvatars.order[connectionAvatars.next])
	}
	connectionAvatars.images[digest] = image
	connectionAvatars.order[connectionAvatars.next] = digest
	connectionAvatars.next = (connectionAvatars.next + 1) % connectionAvatarCacheLimit
	return image
}

func connectionAvatarMarkup(digest [sha256.Size]byte) string {
	palette := [...]string{"#4668ce", "#8a52b8", "#17816f", "#b9632d", "#b44869", "#367e9e", "#727c32", "#6551b8"}
	var svg strings.Builder
	fmt.Fprintf(&svg, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 28 28"><circle cx="14" cy="14" r="14" fill="%s"/><g fill="#ffffff" fill-opacity=".9">`, palette[int(digest[31])%len(palette)])
	for y := 0; y < 5; y++ {
		for x := 0; x < 3; x++ {
			bit := y*3 + x
			if digest[bit/8]&(1<<uint(bit%8)) == 0 && (x != 2 || y != 2) {
				continue
			}
			fmt.Fprintf(&svg, `<rect x="%.2f" y="%.2f" width="3.5" height="3.5" rx=".5"/>`, 5.25+float64(x)*3.5, 5.25+float64(y)*3.5)
			if x != 2 {
				fmt.Fprintf(&svg, `<rect x="%.2f" y="%.2f" width="3.5" height="3.5" rx=".5"/>`, 5.25+float64(4-x)*3.5, 5.25+float64(y)*3.5)
			}
		}
	}
	svg.WriteString(`</g></svg>`)
	return svg.String()
}
