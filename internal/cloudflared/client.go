package cloudflared

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"loris-tunnel/internal/climaint"
	"loris-tunnel/internal/conf"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const commandTimeout = 45 * time.Second

var (
	userHomeDir = os.UserHomeDir
	errNoJSON   = errors.New("cloudflared tunnel list did not return JSON")
)

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
	LoginError        string `json:"loginError"`
	LoginResult       string `json:"loginResult"`
	InstallInProgress bool   `json:"installInProgress"`
}

// Client runs the official cloudflared CLI. An empty Binary is resolved at call time.
// ConfigHome overrides ~/.cloudflared in tests.
type Client struct {
	Binary     string
	ConfigHome string
	certFile   string
	authMu     sync.Mutex
	ZoneAPI    string

	mu                sync.Mutex
	loginVerifyCancel context.CancelFunc
	loginCmd          *exec.Cmd
	loginActive       bool
	loginPhase        string
	loginURL          string
	loginResult       string
	loginError        string
	installing        bool
	installCancel     context.CancelFunc
}

func NewClient(binary string) *Client {
	return &Client{Binary: strings.TrimSpace(binary)}
}

// Inspect reports whether cloudflared can run and whether cert.pem can list tunnels.
func (c *Client) Inspect(ctx context.Context) (Status, error) {
	c.recoverLoginBackup()
	status := Status{AccountState: "unknown"}
	path, source, err := c.resolve()
	c.mu.Lock()
	status.LoginInProgress = c.loginActive
	status.LoginPhase = c.loginPhase
	status.LoginURL = c.loginURL
	status.LoginError = c.loginError
	status.LoginResult = c.loginResult
	status.InstallInProgress = c.installing
	c.mu.Unlock()
	if err != nil {
		status.Detail = ErrNotInstalled.Error()
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
	if !c.accountCredentialExists() {
		status.AccountState = "signed_out"
		status.Detail = "Not authenticated"
		return status, nil
	}
	listOut, listErr := c.runAccount(ctx, path, "tunnel", "list", "-o", "json")
	if listErr == nil {
		if _, parseErr := ParseTunnelList(listOut); parseErr != nil {
			status.AccountState = "unknown"
			status.Detail = clip(parseErr.Error(), 240)
			return status, nil
		}
		status.AccountState = "signed_in"
		status.AccountLabel = "Cloudflare"
		status.Detail = "Cloudflare account connected"
		return status, nil
	}
	state, detail := classifyCLIFailure(listOut, listErr)
	status.Detail = detail
	switch state {
	case ResourceAuth:
		status.AccountState = "expired"
	case ResourceNetwork:
		status.AccountState = "unknown"
	default:
		status.AccountState = "unknown"
	}
	return status, nil
}

// Preflight rejects start attempts that cannot reach Cloudflare yet.
func (c *Client) Preflight(ctx context.Context) error {
	status, err := c.Inspect(ctx)
	if err != nil {
		return err
	}
	if !status.Installed {
		return ErrNotInstalled
	}
	switch status.AccountState {
	case "signed_in":
		return nil
	case "signed_out", "expired":
		return ErrAuthRequired
	default:
		return ErrAccountUnavailable
	}
}

// StartLogin launches `cloudflared tunnel login` and returns without waiting.
// An existing cert.pem is moved aside first, because cloudflared exits immediately
// when that file is present and never opens the browser. Cancel or failure puts it back.
func (c *Client) StartLogin(progress func(phase, url string), done func()) error {
	return c.StartZoneAuthorization("", progress, done)
}

func (c *Client) StartZoneAuthorization(zoneID string, progress func(phase, url string), done func()) error {
	c.recoverLoginBackup()
	path, _, err := c.resolve()
	if err != nil {
		return ErrNotInstalled
	}
	c.mu.Lock()
	if c.loginActive {
		c.mu.Unlock()
		return nil
	}
	c.loginActive = true
	c.loginError = ""
	c.loginResult = ""
	c.loginURL = ""
	c.loginPhase = "starting"
	c.mu.Unlock()
	failStart := func(startErr error) error {
		c.mu.Lock()
		c.loginActive = false
		c.loginPhase = ""
		c.loginError = loginFailureCode("", startErr)
		c.mu.Unlock()
		return startErr
	}

	c.authMu.Lock()
	_, storeErr := c.loadAuthorizationsLocked()
	if os.IsNotExist(storeErr) {
		storeErr = c.saveAuthorizationsLocked(authorizationStore{Entries: []authorizationEntry{}})
	}
	c.authMu.Unlock()
	if storeErr != nil && !errors.Is(storeErr, ErrAuthRequired) {
		return failStart(storeErr)
	}
	backup, parkErr := c.parkCert()
	if parkErr != nil {
		return failStart(parkErr)
	}
	cmd := exec.Command(path, "tunnel", "login")
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
		c.restoreCert(backup, false)
		return failStart(err)
	}
	c.mu.Lock()
	if !c.loginActive || c.loginPhase == "cancelled" || c.loginCmd != nil {
		c.mu.Unlock()
		killProcessTree(cmd)
		_ = cmd.Wait()
		c.restoreCert(backup, false)
		c.mu.Lock()
		c.loginCmd = nil
		c.loginActive = false
		c.loginPhase = ""
		c.loginURL = ""
		c.mu.Unlock()
		if done != nil {
			done()
		}
		return nil
	}
	c.loginCmd = cmd
	c.mu.Unlock()
	if progress != nil {
		progress("starting", "")
	}
	go func() {
		err := cmd.Wait()
		text := output.String()
		cancelled := false
		c.mu.Lock()
		cancelled = c.loginPhase == "cancelled"
		if c.loginCmd == cmd {
			c.loginCmd = nil
		}
		c.mu.Unlock()
		keptNew := err == nil && !cancelled && !existingCertRefusal(text) && c.certExists()

		var authorizationErr error
		authorizationResult := ""
		if keptNew {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			c.mu.Lock()
			c.loginVerifyCancel = cancel
			if c.loginPhase == "cancelled" {
				cancel()
			}
			c.mu.Unlock()
			authorizationResult, authorizationErr = c.acceptAuthorization(ctx, zoneID)
			cancel()
		}
		c.mu.Lock()
		cancelled = cancelled || c.loginPhase == "cancelled"
		c.mu.Unlock()
		keptNew = keptNew && authorizationErr == nil && !cancelled
		// Restore the default certificate even after a successful additional authorization.
		restored := c.restoreCert(backup, keptNew && backup == "")
		if !keptNew && backup == "" {
			if p, e := c.certPath(); e == nil {
				_ = os.Remove(p)
			}
		}
		c.mu.Lock()
		c.loginActive = false
		c.loginVerifyCancel = nil
		c.loginPhase = ""
		c.loginURL = ""
		switch {
		case cancelled:
			c.loginError = ""
		case backup != "" && !restored:
			c.loginError = "cloudflare_authorization_restore_failed"
		case authorizationErr != nil:
			c.loginError = authorizationErr.Error()
		case keptNew:
			c.loginError = ""
			c.loginResult = authorizationResult
		case existingCertRefusal(text):
			c.loginError = "cloudflare_login_existing_cert"
		case restored:
			c.loginError = "cloudflare_login_incomplete_kept"
		default:
			c.loginError = loginFailureCode(text, err)
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
	verifyCancel := c.loginVerifyCancel
	if c.loginActive {
		c.loginPhase = "cancelled"
	}
	c.mu.Unlock()
	if verifyCancel != nil {
		verifyCancel()
	}
	if cmd != nil {
		killProcessTree(cmd)
	}
}

func (c *Client) resolve() (string, string, error) {
	if strings.TrimSpace(c.Binary) != "" {
		info, err := os.Stat(c.Binary)
		if err != nil || info.IsDir() {
			return "", "", ErrNotInstalled
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
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	configureCommand(cmd)
	cmd.Cancel = func() error { killProcessTree(cmd); return nil }
	cmd.WaitDelay = 2 * time.Second
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

func (c *Client) recoverLoginBackup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loginActive {
		return
	}
	path, err := c.certPath()
	if err != nil {
		return
	}
	backup := path + ".loris-login-bak"
	if _, err := os.Stat(backup); err == nil {
		c.restoreCert(backup, false)
	}
}

func (c *Client) accountCredentialExists() bool {
	for _, path := range c.accountCertPaths() {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return true
		}
	}
	return false
}

func (c *Client) runAccount(ctx context.Context, binary string, args ...string) (string, error) {
	paths := c.accountCertPaths()
	if len(paths) == 0 {
		return "", ErrAuthRequired
	}
	var out string
	err := ErrAuthRequired
	for _, path := range paths {
		command := args
		original, pathErr := c.certPath()
		if pathErr != nil || path != original || c.certFile != "" {
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				out, err = "", ErrAuthRequired
				continue
			}
			if _, decodeErr := decodeOriginCert(raw); decodeErr != nil {
				out, err = "", ErrAuthRequired
				continue
			}
			// Put the option after the subcommand to preserve the official CLI's flag scope.
			dir, dirErr := c.authorizationDir()
			if dirErr != nil {
				return "", ErrAuthRequired
			}
			config := filepath.Join(dir, "cli.yml")
			if writeErr := conf.WritePrivateAtomic(config, []byte("{}\n")); writeErr != nil {
				return "", fmt.Errorf("cloudflare_authorization_save_failed")
			}
			command = append([]string{args[0], "--config", config, "--origincert", path}, args[1:]...)
		}
		out, err = c.run(ctx, binary, command...)
		if err == nil && !accountCLIAuthFailure(out) {
			return out, nil
		}
		if err == nil {
			err = fmtError("cloudflared reported a credential error")
		}
		state, _ := classifyCLIFailure(out, err)
		if state != ResourceAuth && state != ResourcePermission {
			return out, err
		}
	}
	return out, err
}

func (c *Client) configDir() (string, error) {
	if strings.TrimSpace(c.ConfigHome) != "" {
		return c.ConfigHome, nil
	}
	home, err := userHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("home directory is unavailable")
	}
	return filepath.Join(home, ".cloudflared"), nil
}

func (c *Client) certExists() bool {
	path, err := c.certPath()
	if c.certFile != "" {
		path = c.certFile
	}
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

func (c *Client) certPath() (string, error) {
	dir, err := c.configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cert.pem"), nil
}

// parkCert moves an existing account certificate out of cloudflared's way.
// cloudflared tunnel login exits 0 without opening a browser when cert.pem exists.
func (c *Client) parkCert() (string, error) {
	path, err := c.certPath()
	if err != nil || !c.certExists() {
		return "", err
	}
	backup := path + ".loris-login-bak"
	if _, err := os.Stat(backup); err == nil {
		return "", fmt.Errorf("cloudflare_login_backup_exists")
	}
	if err := os.Rename(path, backup); err != nil {
		return "", err
	}
	return backup, nil
}

// restoreCert puts a parked certificate back unless login wrote a new one.
func (c *Client) restoreCert(backup string, keepNew bool) bool {
	if backup == "" {
		return false
	}
	path, err := c.certPath()
	if err != nil {
		return false
	}
	if keepNew && c.certExists() {
		_ = os.Remove(backup)
		return false
	}

	raw, err := os.ReadFile(backup)
	if err != nil {
		return false
	}
	if err := conf.WritePrivateAtomic(path, raw); err != nil {
		return false
	}
	return os.Remove(backup) == nil
}

// CredentialPath is ~/.cloudflared/<UUID>.json.
func (c *Client) CredentialPath(id string) (string, error) {
	if !ValidTunnelID(id) {
		return "", fmt.Errorf("invalid cloudflare tunnel id")
	}
	dir, err := c.configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, strings.ToLower(id)+".json"), nil
}

// CredentialExists reports whether this device has the origin credential.
func (c *Client) CredentialExists(id string) bool {
	path, err := c.CredentialPath(id)
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "cloudflared.exe"
	}
	return "cloudflared"
}

func managedBinaryPath() string {
	home, err := userHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".", ".loris-tunnel", "bin", binaryName())
	}
	return filepath.Join(home, ".loris-tunnel", "bin", binaryName())
}

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

func (b *limitedBuffer) String() string { return b.buf.String() }
