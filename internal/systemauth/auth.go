// Package systemauth releases a vault key only after an OS-mediated owner check.
// The OS-specific guarantees differ; see docs/SECURITY.md. No approval RPC
// bypass or software 'authentication succeeded' fallback is provided.
package systemauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sshclient/internal/vault"
	"time"
)

type Status struct {
	Available    bool
	Name, Reason string
	Enabled      bool
}
type Backend interface {
	Probe(context.Context) error
	Wrap(context.Context, []byte) (json.RawMessage, error)
	Unwrap(context.Context, json.RawMessage, string) ([]byte, error)
	Name() string
}
type Service struct{ backend Backend }

// recordRemover is implemented by the backends that keep the wrapped key in a
// system keychain entry, which has to be deleted when the record is replaced or
// removed. Linux stores the device key in secret-tool; the macOS Secure Enclave
// envelope and the Windows DPAPI blob carry their own protected key, so there is
// nothing beside device-unlock.json to clean up.
type recordRemover interface {
	Remove(context.Context, json.RawMessage) error
}

func New(window uintptr) *Service { return &Service{backend: newBackend(window)} }

type record struct {
	Version   int             `json:"version"`
	VaultHash string          `json:"vaultHash"`
	Data      json.RawMessage `json:"data"`
}

func path(dir string) string { return filepath.Join(dir, "device-unlock.json") }
func (s *Service) Status(ctx context.Context, dir string) Status {
	var st Status
	// Enabled means the record is usable, not that a file exists. readRecord
	// rejects what Open would also reject, so the settings cannot offer system
	// unlock that every attempt would then fail to perform.
	if _, e := readRecord(dir); e == nil {
		st.Enabled = true
	} else if !os.IsNotExist(e) {
		st.Reason = e.Error()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	e := s.backend.Probe(ctx)
	st.Name = s.backend.Name() // Probing is what learns whether it is really Touch ID.
	if e == nil {
		st.Available = true
	} else if st.Reason == "" {
		st.Reason = e.Error()
	}
	return st
}
func (s *Service) Enroll(ctx context.Context, dir string, pass []byte) error {
	// A previous record that no longer parses cannot name the system entry it
	// owns, so re-enrolling over it may leave that entry behind. Refusing instead
	// would trap the user: Open and Disable reject the same record, and the only
	// way out is re-enrolling.
	previous, previousErr := readRecord(dir)
	if previousErr != nil {
		previous = record{}
	}
	v, e := vault.Read(filepath.Join(dir, "vault.json"), pass)
	if e != nil {
		return e
	}
	defer v.Close()
	key, e := v.UnlockKey()
	if e != nil {
		return e
	}
	defer wipe(key)
	if e = s.backend.Probe(ctx); e != nil {
		return e
	}
	data, e := s.backend.Wrap(ctx, key)
	if e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		if cleaner, ok := s.backend.(recordRemover); ok {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = cleaner.Remove(cleanup, data)
		}
		return e
	}
	h := sha256.Sum256(key)
	r := record{1, hex.EncodeToString(h[:]), data}
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".device-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(tmp, path(dir)); e != nil {
		return e
	}
	if cleaner, ok := s.backend.(recordRemover); ok && len(previous.Data) > 0 {
		if e = cleaner.Remove(ctx, previous.Data); e != nil {
			return errors.New("系统验证已更新，但旧钥匙串条目清理失败")
		}
	}
	return nil
}
func readRecord(dir string) (record, error) {
	f, e := os.Open(path(dir))
	if e != nil {
		return record{}, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return record{}, e
	}
	if !st.Mode().IsRegular() || st.Size() > 65536 || (runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0) {
		return record{}, errors.New("系统解锁记录的类型或权限不安全")
	}
	var r record
	if e = json.NewDecoder(io.LimitReader(f, 65537)).Decode(&r); e != nil {
		return record{}, e
	}
	if r.Version != 1 {
		return record{}, errors.New("系统解锁记录版本不支持")
	}
	return r, nil
}
func (s *Service) Open(ctx context.Context, dir, reason string) (*vault.Session, error) {
	return s.open(ctx, dir, reason, s.backend.Unwrap)
}

// SupportsEmbedded reports whether this platform hosts the biometric view in
// the application window. Hardware availability is still reported by Status.
func (s *Service) SupportsEmbedded() bool {
	_, ok := s.backend.(embeddedBackend)
	return ok
}

// OpenEmbedded uses the same record and vault checks as Open. Platforms without
// an embedded backend keep their existing owner-verification flow.
func (s *Service) OpenEmbedded(ctx context.Context, dir, reason string, presentation *EmbeddedPresentation) (*vault.Session, error) {
	b, ok := s.backend.(embeddedBackend)
	if !ok {
		return s.Open(ctx, dir, reason)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !presentation.Ready() {
		return nil, &EmbeddedError{Code: "notReady", Message: "窗口内指纹验证区域尚未就绪"}
	}
	return s.open(ctx, dir, reason, func(ctx context.Context, data json.RawMessage, reason string) ([]byte, error) {
		return b.UnwrapEmbedded(ctx, data, reason, presentation)
	})
}

func (s *Service) open(ctx context.Context, dir, reason string, unwrap func(context.Context, json.RawMessage, string) ([]byte, error)) (*vault.Session, error) {
	r, e := readRecord(dir)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	key, e := unwrap(ctx, r.Data, reason)
	defer wipe(key)
	if e != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("系统验证已取消或超时: %w", ctx.Err())
		}
		return nil, e
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("系统验证已取消或超时: %w", ctx.Err())
	}
	h := sha256.Sum256(key)
	if hex.EncodeToString(h[:]) != r.VaultHash {
		return nil, errors.New("系统保护的解锁密钥不匹配")
	}
	session, e := vault.ReadKey(filepath.Join(dir, "vault.json"), key)
	if ctx.Err() != nil {
		if session != nil {
			session.Close()
		}
		return nil, fmt.Errorf("系统验证已取消或超时: %w", ctx.Err())
	}
	return session, e
}
func (s *Service) Disable(dir string, pass []byte) error {
	v, e := vault.Read(filepath.Join(dir, "vault.json"), pass)
	if e != nil {
		return e
	}
	v.Close()
	if cleaner, ok := s.backend.(recordRemover); ok {
		r, err := readRecord(dir)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if e = cleaner.Remove(ctx, r.Data); e != nil {
			return e
		}
	}
	e = os.Remove(path(dir))
	if os.IsNotExist(e) {
		return nil
	}
	return e
}
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
