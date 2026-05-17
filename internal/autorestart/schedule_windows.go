//go:build windows

package autorestart

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func scheduleRelaunch(exe string, args []string) error {
	var parts strings.Builder
	parts.WriteString(strconv.Quote(exe))
	for _, a := range args {
		parts.WriteString(" ")
		parts.WriteString(strconv.Quote(a))
	}
	// ~2s delay so SingleInstanceLock is released before the new UI starts.
	inner := `timeout /t 2 /nobreak >nul & start "" ` + parts.String()
	cmd := exec.Command("cmd.exe", "/C", inner)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}
	return cmd.Start()
}
