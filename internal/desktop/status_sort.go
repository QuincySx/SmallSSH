package desktop

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/monitor"
)

func compareText(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) }

func sortProcesses(items []monitor.Process, order ui.SortOrder, totalMemory uint64) {
	slices.SortStableFunc(items, func(a, b monitor.Process) int {
		var result int
		switch order.Column {
		case "pid":
			result = cmp.Compare(a.PID, b.PID)
		case "cpu":
			result = cmp.Compare(a.CPU, b.CPU)
		case "memory":
			result = cmp.Compare(processMemory(a, totalMemory), processMemory(b, totalMemory))
		case "user":
			result = compareText(a.User, b.User)
		case "state":
			result = compareText(a.State, b.State)
		case "elapsed":
			left, leftOK := elapsedSeconds(a.Elapsed)
			right, rightOK := elapsedSeconds(b.Elapsed)
			// Unknown durations stay below known values in either direction.
			if leftOK != rightOK {
				if leftOK {
					return -1
				}
				return 1
			}
			result = cmp.Compare(left, right)
		default:
			result = compareText(a.Name, b.Name)
		}
		if order.Descending {
			result = -result
		}
		if result != 0 {
			return result
		}
		// Refreshes can arrive in a different order. Resolve ties by stable
		// identity so equally busy processes do not jump around each sample.
		if result = compareText(a.Name, b.Name); result != 0 {
			return result
		}
		return cmp.Compare(a.PID, b.PID)
	})
}

func processMemory(p monitor.Process, total uint64) float64 {
	if p.RSS > 0 {
		return float64(p.RSS)
	}
	return p.Memory * float64(total) / 100
}

func sortServices(items []monitor.Service, order ui.SortOrder) {
	slices.SortStableFunc(items, func(a, b monitor.Service) int {
		var result int
		switch order.Column {
		case "state":
			result = compareText(serviceState(a.Active, a.Sub), serviceState(b.Active, b.Sub))
		case "enabled":
			result = compareText(serviceEnabled(a.Enabled), serviceEnabled(b.Enabled))
		default:
			result = compareText(a.Name, b.Name)
		}
		if order.Descending {
			result = -result
		}
		if result != 0 {
			return result
		}
		return compareText(a.Name, b.Name)
	})
}

// ps etime is [[DD-]HH:]MM:SS. Parsing once per comparison keeps hours and
// days numeric instead of putting e.g. 01:00:00 ahead of 59:59 as strings.
func elapsedSeconds(text string) (uint64, bool) {
	if text == "" || text == "—" || text == "-" {
		return 0, false
	}
	var days uint64
	if d, rest, ok := strings.Cut(text, "-"); ok {
		value, err := strconv.ParseUint(d, 10, 64)
		if err != nil || value > ^uint64(0)/86400 {
			return 0, false
		}
		days = value * 86400
		text = rest
	}
	parts := strings.Split(text, ":")
	if len(parts) < 1 || len(parts) > 3 {
		return 0, false
	}
	var seconds uint64
	for j, part := range parts {
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil || j > 0 && value >= 60 || seconds > (^uint64(0)-value)/60 {
			return 0, false
		}
		seconds = seconds*60 + value
	}
	if seconds > ^uint64(0)-days {
		return 0, false
	}
	return days + seconds, true
}

func serviceEnabled(value string) string {
	switch value {
	case "enabled":
		return tr(nil, "持久")
	case "enabled-runtime":
		return tr(nil, "本次启动")
	case "disabled":
		return tr(nil, "已关闭")
	case "static":
		return tr(nil, "按需启动")
	case "masked", "masked-runtime":
		return tr(nil, "已屏蔽")
	case "indirect":
		return tr(nil, "间接启用")
	case "alias":
		return tr(nil, "别名")
	default:
		return tr(nil, "未读取")
	}
}
