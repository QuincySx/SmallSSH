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
	"os"
	"path/filepath"
	"sshclient/internal/vault"
	"time"
)

type Status struct {
	Available    bool
	Name, Reason string
	Enabled      bool
}

const LinuxFingerprintName = "Linux 指纹认证"

var ErrFingerprintNotRecognized = errors.New("未识别指纹。请重试或使用保险库主密码。")

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
// nothing beside the sealed record to clean up.
type recordRemover interface {
	Remove(context.Context, json.RawMessage) error
}

func New(window uintptr) *Service { return &Service{backend: newBackend(window)} }

type record struct {
	Version   int             `json:"version"`
	VaultHash string          `json:"vaultHash"`
	Data      json.RawMessage `json:"data"`
}

// legacyPath is where releases before in-memory quick unlock kept the wrapped
// key. Any same-user process could read it and ask the OS to unwrap it with a
// prompt of its own wording, so it is only ever read to be deleted.
func legacyPath(dir string) string { return filepath.Join(dir, "device-unlock.json") }

// Status reports whether this machine can verify the owner. Whether a sealed
// record exists is the local service's business, not a file's.
func (s *Service) Status(ctx context.Context, _ string) Status {
	var st Status
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	e := s.backend.Probe(ctx)
	st.Name = s.backend.Name() // Probing is what learns whether it is really Touch ID.
	if e == nil {
		st.Available = true
	} else {
		st.Reason = e.Error()
	}
	return st
}

// Seal wraps the unlock key of an already unlocked vault with this machine's
// owner-verification key and returns the record. The caller hands it to the
// local service, which keeps it in memory only; nothing is written to disk.
func (s *Service) Seal(ctx context.Context, v *vault.Session) ([]byte, error) {
	return s.seal(ctx, "", v)
}

// SealForDirectory also records non-secret cleanup identifiers for backends
// whose device keys live outside the envelope. A new daemon can remove these
// after obtaining the data directory's exclusive service lock.
func (s *Service) SealForDirectory(ctx context.Context, dir string, v *vault.Session) ([]byte, error) {
	return s.seal(ctx, dir, v)
}

type directoryWrapper interface {
	WrapForDirectory(context.Context, string, []byte) (json.RawMessage, error)
}

func (s *Service) seal(ctx context.Context, dir string, v *vault.Session) ([]byte, error) {
	if v == nil {
		return nil, errors.New("保险库未解锁")
	}
	key, e := v.UnlockKey()
	if e != nil {
		return nil, e
	}
	defer wipe(key)
	if e = s.backend.Probe(ctx); e != nil {
		return nil, e
	}
	var data json.RawMessage
	if scoped, ok := s.backend.(directoryWrapper); ok && dir != "" {
		data, e = scoped.WrapForDirectory(ctx, dir, key)
	} else {
		data, e = s.backend.Wrap(ctx, key)
	}
	if e != nil {
		return nil, e
	}
	if e = ctx.Err(); e != nil {
		s.discardData(data)
		return nil, e
	}
	h := sha256.Sum256(key)
	return json.Marshal(record{1, hex.EncodeToString(h[:]), data})
}

// Discard releases what a record holds outside itself (the Linux keyring
// entry). Records of the other backends carry their own protected key.
func (s *Service) Discard(_ context.Context, sealed []byte) {
	r, e := parseRecord(sealed)
	if e != nil {
		return
	}
	s.discardData(r.Data)
}

func (s *Service) discardData(data json.RawMessage) {
	if cleaner, ok := s.backend.(recordRemover); ok && len(data) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = cleaner.Remove(ctx, data)
	}
}

// RemoveLegacy deletes a device-unlock.json left by an older release and its
// keyring entry, and reports whether one existed. It never unwraps it.
func (s *Service) RemoveLegacy(ctx context.Context, dir string) (bool, error) {
	st, e := os.Lstat(legacyPath(dir))
	if os.IsNotExist(e) {
		return false, nil
	}
	if e != nil {
		return true, e
	}
	if st.Mode().IsRegular() && st.Size() <= 65536 {
		if data, err := os.ReadFile(legacyPath(dir)); err == nil {
			if r, err := parseRecord(data); err == nil {
				if cleaner, ok := s.backend.(recordRemover); ok {
					_ = cleaner.Remove(ctx, r.Data)
				}
			}
			clear(data)
		}
	}
	if e = os.Remove(legacyPath(dir)); e != nil && !os.IsNotExist(e) {
		return true, e
	}
	return true, nil
}

func parseRecord(data []byte) (record, error) {
	if len(data) == 0 || len(data) > 65536 {
		return record{}, errors.New("系统解锁记录无效")
	}
	var r record
	if e := json.Unmarshal(data, &r); e != nil {
		return record{}, errors.New("系统解锁记录无效")
	}
	if r.Version != 1 || len(r.VaultHash) != 64 || len(r.Data) == 0 {
		return record{}, errors.New("系统解锁记录版本不支持")
	}
	return r, nil
}

// OpenRecord verifies the owner with the OS, unwraps the sealed key and opens
// the vault. The prompt names reason, which the desktop writes.
func (s *Service) OpenRecord(ctx context.Context, dir string, sealed []byte, reason string) (*vault.Session, error) {
	return s.open(ctx, dir, sealed, reason, s.backend.Unwrap)
}

// SupportsEmbedded reports whether this platform hosts the biometric view in
// the application window. Hardware availability is still reported by Status.
func (s *Service) SupportsEmbedded() bool {
	_, ok := s.backend.(embeddedBackend)
	return ok
}

// OpenRecordEmbedded uses the same record and vault checks as OpenRecord.
// Platforms without an embedded backend keep their existing verification flow.
func (s *Service) OpenRecordEmbedded(ctx context.Context, dir string, sealed []byte, reason string, presentation *EmbeddedPresentation) (*vault.Session, error) {
	b, ok := s.backend.(embeddedBackend)
	if !ok {
		return s.OpenRecord(ctx, dir, sealed, reason)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !presentation.Ready() {
		return nil, &EmbeddedError{Code: "notReady", Message: "窗口内指纹验证区域尚未就绪"}
	}
	return s.open(ctx, dir, sealed, reason, func(ctx context.Context, data json.RawMessage, reason string) ([]byte, error) {
		return b.UnwrapEmbedded(ctx, data, reason, presentation)
	})
}

func (s *Service) open(ctx context.Context, dir string, sealed []byte, reason string, unwrap func(context.Context, json.RawMessage, string) ([]byte, error)) (*vault.Session, error) {
	r, e := parseRecord(sealed)
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
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
