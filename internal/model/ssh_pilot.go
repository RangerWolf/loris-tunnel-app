package model

// SSHPilotAllowedCommand describes one allowed executable for execute_bash validation.
type SSHPilotAllowedCommand struct {
	ID          string `json:"id"`
	Category    string `json:"category"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Command     string `json:"command"`
	ReadOnly    bool   `json:"readOnly"`
}

// SSHPilotLogEntry is an in-memory operation log for SSH Pilot actions.
type SSHPilotLogEntry struct {
	ID      int64  `json:"id"`
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// SSHPilotState is the UI-facing snapshot of SSH Pilot runtime state.
type SSHPilotState struct {
	Enabled            bool                     `json:"enabled"`
	Connected          bool                     `json:"connected"`
	SelectedJumperID   int                      `json:"selectedJumperId"`
	SelectedJumperName string                   `json:"selectedJumperName"`
	Protocol           string                   `json:"protocol"`
	LastError          string                   `json:"lastError"`
	CustomCommands     []string                 `json:"customCommands"`
	AllowedCommands    []SSHPilotAllowedCommand `json:"allowedCommands"`
	Logs               []SSHPilotLogEntry       `json:"logs"`
}

// SSHPilotUpdatePayload updates the selected jumper and enabled state from UI.
type SSHPilotUpdatePayload struct {
	Enabled          bool     `json:"enabled"`
	SelectedJumperID int      `json:"selectedJumperId"`
	CustomCommands   []string `json:"customCommands"`
}

// SSHPilotExecResult is returned after executing one validated command.
type SSHPilotExecResult struct {
	Command           string   `json:"command"`
	ValidatedCommands []string `json:"validatedCommands"`
	Output            string   `json:"output"`
	DurationMs        int64    `json:"durationMs"`
	Success           bool     `json:"success"`
	Error             string   `json:"error"`
	ExecutedAt        string   `json:"executedAt"`
	JumperID          int      `json:"jumperId"`
	JumperName        string   `json:"jumperName"`
	ToolName          string   `json:"toolName"`
	Concurrency       string   `json:"concurrency"`
}
