package monitor

import (
	"math"
	"slices"
	"strconv"
	"strings"
)

// ComposeProject describes observed containers, not the services declared in a
// configuration file. Removed and never-created services cannot be inferred.
type ComposeProject struct {
	Name                                        string
	Containers                                  []Container
	Services, Running, Faults, Starting, Paused int
	Unknown, ResourceContainers                 int
	CPU                                         float64
	Memory                                      uint64
	CPUSamples, MemorySamples                   int
}

func ComposeProjectName(row Container) string {
	if row.Project != "" {
		return row.Project
	}
	for _, label := range strings.Split(row.Labels, ",") {
		key, value, ok := strings.Cut(label, "=")
		if ok && key == "com.docker.compose.project" {
			return value
		}
	}
	return ""
}

// ContainerCondition uses the engine's state and Docker's status suffixes.
// Running alone never proves that an application is healthy.
func ContainerCondition(row Container) string {
	switch row.State {
	case "running":
		status := strings.ToLower(row.Status)
		switch {
		case strings.Contains(status, "(unhealthy)"):
			return "unhealthy"
		case strings.Contains(status, "(health: starting)"):
			return "starting"
		case strings.Contains(status, "(healthy)"):
			return "healthy"
		default:
			return "running"
		}
	case "exited":
		if text, ok := strings.CutPrefix(row.Status, "Exited ("); ok {
			code, _, _ := strings.Cut(text, ")")
			if n, err := strconv.Atoi(code); err == nil && n != 0 {
				return "failed"
			}
		}
		return "stopped"
	case "created", "paused", "restarting", "dead", "removing":
		return row.State
	}
	return "unknown"
}

func (p ComposeProject) State() string {
	switch {
	case p.Faults > 0:
		return "attention"
	case p.Unknown > 0:
		return "unknown"
	case p.Starting > 0:
		return "starting"
	case p.Running == len(p.Containers):
		return "running"
	case p.Running > 0:
		return "partial"
	case p.Paused == len(p.Containers):
		return "paused"
	case p.Paused > 0:
		return "partial"
	default:
		return "stopped"
	}
}

func ComposeProjects(rows []Container, metrics map[string]ContainerStats) []ComposeProject {
	groups := map[string][]Container{}
	seen := map[string]bool{}
	for _, row := range rows {
		name := ComposeProjectName(row)
		if name == "" || row.ID == "" || seen[row.ID] {
			continue
		}
		seen[row.ID] = true
		groups[name] = append(groups[name], row)
	}
	projects := make([]ComposeProject, 0, len(groups))
	for name, containers := range groups {
		p := ComposeProject{Name: name, Containers: containers}
		services := map[string]bool{}
		for _, row := range containers {
			if row.Service != "" {
				services[row.Service] = true
			}
			condition := ContainerCondition(row)
			switch condition {
			case "unhealthy", "failed", "dead", "restarting":
				p.Faults++
			case "starting":
				p.Starting++
			case "paused":
				p.Paused++
			case "unknown", "removing":
				p.Unknown++
			}
			if row.State == "running" {
				p.Running++
			}
			if row.State != "running" && row.State != "paused" && row.State != "restarting" {
				continue
			}
			p.ResourceContainers++
			if row.State == "restarting" {
				continue
			}
			m := metrics[row.ID]
			if m.CPUValid && m.CPU >= 0 && !math.IsNaN(m.CPU) && !math.IsInf(m.CPU, 0) {
				p.CPU += m.CPU
				p.CPUSamples++
			}
			if m.MemoryValid {
				p.Memory += m.Memory
				p.MemorySamples++
			}
		}
		p.Services = len(services)
		slices.SortFunc(p.Containers, func(a, b Container) int {
			if order := conditionPriority(ContainerCondition(a)) - conditionPriority(ContainerCondition(b)); order != 0 {
				return order
			}
			if order := strings.Compare(a.Service, b.Service); order != 0 {
				return order
			}
			if order := strings.Compare(a.Names, b.Names); order != 0 {
				return order
			}
			return strings.Compare(a.ID, b.ID)
		})
		projects = append(projects, p)
	}
	slices.SortFunc(projects, func(a, b ComposeProject) int { return strings.Compare(a.Name, b.Name) })
	return projects
}

func conditionPriority(state string) int {
	switch state {
	case "unhealthy", "failed", "dead", "restarting":
		return 0
	case "starting", "unknown", "removing":
		return 1
	case "paused", "created", "stopped":
		return 2
	default:
		return 3
	}
}
