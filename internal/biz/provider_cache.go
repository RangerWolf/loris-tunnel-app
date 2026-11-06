package biz

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"loris-tunnel/internal/cloudflared"
	"loris-tunnel/internal/devtunnel"
)

const providerCacheTTL = 24 * time.Hour

// Invalidate stale Dev Tunnel access metadata produced by the old ACL parser.
const providerCacheVersion = 2

// Only UI metadata is cached. Connection/start/delete validation uses the live clients.
type providerSnapshot struct {
	ResourceAt map[string]time.Time   `json:"resourceAt,omitempty"`
	Version    int                    `json:"version"`
	ListedAt   time.Time              `json:"listedAt"`
	Dev        []devtunnel.Resource   `json:"dev,omitempty"`
	Cloudflare []cloudflared.Resource `json:"cloudflare,omitempty"`
	Zones      []cloudflared.Zone     `json:"zones,omitempty"`
	ZonesAt    time.Time              `json:"zonesAt"`
}

func (b *TunnelBiz) providerCachePath(provider, account string) string {
	if account == "" {
		return ""
	}
	key := sha256.Sum256([]byte(account))
	return filepath.Join(filepath.Dir(b.storage.Path()), "provider-cache", fmt.Sprintf("%s-%x.json", provider, key[:16]))
}

func readProviderSnapshot(path string) providerSnapshot {
	var snapshot providerSnapshot
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &snapshot) != nil || snapshot.Version != providerCacheVersion {
		return providerSnapshot{Version: providerCacheVersion}
	}
	return snapshot
}

func writeProviderSnapshot(path string, snapshot providerSnapshot) {
	if path == "" {
		return
	}
	if err := saveProviderSnapshot(path, snapshot); err != nil {
		slog.Warn("provider metadata cache write failed", "error", err)
	}
}

func saveProviderSnapshot(path string, snapshot providerSnapshot) error {
	snapshot.Version = providerCacheVersion
	// Never persist observations of remote connections as if they were current.
	for i := range snapshot.Dev {
		snapshot.Dev[i].HostConnections = 0
	}
	for i := range snapshot.Cloudflare {
		snapshot.Cloudflare[i].Connections = 0
		snapshot.Cloudflare[i].Cached = false
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".metadata-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func cacheFresh(at time.Time) bool {
	age := time.Since(at)
	return !at.IsZero() && age >= 0 && age < providerCacheTTL
}

// Caller holds devCacheMu. Account labels come from the official CLI, never from cached metadata.
func (b *TunnelBiz) devCachePath(ctx context.Context) string {
	if !b.devAccountKnown {
		status, err := b.dev.Inspect(ctx)
		if err == nil {
			b.devAccountKnown = true
			b.devAccount = ""
			if status.AccountState == "signed_in" {
				b.devAccount = status.AccountLabel
			}
		}
	}
	return b.providerCachePath("devtunnel", b.devAccount)
}

func (b *TunnelBiz) cloudflareCachePath() string {
	return b.providerCachePath("cloudflare", b.cloudflare.CacheIdentity())
}

func (b *TunnelBiz) ListDevTunnelResourcesCached(ctx context.Context, force bool) devtunnel.ResourceList {
	b.devCacheMu.Lock()
	defer b.devCacheMu.Unlock()
	path := b.devCachePath(ctx)
	snapshot := readProviderSnapshot(path)
	if !force && path != "" && cacheFresh(snapshot.ListedAt) {
		return devtunnel.ResourceList{State: devtunnel.ResourceReady, Items: snapshot.Dev}
	}
	result := b.dev.ListResources(ctx)
	if result.State == devtunnel.ResourceReady {
		// Copy before stripping dynamic fields for disk.
		snapshot.Dev = append([]devtunnel.Resource{}, result.Items...)
		snapshot.ResourceAt = make(map[string]time.Time)
		for _, item := range snapshot.Dev {
			snapshot.ResourceAt[item.ID] = time.Now()
		}
		snapshot.ListedAt = time.Now()
		writeProviderSnapshot(path, snapshot)
	}
	return result
}

func (b *TunnelBiz) GetDevTunnelResourceCached(ctx context.Context, id string, force bool) devtunnel.ResourceDetail {
	b.devCacheMu.Lock()
	defer b.devCacheMu.Unlock()
	path := b.devCachePath(ctx)
	snapshot := readProviderSnapshot(path)
	if !force && path != "" {
		for _, item := range snapshot.Dev {
			if devtunnel.SameTunnelID(item.ID, id) && item.PortsKnown && cacheFresh(snapshot.ResourceAt[item.ID]) {
				return devtunnel.ResourceDetail{State: devtunnel.ResourceReady, Resource: item}
			}
		}
	}
	result := b.dev.GetResource(ctx, id)
	if result.State == devtunnel.ResourceReady {
		snapshot.Dev = upsertDevResource(snapshot.Dev, result.Resource)
		snapshot.markResource(result.Resource.ID)
		writeProviderSnapshot(path, snapshot)
	}
	return result
}

func upsertDevResource(items []devtunnel.Resource, item devtunnel.Resource) []devtunnel.Resource {
	for i := range items {
		if devtunnel.SameTunnelID(items[i].ID, item.ID) {
			items[i] = item
			return items
		}
	}
	return append(items, item)
}

func upsertCloudflareResource(items []cloudflared.Resource, item cloudflared.Resource) []cloudflared.Resource {
	for i := range items {
		if cloudflared.SameTunnelID(items[i].ID, item.ID) {
			items[i] = item
			return items
		}
	}
	return append(items, item)
}

func (b *TunnelBiz) ListCloudflareResourcesCached(ctx context.Context, force bool) cloudflared.ResourceList {
	b.cloudflareCacheMu.Lock()
	defer b.cloudflareCacheMu.Unlock()
	path := b.cloudflareCachePath()
	snapshot := readProviderSnapshot(path)
	if !force && path != "" && cacheFresh(snapshot.ListedAt) {
		for i := range snapshot.Cloudflare {
			snapshot.Cloudflare[i].CredentialReady = b.cloudflare.CredentialExists(snapshot.Cloudflare[i].ID)
			snapshot.Cloudflare[i].Cached = true
		}
		return cloudflared.ResourceList{State: cloudflared.ResourceReady, Items: snapshot.Cloudflare}
	}
	result := b.cloudflare.ListResources(ctx)
	if result.State == cloudflared.ResourceReady {
		snapshot.Cloudflare = append([]cloudflared.Resource{}, result.Items...)
		snapshot.ResourceAt = make(map[string]time.Time)
		for _, item := range snapshot.Cloudflare {
			snapshot.ResourceAt[item.ID] = time.Now()
		}
		snapshot.ListedAt = time.Now()
		writeProviderSnapshot(path, snapshot)
	}
	return result
}

func (b *TunnelBiz) GetCloudflareResourceCached(ctx context.Context, id string, force bool) cloudflared.ResourceDetail {
	if !force {
		b.cloudflareCacheMu.Lock()
		path := b.cloudflareCachePath()
		snapshot := readProviderSnapshot(path)
		for _, item := range snapshot.Cloudflare {
			if path != "" && cloudflared.SameTunnelID(item.ID, id) && cacheFresh(snapshot.ResourceAt[item.ID]) {
				item.CredentialReady = b.cloudflare.CredentialExists(item.ID)
				item.Cached = true
				b.cloudflareCacheMu.Unlock()
				return cloudflared.ResourceDetail{State: cloudflared.ResourceReady, Resource: item}
			}
		}
		b.cloudflareCacheMu.Unlock()
	}

	list := b.ListCloudflareResourcesCached(ctx, force)
	if list.State != cloudflared.ResourceReady {
		return cloudflared.ResourceDetail{State: list.State, Detail: list.Detail}
	}
	for _, item := range list.Items {
		if cloudflared.SameTunnelID(item.ID, id) {
			return cloudflared.ResourceDetail{State: cloudflared.ResourceReady, Resource: item}
		}
	}
	return cloudflared.ResourceDetail{State: cloudflared.ResourceNotFound, Detail: "cloudflare tunnel not found"}
}

func (b *TunnelBiz) ListCloudflareZonesCached(ctx context.Context, force bool) cloudflared.ZoneList {
	b.cloudflareCacheMu.Lock()
	defer b.cloudflareCacheMu.Unlock()
	path := b.cloudflareCachePath()
	snapshot := readProviderSnapshot(path)
	if !force && path != "" && cacheFresh(snapshot.ZonesAt) {
		return cloudflared.ZoneList{State: cloudflared.ResourceReady, Items: snapshot.Zones}
	}
	result := b.cloudflare.ListZones(ctx)
	if result.State == cloudflared.ResourceReady {
		snapshot.Zones = result.Items
		snapshot.ZonesAt = time.Now()
		writeProviderSnapshot(path, snapshot)
	}
	return result
}

// Mutation updates are serialized with list queries, so an older query cannot restore deleted metadata.
func (b *TunnelBiz) updateDevResourceCache(resource devtunnel.Resource, deleted bool) {
	b.devCacheMu.Lock()
	defer b.devCacheMu.Unlock()
	snapshotPath := b.providerCachePath("devtunnel", b.devAccount)
	snapshot := readProviderSnapshot(snapshotPath)
	if deleted {
		kept := snapshot.Dev[:0]
		for _, item := range snapshot.Dev {
			if !devtunnel.SameTunnelID(item.ID, resource.ID) {
				kept = append(kept, item)
			}
		}
		snapshot.Dev = kept
		delete(snapshot.ResourceAt, resource.ID)
	} else {
		snapshot.Dev = upsertDevResource(snapshot.Dev, resource)
		snapshot.markResource(resource.ID)
	}
	writeProviderSnapshot(snapshotPath, snapshot)
}

func (b *TunnelBiz) updateCloudflareResourceCache(resource cloudflared.Resource, deleted bool) {
	b.cloudflareCacheMu.Lock()
	defer b.cloudflareCacheMu.Unlock()
	path := b.cloudflareCachePath()
	snapshot := readProviderSnapshot(path)
	if deleted {
		kept := snapshot.Cloudflare[:0]
		for _, item := range snapshot.Cloudflare {
			if !cloudflared.SameTunnelID(item.ID, resource.ID) {
				kept = append(kept, item)
			}
		}
		snapshot.Cloudflare = kept
		delete(snapshot.ResourceAt, resource.ID)
	} else {
		snapshot.Cloudflare = upsertCloudflareResource(snapshot.Cloudflare, resource)
		snapshot.markResource(resource.ID)
	}
	writeProviderSnapshot(path, snapshot)
}

func (b *TunnelBiz) rememberDevAccount(status devtunnel.Status) {
	b.devCacheMu.Lock()
	defer b.devCacheMu.Unlock()
	b.devAccountKnown = true
	b.devAccount = ""
	if strings.EqualFold(status.AccountState, "signed_in") {
		b.devAccount = status.AccountLabel
	}
}

func (s *providerSnapshot) markResource(id string) {
	if s.ResourceAt == nil {
		s.ResourceAt = make(map[string]time.Time)
	}
	s.ResourceAt[id] = time.Now()
}
