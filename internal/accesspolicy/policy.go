// Package accesspolicy defines the owner's persistent limits for untrusted automation.
// Approval grants never override these limits. Owner actions are checked separately.
package accesspolicy

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"sshclient/internal/approval"
	"sshclient/internal/operationrisk"
)

var (
	ErrDisabled           = errors.New("此服务器已禁止 AI 和自动化访问")
	ErrProtected          = errors.New("操作涉及受保护路径")
	ErrUncontrolled       = errors.New("路径保护模式不允许无法限定文件影响范围的操作")
	ErrCommand            = errors.New("保护模式下不开放交互终端和容器终端；命令请使用可检查的 exec")
	ErrEmptyCommand       = errors.New("命令不能为空")
	ErrDestructiveCommand = errors.New("命令被破坏性命令护栏拦截")
	ErrHighRisk           = errors.New("受控会话拒绝高风险或不可逐项检查的操作")
	ErrInvalidPolicy      = errors.New("自动化策略无效")
)

type Policy struct {
	Disabled       bool     `json:"disabled,omitempty"`
	Unrestricted   bool     `json:"unrestricted,omitempty"`
	ProtectedPaths []string `json:"protectedPaths,omitempty"`
}

var defaultPaths = []string{"/etc", "/boot", "/usr", "/bin", "/sbin", "/lib", "/lib64", "/proc", "/sys", "/dev", "/home", "/root"}

// Clone prevents callers from changing a stored policy through its path slice.
func (p Policy) Clone() Policy {
	p.ProtectedPaths = append([]string(nil), p.ProtectedPaths...)
	return p
}

// Paths returns independent effective protected roots. The root directory itself
// is always protected; listing "/" explicitly protects every descendant as well.
func (p Policy) Paths() []string {
	if len(p.ProtectedPaths) == 0 {
		return append([]string(nil), defaultPaths...)
	}
	return append([]string(nil), p.ProtectedPaths...)
}

func (p Policy) Validate() error {
	if len(p.ProtectedPaths) > 32 {
		return fmt.Errorf("%w: 受保护路径最多32项", ErrInvalidPolicy)
	}
	for _, v := range p.ProtectedPaths {
		if !validPath(v) {
			return fmt.Errorf("%w: 受保护路径需为不含控制字符的规范绝对路径", ErrInvalidPolicy)
		}
	}
	return nil
}

func validPath(v string) bool {
	if v == "" || len(v) > 4096 || !utf8.ValidString(v) || !strings.HasPrefix(v, "/") || path.Clean(v) != v {
		return false
	}
	return !strings.ContainsFunc(v, unicode.IsControl)
}

// CheckPaths rejects invalid mutation names, protected descendants, and ancestors
// whose removal or rename would also move a protected root. It is a lexical check;
// remote symlink and canonical path checks are additionally required before use.
func CheckPaths(paths, protected []string) error {
	return checkPaths(paths, protected, true)
}

func CheckReadPaths(paths, protected []string) error {
	return checkPaths(paths, protected, false)
}

func checkPaths(paths, protected []string, mutation bool) error {
	for _, p := range paths {
		if !validPath(p) || mutation && p == "/" {
			return fmt.Errorf("%w: 写入目标需为根目录以外的规范绝对路径", ErrProtected)
		}
		for _, root := range protected {
			if !validPath(root) {
				return fmt.Errorf("%w: 无法确认受保护路径", ErrProtected)
			}
			if root == "/" || p == root || strings.HasPrefix(p, root+"/") || mutation && strings.HasPrefix(root, p+"/") {
				return ErrProtected
			}
		}
	}
	return nil
}

// MutationPaths returns every directory entry a typed mutation may change.
// Opaque existing-inode changes are denied by Check in protected mode.
func MutationPaths(op approval.Operation) []string {
	switch op.Kind {
	case "put":
		if op.TransferID != "" {
			return []string{op.Path, path.Join(path.Dir(op.Path), ".ssh-part-"+op.TransferID)}
		}
		return []string{op.Path}
	case "mkdir", "remove", "chmod", "file-attributes", "replace", "put-resume":
		return []string{op.Path}
	case "rename":
		return []string{op.Path, op.Destination}
	}
	return nil
}

func ReadPaths(op approval.Operation) []string {
	switch op.Kind {
	case "list", "file-info", "get", "get-resume":
		return []string{op.Path}
	}
	return nil
}

// Check is independent of approval state, so it must run before requesting any
// popup or delegated authorization and again when an untrusted operation starts.
func (p Policy) Check(op approval.Operation) error {
	if p.Disabled {
		return ErrDisabled
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if p.Unrestricted {
		return nil
	}
	switch op.Kind {
	case "exec":
		if strings.TrimSpace(op.Command) == "" {
			return ErrEmptyCommand
		}
		if reason := DestructiveCommand(op.Command); reason != "" {
			return fmt.Errorf("%w：%s（护栏是尽力而为，不是安全边界）", ErrDestructiveCommand, reason)
		}
		return nil
	case "shell", "docker-terminal":
		return ErrCommand
	case "compose-update", "chmod", "file-attributes", "put-resume":
		// SFTP v3 cannot prove that an existing inode is not also hard-linked
		// inside a protected tree. Attribute changes and resumed upload writes
		// therefore require the owner's explicit unrestricted policy.
		return ErrUncontrolled
	case "mkdir", "remove", "rename", "put", "replace":
		return CheckPaths(MutationPaths(op), p.Paths())
	case "list", "file-info", "get", "get-resume":
		return CheckReadPaths(ReadPaths(op), p.Paths())
	case "grant", "monitor", "docker-logs", "compose-plan", "upgrade-preview":
		return nil
	default:
		return ErrUncontrolled
	}
}

// CheckGuarded is additional to the server policy, including when that policy
// is unrestricted. A guarded grant never opens opaque channels or high risks.
func CheckGuarded(op approval.Operation) error {
	assessment := operationrisk.Assess(op.Kind, op.Command, op.Path, op.Destination)
	if assessment.Level == operationrisk.High {
		return fmt.Errorf("%w：%s", ErrHighRisk, assessment.Reason)
	}
	return nil
}

func Reason(err error) string {
	switch {
	case errors.Is(err, ErrHighRisk):
		return "high_risk_operation"
	case errors.Is(err, ErrDisabled):
		return "automation_disabled"
	case errors.Is(err, ErrProtected):
		return "protected_path"
	case errors.Is(err, ErrDestructiveCommand):
		return "destructive_command"
	case errors.Is(err, ErrEmptyCommand):
		return "invalid_operation"
	case errors.Is(err, ErrUncontrolled), errors.Is(err, ErrCommand):
		return "uncontrolled_operation"
	case errors.Is(err, ErrInvalidPolicy):
		return "invalid_policy"
	}
	return ""
}
