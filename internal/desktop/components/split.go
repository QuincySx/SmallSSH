package components

import (
	"math"

	"github.com/egoist/mygo/ui"
)

type SplitLimits struct {
	Min, Max, Default float32
}

// Split keeps the native resize handle and its hover/focus feedback, without
// drawing a permanent rule between the panes.
func Split(c *ui.Context, size *float32, first, second func()) ui.Element {
	return borderlessSplit(c, ui.Split, size, first, second)
}

func borderlessSplit(c *ui.Context, build func(*ui.Context, *float32, func(), func()) ui.Element, size *float32, first, second func()) ui.Element {
	original := c.Theme()
	chrome := *original
	chrome.Border = ui.Transparent
	c.SetTheme(&chrome)
	defer c.SetTheme(original)
	pane := func(draw func()) func() {
		return func() {
			c.SetTheme(original)
			defer c.SetTheme(&chrome)
			draw()
		}
	}
	return build(c, size, pane(first), pane(second))
}

func (l SplitLimits) Clamp(value float32) float32 {
	if value <= 0 || math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
		value = l.Default
	}
	return max(l.Min, min(l.Max, value))
}

// Keep native pointer capture, keyboard resizing and focus handling. Clamp on
// both sides of the native input pass: crossing a bound must never restore the
// initial layout, nor leave an out-of-range value for the next drag increment.
func SplitPercent(c *ui.Context, share *float32, limits SplitLimits, first, second func()) ui.Element {
	return boundedSplit(c, share, limits, first, second, false)
}

func SplitVerticalPercent(c *ui.Context, share *float32, limits SplitLimits, first, second func()) ui.Element {
	return boundedSplit(c, share, limits, first, second, true)
}

func boundedSplit(c *ui.Context, share *float32, limits SplitLimits, first, second func(), vertical bool) ui.Element {
	*share = limits.Clamp(*share)
	build := ui.SplitPercent
	if vertical {
		build = ui.SplitVerticalPercent
	}
	split := borderlessSplit(c, build, share, first, second)
	if clamped := limits.Clamp(*share); clamped != *share {
		*share = clamped
		c.Invalidate()
	}
	return split
}
