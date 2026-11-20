package broker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/auditlog"
	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
	"sshclient/internal/snippet"
	"sshclient/internal/vault"
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
	kind        string
	host        string
	object      string
	fingerprint string
	apply       func() error
}

func (s *Server) workspaceWrite(q Message, channel *callerChannel) (Message, error) {
	if !s.ownerQuery(q, channel) {
		return Message{}, approval.ErrDenied
	}
	if q.WorkspaceChange == nil {
		return Message{}, errors.New("工作区写入载荷缺失")
	}
	ctx := s.ctx
	if channel != nil {
		ctx = channel.ctx
	}
	if q.WorkspaceChange.Kind == "ssh-identity-change" && channel != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		go localipc.WatchDisconnect(ctx, channel.conn, cancel)
	}
	peer := approval.Peer{}
	if channel != nil {
		peer = channel.peer
	}
	action, err := s.prepareWorkspaceWrite(ctx, q.WorkspaceChange, peer)
	if err != nil {
		return Message{}, err
	}

	request := approval.Request{
		OwnerIssued: true,
		ID:          q.Challenge,
		Peer:        peer,
		Operation:   approval.Operation{Kind: action.kind, Host: action.host, Client: "所有者", Path: action.object, HostFingerprint: action.fingerprint},
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

func (s *Server) prepareWorkspaceWrite(caller context.Context, change *protocol.WorkspaceChange, actors ...approval.Peer) (preparedWorkspaceWrite, error) {
	file := VaultPath(s.dir)
	switch change.Kind {
	case "host-trust":
		if change.HostTrust == nil {
			return preparedWorkspaceWrite{}, errors.New("服务器指纹载荷缺失")
		}
		trust := *change.HostTrust
		return preparedWorkspaceWrite{kind: change.Kind, host: trust.ID, fingerprint: trust.Fingerprint, apply: func() error {
			s.ownerMu.Lock()
			defer s.ownerMu.Unlock()
			if err := s.Vault.SaveHostTrust(file, trust); err != nil {
				return err
			}
			s.Approvals.InvalidateHost(trust.ID)
			s.conns.clear()
			s.roots.clear()
			return nil
		}}, nil
	case "grant-policy-change":
		if change.GrantPolicy == nil {
			return preparedWorkspaceWrite{}, errors.New("授权规则载荷缺失")
		}
		policy := *change.GrantPolicy
		return preparedWorkspaceWrite{kind: change.Kind, host: policy.ID, apply: func() error {
			s.ownerMu.Lock()
			defer s.ownerMu.Unlock()
			before, after, err := s.Approvals.SetGrantPolicy(policy)
			if err != nil {
				return err
			}
			name := ""
			if p, e := s.Vault.Profile(after.Operation.Host); e == nil {
				name = p.Name
			}
			v := auditlog.Event{
				Time: time.Now().UTC(), Event: "grant_policy_changed", Kind: "grant", RequestID: after.ID,
				Generation: after.Generation(), Host: after.Operation.Host, HostName: name, Endpoint: after.Operation.Endpoint,
				Expires: after.Expires, IdleSeconds: after.Activity.IdleSeconds, Permission: after.Operation.Grant.Permission,
				MediumReview: after.Activity.MediumReview, Executable: after.Peer.Path, Bundle: after.Peer.Bundle,
				ExecutableID: after.Peer.CodeID, ProcessInstance: after.Peer.Instance, PeerPID: after.Peer.PID, Outcome: "success",
			}
			if policy.MediumReview == nil {
				v.PreviousIdle = &before.Activity.IdleSeconds
			} else {
				v.PreviousMediumReview = before.Activity.MediumReview
			}
			if v.MediumReview != nil {
				v.SkipMediumReview = !*v.MediumReview
			}
			if len(actors) > 0 {
				p := actors[0]
				v.ActorPID = p.PID
				v.ActorExecutable = auditText(p.Path, 1024)
				v.ActorInstance = auditText(p.Instance, 128)
				v.ActorCodeID = auditText(p.CodeID, 128)
			}
			if err := s.audit.store.Append(v); err != nil {
				return s.failAudit(err)
			}
			s.auditExpiries()
			return nil
		}}, nil
	case "connections-reorder":
		if change.ConnectionOrder == nil {
			return preparedWorkspaceWrite{}, errors.New("连接排序载荷缺失")
		}
		order := *change.ConnectionOrder
		order.Items = append([]vault.ConnectionPlacement(nil), order.Items...)
		return preparedWorkspaceWrite{kind: change.Kind, apply: func() error {
			s.ownerMu.Lock()
			defer s.ownerMu.Unlock()
			return s.Vault.SaveConnectionOrder(file, order)
		}}, nil
	case "ssh-identity-change":
		if change.Identity == nil {
			return preparedWorkspaceWrite{}, errors.New("密钥管理载荷缺失")
		}
		intent := *change.Identity
		return preparedWorkspaceWrite{kind: "ssh-key-" + intent.Action, host: intent.ID, object: intent.Name, apply: func() error {
			controller, err := s.identityController()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(caller, 2*time.Minute)
			defer cancel()
			return controller.Change(ctx, intent)
		}}, nil
	case "vnc-settings-save":
		if !workspaceIDValid(change.HostID) || change.VNC == nil {
			return preparedWorkspaceWrite{}, errors.New("VNC 设置载荷无效")
		}
		settings, host := *change.VNC, change.HostID
		if err := settings.Validate(); err != nil {
			return preparedWorkspaceWrite{}, err
		}
		return preparedWorkspaceWrite{kind: change.Kind, host: host, apply: func() error {
			s.ownerMu.Lock()
			defer s.ownerMu.Unlock()
			return s.Vault.SaveVNCSettings(file, host, settings)
		}}, nil
	case "route-save", "route-delete":
		if change.Route == nil {
			return preparedWorkspaceWrite{}, errors.New("线路载荷缺失")
		}
		route, keep, remove := *change.Route, change.KeepRoutePassword, change.Kind == "route-delete"
		return preparedWorkspaceWrite{kind: change.Kind, apply: func() error {
			s.ownerMu.Lock()
			defer s.ownerMu.Unlock()
			if err := s.Vault.SaveRoute(file, route, keep, remove); err != nil {
				return err
			}
			// Existing streams retain their snapshot. Future dials and short
			// operations must not reuse transports built from the previous route.
			s.conns.clear()
			s.roots.clear()
			return nil
		}}, nil
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
