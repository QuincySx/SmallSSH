package identityagent

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// Only the two read/sign protocol messages are decoded. In particular, private
// key import, removal, locking and protocol extensions are never accepted here.
func serveProtocol(c net.Conn, original *connectionAgent) {
	ctx, cancel := context.WithCancel(original.ctx)
	a := *original
	a.ctx = ctx
	// Requests may await authentication concurrently, while responses retain
	// SSH Agent FIFO order. A single pipelined caller can therefore reach the
	// native batch review instead of blocking behind its first prompt.
	queue := make(chan (<-chan []byte), 8)
	slots := make(chan struct{}, 8)
	var workers sync.WaitGroup
	written := make(chan struct{})
	go func() {
		defer close(written)
		defer cancel()
		defer c.Close()
		for {
			var result <-chan []byte
			select {
			case <-ctx.Done():
				return
			case r, ok := <-queue:
				if !ok {
					return
				}
				result = r
			}
			var response []byte
			select {
			case <-ctx.Done():
				return
			case response = <-result:
			}
			if len(response) > 1<<20 {
				return
			}
			frame := make([]byte, 4, len(response)+4)
			binary.BigEndian.PutUint32(frame, uint32(len(response)))
			frame = append(frame, response...)
			_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if writeAll(c, frame) != nil {
				return
			}
			<-slots
		}
	}()
	defer func() { cancel(); c.Close(); workers.Wait(); close(queue); <-written }()
	for {
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Minute))
		var header [4]byte
		if _, err := io.ReadFull(c, header[:]); err != nil {
			return
		}
		n := binary.BigEndian.Uint32(header[:])
		if n == 0 || n > 1<<20 {
			return
		}
		body := make([]byte, int(n))
		if _, err := io.ReadFull(c, body); err != nil {
			return
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			clear(body)
			return
		}
		result := make(chan []byte, 1)
		select {
		case queue <- result:
		case <-ctx.Done():
			clear(body)
			return
		}
		workers.Add(1)
		go func() { defer workers.Done(); defer clear(body); result <- processAgentMessage(body, &a) }()
	}
}

func processAgentMessage(body []byte, a *connectionAgent) []byte {
	response := []byte{5} // SSH_AGENT_FAILURE
	switch body[0] {
	case 11:
		if len(body) != 1 {
			break
		}
		keys, err := a.List()
		if err != nil || len(keys) > 1024 {
			break
		}
		response = append([]byte{12}, ssh.Marshal(struct{ Count uint32 }{uint32(len(keys))})...)
		for _, key := range keys {
			response = append(response, ssh.Marshal(struct {
				Blob    []byte
				Comment string
			}{key.Blob, key.Comment})...)
		}
	case 13:
		var request struct {
			Key, Data []byte
			Flags     uint32
		}
		if ssh.Unmarshal(body[1:], &request) != nil {
			break
		}
		key, err := ssh.ParsePublicKey(request.Key)
		if err != nil {
			break
		}
		signature, err := a.SignWithFlags(key, request.Data, agent.SignatureFlags(request.Flags))
		if err == nil {
			response = append([]byte{14}, ssh.Marshal(struct{ Signature []byte }{ssh.Marshal(signature)})...)
		}
	}
	return response
}
func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}

// Display information is extracted from the signed payload. Unknown hosts are
// labelled unknown, and unknown-purpose requests are never grouped together.
type signTarget struct {
	Label  string
	Batch  string
	Kind   string
	Detail string
	Host   string
}

func signingTarget(data []byte) signTarget {
	if len(data) > 6 && string(data[:6]) == "SSHSIG" {
		var r struct {
			Namespace, Reserved, HashAlgorithm string
			Hash                               []byte
		}
		if ssh.Unmarshal(data[6:], &r) == nil && len(r.Namespace) > 0 && len(r.Namespace) <= 128 && (r.HashAlgorithm == "sha256" || r.HashAlgorithm == "sha512") {
			namespace := safeLabel(r.Namespace)
			return signTarget{Label: "签名 · " + namespace, Batch: "signature:" + r.Namespace, Kind: "signature", Detail: namespace}
		}
	}
	var r struct {
		Session               []byte
		Message               byte
		User, Service, Method string
		Signed                bool
		Algorithm             string
		Key                   []byte
		Rest                  []byte `ssh:"rest"`
	}
	if ssh.Unmarshal(data, &r) != nil || r.Message != 50 || r.Service != "ssh-connection" || !r.Signed || len(r.User) > 128 {
		return signTarget{Label: "未知签名用途"}
	}
	if r.Method != "publickey" && r.Method != "publickey-hostbound-v00@openssh.com" {
		return signTarget{Label: "未知签名用途"}
	}
	if r.Method == "publickey-hostbound-v00@openssh.com" {
		var h struct{ Key []byte }
		if ssh.Unmarshal(r.Rest, &h) == nil {
			if host, err := ssh.ParsePublicKey(h.Key); err == nil {
				fp := ssh.FingerprintSHA256(host)
				user := safeLabel(r.User)
				return signTarget{Label: user + " · 主机 " + fp, Batch: "connection:" + r.User + ":" + fp, Kind: "login", Detail: user, Host: fp}
			}
		}
	}
	user := safeLabel(r.User)
	return signTarget{Label: user + " · 未知主机", Kind: "login", Detail: user}
}
func safeLabel(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, s)
}
