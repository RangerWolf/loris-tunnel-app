package biz

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"loris-tunnel/internal/cloudflared"
	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/devtunnel"
	"loris-tunnel/internal/forward"
	"loris-tunnel/internal/model"
)

type cloudflareRuntime struct {
	host *cloudflared.Host
}

func (c *cloudflareRuntime) Stop() error                         { return c.host.Stop() }
func (c *cloudflareRuntime) Done() <-chan struct{}               { return c.host.Done() }
func (c *cloudflareRuntime) Err() error                          { return c.host.Err() }
func (c *cloudflareRuntime) Events() <-chan forward.RuntimeEvent { return c.host.Events() }
func (c *cloudflareRuntime) PublicURL() string                   { return c.host.PublicURL() }

func (b *TunnelBiz) CloudflareStatus(ctx context.Context) (cloudflared.Status, error) {
	return b.cloudflare.Inspect(ctx)
}

func (b *TunnelBiz) ListCloudflareResources(ctx context.Context) cloudflared.ResourceList {
	return b.cloudflare.ListResources(ctx)
}

func (b *TunnelBiz) ListCloudflareZones(ctx context.Context) cloudflared.ZoneList {
	return b.cloudflare.ListZones(ctx)
}

func (b *TunnelBiz) GetCloudflareResource(ctx context.Context, id string) cloudflared.ResourceDetail {
	return b.cloudflare.GetResource(ctx, id)
}

func (b *TunnelBiz) CreateCloudflareResource(ctx context.Context, input cloudflared.CreateResourceInput) cloudflared.ResourceDetail {
	result := b.cloudflare.CreateResource(ctx, input)
	if result.State == cloudflared.ResourceReady {
		b.updateCloudflareResourceCache(result.Resource, false)
	}
	return result
}

func (b *TunnelBiz) StartCloudflareLogin(progress func(phase, url string), done func()) error {
	return b.StartCloudflareZoneAuthorization("", progress, done)
}

func (b *TunnelBiz) CancelCloudflareLogin() { b.cloudflare.CancelLogin() }

func (b *TunnelBiz) StartCloudflareZoneAuthorization(zoneID string, progress func(string, string), done func()) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cliChanging[model.ProviderCloudflare] {
		return fmt.Errorf("CLI version maintenance is in progress")
	}
	return b.cloudflare.StartZoneAuthorization(zoneID, progress, done)
}

func (b *TunnelBiz) ListCloudflareAuthorizations(ctx context.Context, force bool) cloudflared.ZoneList {
	result := b.ListCloudflareZonesCached(ctx, force)
	cfg, err := b.storage.Load()
	if err != nil {
		return cloudflared.ZoneList{State: cloudflared.ResourceError, Detail: "Could not read local tunnel bindings", Items: []cloudflared.Zone{}}
	}
	entries, err := os.ReadDir(b.deletionDir())
	if err != nil && !os.IsNotExist(err) {
		return cloudflared.ZoneList{State: cloudflared.ResourceError, Detail: "Could not read pending DNS cleanup", Items: []cloudflared.Zone{}}
	}
	for i := range result.Items {
		zone := &result.Items[i]
		for _, tunnel := range cfg.Tunnels {
			if tunnel.ProviderName() != model.ProviderCloudflare || tunnel.Cloudflare == nil {
				continue
			}
			host, _ := cloudflared.NormalizeHostname(tunnel.Cloudflare.Hostname)
			best := ""
			for _, candidate := range result.Items {
				if candidate.Name != "" && (host == candidate.Name || strings.HasSuffix(host, "."+candidate.Name)) && len(candidate.Name) > len(best) {
					best = candidate.Name
				}
			}
			if zone.Name == "" || best == zone.Name {
				zone.LinkedTunnels++
			}
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			task, e := b.readDeletionTask(strings.TrimSuffix(entry.Name(), ".json"))
			if e != nil {
				zone.PendingCleanup = true
				continue
			}
			if task.Provider != model.ProviderCloudflare || (task.Account != "" && !b.cloudflare.MatchesAccount(task.Account)) {
				continue
			}
			for _, record := range task.DNS {
				if record.ZoneID == zone.ID {
					zone.PendingCleanup = true
				}
			}
			if task.CleanupDNS && !task.RemoteIDDeleted && len(task.DNS) == 0 {
				zone.PendingCleanup = true
			}
		}
	}
	return result
}

func (b *TunnelBiz) RemoveCloudflareAuthorization(ctx context.Context, zoneID string) error {
	b.cloudflareAuthMu.Lock()
	defer b.cloudflareAuthMu.Unlock()
	result := b.ListCloudflareAuthorizations(ctx, true)
	if result.State != cloudflared.ResourceReady {
		return fmt.Errorf("cloudflare_authorization_verify_failed")
	}
	for _, zone := range result.Items {
		if zone.ID != zoneID {
			continue
		}
		if zone.LinkedTunnels > 0 {
			return fmt.Errorf("cloudflare_authorization_linked")
		}
		if zone.PendingCleanup {
			return fmt.Errorf("cloudflare_authorization_cleanup_pending")
		}
		return b.cloudflare.RemoveAuthorization(zoneID)
	}
	return fmt.Errorf("cloudflare_authorization_not_found")
}

func (b *TunnelBiz) InstallCloudflareCLI(ctx context.Context) error {
	return b.installManagedCLI(ctx, model.ProviderCloudflare)
}
func (b *TunnelBiz) CancelCloudflareInstall() {
	b.CancelCLIUpdate(model.ProviderCloudflare)
	b.cloudflare.CancelInstall()
}

func (b *TunnelBiz) DeleteCloudflareResource(ctx context.Context, id string) cloudflared.DeleteResourceResult {
	id = strings.TrimSpace(id)
	if !cloudflared.ValidTunnelID(id) {
		return cloudflared.DeleteResourceResult{State: cloudflared.ResourceError, Detail: "invalid cloudflare tunnel id"}
	}
	cfg, err := b.storage.Load()
	if err != nil {
		return cloudflared.DeleteResourceResult{State: cloudflared.ResourceError, Detail: err.Error()}
	}
	for _, item := range cfg.Tunnels {
		if item.ProviderName() != model.ProviderCloudflare || item.Cloudflare == nil {
			continue
		}
		if !cloudflared.SameTunnelID(item.Cloudflare.TunnelID, id) {
			continue
		}
		if b.isRunning(item.ID) {
			return cloudflared.DeleteResourceResult{State: "running", Detail: "stop the tunnel and remove the binding first", TunnelName: item.Name}
		}
		return cloudflared.DeleteResourceResult{State: "linked", Detail: "remove the local tunnel binding first", TunnelName: item.Name}
	}
	if err := b.cloudflare.DeleteRemote(ctx, id); err != nil {
		state, message := cloudflareFailureKind(err)
		return cloudflared.DeleteResourceResult{State: state, Detail: message}
	}
	b.updateCloudflareResourceCache(cloudflared.Resource{ID: id}, true)
	return cloudflared.DeleteResourceResult{State: "deleted"}
}

func (b *TunnelBiz) newStartedCloudflareRuntime(task *model.Tunnel) (tunnelRuntime, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	b.mu.Lock()
	if b.starting[task.ID] != task {
		b.mu.Unlock()
		return nil, context.Canceled
	}
	b.devStartCancel[task] = cancel
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.devStartCancel, task); b.mu.Unlock() }()

	if err := b.cloudflare.Preflight(ctx); err != nil {
		return nil, err
	}
	cfg := task.Cloudflare
	if cfg == nil {
		return nil, fmt.Errorf("cloudflare config is required")
	}
	localHost := strings.TrimSpace(cfg.LocalHost)
	if localHost == "" {
		localHost = task.LocalHost
	}
	port := cfg.LocalPort
	if port == 0 {
		port = task.LocalPort
	}
	protocol := strings.TrimSpace(cfg.Protocol)
	if protocol == "" {
		protocol = task.Mode
	}
	if !devtunnel.ValidTargetHost(localHost) {
		return nil, fmt.Errorf("invalid target host")
	}
	hostname, err := cloudflared.NormalizeHostname(cfg.Hostname)
	if err != nil {
		return nil, err
	}
	if err := probeLocalOrigin(localHost, port); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(cfg.TunnelID)
	if id != "" && !cloudflared.ValidTunnelID(id) {
		return nil, fmt.Errorf("invalid cloudflare tunnel id")
	}
	if name := b.cloudflareOwnerName(task.ID, id); name != "" {
		return nil, cloudflared.TunnelAlreadyUsed(name)
	}
	if id == "" {
		created := b.cloudflare.CreateResource(ctx, cloudflared.CreateResourceInput{Name: cfg.TunnelName})
		if created.State != cloudflared.ResourceReady || !cloudflared.ValidTunnelID(created.Resource.ID) {
			return nil, cloudflareResourceError(created)
		}
		id = created.Resource.ID
		if b.startCanceled(task.ID) {
			_ = b.cloudflare.DeleteRemote(context.Background(), id)
			return nil, fmt.Errorf("cloudflare tunnel start canceled")
		}
		if err := b.persistCloudflareTunnelID(task.ID, id, created.Resource.Name); err != nil {
			_ = b.cloudflare.DeleteRemote(context.Background(), id)
			return nil, err
		}
		cfg.TunnelID = id
		b.updateCloudflareResourceCache(created.Resource, false)
		if created.Resource.Name != "" {
			cfg.TunnelName = created.Resource.Name
		}
		if b.startCanceled(task.ID) {
			return nil, fmt.Errorf("cloudflare tunnel start canceled")
		}
	} else {
		detail := b.cloudflare.GetResource(ctx, id)
		if detail.State != cloudflared.ResourceReady {
			return nil, cloudflareResourceError(detail)
		}
		b.updateCloudflareResourceCache(detail.Resource, false)
	}
	if name := b.cloudflareOwnerName(task.ID, id); name != "" {
		return nil, cloudflared.TunnelAlreadyUsed(name)
	}
	if !b.cloudflare.CredentialExists(id) {
		return nil, cloudflared.ErrCredentialMissing
	}
	unlock := b.lockResource(resourceLockKey(model.ProviderCloudflare, id))
	defer unlock()
	if err := b.cloudflare.EnsureDNSRoute(ctx, id, hostname); err != nil {
		return nil, err
	}
	cred, err := b.cloudflare.CredentialPath(id)
	if err != nil {
		return nil, err
	}
	configPath, err := cloudflared.WriteRuntimeConfig(task.ID, cloudflared.RuntimeSpec{
		TunnelID:        id,
		CredentialsFile: cred,
		Hostname:        hostname,
		Protocol:        protocol,
		LocalHost:       localHost,
		LocalPort:       port,
	})
	if err != nil {
		return nil, err
	}
	host := b.cloudflare.NewHost(id, configPath, hostname)
	runtime := &cloudflareRuntime{host: host}
	b.mu.Lock()
	if b.starting[task.ID] != task {
		b.mu.Unlock()
		_ = host.Stop()
		return nil, fmt.Errorf("cloudflare tunnel start canceled")
	}
	b.inflight[task.ID] = runtime
	b.mu.Unlock()
	if err := host.Start(); err != nil {
		b.mu.Lock()
		delete(b.inflight, task.ID)
		b.mu.Unlock()
		return nil, err
	}
	return runtime, nil
}

func probeLocalOrigin(host string, port int) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconvItoa(port)), 3*time.Second)
	if err != nil {
		return cloudflared.OriginUnreachable(host, port)
	}
	_ = conn.Close()
	return nil
}

func strconvItoa(port int) string {
	return fmt.Sprintf("%d", port)
}

func (b *TunnelBiz) cloudflareOwnerName(selfID int, tunnelID string) string {
	if strings.TrimSpace(tunnelID) == "" {
		return ""
	}
	cfg, err := b.storage.Load()
	if err != nil {
		return "another tunnel"
	}
	for _, item := range cfg.Tunnels {
		if item.ID == selfID || item.ProviderName() != model.ProviderCloudflare || item.Cloudflare == nil {
			continue
		}
		if cloudflared.SameTunnelID(item.Cloudflare.TunnelID, tunnelID) {
			return item.Name
		}
	}
	return ""
}

func (b *TunnelBiz) persistCloudflareTunnelID(id int, tunnelID, name string) error {
	_, err := b.storage.Update(func(cfg *conf.Config) error {
		for i := range cfg.Tunnels {
			if cfg.Tunnels[i].ID != id {
				continue
			}
			if cfg.Tunnels[i].Cloudflare == nil {
				cfg.Tunnels[i].Cloudflare = &model.CloudflareTunnelConfig{}
			}
			cfg.Tunnels[i].Cloudflare.TunnelID = tunnelID
			if strings.TrimSpace(name) != "" {
				cfg.Tunnels[i].Cloudflare.TunnelName = name
			}
			return nil
		}
		return ErrTunnelNotFound
	})
	return err
}

func ensureCloudflareAvailable(tunnels []model.Tunnel, selfID int, payload model.TunnelPayload) error {
	if payload.Provider != model.ProviderCloudflare || payload.Cloudflare == nil {
		return nil
	}
	id := strings.TrimSpace(payload.Cloudflare.TunnelID)
	host := strings.TrimSpace(payload.Cloudflare.Hostname)
	for _, item := range tunnels {
		if item.ID == selfID || item.ProviderName() != model.ProviderCloudflare || item.Cloudflare == nil {
			continue
		}
		if id != "" && cloudflared.SameTunnelID(item.Cloudflare.TunnelID, id) {
			return cloudflared.TunnelAlreadyUsed(item.Name)
		}
		if host != "" && strings.EqualFold(item.Cloudflare.Hostname, host) {
			return cloudflared.HostnameAlreadyUsed(item.Name)
		}
	}
	return nil
}

func validateCloudflareTunnelPayload(payload model.TunnelPayload) error {
	if !devtunnel.ValidTargetHost(payload.LocalHost) {
		return fmt.Errorf("invalid target host: expected an IPv4 address, IPv6 address, or hostname")
	}
	switch payload.Mode {
	case "http", "https":
	default:
		return fmt.Errorf("unsupported cloudflare protocol: %s", payload.Mode)
	}
	if payload.Cloudflare == nil {
		return fmt.Errorf("cloudflare config is required")
	}
	if _, err := cloudflared.NormalizeHostname(payload.Cloudflare.Hostname); err != nil {
		return err
	}
	switch payload.CloudflareIDMode {
	case "", "auto", "bind":
	default:
		return fmt.Errorf("unsupported cloudflare tunnel id mode")
	}
	if payload.CloudflareIDMode == "bind" {
		if strings.TrimSpace(payload.Cloudflare.TunnelID) == "" {
			return fmt.Errorf("cloudflare tunnel id is required")
		}
	} else if strings.TrimSpace(payload.Cloudflare.TunnelID) == "" && !cloudflared.ValidTunnelName(payload.Cloudflare.TunnelName) {
		return cloudflared.ErrInvalidTunnelName
	}
	if payload.Cloudflare.TunnelID != "" && !cloudflared.ValidTunnelID(payload.Cloudflare.TunnelID) {
		return fmt.Errorf("invalid cloudflare tunnel id")
	}
	return nil
}

func cloudflareResourceError(detail cloudflared.ResourceDetail) error {
	switch detail.State {
	case cloudflared.ResourceAuth:
		return cloudflared.ErrAuthRequired
	case cloudflared.ResourceNotFound:
		return cloudflared.ErrTunnelNotFound
	case cloudflared.ResourceUnavailable:
		return cloudflared.ErrNotInstalled
	case cloudflared.ResourcePermission:
		return fmt.Errorf("Cloudflare permission was denied.")
	case cloudflared.ResourceNetwork:
		return fmt.Errorf("Cloudflare could not be reached.")
	default:
		if detail.Detail != "" {
			return fmt.Errorf("%s", detail.Detail)
		}
		return fmt.Errorf("cloudflare query failed")
	}
}

func cloudflareFailureKind(err error) (string, string) {
	text := strings.TrimSpace(err.Error())
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "authentication"), strings.Contains(lower, "not logged"):
		return cloudflared.ResourceAuth, cloudflared.ErrAuthRequired.Error()
	case strings.Contains(lower, "not found"):
		return cloudflared.ResourceNotFound, text
	case strings.Contains(lower, "permission"), strings.Contains(lower, "forbidden"):
		return cloudflared.ResourcePermission, text
	case strings.Contains(lower, "timeout"), strings.Contains(lower, "could not be reached"):
		return cloudflared.ResourceNetwork, text
	default:
		return cloudflared.ResourceError, text
	}
}
