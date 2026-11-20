//go:build browser_integration

package vncview

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const clipText uint32 = 1
const clipCaps uint32 = 1 << 24
const clipRequest uint32 = 1 << 25
const clipNotify uint32 = 1 << 27
const clipProvide uint32 = 1 << 28

type clipboardFixture struct {
	unicode  bool
	text     string
	received chan string
}

func serverClipboard(w io.Writer, extended bool, data []byte) error {
	header := []byte{3, 0, 0, 0, 0, 0, 0, 0}
	length := int32(len(data))
	if extended {
		length = -length
	}
	binary.BigEndian.PutUint32(header[4:], uint32(length))
	if _, err := w.Write(append(header, data...)); err != nil {
		return err
	}
	return nil
}

func clipboardFlags(flags uint32) []byte {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, flags)
	return data
}

func (f *clipboardFixture) start(w io.Writer) error {
	if f.unicode {
		data := append(clipboardFlags(clipCaps|clipRequest|clipNotify|clipProvide|clipText), clipboardFlags(64<<10)...)
		if err := serverClipboard(w, true, data); err != nil {
			return err
		}
		return serverClipboard(w, true, clipboardFlags(clipNotify|clipText))
	}
	data := []byte{}
	for _, r := range f.text {
		if r > 255 {
			return errors.New("legacy fixture needs Latin-1")
		}
		data = append(data, byte(r))
	}
	return serverClipboard(w, false, data)
}

func (f *clipboardFixture) receive(w io.Writer, extended bool, data []byte) error {
	text := ""
	if extended {
		if len(data) < 4 {
			return errors.New("missing extended clipboard flags")
		}
		flags := binary.BigEndian.Uint32(data)
		if flags&clipCaps != 0 {
			return nil
		}
		switch flags & 0xff000000 {
		case clipNotify:
			return serverClipboard(w, true, clipboardFlags(clipRequest|clipText))
		case clipRequest:
			body := []byte(f.text + "\x00")
			var compressed bytes.Buffer
			zw := zlib.NewWriter(&compressed)
			if err := binary.Write(zw, binary.BigEndian, uint32(len(body))); err != nil {
				return err
			}
			if _, err := zw.Write(body); err != nil {
				return err
			}
			if err := zw.Close(); err != nil {
				return err
			}
			return serverClipboard(w, true, append(clipboardFlags(clipProvide|clipText), compressed.Bytes()...))
		case clipProvide:
			zr, err := zlib.NewReader(bytes.NewReader(data[4:]))
			if err != nil {
				return err
			}
			defer zr.Close()
			var length uint32
			if err := binary.Read(zr, binary.BigEndian, &length); err != nil {
				return err
			}
			if length > 1<<20 {
				return errors.New("clipboard decompression exceeds fixture limit")
			}
			body := make([]byte, length)
			if _, err := io.ReadFull(zr, body); err != nil {
				return err
			}
			text = strings.TrimSuffix(string(body), "\x00")
			text = strings.ReplaceAll(text, "\r\n", "\n")
		default:
			return errors.New("unexpected clipboard action")
		}
	} else {
		for _, b := range data {
			text += string(rune(b))
		}
	}
	select {
	case f.received <- text:
		return nil
	default:
		return errors.New("clipboard fixture queue overflow")
	}
}

// Native clipboard access uses synthetic callbacks, and browser APIs are
// poisoned to catch WebView paste prompts. RFB traffic is real; no OS data is used.
func TestNoVNCBrowserClipboardTextBothWaysAndLifecycle(t *testing.T) {
	if os.Getenv("SMALLSSH_BROWSER_TEST") != "1" {
		t.Skip("opt-in headless browser test")
	}
	browser := os.Getenv("SMALLSSH_BROWSER_PATH")
	if browser == "" {
		browser = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	}
	if _, err := os.Stat(browser); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name               string
		unicode, oversized bool
	}{
		{"legacy", false, false}, {"unicode", true, false}, {"oversized-remote", false, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			remote := "remote café\n<plain text>"
			if scenario.unicode {
				remote = "远端中文 😊\n<plain text>"
			}
			if scenario.oversized {
				remote = strings.Repeat("x", 65537)
			}
			fixture := &clipboardFixture{scenario.unicode, remote, make(chan string, 8)}
			var connections atomic.Int32
			go func() {
				for {
					conn, err := listener.Accept()
					if err != nil {
						return
					}
					first := connections.Add(1) == 1
					go func() {
						defer conn.Close()
						stop := context.AfterFunc(ctx, func() { conn.Close() })
						defer stop()
						var keys atomic.Int32
						option := rfbFixtureOptions{version: "003.008"}
						if first {
							option.clipboard = fixture
						}
						_ = syntheticRFB(conn, &keys, option)
					}()
				}
			}()
			session, err := StartDirect(ctx, listener.Addr().String(), "剪贴板测试", false)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			var nativeText, copiedText atomic.Value
			nativeText.Store("generated local text")
			copiedText.Store("")
			var reads, writes atomic.Int32
			var delayed atomic.Bool
			readStarted := make(chan chan string, 1)
			access := func(ctx context.Context, write bool, text string) (string, error) {
				if write {
					writes.Add(1)
					copiedText.Store(text)
					return "", nil
				}
				reads.Add(1)
				if delayed.Load() {
					reply := make(chan string, 1)
					select {
					case readStarted <- reply:
					case <-ctx.Done():
						return "", ctx.Err()
					}
					select {
					case text := <-reply:
						return text, nil
					case <-ctx.Done():
						return "", ctx.Err()
					}
				}
				return nativeText.Load().(string), nil
			}
			session.ConfigureClipboard(access)
			var fullscreen atomic.Int32
			session.ConfigureFullscreen(func(context.Context) error { fullscreen.Add(1); return nil })
			cdp := startHeadlessBrowser(t, ctx, browser, "about:blank")
			cdp.command(t, "Page.enable", map[string]any{}, nil)
			cdp.command(t, "Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": `window.browserClipboardCalls=0; window.nativeClipboardResponses=0; const originalFetch=window.fetch.bind(window); window.fetch=async(...args)=>{const response=await originalFetch(...args);if(new URL(args[0],location.href).pathname.endsWith('/clipboard'))window.nativeClipboardResponses++;return response}; Object.defineProperty(navigator,'clipboard',{configurable:true,get(){window.browserClipboardCalls++;throw new Error('Browser clipboard would show a paste confirmation')}}); window.clipboardBoundaryInstalled=true;`}, nil)
			cdp.command(t, "Page.navigate", map[string]any{"url": session.URL()}, nil)
			cdp.wait(t, `document.body && document.body.dataset.state==='connected'`)
			var boundary bool
			cdp.evaluate(t, `window.clipboardBoundaryInstalled===true`, &boundary)
			if !boundary {
				t.Fatal("private browser clipboard boundary was not installed")
			}
			if scenario.oversized {
				cdp.wait(t, `document.getElementById('notice').textContent.includes('64 KiB') && document.getElementById('copy-clipboard').disabled`)
				return
			}
			cdp.wait(t, `!document.getElementById('copy-clipboard').disabled`)
			if reads.Load() != 0 || writes.Load() != 0 {
				t.Fatal("clipboard accessed without click")
			}
			cdp.evaluate(t, `document.getElementById('copy-clipboard').click(); true`, nil)
			cdp.wait(t, `document.getElementById('notice').textContent==='复制成功'`)
			if writes.Load() != 1 || copiedText.Load().(string) != remote {
				t.Fatal("native clipboard copy failed")
			}
			local := "local café\nsecond line"
			if scenario.unicode {
				local = "本机中文 😊\nsecond line"
			}
			nativeText.Store(local)
			cdp.evaluate(t, `document.getElementById('paste-clipboard').click(); true`, nil)
			cdp.wait(t, `document.getElementById('notice').textContent==='已粘贴到远端剪贴板'`)
			select {
			case got := <-fixture.received:
				if got != local {
					t.Fatalf("clipboard changed: got %q, want %q", got, local)
				}
			case <-ctx.Done():
				t.Fatal("clipboard did not reach RFB", ctx.Err())
			}
			cdp.evaluate(t, `document.getElementById('fullscreen').click(); true`, nil)
			cdp.wait(t, `!document.getElementById('fullscreen').disabled`)
			if fullscreen.Load() != 1 || connections.Load() != 1 {
				t.Fatal("fullscreen reconnected or missed native control")
			}
			captureClipboardPanel(t, cdp, scenario.name)
			// Only two icon actions; no clipboard panel, text field or duplicate name.
			cdp.wait(t, `!document.getElementById('clipboard-panel') && !document.querySelector('textarea') && !document.getElementById('name') && document.getElementById('paste-clipboard').querySelector('svg') && document.getElementById('copy-clipboard').querySelector('svg')`)
			cdp.evaluate(t, `window.screenHeight=document.getElementById('screen').getBoundingClientRect().height; document.getElementById('keyboard-toggle').click(); true`, nil)
			cdp.wait(t, `!document.getElementById('keyboard-panel').hidden && document.getElementById('screen').getBoundingClientRect().height===window.screenHeight`)
			cdp.evaluate(t, `document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true})); true`, nil)
			cdp.wait(t, `document.getElementById('keyboard-panel').hidden`)
			if !scenario.unicode {
				nativeText.Store("中文")
				cdp.evaluate(t, `document.getElementById('paste-clipboard').click(); true`, nil)
				cdp.wait(t, `document.getElementById('notice').textContent.includes('不支持 Unicode')`)
			}
			nativeText.Store(strings.Repeat("x", 65537))
			cdp.evaluate(t, `document.getElementById('paste-clipboard').click(); true`, nil)
			cdp.wait(t, `document.getElementById('notice').textContent.includes('64 KiB')`)
			session.ConfigureClipboard(nil)
			cdp.evaluate(t, `document.getElementById('paste-clipboard').click(); true`, nil)
			cdp.wait(t, `document.getElementById('notice').textContent.includes('无法读取本机剪贴板')`)
			session.ConfigureClipboard(access)
			delayed.Store(true)
			cdp.evaluate(t, `document.getElementById('paste-clipboard').click(); true`, nil)
			var pending chan string
			select {
			case pending = <-readStarted:
			case <-ctx.Done():
				t.Fatal("native read did not start")
			}
			cdp.evaluate(t, `window.nativeResponseBefore=window.nativeClipboardResponses; document.getElementById('view-only').checked=true; document.getElementById('view-only').dispatchEvent(new Event('change')); true`, nil)
			pending <- "late read"
			cdp.wait(t, `window.nativeClipboardResponses>window.nativeResponseBefore`)
			cdp.wait(t, `document.getElementById('paste-clipboard').disabled && document.getElementById('copy-clipboard').disabled`)
			cdp.evaluate(t, `document.getElementById('paste-clipboard').click(); new Promise(resolve=>setTimeout(()=>resolve(true),50))`, nil)
			if len(fixture.received) != 0 {
				t.Fatal("invalid or read-only clipboard was sent")
			}
			cdp.evaluate(t, `document.getElementById('view-only').checked=false; document.getElementById('view-only').dispatchEvent(new Event('change')); document.getElementById('paste-clipboard').click(); true`, nil)
			select {
			case pending = <-readStarted:
			case <-ctx.Done():
				t.Fatal("second native read did not start")
			}
			cdp.evaluate(t, `window.nativeResponseBefore=window.nativeClipboardResponses; document.getElementById('disconnect').click(); true`, nil)
			cdp.wait(t, `document.body.dataset.state==='disconnected' || document.body.dataset.state==='failed'`)
			// The browser's disconnect event can precede the proxy handler's
			// final socket cleanup. Start the next fixture only after release.
			for len(session.viewer) != 0 {
				select {
				case <-ctx.Done():
					t.Fatal("previous viewer did not release", ctx.Err())
				case <-time.After(time.Millisecond):
				}
			}
			cdp.evaluate(t, `document.getElementById('connect').click(); true`, nil)
			cdp.wait(t, `document.body.dataset.state==='connected'`)
			pending <- "old session text"
			cdp.wait(t, `window.nativeClipboardResponses>window.nativeResponseBefore`)
			cdp.evaluate(t, `new Promise(resolve=>setTimeout(()=>resolve(true),50))`, nil)
			cdp.wait(t, `document.getElementById('copy-clipboard').disabled && window.browserClipboardCalls===0`)
		})
	}
}

func captureClipboardPanel(t *testing.T, cdp *browserCDP, name string) {
	t.Helper()
	directory := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if directory == "" {
		directory, _ = filepath.Abs("../../build/snapshots/novnc")
	}
	base, _ := filepath.Abs("../../build")
	relative, err := filepath.Rel(base, directory)
	if err != nil || !filepath.IsAbs(directory) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatal("snapshots must remain in build")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, style := range []string{"light", "dark"} {
		width := 1100
		if style == "dark" {
			width = 640
		}
		cdp.command(t, "Emulation.setDeviceMetricsOverride", map[string]any{"width": width, "height": 760, "deviceScaleFactor": 1, "mobile": false}, nil)
		cdp.command(t, "Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": style}}}, nil)
		var capture struct{ Data string }
		cdp.command(t, "Page.captureScreenshot", map[string]any{"format": "png"}, &capture)
		data, err := base64.StdEncoding.DecodeString(capture.Data)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "clipboard-"+name+"-"+style+".png"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
