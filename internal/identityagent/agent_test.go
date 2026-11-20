package identityagent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"sshclient/internal/identitymodel"
)

// Synthetic software keys exercise the complete agent interface. These tests
// never start the native worker or access authentication UI or Keychain.
type fakeBackend struct {
	private            *ecdsa.PrivateKey
	mu                 sync.Mutex
	calls              []command
	block, corrupt     bool
	started, cancelled chan struct{}
}

func newFakeBackend(t *testing.T) *fakeBackend {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeBackend{private: k, started: make(chan struct{}, 8), cancelled: make(chan struct{}, 8)}
}
func (b *fakeBackend) call(ctx context.Context, c command) (reply, error) {
	b.mu.Lock()
	b.calls = append(b.calls, c)
	b.mu.Unlock()
	if c.Action == "list" {
		return reply{Supported: true, Keys: []nativeKey{{ID: "synthetic", Name: "Offline key", Authentication: "presence", Public: elliptic.Marshal(elliptic.P256(), b.private.X, b.private.Y)}}}, nil
	}
	if c.Action != "sign" {
		return reply{}, nil
	}
	if b.block {
		b.started <- struct{}{}
		<-ctx.Done()
		b.cancelled <- struct{}{}
		return reply{}, ctx.Err()
	}
	sum := sha256.Sum256(c.Data)
	r, s, err := ecdsa.Sign(rand.Reader, b.private, sum[:])
	if err != nil {
		return reply{}, err
	}
	encoded := make([]byte, 64)
	r.FillBytes(encoded[:32])
	s.FillBytes(encoded[32:])
	if b.corrupt {
		clear(encoded)
	}
	return reply{Signature: encoded}, nil
}
func (*fakeBackend) Close() error { return nil }
func startFake(t *testing.T, b backend, observers ...func(*SignEvent) error) (*Server, string) {
	t.Helper()
	base := filepath.Join("..", "..", "build", "test-fixtures")
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(base, "id-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	var observer func(*SignEvent) error
	if len(observers) > 0 {
		observer = observers[0]
	}
	s, err := openAudited(dir, b, observer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, identitymodel.SocketPath(dir)
}
func rawAgentConnection(t *testing.T, path string) net.Conn {
	t.Helper()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(5 * time.Second))
	return c
}
func agentClient(t *testing.T, path string) (agent.ExtendedAgent, net.Conn) {
	c := rawAgentConnection(t, path)
	return agent.NewClient(c), c
}
func TestAgentSignsExactPayloadAndRejectsManagement(t *testing.T) {
	b := newFakeBackend(t)
	s, path := startFake(t, b)
	client, _ := agentClient(t, path)
	state, err := s.State(context.Background())
	if err != nil || len(state.Keys) != 1 || state.Keys[0].Fingerprint == "" {
		t.Fatal(state, err)
	}
	keys, err := client.List()
	if err != nil || len(keys) != 1 {
		t.Fatal(keys, err)
	}
	public, err := ssh.ParsePublicKey(keys[0].Blob)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("offline signed payload")
	sig, err := client.Sign(public, data)
	if err != nil {
		t.Fatal(err)
	}
	if err = public.Verify(data, sig); err != nil {
		t.Fatal(err)
	}
	if public.Verify([]byte("different payload"), sig) == nil {
		t.Fatal("signature covered different data")
	}
	if _, err = client.SignWithFlags(public, data, agent.SignatureFlagRsaSha256); err == nil {
		t.Fatal("accepted incompatible flags")
	}
	other := newFakeBackend(t)
	otherPublic, _ := publicKey(nativeKey{Public: elliptic.Marshal(elliptic.P256(), other.private.X, other.private.Y)})
	if _, err = client.Sign(otherPublic, data); err == nil {
		t.Fatal("signed an unknown identity")
	}
	for _, err = range []error{client.Add(agent.AddedKey{PrivateKey: other.private}), client.Remove(public), client.RemoveAll(), client.Lock([]byte("synthetic")), client.Unlock([]byte("synthetic"))} {
		if err == nil {
			t.Fatal("management was accepted on public agent socket")
		}
	}
	if _, err = client.Extension("session-bind@openssh.com", nil); err == nil {
		t.Fatal("accepted unsupported extension")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	signs := 0
	for _, c := range b.calls {
		if c.Action == "sign" {
			signs++
			if c.PID != os.Getpid() || string(c.Data) != string(data) {
				t.Fatal("signature lost kernel provenance or payload")
			}
		} else if c.Action != "list" {
			t.Fatal("management reached the native worker", c.Action)
		}
	}
	if signs != 1 {
		t.Fatal("unexpected sign count", signs)
	}
}
func TestAgentRejectsInvalidNativeSignature(t *testing.T) {
	b := newFakeBackend(t)
	b.corrupt = true
	_, path := startFake(t, b)
	client, _ := agentClient(t, path)
	keys, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := ssh.ParsePublicKey(keys[0].Blob)
	if _, err = client.Sign(pub, []byte("must verify native signature")); err == nil {
		t.Fatal("returned invalid signature")
	}
}
func TestAgentDisconnectAndShutdownCancelAuthentication(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown"}[shutdown], func(t *testing.T) {
			b := newFakeBackend(t)
			b.block = true
			s, path := startFake(t, b)
			client, c := agentClient(t, path)
			keys, err := client.List()
			if err != nil {
				t.Fatal(err)
			}
			pub, _ := ssh.ParsePublicKey(keys[0].Blob)
			done := make(chan struct{})
			go func() { defer close(done); client.Sign(pub, []byte("pending offline auth")) }()
			select {
			case <-b.started:
			case <-time.After(3 * time.Second):
				t.Fatal("sign did not reach backend")
			}
			if shutdown {
				s.Close()
			} else {
				c.Close()
			}
			select {
			case <-b.cancelled:
			case <-time.After(3 * time.Second):
				t.Fatal("authentication survived client/agent shutdown")
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("client remained blocked")
			}
		})
	}
}

func TestAgentPipelinedRequestsCanReachBatchAuthentication(t *testing.T) {
	b := newFakeBackend(t)
	b.block = true
	_, path := startFake(t, b)
	client, c := agentClient(t, path)
	keys, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := ssh.ParsePublicKey(keys[0].Blob)
	var done sync.WaitGroup
	for i := 0; i < 2; i++ {
		done.Add(1)
		go func() { defer done.Done(); client.Sign(pub, []byte("offline concurrent auth")) }()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-b.started:
		case <-time.After(3 * time.Second):
			t.Fatal("second request was blocked behind the first authentication")
		}
	}
	c.Close()
	done.Wait()
	for i := 0; i < 2; i++ {
		select {
		case <-b.cancelled:
		case <-time.After(3 * time.Second):
			t.Fatal("pipelined authentication survived disconnect")
		}
	}
}

type reorderedBackend struct {
	*fakeBackend
	first, second, release chan struct{}
}

func (b *reorderedBackend) call(ctx context.Context, c command) (reply, error) {
	if c.Action == "sign" && string(c.Data) == "first" {
		close(b.first)
		select {
		case <-b.release:
		case <-ctx.Done():
			return reply{}, ctx.Err()
		}
	}
	if c.Action == "sign" && string(c.Data) == "second" {
		close(b.second)
	}
	return b.fakeBackend.call(ctx, c)
}
func TestAgentPipelinedSignaturesKeepRequestOrder(t *testing.T) {
	b := &reorderedBackend{fakeBackend: newFakeBackend(t), first: make(chan struct{}), second: make(chan struct{}), release: make(chan struct{})}
	_, path := startFake(t, b)
	client, _ := agentClient(t, path)
	keys, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := ssh.ParsePublicKey(keys[0].Blob)
	type result struct {
		sig *ssh.Signature
		err error
	}
	first, second := make(chan result, 1), make(chan result, 1)
	go func() { sig, err := client.Sign(pub, []byte("first")); first <- result{sig, err} }()
	select {
	case <-b.first:
	case <-time.After(3 * time.Second):
		t.Fatal("first request did not start")
	}
	go func() { sig, err := client.Sign(pub, []byte("second")); second <- result{sig, err} }()
	select {
	case <-b.second:
	case <-time.After(3 * time.Second):
		t.Fatal("pipelined request did not execute concurrently")
	}
	close(b.release)
	for _, item := range []struct {
		data string
		ch   <-chan result
	}{{"first", first}, {"second", second}} {
		select {
		case r := <-item.ch:
			if r.err != nil {
				t.Fatal(r.err)
			}
			if err := pub.Verify([]byte(item.data), r.sig); err != nil {
				t.Fatal("response was assigned to a different signing request", item.data, err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("signature response stalled")
		}
	}
}
func TestAgentBoundsAndRejectsMalformedPackets(t *testing.T) {
	b := newFakeBackend(t)
	_, path := startFake(t, b)
	for _, packet := range [][]byte{{0, 0, 0, 0}, {0, 16, 0, 1}, {0, 0, 0, 2, 11, 0}, {0, 0, 0, 2, 13, 0}} {
		c := rawAgentConnection(t, path)
		if _, err := c.Write(packet); err != nil {
			t.Fatal(err)
		}
		var header [4]byte
		_, err := io.ReadFull(c, header[:])
		if binary.BigEndian.Uint32(packet[:4]) > 1<<20 || binary.BigEndian.Uint32(packet[:4]) == 0 {
			if err == nil {
				t.Fatal("invalid frame was accepted")
			}
		} else {
			if err != nil {
				t.Fatal(packet, err)
			}
			if binary.BigEndian.Uint32(header[:]) != 1 {
				t.Fatal("invalid failure frame", packet, header)
			}
			var body [1]byte
			if _, err = io.ReadFull(c, body[:]); err != nil || body[0] != 5 {
				t.Fatal(body, err)
			}
		}
		c.Close()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.calls) != 0 {
		t.Fatal("malformed packet reached native worker")
	}
}
func TestIdentityOwnerChangeValidation(t *testing.T) {
	b := newFakeBackend(t)
	s, _ := startFake(t, b)
	for _, c := range []identitymodel.Change{{Action: "sign"}, {Action: "create", Name: "A", Authentication: "invalid"}, {Action: "persist", ID: "synthetic", Seconds: 59}, {Action: "delete"}} {
		if s.Change(context.Background(), c) == nil {
			t.Fatal("accepted invalid owner change", c)
		}
	}
	if err := s.Change(context.Background(), identitymodel.Change{Action: "persist", ID: "synthetic", Seconds: 300}); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.calls) != 1 || b.calls[0].Action != "persist" {
		t.Fatal("invalid owner action reached native worker")
	}
}
func TestSigningTargetUsesSignedHostBoundPayload(t *testing.T) {
	b := newFakeBackend(t)
	pub, _ := publicKey(nativeKey{Public: elliptic.Marshal(elliptic.P256(), b.private.X, b.private.Y)})
	request := struct {
		Session               []byte
		Message               byte
		User, Service, Method string
		Signed                bool
		Algorithm             string
		Key, Host             []byte
	}{[]byte("synthetic session"), 50, "tester", "ssh-connection", "publickey-hostbound-v00@openssh.com", true, pub.Type(), pub.Marshal(), pub.Marshal()}
	target := signingTarget(ssh.Marshal(request))
	fingerprint := ssh.FingerprintSHA256(pub)
	if target.Batch != "connection:tester:"+fingerprint || target.Kind != "login" || target.Detail != "tester" || target.Host != fingerprint {
		t.Fatal(target)
	}
	request.Method = "publickey"
	request.Host = nil
	if unbound := signingTarget(ssh.Marshal(request)); unbound.Kind != "login" || unbound.Detail != "tester" || unbound.Host != "" || unbound.Batch != "" {
		t.Fatal("plain SSH login did not preserve the account and mark the host unknown", unbound)
	}
	if unknown := signingTarget([]byte("untrusted unknown purpose")); unknown.Batch != "" || unknown.Kind != "" || unknown.Host != "" {
		t.Fatal("unknown purpose gained display metadata or a batch", unknown)
	}
}
