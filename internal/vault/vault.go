// Package vault encrypts connection profiles. It never exposes credentials to the AI protocol.
package vault

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"sshclient/internal/accesspolicy"

	"golang.org/x/crypto/argon2"
)

const Version = 1
const maxFileSize = 32 << 20

var ErrUnlock = errors.New("保险库解锁失败：密码不正确或文件损坏")

type Profile struct {
	Policy           accesspolicy.Policy `json:"automationPolicy,omitempty"`
	Group            string              `json:"group,omitempty"`
	ID               string              `json:"id"`
	Name             string              `json:"name"`
	Host             string              `json:"host"`
	Port             int                 `json:"port"`
	User             string              `json:"user"`
	Fingerprint      string              `json:"fingerprint"`
	AgentFingerprint string              `json:"agentFingerprint,omitempty"`
	Auth             string              `json:"auth"` // password, key, or agent
	Password         string              `json:"password,omitempty"`
	PrivateKey       string              `json:"privateKey,omitempty"`
	KeyPassphrase    string              `json:"keyPassphrase,omitempty"`
}

// A replacement is explicit; blank secret fields never silently erase stored
// credentials when an owner edits only connection metadata.
type ProfileChange struct {
	Profile        Profile `json:"profile"`
	KeepCredential bool    `json:"keepCredential"`
}

func (s *Session) ResolveChange(change ProfileChange) (Profile, error) {
	p := cloneProfile(change.Profile)
	if change.KeepCredential {
		old, err := s.Profile(p.ID)
		if err != nil {
			return Profile{}, err
		}
		if p.Auth != old.Auth {
			return Profile{}, errors.New("更换登录方式时需要提供新的登录凭据")
		}
		p.Password, p.PrivateKey, p.KeyPassphrase, p.AgentFingerprint = old.Password, old.PrivateKey, old.KeyPassphrase, old.AgentFingerprint
	}
	return p, Validate(p)
}

type Header struct {
	Version int    `json:"version"`
	Salt    []byte `json:"salt"`
	ID      []byte `json:"id"`
}
type envelope struct {
	Header     Header `json:"header"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}
type payload struct {
	Profiles []Profile             `json:"profiles"`
	Snippets []Snippet             `json:"snippets,omitempty"`
	Notes    map[string]ServerNote `json:"serverNotes,omitempty"`
	// AuditHead is the last audit record the broker saw before it closed. It
	// lives inside the encrypted vault on purpose: a log that was rewritten whole
	// no longer agrees with it, and rewriting this copy needs the master password.
	AuditHead string `json:"auditHead,omitempty"`
}
type Session struct {
	mu        sync.Mutex
	header    Header
	key       []byte
	profiles  []Profile
	snippets  []Snippet
	notes     map[string]ServerNote
	auditHead string
	closed    bool
}

func derive(pass []byte, salt []byte) []byte { return argon2.IDKey(pass, salt, 3, 64*1024, 1, 32) }
func random(n int) []byte {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic(err)
	}
	return b
}
func New(pass []byte) (*Session, error) {
	if len(pass) < 12 {
		return nil, errors.New("主密码至少12字节，请使用较长的独立口令")
	}
	h := Header{Version: Version, Salt: random(16), ID: random(16)}
	return &Session{header: h, key: derive(pass, h.Salt), profiles: []Profile{}}, nil
}
func Open(data, pass []byte) (*Session, error) {
	var e envelope
	if len(data) > maxFileSize || json.Unmarshal(data, &e) != nil || len(e.Header.Salt) != 16 {
		return nil, ErrUnlock
	}
	key := derive(pass, e.Header.Salt)
	defer wipe(key)
	return OpenKey(data, key)
}

// OpenKey is used only by an owner frontend after OS-protected key release.
// There is no public broker API exporting this key.
func OpenKey(data, key []byte) (*Session, error) {
	if len(data) > maxFileSize {
		return nil, ErrUnlock
	}
	var e envelope
	if json.Unmarshal(data, &e) != nil || e.Header.Version != Version || len(e.Header.Salt) != 16 || len(e.Header.ID) != 16 || len(e.Nonce) != 12 {
		return nil, ErrUnlock
	}
	if len(key) != 32 {
		return nil, ErrUnlock
	}
	key = append([]byte(nil), key...)
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	aad, _ := json.Marshal(e.Header)
	plain, err := aead.Open(nil, e.Nonce, e.Ciphertext, aad)
	if err != nil {
		wipe(key)
		return nil, ErrUnlock
	}
	defer wipe(plain)
	var p payload
	if json.Unmarshal(plain, &p) != nil {
		wipe(key)
		return nil, ErrUnlock
	}
	return &Session{header: e.Header, key: key, profiles: p.Profiles, snippets: p.Snippets, notes: p.Notes, auditHead: p.AuditHead}, nil
}
func (s *Session) Seal() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("保险库已锁定")
	}
	plain, err := json.Marshal(payload{Profiles: s.profiles, Snippets: s.snippets, Notes: s.notes, AuditHead: s.auditHead})
	if err != nil {
		return nil, err
	}
	defer wipe(plain)
	block, _ := aes.NewCipher(s.key)
	aead, _ := cipher.NewGCM(block)
	nonce := random(aead.NonceSize())
	aad, _ := json.Marshal(s.header)
	return json.Marshal(envelope{Header: s.header, Nonce: nonce, Ciphertext: aead.Seal(nil, nonce, plain, aad)})
}
func Validate(p Profile) error {
	if err := p.Policy.Validate(); err != nil {
		return err
	}
	if len(p.Group) > 128 || strings.ContainsAny(p.Group, "\x00\r\n") {
		return errors.New("分组名称无效")
	}
	if len(p.ID) > 128 || len(p.Name) > 256 || len(p.Host) > 255 || len(p.User) > 128 || len(p.Password) > 1<<20 || len(p.PrivateKey) > 1<<20 || len(p.KeyPassphrase) > 1<<20 {
		return errors.New("连接字段过长")
	}
	if p.ID == "" || p.Name == "" || p.Host == "" || p.User == "" || p.Port < 1 || p.Port > 65535 {
		return errors.New("连接名称、地址、用户和端口不完整")
	}
	if bytes.ContainsAny([]byte(p.Host), "\x00\r\n") || bytes.ContainsAny([]byte(p.User), "\x00\r\n") {
		return errors.New("连接字段格式错误")
	}
	if p.Fingerprint == "" {
		return errors.New("必须先核对并保存服务器SHA256主机指纹")
	}
	switch p.Auth {
	case "password":
		if p.Password == "" {
			return errors.New("未提供登录密码")
		}
	case "key":
		if p.PrivateKey == "" {
			return errors.New("未提供私钥")
		}
	case "agent":
		if p.AgentFingerprint == "" {
			return errors.New("请明确选择SSH代理中的公钥指纹")
		}
	default:
		return errors.New("不支持的登录方式")
	}
	return nil
}

type Summary struct {
	Policy           accesspolicy.Policy `json:"automationPolicy,omitempty"`
	ID               string              `json:"id"`
	Name             string              `json:"name"`
	Group            string              `json:"group"`
	Host             string              `json:"host"`
	Port             int                 `json:"port"`
	User             string              `json:"user"`
	Auth             string              `json:"auth"`
	Fingerprint      string              `json:"fingerprint"`
	AgentFingerprint string              `json:"agentFingerprint,omitempty"`
}

func (s *Session) Summaries() []Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	out := make([]Summary, 0, len(s.profiles))
	for _, p := range s.profiles {
		out = append(out, Summary{ID: p.ID, Name: p.Name, Group: p.Group, Host: p.Host, Port: p.Port, User: p.User, Auth: p.Auth, Fingerprint: p.Fingerprint, AgentFingerprint: p.AgentFingerprint, Policy: p.Policy.Clone()})
	}
	return out
}

// AuditHead is the last audit record this vault saw the broker write.
func (s *Session) AuditHead() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.auditHead
}

// SetAuditHead records the last audit record, to be written on the next save.
func (s *Session) SetAuditHead(head string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auditHead = head
}

func (s *Session) UnlockKey() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrUnlock
	}
	return append([]byte(nil), s.key...), nil
}

// SaveDetails preserves the stored credential while updating non-secret fields.
func (s *Session) SaveDetails(file string, d Summary) error {
	p, e := s.Profile(d.ID)
	if e != nil {
		return e
	}
	p.Name = d.Name
	p.Group = d.Group
	p.Host = d.Host
	p.Port = d.Port
	p.User = d.User
	p.Fingerprint = d.Fingerprint
	p.Policy = d.Policy.Clone()
	return s.SaveProfile(file, p)
}
func (s *Session) Put(p Profile) error {
	p = cloneProfile(p)
	if err := Validate(p); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("保险库已锁定")
	}
	for i := range s.profiles {
		if s.profiles[i].ID == p.ID {
			s.profiles[i] = p
			return nil
		}
	}
	s.profiles = append(s.profiles, p)
	return nil
}

// SaveProfile commits to disk before publishing new credentials in memory.
func (s *Session) SaveProfile(file string, p Profile) error {
	p = cloneProfile(p)
	if e := Validate(p); e != nil {
		return e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("保险库已锁定")
	}
	profiles := append([]Profile(nil), s.profiles...)
	found := false
	for i := range profiles {
		if profiles[i].ID == p.ID {
			profiles[i] = p
			found = true
			break
		}
	}
	if !found {
		if len(profiles) >= 1024 {
			return errors.New("连接数量超过上限")
		}
		profiles = append(profiles, p)
	}
	copy := &Session{header: s.header, key: s.key, profiles: profiles, snippets: s.snippets, notes: s.notes}
	if e := Save(file, copy, false); e != nil {
		return e
	}
	s.profiles = profiles
	return nil
}

// Profile is internal authority access, never a public RPC return value.
func (s *Session) Profile(id string) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Profile{}, errors.New("保险库已锁定")
	}
	for _, p := range s.profiles {
		if p.ID == id {
			return cloneProfile(p), nil
		}
	}
	return Profile{}, errors.New("连接不存在")
}

func cloneProfile(p Profile) Profile {
	p.Policy = p.Policy.Clone()
	return p
}

// Names deliberately omits passwords, key material and passphrases.
func (s *Session) Names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	out := make([]string, 0, len(s.profiles))
	for _, p := range s.profiles {
		out = append(out, p.ID+" · "+p.Name)
	}
	return out
}
func (s *Session) Proof(message []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("保险库已锁定")
	}
	k := hmac.New(sha256.New, s.key)
	k.Write([]byte("ssh-client/owner-approval/v1"))
	k.Write(s.header.ID)
	owner := k.Sum(nil)
	defer wipe(owner)
	h := hmac.New(sha256.New, owner)
	h.Write(message)
	return h.Sum(nil), nil
}
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	wipe(s.key)
	s.key = nil
	s.profiles = nil
	s.snippets = nil
	s.notes = nil
	s.closed = true
}
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
	runtime.KeepAlive(b)
}
func Read(path string, pass []byte) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > maxFileSize {
		return nil, ErrUnlock
	}
	if runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0 {
		return nil, errors.New("保险库文件权限必须仅对当前用户开放")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	return Open(data, pass)
}
func Save(path string, s *Session, create bool) error {
	data, err := s.Seal()
	if err != nil {
		return err
	}
	if len(data) > maxFileSize {
		return errors.New("保险库容量超过上限，未写入更改")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if create {
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = f.Write(data)
		if e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		if e != nil {
			return e
		}
		return ce
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".vault-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	return os.Rename(name, path)
}
func ShortID() string            { return base64.RawURLEncoding.EncodeToString(random(18)) }
func (p Profile) String() string { return fmt.Sprintf("%s (%s@%s:%d)", p.Name, p.User, p.Host, p.Port) }

func ReadKey(path string, key []byte) (*Session, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() || st.Size() > maxFileSize || (runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0) {
		return nil, ErrUnlock
	}
	b, e := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if e != nil {
		return nil, e
	}
	return OpenKey(b, key)
}
