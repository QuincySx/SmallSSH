//go:build browser_integration

package vncview

import (
	"bytes"
	"context"
	"crypto/des"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/ws"
	"github.com/sagernet/ws/wsutil"
)

// A private headless browser profile exercises the shipped JS, credentials,
// pixels and keyboard path. It never opens a desktop window or a user profile.
func TestNoVNCBrowserAuthenticatesRendersAndHandlesFailure(t *testing.T) {
	if os.Getenv("SMALLSSH_BROWSER_TEST") != "1" {
		t.Skip("opt-in headless browser test")
	}
	browser := os.Getenv("SMALLSSH_BROWSER_PATH")
	if browser == "" {
		browser = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	}
	if _, err := os.Stat(browser); err != nil {
		t.Fatal("headless browser executable required", err)
	}
	for _, scenario := range []struct {
		name, version                    string
		password, correct, direct, saved bool
	}{
		{"password-correct", "003.008", true, true, false, false},
		{"password-incorrect", "003.008", true, false, false, false},
		{"saved-password", "003.008", true, true, true, true},
		{"saved-password-incorrect", "003.008", true, false, true, true},
		{"minimal-direct-no-auth", "003.003", false, true, true, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			var keys atomic.Int32
			events := make(chan rfbKeyEvent, 64)
			serverErrors := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err == nil {
					defer conn.Close()
					conn.SetDeadline(time.Now().Add(25 * time.Second))
					err = syntheticRFB(conn, &keys, rfbFixtureOptions{version: scenario.version, password: scenario.password, events: events})
				}
				serverErrors <- err
			}()
			start := Start
			if scenario.direct {
				start = StartDirect
			}
			s, err := start(ctx, listener.Addr().String(), "合成 VNC 桌面", false)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			var saved atomic.Int32
			if scenario.saved {
				password := "testpass"
				if !scenario.correct {
					password = "badpass"
				}
				s.ConfigureCredentials(Credentials{Password: password, Remember: true}, nil)
			} else if scenario.password {
				s.ConfigureCredentials(Credentials{Remember: true}, func(_ context.Context, value Credentials) error {
					if value.Password != "testpass" || !value.Remember {
						t.Error("saved wrong credentials")
					}
					saved.Add(1)
					return nil
				})
			}
			cdp := startHeadlessBrowser(t, ctx, browser, s.URL())
			if scenario.password && !scenario.saved {
				cdp.wait(t, `document.getElementById('credentials') && !document.getElementById('credentials').hidden && document.getElementById('username-row').hidden`)
				password := "testpass"
				if !scenario.correct {
					password = "badpass"
				}
				encoded, _ := json.Marshal(password)
				cdp.evaluate(t, `document.getElementById('password').value = `+string(encoded)+`; document.getElementById('login').requestSubmit(); true`, nil)
			}
			if !scenario.correct {
				cdp.wait(t, `document.body && document.body.dataset.state === 'failed' && document.getElementById('status').textContent.includes('VNC 登录失败')`)
				if saved.Load() != 0 {
					t.Fatal("failed login saved password")
				}
				select {
				case err := <-serverErrors:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				return
			}
			cdp.wait(t, `document.body && document.body.dataset.state === 'connected' && (() => { const c = document.querySelector('#screen canvas'); if (!c || c.width !== 32) return false; const p = c.getContext('2d').getImageData(0,0,1,1).data; return p[0] === 32 && p[1] === 88 && p[2] === 204; })()`)
			cdp.wait(t, `document.getElementById('credentials').hidden && document.getElementById('password').value === ''`)
			if scenario.password && !scenario.saved {
				cdp.wait(t, `document.getElementById('status').textContent.includes('密码已保存')`)
				if saved.Load() != 1 {
					t.Fatal("successful login did not save exactly once")
				}
			}
			if scenario.direct {
				cdp.wait(t, `document.getElementById('status').textContent.includes('直连 VNC')`)
			}
			cdp.evaluate(t, `document.getElementById('keyboard-toggle').click(); document.getElementById('keys').click(); true`, nil)
			expectRFBKeys(t, ctx, events, []rfbKeyEvent{{0xffe3, true}, {0xffe9, true}, {0xffff, true}, {0xffff, false}, {0xffe9, false}, {0xffe3, false}})
			cdp.evaluate(t, `document.querySelector('[data-key="Escape"]').click(); document.querySelector('[data-key="Tab"]').click(); document.getElementById('mod-super').click(); document.getElementById('mod-super').click(); true`, nil)
			expectRFBKeys(t, ctx, events, []rfbKeyEvent{{0xff1b, true}, {0xff1b, false}, {0xff09, true}, {0xff09, false}, {0xffeb, true}, {0xffeb, false}})
			cdp.evaluate(t, `document.getElementById('extra-keys').open=true; document.getElementById('mod-ctrl').click(); document.getElementById('mod-shift').click(); document.querySelector('[data-key="KeyP"]').click(); document.getElementById('mod-shift').click(); document.getElementById('mod-ctrl').click(); true`, nil)
			expectRFBKeys(t, ctx, events, []rfbKeyEvent{{0xffe3, true}, {0xffe1, true}, {'P', true}, {'P', false}, {0xffe1, false}, {0xffe3, false}})
			cdp.evaluate(t, `document.getElementById('mod-alt').click(); document.querySelector('[data-key="Tab"]').click(); document.getElementById('mod-alt').click(); true`, nil)
			expectRFBKeys(t, ctx, events, []rfbKeyEvent{{0xffe9, true}, {0xff09, true}, {0xff09, false}, {0xffe9, false}})
			cdp.evaluate(t, `document.querySelector('[data-key="F12"]').click(); true`, nil)
			expectRFBKeys(t, ctx, events, []rfbKeyEvent{{0xffc9, true}, {0xffc9, false}})
			cdp.command(t, "Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "a", "code": "KeyA", "text": "a", "windowsVirtualKeyCode": 65}, nil)
			cdp.command(t, "Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": "a", "code": "KeyA", "windowsVirtualKeyCode": 65}, nil)
			expectRFBKeys(t, ctx, events, []rfbKeyEvent{{'a', true}, {'a', false}})
			// A virtual modifier also applies to real canvas typing, and must be
			// released before read-only suppresses outgoing key events.
			cdp.evaluate(t, `document.getElementById('mod-ctrl').click(); true`, nil)
			cdp.command(t, "Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "a", "code": "KeyA", "text": "a", "windowsVirtualKeyCode": 65}, nil)
			cdp.command(t, "Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": "a", "code": "KeyA", "windowsVirtualKeyCode": 65}, nil)
			cdp.evaluate(t, `const v = document.getElementById('view-only'); v.checked = true; v.dispatchEvent(new Event('change')); true`, nil)
			expectRFBKeys(t, ctx, events, []rfbKeyEvent{{0xffe3, true}, {'a', true}, {'a', false}, {0xffe3, false}})
			cdp.wait(t, `document.getElementById('keys').disabled && document.getElementById('keyboard-toggle').disabled && document.getElementById('keyboard-panel').hidden`)
			before := keys.Load()
			cdp.evaluate(t, `document.getElementById('keys').click(); document.querySelector('[data-key="KeyP"]').click(); new Promise(resolve=>setTimeout(()=>resolve(true),100))`, nil)
			if keys.Load() != before {
				t.Fatal("read-only sent remote keys")
			}
			cdp.evaluate(t, `document.getElementById('view-only').checked=false; document.getElementById('view-only').dispatchEvent(new Event('change')); document.getElementById('keyboard-toggle').click(); true`, nil)
			cdp.evaluate(t, `document.getElementById('extra-keys').open=false; true`, nil)
			if scenario.name == "minimal-direct-no-auth" {
				assertVNCToolbarLayout(t, cdp)
				// Losing the application releases virtual modifiers even when
				// the popover was closed to continue typing on the desktop.
				cdp.evaluate(t, `document.getElementById('mod-alt').click(); window.dispatchEvent(new Event('blur')); true`, nil)
				expectRFBKeys(t, ctx, events, []rfbKeyEvent{{0xffe9, true}, {0xffe9, false}})
				cdp.wait(t, `document.getElementById('mod-alt').getAttribute('aria-pressed')==='false'`)
			}
			for _, dark := range []bool{false, true} {
				value := "light"
				if dark {
					value = "dark"
				}
				cdp.command(t, "Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": value}}}, nil)
				var capture struct{ Data string }
				cdp.command(t, "Page.captureScreenshot", map[string]any{"format": "png"}, &capture)
				data, err := base64.StdEncoding.DecodeString(capture.Data)
				if err != nil {
					t.Fatal(err)
				}
				directory := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
				if directory == "" {
					directory, _ = filepath.Abs("../../build/snapshots/novnc")
				}
				base, _ := filepath.Abs("../../build")
				relative, err := filepath.Rel(base, directory)
				if err != nil || !filepath.IsAbs(directory) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
					t.Fatal("browser snapshots must remain inside build")
				}
				path := filepath.Join(directory, "viewer-"+scenario.name+"-"+value+".png")
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				t.Log(path)
			}
			s.Close()
			cdp.wait(t, `document.body.dataset.state === 'failed' || document.body.dataset.state === 'disconnected'`)
		})
	}
}

func assertVNCToolbarLayout(t *testing.T, cdp *browserCDP) {
	t.Helper()
	cdp.wait(t, `(() => {
        const screen=document.getElementById('screen').getBoundingClientRect(), rail=document.getElementById('toolbar').getBoundingClientRect();
        return !document.querySelector('header') && screen.y===4 && screen.height===innerHeight-8 && rail.width===40 && rail.right===innerWidth-4 && document.getElementById('toolbar-primary').children.length===8 && document.getElementById('more-toggle').hidden && document.getElementById('overflow-actions').children.length===0;
    })()`)
	cdp.evaluate(t, `window.originalVNCCanvas=document.querySelector('#screen canvas'); true`, nil)
	cdp.command(t, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 320, "height": 180, "deviceScaleFactor": 1, "mobile": false}, nil)
	cdp.wait(t, `!document.getElementById('more-toggle').hidden && document.getElementById('overflow-actions').children.length>0 && document.documentElement.scrollWidth<=innerWidth && document.documentElement.scrollHeight<=innerHeight && document.getElementById('screen').getBoundingClientRect().height===innerHeight-8`)
	cdp.evaluate(t, `document.getElementById('more-toggle').click(); true`, nil)
	cdp.wait(t, `!document.getElementById('more-panel').hidden && document.getElementById('keyboard-panel').hidden && (()=>{const r=document.getElementById('more-panel').getBoundingClientRect();return r.left>=0&&r.top>=0&&r.bottom<=innerHeight&&r.right<=innerWidth-40;})()`)
	cdp.evaluate(t, `document.getElementById('scale').click(); true`, nil)
	cdp.wait(t, `!document.getElementById('scale').checked && document.querySelector('#screen canvas')===window.originalVNCCanvas`)
	directory := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if directory == "" {
		directory, _ = filepath.Abs("../../build/snapshots/novnc")
	}
	base, _ := filepath.Abs("../../build")
	relative, err := filepath.Rel(base, directory)
	if err != nil || !filepath.IsAbs(directory) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatal("toolbar snapshots must remain inside build")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, style := range []string{"light", "dark"} {
		cdp.command(t, "Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": style}}}, nil)
		var capture struct{ Data string }
		cdp.command(t, "Page.captureScreenshot", map[string]any{"format": "png"}, &capture)
		data, err := base64.StdEncoding.DecodeString(capture.Data)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "toolbar-compact-"+style+".png"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Growing while the overflow menu is open removes both the menu and its
	// trigger, and returns the same controls and saved state to the rail.
	cdp.command(t, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1100, "height": 760, "deviceScaleFactor": 1, "mobile": false}, nil)
	cdp.wait(t, `document.getElementById('more-toggle').hidden && document.getElementById('more-panel').hidden && document.getElementById('toolbar-primary').children.length===8 && !document.getElementById('scale').checked && document.querySelector('#screen canvas')===window.originalVNCCanvas`)
	cdp.command(t, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 320, "height": 180, "deviceScaleFactor": 1, "mobile": false}, nil)
	cdp.wait(t, `!document.getElementById('more-toggle').hidden`)
	cdp.evaluate(t, `document.getElementById('more-toggle').click(); true`, nil)
	cdp.evaluate(t, `document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true})); true`, nil)
	cdp.wait(t, `document.getElementById('more-panel').hidden && document.activeElement.id==='more-toggle'`)
	cdp.command(t, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 320, "height": 100, "deviceScaleFactor": 1, "mobile": false}, nil)
	cdp.wait(t, `document.getElementById('toolbar-primary').children.length===0 && document.getElementById('overflow-actions').children.length===8`)
	cdp.evaluate(t, `document.getElementById('more-toggle').click(); document.getElementById('keyboard-toggle').click(); true`, nil)
	cdp.wait(t, `!document.getElementById('keyboard-panel').hidden && document.getElementById('more-panel').hidden`)
	cdp.evaluate(t, `document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true})); true`, nil)
	cdp.wait(t, `document.getElementById('keyboard-panel').hidden && document.activeElement.id==='more-toggle'`)
	cdp.command(t, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1100, "height": 760, "deviceScaleFactor": 1, "mobile": false}, nil)
	cdp.wait(t, `document.getElementById('more-toggle').hidden && document.getElementById('toolbar-primary').children.length===8 && document.getElementById('overflow-actions').children.length===0 && document.querySelector('#screen canvas')===window.originalVNCCanvas && !document.getElementById('scale').checked`)
	cdp.evaluate(t, `document.getElementById('scale').click(); document.getElementById('keyboard-toggle').click(); true`, nil)
	cdp.wait(t, `!document.getElementById('keyboard-panel').hidden && document.getElementById('more-panel').hidden`)
}

type rfbKeyEvent struct {
	sym  uint32
	down bool
}

type rfbFixtureOptions struct {
	version   string
	password  bool
	events    chan rfbKeyEvent
	clipboard *clipboardFixture
}

func expectRFBKeys(t *testing.T, ctx context.Context, events <-chan rfbKeyEvent, expected []rfbKeyEvent) {
	t.Helper()
	for _, want := range expected {
		select {
		case got := <-events:
			if got != want {
				t.Fatalf("remote key sequence: got %#v, want %#v", got, want)
			}
		case <-ctx.Done():
			t.Fatal("remote key did not arrive", want, ctx.Err())
		}
	}
}

func syntheticRFB(conn net.Conn, keys *atomic.Int32, options ...rfbFixtureOptions) error {
	option := rfbFixtureOptions{version: "003.008", password: true}
	if len(options) != 0 {
		option = options[0]
	}
	read := func(n int) ([]byte, error) {
		data := make([]byte, n)
		_, err := io.ReadFull(conn, data)
		return data, err
	}
	if _, err := io.WriteString(conn, "RFB "+option.version+"\n"); err != nil {
		return err
	}
	if _, err := read(12); err != nil {
		return err
	}
	security := byte(1)
	if option.password {
		security = 2
	}
	if option.version == "003.003" {
		if err := binary.Write(conn, binary.BigEndian, uint32(security)); err != nil {
			return err
		}
	} else {
		conn.Write([]byte{1, security})
		if choice, err := read(1); err != nil || choice[0] != security {
			return fmt.Errorf("unexpected security selection: %v %v", choice, err)
		}
	}
	if option.password {
		challenge := []byte("0123456789abcdef")
		conn.Write(challenge)
		response, err := read(16)
		if err != nil {
			return err
		}
		key := []byte("testpass")
		for i := range key {
			key[i] = bits.Reverse8(key[i])
		}
		cipher, _ := des.NewCipher(key)
		expected := make([]byte, 16)
		cipher.Encrypt(expected[:8], challenge[:8])
		cipher.Encrypt(expected[8:], challenge[8:])
		if !bytes.Equal(response, expected) {
			reason := "fixture authentication failed"
			_ = binary.Write(conn, binary.BigEndian, uint32(1))
			_ = binary.Write(conn, binary.BigEndian, uint32(len(reason)))
			_, err := io.WriteString(conn, reason)
			return err
		}
	}
	if option.password || option.version != "003.003" {
		conn.Write([]byte{0, 0, 0, 0})
	}
	if _, err := read(1); err != nil {
		return err
	}
	format := []byte{32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0}
	init := []byte{0, 32, 0, 24}
	init = append(init, format...)
	init = append(init, 0, 0, 0, 7)
	init = append(init, []byte("Fixture")...)
	conn.Write(init)
	if option.clipboard != nil {
		if err := option.clipboard.start(conn); err != nil {
			return err
		}
	}
	for {
		kind, err := read(1)
		if err != nil {
			return err
		}
		switch kind[0] {
		case 0:
			data, err := read(19)
			if err != nil {
				return err
			}
			format = data[3:]
		case 2:
			header, err := read(3)
			if err != nil {
				return err
			}
			if _, err := read(int(binary.BigEndian.Uint16(header[1:])) * 4); err != nil {
				return err
			}
		case 3:
			if _, err := read(9); err != nil {
				return err
			}
			frame := []byte{0, 0, 0, 1, 0, 0, 0, 0, 0, 32, 0, 24, 0, 0, 0, 0}
			pixel := uint32(32)<<format[10] | uint32(88)<<format[11] | uint32(204)<<format[12]
			var encoded [4]byte
			if format[2] == 0 {
				binary.LittleEndian.PutUint32(encoded[:], pixel)
			} else {
				binary.BigEndian.PutUint32(encoded[:], pixel)
			}
			for range 32 * 24 {
				frame = append(frame, encoded[:int(format[0])/8]...)
			}
			if _, err := conn.Write(frame); err != nil {
				return err
			}
		case 4:
			data, err := read(7)
			if err != nil {
				return err
			}
			keys.Add(1)
			if option.events != nil {
				select {
				case option.events <- rfbKeyEvent{binary.BigEndian.Uint32(data[3:]), data[0] != 0}:
				default:
					return errors.New("fixture key capture overflow")
				}
			}
		case 5:
			if _, err := read(5); err != nil {
				return err
			}
		case 6:
			header, err := read(7)
			if err != nil {
				return err
			}
			length := int32(binary.BigEndian.Uint32(header[3:]))
			size := int64(length)
			if size < 0 {
				size = -size
			}
			if size > 1<<20 {
				return errors.New("fixture clipboard is too large")
			}
			data, err := read(int(size))
			if err != nil {
				return err
			}
			if option.clipboard != nil {
				if err := option.clipboard.receive(conn, length < 0, data); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unexpected RFB message %d", kind[0])
		}
	}
}

type browserCDP struct {
	conn net.Conn
	rw   io.ReadWriter
	id   int
}

func startHeadlessBrowser(t *testing.T, ctx context.Context, browser, page string) *browserCDP {
	t.Helper()
	base, _ := filepath.Abs("../../build/test-fixtures")
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	profile, err := os.MkdirTemp(base, "novnc-browser-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(profile) })
	cmd := exec.CommandContext(ctx, browser, "--headless=new", "--disable-gpu", "--disable-background-networking", "--disable-component-update", "--disable-sync", "--no-first-run", "--no-default-browser-check", "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0", "--user-data-dir="+profile, "--window-size=1100,760", page)
	var output bytes.Buffer
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	var port string
	for port == "" {
		data, _ := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
		if len(data) > 0 {
			port = strings.Split(string(data), "\n")[0]
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("headless browser failed to start", ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	response, err := http.Get("http://127.0.0.1:" + port + "/json/list")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var targets []struct {
		Type, URL, WebSocketDebuggerURL string
	}
	if err := json.NewDecoder(response.Body).Decode(&targets); err != nil {
		t.Fatal(err)
	}
	var endpoint string
	for _, target := range targets {
		if target.Type == "page" {
			endpoint = target.WebSocketDebuggerURL
			break
		}
	}
	if endpoint == "" {
		t.Fatal("headless browser has no page target")
	}
	conn, buffered, _, err := ws.DefaultDialer.Dial(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(25 * time.Second))
	var rw io.ReadWriter = conn
	if buffered != nil {
		rw = struct {
			io.Reader
			io.Writer
		}{buffered, conn}
	}
	return &browserCDP{conn: conn, rw: rw}
}

func (c *browserCDP) command(t *testing.T, method string, params, result any) {
	t.Helper()
	c.id++
	data, _ := json.Marshal(map[string]any{"id": c.id, "method": method, "params": params})
	if err := wsutil.WriteClientText(c.conn, data); err != nil {
		t.Fatal(err)
	}
	for {
		data, _, err := wsutil.ReadServerData(c.rw)
		if err != nil {
			t.Fatal("browser command failed", method, err)
		}
		var message struct {
			ID     int
			Result json.RawMessage
			Error  json.RawMessage
		}
		if err := json.Unmarshal(data, &message); err != nil {
			t.Fatal(err)
		}
		if message.ID != c.id {
			continue
		}
		if len(message.Error) > 0 {
			t.Fatal(method, string(message.Error))
		}
		if result != nil {
			if err := json.Unmarshal(message.Result, result); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
}

func (c *browserCDP) evaluate(t *testing.T, expression string, result any) {
	t.Helper()
	var response struct {
		Result           struct{ Value json.RawMessage }
		ExceptionDetails json.RawMessage
	}
	c.command(t, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": true}, &response)
	if len(response.ExceptionDetails) > 0 {
		t.Fatal("viewer script exception", string(response.ExceptionDetails))
	}
	if result != nil {
		if err := json.Unmarshal(response.Result.Value, result); err != nil {
			t.Fatal(err)
		}
	}
}

func (c *browserCDP) wait(t *testing.T, expression string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var ready bool
		c.evaluate(t, "Boolean("+expression+")", &ready)
		if ready {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	var state string
	c.evaluate(t, `document.body?.dataset.state + ': ' + document.getElementById('status')?.textContent`, &state)
	t.Fatal(errors.New("viewer state timed out"), state, expression)
}
