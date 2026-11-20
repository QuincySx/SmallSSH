package vncview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/sagernet/ws"
	"github.com/sagernet/ws/wsutil"
)

func TestViewerAcceptsOnlyPrivateLoopbackTunnelAndLocalOrigin(t *testing.T) {
	for _, target := range []string{"example.com:5900", "192.0.2.1:5900", "0.0.0.0:5900", "127.0.0.1:0", "127.0.0.1", "localhost:5900"} {
		if session, err := Start(context.Background(), target, "fixture", false); err == nil {
			session.Close()
			t.Errorf("unsafe or unallocated destination accepted: %s", target)
		}
	}
	s, err := Start(context.Background(), "127.0.0.1:5900", "<fixture>", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	for _, test := range []struct {
		path, host, origin string
		status             int
	}{{s.URL(), "", "", 200}, {s.URL() + "novnc/core/rfb.js", "", "", 200}, {s.origin + "/", "", "", 404}, {s.URL(), "attacker.example", "", 404}, {s.URL(), "", "https://attacker.example", 404}, {s.URL() + "socket", "", "", 403}} {
		req, _ := http.NewRequest(http.MethodGet, test.path, nil)
		if test.host != "" {
			req.Host = test.host
		}
		req.Header.Set("Origin", test.origin)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != test.status {
			t.Errorf("%s host=%s origin=%s: %d", test.path, test.host, test.origin, response.StatusCode)
		}
		if response.StatusCode == 200 && (response.Header.Get("Content-Security-Policy") == "" || response.Header.Get("Cache-Control") != "no-store") {
			t.Fatal("viewer lost its private content policy")
		}
	}
	response, err := http.Get(s.URL() + "config.json")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var config struct {
		Name     string `json:"name"`
		ViewOnly bool   `json:"viewOnly"`
	}
	if err := json.NewDecoder(response.Body).Decode(&config); err != nil || config.Name != "<fixture>" || !config.ViewOnly {
		t.Fatal("viewer configuration changed", config, err)
	}
}

func TestViewerBridgesBinaryPingAndFragmentedFramesAndClosesWithOwner(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	targetClosed := make(chan struct{})
	go func() {
		defer close(targetClosed)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s, err := Start(ctx, listener.Addr().String(), "fixture", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	endpoint, _ := url.Parse(s.URL() + "socket")
	endpoint.Scheme = "ws"
	dialer := ws.Dialer{Header: ws.HandshakeHeaderHTTP(http.Header{"Origin": []string{s.origin}})}
	conn, buffered, _, err := dialer.Dial(ctx, endpoint.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if buffered != nil && buffered.Buffered() != 0 {
		t.Fatal("unexpected pre-handshake data")
	}
	payload := []byte{0, 255, 32, 0, 8}
	frame := ws.NewBinaryFrame(bytes.Clone(payload))
	frame.Header.Fin = false
	if err := ws.WriteFrame(conn, ws.MaskFrameInPlace(frame)); err != nil {
		t.Fatal(err)
	}
	if err := wsutil.WriteClientMessage(conn, ws.OpPing, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	last := ws.NewFrame(ws.OpContinuation, true, []byte("tail"))
	if err := ws.WriteFrame(conn, ws.MaskFrameInPlace(last)); err != nil {
		t.Fatal(err)
	}
	var received []byte
	gotPong := false
	for len(received) < len(payload)+4 || !gotPong {
		messages, err := wsutil.ReadServerMessage(conn, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, message := range messages {
			if message.OpCode == ws.OpPong {
				gotPong = bytes.Equal(message.Payload, []byte("ping"))
			} else if message.OpCode == ws.OpBinary {
				received = append(received, message.Payload...)
			}
		}
	}
	if !bytes.Equal(received, append(payload, []byte("tail")...)) {
		t.Fatal("binary RFB bytes changed", received)
	}
	cancel()
	select {
	case <-targetClosed:
	case <-time.After(3 * time.Second):
		t.Fatal("owner cancellation leaked the target connection")
	}
	if _, err := wsutil.ReadServerMessage(conn, nil); err == nil {
		t.Fatal("owner cancellation left the WebSocket alive")
	}
}

func TestBundledNoVNCFilesMatchPinnedOfficialSource(t *testing.T) {
	manifest, err := assets.ReadFile("web/novnc/ORIGIN.json")
	if err != nil {
		t.Fatal(err)
	}
	var origin struct {
		Commit string `json:"commit"`
		Files  []struct {
			Path, SHA256 string
		} `json:"files"`
	}
	if err := json.Unmarshal(manifest, &origin); err != nil || len(origin.Files) < 40 || origin.Commit != "63107bd06d9e1f6136ff21aeda8cd62cbf0d433e" {
		t.Fatal("missing pinned source manifest", err)
	}
	for _, file := range origin.Files {
		data, err := assets.ReadFile("web/novnc/" + file.Path)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != file.SHA256 {
			t.Errorf("upstream file changed without updating provenance: %s", file.Path)
		}
	}
}
