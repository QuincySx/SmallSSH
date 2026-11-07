//go:build windows

package terminalcodec

import (
	"errors"
	"golang.org/x/sys/windows"
	"runtime"
	"unsafe"
)

var kernel = windows.NewLazySystemDLL("kernel32.dll")
var multiToWide = kernel.NewProc("MultiByteToWideChar")
var wideToMulti = kernel.NewProc("WideCharToMultiByte")

type native struct {
	from, to         uintptr
	fromName, toName string
}

func newConverter(from, to string) (converter, error) {
	pages := map[string]uintptr{"utf-8": 65001, "gb18030": 54936, "gbk": 936}
	return &native{from: pages[from], to: pages[to], fromName: from, toName: to}, nil
}
func (n *native) convert(src []byte) ([]byte, error) {
	return convertSupplementary(src, n.fromName, n.toName, n.systemConvert)
}
func (n *native) systemConvert(src []byte) ([]byte, error) {
	if len(src) == 0 {
		return nil, nil
	}
	size, _, _ := multiToWide.Call(n.from, 8, uintptr(unsafe.Pointer(&src[0])), uintptr(len(src)), 0, 0)
	if size == 0 {
		return nil, errors.New("终端文本不是有效的选定编码")
	}
	wide := make([]uint16, size)
	converted, _, _ := multiToWide.Call(n.from, 8, uintptr(unsafe.Pointer(&src[0])), uintptr(len(src)), uintptr(unsafe.Pointer(&wide[0])), size)
	runtime.KeepAlive(src)
	if converted == 0 {
		return nil, errors.New("无法解码终端文本")
	}
	flags := uintptr(0)
	if n.to == 54936 || n.to == 65001 {
		flags = 128
	}
	var used uint32
	var usedPtr uintptr
	if n.to == 936 {
		usedPtr = uintptr(unsafe.Pointer(&used))
	}
	length, _, _ := wideToMulti.Call(n.to, flags, uintptr(unsafe.Pointer(&wide[0])), converted, 0, 0, 0, usedPtr)
	if length == 0 {
		return nil, errors.New("字符编码不可用")
	}
	dst := make([]byte, length)
	result, _, _ := wideToMulti.Call(n.to, flags, uintptr(unsafe.Pointer(&wide[0])), converted, uintptr(unsafe.Pointer(&dst[0])), length, 0, usedPtr)
	runtime.KeepAlive(wide)
	runtime.KeepAlive(dst)
	runtime.KeepAlive(&used)
	if result == 0 || used != 0 {
		return nil, errors.New("文本包含选定编码无法表示的字符")
	}
	return dst[:result], nil
}
func (n *native) Close() error { return nil }
