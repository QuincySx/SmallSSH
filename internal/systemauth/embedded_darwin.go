//go:build darwin

package systemauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/ebitengine/purego"
)

type nativeEmbeddedBridge struct {
	start   func(uintptr, []byte, int32, float64, float64, float64, float64) uint64
	move    func(uint64, float64, float64, float64, float64)
	cancel  func(uint64)
	poll    func(uint64, []byte, int32) int32
	release func(uint64)
}

func (b *nativeEmbeddedBridge) Start(window uintptr, input []byte, rect EmbeddedRect) uint64 {
	token := b.start(window, input, int32(len(input)), rect.X, rect.Y, rect.Width, rect.Height)
	runtime.KeepAlive(input)
	return token
}
func (b *nativeEmbeddedBridge) Move(token uint64, rect EmbeddedRect) {
	b.move(token, rect.X, rect.Y, rect.Width, rect.Height)
}
func (b *nativeEmbeddedBridge) Cancel(token uint64) { b.cancel(token) }
func (b *nativeEmbeddedBridge) Poll(token uint64, output []byte) int32 {
	n := b.poll(token, output, int32(len(output)))
	runtime.KeepAlive(output)
	return n
}
func (b *nativeEmbeddedBridge) Release(token uint64) { b.release(token) }

var embeddedLibrary struct {
	sync.Once
	bridge embeddedBridge
	err    error
}

func loadEmbeddedBridge() (embeddedBridge, error) {
	embeddedLibrary.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			embeddedLibrary.err = err
			return
		}
		library, err := verifiedEmbeddedLibrary(exe, hashes)
		if err != nil {
			embeddedLibrary.err = err
			return
		}
		handle, err := purego.Dlopen(library, purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			embeddedLibrary.err = errors.New("窗口内指纹验证组件无法加载")
			return
		}
		bridge := &nativeEmbeddedBridge{}
		for _, binding := range []struct {
			name string
			fn   any
		}{
			{"smallssh_auth_start", &bridge.start},
			{"smallssh_auth_move", &bridge.move},
			{"smallssh_auth_cancel", &bridge.cancel},
			{"smallssh_auth_poll", &bridge.poll},
			{"smallssh_auth_release", &bridge.release},
		} {
			symbol, err := purego.Dlsym(handle, binding.name)
			if err != nil {
				_ = purego.Dlclose(handle)
				embeddedLibrary.err = errors.New("窗口内指纹验证组件接口不完整")
				return
			}
			purego.RegisterFunc(binding.fn, symbol)
		}
		// A successfully loaded library stays resident while any session can use
		// its function pointers; only failed loads are closed.
		embeddedLibrary.bridge = bridge
	})
	return embeddedLibrary.bridge, embeddedLibrary.err
}

func verifiedEmbeddedLibrary(executable string, manifest []byte) (string, error) {
	var expected map[string]string
	if err := json.Unmarshal(manifest, &expected); err != nil {
		return "", errors.New("窗口内指纹验证组件校验记录无效")
	}
	want := expected["darwin/"+runtime.GOARCH+"/embedded"]
	decoded, err := hex.DecodeString(want)
	if err != nil || len(decoded) != sha256.Size {
		return "", errors.New("请先构建窗口内指纹验证组件")
	}
	base := filepath.Dir(executable)
	for _, candidate := range []string{
		filepath.Join(base, "libsmallssh-auth.dylib"),
		filepath.Join(base, "..", "Resources", "libsmallssh-auth.dylib"),
		filepath.Join(base, "..", "libsmallssh-auth.dylib"),
	} {
		file, err := os.Open(candidate)
		if err != nil {
			continue
		}
		st, err := file.Stat()
		if err != nil || !st.Mode().IsRegular() || st.Size() > 64<<20 {
			file.Close()
			continue
		}
		hash := sha256.New()
		n, err := io.Copy(hash, io.LimitReader(file, (64<<20)+1))
		file.Close()
		if err == nil && n <= 64<<20 && hex.EncodeToString(hash.Sum(nil)) == want {
			return candidate, nil
		}
	}
	return "", errors.New("窗口内指纹验证组件缺失或校验不匹配")
}

func (b *macBackend) UnwrapEmbedded(ctx context.Context, raw json.RawMessage, reason string, presentation *EmbeddedPresentation) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !presentation.Ready() {
		return nil, &EmbeddedError{Code: "notReady", Message: "窗口内指纹验证区域尚未就绪"}
	}
	input, err := embeddedInput(raw, reason)
	if err != nil {
		return nil, err
	}
	defer wipe(input)
	bridge, err := loadEmbeddedBridge()
	if err != nil {
		return nil, err
	}
	return runEmbedded(ctx, presentation, bridge, input)
}
