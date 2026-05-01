package model

type AIDebugCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type AIDebugResult struct {
	Reason        string         `json:"reason"`
	Summary       string         `json:"summary"`
	Steps         []string       `json:"steps"`
	Confidence    string         `json:"confidence"`
	Locale        string         `json:"locale"`
	SSHClientPath string         `json:"sshClientPath"`
	SSHVersion    string         `json:"sshVersion"`
	RawError      string         `json:"rawError"`
	MatchedRules  []string       `json:"matchedRules"`
	Checks        []AIDebugCheck `json:"checks"`
	DebugExcerpt  string         `json:"debugExcerpt"`
	UsedFallback  bool           `json:"usedFallback"`
}
