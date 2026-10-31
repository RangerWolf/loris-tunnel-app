package biz

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"loris-tunnel/internal/devtunnel"
	"loris-tunnel/internal/forward"
	"loris-tunnel/internal/model"
)

type fakeDevServiceHost struct {
	targets  []devtunnel.ServiceTarget
	done     chan struct{}
	events   chan forward.RuntimeEvent
	once     sync.Once
	err      error
	startErr error
}

func (h *fakeDevServiceHost) Start() error                        { return h.startErr }
func (h *fakeDevServiceHost) Stop() error                         { h.once.Do(func() { close(h.done) }); return nil }
func (h *fakeDevServiceHost) Done() <-chan struct{}               { return h.done }
func (h *fakeDevServiceHost) Events() <-chan forward.RuntimeEvent { return h.events }
func (h *fakeDevServiceHost) Err() error                          { return h.err }
func (h *fakeDevServiceHost) URL() string {
	var values []string
	for _, target := range h.targets {
		values = append(values, fmt.Sprintf("https://abc-%d.usw3.devtunnels.ms", target.Port))
	}
	return strings.Join(values, ", ")
}
func serviceRuntime(b *TunnelBiz, id, published, target int) *devServiceRuntime {
	return &devServiceRuntime{biz: b, key: "abc.usw3", id: id, target: devtunnel.ServiceTarget{Host: fmt.Sprintf("127.0.0.%d", id), Port: published, LocalPort: target}, done: make(chan struct{}), events: make(chan forward.RuntimeEvent, 16)}
}
func fakeServiceBiz(failAttempt int) (*TunnelBiz, *[]*fakeDevServiceHost) {
	b := &TunnelBiz{}
	var hosts []*fakeDevServiceHost
	b.newDevServiceHost = func(_ string, targets []devtunnel.ServiceTarget) devServiceHost {
		host := &fakeDevServiceHost{targets: append([]devtunnel.ServiceTarget{}, targets...), done: make(chan struct{}), events: make(chan forward.RuntimeEvent, 16)}
		hosts = append(hosts, host)
		if len(hosts) == failAttempt {
			host.startErr = fmt.Errorf("CLI startup failed")
		}
		return host
	}
	return b, &hosts
}
func assertServiceOpen(t *testing.T, r *devServiceRuntime) {
	t.Helper()
	select {
	case <-r.Done():
		t.Fatalf("service closed unexpectedly: %v", r.Err())
	default:
	}
}

func TestSharedDevServicesIndependentStopAndURLs(t *testing.T) {
	b, hosts := fakeServiceBiz(0)
	first, second := serviceRuntime(b, 1, 8080, 8080), serviceRuntime(b, 2, 8081, 8080)
	if err := b.startDevService(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if err := b.startDevService(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	if len(*hosts) != 2 || len((*hosts)[1].targets) != 2 {
		t.Fatal("ports were not combined")
	}
	if first.PublicURL() != "https://abc-8080.usw3.devtunnels.ms" || second.PublicURL() != "https://abc-8081.usw3.devtunnels.ms" {
		t.Fatal(first.PublicURL(), second.PublicURL())
	}
	if err := first.Stop(); err != nil {
		t.Fatal(err)
	}
	assertServiceOpen(t, second)
	if targets := (*hosts)[2].targets; len(targets) != 1 || targets[0].Port != 8081 || targets[0].LocalPort != 8080 {
		t.Fatal(targets)
	}
	if err := second.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := second.Stop(); err != nil {
		t.Fatal(err)
	}
	if len(b.devSessions) != 0 {
		t.Fatal("shared process retained after last stop")
	}
}

func TestSharedDevServicesFailedJoinRestoresExistingService(t *testing.T) {
	b, hosts := fakeServiceBiz(2)
	first, second := serviceRuntime(b, 1, 8080, 8080), serviceRuntime(b, 2, 8081, 8081)
	if err := b.startDevService(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if err := b.startDevService(t.Context(), second); err == nil {
		t.Fatal("failed join reported success")
	}
	assertServiceOpen(t, first)
	if len(*hosts) != 3 || len((*hosts)[2].targets) != 1 || (*hosts)[2].targets[0].Port != 8080 {
		t.Fatal("original service not restored")
	}
	if second.Err() == nil {
		t.Fatal("failed service has no error")
	}
	_ = first.Stop()
}

func TestSharedDevServicesFailedStopNeverRestoresPausedPort(t *testing.T) {
	b, hosts := fakeServiceBiz(3)
	first, second := serviceRuntime(b, 1, 8080, 8080), serviceRuntime(b, 2, 8081, 8081)
	if err := b.startDevService(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if err := b.startDevService(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	if err := first.Stop(); err == nil {
		t.Fatal("failed restart hidden")
	}
	if len((*hosts)[2].targets) != 1 || (*hosts)[2].targets[0].Port != 8081 {
		t.Fatal("paused port was restored")
	}
	if second.Err() == nil {
		t.Fatal("surviving row should report shared process failure")
	}
}

func TestSharedDevServicesCancellationAndProcessFailure(t *testing.T) {
	b, hosts := fakeServiceBiz(0)
	canceled := serviceRuntime(b, 1, 8080, 8080)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := b.startDevService(ctx, canceled); err == nil {
		t.Fatal("canceled service started")
	}
	first := serviceRuntime(b, 2, 8081, 8081)
	if err := b.startDevService(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	host := (*hosts)[0]
	host.err = fmt.Errorf("CLI exited")
	_ = host.Stop()
	select {
	case <-first.Done():
	case <-time.After(time.Second):
		t.Fatal("shared exit was not propagated")
	}
	if first.Err() == nil {
		t.Fatal("shared error lost")
	}
}

func TestDevServiceBindingIgnoresProtocolButDistinguishesHostsAndIDs(t *testing.T) {
	b := metadataCacheBiz(t, t.TempDir())
	payload := func(name, id, host, protocol string) model.TunnelPayload {
		return model.TunnelPayload{Name: name, Provider: model.ProviderDevTunnel, Mode: protocol, LocalHost: host, LocalPort: 8080, Status: "stopped", DevTunnelIDMode: "bind", DevTunnel: &model.DevTunnelConfig{TunnelID: id, Access: "private"}}
	}
	first, err := b.Create(payload("first", "abc.usw3", "127.0.0.1", "http"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Create(payload("duplicate", "abc", "127.0.0.1", "https")); err == nil {
		t.Fatal("protocol bypassed duplicate service check")
	}
	second, err := b.Create(payload("second", "abc.usw3", "127.0.0.2", "https"))
	if err != nil {
		t.Fatal("different host rejected", err)
	}
	if _, err = b.Create(payload("other-id", "other.usw3", "127.0.0.1", "https")); err != nil {
		t.Fatal("different ID rejected", err)
	}
	if _, err = b.Update(first.ID, payload("first", "abc.usw3", "127.0.0.1", "https")); err != nil {
		t.Fatal("self edit rejected", err)
	}
	a, err := b.allocateDevTunnelPort(first.ID, "abc.usw3", "127.0.0.1", 8080)
	if err != nil {
		t.Fatal(err)
	}
	c, err := b.allocateDevTunnelPort(second.ID, "abc.usw3", "127.0.0.2", 8080)
	if err != nil {
		t.Fatal(err)
	}
	if a == c {
		t.Fatal("two targets received the same public port")
	}
	again, err := b.allocateDevTunnelPort(first.ID, "abc.usw3", "127.0.0.1", 8080)
	if err != nil || a != again {
		t.Fatal("public port was not stable", a, again, err)
	}
}

func TestCloudflareHostnameDuplicateIgnoresCaseAndExcludesSelf(t *testing.T) {
	b := metadataCacheBiz(t, t.TempDir())
	payload := func(name, id, host string) model.TunnelPayload {
		return model.TunnelPayload{Name: name, Provider: model.ProviderCloudflare, Mode: "http", LocalHost: "127.0.0.1", LocalPort: 3000, Status: "stopped", CloudflareIDMode: "bind", Cloudflare: &model.CloudflareTunnelConfig{TunnelID: id, Hostname: host}}
	}
	first, err := b.Create(payload("first", cloudflareUUID, "dev.example.com"))
	if err != nil {
		t.Fatal(err)
	}
	other := "13e12345-6789-4abc-8def-0123456789ab"
	if _, err = b.Create(payload("dup", other, "HTTPS://DEV.EXAMPLE.COM/")); err == nil {
		t.Fatal("duplicate domain accepted")
	}
	if _, err = b.Create(payload("different-domain", other, "dev.other.example.com")); err != nil {
		t.Fatal("different complete hostname rejected", err)
	}
	if _, err = b.Update(first.ID, payload("first", cloudflareUUID, "dev.example.com")); err != nil {
		t.Fatal("self edit rejected", err)
	}
}
