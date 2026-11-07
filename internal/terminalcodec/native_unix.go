//go:build darwin || linux

package terminalcodec

import (
	"errors"
	"github.com/ebitengine/purego"
	"runtime"
	"strings"
	"sync"
	"unsafe"
)

var iconvAPI struct {
	sync.Once
	err     error
	open    func(string, string) uintptr
	convert func(uintptr, *unsafe.Pointer, *uintptr, *unsafe.Pointer, *uintptr) uintptr
	close   func(uintptr) int32
}

type native struct {
	handle   uintptr
	closed   bool
	from, to string
}

func newConverter(from, to string) (converter, error) {
	iconvAPI.Do(func() {
		library := "libc.so.6"
		if runtime.GOOS == "darwin" {
			library = "/usr/lib/libiconv.2.dylib"
		}
		h, e := purego.Dlopen(library, purego.RTLD_NOW|purego.RTLD_LOCAL)
		if e != nil {
			iconvAPI.err = errors.New("系统字符转码库不可用")
			return
		}
		for _, entry := range []struct {
			name     string
			function any
		}{{"iconv_open", &iconvAPI.open}, {"iconv", &iconvAPI.convert}, {"iconv_close", &iconvAPI.close}} {
			ptr, e := purego.Dlsym(h, entry.name)
			if e != nil {
				ptr, e = purego.Dlsym(h, "lib"+entry.name)
			}
			if e != nil {
				iconvAPI.err = errors.New("系统字符转码函数不可用")
				return
			}
			purego.RegisterFunc(entry.function, ptr)
		}
	})
	if iconvAPI.err != nil {
		return nil, iconvAPI.err
	}
	handle := iconvAPI.open(strings.ToUpper(to), strings.ToUpper(from))
	if handle == ^uintptr(0) {
		return nil, errors.New("系统不支持选择的字符编码")
	}
	return &native{handle: handle, from: from, to: to}, nil
}
func (n *native) convert(src []byte) ([]byte, error) {
	return convertSupplementary(src, n.from, n.to, n.systemConvert)
}
func (n *native) systemConvert(src []byte) ([]byte, error) {
	if n.closed {
		return nil, errors.New("字符转码会话已关闭")
	}
	if len(src) == 0 {
		return nil, nil
	}
	dst := make([]byte, len(src)*4+32)
	input, output := unsafe.Pointer(&src[0]), unsafe.Pointer(&dst[0])
	left, space := uintptr(len(src)), uintptr(len(dst))
	result := iconvAPI.convert(n.handle, &input, &left, &output, &space)
	runtime.KeepAlive(src)
	runtime.KeepAlive(dst)
	if result == ^uintptr(0) || left != 0 {
		return nil, errors.New("文本与选定字符编码不兼容，请切换 UTF-8 / GB18030 / GBK")
	}
	return dst[:len(dst)-int(space)], nil
}
func (n *native) Close() error {
	if !n.closed {
		iconvAPI.close(n.handle)
		n.closed = true
	}
	return nil
}
