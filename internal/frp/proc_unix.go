//go:build !windows

package frp

import (
	"os/exec"
	"syscall"
)

func configureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func requestStop(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	signalPID(cmd.Process.Pid, syscall.SIGTERM)
}

func forceStop(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	signalPID(cmd.Process.Pid, syscall.SIGKILL)
}

func signalPID(pid int, sig syscall.Signal) {
	if pid <= 1 {
		return
	}
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid == pid {
		if syscall.Kill(-pid, sig) == nil {
			return
		}
	}
	_ = syscall.Kill(pid, sig)
}
