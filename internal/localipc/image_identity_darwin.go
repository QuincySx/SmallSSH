//go:build darwin

package localipc

import (
	"encoding/hex"
	"os"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

func executableCodeID(path string) (string, error) {
	if path == "" || strings.IndexByte(path, 0) >= 0 {
		return "", ErrIdentityUnavailable
	}
	initial, err := os.Stat(path)
	if err != nil || !initial.Mode().IsRegular() {
		return "", ErrIdentityUnavailable
	}
	file, err := os.Open(path)
	if err != nil {
		return "", ErrIdentityUnavailable
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !sameImage(initial, before) || before.Size() > 512<<20 {
		return "", ErrIdentityUnavailable
	}
	onDisk, err := os.Stat(path)
	if err != nil || !sameImage(before, onDisk) {
		return "", ErrIdentityUnavailable
	}
	code, err := staticCodeID(path)
	if err != nil {
		return "", err
	}
	after, err := file.Stat()
	if err != nil || !sameImage(before, after) {
		return "", ErrIdentityUnavailable
	}
	onDisk, err = os.Stat(path)
	if err != nil || !sameImage(before, onDisk) {
		return "", ErrIdentityUnavailable
	}
	return code, nil
}

var staticCodeSecurity struct {
	sync.Once
	err       error
	url       func(uintptr, unsafe.Pointer, int64, uint8) uintptr
	create    func(uintptr, uint32, *uintptr) int32
	valid     func(uintptr, uint32, uintptr) int32
	info      func(uintptr, uint32, *uintptr) int32
	value     func(uintptr, uintptr) uintptr
	length    func(uintptr) int64
	bytes     func(uintptr) unsafe.Pointer
	release   func(uintptr)
	uniqueKey uintptr
}

func staticCodeID(path string) (string, error) {
	staticCodeSecurity.Do(func() {
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			staticCodeSecurity.err = err
			return
		}
		security, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			staticCodeSecurity.err = err
			return
		}
		for _, binding := range []struct {
			library uintptr
			name    string
			fn      any
		}{
			{cf, "CFURLCreateFromFileSystemRepresentation", &staticCodeSecurity.url},
			{cf, "CFDictionaryGetValue", &staticCodeSecurity.value},
			{cf, "CFDataGetLength", &staticCodeSecurity.length},
			{cf, "CFDataGetBytePtr", &staticCodeSecurity.bytes},
			{cf, "CFRelease", &staticCodeSecurity.release},
			{security, "SecStaticCodeCreateWithPath", &staticCodeSecurity.create},
			{security, "SecStaticCodeCheckValidity", &staticCodeSecurity.valid},
			{security, "SecCodeCopySigningInformation", &staticCodeSecurity.info},
		} {
			symbol, err := purego.Dlsym(binding.library, binding.name)
			if err != nil {
				staticCodeSecurity.err = err
				return
			}
			purego.RegisterFunc(binding.fn, symbol)
		}
		var symbolPointer func(uintptr, string) unsafe.Pointer
		purego.RegisterLibFunc(&symbolPointer, cf, "dlsym")
		symbol := symbolPointer(security, "kSecCodeInfoUnique")
		if symbol == nil {
			staticCodeSecurity.err = ErrIdentityUnavailable
			return
		}
		staticCodeSecurity.uniqueKey = *(*uintptr)(symbol)
		if staticCodeSecurity.uniqueKey == 0 {
			staticCodeSecurity.err = ErrIdentityUnavailable
		}
	})
	if staticCodeSecurity.err != nil {
		return "", ErrIdentityUnavailable
	}
	pathBytes := []byte(path)
	url := staticCodeSecurity.url(0, unsafe.Pointer(&pathBytes[0]), int64(len(pathBytes)), 0)
	runtime.KeepAlive(pathBytes)
	if url == 0 {
		return "", ErrIdentityUnavailable
	}
	defer staticCodeSecurity.release(url)
	var code uintptr
	if staticCodeSecurity.create(url, 0, &code) != 0 || code == 0 {
		return "", ErrIdentityUnavailable
	}
	defer staticCodeSecurity.release(code)
	if staticCodeSecurity.valid(code, 0, 0) != 0 {
		return "", ErrIdentityUnavailable
	}
	var information uintptr
	if staticCodeSecurity.info(code, 2, &information) != 0 || information == 0 {
		return "", ErrIdentityUnavailable
	}
	defer staticCodeSecurity.release(information)
	unique := staticCodeSecurity.value(information, staticCodeSecurity.uniqueKey)
	if unique == 0 {
		return "", ErrIdentityUnavailable
	}
	n := staticCodeSecurity.length(unique)
	ptr := staticCodeSecurity.bytes(unique)
	if n < 16 || n > 64 || ptr == nil {
		return "", ErrIdentityUnavailable
	}
	return "cdhash:" + hex.EncodeToString(unsafe.Slice((*byte)(ptr), int(n))), nil
}
