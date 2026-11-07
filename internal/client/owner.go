package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/vault"
	"time"
)

// Decide requires a fresh owner unlock. No approve-without-proof endpoint exists.
func (c Client) Decide(ctx context.Context, id string, allow, revoke bool, pass []byte) error {
	v, e := vault.Read(broker.VaultPath(c.Dir), pass)
	if e != nil {
		return e
	}
	defer v.Close()
	return c.DecideWithOwner(ctx, id, allow, revoke, v)
}

func (c Client) DecideWithOwner(ctx context.Context, id string, allow, revoke bool, v OwnerProof) error {
	r, e := c.CallWithOwner(ctx, broker.Message{Action: "status", ID: id}, v)
	if e != nil && revoke {
		r, e = c.CallWithOwner(ctx, broker.Message{Action: "sessionStatus", ID: id}, v)
	}
	if e != nil {
		return e
	}
	if r.Request == nil {
		return approval.ErrDenied
	}
	return c.DecideReviewedWithOwner(ctx, *r.Request, allow, revoke, v)
}

// Sign the exact generation the owner reviewed. A renewal may keep the same
// session ID but always has a fresh nonce; never fetch and approve a later one.
func (c Client) DecideReviewed(ctx context.Context, r approval.Request, allow, revoke bool, pass []byte) error {
	v, e := vault.Read(broker.VaultPath(c.Dir), pass)
	if e != nil {
		return e
	}
	defer v.Close()
	return c.DecideReviewedWithOwner(ctx, r, allow, revoke, v)
}

func (c Client) DecideReviewedWithOwner(ctx context.Context, r approval.Request, allow, revoke bool, v OwnerProof) error {
	return c.DecideReviewedWithLifetime(ctx, r, allow, revoke, 0, 0, v)
}

// DecideReviewedWithLifetime applies the owner's chosen session duration to
// the copy that is signed and to the broker's stored request. Zero keeps the
// requester's value; revoking ignores it.
func (c Client) DecideReviewedWithLifetime(ctx context.Context, r approval.Request, allow, revoke bool, lifetimeSeconds, idleSeconds int, v OwnerProof) error {
	if !HasOwnerProof(v) {
		return approval.ErrDenied
	}
	if allow && !revoke && lifetimeSeconds > 0 {
		if r.Operation.Kind != "grant" {
			// A one-shot operation has no session duration; ignore the value
			// instead of refusing an otherwise valid decision.
			lifetimeSeconds = 0
		} else {
			r.Operation.Grant.LifetimeSeconds = lifetimeSeconds
			if idleSeconds > 0 && idleSeconds <= lifetimeSeconds {
				r.Operation.Grant.IdleSeconds = idleSeconds
			}
			if r.Operation.Grant.IdleSeconds > lifetimeSeconds {
				r.Operation.Grant.IdleSeconds = lifetimeSeconds
			}
		}
	} else {
		lifetimeSeconds = 0
	}
	var proof []byte
	var e error
	action := "decide"
	if revoke {
		proof, e = approval.RevokeProof(r, v.Proof)
		action = "revoke"
	} else {
		proof, e = approval.DecisionProof(r, allow, v.Proof)
	}
	if e != nil {
		return e
	}
	_, e = c.Call(ctx, broker.Message{Action: action, ID: r.ID, Allow: allow, TTL: lifetimeSeconds, Idle: idleSeconds, Proof: proof})
	return e
}

// Describe quotes terminal control characters so an untrusted request cannot
// repaint a TTY approval prompt. Client is a self-declared, unverified label.
func (c Client) PutProfile(ctx context.Context, p vault.Profile, pass []byte) error {
	if e := vault.Validate(p); e != nil {
		return e
	}
	v, e := vault.Read(broker.VaultPath(c.Dir), pass)
	if e != nil {
		return e
	}
	defer v.Close()
	challenge, e := c.Call(ctx, broker.Message{Action: "profileChallenge"})
	if e != nil {
		return e
	}
	proof, e := broker.ProfileProof(challenge.ID, p, v.Proof)
	if e != nil {
		return e
	}
	_, e = c.Call(ctx, broker.Message{Action: "profilePut", ID: challenge.ID, Profile: &p, Proof: proof})
	return e
}

func Describe(r approval.Request) string {
	if r.Operation.Kind == "tunnel" {
		spec := r.Operation.Tunnel
		r.Operation.Kind = "SSH 隧道（" + spec.Kind + "）"
		r.Operation.Path = spec.Listen
		r.Operation.Destination = spec.Target
		return "监听端口对其所在机器的其他进程开放；动态 SOCKS5 可访问任意 TCP 目标。关闭 / 撤销 / 到期会关闭监听及现有连接。\n" + Describe(r)
	}
	if r.Renewal && r.Renewals > 1 {
		return fmt.Sprintf("连续第 %d 次续期\n", r.Renewals) + describeOperation(r)
	}
	return describeOperation(r)
}

func describeOperation(r approval.Request) string {
	o := r.Operation
	if o.Kind == "docker-logs" || o.Kind == "docker-terminal" {
		r.Operation.Kind = "Docker 操作"
		return fmt.Sprintf("Docker 操作: %s\n实例: %s\n容器: %s\nShell: %s\n日志末尾: %d 行；持续跟随: %t\n", o.Kind, o.Docker.Target.Engine, o.Docker.Target.ID, o.Docker.Shell, o.Docker.Tail, o.Docker.Follow) + Describe(r)
	}
	if o.Kind == "compose-plan" || o.Kind == "compose-update" {
		r.Operation.Kind = "Compose 操作"
		return fmt.Sprintf("Compose 操作: %s\n项目: %s\n服务: %s\n目录: %q\n配置: %q\n计划摘要: %s\n", o.Kind, o.Compose.Project, o.Compose.Services, o.Compose.Directory, o.Compose.Files, o.Compose.ConfigHash) + Describe(r)
	}
	if o.Kind == "file-attributes" {
		summary := "文件属性变更（仅当前目标，不递归）\n"
		if o.Attributes.SetMode {
			summary += fmt.Sprintf("权限：%03o → %03o\n", o.Attributes.Expected.Mode&0777, o.Attributes.Mode)
		}
		if o.Attributes.SetUID {
			summary += fmt.Sprintf("所属用户 UID：%d → %d\n", o.Attributes.Expected.UID, o.Attributes.UID)
		}
		if o.Attributes.SetGID {
			summary += fmt.Sprintf("所属组 GID：%d → %d\n", o.Attributes.Expected.GID, o.Attributes.GID)
		}
		r.Operation.Kind = "修改文件属性"
		return summary + Describe(r)
	}
	if o.Kind == "grant" {
		header := ""
		if r.Renewal {
			header = "会话已自然过期 · 按原服务器与权限申请续期\n"
		}
		permission := "只读：目录、文件下载和固定状态采样"
		if o.PathProtection {
			permission += "；保护路径不可读取"
		}
		if o.Grant.Permission == "full" {
			permission = "完整：此 SSH 账号的全部权限，包括任意命令与文件写入"
			if o.PathProtection {
				permission = "读写：受服务器策略限制，仅拦截常见破坏性命令；护栏不能替代核对，交互终端、属性与续传写入不开放"
			}
		}
		if o.Grant.ReadRoot != "" || o.Grant.WriteRoot != "" {
			readRoot, writeRoot := o.Grant.ReadRoot, o.Grant.WriteRoot
			if readRoot == "" {
				readRoot = "不开放"
			}
			if writeRoot == "" {
				writeRoot = "不开放"
			}
			permission = fmt.Sprintf("限定目录文件操作；读取根目录：%q；写入根目录：%q；不含命令、终端、状态采样或容器日志", readRoot, writeRoot)
		}
		r.Operation.Kind = "客户端会话"
		return header + fmt.Sprintf("会话权限: %s\n配对码（与申请端核对）: %s\n最长使用: %s；闲置: %s\n", permission, o.Grant.PairingCode(), time.Duration(o.Grant.LifetimeSeconds)*time.Second, time.Duration(o.Grant.IdleSeconds)*time.Second) + Describe(r)
	}
	expires := r.Expires.Local().Format("2006-01-02 15:04:05 MST")
	if r.OwnerSession {
		expires = "关闭终端、锁定或退出时结束"
	}
	if o.Kind == "shell" && o.ServerStatus {
		o.Kind = "终端连接＋自动读取服务器状态（固定只读命令，随连接停止）"
	}
	peer := ""
	if r.Peer.PID > 0 {
		peer = fmt.Sprintf("进程 ID: %d\n进程路径: %q\n进程实例: %s\n程序摘要: %s\n", r.Peer.PID, r.Peer.Path, r.Peer.Instance, r.Peer.CodeID)
	}
	return peer + fmt.Sprintf("申请方（未验证）: %q\n服务器 ID: %q\n连接目标: %q\n主机指纹: %q\n操作: %q\n命令: %q\n远程路径: %q\n目标路径: %q\n权限: %03o\n新内容 SHA256: %q\n原文件 SHA256: %q\n传输 ID: %q\n大小 / 偏移: %d / %d\n到期: %s\n授权 ID: %s", o.Client, o.Host, o.Endpoint, o.HostFingerprint, o.Kind, o.Command, o.Path, o.Destination, o.Mode, o.ContentHash, o.OriginalHash, o.TransferID, o.Size, o.Offset, expires, r.ID)
}

func (c Client) PutDetails(ctx context.Context, d vault.Summary, owner OwnerProof) error {
	if !HasOwnerProof(owner) {
		return errors.New("保险库已锁定")
	}
	challenge, e := c.Call(ctx, broker.Message{Action: "profileChallenge"})
	if e != nil {
		return e
	}
	b, _ := json.Marshal(d)
	proof, e := owner.Proof(append([]byte("ssh-client/profile-details/v1\x00"+challenge.ID+"\x00"), b...))
	if e != nil {
		return e
	}
	_, e = c.Call(ctx, broker.Message{Action: "profileDetails", ID: challenge.ID, Details: &d, Proof: proof})
	return e
}

func (c Client) DeleteProfile(ctx context.Context, id string, owner OwnerProof) error {
	if !HasOwnerProof(owner) {
		return errors.New("保险库已锁定")
	}
	challenge, e := c.Call(ctx, broker.Message{Action: "profileChallenge"})
	if e != nil {
		return e
	}
	proof, e := owner.Proof([]byte("ssh-client/profile-delete/v1\x00" + challenge.ID + "\x00" + id))
	if e != nil {
		return e
	}
	_, e = c.Call(ctx, broker.Message{Action: "profileDelete", ID: challenge.ID, Target: id, Proof: proof})
	return e
}
