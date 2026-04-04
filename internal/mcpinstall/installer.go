package mcpinstall

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"loris-tunnel/internal/model"
)

const (
	serverName = "loris_ssh_pilot"
)

type installTarget struct {
	model.MCPInstallTarget
	kind string
}

type cursorMCPConfig struct {
	MCPServers map[string]cursorServerConfig `json:"mcpServers"`
}

type cursorServerConfig struct {
	Type        string `json:"type,omitempty"`
	URL         string `json:"url,omitempty"`
	BaseURL     string `json:"baseUrl,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

type cherryImportConfig struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	BaseURL     string `json:"baseUrl"`
	URL         string `json:"url,omitempty"`
}

func ListTargets() ([]model.MCPInstallTarget, error) {
	targets, err := buildTargets()
	if err != nil {
		return nil, err
	}
	out := make([]model.MCPInstallTarget, 0, len(targets))
	for _, item := range targets {
		out = append(out, item.MCPInstallTarget)
	}
	return out, nil
}

func Install(targetIDs []string, endpointURL string) (model.MCPInstallResult, error) {
	targets, err := buildTargets()
	if err != nil {
		return model.MCPInstallResult{}, err
	}
	if strings.TrimSpace(endpointURL) == "" {
		return model.MCPInstallResult{}, fmt.Errorf("mcp endpoint url is empty")
	}

	byID := make(map[string]installTarget, len(targets))
	for _, item := range targets {
		byID[item.ID] = item
	}

	results := make([]model.MCPInstallItemResult, 0, len(targetIDs))
	seen := make(map[string]struct{}, len(targetIDs))
	for _, rawID := range targetIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}

		target, ok := byID[id]
		if !ok {
			results = append(results, model.MCPInstallItemResult{
				TargetID:   id,
				TargetName: id,
				Success:    false,
				Message:    "Unsupported target",
			})
			continue
		}
		if !target.Available {
			results = append(results, model.MCPInstallItemResult{
				TargetID:      target.ID,
				TargetName:    target.Name,
				InstalledPath: target.Path,
				Success:       false,
				Message:       target.Reason,
			})
			continue
		}

		var installErr error
		switch target.kind {
		case "cursor":
			installErr = installCursor(target.Path, endpointURL)
		case "codex":
			installErr = installCodex(target.Path, endpointURL)
		default:
			installErr = fmt.Errorf("unsupported install target")
		}

		if installErr != nil {
			results = append(results, model.MCPInstallItemResult{
				TargetID:      target.ID,
				TargetName:    target.Name,
				InstalledPath: target.Path,
				Success:       false,
				Message:       installErr.Error(),
			})
			continue
		}

		results = append(results, model.MCPInstallItemResult{
			TargetID:      target.ID,
			TargetName:    target.Name,
			InstalledPath: target.Path,
			Success:       true,
			Message:       "Installed MCP server config",
		})
	}

	return model.MCPInstallResult{Results: results}, nil
}

func BuildInstallJSON(endpointURL string) (string, error) {
	if strings.TrimSpace(endpointURL) == "" {
		return "", fmt.Errorf("mcp endpoint url is empty")
	}
	cfg := cherryImportConfig{
		Name:        "Loris SSH Pilot MCP",
		Type:        "streamableHttp",
		Description: "Read-only execute_bash via Loris Tunnel MCP HTTP endpoint",
		BaseURL:     endpointURL,
		URL:         endpointURL,
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func buildTargets() ([]installTarget, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return nil, fmt.Errorf("cannot resolve user home directory")
	}

	cursorConfigPath := filepath.Join(home, ".cursor", "mcp.json")
	cursorInstalled, cursorReason := detectCursorInstalled(home)

	codexConfigPath := filepath.Join(home, ".codex", "config.toml")
	codexInstalled, codexReason := detectCodexInstalled(home)

	cherryConfigPath := cherryStudioConfigPath(home)
	_, cherryReason := detectCherryStudioInstalled(home)

	return []installTarget{
		{
			MCPInstallTarget: model.MCPInstallTarget{
				ID:          "cursor",
				Name:        "Cursor",
				Description: "Install MCP server to Cursor global mcp.json",
				Path:        cursorConfigPath,
				Available:   cursorInstalled,
				Reason:      cursorReason,
			},
			kind: "cursor",
		},
		{
			MCPInstallTarget: model.MCPInstallTarget{
				ID:          "codex",
				Name:        "Codex",
				Description: "Install MCP server block to Codex config.toml",
				Path:        codexConfigPath,
				Available:   codexInstalled,
				Reason:      codexReason,
			},
			kind: "codex",
		},
		{
			MCPInstallTarget: model.MCPInstallTarget{
				ID:          "cherry_studio",
				Name:        "Cherry Studio",
				Description: "Auto-install not supported yet (manual JSON install only)",
				Path:        cherryConfigPath,
				Available:   false,
				Reason:      cherryReason,
			},
			kind: "unsupported",
		},
	}, nil
}

func installCursor(configPath string, endpointURL string) error {
	cfg := cursorMCPConfig{MCPServers: map[string]cursorServerConfig{}}
	if data, err := os.ReadFile(configPath); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parse existing cursor mcp.json failed: %w", err)
		}
	}
	if cfg.MCPServers == nil {
		cfg.MCPServers = map[string]cursorServerConfig{}
	}
	cfg.MCPServers[serverName] = cursorServerConfig{
		Type:        "streamableHttp",
		URL:         endpointURL,
		BaseURL:     endpointURL,
		Name:        "Loris SSH Pilot MCP",
		Description: "Read-only execute_bash via Loris Tunnel MCP HTTP endpoint",
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("create cursor config directory failed: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		return fmt.Errorf("write cursor mcp.json failed: %w", err)
	}
	return nil
}

func installCodex(configPath string, endpointURL string) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("create codex config directory failed: %w", err)
	}

	content := ""
	if data, err := os.ReadFile(configPath); err == nil {
		content = string(data)
	}

	sectionHeader := "[mcp_servers." + serverName + "]"
	if strings.Contains(content, sectionHeader) {
		// Already configured. Keep existing user edits.
		return nil
	}

	block := fmt.Sprintf("\n%s\nurl = %q\n", sectionHeader, endpointURL)
	newContent := strings.TrimRight(content, "\n") + block + "\n"
	if err := os.WriteFile(configPath, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("write codex config.toml failed: %w", err)
	}
	return nil
}

func detectCursorInstalled(home string) (bool, string) {
	switch runtime.GOOS {
	case "darwin":
		paths := []string{
			"/Applications/Cursor.app",
			filepath.Join(home, "Applications", "Cursor.app"),
		}
		for _, p := range paths {
			if pathExists(p) {
				return true, ""
			}
		}
		return false, "Cursor app not found in /Applications or ~/Applications"
	case "windows":
		paths := []string{
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Cursor", "Cursor.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Cursor", "Cursor.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Cursor", "Cursor.exe"),
		}
		for _, p := range paths {
			if pathExists(p) {
				return true, ""
			}
		}
		return false, "Cursor.exe not found in common install locations"
	default:
		if _, err := exec.LookPath("cursor"); err == nil {
			return true, ""
		}
		return false, "Cursor binary not found in PATH (auto-install supports macOS/Windows best)"
	}
}

func detectCodexInstalled(home string) (bool, string) {
	if pathExists(filepath.Join(home, ".codex")) {
		return true, ""
	}
	if _, err := exec.LookPath("codex"); err == nil {
		return true, ""
	}
	return false, "Codex config or executable not found"
}

func detectCherryStudioInstalled(home string) (bool, string) {
	switch runtime.GOOS {
	case "darwin":
		paths := []string{
			"/Applications/Cherry Studio.app",
			filepath.Join(home, "Applications", "Cherry Studio.app"),
		}
		for _, p := range paths {
			if pathExists(p) {
				return false, "Detected Cherry Studio, but automatic MCP config path is not stable yet"
			}
		}
		return false, "Cherry Studio app not found"
	case "windows":
		paths := []string{
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Cherry Studio", "Cherry Studio.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Cherry Studio", "Cherry Studio.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Cherry Studio", "Cherry Studio.exe"),
		}
		for _, p := range paths {
			if pathExists(p) {
				return false, "Detected Cherry Studio, but automatic MCP config path is not stable yet"
			}
		}
		return false, "Cherry Studio.exe not found"
	default:
		return false, "Auto-install for Cherry Studio currently supports macOS/Windows best"
	}
}

func cherryStudioConfigPath(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "CherryStudio")
	case "windows":
		base := strings.TrimSpace(os.Getenv("APPDATA"))
		if base == "" {
			base = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(base, "CherryStudio")
	default:
		return filepath.Join(home, ".config", "CherryStudio")
	}
}

func pathExists(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
