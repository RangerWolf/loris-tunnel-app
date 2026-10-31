//go:build windows

package frp

import (
	"os/exec"
	"syscall"
)

func configureCommand(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }

func requestStop(cmd *exec.Cmd) { forceStop(cmd) }

func forceStop(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
