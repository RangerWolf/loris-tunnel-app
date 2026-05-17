//go:build !windows

package autorestart

import (
	"os/exec"
	"strings"
	"syscall"
)

func posixSingleQuote(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `'"'"'`) + `'`
}

func scheduleRelaunch(exe string, args []string) error {
	var b strings.Builder
	b.WriteString("sleep 2; exec ")
	b.WriteString(posixSingleQuote(exe))
	for _, a := range args {
		b.WriteString(" ")
		b.WriteString(posixSingleQuote(a))
	}
	cmd := exec.Command("/bin/sh", "-c", b.String())
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	return cmd.Start()
}
