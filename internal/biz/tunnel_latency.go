package biz

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"loris-tunnel/internal/model"
)

// Latency is sampled independently of runtime readiness and never changes it.
type publicLatency struct {
	run     tunnelRuntime
	cancel  context.CancelFunc
	url     string
	state   string
	ms      int64
	checked string
	code    int
	detail  string
}

type latencyResult struct {
	state   string
	ms      int64
	code    int
	detail  string
	checked string
}

func latencyTarget(raw string) string {
	if target := preferredProbeURL(raw); target != "" {
		return target
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "://") {
		return ""
	}
	host, port, err := net.SplitHostPort(raw)
	if err != nil || host == "" || port == "" {
		return ""
	}
	return net.JoinHostPort(host, port)
}

func preferredProbeURL(raw string) string {
	var first string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		u, err := url.Parse(part)
		if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
			continue
		}
		if first == "" {
			first = part
		}
		if u.Scheme == "https" && (u.Port() == "" || u.Port() == "443") {
			return part
		}
	}
	return first
}

func sameProbeOrigin(a, b *url.URL) bool {
	port := func(u *url.URL) string {
		if u.Port() != "" {
			return u.Port()
		}
		if u.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b)
}

func measurePublicLatency(ctx context.Context, transport http.RoundTripper, target string, timeout time.Duration) latencyResult {
	result := latencyResult{state: "failed", checked: time.Now().UTC().Format(time.RFC3339)}
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		result.detail = "Invalid public URL"
		return result
	}
	if req.URL.User != nil {
		result.detail = "Public URL must not contain credentials"
		return result
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(next *http.Request, via []*http.Request) error {
		if next.URL.User != nil || !sameProbeOrigin(via[0].URL, next.URL) {
			return http.ErrUseLastResponse
		}
		if len(via) > 3 {
			return errors.New("too many redirects")
		}
		return nil
	}}
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	result.checked = time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.state = "timeout"
			result.detail = "Public request timed out"
		} else {
			result.detail = "Public request failed"
		}
		return result
	}
	defer resp.Body.Close()
	result.code = resp.StatusCode
	result.state = "ok"
	result.ms = max(1, elapsed.Milliseconds())
	return result
}

func measureTCPLatency(ctx context.Context, target string, timeout time.Duration) latencyResult {
	result := latencyResult{state: "failed", checked: time.Now().UTC().Format(time.RFC3339)}
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", target)
	elapsed := time.Since(start)
	result.checked = time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.state = "timeout"
			result.detail = "Public connection timed out"
		} else {
			result.detail = "Public connection failed"
		}
		return result
	}
	_ = conn.Close()
	result.state = "ok"
	result.ms = max(1, elapsed.Milliseconds())
	return result
}

func (b *TunnelBiz) startPublicLatency(t model.Tunnel, run tunnelRuntime) {
	provider := t.ProviderName()
	if provider != model.ProviderDevTunnel && provider != model.ProviderCloudflare && provider != model.ProviderFRP {
		return
	}
	source, ok := run.(urlRuntime)
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	sample := &publicLatency{run: run, cancel: cancel, state: "measuring"}
	b.mu.Lock()
	if b.runs[t.ID] != run {
		b.mu.Unlock()
		cancel()
		return
	}
	if old := b.publicLatency[t.ID]; old != nil {
		old.cancel()
	}
	b.publicLatency[t.ID] = sample
	b.probeWG.Add(1)
	b.mu.Unlock()
	go func() {
		defer b.probeWG.Done()
		defer cancel()
		transport := http.DefaultTransport.(*http.Transport).Clone()
		defer transport.CloseIdleConnections()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		var pending chan latencyResult
		var requestCancel context.CancelFunc
		var requestedURL string
		var next time.Time
		defer func() {
			if requestCancel != nil {
				requestCancel()
				<-pending
			}
		}()
		for {
			target := latencyTarget(source.PublicURL())
			private := t.ProviderName() == model.ProviderDevTunnel && t.DevTunnel != nil && t.DevTunnel.Access != "public"
			b.mu.Lock()
			if b.runs[t.ID] != run || b.publicLatency[t.ID] != sample {
				b.mu.Unlock()
				return
			}
			if target != sample.url {
				sample.url = target
				sample.state = "measuring"
				sample.ms = 0
				sample.checked = ""
				sample.code = 0
				sample.detail = ""
				next = time.Time{}
				if requestCancel != nil {
					requestCancel()
				}
			}
			if private {
				sample.state = "auth_required"
				sample.ms = 0
			}
			if !private && target != "" && pending == nil && !time.Now().Before(next) {
				requestCtx, stop := context.WithCancel(ctx)
				requestCancel = stop
				requestedURL = target
				pending = make(chan latencyResult, 1)
				output := pending
				go func() {
					if strings.Contains(target, "://") {
						output <- measurePublicLatency(requestCtx, transport, target, 5*time.Second)
					} else {
						output <- measureTCPLatency(requestCtx, target, 5*time.Second)
					}
				}()
			}
			b.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-run.Done():
				return
			case result := <-pending:
				requestCancel()
				requestCancel = nil
				pending = nil
				b.mu.Lock()
				if b.runs[t.ID] == run && b.publicLatency[t.ID] == sample && sample.url == requestedURL && latencyTarget(source.PublicURL()) == requestedURL {
					sample.state = result.state
					sample.ms = result.ms
					sample.checked = result.checked
					sample.code = result.code
					sample.detail = result.detail
					next = time.Now().Add(30 * time.Second)
				}
				b.mu.Unlock()
			case <-tick.C:
			}
		}
	}()
}

// Caller holds b.mu. Canceling is non-blocking; the worker drains its request.
func (b *TunnelBiz) cancelPublicLatency(id int) {
	if sample := b.publicLatency[id]; sample != nil {
		sample.cancel()
		delete(b.publicLatency, id)
	}
}

func (b *TunnelBiz) attachPublicLatency(t *model.Tunnel, run tunnelRuntime) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t.LatencyMs = 0
	t.LatencyState = "measuring"
	sample := b.publicLatency[t.ID]
	if sample == nil || sample.run != run || b.runs[t.ID] != run {
		return
	}
	source, ok := run.(urlRuntime)
	if !ok || sample.url != latencyTarget(source.PublicURL()) {
		return
	}
	t.LatencyMs = sample.ms
	t.LatencyState = sample.state
	t.LatencyCheckedAt = sample.checked
	t.LatencyHTTPStatus = sample.code
	t.LatencyDetail = sample.detail
}
