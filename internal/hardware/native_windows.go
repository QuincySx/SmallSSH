//go:build windows

package hardware

import (
	"golang.org/x/sys/windows"
	"os/exec"
	"syscall"
)

func configureWorker(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

type word = uint32

// Cryptoki's Windows ABI uses 32-bit unsigned long and one-byte packing.
const functionOffset = 2
const attributeSize = 16
const valueOffset = 4
const lengthOffset = 12
const mechanismSize = 16

func loadModule(path string) (uintptr, func(), error) {
	h, e := windows.LoadLibraryEx(path, 0, windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_DEFAULT_DIRS)
	if e != nil {
		return 0, nil, e
	}
	symbol, e := windows.GetProcAddress(h, "C_GetFunctionList")
	if e != nil {
		windows.FreeLibrary(h)
		return 0, nil, e
	}
	return symbol, func() { windows.FreeLibrary(h) }, nil
}
