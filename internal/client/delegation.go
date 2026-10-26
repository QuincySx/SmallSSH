package client

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/localipc"
)

// Delegation holds only this client's restricted authority. It is never a
// password-manager session or a vault/SSH key. Treat a saved file as sensitive.
type Delegation struct {
	ParentID   string `json:"parentId"`
	Directory  string `json:"directory"`
	Key        []byte `json:"key"`
	Ticket     string `json:"ticket"`
	Permission string `json:"permission"`
	Host       string `json:"host"`
	PublicKey  string `json:"publicKey"`
}

func (c Client) RequestSession(ctx context.Context, host, label, permission string, lifetime, idle time.Duration) (approval.Request, *Delegation, error) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return approval.Request{}, nil, err
	}
	scope := approval.GrantScope{PublicKey: base64.RawURLEncoding.EncodeToString(pub), Permission: permission, LifetimeSeconds: int(lifetime / time.Second), IdleSeconds: int(idle / time.Second)}
	if err = scope.Validate(); err != nil {
		return approval.Request{}, nil, err
	}
	r, t, err := c.Request(ctx, approval.Operation{Host: host, Client: label, Kind: "grant", Grant: scope}, 5*time.Minute)
	if err != nil {
		return approval.Request{}, nil, err
	}
	dir, err := filepath.Abs(c.Dir)
	if err != nil {
		return approval.Request{}, nil, err
	}
	return r, &Delegation{ParentID: r.ID, Directory: dir, Key: key, Ticket: t, Permission: permission, Host: host, PublicKey: scope.PublicKey}, nil
}

func (d *Delegation) Close() {
	for i := range d.Key {
		d.Key[i] = 0
	}
	d.Key = nil
	d.Ticket = ""
}

func (d *Delegation) Validate(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil || abs != d.Directory || len(d.Key) != ed25519.PrivateKeySize || d.ParentID == "" || d.Host == "" || (d.Permission != "read" && d.Permission != "full") {
		return errors.New("客户端会话文件与当前代理不匹配或已损坏")
	}
	derived := ed25519.NewKeyFromSeed(d.Key[:ed25519.SeedSize])
	if !ed25519.PrivateKey(d.Key).Equal(derived) || base64.RawURLEncoding.EncodeToString(derived.Public().(ed25519.PublicKey)) != d.PublicKey {
		return errors.New("客户端会话密钥不匹配")
	}
	return nil
}

func (c Client) RequestDelegated(ctx context.Context, op approval.Operation, ttl time.Duration, d *Delegation) (approval.Request, string, error) {
	if d == nil {
		return approval.Request{}, "", approval.ErrDenied
	}
	if err := d.Validate(c.Dir); err != nil {
		return approval.Request{}, "", err
	}
	// Submit even a cross-host attempt to the broker: it must be denied and
	// recorded there, rather than disappearing in local client validation.
	r, err := c.Call(ctx, broker.Message{Action: "delegationChallenge", ID: d.ParentID, Operation: op, TTL: int(min(ttl, 5*time.Minute) / time.Second)})
	if errors.Is(err, approval.ErrSessionExpired) || errors.Is(err, approval.ErrSessionRenewing) {
		if err = c.renewDelegation(ctx, d, op); err != nil {
			return approval.Request{}, "", err
		}
		// No operation has started yet. Retry only its authorization challenge,
		// never replay a command or file write that was already submitted.
		r, err = c.Call(ctx, broker.Message{Action: "delegationChallenge", ID: d.ParentID, Operation: op, TTL: int(min(ttl, 5*time.Minute) / time.Second)})
	}
	if err != nil {
		return approval.Request{}, "", err
	}
	if r.Delegation == nil || r.Delegation.ParentID != d.ParentID {
		return approval.Request{}, "", errors.New("代理未返回会话挑战")
	}
	if op.Host != d.Host {
		return approval.Request{}, "", approval.ErrDenied
	}
	challenge := *r.Delegation
	if op.Endpoint != "" && op.Endpoint != challenge.Operation.Endpoint || op.HostFingerprint != "" && op.HostFingerprint != challenge.Operation.HostFingerprint {
		return approval.Request{}, "", errors.New("授权挑战的连接目标或指纹已变化")
	}
	// The broker may only fill the server endpoint and host fingerprint. It
	// cannot substitute the operation before asking the client to sign it.
	bound := challenge.Operation
	bound.Endpoint = op.Endpoint
	bound.HostFingerprint = op.HostFingerprint
	if bound != op {
		return approval.Request{}, "", errors.New("授权挑战中的操作已变化")
	}
	sig := ed25519.Sign(d.Key, approval.DelegationProofMessage(challenge))
	r, err = c.Call(ctx, broker.Message{Action: "delegate", ID: challenge.ID, Proof: sig})
	if err != nil {
		return approval.Request{}, "", err
	}
	if r.Request == nil || r.Request.ParentID != d.ParentID {
		return approval.Request{}, "", errors.New("代理未返回子授权")
	}
	if r.Request.Operation != challenge.Operation {
		return approval.Request{}, "", errors.New("子授权操作与签名挑战不一致")
	}
	return *r.Request, r.Ticket, nil
}

func (d *Delegation) Save(path string) error {
	if err := d.Validate(d.Directory); err != nil {
		return err
	}
	var err error
	path, err = DelegationPath(d.Directory, path)
	if err != nil {
		return err
	}
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	defer func() {
		for i := range b {
			b[i] = 0
		}
	}()
	// O_EXCL avoids clobbering another client's grant or following a symlink.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func ReadDelegation(path, dir string) (*Delegation, error) {
	var err error
	path, err = DelegationPath(dir, path)
	if err != nil {
		return nil, err
	}
	lst, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !lst.Mode().IsRegular() || lst.Size() > 8192 || runtime.GOOS != "windows" && lst.Mode().Perm()&0077 != 0 {
		return nil, errors.New("会话文件必须是仅当前用户可读写的普通文件")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !os.SameFile(lst, st) || !st.Mode().IsRegular() || st.Size() > 8192 || runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0 {
		return nil, errors.New("会话文件在读取期间已变化")
	}
	b, err := io.ReadAll(io.LimitReader(f, 8193))
	defer func() {
		for i := range b {
			b[i] = 0
		}
	}()
	if err != nil {
		return nil, err
	}
	var d Delegation
	if len(b) > 8192 || json.Unmarshal(b, &d) != nil {
		return nil, errors.New("会话文件格式无效")
	}
	if err = d.Validate(dir); err != nil {
		d.Close()
		return nil, err
	}
	return &d, nil
}

// A fixed private parent protects saved client authority on Windows as well as
// Unix. No user-selected public/shared directory receives a delegation key.
func DelegationPath(dir, path string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	parent := filepath.Join(abs, "client-sessions")
	if filepath.IsAbs(path) {
		if filepath.Dir(filepath.Clean(path)) != parent {
			return "", errors.New("会话文件只能保存在代理的 client-sessions 私有目录；也可直接输入文件名")
		}
	} else if path == "" || filepath.Base(path) != path || path == "." || path == ".." {
		return "", errors.New("请提供会话文件名，不要包含路径")
	} else {
		path = filepath.Join(parent, path)
	}
	if err = localipc.Prepare(abs); err != nil {
		return "", err
	}
	if err = localipc.Prepare(parent); err != nil {
		return "", err
	}
	return path, nil
}
