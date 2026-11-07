package broker

import (
	"errors"
	"fmt"

	"sshclient/internal/approval"
	"sshclient/internal/protocol"
	"sshclient/internal/snippet"
)

const (
	workspaceKindSnippetSave           = "snippet-save"
	workspaceKindSnippetDelete         = "snippet-delete"
	workspaceKindServerNoteSave        = "server-note-save"
	workspaceKindParameterHistorySave  = "parameter-history-save"
	workspaceKindParameterHistoryClear = "parameter-history-clear"
	workspaceMaxInputBytes             = 512
)

type preparedWorkspaceWrite struct {
	kind  string
	host  string
	apply func() error
}

func (s *Server) workspaceWrite(q Message, channel *callerChannel) (Message, error) {
	if !s.ownerQuery(q, channel) {
		return Message{}, approval.ErrDenied
	}
	if q.WorkspaceChange == nil {
		return Message{}, errors.New("工作区写入载荷缺失")
	}
	action, err := s.prepareWorkspaceWrite(q.WorkspaceChange)
	if err != nil {
		return Message{}, err
	}

	peer := approval.Peer{}
	if channel != nil {
		peer = channel.peer
	}
	request := approval.Request{
		OwnerIssued: true,
		ID:          q.Challenge,
		Peer:        peer,
		Operation:   approval.Operation{Kind: action.kind, Host: action.host, Client: "所有者"},
	}
	if err = s.auditRequest("workspace_write", &request); err != nil {
		return Message{}, err
	}

	writeErr := action.apply()
	outcome := "success"
	if writeErr != nil {
		outcome = "failed"
	}
	if err = s.auditCompletion(&request, outcome, nil); err != nil {
		return Message{}, err
	}
	if writeErr != nil {
		return Message{}, writeErr
	}
	return Message{Action: "ok"}, nil
}

func (s *Server) prepareWorkspaceWrite(change *protocol.WorkspaceChange) (preparedWorkspaceWrite, error) {
	file := VaultPath(s.dir)
	switch change.Kind {
	case workspaceKindSnippetSave:
		if change.Command == nil {
			return preparedWorkspaceWrite{}, errors.New("常用命令载荷缺失")
		}
		command := change.Command.Clone()
		command.History = nil
		if !workspaceIDValid(command.ID) {
			return preparedWorkspaceWrite{}, errors.New("命令 ID 无效")
		}
		if err := snippet.Validate(command); err != nil {
			return preparedWorkspaceWrite{}, err
		}
		return preparedWorkspaceWrite{
			kind: workspaceKindSnippetSave,
			apply: func() error {
				return s.Vault.SaveSnippet(file, command, false)
			},
		}, nil
	case workspaceKindSnippetDelete:
		if !workspaceIDValid(change.CommandID) {
			return preparedWorkspaceWrite{}, errors.New("命令 ID 无效")
		}
		commandID := change.CommandID
		return preparedWorkspaceWrite{
			kind: workspaceKindSnippetDelete,
			apply: func() error {
				return s.Vault.SaveSnippet(file, snippet.Command{ID: commandID}, true)
			},
		}, nil
	case workspaceKindServerNoteSave:
		if !workspaceIDValid(change.HostID) {
			return preparedWorkspaceWrite{}, errors.New("服务器 ID 无效")
		}
		if change.Note == nil {
			return preparedWorkspaceWrite{}, errors.New("服务器备注载荷缺失")
		}
		hostID, note := change.HostID, *change.Note
		return preparedWorkspaceWrite{
			kind: workspaceKindServerNoteSave,
			host: hostID,
			apply: func() error {
				return s.Vault.SaveServerNote(file, hostID, note)
			},
		}, nil
	case workspaceKindParameterHistorySave:
		return s.prepareParameterHistoryWrite(file, change, false)
	case workspaceKindParameterHistoryClear:
		return s.prepareParameterHistoryWrite(file, change, true)
	default:
		return preparedWorkspaceWrite{}, errors.New("未知的工作区写入类型")
	}
}

func (s *Server) prepareParameterHistoryWrite(file string, change *protocol.WorkspaceChange, clear bool) (preparedWorkspaceWrite, error) {
	if change.Command == nil {
		return preparedWorkspaceWrite{}, errors.New("参数历史命令快照缺失")
	}
	command := change.Command.Clone()
	command.History = nil
	if !workspaceIDValid(command.ID) {
		return preparedWorkspaceWrite{}, errors.New("命令 ID 无效")
	}
	if !workspaceTargetValid(change.Target) {
		return preparedWorkspaceWrite{}, errors.New("目标标识无效")
	}
	if !workspaceSystemValid(change.System) {
		return preparedWorkspaceWrite{}, errors.New("系统类型无效")
	}
	target, system := change.Target, change.System
	if clear {
		if change.Parameter < 0 || change.Parameter >= snippet.MaxArgs {
			return preparedWorkspaceWrite{}, fmt.Errorf("参数序号必须为 0 至 %d", snippet.MaxArgs-1)
		}
		parameter := change.Parameter
		return preparedWorkspaceWrite{
			kind: workspaceKindParameterHistoryClear,
			apply: func() error {
				return s.Vault.SaveParameterHistory(file, command, target, system, nil, parameter)
			},
		}, nil
	}
	if len(change.Inputs) > snippet.MaxArgs {
		return preparedWorkspaceWrite{}, fmt.Errorf("参数输入最多 %d 个", snippet.MaxArgs)
	}
	for _, input := range change.Inputs {
		if len(input) > workspaceMaxInputBytes {
			return preparedWorkspaceWrite{}, fmt.Errorf("单个参数输入最多 %d 字节", workspaceMaxInputBytes)
		}
	}
	inputs := append([]string(nil), change.Inputs...)
	return preparedWorkspaceWrite{
		kind: workspaceKindParameterHistorySave,
		apply: func() error {
			return s.Vault.SaveParameterHistory(file, command, target, system, inputs, -1)
		},
	}, nil
}

func workspaceIDValid(id string) bool {
	return id != "" && len(id) <= workspaceMaxIDBytes
}
