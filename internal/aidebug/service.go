package aidebug

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"loris-tunnel/internal/model"
)

const (
	llmRequestTimeout = 25 * time.Second
	probeTimeout      = 6 * time.Second
	sshDebugTimeout   = 12 * time.Second
	maxDebugChars     = 12000
	maxExcerptChars   = 4000
)

type Service struct {
	httpClient     *http.Client
	backendBaseURL string
	machineID      string
}

type DiagnosticInput struct {
	TargetType  string
	RawError    string
	UILocale    string
	Tunnel      *model.Tunnel
	JumperChain []model.Jumper
}

type sshClient struct {
	Path    string
	Version string
}

type remoteDiagnoseRequest struct {
	MachineID     string               `json:"machine_id"`
	TargetType    string               `json:"target_type"`
	UILocale      string               `json:"ui_locale"`
	RawError      string               `json:"raw_error"`
	MatchedRules  []string             `json:"matched_rules"`
	Checks        []model.AIDebugCheck `json:"checks"`
	SSHClientPath string               `json:"ssh_client_path"`
	SSHVersion    string               `json:"ssh_version"`
	DebugOutput   string               `json:"debug_output"`
	Tunnel        map[string]any       `json:"tunnel,omitempty"`
	Jumpers       []map[string]any     `json:"jumpers,omitempty"`
}

type apiErrorResponse struct {
	Detail  string `json:"detail"`
	Message string `json:"message"`
}

func NewService(backendBaseURL, machineID string) *Service {
	return &Service{
		httpClient:     &http.Client{Timeout: llmRequestTimeout},
		backendBaseURL: strings.TrimRight(strings.TrimSpace(backendBaseURL), "/"),
		machineID:      strings.TrimSpace(machineID),
	}
}

func (s *Service) Diagnose(ctx context.Context, input DiagnosticInput) (model.AIDebugResult, error) {
	locale := normalizeLocale(input.UILocale)
	checks := make([]model.AIDebugCheck, 0, 8)
	chain := normalizeJumpers(input.JumperChain)
	rawError := strings.TrimSpace(input.RawError)

	if len(chain) == 0 {
		checks = append(checks, model.AIDebugCheck{Name: "config", Status: "skipped", Detail: "No jumper chain available for SSH debug."})
		fallback := fallbackResult(locale, rawError, []string{"No jumper chain available."}, checks)
		return fallback, nil
	}

	first := chain[0]
	last := chain[len(chain)-1]

	checks = append(checks, dnsCheck(ctx, first.Host)...)
	checks = append(checks, tcpCheck(ctx, first.Host, first.Port, "tcp_first_hop"))
	if len(chain) > 1 && (last.Host != first.Host || last.Port != first.Port) {
		checks = append(checks, tcpCheck(ctx, last.Host, last.Port, "tcp_final_hop"))
	}
	checks = append(checks, pingCheck(ctx, first.Host))

	for _, hop := range chain {
		if strings.TrimSpace(hop.KeyPath) != "" {
			checks = append(checks, checkKeyFile(hop.KeyPath))
		}
	}

	client, clientChecks := discoverSSHClient(ctx)
	checks = append(checks, clientChecks...)

	debugOutput := ""
	if client.Path != "" {
		cfgPath, cleanup, cfgChecks := buildSSHConfig(chain)
		checks = append(checks, cfgChecks...)
		if cleanup != nil {
			defer cleanup()
		}
		stdout, stderr, exitCode, err := runSSHDebug(ctx, client.Path, cfgPath, chain)
		debugOutput = summarizeCommandOutput(stdout, stderr, exitCode, err)
		status := "ok"
		detail := fmt.Sprintf("ssh debug command executed with exit code %d.", exitCode)
		if err != nil {
			status = "error"
			detail = fmt.Sprintf("ssh debug command finished with error: %v", err)
		}
		checks = append(checks, model.AIDebugCheck{Name: "ssh_debug", Status: status, Detail: detail})
	} else {
		checks = append(checks, model.AIDebugCheck{Name: "ssh_debug", Status: "skipped", Detail: "No usable SSH client found."})
	}

	rules := matchRules(rawError, debugOutput)
	llmResult, err := s.callLLM(ctx, locale, rawError, rules, checks, client, debugOutput, input)
	if err != nil {
		return model.AIDebugResult{}, err
	}
	checks = append(checks, model.AIDebugCheck{Name: "llm", Status: "ok", Detail: "AI analysis completed."})
	llmResult.SSHClientPath = client.Path
	llmResult.SSHVersion = client.Version
	llmResult.RawError = rawError
	llmResult.MatchedRules = append([]string{}, rules...)
	llmResult.Checks = checks
	llmResult.DebugExcerpt = trimText(debugOutput, maxExcerptChars)
	llmResult.Locale = locale
	llmResult.UsedFallback = false
	return llmResult, nil
}

func normalizeJumpers(items []model.Jumper) []model.Jumper {
	out := make([]model.Jumper, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Host) == "" {
			continue
		}
		if item.Port <= 0 {
			item.Port = 22
		}
		if item.TimeoutMs <= 0 {
			item.TimeoutMs = 5000
		}
		out = append(out, item)
	}
	return out
}

func normalizeLocale(locale string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return "en"
	}
	return locale
}

func dnsCheck(ctx context.Context, host string) []model.AIDebugCheck {
	host = strings.TrimSpace(host)
	if host == "" {
		return []model.AIDebugCheck{{Name: "dns", Status: "skipped", Detail: "Host is empty."}}
	}
	lookupCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil {
		return []model.AIDebugCheck{{Name: "dns", Status: "error", Detail: fmt.Sprintf("DNS lookup failed: %v", err)}}
	}
	values := make([]string, 0, len(ips))
	for _, ip := range ips {
		values = append(values, ip.IP.String())
	}
	return []model.AIDebugCheck{{Name: "dns", Status: "ok", Detail: fmt.Sprintf("Resolved %s to %s.", host, strings.Join(values, ", "))}}
}

func tcpCheck(ctx context.Context, host string, port int, name string) model.AIDebugCheck {
	if port <= 0 {
		port = 22
	}
	addr := net.JoinHostPort(strings.TrimSpace(host), strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: probeTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return model.AIDebugCheck{Name: name, Status: "error", Detail: fmt.Sprintf("TCP connect to %s failed: %v", addr, err)}
	}
	_ = conn.Close()
	return model.AIDebugCheck{Name: name, Status: "ok", Detail: fmt.Sprintf("TCP connect to %s succeeded.", addr)}
}

func pingCheck(ctx context.Context, host string) model.AIDebugCheck {
	path, err := exec.LookPath("ping")
	if err != nil {
		return model.AIDebugCheck{Name: "ping", Status: "skipped", Detail: "ping command not found."}
	}
	args := []string{"-c", "1", "-W", "1000", host}
	if runtime.GOOS == "windows" {
		args = []string{"-n", "1", "-w", "1000", host}
	}
	pingCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(pingCtx, path, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return model.AIDebugCheck{Name: "ping", Status: "error", Detail: trimText(fmt.Sprintf("ping failed: %v | %s", err, string(output)), 240)}
	}
	return model.AIDebugCheck{Name: "ping", Status: "ok", Detail: trimText(string(output), 240)}
}

func checkKeyFile(keyPath string) model.AIDebugCheck {
	rawPath := strings.TrimSpace(keyPath)
	resolvedPath, err := resolveKeyPath(rawPath)
	if err != nil {
		return model.AIDebugCheck{Name: "key_file", Status: "error", Detail: fmt.Sprintf("Failed to resolve key file path: %s (%v)", rawPath, err)}
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			if resolvedPath != rawPath {
				return model.AIDebugCheck{Name: "key_file", Status: "error", Detail: fmt.Sprintf("Key file does not exist: %s (resolved to %s)", rawPath, resolvedPath)}
			}
			return model.AIDebugCheck{Name: "key_file", Status: "error", Detail: fmt.Sprintf("Key file does not exist: %s", rawPath)}
		}
		return model.AIDebugCheck{Name: "key_file", Status: "error", Detail: fmt.Sprintf("Failed to check key file: %s (%v)", resolvedPath, err)}
	}
	if info.IsDir() {
		return model.AIDebugCheck{Name: "key_file", Status: "error", Detail: fmt.Sprintf("Key file path is a directory: %s", resolvedPath)}
	}

	f, err := os.Open(resolvedPath)
	if err != nil {
		return model.AIDebugCheck{Name: "key_file", Status: "error", Detail: fmt.Sprintf("Key file is not readable: %s (%v)", resolvedPath, err)}
	}
	f.Close()

	if resolvedPath != rawPath {
		return model.AIDebugCheck{Name: "key_file", Status: "ok", Detail: fmt.Sprintf("Key file exists and is readable: %s (resolved from %s)", resolvedPath, rawPath)}
	}
	return model.AIDebugCheck{Name: "key_file", Status: "ok", Detail: fmt.Sprintf("Key file exists and is readable: %s", resolvedPath)}
}

func resolveKeyPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("ssh_key auth requires keyPath")
	}

	if strings.HasPrefix(trimmed, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir failed: %w", err)
		}
		trimmed = filepath.Join(home, strings.TrimPrefix(trimmed, "~/"))
	}

	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed), nil
	}

	if _, err := os.Stat(trimmed); err == nil {
		return filepath.Clean(trimmed), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Clean(trimmed), nil
	}
	fromSSHDir := filepath.Join(home, ".ssh", trimmed)
	if _, err := os.Stat(fromSSHDir); err == nil {
		return filepath.Clean(fromSSHDir), nil
	}

	return filepath.Clean(trimmed), nil
}

func discoverSSHClient(ctx context.Context) (sshClient, []model.AIDebugCheck) {
	_ = ctx
	candidates := sshClientCandidates()
	seen := make(map[string]struct{})
	checks := make([]model.AIDebugCheck, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[strings.ToLower(candidate)]; ok {
			continue
		}
		seen[strings.ToLower(candidate)] = struct{}{}
		path, err := resolveExecutable(candidate)
		if err != nil {
			checks = append(checks, model.AIDebugCheck{Name: "ssh_client", Status: "error", Detail: fmt.Sprintf("Candidate %s unavailable: %v", candidate, err)})
			continue
		}
		version := detectSSHVersion(path)
		checks = append(checks, model.AIDebugCheck{Name: "ssh_client", Status: "ok", Detail: fmt.Sprintf("Using SSH client %s (%s)", path, version)})
		return sshClient{Path: path, Version: version}, checks
	}
	checks = append(checks, model.AIDebugCheck{Name: "ssh_client", Status: "error", Detail: "No usable SSH client found."})
	return sshClient{}, checks
}

func sshClientCandidates() []string {
	candidates := []string{"ssh"}
	if runtime.GOOS == "windows" {
		candidates = append(candidates, `C:\\Windows\\System32\\OpenSSH\\ssh.exe`)
		for _, envKey := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
			base := strings.TrimSpace(os.Getenv(envKey))
			if base == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(base, "Git", "usr", "bin", "ssh.exe"),
				filepath.Join(base, "Git", "bin", "ssh.exe"),
				filepath.Join(base, "Git", "mingw64", "bin", "ssh.exe"),
				filepath.Join(base, "Git", "mingw32", "bin", "ssh.exe"),
			)
		}
	}
	return candidates
}

func resolveExecutable(candidate string) (string, error) {
	if strings.Contains(candidate, string(os.PathSeparator)) || filepath.IsAbs(candidate) {
		if _, err := os.Stat(candidate); err != nil {
			return "", err
		}
		return candidate, nil
	}
	return exec.LookPath(candidate)
}

func detectSSHVersion(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-V")
	output, err := cmd.CombinedOutput()
	if err != nil && len(output) == 0 {
		return "unknown"
	}
	version := strings.TrimSpace(string(output))
	if version == "" {
		return "unknown"
	}
	return version
}

func buildSSHConfig(chain []model.Jumper) (string, func(), []model.AIDebugCheck) {
	if len(chain) == 0 {
		return "", nil, []model.AIDebugCheck{{Name: "ssh_config", Status: "skipped", Detail: "No jumpers available."}}
	}
	var buf bytes.Buffer
	for i, hop := range chain {
		alias := fmt.Sprintf("loris-hop-%d", i+1)
		buf.WriteString("Host ")
		buf.WriteString(alias)
		buf.WriteString("\n")
		buf.WriteString("  HostName ")
		buf.WriteString(strings.TrimSpace(hop.Host))
		buf.WriteString("\n")
		buf.WriteString("  User ")
		buf.WriteString(defaultIfEmpty(strings.TrimSpace(hop.User), "root"))
		buf.WriteString("\n")
		buf.WriteString("  Port ")
		buf.WriteString(strconv.Itoa(defaultPort(hop.Port)))
		buf.WriteString("\n")
		buf.WriteString("  ConnectTimeout ")
		buf.WriteString(strconv.Itoa(defaultTimeoutSeconds(hop.TimeoutMs)))
		buf.WriteString("\n")
		buf.WriteString("  LogLevel DEBUG3\n")
		buf.WriteString("  NumberOfPasswordPrompts 1\n")
		if hop.BypassHostVerification {
			buf.WriteString("  StrictHostKeyChecking no\n")
			buf.WriteString("  UserKnownHostsFile ")
			buf.WriteString(os.DevNull)
			buf.WriteString("\n")
		}
		switch strings.TrimSpace(hop.AuthType) {
		case "password":
			buf.WriteString("  PreferredAuthentications password,keyboard-interactive\n")
			buf.WriteString("  PubkeyAuthentication no\n")
		case "ssh_key":
			buf.WriteString("  PreferredAuthentications publickey\n")
			buf.WriteString("  PubkeyAuthentication yes\n")
			if keyPath := strings.TrimSpace(hop.KeyPath); keyPath != "" {
				resolvedKeyPath, err := resolveKeyPath(keyPath)
				if err != nil {
					resolvedKeyPath = keyPath
				}
				buf.WriteString("  IdentityFile ")
				buf.WriteString(resolvedKeyPath)
				buf.WriteString("\n")
				buf.WriteString("  IdentitiesOnly yes\n")
			}
		default:
			buf.WriteString("  PreferredAuthentications publickey\n")
			buf.WriteString("  PubkeyAuthentication yes\n")
		}
		if i > 0 {
			buf.WriteString("  ProxyJump loris-hop-")
			buf.WriteString(strconv.Itoa(i))
			buf.WriteString("\n")
		}
		buf.WriteString("\n")
	}
	file, err := os.CreateTemp("", "loris-ai-debug-ssh-*.conf")
	if err != nil {
		return "", nil, []model.AIDebugCheck{{Name: "ssh_config", Status: "error", Detail: fmt.Sprintf("Create temp ssh config failed: %v", err)}}
	}
	if _, err := file.WriteString(buf.String()); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return "", nil, []model.AIDebugCheck{{Name: "ssh_config", Status: "error", Detail: fmt.Sprintf("Write temp ssh config failed: %v", err)}}
	}
	_ = file.Close()
	cleanup := func() { _ = os.Remove(file.Name()) }
	return file.Name(), cleanup, []model.AIDebugCheck{{Name: "ssh_config", Status: "ok", Detail: fmt.Sprintf("Built temporary SSH config for %d hop(s).", len(chain))}}
}

func defaultTimeoutSeconds(timeoutMs int) int {
	if timeoutMs <= 0 {
		return 5
	}
	sec := timeoutMs / 1000
	if sec <= 0 {
		sec = 5
	}
	return sec
}

func defaultPort(port int) int {
	if port <= 0 {
		return 22
	}
	return port
}

func runSSHDebug(ctx context.Context, sshPath, cfgPath string, chain []model.Jumper) (string, string, int, error) {
	debugCtx, cancel := context.WithTimeout(ctx, sshDebugTimeout)
	defer cancel()
	alias := fmt.Sprintf("loris-hop-%d", len(chain))
	args := []string{"-F", cfgPath, "-vvv", "-T", alias, "exit"}
	cmd := exec.CommandContext(debugCtx, sshPath, args...)
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	return stdout.String(), stderr.String(), exitCode, err
}

func summarizeCommandOutput(stdout, stderr string, exitCode int, err error) string {
	parts := []string{fmt.Sprintf("exit_code=%d", exitCode)}
	if err != nil {
		parts = append(parts, fmt.Sprintf("command_error=%v", err))
	}
	if strings.TrimSpace(stdout) != "" {
		parts = append(parts, "stdout:\n"+trimText(stdout, maxDebugChars/2))
	}
	if strings.TrimSpace(stderr) != "" {
		parts = append(parts, "stderr:\n"+trimText(stderr, maxDebugChars))
	}
	return trimText(strings.Join(parts, "\n\n"), maxDebugChars)
}

func matchRules(rawError, debugOutput string) []string {
	text := strings.ToLower(strings.TrimSpace(rawError + "\n" + debugOutput))
	rules := make([]string, 0, 6)
	add := func(rule string, match bool) {
		if !match {
			return
		}
		for _, existing := range rules {
			if existing == rule {
				return
			}
		}
		rules = append(rules, rule)
	}
	add("Host key verification issue", strings.Contains(text, "host key verification failed") || strings.Contains(text, "knownhosts") || strings.Contains(text, "host key mismatch"))
	add("Public key was offered but rejected by remote server", publicKeyOfferedButRejected(text))
	add("Public key authentication failed", strings.Contains(text, "permission denied (publickey)") || strings.Contains(text, "unable to authenticate"))
	add("SSH agent has no usable identity", strings.Contains(text, "ssh agent has no identities") || strings.Contains(text, "no identities"))
	add("Key file is invalid or unreadable", strings.Contains(text, "read key file failed") || strings.Contains(text, "parse key failed") || strings.Contains(text, "identity file"))
	add("Remote host or port is unreachable", strings.Contains(text, "connection refused") || strings.Contains(text, "no route to host") || strings.Contains(text, "network is unreachable"))
	add("DNS resolution failed", strings.Contains(text, "no such host") || strings.Contains(text, "could not resolve hostname"))
	add("TCP connection timed out", strings.Contains(text, "i/o timeout") || strings.Contains(text, "operation timed out") || strings.Contains(text, "connection timed out"))
	add("Too many SSH authentication attempts", strings.Contains(text, "too many authentication failures"))
	add("Port forwarding is disabled by SSH server", strings.Contains(text, "administratively prohibited") || strings.Contains(text, "forwarding disabled"))
	sort.Strings(rules)
	return rules
}

func publicKeyOfferedButRejected(text string) bool {
	hasAuthFailure := strings.Contains(text, "permission denied (publickey)") ||
		strings.Contains(text, "unable to authenticate") ||
		strings.Contains(text, "no supported methods remain")
	hasPublicKeyAuth := strings.Contains(text, "authentications that can continue: publickey") ||
		strings.Contains(text, "attempted methods [none publickey]") ||
		strings.Contains(text, "offering public key") ||
		strings.Contains(text, "trying private key")
	hasLocalKeyReadError := strings.Contains(text, "read key file failed") ||
		strings.Contains(text, "parse key failed") ||
		strings.Contains(text, "no such identity") ||
		strings.Contains(text, "no such file or directory") ||
		strings.Contains(text, "identity file") && strings.Contains(text, "not accessible")
	return hasAuthFailure && hasPublicKeyAuth && !hasLocalKeyReadError
}

func fallbackResult(locale, rawError string, rules []string, checks []model.AIDebugCheck) model.AIDebugResult {
	reason := localizedText(locale,
		"Unable to complete AI analysis yet.",
		"暂时无法完成 AI 分析。",
		"暂時無法完成 AI 分析。",
		"暂時無法完成 AI 分析。",
		"Пока не удалось завершить AI-анализ.",
	)
	summary := localizedText(locale,
		"Using local diagnostics only. Review the matched rule and SSH debug details below.",
		"当前先展示本地诊断结果，请结合命中的规则和 SSH 调试详情继续排查。",
		"目前先展示本地診斷結果，請結合命中的規則和 SSH 偵錯詳情繼續排查。",
		"目前先展示本地診斷結果，請結合命中的規則和 SSH 偵錯詳情繼續排查。",
		"Сейчас показывается локальная диагностика. Проверьте совпавшие правила и детали SSH-отладки ниже.",
	)
	steps := []string{localizedText(locale,
		"Review the SSH debug details to locate the failing stage.",
		"先查看 SSH 调试详情，确认失败发生在哪个阶段。",
		"先查看 SSH 偵錯詳情，確認失敗發生在哪個階段。",
		"先查看 SSH 偵錯詳情，確認失敗發生在哪個階段。",
		"Сначала проверьте детали SSH-отладки и определите, на каком этапе произошёл сбой.",
	)}
	if len(rules) > 0 {
		steps = append(steps, localizedText(locale,
			"Start with the matched rule: "+rules[0],
			"优先从命中的规则开始排查："+rules[0],
			"優先從命中的規則開始排查："+rules[0],
			"優先從命中的規則開始排查："+rules[0],
			"Сначала проверьте совпавшее правило: "+rules[0],
		))
	}
	if rawError != "" {
		steps = append(steps, localizedText(locale,
			"Compare the raw error with the suggested next steps.",
			"将原始错误与建议步骤对照查看。",
			"將原始錯誤與建議步驟對照查看。",
			"將原始錯誤與建議步驟對照查看。",
			"Сопоставьте исходную ошибку с предложенными шагами.",
		))
	}
	if len(steps) > 3 {
		steps = steps[:3]
	}
	return model.AIDebugResult{
		Reason:       reason,
		Summary:      summary,
		Steps:        steps,
		Confidence:   "low",
		Locale:       locale,
		MatchedRules: append([]string{}, rules...),
		Checks:       append([]model.AIDebugCheck{}, checks...),
		UsedFallback: true,
	}
}

func localizedText(locale, en, zhCN, zhTW, zhHK, ru string) string {
	locale = strings.ToLower(strings.TrimSpace(locale))
	switch {
	case strings.HasPrefix(locale, "zh-cn") || locale == "zh":
		return zhCN
	case strings.HasPrefix(locale, "zh-tw"):
		return zhTW
	case strings.HasPrefix(locale, "zh-hk"):
		return zhHK
	case strings.HasPrefix(locale, "ru"):
		return ru
	default:
		return en
	}
}

func trimText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	head := limit * 2 / 3
	tail := limit - head - len("\n...\n")
	if tail < 0 {
		tail = 0
	}
	return text[:head] + "\n...\n" + text[len(text)-tail:]
}

func defaultIfEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (s *Service) callLLM(ctx context.Context, locale, rawError string, rules []string, checks []model.AIDebugCheck, client sshClient, debugOutput string, input DiagnosticInput) (model.AIDebugResult, error) {
	if s == nil || strings.TrimSpace(s.backendBaseURL) == "" || strings.TrimSpace(s.machineID) == "" {
		return model.AIDebugResult{}, fmt.Errorf("ai debug backend is not configured")
	}
	payload := remoteDiagnoseRequest{
		MachineID:     s.machineID,
		TargetType:    strings.TrimSpace(input.TargetType),
		UILocale:      locale,
		RawError:      trimText(rawError, maxExcerptChars),
		MatchedRules:  append([]string{}, rules...),
		Checks:        checks,
		SSHClientPath: client.Path,
		SSHVersion:    client.Version,
		DebugOutput:   trimText(debugOutput, maxDebugChars),
		Tunnel:        tunnelSummary(input.Tunnel),
		Jumpers:       jumpersSummary(input.JumperChain),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return model.AIDebugResult{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, llmRequestTimeout)
	defer cancel()
	path := strings.TrimRight(s.backendBaseURL, "/") + "/ai-debug/diagnose"
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		return model.AIDebugResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return model.AIDebugResult{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.AIDebugResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return model.AIDebugResult{}, buildAPIError(resp.StatusCode, respBody)
	}
	var result model.AIDebugResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return model.AIDebugResult{}, err
	}
	result.Locale = defaultIfEmpty(result.Locale, locale)
	result.UsedFallback = false
	return result, nil
}

func buildAPIError(statusCode int, body []byte) error {
	var payload apiErrorResponse
	if err := json.Unmarshal(body, &payload); err == nil {
		if msg := strings.TrimSpace(payload.Detail); msg != "" {
			return fmt.Errorf("ai debug request failed: HTTP %d: %s", statusCode, msg)
		}
		if msg := strings.TrimSpace(payload.Message); msg != "" {
			return fmt.Errorf("ai debug request failed: HTTP %d: %s", statusCode, msg)
		}
	}
	return fmt.Errorf("ai debug request failed: HTTP %d: %s", statusCode, trimText(string(body), 400))
}

func normalizeLLMConfidence(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		text := strings.ToLower(strings.TrimSpace(typed))
		switch text {
		case "high", "高", "высокая", "высокий", "высокое":
			return "high"
		case "medium", "中", "средняя", "средний", "среднее":
			return "medium"
		case "low", "低", "низкая", "низкий", "низкое":
			return "low"
		default:
			if parsed, err := strconv.ParseFloat(text, 64); err == nil {
				return confidenceFromNumber(parsed)
			}
			return text
		}
	case float64:
		return confidenceFromNumber(typed)
	case float32:
		return confidenceFromNumber(float64(typed))
	case int:
		return confidenceFromNumber(float64(typed))
	case int64:
		return confidenceFromNumber(float64(typed))
	case bool:
		if typed {
			return "high"
		}
		return "low"
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func confidenceFromNumber(value float64) string {
	if value > 1 {
		value = value / 100
	}
	if value >= 0.67 {
		return "high"
	}
	if value >= 0.34 {
		return "medium"
	}
	return "low"
}

func tunnelSummary(tunnel *model.Tunnel) map[string]any {
	if tunnel == nil {
		return nil
	}
	return map[string]any{
		"name":       strings.TrimSpace(tunnel.Name),
		"mode":       strings.TrimSpace(tunnel.Mode),
		"localHost":  strings.TrimSpace(tunnel.LocalHost),
		"localPort":  tunnel.LocalPort,
		"remoteHost": strings.TrimSpace(tunnel.RemoteHost),
		"remotePort": tunnel.RemotePort,
	}
}

func jumpersSummary(jumpers []model.Jumper) []map[string]any {
	if len(jumpers) == 0 {
		return nil
	}
	items := make([]map[string]any, 0, len(jumpers))
	for _, jumper := range jumpers {
		items = append(items, map[string]any{
			"name":                   strings.TrimSpace(jumper.Name),
			"host":                   strings.TrimSpace(jumper.Host),
			"port":                   jumper.Port,
			"user":                   strings.TrimSpace(jumper.User),
			"authType":               strings.TrimSpace(jumper.AuthType),
			"keyPath":                strings.TrimSpace(jumper.KeyPath),
			"agentSocketPath":        strings.TrimSpace(jumper.AgentSocketPath),
			"bypassHostVerification": jumper.BypassHostVerification,
		})
	}
	return items
}
