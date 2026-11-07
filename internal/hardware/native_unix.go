//go:build darwin || linux

package hardware

import (
	"github.com/ebitengine/purego"
	"os/exec"
)

func configureWorker(*exec.Cmd) {}

type word = uint64

const functionOffset = 8
const attributeSize = 24
const valueOffset = 8
const lengthOffset = 16
const mechanismSize = 24

func loadModule(path string) (uintptr, func(), error) {
	h, e := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if e != nil {
		return 0, nil, e
	}
	symbol, e := purego.Dlsym(h, "C_GetFunctionList")
	if e != nil {
		purego.Dlclose(h)
		return 0, nil, e
	}
	return symbol, func() { purego.Dlclose(h) }, nil
}
