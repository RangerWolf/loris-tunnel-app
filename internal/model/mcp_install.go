package model

// MCPInstallTarget describes one app target that can receive installed MCP config files.
type MCPInstallTarget struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Available   bool   `json:"available"`
	Reason      string `json:"reason"`
}

// MCPInstallRequest includes selected target app ids.
type MCPInstallRequest struct {
	TargetIDs []string `json:"targetIds"`
}

// MCPInstallItemResult is the per-target install result.
type MCPInstallItemResult struct {
	TargetID      string `json:"targetId"`
	TargetName    string `json:"targetName"`
	InstalledPath string `json:"installedPath"`
	Success       bool   `json:"success"`
	Message       string `json:"message"`
}

// MCPInstallResult aggregates all per-target install outcomes.
type MCPInstallResult struct {
	Results []MCPInstallItemResult `json:"results"`
}

// Backward-compatibility aliases.
type SkillInstallTarget = MCPInstallTarget
type SkillInstallRequest = MCPInstallRequest
type SkillInstallItemResult = MCPInstallItemResult
type SkillInstallResult = MCPInstallResult
