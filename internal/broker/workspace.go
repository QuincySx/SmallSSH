package broker

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"sshclient/internal/approval"
	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
	"sshclient/internal/snippet"
)

const (
	workspaceKindSnippetList = "snippet-list"
	workspaceKindServerNote  = "server-note"
	workspaceKindHistory     = "parameter-history"
	workspaceDefaultLimit    = 16
	workspaceMaxLimit        = 32
	workspaceMaxIDBytes      = 128
	workspacePageBudget      = 512 << 10
)

func (s *Server) workspaceRead(q Message, channel *callerChannel) (Message, error) {
	if !s.ownerQuery(q, channel) {
		return Message{}, approval.ErrDenied
	}
	if q.Workspace == nil {
		return Message{}, errors.New("工作区查询缺失")
	}
	switch q.Workspace.Kind {
	case workspaceKindSnippetList:
		return s.workspaceSnippetPage(q.Workspace)
	case workspaceKindServerNote:
		return s.workspaceServerNote(q.Workspace)
	case workspaceKindHistory:
		return s.workspaceParameterHistory(q.Workspace)
	default:
		return Message{}, errors.New("未知的工作区查询类型")
	}
}

func (s *Server) workspaceSnippetPage(q *protocol.WorkspaceQuery) (Message, error) {
	limit := q.Limit
	if limit == 0 {
		limit = workspaceDefaultLimit
	}
	if limit < 1 || limit > workspaceMaxLimit {
		return Message{}, fmt.Errorf("常用命令分页上限必须为 1 至 %d", workspaceMaxLimit)
	}
	if len(q.After) > workspaceMaxIDBytes {
		return Message{}, errors.New("常用命令游标过长")
	}

	commands := s.Vault.Snippets()
	for i := range commands {
		commands[i].History = nil
	}
	sort.SliceStable(commands, func(i, j int) bool { return commands[i].ID < commands[j].ID })
	start := sort.Search(len(commands), func(i int) bool { return commands[i].ID > q.After })
	page := make([]snippet.Command, 0, min(limit, len(commands)-start))
	next := ""
	for i := start; i < len(commands); i++ {
		page = append(page, commands[i])
		size, err := workspaceResponseSize(page, "")
		if err != nil {
			return Message{}, err
		}
		if size > workspacePageBudget {
			page = page[:len(page)-1]
			if len(page) == 0 {
				return Message{}, fmt.Errorf("单个常用命令超过 %d KiB 分页预算", workspacePageBudget>>10)
			}
			next = page[len(page)-1].ID
			break
		}
		if len(page) == limit {
			if i+1 < len(commands) {
				next = page[len(page)-1].ID
			}
			break
		}
	}

	size, err := workspaceResponseSize(page, next)
	if err != nil {
		return Message{}, err
	}
	if size >= localipc.MaxFrame {
		return Message{}, errors.New("常用命令响应超过本地消息上限")
	}
	return Message{WorkspaceResult: &protocol.WorkspaceResult{Commands: page, Next: next}}, nil
}

func (s *Server) workspaceServerNote(q *protocol.WorkspaceQuery) (Message, error) {
	if q.HostID == "" || len(q.HostID) > workspaceMaxIDBytes {
		return Message{}, errors.New("服务器 ID 无效")
	}
	note, err := s.Vault.ServerNote(q.HostID)
	if err != nil {
		return Message{}, err
	}
	return Message{WorkspaceResult: &protocol.WorkspaceResult{Note: &note}}, nil
}

func (s *Server) workspaceParameterHistory(q *protocol.WorkspaceQuery) (Message, error) {
	if q.CommandID == "" || len(q.CommandID) > workspaceMaxIDBytes {
		return Message{}, errors.New("命令 ID 无效")
	}
	if !workspaceTargetValid(q.Target) {
		return Message{}, errors.New("目标标识无效")
	}
	if !workspaceSystemValid(q.System) {
		return Message{}, errors.New("系统类型无效")
	}
	if q.Parameter < 0 || q.Parameter >= snippet.MaxArgs {
		return Message{}, fmt.Errorf("参数序号必须为 0 至 %d", snippet.MaxArgs-1)
	}
	history := s.Vault.ParameterHistory(q.CommandID, q.Target, q.System, q.Parameter)
	return Message{WorkspaceResult: &protocol.WorkspaceResult{History: history}}, nil
}

func workspaceResponseSize(commands []snippet.Command, next string) (int, error) {
	data, err := json.Marshal(Message{WorkspaceResult: &protocol.WorkspaceResult{Commands: commands, Next: next}})
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

func workspaceTargetValid(target string) bool {
	if len(target) != 64 {
		return false
	}
	for i := 0; i < len(target); i++ {
		c := target[i]
		if ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') {
			continue
		}
		return false
	}
	return true
}

func workspaceSystemValid(system string) bool {
	if system == snippet.OSDefault {
		return true
	}
	for _, candidate := range snippet.SystemKeys() {
		if candidate == system {
			return true
		}
	}
	return false
}
