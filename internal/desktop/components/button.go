package components

import "github.com/egoist/mygo/ui"

type ButtonVariant uint8

const (
	Secondary ButtonVariant = iota
	Primary
	Destructive
	Quiet
)

type ButtonOptions struct {
	Variant  ButtonVariant
	Disabled bool
	Busy     bool
}

// Button keeps MyGo's native focus, keyboard activation, accessibility and
// pointer states. Busy actions are disabled, so they cannot be submitted twice.
func Button(c *ui.Context, label string, options ...ButtonOptions) ui.Element {
	var o ButtonOptions
	if len(options) > 0 {
		o = options[0]
	}
	original := c.Theme()
	theme := *original
	theme.Surface = theme.Background.Mix(theme.Surface, .45)
	theme.Border = ui.Transparent
	switch o.Variant {
	case Destructive:
		theme.Accent = theme.Danger
		theme.AccentHover = theme.Danger.Mix(ui.Hex("#000000"), .12)
		theme.AccentPressed = theme.Danger.Mix(ui.Hex("#000000"), .24)
		theme.AccentText = ui.Hex("#ffffff")
		if theme.Dark {
			theme.AccentText = ui.Hex("#35131b")
		}
	case Quiet:
		theme.Surface, theme.Border = ui.Transparent, ui.Transparent
	}
	c.SetTheme(&theme)
	text := label
	if o.Busy {
		text = ""
	}
	var button ui.Element
	if o.Variant == Primary || o.Variant == Destructive {
		button = ui.PrimaryButton(c, text)
	} else {
		button = ui.Button(c, text)
	}
	button.Height(ControlHeight).Radius(8).Disabled(o.Disabled || o.Busy).Label(label)
	if o.Busy {
		button.Children(func() {
			ui.Spinner(c).Size(12, 12)
			ui.Text(c, label).SingleLine()
		})
	}
	c.SetTheme(original)
	return button
}

func PrimaryButton(c *ui.Context, label string) ui.Element {
	return Button(c, label, ButtonOptions{Variant: Primary})
}

func IconButton(c *ui.Context, glyph, label string) ui.Element {
	return Button(c, glyph, ButtonOptions{Variant: Quiet}).Size(IconSize, IconSize).
		Padding(0).FontSize(16).Label(label).Tooltip(label)
}

// ActionGroup keeps dialog actions together at the end. Narrow dialogs wrap
// the group rather than clipping long action labels or pushing controls out.
func ActionGroup(c *ui.Context, align ui.Align, children func()) ui.Element {
	row := ui.Row(c).FillWidth().Gap(ActionGap).AlignItems(ui.Center)
	if align == ui.End {
		row.Justify(ui.End).Wrap()
	}
	return row.Children(children)
}

// ButtonGroup provides native toolbar arrow-key navigation between actions.
func ButtonGroup(c *ui.Context, children func()) ui.Element {
	return ui.Toolbar(c, children).Gap(4)
}
