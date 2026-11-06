package devtunnel

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"loris-tunnel/internal/climaint"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const commandTimeout = 45 * time.Second

// Status is the CLI and account snapshot shown on the Providers page.
type Status struct {
	Installed         bool   `json:"installed"`
	Version           string `json:"version"`
	Path              string `json:"path"`
	Source            string `json:"source"`
	AccountState      string `json:"accountState"`
	AccountLabel      string `json:"accountLabel"`
	Detail            string `json:"detail"`
	LoginInProgress   bool   `json:"loginInProgress"`
	LoginPhase        string `json:"loginPhase"`
	LoginURL          string `json:"loginUrl"`
	InstallInProgress bool   `json:"installInProgress"`
	LoginError        string `json:"loginError"`
}

// Client runs the official Dev Tunnel CLI. An empty Binary is resolved at call time.
type Client struct {
	Binary  string
	hostDir string

	mu            sync.Mutex
	version       string
	loginCmd      *exec.Cmd
	loginPhase    string
	loginURL      string
	loginError    string
	installing    bool
	installCancel context.CancelFunc

	regionMu         sync.Mutex
	regionClusters   []clusterRecord
	regionClustersAt time.Time
	regionProbe      RegionProbe
	regionProbedAt   time.Time
	regionFlight     map[string]*regionFlight
}

func NewClient(binary string) *Client {
	client := &Client{Binary: strings.TrimSpace(binary)}
	if client.Binary != "" {
		client.hostDir = filepath.Join(filepath.Dir(client.Binary), ".loris-hosts")
	}
	return client
}

// Inspect reports whether a CLI can be executed and how the account looks.
func (c *Client) Inspect(ctx context.Context) (Status, error) {
	status := Status{AccountState: "unknown"}
	path, source, err := c.resolve()
	c.mu.Lock()
	status.LoginInProgress = c.loginCmd != nil
	status.LoginPhase = c.loginPhase
	status.LoginURL = c.loginURL
	status.InstallInProgress = c.installing
	status.LoginError = c.loginError
	c.mu.Unlock()
	if err != nil {
		status.Detail = "Dev Tunnel CLI is not installed"
		return status, nil
	}
	status.Installed = true
	status.Path = path
	status.Source = source
	versionOut, versionErr := c.run(ctx, path, "--version")
	if versionErr != nil && strings.TrimSpace(versionOut) == "" {
		status.Installed = false
		status.Detail = clip(versionErr.Error(), 240)
		return status, nil
	}
	status.Version = ParseVersion(versionOut)
	c.mu.Lock()
	c.version = status.Version
	c.mu.Unlock()
	userOut, userErr := c.run(ctx, path, "user", "show", "-j")
	status.AccountState, status.AccountLabel, status.Detail = ParseAccount(userOut, userErr)
	return status, nil
}

// Preflight rejects start attempts that cannot host yet.
func (c *Client) Preflight(ctx context.Context) error {
	status, err := c.Inspect(ctx)
	if err != nil {
		return err
	}
	if !status.Installed {
		return fmt.Errorf("dev tunnel cli is not installed")
	}
	switch status.AccountState {
	case "signed_in":
		return nil
	case "expired":
		return fmt.Errorf("dev tunnel login expired")
	case "signed_out":
		return fmt.Errorf("dev tunnel is not signed in")
	default:
		return fmt.Errorf("dev tunnel account status is unavailable")
	}
}

// StartLogin launches the official browser login and returns without waiting for it.
// progress reports starting, browser, and verifying phases. done runs after the CLI exits.
func (c *Client) StartLogin(kind string, progress func(phase, url string), done func()) error {
	path, _, err := c.resolve()
	if err != nil {
		return fmt.Errorf("dev tunnel cli is not installed")
	}
	args := []string{"user", "login"}
	switch strings.TrimSpace(kind) {
	case "", "microsoft":
	case "github":
		args = append(args, "-g")
	default:
		return fmt.Errorf("unsupported login kind")
	}
	c.mu.Lock()
	if c.loginCmd != nil {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	cmd := exec.Command(path, args...)
	configureCommand(cmd)
	output := &loginOutput{}
	output.onLine = func(line string) {
		phase, url := ClassifyLoginLine(line)
		if phase == "" {
			return
		}
		c.mu.Lock()
		c.loginPhase = phase
		if url != "" {
			c.loginURL = url
		}
		currentURL := c.loginURL
		c.mu.Unlock()
		if progress != nil {
			progress(phase, currentURL)
		}
	}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		return err
	}

	c.mu.Lock()
	if c.loginCmd != nil {
		c.mu.Unlock()
		killProcessTree(cmd)
		_ = cmd.Wait()
		return nil
	}
	c.loginCmd = cmd
	c.loginError = ""
	c.loginURL = ""
	c.loginPhase = "browser"
	c.mu.Unlock()
	if progress != nil {
		progress("browser", "")
	}

	go func() {
		err := cmd.Wait()
		c.mu.Lock()
		cancelled := c.loginPhase == "cancelled"
		if c.loginCmd == cmd {
			c.loginCmd = nil
		}
		c.loginPhase = ""
		c.loginURL = ""
		if cancelled || err == nil {
			c.loginError = ""
		} else {
			c.loginError = clip(output.String()+" "+err.Error(), 240)
		}
		c.mu.Unlock()
		if done != nil {
			done()
		}
	}()
	return nil
}

// CancelLogin stops a browser login that is still waiting.
func (c *Client) CancelLogin() {
	c.mu.Lock()
	cmd := c.loginCmd
	if cmd != nil {
		c.loginPhase = "cancelled"
	}
	c.mu.Unlock()
	if cmd != nil {
		killProcessTree(cmd)
	}
}

// Logout asks the CLI to clear its own stored login. Tokens are not copied by Loris.
func (c *Client) Logout(ctx context.Context) error {
	path, _, err := c.resolve()
	if err != nil {
		return fmt.Errorf("dev tunnel cli is not installed")
	}
	_, err = c.run(ctx, path, "user", "logout")
	return err
}

// CreatePersistent creates one persistent tunnel and returns its id.
// An empty region keeps Microsoft's automatic cluster selection.
// An empty name asks Microsoft for a random id; a non-empty name must pass
// NormalizeTunnelName and is sent as the CLI positional tunnel id.
func (c *Client) CreatePersistent(ctx context.Context, public bool, expiration, region, name string) (string, error) {
	expiration, err := NormalizeExpiration(expiration)
	if err != nil {
		return "", err
	}
	region, err = NormalizeRegion(region)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(name) != "" {
		name, err = NormalizeTunnelName(name)
		if err != nil {
			return "", err
		}
	} else {
		name = ""
	}
	path, _, err := c.resolve()
	if err != nil {
		return "", fmt.Errorf("dev tunnel cli is not installed")
	}
	args := []string{"create"}
	if name != "" {
		args = append(args, name)
	}
	args = append(args, "--expiration", expiration, "-j")
	stdin := ""
	if region != "" {
		uri, uriErr := c.serviceURI(ctx, region)
		if uriErr != nil {
			return "", uriErr
		}
		// A regional --service-uri looks up a different keychain account and
		// otherwise sends the create anonymously. Reuse the existing login.
		token, tokenErr := c.loginToken(ctx)
		if tokenErr != nil {
			return "", tokenErr
		}
		args = append(args, "--service-uri", uri, "--access-token", "-")
		stdin = token + "\n"
	}
	if public {
		args = append(args, "--allow-anonymous")
	}
	out, err := c.runInput(ctx, path, stdin, args...)
	if err != nil {
		text := out + " " + err.Error()
		if tunnelNameConflict(text) {
			return "", fmt.Errorf("%w: create dev tunnel %s: %s", ErrTunnelNameConflict, name, clip(redactSecrets(text), 300))
		}
		return "", fmt.Errorf("create dev tunnel: %s", clip(redactSecrets(text), 300))
	}
	id := ParseTunnelID(out)
	if id == "" {
		return "", fmt.Errorf("create dev tunnel did not return a tunnel id")
	}
	if !validTunnelID(id) {
		return "", fmt.Errorf("create dev tunnel returned an unexpected id")
	}
	return c.confirmCreatedRegion(ctx, id, region)
}

// EnsurePort binds one local port. An existing port is treated as success.
func (c *Client) EnsurePort(ctx context.Context, tunnelID string, port int, protocol string) error {
	if !validTunnelID(tunnelID) {
		return fmt.Errorf("invalid dev tunnel id")
	}
	path, _, err := c.resolve()
	if err != nil {
		return fmt.Errorf("dev tunnel cli is not installed")
	}
	out, err := c.run(ctx, path, "port", "create", tunnelID, "-p", fmt.Sprintf("%d", port), "--protocol", protocol, "-j")
	if err == nil {
		return nil
	}
	if !portAlreadyConfigured(out + " " + err.Error()) {
		return fmt.Errorf("configure dev tunnel port: %s", clip(out+" "+err.Error(), 300))
	}
	detail := c.GetResource(ctx, tunnelID)
	if detail.State != ResourceReady {
		return fmt.Errorf("configure dev tunnel port: %s", clip(detail.Detail, 300))
	}
	if matchErr := MatchPort(detail.Resource, port, protocol); matchErr != nil {
		return fmt.Errorf("configure dev tunnel port: %w", matchErr)
	}
	return nil
}

// UpdateExpiration applies the saved lifetime to a tunnel that already exists.
func (c *Client) UpdateExpiration(ctx context.Context, tunnelID, expiration string) error {
	expiration, err := NormalizeExpiration(expiration)
	if err != nil {
		return err
	}
	if !validTunnelID(tunnelID) {
		return fmt.Errorf("invalid dev tunnel id")
	}
	path, _, err := c.resolve()
	if err != nil {
		return fmt.Errorf("dev tunnel cli is not installed")
	}
	out, err := c.run(ctx, path, "update", tunnelID, "--expiration", expiration, "-j")
	if err != nil {
		return fmt.Errorf("update dev tunnel expiration: %s", clip(out+" "+err.Error(), 300))
	}
	return nil
}

// DeleteRemote removes a tunnel that was created but could not be saved locally.
func (c *Client) DeleteRemote(ctx context.Context, tunnelID string) error {
	return c.deleteRemote(ctx, tunnelID, true)
}

// DeleteRemoteGuarded deletes a tunnel without -f, so an active connection can still block it.
func (c *Client) DeleteRemoteGuarded(ctx context.Context, tunnelID string) error {
	return c.deleteRemote(ctx, tunnelID, false)
}

func (c *Client) deleteRemote(ctx context.Context, tunnelID string, force bool) error {
	if !validTunnelID(tunnelID) {
		return fmt.Errorf("invalid dev tunnel id")
	}
	path, _, err := c.resolve()
	if err != nil {
		return err
	}
	args := []string{"delete", tunnelID}
	if force {
		args = append(args, "-f")
	}
	out, err := c.run(ctx, path, args...)
	if err != nil {
		return fmt.Errorf("%s", clip(strings.TrimSpace(out+" "+err.Error()), 300))
	}
	return nil
}

// DeletePort removes one published port. A missing port is success.
// -f only skips the CLI prompt; tunnel deletion uses DeleteRemoteGuarded instead.
func (c *Client) DeletePort(ctx context.Context, tunnelID string, port int) error {
	if !validTunnelID(tunnelID) {
		return fmt.Errorf("invalid dev tunnel id")
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid dev tunnel port")
	}
	path, _, err := c.resolve()
	if err != nil {
		return fmt.Errorf("dev tunnel cli is not installed")
	}
	out, err := c.run(ctx, path, "port", "delete", tunnelID, "-p", strconv.Itoa(port), "-f")
	if err == nil {
		return nil
	}
	text := out + " " + err.Error()
	if state, _ := classifyCLIFailure(text, err); state == ResourceNotFound || portAlreadyGone(text) {
		return nil
	}
	return fmt.Errorf("delete dev tunnel port: %s", clip(strings.TrimSpace(text), 300))
}

func portAlreadyGone(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "not found") || strings.Contains(lower, "does not exist") || strings.Contains(lower, "no such port")
}

func (c *Client) resolve() (string, string, error) {
	if strings.TrimSpace(c.Binary) != "" {
		info, err := os.Stat(c.Binary)
		if err != nil || info.IsDir() {
			return "", "", fmt.Errorf("dev tunnel cli is not installed")
		}
		return c.Binary, "test", nil
	}
	managed := managedBinaryPath()
	if selected, chosen, err := climaint.SelectedPath(managed); err != nil {
		return "", "", err
	} else if chosen {
		source := "system"
		if selected == managed {
			source = "managed"
		}
		info, err := os.Stat(selected)
		if err != nil {
			return selected, source, err
		}
		if info.IsDir() {
			return selected, source, fmt.Errorf("CLI selection is not an executable file")
		}
		return selected, source, nil
	}
	if info, err := os.Stat(managed); err == nil && !info.IsDir() {
		return managed, "managed", nil
	}
	found, err := exec.LookPath(binaryName())
	if err != nil {
		return "", "", err
	}
	return found, "system", nil
}

func (c *Client) run(ctx context.Context, path string, args ...string) (string, error) {
	return c.runInput(ctx, path, "", args...)
}

// loginToken reads the current CLI login. Regional creates need it on stdin
// because the CLI will not reuse the global login for another service URI.
func (c *Client) loginToken(ctx context.Context) (string, error) {
	path, _, err := c.resolve()
	if err != nil {
		return "", fmt.Errorf("dev tunnel cli is not installed")
	}
	out, err := c.run(ctx, path, "user", "show", "-v")
	token := parseUserToken(out)
	if token == "" {
		if err != nil {
			return "", fmt.Errorf("dev tunnel login required")
		}
		return "", fmt.Errorf("dev tunnel login required")
	}
	return token, nil
}

func parseUserToken(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		const prefix = "Token:"
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		token := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		if token != "" && !strings.ContainsAny(token, " \t") {
			return token
		}
	}
	return ""
}

var secretPattern = regexp.MustCompile(`(?:ghu|gho|ghs|ghr)_[A-Za-z0-9]+|github_pat_[A-Za-z0-9_]+|eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)

func redactSecrets(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, "Token:"); idx >= 0 {
			lines[i] = strings.TrimRight(line[:idx+len("Token:")], " ") + " [redacted]"
		}
	}
	return secretPattern.ReplaceAllString(strings.Join(lines, "\n"), "[redacted]")
}

func (c *Client) runInput(ctx context.Context, path, stdin string, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	configureCommand(cmd)
	cmd.Cancel = func() error { killProcessTree(cmd); return nil }
	cmd.WaitDelay = 2 * time.Second
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

func validTunnelID(id string) bool {
	if id == "" || len(id) > 80 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
		default:
			return false
		}
	}
	return true
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "devtunnel.exe"
	}
	return "devtunnel"
}

func managedBinaryPath() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".", ".loris-tunnel", "bin", binaryName())
	}
	return filepath.Join(home, ".loris-tunnel", "bin", binaryName())
}

// limitedBuffer keeps the start of CLI output without blocking the process.
type limitedBuffer struct {
	buf bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.max > 0 && b.buf.Len() >= b.max {
		return len(p), nil
	}
	chunk := p
	if b.max > 0 {
		remain := b.max - b.buf.Len()
		if len(chunk) > remain {
			chunk = chunk[:remain]
		}
	}
	_, _ = b.buf.Write(chunk)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	return b.buf.String()
}

// loginOutput splits CLI output into lines and keeps a short copy for errors.
type loginOutput struct {
	buf    limitedBuffer
	rest   string
	mu     sync.Mutex
	onLine func(string)
}

func (o *loginOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	if o.buf.max == 0 {
		o.buf.max = 4000
	}
	_, _ = o.buf.Write(p)
	o.rest += string(p)
	var lines []string
	for {
		index := strings.IndexByte(o.rest, '\n')
		if index < 0 {
			break
		}
		line := strings.TrimSpace(strings.TrimRight(o.rest[:index], "\r"))
		o.rest = o.rest[index+1:]
		if line != "" {
			lines = append(lines, line)
		}
	}
	o.mu.Unlock()
	for _, line := range lines {
		if o.onLine != nil {
			o.onLine(line)
		}
	}
	return len(p), nil
}

func (o *loginOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

var _ io.Writer = (*loginOutput)(nil)
