package biz

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/forward"
	"loris-tunnel/internal/model"
)

var ErrTunnelNotFound = errors.New("tunnel not found")

type TunnelBiz struct {
	storage *conf.Storage
	mu      sync.Mutex
	runs    map[int]*forward.LocalForward
}

func NewTunnelBiz(storage *conf.Storage) *TunnelBiz {
	return &TunnelBiz{
		storage: storage,
		runs:    make(map[int]*forward.LocalForward),
	}
}

func (b *TunnelBiz) List() ([]model.Tunnel, error) {
	cfg, err := b.storage.Load()
	if err != nil {
		return nil, err
	}

	items := append([]model.Tunnel{}, cfg.Tunnels...)
	b.attachRuntimeLatencies(items)
	return items, nil
}

func (b *TunnelBiz) Create(payload model.TunnelPayload) (model.Tunnel, error) {
	payload = normalizeTunnelPayload(payload)

	var created model.Tunnel
	_, err := b.storage.Update(func(cfg *conf.Config) error {
		if err := validateTunnelPayload(payload); err != nil {
			return err
		}
		if _, err := collectJumpers(cfg.Jumpers, payload.JumperIDs); err != nil {
			return err
		}

		created = model.Tunnel{
			ID:          nextTunnelID(cfg.Tunnels),
			Name:        payload.Name,
			Mode:        payload.Mode,
			JumperIDs:   append([]int{}, payload.JumperIDs...),
			LocalHost:   payload.LocalHost,
			LocalPort:   payload.LocalPort,
			RemoteHost:  payload.RemoteHost,
			RemotePort:  payload.RemotePort,
			AutoStart:   payload.AutoStart,
			Status:      payload.Status,
			LastError:   "",
			Description: payload.Description,
		}
		cfg.Tunnels = append(cfg.Tunnels, created)
		return nil
	})
	if err != nil {
		return model.Tunnel{}, err
	}

	return created, nil
}

func (b *TunnelBiz) Update(id int, payload model.TunnelPayload) (model.Tunnel, error) {
	if id <= 0 {
		return model.Tunnel{}, fmt.Errorf("invalid tunnel id")
	}
	if b.isRunning(id) {
		return model.Tunnel{}, fmt.Errorf("tunnel is running, stop it before editing")
	}

	payload = normalizeTunnelPayload(payload)

	var updated model.Tunnel
	_, err := b.storage.Update(func(cfg *conf.Config) error {
		if err := validateTunnelPayload(payload); err != nil {
			return err
		}
		if _, err := collectJumpers(cfg.Jumpers, payload.JumperIDs); err != nil {
			return err
		}

		idx := -1
		for i := range cfg.Tunnels {
			if cfg.Tunnels[i].ID == id {
				idx = i
				break
			}
		}
		if idx == -1 {
			return ErrTunnelNotFound
		}

		updated = model.Tunnel{
			ID:          id,
			Name:        payload.Name,
			Mode:        payload.Mode,
			JumperIDs:   append([]int{}, payload.JumperIDs...),
			LocalHost:   payload.LocalHost,
			LocalPort:   payload.LocalPort,
			RemoteHost:  payload.RemoteHost,
			RemotePort:  payload.RemotePort,
			AutoStart:   payload.AutoStart,
			Status:      payload.Status,
			LastError:   cfg.Tunnels[idx].LastError,
			Description: payload.Description,
		}
		cfg.Tunnels[idx] = updated
		return nil
	})
	if err != nil {
		return model.Tunnel{}, err
	}

	return updated, nil
}

func (b *TunnelBiz) Delete(id int) error {
	if id <= 0 {
		return fmt.Errorf("invalid tunnel id")
	}
	if err := b.stopRuntime(id); err != nil {
		return err
	}

	_, err := b.storage.Update(func(cfg *conf.Config) error {
		idx := -1
		for i := range cfg.Tunnels {
			if cfg.Tunnels[i].ID == id {
				idx = i
				break
			}
		}
		if idx == -1 {
			return ErrTunnelNotFound
		}

		cfg.Tunnels = append(cfg.Tunnels[:idx], cfg.Tunnels[idx+1:]...)
		return nil
	})
	return err
}

func (b *TunnelBiz) Toggle(id int) (model.Tunnel, error) {
	if id <= 0 {
		return model.Tunnel{}, fmt.Errorf("invalid tunnel id")
	}

	cfg, err := b.storage.Load()
	if err != nil {
		return model.Tunnel{}, err
	}

	tunnel, ok := findTunnelByID(cfg.Tunnels, id)
	if !ok {
		return model.Tunnel{}, ErrTunnelNotFound
	}

	if b.isRunning(id) || tunnel.Status == "running" {
		slog.Info("tunnel toggle stop", "tunnel_id", tunnel.ID, "name", tunnel.Name)
		if err := b.stopRuntime(id); err != nil {
			return model.Tunnel{}, err
		}
		return b.updateStatus(id, "stopped", "")
	}

	jumpers, err := collectJumpers(cfg.Jumpers, tunnel.JumperIDs)
	if err != nil {
		updated, statusErr := b.updateStatus(id, "error", "jumper not found")
		if statusErr != nil {
			return model.Tunnel{}, ErrJumperNotFound
		}
		return updated, nil
	}
	if tunnel.Mode != "local" && tunnel.Mode != "remote" && tunnel.Mode != "dynamic" {
		msg := fmt.Sprintf("mode %s is not supported yet, only local, remote and dynamic forward are implemented", tunnel.Mode)
		updated, statusErr := b.updateStatus(id, "error", msg)
		if statusErr != nil {
			return model.Tunnel{}, fmt.Errorf(msg)
		}
		return updated, nil
	}

	if err := b.startRuntime(tunnel, jumpers); err != nil {
		updated, statusErr := b.updateStatus(id, "error", errReason(err))
		if statusErr != nil {
			return model.Tunnel{}, fmt.Errorf("start tunnel failed: %v (persist status failed: %v)", err, statusErr)
		}
		return updated, nil
	}

	slog.Info("tunnel toggle start", "tunnel_id", tunnel.ID, "name", tunnel.Name)
	updated, err := b.updateStatus(id, "running", "")
	if err != nil {
		_ = b.stopRuntime(id)
		return model.Tunnel{}, err
	}
	return updated, nil
}

func (b *TunnelBiz) TestConnection(payload model.TunnelPayload, inlineJumper *model.JumperPayload) (time.Duration, error) {
	payload = normalizeTunnelPayload(payload)
	if payload.Status == "" {
		payload.Status = "stopped"
	}
	allowEmptyJumpers := inlineJumper != nil
	if err := validateTunnelPayloadWithOption(payload, !allowEmptyJumpers); err != nil {
		return 0, err
	}

	chain := make([]model.Jumper, 0, len(payload.JumperIDs)+1)
	var inline model.Jumper
	hasInline := false
	if inlineJumper != nil {
		jumperPayload := normalizeJumperPayload(*inlineJumper)
		if err := validateJumperPayload(jumperPayload); err != nil {
			return 0, fmt.Errorf("jumper: %w", err)
		}
		inline = model.Jumper{
			Name:                   jumperPayload.Name,
			Host:                   jumperPayload.Host,
			Port:                   jumperPayload.Port,
			User:                   jumperPayload.User,
			AuthType:               jumperPayload.AuthType,
			KeyPath:                jumperPayload.KeyPath,
			AgentSocketPath:        jumperPayload.AgentSocketPath,
			Password:               jumperPayload.Password,
			BypassHostVerification: jumperPayload.BypassHostVerification,
			KeepAliveIntervalMs:    jumperPayload.KeepAliveIntervalMs,
			TimeoutMs:              jumperPayload.TimeoutMs,
			HostKeyAlgorithms:      jumperPayload.HostKeyAlgorithms,
			Notes:                  jumperPayload.Notes,
		}
		hasInline = true
	}
	if len(payload.JumperIDs) > 0 {
		cfg, err := b.storage.Load()
		if err != nil {
			return 0, err
		}
		jumpers, err := collectJumpers(cfg.Jumpers, payload.JumperIDs)
		if err != nil {
			return 0, err
		}
		chain = append(chain, jumpers...)
	}
	if hasInline {
		chain = append(chain, inline)
	}

	t := model.Tunnel{
		Name:       payload.Name,
		Mode:       payload.Mode,
		LocalHost:  payload.LocalHost,
		LocalPort:  payload.LocalPort,
		RemoteHost: payload.RemoteHost,
		RemotePort: payload.RemotePort,
	}
	return forward.TestTunnelConnection(t, chain)
}

func (b *TunnelBiz) attachRuntimeLatencies(items []model.Tunnel) {
	if len(items) == 0 {
		return
	}

	b.mu.Lock()
	runs := make(map[int]*forward.LocalForward, len(b.runs))
	for id, run := range b.runs {
		runs[id] = run
	}
	b.mu.Unlock()

	for i := range items {
		if items[i].Status != "running" {
			items[i].LatencyMs = 0
			continue
		}
		run, ok := runs[items[i].ID]
		if !ok || run == nil {
			items[i].LatencyMs = 0
			continue
		}
		latency, hasLatency := run.LastLatency()
		if !hasLatency || latency <= 0 {
			items[i].LatencyMs = 0
			continue
		}
		items[i].LatencyMs = latency.Milliseconds()
	}
}

func (b *TunnelBiz) StartAutoStart() error {
	cfg, err := b.storage.Load()
	if err != nil {
		return err
	}

	for _, t := range cfg.Tunnels {
		if !t.AutoStart {
			continue
		}
		if t.Mode != "local" && t.Mode != "remote" && t.Mode != "dynamic" {
			_, _ = b.updateStatus(t.ID, "error", fmt.Sprintf("mode %s is not supported yet, only local, remote and dynamic forward are implemented", t.Mode))
			continue
		}

		jumpers, err := collectJumpers(cfg.Jumpers, t.JumperIDs)
		if err != nil {
			_, _ = b.updateStatus(t.ID, "error", "jumper not found")
			continue
		}
		if err := b.startRuntime(t, jumpers); err != nil {
			_, _ = b.updateStatus(t.ID, "error", errReason(err))
			continue
		}
		_, _ = b.updateStatus(t.ID, "running", "")
	}
	return nil
}

func (b *TunnelBiz) Shutdown() {
	b.mu.Lock()
	ids := make([]int, 0, len(b.runs))
	for id := range b.runs {
		ids = append(ids, id)
	}
	b.mu.Unlock()

	for _, id := range ids {
		_ = b.stopRuntime(id)
		_, _ = b.updateStatus(id, "stopped", "")
	}
}

func (b *TunnelBiz) startRuntime(t model.Tunnel, jumpers []model.Jumper) error {
	b.mu.Lock()
	if _, ok := b.runs[t.ID]; ok {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	run := forward.NewLocalForward(t, jumpers)
	if err := run.Start(); err != nil {
		slog.Error("tunnel runtime start failed", "tunnel_id", t.ID, "name", t.Name, "err", err)
		return err
	}

	b.mu.Lock()
	if _, ok := b.runs[t.ID]; ok {
		b.mu.Unlock()
		_ = run.Stop()
		return nil
	}
	b.runs[t.ID] = run
	b.mu.Unlock()
	slog.Info("tunnel runtime started", "tunnel_id", t.ID, "name", t.Name)

	go b.watchRuntime(t.ID, run)
	return nil
}

func (b *TunnelBiz) watchRuntime(id int, run *forward.LocalForward) {
	done := run.Done()
	if done == nil {
		return
	}

	events := run.Events()
	for {
		select {
		case <-done:
			b.mu.Lock()
			active, ok := b.runs[id]
			if !ok || active != run {
				b.mu.Unlock()
				return
			}
			delete(b.runs, id)
			b.mu.Unlock()

			if run.Err() != nil {
				slog.Warn("tunnel runtime exited with error", "tunnel_id", id, "err", run.Err())
				_, _ = b.updateStatus(id, "error", errReason(run.Err()))
			} else {
				slog.Info("tunnel runtime exited", "tunnel_id", id)
			}
			return
		case evt, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			b.mu.Lock()
			active, stillRunning := b.runs[id]
			b.mu.Unlock()
			if !stillRunning || active != run {
				continue
			}
			switch evt.Type {
			case forward.RuntimeEventDisconnected:
				slog.Warn("tunnel runtime disconnected", "tunnel_id", id, "err", evt.Err)
				_, _ = b.updateStatus(id, "error", errReason(evt.Err))
			case forward.RuntimeEventReconnected:
				slog.Info("tunnel runtime reconnected", "tunnel_id", id)
				_, _ = b.updateStatus(id, "running", "")
			}
		}
	}
}

func (b *TunnelBiz) stopRuntime(id int) error {
	b.mu.Lock()
	run, ok := b.runs[id]
	if ok {
		delete(b.runs, id)
	}
	b.mu.Unlock()

	if !ok {
		return nil
	}
	return run.Stop()
}

func (b *TunnelBiz) isRunning(id int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.runs[id]
	return ok
}

func (b *TunnelBiz) updateStatus(id int, status, lastError string) (model.Tunnel, error) {
	var updated model.Tunnel
	_, err := b.storage.Update(func(cfg *conf.Config) error {
		idx := -1
		for i := range cfg.Tunnels {
			if cfg.Tunnels[i].ID == id {
				idx = i
				break
			}
		}
		if idx == -1 {
			return ErrTunnelNotFound
		}
		cfg.Tunnels[idx].Status = status
		cfg.Tunnels[idx].LastError = strings.TrimSpace(lastError)
		updated = cfg.Tunnels[idx]
		return nil
	})
	if err != nil {
		return model.Tunnel{}, err
	}
	if updated.LastError != "" {
		slog.Info("tunnel status updated", "tunnel_id", updated.ID, "name", updated.Name, "status", updated.Status, "error", updated.LastError)
	} else {
		slog.Info("tunnel status updated", "tunnel_id", updated.ID, "name", updated.Name, "status", updated.Status)
	}
	return updated, nil
}

func errReason(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

func normalizeTunnelPayload(payload model.TunnelPayload) model.TunnelPayload {
	payload.Name = strings.TrimSpace(payload.Name)
	payload.Mode = strings.TrimSpace(payload.Mode)
	payload.LocalHost = strings.TrimSpace(payload.LocalHost)
	payload.RemoteHost = strings.TrimSpace(payload.RemoteHost)
	payload.Description = strings.TrimSpace(payload.Description)
	payload.Status = strings.TrimSpace(payload.Status)
	payload.JumperIDs = normalizeJumperIDs(payload.JumperIDs)

	if payload.Mode == "" {
		payload.Mode = "local"
	}
	if payload.Status == "" {
		payload.Status = "stopped"
	}
	if payload.LocalHost == "" {
		payload.LocalHost = "127.0.0.1"
	}

	return payload
}

func validateTunnelPayload(payload model.TunnelPayload) error {
	return validateTunnelPayloadWithOption(payload, true)
}

func validateTunnelPayloadWithOption(payload model.TunnelPayload, requireJumpers bool) error {
	if payload.Name == "" {
		return fmt.Errorf("name is required")
	}
	if requireJumpers && len(payload.JumperIDs) == 0 {
		return fmt.Errorf("jumperIds is required")
	}
	if payload.LocalHost == "" {
		return fmt.Errorf("localHost is required")
	}
	if payload.LocalPort < 1 || payload.LocalPort > 65535 {
		return fmt.Errorf("localPort must be between 1 and 65535")
	}
	switch payload.Mode {
	case "local", "remote", "dynamic":
	default:
		return fmt.Errorf("unsupported mode: %s", payload.Mode)
	}
	if payload.Mode != "dynamic" {
		if payload.RemoteHost == "" {
			return fmt.Errorf("remoteHost is required for non-dynamic mode")
		}
		if payload.RemotePort < 1 || payload.RemotePort > 65535 {
			return fmt.Errorf("remotePort must be between 1 and 65535")
		}
	}
	switch payload.Status {
	case "running", "stopped", "error":
	default:
		return fmt.Errorf("unsupported status: %s", payload.Status)
	}
	return nil
}

func collectJumpers(items []model.Jumper, ids []int) ([]model.Jumper, error) {
	if len(ids) == 0 {
		return nil, ErrJumperNotFound
	}
	collected := make([]model.Jumper, 0, len(ids))
	for _, id := range ids {
		item, ok := findJumperByID(items, id)
		if !ok {
			return nil, ErrJumperNotFound
		}
		collected = append(collected, item)
	}
	return collected, nil
}

func normalizeJumperIDs(ids []int) []int {
	out := make([]int, 0, len(ids))
	seen := make(map[int]struct{}, len(ids))
	appendID := func(id int) {
		if id <= 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range ids {
		appendID(id)
	}
	return out
}

func findJumperByID(items []model.Jumper, id int) (model.Jumper, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return model.Jumper{}, false
}

func findTunnelByID(items []model.Tunnel, id int) (model.Tunnel, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return model.Tunnel{}, false
}

func nextTunnelID(items []model.Tunnel) int {
	next := 1
	for _, item := range items {
		if item.ID >= next {
			next = item.ID + 1
		}
	}
	return next
}
