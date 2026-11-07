package client

import (
	"context"
	"errors"

	"sshclient/internal/broker"
	"sshclient/internal/protocol"
	"sshclient/internal/snippet"
	"sshclient/internal/vault"
)

const (
	workspaceSnippetPageLimit = 32
	workspaceMaxCommands      = 1024
	workspaceMaxCursorBytes   = 128
)

// ListSnippets reads the owner's saved commands without parameter histories.
// Each page carries its own one-use owner proof and the cursor must advance.
func (c Client) ListSnippets(ctx context.Context, owner OwnerProof) ([]snippet.Command, error) {
	commands := make([]snippet.Command, 0)
	after := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := c.CallWithOwner(ctx, broker.Message{
			Action: "workspaceRead",
			Workspace: &protocol.WorkspaceQuery{
				Kind:  "snippet-list",
				After: after,
				Limit: workspaceSnippetPageLimit,
			},
		}, owner)
		if err != nil {
			return nil, err
		}
		if page.WorkspaceResult == nil {
			return nil, errors.New("代理未返回常用命令")
		}
		next := page.WorkspaceResult.Next
		if len(commands)+len(page.WorkspaceResult.Commands) > workspaceMaxCommands {
			return nil, errors.New("常用命令数量超过上限")
		}
		for _, command := range page.WorkspaceResult.Commands {
			command.History = nil
			commands = append(commands, command)
		}
		if next == "" {
			return commands, nil
		}
		if len(page.WorkspaceResult.Commands) == 0 || len(next) > workspaceMaxCursorBytes || next <= after {
			return nil, errors.New("无效的常用命令分页")
		}
		after = next
	}
}

// ServerNote reads the owner's note for exactly one host.
func (c Client) ServerNote(ctx context.Context, hostID string, owner OwnerProof) (vault.ServerNote, error) {
	page, err := c.CallWithOwner(ctx, broker.Message{
		Action:    "workspaceRead",
		Workspace: &protocol.WorkspaceQuery{Kind: "server-note", HostID: hostID},
	}, owner)
	if err != nil {
		return vault.ServerNote{}, err
	}
	if page.WorkspaceResult == nil || page.WorkspaceResult.Note == nil {
		return vault.ServerNote{}, errors.New("代理未返回服务器备注")
	}
	return *page.WorkspaceResult.Note, nil
}

// ParameterHistory reads one command parameter's history for exactly one host
// and system. Other commands, targets, and systems remain unreadable here.
func (c Client) ParameterHistory(ctx context.Context, commandID, target, system string, parameter int, owner OwnerProof) ([]string, error) {
	page, err := c.CallWithOwner(ctx, broker.Message{
		Action: "workspaceRead",
		Workspace: &protocol.WorkspaceQuery{
			Kind:      "parameter-history",
			CommandID: commandID,
			Target:    target,
			System:    system,
			Parameter: parameter,
		},
	}, owner)
	if err != nil {
		return nil, err
	}
	if page.WorkspaceResult == nil {
		return nil, errors.New("代理未返回参数历史")
	}
	return append([]string(nil), page.WorkspaceResult.History...), nil
}

// SaveSnippet writes one owner-reviewed command. Removing a command uses only
// its ID; saving carries a copy with parameter history stripped so the broker
// remains the only authority that can preserve owner-local history.
func (c Client) SaveSnippet(ctx context.Context, command snippet.Command, remove bool, owner OwnerProof) error {
	change := &protocol.WorkspaceChange{Kind: "snippet-save"}
	if remove {
		change.Kind = "snippet-delete"
		change.CommandID = command.ID
	} else {
		copy := command.Clone()
		copy.History = nil
		change.Command = &copy
	}
	return c.workspaceWrite(ctx, change, owner)
}

// SaveServerNote writes or clears one connection's owner-only note. A zero
// ServerNote clears it; the broker rejects a connection that does not exist.
func (c Client) SaveServerNote(ctx context.Context, hostID string, note vault.ServerNote, owner OwnerProof) error {
	copy := note
	return c.workspaceWrite(ctx, &protocol.WorkspaceChange{
		Kind:   "server-note-save",
		HostID: hostID,
		Note:   &copy,
	}, owner)
}

// SaveParameterHistory saves one run's explicit inputs when clearParameter is
// -1, or clears exactly one command/target/system/parameter scope when it is
// non-negative. The command copy never carries injected history to the broker.
func (c Client) SaveParameterHistory(ctx context.Context, command snippet.Command, target, system string, inputs []string, clearParameter int, owner OwnerProof) error {
	if clearParameter < -1 {
		return errors.New("参数清除序号无效")
	}
	copy := command.Clone()
	copy.History = nil
	change := &protocol.WorkspaceChange{
		Command: &copy,
		Target:  target,
		System:  system,
	}
	if clearParameter >= 0 {
		change.Kind = "parameter-history-clear"
		change.Parameter = clearParameter
	} else {
		change.Kind = "parameter-history-save"
		change.Inputs = append([]string(nil), inputs...)
	}
	return c.workspaceWrite(ctx, change, owner)
}

func (c Client) workspaceWrite(ctx context.Context, change *protocol.WorkspaceChange, owner OwnerProof) error {
	response, err := c.CallWithOwner(ctx, broker.Message{
		Action:          "workspaceWrite",
		WorkspaceChange: change,
	}, owner)
	if err != nil {
		return err
	}
	if response.Action != "ok" {
		return errors.New("代理未确认工作区写入")
	}
	return nil
}
