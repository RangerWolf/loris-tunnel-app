package biz

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"loris-tunnel/internal/cloudflared"
	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/devtunnel"
)

func metadataCacheBiz(t *testing.T, dir string) *TunnelBiz {
	t.Helper()
	storage, err := conf.NewStorage(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	b := NewTunnelBiz(storage)
	b.rememberDevAccount(devtunnel.Status{AccountState: "signed_in", AccountLabel: "alice/github"})
	return b
}

func writeCacheTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0700); err != nil {
		t.Fatal(err)
	}
}

func TestProviderMetadataCacheDevPersistenceRefreshAndFailure(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	binary := filepath.Join(dir, "devtunnel")
	data := filepath.Join(dir, "response.json")
	calls := filepath.Join(dir, "calls")
	writeCacheTestFile(t, data, `[{"tunnelId":"one","clusterId":"usw3","ports":[],"allowAnonymous":true,"hostConnections":5}]`)
	writeCacheTestFile(t, binary, "#!/bin/sh\necho call >> '"+calls+"'\ncat '"+data+"'\n")
	b := metadataCacheBiz(t, dir)
	b.dev = devtunnel.NewClient(binary)
	var pending sync.WaitGroup
	results := make(chan devtunnel.ResourceList, 8)
	for range 8 {
		pending.Add(1)
		go func() { defer pending.Done(); results <- b.ListDevTunnelResourcesCached(ctx, false) }()
	}
	pending.Wait()
	first := <-results
	if first.State != "ready" || len(first.Items) != 1 || first.Items[0].Access != "public" {
		t.Fatalf("first: %+v", first)
	}
	// New business/client instances simulate an app restart; CLI is now unavailable.
	restarted := metadataCacheBiz(t, dir)
	restarted.dev = devtunnel.NewClient(filepath.Join(dir, "missing-cli"))
	list := restarted.ListDevTunnelResourcesCached(ctx, false)
	detail := restarted.GetDevTunnelResourceCached(ctx, "one", false)
	if list.State != "ready" || len(list.Items) != 1 || detail.Resource.Access != "public" || detail.Resource.HostConnections != 0 {
		t.Fatalf("disk cache: %+v %+v", list, detail)
	}
	raw, _ := os.ReadFile(calls)
	if strings.Count(string(raw), "call") != 1 {
		t.Fatalf("unnecessary CLI calls: %s", raw)
	}
	path := b.providerCachePath("devtunnel", "alice/github")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v %v", info, err)
	}
	// A failed manual refresh must be visible and must not overwrite confirmed metadata.
	if failed := restarted.ListDevTunnelResourcesCached(ctx, true); failed.State == "ready" {
		t.Fatal("refresh faked success")
	}
	if cached := restarted.ListDevTunnelResourcesCached(ctx, false); cached.State != "ready" {
		t.Fatal(cached)
	}
	writeCacheTestFile(t, data, `[{"tunnelId":"one","clusterId":"usw3","ports":[],"allowAnonymous":false}]`)
	refreshed := b.ListDevTunnelResourcesCached(ctx, true)
	if refreshed.Items[0].Access != "private" {
		t.Fatal(refreshed)
	}
	if got := restarted.GetDevTunnelResourceCached(ctx, "one", false); got.Resource.Access != "private" {
		t.Fatal(got)
	}
	// Account changes cannot reuse Alice's resources.
	restarted.rememberDevAccount(devtunnel.Status{AccountState: "signed_in", AccountLabel: "bob/github"})
	if got := restarted.ListDevTunnelResourcesCached(ctx, false); got.State == "ready" {
		t.Fatal("account cache leaked", got)
	}
}

func TestProviderMetadataCacheExpiryCorruptionAndMutations(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := metadataCacheBiz(t, dir)
	b.dev = devtunnel.NewClient(filepath.Join(dir, "missing-cli"))
	path := b.providerCachePath("devtunnel", "alice/github")
	snapshot := providerSnapshot{Version: providerCacheVersion, ListedAt: time.Now(), Dev: []devtunnel.Resource{{ID: "one.usw3", Access: "private", PortsKnown: true}}}
	snapshot.markResource("one.usw3")
	if err := saveProviderSnapshot(path, snapshot); err != nil {
		t.Fatal(err)
	}
	b.updateDevResourceCache(devtunnel.Resource{ID: "two.usw3", Access: "public", PortsKnown: true}, false)
	if got := b.ListDevTunnelResourcesCached(ctx, false); len(got.Items) != 2 {
		t.Fatal(got)
	}
	b.updateDevResourceCache(devtunnel.Resource{ID: "one"}, true)
	got := b.ListDevTunnelResourcesCached(ctx, false)
	if len(got.Items) != 1 || got.Items[0].ID != "two.usw3" {
		t.Fatal(got)
	}
	snapshot = readProviderSnapshot(path)
	snapshot.ListedAt = time.Now().Add(-25 * time.Hour)
	if err := saveProviderSnapshot(path, snapshot); err != nil {
		t.Fatal(err)
	}
	if got := b.ListDevTunnelResourcesCached(ctx, false); got.State == "ready" {
		t.Fatal("expired cache used", got)
	}
	writeCacheTestFile(t, path, "broken-json")
	if got := b.ListDevTunnelResourcesCached(ctx, false); got.State == "ready" {
		t.Fatal("corrupt cache used", got)
	}
	// Creating the first resource caches its details without pretending to have a full account list.
	b.updateDevResourceCache(devtunnel.Resource{ID: "new.usw3", Access: "private", PortsKnown: true}, false)
	if got := b.GetDevTunnelResourceCached(ctx, "new", false); got.State != "ready" {
		t.Fatal(got)
	}
	if got := b.ListDevTunnelResourcesCached(ctx, false); got.State == "ready" {
		t.Fatal("partial list treated as complete", got)
	}
}

func TestProviderMetadataCacheCloudflareAndZones(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := metadataCacheBiz(t, dir)
	b.cloudflare = cloudflared.NewClient(filepath.Join(dir, "missing-cli"))
	b.cloudflare.ConfigHome = dir
	cert := filepath.Join(dir, "cert.pem")
	writeCacheTestFile(t, cert, "account-a-secret-certificate")
	path := b.cloudflareCachePath()
	snapshot := providerSnapshot{Version: providerCacheVersion, ListedAt: time.Now(), ZonesAt: time.Now(), Cloudflare: []cloudflared.Resource{{ID: cloudflareUUID, Name: "web", CreatedAt: "2026-01-01", Connections: 3, CredentialReady: true}}, Zones: []cloudflared.Zone{{ID: "zone", Name: "example.com"}}}
	snapshot.markResource(cloudflareUUID)
	if err := saveProviderSnapshot(path, snapshot); err != nil {
		t.Fatal(err)
	}
	list := b.ListCloudflareResourcesCached(ctx, false)
	if list.State != "ready" || list.Items[0].Name != "web" || !list.Items[0].Cached || list.Items[0].Connections != 0 || list.Items[0].CredentialReady {
		t.Fatal(list)
	}
	// Credential readiness is rechecked locally rather than trusting the disk snapshot.
	writeCacheTestFile(t, filepath.Join(dir, cloudflareUUID+".json"), "{}")
	detail := b.GetCloudflareResourceCached(ctx, cloudflareUUID, false)
	if detail.State != "ready" || !detail.Resource.CredentialReady {
		t.Fatal(detail)
	}
	if zones := b.ListCloudflareZonesCached(ctx, false); zones.State != "ready" || zones.Items[0].Name != "example.com" {
		t.Fatal(zones)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "secret-certificate") {
		t.Fatal("certificate copied to metadata cache")
	}
	b.updateCloudflareResourceCache(cloudflared.Resource{ID: cloudflareUUID}, true)
	if list := b.ListCloudflareResourcesCached(ctx, false); len(list.Items) != 0 {
		t.Fatal(list)
	}
	if zones := b.ListCloudflareZonesCached(ctx, false); len(zones.Items) != 1 {
		t.Fatal("deleting a tunnel removed zones", zones)
	}
	writeCacheTestFile(t, cert, "account-b-secret-certificate")
	if b.cloudflareCachePath() == path {
		t.Fatal("certificate change did not isolate account")
	}
	if got := b.ListCloudflareResourcesCached(ctx, false); got.State == "ready" {
		t.Fatal("account cache leaked", got)
	}
}
