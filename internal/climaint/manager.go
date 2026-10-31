package climaint

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"loris-tunnel/internal/updater"
)

const maxBytes int64 = 80 << 20
const cacheTTL = 24 * time.Hour

type Spec struct {
	Provider   string
	Target     string
	Resolve    func() (string, string, error)
	Inspect    func(context.Context, string) (string, error)
	Unpack     func(string, string) (string, error)
	ReleaseAPI string // Test-only replacement for release metadata.
	DirectURL  string // Test-only replacement for the official download source.
}
type Status struct {
	Provider         string `json:"provider"`
	Installed        bool   `json:"installed"`
	CurrentVersion   string `json:"currentVersion"`
	Source           string `json:"source"`
	Path             string `json:"path"`
	LatestVersion    string `json:"latestVersion"`
	CandidateID      string `json:"candidateId"`
	CandidateVersion string `json:"candidateVersion"`
	CandidateKind    string `json:"candidateKind"`
	HasUpdate        bool   `json:"hasUpdate"`
	ComparisonKnown  bool   `json:"comparisonKnown"`
	PreviousVersion  string `json:"previousVersion"`
	CanRestore       bool   `json:"canRestore"`
	Phase            string `json:"phase"`
	Busy             bool   `json:"busy"`
	CanCancel        bool   `json:"canCancel"`
	Downloaded       int64  `json:"downloaded"`
	Total            int64  `json:"total"`
	BlockedReason    string `json:"blockedReason"`
	BlockedCount     int    `json:"blockedCount"`
	Error            string `json:"error"`
	DocsURL          string `json:"docsUrl"`
}
type candidate struct {
	ID          string    `json:"id"`
	Version     string    `json:"version"`
	URL         string    `json:"url"`
	Asset       string    `json:"asset"`
	ChecksumURL string    `json:"checksumUrl,omitempty"`
	Checksum    string    `json:"checksum,omitempty"`
	ETag        string    `json:"etag,omitempty"`
	File        string    `json:"file,omitempty"`
	Digest      string    `json:"digest,omitempty"`
	Kind        string    `json:"kind"`
	CheckedAt   time.Time `json:"checkedAt"`
}
type Manager struct {
	Spec   Spec
	op     sync.Mutex
	mu     sync.Mutex
	status Status
	cancel context.CancelFunc
	// Injected only in tests to keep every failure phase reproducible.
	beforeCommit func() error
}

func New(spec Spec) *Manager {
	return &Manager{Spec: spec, status: Status{Provider: spec.Provider, Phase: "idle", DocsURL: docsURL(spec.Provider)}}
}
func (m *Manager) candidatePath() string {
	return filepath.Join(directory(m.Spec.Target), "candidate.json")
}
func (m *Manager) phase(phase string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.Phase = phase
	m.status.CanCancel = phase == "checking" || phase == "downloading" || phase == "validating"
}
func (m *Manager) snapshot() Status { m.mu.Lock(); defer m.mu.Unlock(); return m.status }
func (m *Manager) refresh(ctx context.Context) error {
	if _, _, err := SelectedPath(m.Spec.Target); err != nil {
		return err
	}
	state, err := loadSelection(m.Spec.Target)
	if err != nil {
		return err
	}
	path, source, resolveErr := m.Spec.Resolve()
	version := ""
	if resolveErr == nil {
		version, err = m.Spec.Inspect(ctx, path)
	}
	m.mu.Lock()
	m.status.Path = path
	m.status.Source = source
	m.status.CurrentVersion = version
	m.status.Installed = version != "" && err == nil
	m.status.PreviousVersion = ""
	m.status.CanRestore = false
	if state.Previous != nil {
		m.status.PreviousVersion = state.Previous.Version
		_, backupErr := snapshotBytes(m.Spec.Target, state.Previous)
		m.status.CanRestore = backupErr == nil
	}

	if m.status.CandidateID != "" {
		cmp, known := updater.CompareVersions(m.status.CandidateVersion, m.status.CurrentVersion)
		m.status.ComparisonKnown = known
		m.status.HasUpdate = !m.status.Installed || known && cmp > 0
	}
	m.mu.Unlock()
	// A missing/broken current CLI can be repaired by installation/import.
	return nil
}
func (m *Manager) Status(ctx context.Context) Status {
	if m.op.TryLock() {
		if err := m.refresh(ctx); err != nil {
			m.mu.Lock()
			m.status.Error = err.Error()
			m.mu.Unlock()
		}
		m.op.Unlock()
	}
	return m.snapshot()
}
func (m *Manager) begin(ctx context.Context, phase string) (context.Context, func(error), error) {
	if !m.op.TryLock() {
		return nil, nil, fmt.Errorf("CLI maintenance is already running")
	}
	if err := m.refresh(ctx); err != nil {
		m.op.Unlock()
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.cancel = cancel
	m.status.Busy = true
	m.status.Error = ""
	m.status.Downloaded = 0
	m.status.Total = 0
	m.mu.Unlock()
	m.phase(phase)
	done := func(err error) {
		cancel()
		m.mu.Lock()
		m.cancel = nil
		m.status.Busy = false
		m.status.CanCancel = false
		if err != nil {
			m.status.Error = err.Error()
			m.status.Phase = "error"
			if errors.Is(err, context.Canceled) {
				m.status.Phase = "cancelled"
				m.status.Error = ""
			}
		}
		m.mu.Unlock()
		m.op.Unlock()
	}
	return ctx, done, nil
}
func (m *Manager) Cancel() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil && m.status.CanCancel {
		m.cancel()
		return true
	}
	return !m.status.Busy
}
func newID() string { var raw [16]byte; _, _ = rand.Read(raw[:]); return hex.EncodeToString(raw[:]) }
func (m *Manager) showCandidate(item candidate) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.CandidateID = item.ID
	m.status.CandidateVersion = item.Version
	m.status.CandidateKind = item.Kind
	if item.Kind != "import" {
		m.status.LatestVersion = item.Version
	}
	cmp, known := updater.CompareVersions(item.Version, m.status.CurrentVersion)
	m.status.ComparisonKnown = known
	m.status.HasUpdate = !m.status.Installed || known && cmp > 0
}
func (m *Manager) Check(ctx context.Context, force bool) (result Status, err error) {
	ctx, done, err := m.begin(ctx, "checking")
	if err != nil {
		return m.snapshot(), err
	}
	defer func() { done(err); result = m.snapshot() }()
	var cached candidate
	cacheErr := readJSON(m.candidatePath(), &cached)
	if !force && cacheErr == nil && cached.Kind == "release" && time.Since(cached.CheckedAt) >= 0 && time.Since(cached.CheckedAt) < cacheTTL {
		m.showCandidate(cached)
		m.phase("checked")
		return m.snapshot(), nil
	}
	item, err := m.latest(ctx, cached)
	if err != nil {
		return m.snapshot(), err
	}
	if err = m.saveCandidate(item); err != nil {
		return m.snapshot(), err
	}
	m.showCandidate(item)
	m.phase("checked")
	return m.snapshot(), nil
}
func (m *Manager) Import(ctx context.Context, path string) (result Status, err error) {
	ctx, done, err := m.begin(ctx, "validating")
	if err != nil {
		return m.snapshot(), err
	}
	defer func() { done(err); result = m.snapshot() }()
	raw, err := readBinary(path)
	if err != nil {
		return m.snapshot(), err
	}
	item := candidate{ID: newID(), Kind: "import", Digest: digest(raw), CheckedAt: time.Now()}
	item.File = "candidate-" + item.ID + filepath.Ext(m.Spec.Target)
	saved := filepath.Join(directory(m.Spec.Target), item.File)
	if err = writeExecutable(saved, raw); err != nil {
		return m.snapshot(), err
	}
	item.Version, err = m.Spec.Inspect(ctx, saved)
	if err != nil {
		_ = os.Remove(saved)
		return m.snapshot(), err
	}
	if err = m.saveCandidate(item); err != nil {
		_ = os.Remove(saved)
		return m.snapshot(), err
	}
	m.showCandidate(item)
	m.phase("checked")
	return m.snapshot(), nil
}
func (m *Manager) Apply(ctx context.Context, id string, guard func() (func(), error)) (result Status, err error) {
	ctx, done, err := m.begin(ctx, "downloading")
	if err != nil {
		return m.snapshot(), err
	}
	defer func() { done(err); result = m.snapshot() }()
	var item candidate
	if err = readJSON(m.candidatePath(), &item); err != nil {
		return m.snapshot(), err
	}
	if id == "" || item.ID != id {
		return m.snapshot(), fmt.Errorf("The selected CLI version changed; check again")
	}
	if item.File == "" {
		if err = m.prepare(ctx, &item); err != nil {
			return m.snapshot(), err
		}
		if err = m.saveCandidate(item); err != nil {
			return m.snapshot(), err
		}
	}
	path, err := m.preparedPath(item)
	if err != nil {
		return m.snapshot(), err
	}
	version, err := m.Spec.Inspect(ctx, path)
	if err != nil {
		return m.snapshot(), err
	}
	if version != item.Version {
		return m.snapshot(), fmt.Errorf("The selected CLI version changed; check again")
	}
	if item.Kind != "import" && m.snapshot().Installed {
		cmp, known := updater.CompareVersions(version, m.snapshot().CurrentVersion)
		if !known || cmp <= 0 {
			return m.snapshot(), fmt.Errorf("No newer comparable CLI version is selected")
		}
	}
	if err = ctx.Err(); err != nil {
		return m.snapshot(), err
	}
	release, err := guard()
	if err != nil {
		return m.snapshot(), err
	}
	defer release()
	m.phase("replacing")
	err = m.replace(context.WithoutCancel(ctx), path, version, "")
	if err == nil {
		m.phase("updated")
		err = m.refresh(context.WithoutCancel(ctx))
	}
	return m.snapshot(), err
}
func (m *Manager) Restore(ctx context.Context, guard func() (func(), error)) (result Status, err error) {
	ctx, done, err := m.begin(ctx, "validating")
	if err != nil {
		return m.snapshot(), err
	}
	defer func() { done(err); result = m.snapshot() }()
	state, err := loadSelection(m.Spec.Target)
	if err != nil {
		return m.snapshot(), err
	}
	raw, err := snapshotBytes(m.Spec.Target, state.Previous)
	if err != nil {
		return m.snapshot(), err
	}
	source := filepath.Join(directory(m.Spec.Target), state.Previous.File)
	version, err := m.Spec.Inspect(ctx, source)
	if err != nil {
		return m.snapshot(), err
	}
	if version != state.Previous.Version {
		return m.snapshot(), fmt.Errorf("Previous CLI backup has an unexpected version")
	}
	external := ""
	if state.Previous.Source != "managed" {
		if original, e := readBinary(state.Previous.SourcePath); e == nil && digest(original) == digest(raw) {
			external = state.Previous.SourcePath
		}
	}
	if err = ctx.Err(); err != nil {
		return m.snapshot(), err
	}
	release, err := guard()
	if err != nil {
		return m.snapshot(), err
	}
	defer release()
	m.phase("restoring")
	err = m.replace(context.WithoutCancel(ctx), source, version, external)
	if err == nil {
		m.phase("restored")
		err = m.refresh(context.WithoutCancel(ctx))
	}
	return m.snapshot(), err
}
func (m *Manager) preparedPath(item candidate) (string, error) {
	if item.File == "" || filepath.Base(item.File) != item.File {
		return "", fmt.Errorf("Prepared CLI file is missing")
	}
	path := filepath.Join(directory(m.Spec.Target), item.File)
	raw, err := readBinary(path)
	if err != nil || digest(raw) != item.Digest {
		return "", fmt.Errorf("Prepared CLI file is missing or damaged; check again")
	}
	return path, nil
}
func (m *Manager) replace(ctx context.Context, candidatePath, version, external string) (err error) {
	currentPath, source, resolveErr := m.Spec.Resolve()
	currentVersion := ""
	if resolveErr == nil {
		currentVersion, _ = m.Spec.Inspect(ctx, currentPath)
	}
	lock := fileLock(m.Spec.Target)
	lock.Lock()
	defer lock.Unlock()
	if err = recoverLocked(m.Spec.Target); err != nil {
		return err
	}
	before, err := loadSelection(m.Spec.Target)
	if err != nil {
		return err
	}
	next := before
	if currentVersion != "" {
		next.Previous, err = saveSnapshot(m.Spec.Target, currentPath, currentVersion, source)
		if err != nil {
			return err
		}
	}
	entry := journal{Before: before}
	if _, e := os.Stat(m.Spec.Target); e == nil {
		entry.TargetExisted = true
		entry.SavedTarget, err = saveSnapshot(m.Spec.Target, m.Spec.Target, "", "managed")
		if err != nil {
			return err
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	raw, err := readBinary(candidatePath)
	if err != nil {
		return err
	}
	if err = writeJSON(journalPath(m.Spec.Target), entry); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if recoveryErr := rollback(m.Spec.Target, entry); recoveryErr != nil {
				err = fmt.Errorf("%v; recovery failed, backups preserved: %w", err, recoveryErr)
			}
		}
	}()
	next.Selected = m.Spec.Target
	if external != "" {
		next.Selected = external
	} else if err = writeExecutable(m.Spec.Target, raw); err != nil {
		return err
	}
	actual, err := m.Spec.Inspect(ctx, next.Selected)
	if err != nil {
		return err
	}
	if actual != version {
		return fmt.Errorf("Installed CLI version does not match the selected version")
	}
	if m.beforeCommit != nil {
		if err = m.beforeCommit(); err != nil {
			return err
		}
	}
	if err = writeJSON(statePath(m.Spec.Target), next); err != nil {
		return err
	}
	if err = os.Remove(journalPath(m.Spec.Target)); err != nil {
		return err
	}
	collectBackups(m.Spec.Target, next.Previous)
	return nil
}

func (m *Manager) saveCandidate(item candidate) error {
	if err := writeJSON(m.candidatePath(), item); err != nil {
		return err
	}
	files, _ := filepath.Glob(filepath.Join(directory(m.Spec.Target), "candidate-*"))
	for _, file := range files {
		if filepath.Base(file) != item.File {
			_ = os.Remove(file)
		}
	}
	return nil
}
