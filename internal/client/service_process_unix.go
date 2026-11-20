//go:build !windows

package client

import (
	"os/exec"
	"syscall"
)

func configureServiceProcess(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
