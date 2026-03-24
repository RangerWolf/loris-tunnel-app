package model

// SSHConfigImportCandidate describes one explicit host alias parsed from ~/.ssh/config.
type SSHConfigImportCandidate struct {
	Alias                  string   `json:"alias"`
	Name                   string   `json:"name"`
	Host                   string   `json:"host"`
	Port                   int      `json:"port"`
	User                   string   `json:"user"`
	AuthType               string   `json:"authType"`
	KeyPath                string   `json:"keyPath"`
	AgentSocketPath        string   `json:"agentSocketPath"`
	BypassHostVerification bool     `json:"bypassHostVerification"`
	KeepAliveIntervalMs    int      `json:"keepAliveIntervalMs"`
	TimeoutMs              int      `json:"timeoutMs"`
	HostKeyAlgorithms      string   `json:"hostKeyAlgorithms"`
	ProxyJump              string   `json:"proxyJump"`
	SourcePath             string   `json:"sourcePath"`
	Warnings               []string `json:"warnings"`
}

// SSHConfigImportSource describes a selectable SSH config source file.
type SSHConfigImportSource struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

// SSHConfigImportResult contains the parsed import preview for ~/.ssh/config.
type SSHConfigImportResult struct {
	Candidates []SSHConfigImportCandidate `json:"candidates"`
}
