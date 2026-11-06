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

func TestDevServiceRejectsSecondMemberWithoutRestartingFirst(t *testing.T) {
	b, hosts := fakeServiceBiz(0)
	first, second := serviceRuntime(b, 1, 8080, 8080), serviceRuntime(b, 2, 9090, 9090)
	if err := b.startDevService(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if err := b.startDevService(t.Context(), second); err == nil {
		t.Fatal("second service joined exclusive ID")
	}
	assertServiceOpen(t, first)
	if len(*hosts) != 1 {
		t.Fatal("first service was restarted")
	}
	if first.PublicURL() != "https://abc-8080.usw3.devtunnels.ms" {
		t.Fatal(first.PublicURL())
	}
	if err := first.Stop(); err != nil {
		t.Fatal(err)
	}
	if len(b.devSessions) != 0 {
		t.Fatal("session retained after stop")
	}
}

func TestDevServiceFailedStartCanRetry(t *testing.T) {
	b, hosts := fakeServiceBiz(1)
	if err := b.startDevService(t.Context(), serviceRuntime(b, 1, 8080, 8080)); err == nil {
		t.Fatal("expected host failure")
	}
	retry := serviceRuntime(b, 1, 8080, 8080)
	if err := b.startDevService(t.Context(), retry); err != nil {
		t.Fatal(err)
	}
	if len(*hosts) != 2 {
		t.Fatal("retry did not create a host")
	}
	_ = retry.Stop()
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

func TestDevServiceBindingExclusivelyOwnsID(t *testing.T) {
	b := metadataCacheBiz(t, t.TempDir())
	payload := func(name, id, host, protocol string) model.TunnelPayload {
		return model.TunnelPayload{Name: name, Provider: model.ProviderDevTunnel, Mode: protocol, LocalHost: host, LocalPort: 8080, Status: "stopped", DevTunnelIDMode: "bind", DevTunnel: &model.DevTunnelConfig{TunnelID: id, Access: "private"}}
	}
	first, err := b.Create(payload("first", "abc.usw3", "127.0.0.1", "http"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []model.TunnelPayload{payload("protocol", "ABC", "127.0.0.1", "https"), payload("host", "abc.usw3", "127.0.0.2", "auto")} {
		if _, err := b.Create(p); err == nil {
			t.Fatal("duplicate ID accepted", p)
		}
	}
	p := payload("port", "abc.usw3", "127.0.0.1", "http")
	p.LocalPort = 7676
	if _, err := b.Create(p); err == nil {
		t.Fatal("different port bypassed exclusive binding")
	}
	if _, err := b.Create(payload("other", "other.usw3", "127.0.0.1", "http")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Update(first.ID, payload("first", "abc.usw3", "127.0.0.1", "https")); err != nil {
		t.Fatal(err)
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

func TestDevSessionReconnectDiagnosticsAndURLSnapshot(t *testing.T) {
	b, hosts := fakeServiceBiz(0)
	r := serviceRuntime(b, 1, 8080, 8080)
	if err := b.startDevService(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	<-r.Events() // Initial session readiness.
	host := (*hosts)[0]
	host.events <- forward.RuntimeEvent{Type: forward.RuntimeEventDisconnected, Err: fmt.Errorf("disconnected"), Reconnect: &model.ReconnectInfo{Kind: "host_conflict", Attempt: 2, HostIDs: []string{"peer"}}}
	select {
	case <-r.Events():
	case <-time.After(time.Second):
		t.Fatal("no disconnect event")
	}
	info := r.ReconnectInfo()
	if info == nil || info.Attempt != 2 {
		t.Fatalf("missing diagnostics: %+v", info)
	}
	info.HostIDs[0] = "mutated"
	if r.ReconnectInfo().HostIDs[0] != "peer" {
		t.Fatal("diagnostics escaped as shared mutable state")
	}
	// The event carries the URL for that attempt, independent of the host's
	// current URL, which may already belong to a later disconnected attempt.
	host.events <- forward.RuntimeEvent{Type: forward.RuntimeEventReconnected, PublicURL: "https://new-8080.usw3.devtunnels.ms"}
	select {
	case <-r.Events():
	case <-time.After(time.Second):
		t.Fatal("no recovery event")
	}
	if r.PublicURL() != "https://new-8080.usw3.devtunnels.ms" || r.ReconnectInfo() != nil {
		t.Fatalf("stale recovery state: %s %+v", r.PublicURL(), r.ReconnectInfo())
	}
	host.events <- forward.RuntimeEvent{Type: forward.RuntimeEventReconnected, PublicURL: "https://new-8080.usw3.devtunnels.ms, https://new-9090.usw3.devtunnels.ms"}
	select {
	case <-r.Done():
		if r.Err() == nil {
			t.Fatal("extra port accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("unsafe recovery not rejected")
	}
}
