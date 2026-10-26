//go:build windows

package systemauth

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

type windowsBackend struct{ window uintptr }

func newBackend(window uintptr) Backend { return windowsBackend{window} }
func (windowsBackend) Name() string     { return "Windows Hello" }

var combase = windows.NewLazySystemDLL("combase.dll")

func hr(r uintptr) error {
	if int32(uint32(r)) < 0 {
		return fmt.Errorf("Windows 认证接口错误 0x%08x", uint32(r))
	}
	return nil
}

//go:uintptrescapes
func method(object uintptr, index int, args ...uintptr) error {
	vtable := *(*uintptr)(unsafe.Pointer(object))
	fn := *(*uintptr)(unsafe.Pointer(vtable + uintptr(index)*unsafe.Sizeof(uintptr(0))))
	params := append([]uintptr{object}, args...)
	r, _, _ := syscall.SyscallN(fn, params...)
	return hr(r)
}
func guid(s string) windows.GUID {
	g, e := windows.GUIDFromString(s)
	if e != nil {
		panic(e)
	}
	return g
}
func hstring(s string) (uintptr, error) {
	u, e := windows.UTF16FromString(s)
	if e != nil {
		return 0, e
	}
	var h uintptr
	r, _, _ := combase.NewProc("WindowsCreateString").Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h)))
	return h, hr(r)
}
func delString(h uintptr) { combase.NewProc("WindowsDeleteString").Call(h) }

// WinRT generic interface UUID algorithm (UUIDv5 over the type signature).
func asyncIID(t string) windows.GUID {
	ns, _ := hex.DecodeString("11f47ad57b7342c0abae878b1e16adee")
	h := sha1.New()
	h.Write(ns)
	h.Write([]byte("pinterface({9fc2b0bb-e446-44e2-aa61-9cab8f636af2};enum(Windows.Security.Credentials.UI." + t + ";i4))"))
	b := h.Sum(nil)
	b[6] = (b[6] & 15) | 80
	b[8] = (b[8] & 63) | 128
	g := windows.GUID{Data1: binary.BigEndian.Uint32(b[:4]), Data2: binary.BigEndian.Uint16(b[4:6]), Data3: binary.BigEndian.Uint16(b[6:8])}
	copy(g.Data4[:], b[8:16])
	return g
}
func factory(iid windows.GUID) (uintptr, func(), error) {
	runtime.LockOSThread()
	r, _, _ := combase.NewProc("RoInitialize").Call(1)
	if e := hr(r); e != nil {
		runtime.UnlockOSThread()
		return 0, nil, e
	}
	h, e := hstring("Windows.Security.Credentials.UI.UserConsentVerifier")
	if e != nil {
		runtime.UnlockOSThread()
		return 0, nil, e
	}
	defer delString(h)
	var p uintptr
	r, _, _ = combase.NewProc("RoGetActivationFactory").Call(h, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&p)))
	if e = hr(r); e != nil {
		combase.NewProc("RoUninitialize").Call()
		runtime.UnlockOSThread()
		return 0, nil, e
	}
	return p, func() { method(p, 2); combase.NewProc("RoUninitialize").Call(); runtime.UnlockOSThread() }, nil
}
func await(ctx context.Context, op uintptr) (int32, error) {
	defer method(op, 2)
	iid := guid("{00000036-0000-0000-C000-000000000046}")
	var info uintptr
	if e := method(op, 0, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&info))); e != nil {
		return 0, e
	}
	defer method(info, 2)
	defer method(info, 10)
	for {
		var status int32
		if e := method(info, 7, uintptr(unsafe.Pointer(&status))); e != nil {
			return 0, e
		}
		switch status {
		case 1:
			var result int32
			e := method(op, 8, uintptr(unsafe.Pointer(&result)))
			return result, e
		case 2, 3:
			return 0, errors.New("系统认证已取消或失败")
		}
		select {
		case <-ctx.Done():
			method(info, 9)
			return 0, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
func (windowsBackend) Probe(ctx context.Context) error {
	p, release, e := factory(guid("{AF4F3F91-564C-4DDC-B8B5-973447627C65}"))
	if e != nil {
		return e
	}
	defer release()
	var op uintptr
	if e = method(p, 6, uintptr(unsafe.Pointer(&op))); e != nil {
		return e
	}
	result, e := await(ctx, op)
	if e != nil {
		return e
	}
	if result != 0 {
		return fmt.Errorf("Windows Hello 尚不可用（状态 %d）", result)
	}
	return nil
}
func (b windowsBackend) verify(ctx context.Context, reason string) error {
	p, release, e := factory(guid("{39E050C3-4E74-441A-8DC0-B81104DF949C}"))
	if e != nil {
		return e
	}
	defer release()
	hwnd := b.window
	if hwnd == 0 {
		hwnd, _, _ = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow").Call()
	}
	if hwnd == 0 {
		return errors.New("Windows Hello 需要可用的桌面或终端窗口")
	}
	h, e := hstring(reason)
	if e != nil {
		return e
	}
	defer delString(h)
	iid := asyncIID("UserConsentVerificationResult")
	var op uintptr
	if e = method(p, 6, hwnd, h, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&op))); e != nil {
		return e
	}
	result, e := await(ctx, op)
	if e != nil {
		return e
	}
	if result != 0 {
		return fmt.Errorf("Windows Hello 未批准（状态 %d）", result)
	}
	return nil
}
func (b windowsBackend) Wrap(ctx context.Context, key []byte) (json.RawMessage, error) {
	if e := b.verify(ctx, "启用 SmallSSH 系统解锁"); e != nil {
		return nil, e
	}
	in := windows.DataBlob{Size: uint32(len(key)), Data: &key[0]}
	var out windows.DataBlob
	label, _ := windows.UTF16PtrFromString("SmallSSH")
	if e := windows.CryptProtectData(&in, label, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); e != nil {
		return nil, e
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(out.Data))))
	return json.Marshal(append([]byte(nil), unsafe.Slice(out.Data, out.Size)...))
}
func (b windowsBackend) Unwrap(ctx context.Context, raw json.RawMessage, reason string) ([]byte, error) {
	if e := b.verify(ctx, reason); e != nil {
		return nil, e
	}
	var data []byte
	if e := json.Unmarshal(raw, &data); e != nil || len(data) == 0 {
		return nil, errors.New("系统解锁数据无效")
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	if e := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); e != nil {
		return nil, e
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(out.Data))))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}
