//go:build unix

package frp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func orphanProcesses(ctx context.Context) ([]orphanProcess, error) {
	cmd := exec.CommandContext(ctx, "ps", "-axww", "-o", "pid=,ppid=,stat=,lstart=,command=")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var result []orphanProcess
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 9 || fields[1] != "1" || strings.HasPrefix(fields[2], "Z") {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 1 {
			continue
		}
		// ps prints argv without quoting; preserve spaces within executable and config paths.
		rest := strings.TrimSpace(line)
		for range 8 {
			_, rest, _ = strings.Cut(rest, " ")
			rest = strings.TrimSpace(rest)
		}
		result = append(result, orphanProcess{pid: pid, identity: strings.Join(fields[3:8], " "), command: rest})
	}
	return result, nil
}

func usesRetryConfig(command, binary, config string) bool { return command == binary+" -c "+config }
func signalOrphan(pid int, force bool) error {
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	err := syscall.Kill(pid, sig)
	if err == syscall.ESRCH {
		return nil
	}
	return err
}

func inspectRetryPID(ctx context.Context, pid int) (orphanProcess, bool, bool, error) {
	cmd := exec.CommandContext(ctx, "ps", "-ww", "-p", strconv.Itoa(pid), "-o", "pid=,ppid=,stat=,lstart=,command=")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return orphanProcess{}, false, false, ctx.Err()
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 && len(strings.TrimSpace(string(out))) == 0 && len(exit.Stderr) == 0 {
			return orphanProcess{}, false, false, nil
		}
		return orphanProcess{}, false, false, err
	}
	fields := strings.Fields(string(out))
	if len(fields) < 9 {
		return orphanProcess{}, false, false, fmt.Errorf("invalid process inspection response")
	}
	if strings.HasPrefix(fields[2], "Z") {
		return orphanProcess{}, false, false, nil
	}
	rest := strings.TrimSpace(string(out))
	for range 8 {
		_, rest, _ = strings.Cut(rest, " ")
		rest = strings.TrimSpace(rest)
	}
	return orphanProcess{pid: pid, identity: strings.Join(fields[3:8], " "), command: rest}, true, fields[1] == "1", nil
}
