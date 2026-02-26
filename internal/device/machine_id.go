package device

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

var darwinUUIDPattern = regexp.MustCompile(`"IOPlatformUUID"\s*=\s*"([^"]+)"`)

// MachineID returns a stable machine identifier.
// It prefers OS-provided hardware/system IDs and falls back to a deterministic hash.
func MachineID() string {
	if id := strings.TrimSpace(machineIDFromOS()); id != "" {
		return normalizeID(id)
	}
	return fallbackMachineID()
}

func machineIDFromOS() string {
	switch runtime.GOOS {
	case "darwin":
		return machineIDFromDarwin()
	case "linux":
		return machineIDFromLinux()
	case "windows":
		return machineIDFromWindows()
	default:
		return ""
	}
}

func machineIDFromDarwin() string {
	output, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return ""
	}
	match := darwinUUIDPattern.FindStringSubmatch(string(output))
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func machineIDFromLinux() string {
	candidates := []string{"/etc/machine-id", "/var/lib/dbus/machine-id"}
	for _, file := range candidates {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		id := strings.TrimSpace(string(content))
		if id != "" {
			return id
		}
	}
	return ""
}

func machineIDFromWindows() string {
	output, err := exec.Command("reg", "query", `HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Cryptography`, "/v", "MachineGuid").Output()
	if err != nil {
		return ""
	}
	text := strings.ReplaceAll(string(output), "\r\n", "\n")
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), "machineguid") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			return fields[len(fields)-1]
		}
	}
	return ""
}

func fallbackMachineID() string {
	parts := []string{runtime.GOOS}

	var macs []string
	interfaces, err := net.Interfaces()
	if err == nil {
		for _, nic := range interfaces {
			if nic.Flags&net.FlagLoopback != 0 {
				continue
			}
			if len(nic.HardwareAddr) == 0 {
				continue
			}
			macs = append(macs, strings.ToLower(nic.HardwareAddr.String()))
		}
	}
	sort.Strings(macs)
	parts = append(parts, strings.Join(macs, ","))

	if hostname, err := os.Hostname(); err == nil {
		parts = append(parts, strings.ToLower(strings.TrimSpace(hostname)))
	}

	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	// Keep identifier compact but stable.
	return "fallback-" + hex.EncodeToString(sum[:16])
}

func normalizeID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}
