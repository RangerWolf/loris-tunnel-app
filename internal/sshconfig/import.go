package sshconfig

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"loris-tunnel/internal/model"
)

type configEntry struct {
	patterns []hostPattern
	options  []configOption
	source   string
}

type hostPattern struct {
	value   string
	negated bool
}

type configOption struct {
	key   string
	value string
}

type resolvedAlias struct {
	host                   string
	user                   string
	port                   int
	keyPath                string
	agentSocketPath        string
	bypassHostVerification bool
	keepAliveIntervalMs    int
	timeoutMs              int
	hostKeyAlgorithms      string
	proxyJump              string
	sourcePath             string
}

type parser struct {
	entries      []configEntry
	visitedFiles map[string]struct{}
}

// DefaultConfigPath returns the default user SSH config path.
func DefaultConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory failed: %w", err)
	}
	return filepath.Join(homeDir, ".ssh", "config"), nil
}

// GetImportSources returns selectable SSH config sources for the importer UI.
func GetImportSources() ([]model.SSHConfigImportSource, error) {
	defaultPath, err := DefaultConfigPath()
	if err != nil {
		return nil, err
	}
	return []model.SSHConfigImportSource{
		{
			Label: defaultPath,
			Path:  defaultPath,
		},
	}, nil
}

// LoadImportCandidates parses an SSH config file and produces explicit host aliases
// suitable for importing into jumpers.
func LoadImportCandidates(configPath string) (model.SSHConfigImportResult, error) {
	resolvedPath, err := resolveConfigPath(configPath)
	if err != nil {
		return model.SSHConfigImportResult{}, err
	}

	absPath, err := filepath.Abs(resolvedPath)
	if err != nil {
		return model.SSHConfigImportResult{}, fmt.Errorf("resolve ssh config path failed: %w", err)
	}
	if _, err := os.Stat(absPath); err != nil {
		if os.IsNotExist(err) {
			return model.SSHConfigImportResult{}, fmt.Errorf("SSH config not found at %s", absPath)
		}
		return model.SSHConfigImportResult{}, fmt.Errorf("stat ssh config failed: %w", err)
	}

	p := &parser{
		visitedFiles: make(map[string]struct{}),
	}
	if err := p.parseFile(absPath); err != nil {
		return model.SSHConfigImportResult{}, err
	}

	aliases := collectExplicitAliases(p.entries)
	candidates := make([]model.SSHConfigImportCandidate, 0, len(aliases))
	for _, alias := range aliases {
		resolved := resolveAlias(alias, p.entries)
		candidates = append(candidates, model.SSHConfigImportCandidate{
			Alias:                  alias,
			Name:                   alias,
			Host:                   resolved.host,
			Port:                   resolved.port,
			User:                   resolved.user,
			AuthType:               deriveAuthType(resolved),
			KeyPath:                resolved.keyPath,
			AgentSocketPath:        resolved.agentSocketPath,
			BypassHostVerification: resolved.bypassHostVerification,
			KeepAliveIntervalMs:    resolved.keepAliveIntervalMs,
			TimeoutMs:              resolved.timeoutMs,
			HostKeyAlgorithms:      resolved.hostKeyAlgorithms,
			ProxyJump:              resolved.proxyJump,
			SourcePath:             resolved.sourcePath,
			Warnings:               deriveWarnings(resolved),
		})
	}

	return model.SSHConfigImportResult{
		Candidates: candidates,
	}, nil
}

func resolveConfigPath(configPath string) (string, error) {
	if strings.TrimSpace(configPath) != "" {
		return expandPath(configPath, "")
	}
	return DefaultConfigPath()
}

func (p *parser) parseFile(filePath string) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("resolve config file path failed: %w", err)
	}
	if _, ok := p.visitedFiles[absPath]; ok {
		return nil
	}
	p.visitedFiles[absPath] = struct{}{}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read ssh config failed: %w", err)
	}

	current := configEntry{source: absPath}
	lines := strings.Split(string(content), "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(stripInlineComment(rawLine))
		if line == "" {
			continue
		}

		key, value, ok := splitDirective(line)
		if !ok {
			continue
		}
		lowerKey := strings.ToLower(key)

		switch lowerKey {
		case "host":
			if len(current.options) > 0 || len(current.patterns) > 0 {
				p.entries = append(p.entries, current)
			}
			current = configEntry{
				patterns: parseHostPatterns(value),
				source:   absPath,
			}
		case "include":
			for _, includePath := range splitValueList(value) {
				resolvedInclude, includeErr := expandPath(includePath, filepath.Dir(absPath))
				if includeErr != nil {
					return fmt.Errorf("resolve include %q failed: %w", includePath, includeErr)
				}
				matches, globErr := filepath.Glob(resolvedInclude)
				if globErr != nil {
					return fmt.Errorf("glob include %q failed: %w", includePath, globErr)
				}
				slices.Sort(matches)
				for _, match := range matches {
					info, statErr := os.Stat(match)
					if statErr != nil || info.IsDir() {
						continue
					}
					if err := p.parseFile(match); err != nil {
						return err
					}
				}
			}
		case "match":
			// Match blocks are intentionally ignored for now because the app imports
			// static aliases only.
			continue
		default:
			current.options = append(current.options, configOption{
				key:   lowerKey,
				value: cleanValue(value),
			})
		}
	}

	if len(current.options) > 0 || len(current.patterns) > 0 {
		p.entries = append(p.entries, current)
	}
	return nil
}

func collectExplicitAliases(entries []configEntry) []string {
	aliases := make([]string, 0)
	seen := make(map[string]struct{})
	for _, entry := range entries {
		for _, pattern := range entry.patterns {
			if pattern.negated || !isExplicitAlias(pattern.value) {
				continue
			}
			alias := strings.TrimSpace(pattern.value)
			if alias == "" {
				continue
			}
			key := strings.ToLower(alias)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			aliases = append(aliases, alias)
		}
	}
	return aliases
}

func resolveAlias(alias string, entries []configEntry) resolvedAlias {
	out := resolvedAlias{
		host:                alias,
		port:                22,
		keepAliveIntervalMs: 5000,
		timeoutMs:           5000,
	}

	for _, entry := range entries {
		if !entryMatchesAlias(entry, alias) {
			continue
		}
		if out.sourcePath == "" {
			out.sourcePath = entry.source
		}
		for _, option := range entry.options {
			switch option.key {
			case "hostname":
				if out.host == alias && option.value != "" {
					out.host = option.value
				}
			case "user":
				if out.user == "" && option.value != "" {
					out.user = option.value
				}
			case "port":
				if out.port == 22 {
					if value, ok := parsePositiveInt(option.value); ok {
						out.port = value
					}
				}
			case "identityfile":
				if out.keyPath == "" && option.value != "" {
					out.keyPath = option.value
				}
			case "identityagent":
				if out.agentSocketPath == "" && !strings.EqualFold(option.value, "none") {
					out.agentSocketPath = option.value
				}
			case "stricthostkeychecking":
				switch strings.ToLower(option.value) {
				case "no", "off":
					out.bypassHostVerification = true
				case "yes", "ask", "accept-new":
					out.bypassHostVerification = false
				}
			case "serveraliveinterval":
				if out.keepAliveIntervalMs == 5000 {
					if value, ok := parsePositiveInt(option.value); ok {
						out.keepAliveIntervalMs = value * 1000
					}
				}
			case "connecttimeout":
				if out.timeoutMs == 5000 {
					if value, ok := parsePositiveInt(option.value); ok {
						out.timeoutMs = value * 1000
					}
				}
			case "hostkeyalgorithms":
				if out.hostKeyAlgorithms == "" && option.value != "" {
					out.hostKeyAlgorithms = option.value
				}
			case "proxyjump":
				if out.proxyJump == "" && !strings.EqualFold(option.value, "none") {
					out.proxyJump = option.value
				}
			}
		}
	}

	if strings.TrimSpace(out.host) == "" {
		out.host = alias
	}
	return out
}

func deriveAuthType(resolved resolvedAlias) string {
	if strings.TrimSpace(resolved.keyPath) != "" {
		return "ssh_key"
	}
	return "ssh_agent"
}

func deriveWarnings(resolved resolvedAlias) []string {
	warnings := make([]string, 0, 3)
	if strings.TrimSpace(resolved.user) == "" {
		warnings = append(warnings, "missing_user")
	}
	if strings.TrimSpace(resolved.host) == "" {
		warnings = append(warnings, "missing_host")
	}
	if strings.TrimSpace(resolved.proxyJump) != "" {
		warnings = append(warnings, "proxy_jump")
	}
	if strings.TrimSpace(resolved.keyPath) == "" && strings.TrimSpace(resolved.agentSocketPath) == "" {
		warnings = append(warnings, "agent_fallback")
	}
	return warnings
}

func entryMatchesAlias(entry configEntry, alias string) bool {
	if len(entry.patterns) == 0 {
		return true
	}

	matchedPositive := false
	hasPositive := false
	for _, patternItem := range entry.patterns {
		if patternItem.value == "" {
			continue
		}
		matched := matchPattern(patternItem.value, alias)
		if patternItem.negated && matched {
			return false
		}
		if !patternItem.negated {
			hasPositive = true
			if matched {
				matchedPositive = true
			}
		}
	}
	if !hasPositive {
		return false
	}
	return matchedPositive
}

func matchPattern(patternValue, alias string) bool {
	patternLower := strings.ToLower(strings.TrimSpace(patternValue))
	aliasLower := strings.ToLower(strings.TrimSpace(alias))
	if patternLower == "" || aliasLower == "" {
		return false
	}
	matched, err := path.Match(patternLower, aliasLower)
	if err != nil {
		return patternLower == aliasLower
	}
	return matched
}

func parseHostPatterns(value string) []hostPattern {
	fields := strings.Fields(value)
	patterns := make([]hostPattern, 0, len(fields))
	for _, field := range fields {
		cleaned := cleanValue(field)
		if cleaned == "" {
			continue
		}
		patternItem := hostPattern{value: cleaned}
		if strings.HasPrefix(cleaned, "!") {
			patternItem.negated = true
			patternItem.value = strings.TrimPrefix(cleaned, "!")
		}
		patterns = append(patterns, patternItem)
	}
	return patterns
}

func isExplicitAlias(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && trimmed != "*" && !strings.ContainsAny(trimmed, "*?")
}

func splitValueList(value string) []string {
	fields := strings.Fields(value)
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		cleaned := cleanValue(field)
		if cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return out
}

func parsePositiveInt(value string) (int, bool) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return parsed, true
}

func splitDirective(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", "", false
	}

	if index := strings.IndexAny(trimmed, " \t="); index >= 0 {
		key := strings.TrimSpace(trimmed[:index])
		value := strings.TrimSpace(trimmed[index+1:])
		if strings.HasPrefix(value, "=") {
			value = strings.TrimSpace(strings.TrimPrefix(value, "="))
		}
		return key, value, key != ""
	}
	return trimmed, "", true
}

func stripInlineComment(line string) string {
	var inSingleQuote bool
	var inDoubleQuote bool
	for i, r := range line {
		switch r {
		case '\'':
			if !inDoubleQuote {
				inSingleQuote = !inSingleQuote
			}
		case '"':
			if !inSingleQuote {
				inDoubleQuote = !inDoubleQuote
			}
		case '#':
			if !inSingleQuote && !inDoubleQuote {
				return line[:i]
			}
		}
	}
	return line
}

func cleanValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) >= 2 {
		if (strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"")) ||
			(strings.HasPrefix(trimmed, "'") && strings.HasSuffix(trimmed, "'")) {
			return trimmed[1 : len(trimmed)-1]
		}
	}
	return trimmed
}

func expandPath(rawPath string, baseDir string) (string, error) {
	expanded := os.ExpandEnv(strings.TrimSpace(rawPath))
	if expanded == "" {
		return "", nil
	}

	if strings.HasPrefix(expanded, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory failed: %w", err)
		}
		switch {
		case expanded == "~":
			expanded = homeDir
		case strings.HasPrefix(expanded, "~/"):
			expanded = filepath.Join(homeDir, expanded[2:])
		}
	}

	if filepath.IsAbs(expanded) {
		return filepath.Clean(expanded), nil
	}
	if strings.TrimSpace(baseDir) == "" {
		return filepath.Clean(expanded), nil
	}
	return filepath.Clean(filepath.Join(baseDir, expanded)), nil
}
