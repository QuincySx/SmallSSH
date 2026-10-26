package components

import (
	"math"

	"github.com/egoist/mygo/ui"
)

type SplitLimits struct {
	Min, Max, Default float32
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
	var split ui.Element
	if vertical {
		split = ui.SplitVerticalPercent(c, share, first, second)
	} else {
		split = ui.SplitPercent(c, share, first, second)
	}
	if clamped := limits.Clamp(*share); clamped != *share {
		*share = clamped
		c.Invalidate()
	}
	return split
}
