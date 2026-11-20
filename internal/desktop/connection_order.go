package desktop

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/vault"
)

type connectionDrag struct {
	Revision, ID, Group string
	Header              bool
}

// Drop targets are stable IDs, never indexes from a filtered or folded view.
// Moving a group includes its hidden members; the model never edits secrets.
func connectionOrderDrop(rows []vault.Summary, drag connectionDrag, target connectionRailRow, after bool) (vault.ConnectionOrder, error) {
	version := vault.ConnectionOrderRevision(rows)
	if drag.Revision != version {
		return vault.ConnectionOrder{}, errors.New("连接列表已更新，请重试拖动")
	}
	if !drag.Header && !target.header && drag.ID == target.connection.ID {
		unchanged := vault.ConnectionOrder{Revision: version}
		for _, r := range rows {
			unchanged.Items = append(unchanged.Items, vault.ConnectionPlacement{ID: r.ID, Group: r.Group})
		}
		return unchanged, nil
	}
	groups := []string{}
	members := map[string][]vault.Summary{}
	for _, row := range rows {
		if _, ok := members[row.Group]; !ok {
			groups = append(groups, row.Group)
		}
		members[row.Group] = append(members[row.Group], row)
	}
	if _, ok := members[target.group]; !ok {
		return vault.ConnectionOrder{}, errors.New("目标分组不存在")
	}
	if drag.Header {
		from, to := slices.Index(groups, drag.Group), slices.Index(groups, target.group)
		if from < 0 || to < 0 {
			return vault.ConnectionOrder{}, errors.New("分组不存在")
		}
		if from != to {
			groups = append(groups[:from:from], groups[from+1:]...)
			to = slices.Index(groups, target.group)
			if after {
				to++
			}
			groups = slices.Insert(groups, to, drag.Group)
		}
	} else {
		var moved vault.Summary
		found := false
		for group, items := range members {
			for i, row := range items {
				if row.ID == drag.ID {
					moved = row
					found = true
					members[group] = slices.Delete(slices.Clone(items), i, i+1)
					break
				}
			}
		}
		if !found {
			return vault.ConnectionOrder{}, errors.New("连接不存在")
		}
		moved.Group = target.group
		items := members[target.group]
		at := len(items)
		if !target.header {
			at = slices.IndexFunc(items, func(row vault.Summary) bool { return row.ID == target.connection.ID })
			if at < 0 {
				if target.connection.ID == drag.ID {
					at = len(items)
				} else {
					return vault.ConnectionOrder{}, errors.New("目标连接不存在")
				}
			}
			if after && at < len(items) {
				at++
			}
		}
		members[target.group] = slices.Insert(items, at, moved)
	}
	change := vault.ConnectionOrder{Revision: version}
	for _, group := range groups {
		for _, row := range members[group] {
			change.Items = append(change.Items, vault.ConnectionPlacement{ID: row.ID, Group: row.Group})
		}
	}
	return change, nil
}

func (a *app) connectionDropTarget(c *ui.Context, item ui.Element, row connectionRailRow) {
	if a.connectionRailCache.pending != nil || a.page == 0 {
		return
	}
	_, hover := ui.DragOver[connectionDrag](item)
	drag, dropped := ui.Drop[connectionDrag](item)
	after := false
	if hover || dropped {
		_, y, _ := item.PointerPosition()
		after = y > item.Bounds().H/2
	}
	if hover {
		accent := c.Theme().Accent
		item.DrawOver(func(p *ui.Painter, r ui.Rect) {
			if row.header {
				p.Stroke(r, accent, 4, 1.5)
				return
			}
			line := r.Y + 1
			if after {
				line = r.Y + r.H - 1
			}
			p.Line(r.X, line, r.X+r.W, line, 2, accent)
		})
	}
	if dropped {
		change, err := connectionOrderDrop(a.connections, drag, row, after)
		if err != nil {
			a.status = tr(c, err.Error())
			return
		}
		if a.saveConnectionOrder(change) {
			if open := a.groups[row.group]; !drag.Header && open != nil {
				*open = true
			}
			c.Invalidate()
		}
	}
}

// The preview updates only the sidebar. Other actions still use the committed
// connections while the owner write is pending.
func (a *app) saveConnectionOrder(change vault.ConnectionOrder) bool {
	if a.connectionRailCache.pending != nil || !a.online {
		return false
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		a.status = err.Error()
		return false
	}
	next := orderedConnections(a.connections, change.Items)
	if vault.ConnectionOrderRevision(next) == change.Revision {
		return false
	}
	a.connectionRailCache.pending = next
	gen, cli := a.generation, a.client
	ctx, cancel := context.WithTimeout(a.operationContext(), 5*time.Second)
	go func() {
		defer cancel()
		err := cli.SaveConnectionOrder(ctx, change, owner)
		a.postUI(func() {
			if a.generation != gen {
				return
			}
			// Ignore snapshots requested before this write completed, even if
			// their UI callback runs after the save acknowledgement.
			a.connectionOrderVersion.Add(1)
			a.connectionRailCache.pending = nil
			if err != nil {
				a.status = err.Error()
			} else if vault.ConnectionOrderRevision(a.connections) == change.Revision {
				a.connections = orderedConnections(a.connections, change.Items)
			}
			a.wakePoll()
		})
	}()
	return true
}

func orderedConnections(rows []vault.Summary, placements []vault.ConnectionPlacement) []vault.Summary {
	byID := make(map[string]vault.Summary, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	next := make([]vault.Summary, 0, len(placements))
	for _, placement := range placements {
		r := byID[placement.ID]
		r.Group = placement.Group
		next = append(next, r)
	}
	return next
}
