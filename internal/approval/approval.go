// Package approval implements immutable operations and expiring, one-use requests. A caller
// cannot approve its own request without an owner proof derived from the vault.
// Renewable sessions retain their ID but require a fresh signed nonce generation.
package approval

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sshclient/internal/fileattrs"
	"sshclient/internal/serverops"
	"sync"
	"time"
)

var ErrDenied = errors.New("请求未获批准、已到期或已撤销")

type Operation struct {
	Docker          serverops.DockerRequest  `json:"docker,omitzero"`
	Compose         serverops.ComposeRequest `json:"compose,omitzero"`
	Attributes      fileattrs.Change         `json:"attributes,omitzero"`
	Grant           GrantScope               `json:"grant,omitzero"`
	ServerStatus    bool                     `json:"serverStatus,omitempty"`
	TransferID      string                   `json:"transferId,omitempty"`
	Size            int64                    `json:"size,omitempty"`
	Offset          int64                    `json:"offset,omitempty"`
	PrefixHash      string                   `json:"prefixHash,omitempty"`
	Host            string                   `json:"host"`
	Kind            string                   `json:"kind"`
	Command         string                   `json:"command,omitempty"`
	Path            string                   `json:"path,omitempty"`
	Destination     string                   `json:"destination,omitempty"`
	Mode            uint32                   `json:"mode,omitempty"`
	ContentHash     string                   `json:"contentHash,omitempty"`
	OriginalHash    string                   `json:"originalHash,omitempty"`
	Endpoint        string                   `json:"endpoint,omitempty"`
	HostFingerprint string                   `json:"hostFingerprint,omitempty"`
	PathProtection  bool                     `json:"pathProtection,omitempty"`
	Client          string                   `json:"client"`
}
type Request struct {
	Renewal bool `json:"renewal,omitempty"`
	// Renewals counts how many times this grant has been extended. An authority
	// that keeps being extended is worth seeing in the review and in the log.
	Renewals     int       `json:"renewals,omitempty"`
	ParentID     string    `json:"parentId,omitempty"`
	OwnerIssued  bool      `json:"ownerIssued,omitempty"`
	OwnerSession bool      `json:"ownerSession,omitempty"`
	ID           string    `json:"id"`
	Nonce        string    `json:"nonce"`
	Operation    Operation `json:"operation"`
	Expires      time.Time `json:"expires"`
	Status       string    `json:"status"`
}
type entry struct {
	// bucket is the caller identity the broker established for this request.
	bucket      string
	renewedFrom string
	dormantAt   time.Time
	lastUsed    time.Time
	request     Request
	ticketHash  [32]byte
	used        bool
	cancel      context.CancelFunc
}
type Manager struct {
	expired     []Request
	dormant     map[string]*entry
	renewals    map[string]RenewalChallenge
	delegations map[string]DelegationChallenge
	mu          sync.Mutex
	pending     map[string]*entry
	prove       func([]byte) ([]byte, error)
	now         func() time.Time
}

func New(prove func([]byte) ([]byte, error)) *Manager {
	return NewWithClock(prove, time.Now)
}

// NewWithClock supplies a concurrency-safe clock, useful for deterministic
// expiry tests. Production brokers use the default monotonic wall clock.
func NewWithClock(prove func([]byte) ([]byte, error), now func() time.Time) *Manager {
	return &Manager{pending: map[string]*entry{}, dormant: map[string]*entry{}, prove: prove, now: now}
}
func token() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// maxPendingPerClient bounds one caller's share of the approval queue.
const maxPendingPerClient = 16

func canonical(r Request) []byte {
	r.Status = ""
	b, _ := json.Marshal(r)
	return append([]byte("ssh-client/request/v1\x00"), b...)
}
func (m *Manager) Create(op Operation, ttl time.Duration) (Request, string, error) {
	return m.create(op, ttl, false, "")
}

// CreateFrom is Create for a caller the broker could identify itself, such as the
// process on the other end of the local socket. Its share of the queue is counted
// by that identity, so renaming the caller does not buy another sixteen slots.
func (m *Manager) CreateFrom(op Operation, ttl time.Duration, bucket string) (Request, string, error) {
	return m.create(op, ttl, false, bucket)
}

// CreateOwner is reached only after the broker verifies a signed one-use
// owner challenge. Caller labels cannot select this scope.
func (m *Manager) CreateOwner(op Operation) (Request, string, error) {
	return m.create(op, 15*time.Minute, true, "")
}

func (m *Manager) create(op Operation, ttl time.Duration, owner bool, bucket string) (Request, string, error) {
	if err := validateFields(op); err != nil {
		return Request{}, "", err
	}
	if len(op.Command) > 32768 || len(op.Path) > 4096 || len(op.Destination) > 4096 || len(op.Client) > 128 || len(op.Host) > 128 {
		return Request{}, "", errors.New("操作字段超过上限")
	}
	if ttl <= 0 || ttl > 15*time.Minute || op.Host == "" || op.Client == "" {
		return Request{}, "", errors.New("授权需指定服务器、申请方和不超过15分钟的期限")
	}
	if op.ServerStatus && (op.Kind != "shell" && op.Kind != "docker-terminal" || op.Command != "") {
		return Request{}, "", errors.New("自动服务器状态仅用于终端连接，并且只运行固定读取命令")
	}
	if op.Kind != "exec" && op.Command != "" {
		return Request{}, "", errors.New("仅执行命令操作可以包含命令内容")
	}
	if op.Kind != "file-attributes" && op.Attributes != (fileattrs.Change{}) {
		return Request{}, "", errors.New("仅属性修改操作可以携带文件属性变更")
	}
	if op.Kind != "docker-logs" && op.Kind != "docker-terminal" && op.Docker != (serverops.DockerRequest{}) {
		return Request{}, "", errors.New("普通操作不能携带 Docker 目标")
	}
	if op.Kind != "compose-plan" && op.Kind != "compose-update" && op.Compose != (serverops.ComposeRequest{}) {
		return Request{}, "", errors.New("普通操作不能携带 Compose 目标")
	}
	switch op.Kind {
	case "docker-logs", "docker-terminal":
		if err := op.Docker.Validate(op.Kind); err != nil {
			return Request{}, "", err
		}
	case "compose-plan", "compose-update":
		if err := op.Compose.Validate(op.Kind == "compose-update"); err != nil {
			return Request{}, "", err
		}
	case "file-attributes":
		if op.Path == "" || op.Path == "/" || op.Mode != 0 || op.Destination != "" {
			return Request{}, "", errors.New("文件属性操作目标无效")
		}
		if err := op.Attributes.Validate(); err != nil {
			return Request{}, "", err
		}
	case "grant":
		if owner || op.Command != "" || op.Path != "" || op.Destination != "" || op.ServerStatus || op.ContentHash != "" || op.OriginalHash != "" || op.TransferID != "" || op.Size != 0 || op.Offset != 0 || op.Mode != 0 {
			return Request{}, "", errors.New("会话授权只能指定服务器和授权范围")
		}
		if err := op.Grant.Validate(); err != nil {
			return Request{}, "", err
		}
	case "mkdir", "rename", "remove", "chmod":
		if op.Path == "" || op.Path == "/" || (op.Kind == "rename" && op.Destination == "") || op.Mode > 0777 {
			return Request{}, "", errors.New("文件操作目标、名称或权限无效")
		}
	case "exec":
		if op.Command == "" {
			return Request{}, "", errors.New("命令不能为空")
		}
	case "put-resume", "get-resume":
		if op.Path == "" || op.TransferID == "" || op.Offset < 0 || op.Size < 0 {
			return Request{}, "", errors.New("续传参数不完整")
		}
		if op.Kind == "put-resume" && op.ContentHash == "" {
			return Request{}, "", errors.New("上传需要内容指纹")
		}
	case "list", "file-info", "get", "put", "replace":
		if op.Path == "" {
			return Request{}, "", errors.New("路径不能为空")
		}
		if (op.Kind == "put" || op.Kind == "replace") && op.ContentHash == "" {
			return Request{}, "", errors.New("上传需绑定文件SHA256")
		}
		if op.Kind == "replace" && op.OriginalHash == "" {
			return Request{}, "", errors.New("覆盖保存需绑定原文件SHA256")
		}
	case "shell", "monitor":
		if op.Kind == "monitor" && op.Command != "" {
			return Request{}, "", errors.New("监控仅运行固定采样命令")
		}
	default:
		return Request{}, "", errors.New("不支持的操作")
	}
	if op.Kind != "grant" && op.Grant != (GrantScope{}) {
		return Request{}, "", errors.New("普通操作不能携带会话授权")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	if len(m.pending) >= 128 {
		// Finished history must not block normal directory browsing or transfers.
		// Pending, approved and running authority is never evicted.
		oldest := ""
		var expires time.Time
		for id, e := range m.pending {
			switch e.request.Status {
			case "finished", "denied", "revoked", "withdrawn":
				if oldest == "" || e.request.Expires.Before(expires) {
					oldest, expires = id, e.request.Expires
				}
			}
		}
		if oldest == "" {
			return Request{}, "", errors.New("待处理授权过多")
		}
		delete(m.pending, oldest)
	}
	// One caller cannot fill the queue and push everyone else's request out of
	// the owner's view. The bucket is the caller identity the broker established;
	// without one, the label it reports is all there is.
	queued := 0
	for _, e := range m.pending {
		if e.request.Status != "pending" {
			continue
		}
		same := e.bucket == bucket && e.request.Operation.Client == op.Client
		if bucket != "" {
			same = e.bucket == bucket
		}
		if same {
			queued++
			if queued >= maxPendingPerClient {
				return Request{}, "", errors.New("该申请方待处理请求过多，请先处理或撤销")
			}
		}
	}
	r := Request{OwnerIssued: owner, OwnerSession: owner && (op.Kind == "shell" || op.Kind == "docker-terminal" || op.Kind == "docker-logs" && op.Docker.Follow), ID: token(), Nonce: token(), Operation: op, Expires: m.now().Add(ttl).UTC(), Status: "pending"}
	t := token()
	m.pending[r.ID] = &entry{request: r, ticketHash: sha256.Sum256([]byte(t)), bucket: bucket}
	return r, t, nil
}
func (m *Manager) prune() {
	for id, e := range m.pending {
		if e.request.Operation.Kind == "grant" && (!m.now().Before(e.request.Expires) || e.request.Status == "approved" && !m.now().Before(e.lastUsed.Add(time.Duration(e.request.Operation.Grant.IdleSeconds)*time.Second))) {
			if e.request.Status == "pending" || e.request.Status == "approved" {
				r := e.request
				r.Status = "expired"
				m.expired = append(m.expired, r)
			}
			m.cancelChildren(id)
			delete(m.pending, id)
			// Preserve the old approved scope after natural expiry or an
			// unanswered renewal. Denial and revocation never enable renewal.
			if e.request.Status == "approved" || e.request.Status == "pending" && e.request.Renewal {
				e.request.Status = "expired"
				e.dormantAt = m.now()
				m.dormant[id] = e
			}
			continue
		}
		if !(e.request.OwnerSession && e.request.Status == "running") && !m.now().Before(e.request.Expires) {
			if e.request.Status == "pending" || e.request.Status == "approved" || e.request.Status == "running" {
				r := e.request
				r.Status = "expired"
				m.expired = append(m.expired, r)
			}
			if e.cancel != nil {
				e.cancel()
			}
			delete(m.pending, id)
		}
	}
	m.pruneRenewals()
}

// DrainExpired reports each authority expiry once, including expiries noticed
// by another manager method before the broker's next scheduled sweep.
func (m *Manager) DrainExpired() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := m.expired
	m.expired = nil
	return rows
}

// Configuration changes invalidate only grants pinned to the changed host.
func (m *Manager) InvalidateHost(host string) {
	m.invalidateHost(host, false)
}

// Policy-only edits revoke untrusted authority while keeping the owner's
// existing terminal available. Endpoint/credential changes still revoke both.
func (m *Manager) InvalidateAutomationHost(host string) {
	m.invalidateHost(host, true)
}

func (m *Manager) invalidateHost(host string, automationOnly bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, e := range m.pending {
		if e.request.Operation.Host == host && (!automationOnly || !e.request.OwnerIssued) {
			if e.cancel != nil {
				e.cancel()
			}
			delete(m.pending, id)
		}
	}
	for id, e := range m.dormant {
		if e.request.Operation.Host == host {
			delete(m.dormant, id)
		}
	}
	for id, c := range m.renewals {
		if c.Operation.Host == host {
			delete(m.renewals, id)
		}
	}
	for id, c := range m.delegations {
		if c.Operation.Host == host {
			delete(m.delegations, id)
		}
	}
}
func (m *Manager) List() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	out := make([]Request, 0, len(m.pending))
	for _, e := range m.pending {
		out = append(out, e.request)
	}
	return out
}
func (m *Manager) Get(id string) (Request, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	e, ok := m.pending[id]
	if !ok {
		return Request{}, ErrDenied
	}
	return e.request, nil
}

// A ticket grants visibility only into its own operation, including after it
// was consumed for execution. It never grants global queue or host visibility.
func (m *Manager) GetForTicket(id, ticket string) (Request, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	e := m.pending[id]
	if e == nil {
		e = m.dormant[id]
	}
	h := sha256.Sum256([]byte(ticket))
	if e == nil || ticket == "" || !hmac.Equal(h[:], e.ticketHash[:]) {
		return Request{}, ErrDenied
	}
	return e.request, nil
}
func Proof(r Request, prove func([]byte) ([]byte, error)) ([]byte, error) { return prove(canonical(r)) }
func (m *Manager) Decide(id string, allow bool, proof []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	e, ok := m.pending[id]
	if !ok || e.request.Status != "pending" {
		return ErrDenied
	}
	message := append(canonical(e.request), byte(0))
	if allow {
		message[len(message)-1] = 1
	}
	expected, err := m.prove(message)
	if err != nil || !hmac.Equal(expected, proof) {
		return ErrDenied
	}
	if allow {
		e.request.Status = "approved"
		if e.request.Operation.Kind == "grant" {
			e.request.Expires = m.now().Add(time.Duration(e.request.Operation.Grant.LifetimeSeconds) * time.Second).UTC()
			e.lastUsed = m.now()
		}
	} else {
		e.request.Status = "denied"
	}
	return nil
}
func DecisionProof(r Request, allow bool, prove func([]byte) ([]byte, error)) ([]byte, error) {
	b := append(canonical(r), byte(0))
	if allow {
		b[len(b)-1] = 1
	}
	return prove(b)
}

// Start atomically consumes the ticket. The deadline also cancels work in progress.
func (m *Manager) Start(parent context.Context, id, ticket string) (context.Context, Operation, context.CancelFunc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	e, ok := m.pending[id]
	hash := sha256.Sum256([]byte(ticket))
	if !ok || e.used || e.request.Operation.Kind == "grant" || e.request.Status != "approved" || !hmac.Equal(hash[:], e.ticketHash[:]) {
		return nil, Operation{}, nil, ErrDenied
	}
	e.used = true
	var ctx context.Context
	var cancel context.CancelFunc
	if e.request.OwnerSession {
		ctx, cancel = context.WithCancel(parent)
	} else {
		ctx, cancel = context.WithDeadline(parent, e.request.Expires)
	}
	e.cancel = cancel
	e.request.Status = "running"
	return ctx, e.request.Operation, cancel, nil
}
func (m *Manager) Revoke(id string, proof []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	e, ok := m.pending[id]
	if !ok {
		e, ok = m.dormant[id]
	}
	if !ok {
		return ErrDenied
	}
	b := append(canonical(e.request), []byte("\x00revoke")...)
	expected, err := m.prove(b)
	if err != nil || !hmac.Equal(expected, proof) {
		return ErrDenied
	}
	e.request.Status = "revoked"
	m.cancelChildren(id)
	if e.cancel != nil {
		e.cancel()
	}
	return nil
}
func RevokeProof(r Request, prove func([]byte) ([]byte, error)) ([]byte, error) {
	return prove(append(canonical(r), []byte("\x00revoke")...))
}
func (m *Manager) Lock() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.pending {
		if e.cancel != nil {
			e.cancel()
		}
	}
	m.pending = map[string]*entry{}
	m.delegations = nil
	m.dormant = map[string]*entry{}
	m.renewals = nil
}

func (m *Manager) Finish(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.pending[id]; e != nil && e.request.Status == "running" {
		e.request.Status = "finished"
		if e.cancel != nil {
			e.cancel()
		}
	}
}

// Withdraw lets the ticket holder cancel its own operation or parent session.
// A parent's withdrawal ticket may be in its protected client session file;
// neither kind of ticket can approve or extend authority.
func (m *Manager) Withdraw(id, ticket string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	e, ok := m.pending[id]
	if !ok {
		e, ok = m.dormant[id]
	}
	h := sha256.Sum256([]byte(ticket))
	if !ok || !hmac.Equal(h[:], e.ticketHash[:]) {
		return ErrDenied
	}
	e.request.Status = "withdrawn"
	m.cancelChildren(id)
	if e.cancel != nil {
		e.cancel()
	}
	return nil
}

func pairingFingerprint(key []byte) string {
	h := sha256.Sum256(key)
	s := hex.EncodeToString(h[:8])
	return s[:4] + " " + s[4:8] + " " + s[8:12] + " " + s[12:]
}
