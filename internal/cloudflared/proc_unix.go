//go:build unix

package cloudflared

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func configureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func lockHostFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}

func processIdentity(pid int) (string, error) {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "stat=,lstart=")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return "", nil
		}
		return "", fmt.Errorf("inspect process %d: %w", pid, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 || strings.HasPrefix(fields[0], "Z") {
		return "", nil
	}
	return strings.Join(fields[1:], " "), nil
}

func killHostPID(pid int) {
	if pid <= 1 {
		return
	}
	if group, err := syscall.Getpgid(pid); err == nil && group == pid {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	} else {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) != nil {
		_ = cmd.Process.Kill()
	}
}

func containHostProcess(cmd *exec.Cmd) (func(), error) {
	return func() { killProcessTree(cmd) }, nil
}
