//go:build darwin

package systemauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
)

//go:embed native-hashes.json
var hashes []byte

type macBackend struct{ touchID atomic.Bool }

func newBackend(window uintptr) Backend { return &macBackend{} }
func (b *macBackend) Name() string {
	if b.touchID.Load() {
		return "Touch ID"
	}
	return "系统验证"
}

type nativeReply struct {
	Supported bool   `json:"supported"`
	TouchID   bool   `json:"touchID"`
	Blob      []byte `json:"blob"`
	Key       []byte `json:"key"`
	Error     string `json:"error"`
}

func (*macBackend) call(ctx context.Context, action string, key, blob []byte, reason string) (nativeReply, error) {
	var reply nativeReply
	exe, e := os.Executable()
	if e != nil {
		return reply, e
	}
	base := filepath.Dir(exe)
	var expected map[string]string
	json.Unmarshal(hashes, &expected)
	want := expected["darwin/"+runtime.GOARCH]
	if want == "" {
		return reply, errors.New("请先构建本机系统认证组件")
	}
	var helper string
	for _, candidate := range []string{filepath.Join(base, "biometric-helper"), filepath.Join(base, "..", "Resources", "biometric-helper"), filepath.Join(base, "..", "biometric-helper")} {
		b, e := os.ReadFile(candidate)
		if e != nil {
			continue
		}
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) == want {
			helper = candidate
			break
		}
	}
	if helper == "" {
		return reply, errors.New("本机认证组件缺失或校验不匹配")
	}
	input, e := json.Marshal(map[string]any{"action": action, "key": key, "blob": blob, "reason": reason})
	if e != nil {
		return reply, e
	}
	defer wipe(input)
	cmd := exec.CommandContext(ctx, helper)
	cmd.Stdin = bytes.NewReader(input)
	var output bytes.Buffer
	cmd.Stdout = &output
	e = cmd.Run()
	if output.Len() > 65536 {
		return reply, errors.New("认证组件响应过大")
	}
	if json.Unmarshal(output.Bytes(), &reply) != nil {
		return reply, fmt.Errorf("认证组件未正常响应: %w", e)
	}
	if reply.Error != "" {
		return reply, errors.New(reply.Error)
	}
	if e != nil {
		return reply, e
	}
	return reply, nil
}
func (b *macBackend) Probe(ctx context.Context) error {
	r, e := b.call(ctx, "probe", nil, nil, "")
	b.touchID.Store(e == nil && r.Supported && r.TouchID)
	if e == nil && !r.Supported {
		return errors.New("本机不支持系统验证")
	}
	return e
}
func (b *macBackend) Wrap(ctx context.Context, key []byte) (json.RawMessage, error) {
	r, e := b.call(ctx, "wrap", key, nil, "")
	if e != nil {
		return nil, e
	}
	return json.Marshal(r.Blob)
}
func (b *macBackend) Unwrap(ctx context.Context, raw json.RawMessage, reason string) ([]byte, error) {
	var blob []byte
	if e := json.Unmarshal(raw, &blob); e != nil {
		return nil, e
	}
	r, e := b.call(ctx, "unwrap", nil, blob, reason)
	if e != nil {
		return nil, e
	}
	if len(r.Key) != 32 {
		return nil, errors.New("设备密钥长度错误")
	}
	return r.Key, nil
}
