package desktop

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/monitor"
)

const statusHistoryLimit = 451 // Fifteen minutes at two seconds, including both endpoints.

type networkRate struct {
	name   string
	rx, tx float64
	valid  bool
}
type statusPoint struct {
	at                              time.Time
	cpu, user, system, wait, memory float64
	read, write                     float64
	cpuValid, ioValid               bool
	networks                        []networkRate
}
type statusHistory struct{ points []statusPoint }

func (h *statusHistory) add(s *monitor.Stats) {
	if s == nil || s.Time.IsZero() {
		return
	}
	if n := len(h.points); n > 0 && !s.Time.After(h.points[n-1].at) {
		return
	}
	p := statusPoint{at: s.Time, cpu: s.CPU, user: s.CPUDetail.User, system: s.CPUDetail.System, wait: s.CPUDetail.Wait, memory: percentage(s.MemoryUsed, s.MemoryTotal), cpuValid: s.CPUDetail.Valid}
	for _, b := range s.BlockIO {
		p.read += b.ReadRate
		p.write += b.WriteRate
		p.ioValid = p.ioValid || b.Valid
	}
	for _, n := range s.Networks {
		p.networks = append(p.networks, networkRate{n.Name, n.RXRate, n.TXRate, n.Valid})
	}
	// A bounded compact history never retains process lists, inventory, secrets,
	// or terminal output. Live tabs alias one collector-owned history.
	if len(h.points) == statusHistoryLimit {
		copy(h.points, h.points[1:])
		h.points = h.points[:statusHistoryLimit-1]
	}
	h.points = append(h.points, p)
	cutoff := s.Time.Add(-15 * time.Minute)
	start := 0
	for start < len(h.points)-1 && h.points[start].at.Before(cutoff) {
		start++
	}
	if start > 0 {
		copy(h.points, h.points[start:])
		h.points = h.points[:len(h.points)-start]
	}
}

func percentage(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return min(100, float64(used)/float64(total)*100)
}
func rateSize(value float64) string {
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return "—"
	}
	return size(uint64(value)) + "/s"
}
func (a *app) statusWindow() time.Duration {
	switch a.statusRange {
	case "1 分钟":
		return time.Minute
	case "15 分钟":
		return 15 * time.Minute
	}
	return 5 * time.Minute
}

type trendSeries struct {
	name   string
	color  ui.Color
	value  func(statusPoint) (float64, bool)
	format func(float64) string
}

func cpuSeries(c *ui.Context) []trendSeries {
	return []trendSeries{{"CPU", c.Theme().Accent, func(p statusPoint) (float64, bool) { return p.cpu, p.cpuValid }, percentText}}
}
func memorySeries(c *ui.Context) []trendSeries {
	return []trendSeries{{tr(c, "内存"), ui.Hex("#8b5cf6"), func(p statusPoint) (float64, bool) { return p.memory, true }, percentText}}
}
func ioSeries(c *ui.Context) []trendSeries {
	return []trendSeries{{tr(c, "读取"), c.Theme().Accent, func(p statusPoint) (float64, bool) { return p.read, p.ioValid }, rateSize}, {tr(c, "写入"), ui.Hex("#f59e0b"), func(p statusPoint) (float64, bool) { return p.write, p.ioValid }, rateSize}}
}
func networkSeries(c *ui.Context, name string) []trendSeries {
	value := func(tx bool) func(statusPoint) (float64, bool) {
		return func(p statusPoint) (float64, bool) {
			for _, n := range p.networks {
				if n.name == name {
					if tx {
						return n.tx, n.valid
					}
					return n.rx, n.valid
				}
			}
			return 0, false
		}
	}
	return []trendSeries{{tr(c, "接收"), c.Theme().Accent, value(false), rateSize}, {tr(c, "发送"), ui.Hex("#14b8a6"), value(true), rateSize}}
}
func percentText(v float64) string { return fmt.Sprintf("%.1f%%", v) }

// Charts use timestamps rather than sample indexes. Missing probes and reset
// counters break the line instead of inventing zeroes or joining outages.
func trendChart(c *ui.Context, label string, history *statusHistory, window time.Duration, series []trendSeries, ceiling float64, height float32, compact bool) {
	theme := *c.Theme()
	chart := ui.Box(c).FillWidth().Height(height).Shrink(0).Label(label)
	var pointerX float32
	var hover bool
	if !compact {
		pointerX, _, hover = chart.PointerPosition()
	} else {
		chart.PassThrough()
	}
	chart.Draw(func(p *ui.Painter, r ui.Rect) {
		points := history.points
		left, bottom := float32(44), float32(22)
		if compact {
			left, bottom = 0, 2
		}
		plot := ui.Rect{X: r.X + left, Y: r.Y + 7, W: max(1, r.W-left-5), H: max(1, r.H-bottom-9)}
		upper := ceiling
		end := time.Now()
		if len(points) > 0 {
			end = points[len(points)-1].at
		}
		start := end.Add(-window)
		for _, point := range points {
			if point.at.Before(start) {
				continue
			}
			for _, s := range series {
				v, ok := s.value(point)
				if ok && isFinite(v) {
					upper = max(upper, v)
				}
			}
		}
		if upper <= 0 {
			upper = 1024
		}
		if ceiling == 0 {
			upper = math.Pow(2, math.Ceil(math.Log2(upper)))
		}
		if !compact {
			for j := 0; j < 3; j++ {
				y := plot.Y + plot.H*float32(j)/2
				p.Line(plot.X, y, plot.X+plot.W, y, 1, theme.Border.Alpha(.6))
				v := upper * (1 - float64(j)/2)
				text := size(uint64(v))
				if ceiling == 100 {
					text = fmt.Sprintf("%.0f%%", v)
				}
				p.Text(r.X+1, y-5, text, 9, theme.TextMuted)
			}
			p.Text(plot.X, plot.Y+plot.H+6, start.Local().Format("15:04:05"), 9, theme.TextMuted)
			p.Text(plot.X+plot.W-45, plot.Y+plot.H+6, end.Local().Format("15:04:05"), 9, theme.TextMuted)
		} else {
			p.Line(plot.X, plot.Y+plot.H, plot.X+plot.W, plot.Y+plot.H, 1, theme.Border.Alpha(.5))
		}
		validPoints := 0
		for _, s := range series {
			var path, area ui.Path
			drawing := false
			var last time.Time
			var firstX, lastX float32
			flush := func() {
				if !drawing {
					return
				}
				area.LineTo(lastX, plot.Y+plot.H).LineTo(firstX, plot.Y+plot.H).Close()
				p.FillPath(&area, s.color.Alpha(.09))
				p.StrokePath(&path, 1.7, s.color)
				path.Reset()
				area.Reset()
				drawing = false
			}
			for _, point := range points {
				v, ok := s.value(point)
				if point.at.Before(start) || !ok || !isFinite(v) {
					flush()
					continue
				}
				x := plot.X + float32(point.at.Sub(start).Seconds()/window.Seconds())*plot.W
				y := plot.Y + plot.H*(1-float32(max(0, min(upper, v))/upper))
				if drawing && point.at.Sub(last) > 8*time.Second {
					flush()
				}
				if !drawing {
					path.MoveTo(x, y)
					area.MoveTo(x, plot.Y+plot.H).LineTo(x, y)
					firstX = x
					drawing = true
				} else {
					path.LineTo(x, y)
					area.LineTo(x, y)
				}
				lastX, last = x, point.at
				validPoints++
			}
			flush()
		}
		if validPoints < 2 && !compact {
			p.Text(plot.X+8, plot.Y+plot.H/2-6, tr(c, "等待连续采样…"), 11, theme.TextMuted)
		}
		if !hover || compact || len(points) == 0 {
			return
		}
		x := max(plot.X, min(plot.X+plot.W, r.X+pointerX))
		target := start.Add(time.Duration(float64(x-plot.X) / float64(plot.W) * float64(window)))
		index := nearestStatusPoint(points, target)
		if index < 0 || points[index].at.Before(start) {
			return
		}
		point := points[index]
		x = plot.X + float32(point.at.Sub(start).Seconds()/window.Seconds())*plot.W
		p.Line(x, plot.Y, x, plot.Y+plot.H, 1, theme.TextMuted.Alpha(.7))
		width := float32(164)
		box := ui.Rect{X: max(r.X, min(x+8, r.X+r.W-width)), Y: plot.Y + 3, W: width, H: float32(26 + len(series)*17)}
		p.Fill(box, theme.Surface, 4)
		p.Text(box.X+9, box.Y+7, point.at.Local().Format("15:04:05"), 10, theme.TextMuted)
		for j, s := range series {
			v, ok := s.value(point)
			text := "—"
			if ok && isFinite(v) {
				text = s.format(v)
			}
			p.Text(box.X+9, box.Y+25+float32(j)*17, s.name+"  "+text, 11, s.color)
		}
	})
}

func nearestStatusPoint(points []statusPoint, at time.Time) int {
	if len(points) == 0 {
		return -1
	}
	lo, hi := 0, len(points)
	for lo < hi {
		mid := (lo + hi) / 2
		if points[mid].at.Before(at) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == len(points) {
		return lo - 1
	}
	if lo > 0 && at.Sub(points[lo-1].at) < points[lo].at.Sub(at) {
		return lo - 1
	}
	return lo
}
func isFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
