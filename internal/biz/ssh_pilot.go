package biz

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/forward"
	"loris-tunnel/internal/model"
)

const (
	sshPilotLogLimit   = 200
	sshPilotToolName   = "execute_bash"
	sshPilotProtocol   = "go-mcp (execute_bash + whitelist validation)"
	maxCommandOutputCh = 24000
)

var blockedFragments = []string{
	"&&", "||", ">", "<", "$(", "`",
}

var customCommandPattern = regexp.MustCompile(`^[A-Za-z0-9._:/-]+$`)

type SSHPilotBiz struct {
	storage *conf.Storage

	mu        sync.Mutex
	logs      []model.SSHPilotLogEntry
	nextLogID int64
}

func NewSSHPilotBiz(storage *conf.Storage) *SSHPilotBiz {
	biz := &SSHPilotBiz{storage: storage}
	biz.log("info", "SSH Pilot initialized (execute_bash with command whitelist)")
	return biz
}

func (b *SSHPilotBiz) GetState() (model.SSHPilotState, error) {
	cfg, err := b.storage.Load()
	if err != nil {
		return model.SSHPilotState{}, err
	}

	selectedID := cfg.SSHPilot.SelectedJumperID
	selectedName := ""
	for _, jumper := range cfg.Jumpers {
		if jumper.ID == selectedID {
			selectedName = jumper.Name
			break
		}
	}

	state := model.SSHPilotState{
		Enabled:            cfg.SSHPilot.Enabled,
		Connected:          cfg.SSHPilot.Enabled && selectedID > 0 && selectedName != "",
		SelectedJumperID:   selectedID,
		SelectedJumperName: selectedName,
		Protocol:           sshPilotProtocol,
		CustomCommands:     append([]string{}, cfg.SSHPilot.CustomCommands...),
		AllowedCommands:    append([]model.SSHPilotAllowedCommand{}, sshPilotAllowedCommands(cfg.SSHPilot.CustomCommands)...),
		Logs:               b.listLogs(),
	}

	if cfg.SSHPilot.Enabled {
		if selectedID <= 0 {
			state.LastError = "Please select one jumper before enabling SSH Pilot."
		} else if selectedName == "" {
			state.LastError = "Selected jumper does not exist."
		}
	}

	return state, nil
}

func (b *SSHPilotBiz) UpdateSettings(payload model.SSHPilotUpdatePayload) (model.SSHPilotState, error) {
	var (
		customCommands []string
		err            error
	)
	if payload.CustomCommands != nil {
		customCommands, err = normalizeUserCustomCommands(payload.CustomCommands)
		if err != nil {
			return model.SSHPilotState{}, err
		}
	}

	_, err = b.storage.Update(func(cfg *conf.Config) error {
		cfg.SSHPilot.Enabled = payload.Enabled
		cfg.SSHPilot.SelectedJumperID = payload.SelectedJumperID
		if payload.CustomCommands != nil {
			cfg.SSHPilot.CustomCommands = customCommands
		}
		return nil
	})
	if err != nil {
		return model.SSHPilotState{}, err
	}

	if payload.Enabled {
		b.log("info", fmt.Sprintf("SSH Pilot enabled (jumper_id=%d)", payload.SelectedJumperID))
	} else {
		b.log("warn", "SSH Pilot disabled")
	}

	return b.GetState()
}

func (b *SSHPilotBiz) ExecuteCommand(command string) (model.SSHPilotExecResult, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return model.SSHPilotExecResult{}, fmt.Errorf("command is required")
	}

	cfg, err := b.storage.Load()
	if err != nil {
		return model.SSHPilotExecResult{}, err
	}
	if !cfg.SSHPilot.Enabled {
		return model.SSHPilotExecResult{}, fmt.Errorf("SSH Pilot is disabled")
	}
	if cfg.SSHPilot.SelectedJumperID <= 0 {
		return model.SSHPilotExecResult{}, fmt.Errorf("no jumper selected")
	}

	var target *model.Jumper
	for i := range cfg.Jumpers {
		if cfg.Jumpers[i].ID == cfg.SSHPilot.SelectedJumperID {
			target = &cfg.Jumpers[i]
			break
		}
	}
	if target == nil {
		return model.SSHPilotExecResult{}, fmt.Errorf("selected jumper does not exist")
	}

	usedCommands, err := validateWhitelistCommand(command, allowedCommandSet(cfg.SSHPilot.CustomCommands))
	if err != nil {
		denyMsg := fmt.Sprintf(
			"Command rejected by SSH Pilot whitelist validation: %v. Manual action required: add this command to whitelist in the SSH Pilot App settings, then retry after approval. Do not retry automatically.",
			err,
		)
		b.log("warn", fmt.Sprintf("%s validate: command=%q whitelist=deny reason=%v", sshPilotToolName, command, err))
		return model.SSHPilotExecResult{
			Command:           command,
			ValidatedCommands: []string{},
			Output:            "",
			DurationMs:        0,
			Success:           false,
			Error:             denyMsg,
			ExecutedAt:        time.Now().Format(time.RFC3339),
			JumperID:          target.ID,
			JumperName:        target.Name,
			ToolName:          sshPilotToolName,
			Concurrency:       "single",
		}, nil
	}
	b.log("info", fmt.Sprintf("%s validate: command=%q whitelist=allow commands=%s", sshPilotToolName, command, strings.Join(usedCommands, ",")))

	b.log("info", fmt.Sprintf("%s start: %s on %s", sshPilotToolName, command, target.Name))
	start := time.Now()
	stdout, stderr, runErr := forward.ExecuteRemoteCommand([]model.Jumper{*target}, command)
	elapsed := time.Since(start)

	output := strings.TrimSpace(strings.TrimSpace(stdout) + "\n" + strings.TrimSpace(stderr))
	output = strings.TrimSpace(output)
	if len(output) > maxCommandOutputCh {
		output = output[:maxCommandOutputCh] + "\n...(truncated)"
	}

	result := model.SSHPilotExecResult{
		Command:           command,
		ValidatedCommands: usedCommands,
		Output:            output,
		DurationMs:        elapsed.Milliseconds(),
		Success:           runErr == nil,
		Error:             "",
		ExecutedAt:        time.Now().Format(time.RFC3339),
		JumperID:          target.ID,
		JumperName:        target.Name,
		ToolName:          sshPilotToolName,
		Concurrency:       "single",
	}

	if runErr != nil {
		result.Error = runErr.Error()
		b.log("error", fmt.Sprintf("%s failed on %s: %v", sshPilotToolName, target.Name, runErr))
		return result, nil
	}

	b.log("info", fmt.Sprintf("%s done in %dms (%s)", sshPilotToolName, elapsed.Milliseconds(), strings.Join(usedCommands, ",")))
	return result, nil
}

func (b *SSHPilotBiz) log(level, message string) {
	level = strings.ToLower(strings.TrimSpace(level))
	if level == "" {
		level = "info"
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextLogID++
	entry := model.SSHPilotLogEntry{
		ID:      b.nextLogID,
		Time:    time.Now().Format(time.RFC3339),
		Level:   level,
		Message: message,
	}
	b.logs = append([]model.SSHPilotLogEntry{entry}, b.logs...)
	if len(b.logs) > sshPilotLogLimit {
		b.logs = b.logs[:sshPilotLogLimit]
	}
}

func (b *SSHPilotBiz) listLogs() []model.SSHPilotLogEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]model.SSHPilotLogEntry{}, b.logs...)
}

func validateWhitelistCommand(command string, allowed map[string]struct{}) ([]string, error) {
	if strings.Contains(command, "\n") || strings.Contains(command, "\r") {
		return nil, fmt.Errorf("multi-line command is not allowed")
	}
	if len(command) > 2048 {
		return nil, fmt.Errorf("command is too long")
	}

	lowered := strings.ToLower(command)
	for _, part := range blockedFragments {
		if strings.Contains(lowered, part) {
			return nil, fmt.Errorf("operator %q is not allowed", part)
		}
	}

	pipeSegs := strings.Split(command, "|")
	used := make([]string, 0, len(pipeSegs))

	for _, seg := range pipeSegs {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			return nil, fmt.Errorf("empty command segment is not allowed")
		}
		for _, sub := range splitSemicolonOutsideQuotes(seg) {
			sub = strings.TrimSpace(sub)
			if sub == "" {
				continue
			}
			fields := strings.Fields(sub)
			if len(fields) == 0 {
				return nil, fmt.Errorf("invalid command sub-segment")
			}
			cmd := strings.TrimSpace(fields[0])
			if cmd == "" {
				return nil, fmt.Errorf("invalid executable")
			}
			if _, ok := allowed[cmd]; !ok {
				return nil, fmt.Errorf("command %q is not in whitelist", cmd)
			}
			used = append(used, cmd)
		}
	}

	if len(used) == 0 {
		return nil, fmt.Errorf("invalid command")
	}
	return used, nil
}

// splitSemicolonOutsideQuotes splits on ';' only outside '...' and "..." (sufficient for typical read-only diagnostics; not full bash parsing).
func splitSemicolonOutsideQuotes(cmd string) []string {
	var parts []string
	var b strings.Builder
	inSingle, inDouble := false, false
	for _, r := range cmd {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
			b.WriteRune(r)
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
			b.WriteRune(r)
		case ';':
			if !inSingle && !inDouble {
				p := strings.TrimSpace(b.String())
				if p != "" {
					parts = append(parts, p)
				}
				b.Reset()
			} else {
				b.WriteRune(r)
			}
		default:
			b.WriteRune(r)
		}
	}
	p := strings.TrimSpace(b.String())
	if p != "" {
		parts = append(parts, p)
	}
	return parts
}

func allowedCommandSet(customCommands []string) map[string]struct{} {
	commands := sshPilotAllowedCommands(customCommands)
	out := make(map[string]struct{}, len(commands))
	for _, item := range commands {
		out[item.Command] = struct{}{}
	}
	return out
}

func normalizeUserCustomCommands(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		cmd := strings.TrimSpace(item)
		if cmd == "" {
			continue
		}
		if strings.Contains(cmd, " ") || strings.Contains(cmd, "\t") {
			return nil, fmt.Errorf("custom command %q is invalid: spaces are not allowed", cmd)
		}
		if !customCommandPattern.MatchString(cmd) {
			return nil, fmt.Errorf("custom command %q is invalid: only letters, digits, ., _, :, /, - are allowed", cmd)
		}
		if _, ok := seen[cmd]; ok {
			continue
		}
		seen[cmd] = struct{}{}
		out = append(out, cmd)
	}
	return out, nil
}

func sshPilotAllowedCommands(customCommands []string) []model.SSHPilotAllowedCommand {
	commands := append([]model.SSHPilotAllowedCommand{}, builtinSSHPilotAllowedCommands()...)
	for _, cmd := range customCommands {
		commands = append(commands, model.SSHPilotAllowedCommand{
			ID:          "custom-" + cmd,
			Category:    "custom",
			Name:        cmd,
			Description: "User custom whitelist command",
			Command:     cmd,
			ReadOnly:    false,
		})
	}
	return commands
}

func builtinSSHPilotAllowedCommands() []model.SSHPilotAllowedCommand {
	return []model.SSHPilotAllowedCommand{
		{ID: "cat", Category: "filesystem", Name: "cat", Description: "Read file content", Command: "cat", ReadOnly: true},
		{ID: "df", Category: "filesystem", Name: "df", Description: "Show filesystem usage", Command: "df", ReadOnly: true},
		{ID: "du", Category: "filesystem", Name: "du", Description: "Estimate file space usage", Command: "du", ReadOnly: true},
		{ID: "ls", Category: "filesystem", Name: "ls", Description: "List files", Command: "ls", ReadOnly: true},
		{ID: "grep", Category: "logs", Name: "grep", Description: "Search text patterns", Command: "grep", ReadOnly: true},
		{ID: "head", Category: "logs", Name: "head", Description: "Read top lines", Command: "head", ReadOnly: true},
		{ID: "tail", Category: "logs", Name: "tail", Description: "Read tail lines", Command: "tail", ReadOnly: true},
		{ID: "awk", Category: "process", Name: "awk", Description: "Text filtering", Command: "awk", ReadOnly: true},
		{ID: "sed", Category: "process", Name: "sed", Description: "Text filtering", Command: "sed", ReadOnly: true},
		{ID: "ps", Category: "process", Name: "ps", Description: "Process status", Command: "ps", ReadOnly: true},
		{ID: "uptime", Category: "system", Name: "uptime", Description: "Load average and uptime", Command: "uptime", ReadOnly: true},
		{ID: "free", Category: "system", Name: "free", Description: "Memory usage", Command: "free", ReadOnly: true},
		{ID: "uname", Category: "system", Name: "uname", Description: "System info", Command: "uname", ReadOnly: true},
		{ID: "who", Category: "system", Name: "who", Description: "Current users", Command: "who", ReadOnly: true},
		{ID: "w", Category: "system", Name: "w", Description: "Current users and load", Command: "w", ReadOnly: true},
		{ID: "echo", Category: "system", Name: "echo", Description: "Print markers or labels in chained diagnostics", Command: "echo", ReadOnly: true},
		{ID: "systemctl", Category: "system", Name: "systemctl", Description: "systemd unit inspection (e.g. status)", Command: "systemctl", ReadOnly: true},
		{ID: "ss", Category: "network", Name: "ss", Description: "Socket stats", Command: "ss", ReadOnly: true},
		{ID: "netstat", Category: "network", Name: "netstat", Description: "Network stats", Command: "netstat", ReadOnly: true},
		{ID: "hostname", Category: "network", Name: "hostname", Description: "Hostname and addresses (e.g. hostname -I)", Command: "hostname", ReadOnly: true},
		{ID: "ip", Category: "network", Name: "ip", Description: "iproute2 show (addr, route, link)", Command: "ip", ReadOnly: true},
		{ID: "curl", Category: "network", Name: "curl", Description: "HTTP(S) fetch (e.g. public IP echo services)", Command: "curl", ReadOnly: true},
		{ID: "dig", Category: "network", Name: "dig", Description: "DNS lookup", Command: "dig", ReadOnly: true},
		{ID: "host", Category: "network", Name: "host", Description: "Simple DNS lookup", Command: "host", ReadOnly: true},
		{ID: "nginx", Category: "nginx", Name: "nginx", Description: "Nginx inspect only", Command: "nginx", ReadOnly: true},
		{ID: "docker", Category: "docker", Name: "docker", Description: "Docker inspect commands", Command: "docker", ReadOnly: true},
		{ID: "redis-cli", Category: "redis", Name: "redis-cli", Description: "Redis inspect commands", Command: "redis-cli", ReadOnly: true},
	}
}
