//go:build darwin

package localipc

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

func identify(c net.Conn) (*ProcessIdentity, error) {
	conn, ok := c.(*net.UnixConn)
	if !ok {
		return nil, ErrIdentityUnavailable
	}
	raw, err := conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var token [8]uint32
	length := uint32(unsafe.Sizeof(token))
	var credentialErr error
	if err := raw.Control(func(fd uintptr) {
		_, _, errno := unix.Syscall6(unix.SYS_GETSOCKOPT, fd, unix.SOL_LOCAL, unix.LOCAL_PEERTOKEN, uintptr(unsafe.Pointer(&token)), uintptr(unsafe.Pointer(&length)), 0)
		if errno != 0 {
			credentialErr = errno
		}
	}); err != nil {
		return nil, err
	}
	if credentialErr != nil || length != 32 || token[5] == 0 || token[7] == 0 {
		return nil, ErrIdentityUnavailable
	}
	code, appID, err := auditCodeIdentity(token)
	if err != nil {
		return nil, err
	}
	bytes := unsafe.Slice((*byte)(unsafe.Pointer(&token)), 32)
	digest := sha256.Sum256(bytes)
	p := &ProcessIdentity{PID: int(token[5]), Instance: "darwin-audit:" + hex.EncodeToString(digest[:]), CodeID: code, AppID: appID}
	p.verify = func() error {
		current, currentAppID, err := auditCodeIdentity(token)
		if err != nil || current != code || currentAppID != appID {
			return ErrIdentityUnavailable
		}
		return nil
	}
	return p, nil
}

// Security resolves the audit token, including its process generation, rather
// than looking up a PID and racing a restart/exec between the two lookups.
var codeSecurity struct {
	sync.Once
	err       error
	guestKey  uintptr
	uniqueKey uintptr
	idKey     uintptr
	data      func(uintptr, unsafe.Pointer, int64) uintptr
	dict      func(uintptr, unsafe.Pointer, unsafe.Pointer, int64, uintptr, uintptr) uintptr
	guest     func(uintptr, uintptr, uint32, *uintptr) int32
	valid     func(uintptr, uint32, uintptr) int32
	info      func(uintptr, uint32, *uintptr) int32
	value     func(uintptr, uintptr) uintptr
	length    func(uintptr) int64
	bytes     func(uintptr) unsafe.Pointer
	string    func(uintptr, *byte, int64, uint32) bool
	release   func(uintptr)
}

func auditCodeIdentity(token [8]uint32) (codeID, appID string, err error) {
	codeSecurity.Do(func() {
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			codeSecurity.err = err
			return
		}
		security, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			codeSecurity.err = err
			return
		}
		for _, binding := range []struct {
			library uintptr
			name    string
			fn      any
		}{
			{cf, "CFDataCreate", &codeSecurity.data}, {cf, "CFDictionaryCreate", &codeSecurity.dict},
			{cf, "CFDictionaryGetValue", &codeSecurity.value}, {cf, "CFDataGetLength", &codeSecurity.length},
			{cf, "CFDataGetBytePtr", &codeSecurity.bytes}, {cf, "CFRelease", &codeSecurity.release},
			{cf, "CFStringGetCString", &codeSecurity.string},
			{security, "SecCodeCopyGuestWithAttributes", &codeSecurity.guest},
			{security, "SecCodeCheckValidity", &codeSecurity.valid},
			{security, "SecCodeCopySigningInformation", &codeSecurity.info},
		} {
			symbol, err := purego.Dlsym(binding.library, binding.name)
			if err != nil {
				codeSecurity.err = err
				return
			}
			purego.RegisterFunc(binding.fn, symbol)
		}
		var symbolPointer func(uintptr, string) unsafe.Pointer
		purego.RegisterLibFunc(&symbolPointer, cf, "dlsym")
		for _, key := range []struct {
			name string
			out  *uintptr
		}{{"kSecGuestAttributeAudit", &codeSecurity.guestKey}, {"kSecCodeInfoUnique", &codeSecurity.uniqueKey},
			{"kSecCodeInfoIdentifier", &codeSecurity.idKey}} {
			symbol := symbolPointer(security, key.name)
			if symbol == nil {
				codeSecurity.err = ErrIdentityUnavailable
				return
			}
			*key.out = *(*uintptr)(symbol)
		}
	})
	if codeSecurity.err != nil {
		return "", "", ErrIdentityUnavailable
	}
	s := &codeSecurity
	data := s.data(0, unsafe.Pointer(&token), 32)
	runtime.KeepAlive(token)
	if data == 0 {
		return "", "", ErrIdentityUnavailable
	}
	defer s.release(data)
	attributes := s.dict(0, unsafe.Pointer(&s.guestKey), unsafe.Pointer(&data), 1, 0, 0)
	if attributes == 0 {
		return "", "", ErrIdentityUnavailable
	}
	defer s.release(attributes)
	var code uintptr
	if s.guest(0, attributes, 0, &code) != 0 || code == 0 {
		return "", "", ErrIdentityUnavailable
	}
	defer s.release(code)
	if s.valid(code, 0, 0) != 0 {
		return "", "", ErrIdentityUnavailable
	}
	var information uintptr
	if s.info(code, 2, &information) != 0 || information == 0 {
		return "", "", ErrIdentityUnavailable
	}
	defer s.release(information)
	unique := s.value(information, s.uniqueKey)
	if unique == 0 {
		return "", "", ErrIdentityUnavailable
	}
	n := s.length(unique)
	ptr := s.bytes(unique)
	if n < 16 || n > 64 || ptr == nil {
		return "", "", ErrIdentityUnavailable
	}
	// The signing identifier is part of the validated code signature. An
	// ad-hoc executable can choose its identifier, so it never replaces the hash.
	if identifier := s.value(information, s.idKey); identifier != 0 {
		const utf8Encoding = 0x08000100
		var value [257]byte
		if s.string(identifier, &value[0], int64(len(value)), utf8Encoding) {
			appID = unix.ByteSliceToString(value[:])
		}
	}
	return "cdhash:" + hex.EncodeToString(unsafe.Slice((*byte)(ptr), int(n))), appID, nil
}
