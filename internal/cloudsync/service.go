package cloudsync

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"loris-tunnel/internal/conf"
)

type Options struct {
	Enabled           bool              `json:"enabled"`
	ServerURL         string            `json:"server_url"`
	SpaceID           string            `json:"space_id"`
	DeviceName        string            `json:"device_name"`
	Automatic         bool              `json:"automatic"`
	CredentialAccount string            `json:"credential_account,omitempty"`
	Baseline          conf.SyncBaseline `json:"baseline"`
	Protocol          int               `json:"protocol"`
}

const SettingsFileName = "cloud-sync.json"

const DevSettingsFileName = "sync.dev.json"
const ProdSettingsFileName = "sync.prod.json"

func SettingsFileForEnvironment(environment string) string {
	if environment == "dev" {
		return DevSettingsFileName
	}
	return ProdSettingsFileName
}

func SettingsFiles() []string {
	return []string{SettingsFileName, DevSettingsFileName, ProdSettingsFileName}
}

// LockForRelocation must be acquired before the app's configuration lock.
// It prevents connection settings from changing while their file is copied.
func (s *Service) LockForRelocation() func() {
	s.op.Lock()
	return s.op.Unlock
}

func credentialAccount(path, deviceID string) string {
	hash := sha256.Sum256([]byte(filepath.Clean(path) + ":" + deviceID))
	return hex.EncodeToString(hash[:])
}

// MigrateLegacy adopts a colocated connection by server, or an old shared connection
// only when this TOML owns its baseline.
// Keep the original file so a different configuration directory cannot lose its connection.
func MigrateLegacy(path, legacyPath, deviceID, server string, baseline conf.SyncBaseline) error {
	var existing Options
	if data, e := os.ReadFile(path); e == nil {
		if e = json.Unmarshal(data, &existing); e != nil {
			return nil
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(data, &fields)
		if existing.ServerURL != "" || fields["enabled"] != nil || existing.CredentialAccount != "" {
			return nil
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if baseline.SpaceID == "" && filepath.Dir(path) != filepath.Dir(legacyPath) {
		return nil
	}
	data, err := os.ReadFile(legacyPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var options Options
	if err = json.Unmarshal(data, &options); err != nil {
		return fmt.Errorf("invalid legacy cloud sync settings")
	}
	if options.ServerURL != server || (baseline.SpaceID != "" &&
		(options.SpaceID != baseline.SpaceID || options.ServerURL != baseline.ServerURL)) {
		return nil
	}
	if options.CredentialAccount == "" {
		options.CredentialAccount = credentialAccount(legacyPath, deviceID)
	}
	options.Baseline = existing.Baseline
	s := &Service{path: path}
	return s.persist(options)
}

type Remote struct {
	SpaceID       string     `json:"space_id"`
	Revision      int64      `json:"revision"`
	UpdateTime    *time.Time `json:"update_time"`
	DeviceName    string     `json:"device_name"`
	SchemaVersion int        `json:"schema_version"`
	Config        *Config    `json:"config,omitempty"`
}
type Status struct {
	Enabled            bool       `json:"enabled"`
	Connected          bool       `json:"connected"`
	Automatic          bool       `json:"automatic"`
	ServerURL          string     `json:"server_url"`
	DeviceName         string     `json:"device_name"`
	State              string     `json:"state"`
	Error              string     `json:"error"`
	Revision           int64      `json:"revision"`
	LastSyncedRevision int64      `json:"last_synced_revision"`
	LastSyncedAt       string     `json:"last_synced_at"`
	UpdateTime         *time.Time `json:"update_time"`
	UpdatedBy          string     `json:"updated_by"`
	LocalFingerprint   string     `json:"local_fingerprint"`
}
type Hooks struct {
	Snapshot func() ([]byte, error)
	Apply    func([]byte, string) error
	Changed  func(Status)
}
type Service struct {
	op                      sync.Mutex
	mu                      sync.RWMutex
	status                  Status
	lastFocus               time.Time
	options                 Options
	path, account, deviceID string
	vault                   Vault
	hooks                   Hooks
	client                  *http.Client
	ctx                     context.Context
	cancel                  context.CancelFunc
	wg                      sync.WaitGroup
	changes                 chan struct{}
	wake                    chan struct{}
	retryAt                 time.Time
	backoff                 time.Duration
	pendingToken            string
	suspended               bool
}

func New(path, deviceID, deviceName string, vault Vault, hooks Hooks) (*Service, error) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{
		path:     path,
		account:  credentialAccount(path, deviceID),
		deviceID: deviceID,
		vault:    vault,
		hooks:    hooks,
		ctx:      ctx,
		cancel:   cancel,
		changes:  make(chan struct{}, 1),
		wake:     make(chan struct{}, 1),
		client: &http.Client{
			Timeout:       15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	s.options.DeviceName = deviceName
	if b, e := os.ReadFile(path); e == nil {
		if e = json.Unmarshal(b, &s.options); e != nil {
			cancel()
			return nil, fmt.Errorf("invalid local cloud sync settings")
		}
	} else if !os.IsNotExist(e) {
		cancel()
		return nil, e
	}
	if s.options.ServerURL != "" {
		normalized, e := normalizeURL(s.options.ServerURL)
		if e != nil {
			cancel()
			return nil, e
		}
		s.options.ServerURL = normalized
	}
	if s.options.CredentialAccount != "" {
		s.account = s.options.CredentialAccount
	} else if s.options.ServerURL != "" {
		// Save the existing vault reference before any configuration-directory move.
		s.options.CredentialAccount = s.account
		if e := s.persist(s.options); e != nil {
			cancel()
			return nil, e
		}
	}
	s.status = Status{
		Enabled:    s.options.Enabled,
		Connected:  s.options.ServerURL != "",
		Automatic:  s.options.Automatic,
		ServerURL:  s.options.ServerURL,
		DeviceName: s.options.DeviceName,
	}
	s.restoreStatusLocked()
	return s, nil
}
func normalizeURL(raw string) (string, error) {
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil ||
		u.Host == "" ||
		u.User != nil ||
		u.RawQuery != "" ||
		u.Fragment != "" ||
		(u.Path != "" &&
			u.Path != "/") {
		return "", fmt.Errorf("enter a server origin such as https://sync.example.com")
	}
	host := u.Hostname()
	if u.Scheme != "https" && !(u.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1")) {
		return "", fmt.Errorf("cloud sync requires HTTPS (HTTP is allowed only for loopback development)")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// ConfigureEnvironment runs before Start; never reuse a connection across servers.
func (s *Service) ConfigureEnvironment(base, name string) error {
	base, err := normalizeURL(base)
	if err != nil {
		return err
	}
	st := s.Status()
	if s.options.ServerURL != "" && s.options.ServerURL != base {
		s.options = Options{Enabled: s.options.Enabled}
		st = Status{State: "disconnected", Error: "Sync server changed; reconnect for this environment"}
	}
	s.options.DeviceName = name
	st.ServerURL, st.DeviceName = base, name
	s.publish(st)
	return nil
}

func (s *Service) GenerateToken(base, name string) (string, error) {
	s.op.Lock()
	defer s.op.Unlock()
	if err := s.requireEnabledLocked(); err != nil {
		return "", err
	}
	base, err := normalizeURL(base)
	if err != nil {
		return "", err
	}
	if s.pendingToken == "" {
		var value [32]byte
		if _, err = rand.Read(value[:]); err != nil {
			return "", err
		}
		s.pendingToken = base64.RawURLEncoding.EncodeToString(value[:])
	}
	token := s.pendingToken
	_, err = s.request(base, "", "POST", "register", struct {
		Token      string `json:"token"`
		DeviceName string `json:"device_name"`
	}{token, name})
	if err != nil {
		return "", err
	}
	s.pendingToken = ""
	return token, nil
}

func (s *Service) Status() Status { s.mu.RLock(); defer s.mu.RUnlock(); return s.status }
func (s *Service) publish(st Status) {
	st.Enabled = s.options.Enabled
	if !st.Enabled {
		st.State = "disabled"
		st.Error = ""
	}
	s.mu.Lock()
	s.status = st
	s.mu.Unlock()
	if s.hooks.Changed != nil {
		s.hooks.Changed(st)
	}
}
func (s *Service) persist(o Options) error {
	if s.ctx != nil && s.Stopped() {
		return fmt.Errorf("cloud sync stopped; restart the app")
	}
	b, e := json.MarshalIndent(o, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(s.path), 0700); e != nil {
		return e
	}
	return conf.WritePrivateAtomic(s.path, b)
}

type AppliedError struct{ Err error }

func (e *AppliedError) Error() string { return e.Err.Error() }
func (e *AppliedError) Unwrap() error { return e.Err }

type HTTPError struct {
	Code         int
	RetryAfter   time.Duration
	Registration bool
}

func (e *HTTPError) Error() string {
	switch e.Code {
	case 403:
		return "Cloud sync requires an active Pro license"
	case 401:
		return "Token is invalid, revoked, or its cloud backup expired; reconnect with a new token if needed"
	case 409:
		if e.Registration {
			return "registration conflicted; retry"
		}
		return "cloud revision changed; check again before confirming"
	case 429:
		return "server rate limit exceeded; retry later"
	case 413:
		return "backup request exceeds 2 MiB"
	case 426:
		return "old object backup protocol detected; upgrade server and reset old test snapshots"
	case 422:
		return "server rejected the configuration"
	default:
		return fmt.Sprintf("cloud server returned HTTP %d", e.Code)
	}
}
func (s *Service) request(base, token, method, path string, payload any) (Remote, error) {
	var result Remote
	var body io.Reader
	if payload != nil {
		b, e := json.Marshal(payload)
		if e != nil {
			return result, e
		}
		if len(b) > MaxWireSize {
			return result, fmt.Errorf("backup request exceeds 2 MiB")
		}
		body = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(s.ctx, method, base+"/api/v1/sync/"+path, body)
	if e != nil {
		return result, fmt.Errorf("invalid sync request")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("X-Device-ID", s.deviceID)
	req.Header.Set("Content-Type", "application/json")
	resp, e := s.client.Do(req)
	if e != nil {
		return result, fmt.Errorf("cloud server could not be reached")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		retry := time.Duration(0)
		if n, e := strconv.Atoi(resp.Header.Get("Retry-After")); e == nil && n > 0 {
			if n > 86400 {
				n = 86400
			}
			retry = time.Duration(n) * time.Second
		} else if date, e := http.ParseTime(resp.Header.Get("Retry-After")); e == nil {
			retry = time.Until(date)
		}
		return result, &HTTPError{Code: resp.StatusCode, RetryAfter: retry, Registration: path == "register"}
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, MaxWireSize+1))
	if e != nil || len(b) > MaxWireSize {
		return result, fmt.Errorf("invalid or oversized cloud response")
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&result); e != nil {
		return result, fmt.Errorf("invalid cloud response")
	}
	if result.SchemaVersion != SchemaVersion || result.SpaceID == "" || result.Revision < 0 {
		return result, fmt.Errorf("unsupported cloud backup protocol; upgrade client and server")
	}
	return result, nil
}
func (s *Service) Test(base, token string) (Remote, error) {
	s.op.Lock()
	defer s.op.Unlock()
	if err := s.requireEnabledLocked(); err != nil {
		return Remote{}, err
	}
	base, e := normalizeURL(base)
	if e != nil {
		return Remote{}, e
	}
	if !tokenPattern.MatchString(token) {
		return Remote{}, fmt.Errorf("invalid Access Token format")
	}
	return s.request(base, token, "GET", "status", nil)
}
func (s *Service) Connect(base, token, name string) (Status, error) {
	s.op.Lock()
	defer s.op.Unlock()
	if err := s.requireEnabledLocked(); err != nil {
		return s.Status(), err
	}
	base, e := normalizeURL(base)
	if e != nil {
		return s.Status(), e
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 200 {
		return s.Status(), fmt.Errorf("device name must contain 1–200 characters")
	}
	if !tokenPattern.MatchString(token) {
		return s.Status(), fmt.Errorf("invalid Access Token format")
	}
	remote, e := s.request(base, token, "GET", "status", nil)
	if e != nil {
		return s.Status(), e
	}
	oldToken, _ := s.vault.Get(s.account)
	if e = s.vault.Set(s.account, token); e != nil {
		return s.Status(), e
	}
	o := Options{Enabled: s.options.Enabled, ServerURL: base, SpaceID: remote.SpaceID, DeviceName: name, CredentialAccount: s.account}
	if s.options.SpaceID == o.SpaceID && s.options.ServerURL == o.ServerURL {
		o = s.options
		o.DeviceName = name
	}
	if e = s.persist(o); e != nil {
		if oldToken != "" {
			_ = s.vault.Set(s.account, oldToken)
		} else {
			_ = s.vault.Delete(s.account)
		}
		return s.Status(), e
	}
	s.options = o
	s.retryAt = time.Time{}
	s.backoff = 0
	s.signalWake()
	s.publish(Status{Connected: true, Automatic: o.Automatic, ServerURL: base, DeviceName: name, State: "idle", Revision: remote.Revision})
	return s.syncLocked()
}
func (s *Service) Disconnect() error {
	s.op.Lock()
	defer s.op.Unlock()
	if err := s.requireEnabledLocked(); err != nil {
		return err
	}
	old := s.options
	if e := s.persist(Options{Enabled: old.Enabled, DeviceName: old.DeviceName, CredentialAccount: s.account}); e != nil {
		return e
	}
	if e := s.vault.Delete(s.account); e != nil {
		_ = s.persist(old)
		return e
	}
	s.options = Options{Enabled: old.Enabled, DeviceName: old.DeviceName, CredentialAccount: s.account}
	s.retryAt = time.Time{}
	s.publish(Status{DeviceName: old.DeviceName, State: "disconnected"})
	s.signalWake()
	return nil
}
func (s *Service) SetPreferences(automatic bool, name string) error {
	s.op.Lock()
	defer s.op.Unlock()
	if err := s.requireEnabledLocked(); err != nil {
		return err
	}
	if !s.Status().Connected {
		return fmt.Errorf("connect to cloud first")
	}
	if automatic && s.suspended {
		return fmt.Errorf("retry manual backup to save the baseline before enabling automatic backup")
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 200 {
		return fmt.Errorf("invalid device name")
	}
	o := s.options
	o.Automatic = automatic
	o.DeviceName = name
	if e := s.persist(o); e != nil {
		return e
	}
	s.options = o
	st := s.Status()
	st.Automatic = automatic
	st.DeviceName = name
	s.publish(st)
	if automatic {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
	return nil
}
func (s *Service) Token() (string, error) {
	s.op.Lock()
	defer s.op.Unlock()
	if err := s.requireEnabledLocked(); err != nil {
		return "", err
	}
	if !s.Status().Connected {
		return "", fmt.Errorf("connect to cloud first")
	}
	return s.vault.Get(s.account)
}
func baseline(o Options, r Remote, hash string) conf.SyncBaseline {
	return conf.SyncBaseline{
		SpaceID:     r.SpaceID,
		ServerURL:   o.ServerURL,
		Revision:    r.Revision,
		Fingerprint: hash,
		SyncedAt:    time.Now().UTC().Format(time.RFC3339),
	}
}
func (s *Service) fail(e error) (Status, error) {
	st := s.Status()
	st.State = "error"
	st.Error = e.Error()
	s.publish(st)
	if s.backoff == 0 {
		s.backoff = 5 * time.Second
	} else {
		s.backoff *= 2
	}
	if s.backoff > 5*time.Minute {
		s.backoff = 5 * time.Minute
	}
	delay := s.backoff
	var he *HTTPError
	if errors.As(e, &he) && he.RetryAfter > delay {
		delay = he.RetryAfter
	}
	s.retryAt = time.Now().Add(delay)
	return st, e
}
func (s *Service) Sync() (Status, error) { s.op.Lock(); defer s.op.Unlock(); return s.syncLocked() }
func (s *Service) syncLocked() (Status, error) {
	if err := s.requireEnabledLocked(); err != nil {
		return s.Status(), err
	}
	st := s.Status()
	if !st.Connected {
		return st, nil
	}
	if time.Now().Before(s.retryAt) {
		return st, fmt.Errorf("waiting before retrying cloud sync")
	}
	st.State = "syncing"
	st.Error = ""
	s.publish(st)
	token, e := s.vault.Get(s.account)
	if e != nil {
		return s.fail(e)
	}
	remote, e := s.request(s.options.ServerURL, token, "GET", "status", nil)
	if e != nil {
		return s.fail(e)
	}
	if remote.SpaceID != s.options.SpaceID {
		return s.fail(fmt.Errorf("configuration space changed; reconnect"))
	}
	local, e := s.hooks.Snapshot()
	if e != nil {
		return s.fail(e)
	}
	hash := Fingerprint(local)
	b := s.options.Baseline
	known := b.SpaceID == remote.SpaceID && b.ServerURL == s.options.ServerURL && b.Fingerprint != "" && s.options.Protocol == SchemaVersion
	st = s.Status()
	st.Revision = remote.Revision
	st.UpdateTime = remote.UpdateTime
	st.UpdatedBy = remote.DeviceName
	st.LocalFingerprint = hash
	st.LastSyncedRevision = 0
	st.LastSyncedAt = ""
	if known {
		st.LastSyncedRevision = b.Revision
		st.LastSyncedAt = b.SyncedAt
	}
	s.publish(st)
	if known && remote.Revision < b.Revision {
		return s.fail(fmt.Errorf("cloud revision moved backwards; disconnect and reconnect before replacing data"))
	}
	if !known || remote.Revision != b.Revision {
		if remote.Revision == 0 {
			if known && b.Revision != 0 {
				return s.fail(fmt.Errorf("cloud revision reset; reconnect before replacing data"))
			}
			return s.push(token, remote, local)
		}
		full, e := s.request(s.options.ServerURL, token, "GET", "config", nil)
		if e != nil {
			return s.fail(e)
		}
		if full.SpaceID != remote.SpaceID || full.Revision != remote.Revision {
			return s.fail(&HTTPError{Code: 409})
		}
		if full.Config == nil {
			return s.fail(fmt.Errorf("cloud configuration is missing"))
		}
		data, e := s.open(full)
		if e != nil {
			return s.fail(e)
		}
		if Fingerprint(data) == hash {
			return s.commit(remote, hash)
		}
		st = s.Status()
		st.State = "update_available"
		if (known && hash != b.Fingerprint) || !known {
			st.State = "conflict"
		}
		s.backoff = 0
		s.retryAt = time.Time{}
		s.publish(st)
		return st, nil
	}
	if hash != b.Fingerprint {
		return s.push(token, remote, local)
	}
	return s.commit(remote, hash)
}
func (s *Service) commit(r Remote, hash string) (Status, error) {
	b := baseline(s.options, r, hash)
	if old := s.options.Baseline; old.Revision == r.Revision && old.Fingerprint == hash && old.SpaceID == r.SpaceID {
		b = old
	}
	o := s.options
	o.Baseline = b
	o.Protocol = SchemaVersion
	if e := s.persist(o); e != nil {
		s.suspended = true
		st := s.Status()
		st.Automatic = false
		s.publish(st)
		return s.fail(fmt.Errorf("file operation completed, but baseline save failed; automatic backup stopped: %w", e))
	}
	s.options = o
	s.suspended = false
	st := s.Status()
	st.State = "synced"
	st.Error = ""
	st.Revision = r.Revision
	st.UpdateTime = r.UpdateTime
	st.UpdatedBy = r.DeviceName
	st.LastSyncedRevision = b.Revision
	st.LastSyncedAt = b.SyncedAt
	st.LocalFingerprint = hash
	latest, e := s.hooks.Snapshot()
	if e != nil {
		return s.fail(e)
	}
	if current := Fingerprint(latest); current != hash {
		st.State = "pending"
		st.LocalFingerprint = current
	}
	s.backoff = 0
	s.retryAt = time.Time{}
	s.publish(st)
	return st, nil
}
func (s *Service) push(token string, r Remote, data []byte) (Status, error) {
	if r.Revision == int64(9223372036854775807) {
		return s.fail(fmt.Errorf("revision exhausted"))
	}
	sealed, e := Pack(data)
	if e != nil {
		return s.fail(e)
	}
	remote, e := s.request(s.options.ServerURL, token, "PUT", "config", struct {
		BaseRevision int64   `json:"base_revision"`
		DeviceName   string  `json:"device_name"`
		Config       *Config `json:"config"`
	}{r.Revision, s.options.DeviceName, sealed})
	if e != nil {
		return s.fail(e)
	}
	if remote.SpaceID != r.SpaceID || remote.Revision != r.Revision+1 {
		return s.fail(fmt.Errorf("invalid upload acknowledgement"))
	}
	return s.commit(remote, Fingerprint(data))
}
func (s *Service) open(r Remote) ([]byte, error) {
	if r.Config == nil {
		return nil, fmt.Errorf("cloud file backup is missing")
	}
	return r.Config.Unpack()
}
func (s *Service) Resolve(choice string, revision int64, fingerprint string) (Status, error) {
	s.op.Lock()
	defer s.op.Unlock()
	if err := s.requireEnabledLocked(); err != nil {
		return s.Status(), err
	}
	st := s.Status()
	if !st.Connected ||
		(st.State != "conflict" &&
			st.State != "update_available") ||
		st.Revision != revision ||
		st.LocalFingerprint != fingerprint {
		return st, fmt.Errorf("confirmation expired; check for updates again")
	}
	if time.Now().Before(s.retryAt) {
		return st, fmt.Errorf("waiting before retrying cloud sync")
	}
	token, e := s.vault.Get(s.account)
	if e != nil {
		return s.fail(e)
	}
	remote, e := s.request(s.options.ServerURL, token, "GET", "config", nil)
	if e != nil {
		return s.fail(e)
	}
	local, e := s.hooks.Snapshot()
	if e != nil {
		return s.fail(e)
	}
	if remote.SpaceID != s.options.SpaceID ||
		remote.Revision != revision ||
		Fingerprint(local) != fingerprint {
		return s.syncLocked()
	}
	if remote.Config == nil {
		return s.fail(fmt.Errorf("cloud configuration is missing"))
	}
	data, e := s.open(remote)
	if e != nil {
		return s.fail(e)
	}
	if choice == "local" {
		return s.push(token, remote, local)
	}
	if choice != "cloud" {
		return st, fmt.Errorf("invalid conflict choice")
	}
	if e = s.hooks.Apply(data, fingerprint); e != nil {
		var applied *AppliedError
		if errors.As(e, &applied) {
			if _, commitErr := s.commit(remote, Fingerprint(data)); commitErr != nil {
				return s.Status(), commitErr
			}
			return s.fail(e)
		}
		return s.fail(e)
	}
	return s.commit(remote, Fingerprint(data))
}

// Notify never reads configuration or takes the operation lock: saves may hold configGate.
func (s *Service) Notify() {
	st := s.Status()
	if !st.Enabled || !st.Connected {
		return
	}
	select {
	case s.changes <- struct{}{}:
	default:
	}
}
func (s *Service) signalWake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Service) Focus() {
	s.mu.Lock()
	allowed := s.status.Enabled && s.status.Connected && s.status.Automatic && time.Since(s.lastFocus) >= 30*time.Second
	if allowed {
		s.lastFocus = time.Now()
	}
	s.mu.Unlock()
	if allowed {
		s.signalWake()
	}
}
func (s *Service) Start() { s.wg.Add(1); go s.run() }
func (s *Service) Close() { s.cancel(); s.wg.Wait() }

func (s *Service) requireEnabledLocked() error {
	if s.Stopped() {
		return fmt.Errorf("cloud sync stopped; restart the app")
	}
	if !s.options.Enabled {
		return fmt.Errorf("cloud sync is disabled")
	}
	return nil
}

// restoreStatusLocked checks local changes only; enabling manual mode never accesses the server.
func (s *Service) restoreStatusLocked() {
	st := s.Status()
	st.Enabled = s.options.Enabled
	st.State, st.Error = "disconnected", ""
	st.Automatic = s.options.Automatic && !s.suspended
	if !st.Enabled {
		st.State = "disabled"
	} else if st.Connected {
		st.State = "idle"
		b := s.options.Baseline
		if b.SpaceID == s.options.SpaceID && b.ServerURL == s.options.ServerURL && s.options.Protocol == SchemaVersion {
			st.LastSyncedRevision, st.Revision, st.LastSyncedAt = b.Revision, b.Revision, b.SyncedAt
		}
		if local, err := s.hooks.Snapshot(); err == nil {
			st.LocalFingerprint = Fingerprint(local)
			if st.LocalFingerprint != b.Fingerprint {
				st.State = "pending"
			}
		} else {
			st.State, st.Error = "error", err.Error()
		}
	}
	s.publish(st)
}
func (s *Service) SetEnabled(enabled bool) (Status, error) {
	s.op.Lock()
	defer s.op.Unlock()
	if s.Stopped() {
		return s.Status(), fmt.Errorf("cloud sync stopped; restart the app")
	}
	if s.options.Enabled == enabled {
		return s.Status(), nil
	}
	o := s.options
	o.Enabled = enabled
	if err := s.persist(o); err != nil {
		return s.Status(), err
	}
	s.options = o
	s.pendingToken = ""
	s.restoreStatusLocked()
	s.signalWake()
	return s.Status(), nil
}

func (s *Service) run() {
	defer s.wg.Done()
	var poll, check *time.Ticker
	var pollC, checkC, debounce <-chan time.Time
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	stopTimers := func() {
		if poll != nil {
			poll.Stop()
			check.Stop()
			poll, check = nil, nil
		}
		pollC, checkC, debounce = nil, nil, nil
		timer.Stop()
	}
	defer stopTimers()
	last := ""
	syncAuto := func() {
		s.op.Lock()
		defer s.op.Unlock()
		st := s.Status()
		if !s.Stopped() && st.Enabled && st.Connected && st.Automatic && !s.suspended {
			_, _ = s.syncLocked()
		}
	}
	scan := func() {
		s.op.Lock()
		defer s.op.Unlock()
		st := s.Status()
		if s.Stopped() || !st.Enabled || !st.Connected {
			return
		}
		data, err := s.hooks.Snapshot()
		if err != nil {
			return
		}
		hash := Fingerprint(data)
		if hash == last {
			return
		}
		last = hash
		s.markLocalChangeLocked(hash)
		timer.Stop()
		timer.Reset(2 * time.Second)
		debounce = timer.C
	}
	// Startup and every state transition use the same wake path.
	s.signalWake()
	for {
		st := s.Status()
		if st.Enabled && st.Connected && !s.Stopped() {
			if poll == nil {
				poll, check = time.NewTicker(time.Second), time.NewTicker(5*time.Minute)
				pollC, checkC = poll.C, check.C
				last = ""
			}
		} else {
			stopTimers()
			last = ""
		}
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
			scan()
			syncAuto()
		case <-s.changes:
			scan()
		case <-pollC:
			scan()
			s.op.Lock()
			due := !s.retryAt.IsZero() && !time.Now().Before(s.retryAt)
			s.op.Unlock()
			if due {
				syncAuto()
			}
		case <-checkC:
			syncAuto()
		case <-debounce:
			debounce = nil
			syncAuto()
		}
	}
}

// Suspend cancels requests before a config-directory relocation; restart creates a new service.
func (s *Service) Suspend() { s.cancel() }

// Caller holds op, and has just read this fingerprint from the local configuration.
func (s *Service) markLocalChangeLocked(hash string) {
	st := s.Status()
	if !st.Enabled || !st.Connected || st.State == "syncing" || st.State == "error" {
		return
	}
	if hash == s.options.Baseline.Fingerprint {
		return
	}
	if st.State == "update_available" {
		st.State = "conflict"
	} else if st.State != "conflict" {
		st.State = "pending"
	}
	st.LocalFingerprint = hash
	s.publish(st)
}

func (s *Service) Stopped() bool { return s.ctx.Err() != nil }
