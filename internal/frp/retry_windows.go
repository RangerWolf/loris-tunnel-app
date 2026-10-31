//go:build windows

package frp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
)

func orphanProcesses(ctx context.Context) ([]orphanProcess, error) {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "@(Get-CimInstance Win32_Process | Select-Object ProcessId,ParentProcessId,@{Name='CreationDate';Expression={$_.CreationDate.ToUniversalTime().ToString('o')}},CommandLine) | ConvertTo-Json -Compress")
	configureCommand(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ProcessId, ParentProcessId int
		CreationDate, CommandLine  string
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, err
	}
	alive := make(map[int]bool)
	for _, row := range rows {
		alive[row.ProcessId] = true
	}
	var result []orphanProcess
	for _, row := range rows {
		if row.ProcessId > 1 && row.ParentProcessId > 0 && !alive[row.ParentProcessId] && row.CreationDate != "" {
			result = append(result, orphanProcess{pid: row.ProcessId, identity: row.CreationDate, command: row.CommandLine})
		}
	}
	return result, nil
}

func usesRetryConfig(command, binary, config string) bool {
	args, err := windows.DecomposeCommandLine(command)
	return err == nil && len(args) == 3 && strings.EqualFold(args[0], binary) && args[1] == "-c" && strings.EqualFold(args[2], config)
}
func signalOrphan(pid int, force bool) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	defer process.Release()
	return process.Kill()
}

func inspectRetryPID(ctx context.Context, pid int) (orphanProcess, bool, bool, error) {
	script := "$p=Get-CimInstance Win32_Process -Filter 'ProcessId=" + strconv.Itoa(pid) + "' -ErrorAction Stop; if($p){ $parent=Get-CimInstance Win32_Process -Filter ('ProcessId='+$p.ParentProcessId) -ErrorAction Stop; [pscustomobject]@{Pid=$p.ProcessId; Identity=$p.CreationDate.ToUniversalTime().ToString('o'); Command=$p.CommandLine; Orphan=($p.ParentProcessId -gt 0 -and $null -eq $parent)} | ConvertTo-Json -Compress }"
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	configureCommand(cmd)
	out, err := cmd.Output()
	if err != nil {
		return orphanProcess{}, false, false, err
	}
	if strings.TrimSpace(string(out)) == "" {
		return orphanProcess{}, false, false, nil
	}
	var row struct {
		Pid               int
		Identity, Command string
		Orphan            bool
	}
	if err := json.Unmarshal(out, &row); err != nil {
		return orphanProcess{}, false, false, err
	}
	return orphanProcess{pid: row.Pid, identity: row.Identity, command: row.Command}, true, row.Orphan, nil
}
