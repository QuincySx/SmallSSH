package client

import (
	"os/exec"
	"syscall"
)

func configureServiceProcess(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
