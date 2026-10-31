package devtunnel

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const regionCacheTTL = 5 * time.Minute

var clusterIDPattern = regexp.MustCompile(`^[a-z0-9]{2,16}$`)

// Region is one Microsoft cluster the UI can offer. URI stays on the server.
type Region struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	LatencyMs *int   `json:"latencyMs"`
	Status    string `json:"status"`
}

// RegionProbe is a cluster list, with latency when a probe has completed.
type RegionProbe struct {
	State      string   `json:"state"`
	Detail     string   `json:"detail"`
	Regions    []Region `json:"regions"`
	MeasuredAt string   `json:"measuredAt"`
	Cached     bool     `json:"cached"`
}

type clusterRecord struct {
	ID      string
	Name    string
	URI     string
	Latency *int
	Status  string
}

type regionFlight struct {
	done  chan struct{}
	probe RegionProbe
}

// NormalizeRegion accepts auto or a cluster id. Anything else is rejected.
func NormalizeRegion(region string) (string, error) {
	region = strings.ToLower(strings.TrimSpace(region))
	if region == "" || region == "auto" {
		return "", nil
	}
	if !clusterIDPattern.MatchString(region) {
		return "", fmt.Errorf("unsupported dev tunnel region")
	}
	return region, nil
}

// LoadRegions returns the cluster catalog. measure runs `clusters --ping`.
// force skips the 5 minute cache. Concurrent callers share one CLI process.
func (c *Client) LoadRegions(ctx context.Context, measure, force bool) RegionProbe {
	key := "list"
	if measure {
		key = "ping"
	}
	c.regionMu.Lock()
	if probe, ok := c.lookupCached(measure, force); ok {
		c.regionMu.Unlock()
		return probe
	}
	if c.regionFlight == nil {
		c.regionFlight = map[string]*regionFlight{}
	}
	if call, ok := c.regionFlight[key]; ok {
		c.regionMu.Unlock()
		<-call.done
		return call.probe
	}
	call := &regionFlight{done: make(chan struct{})}
	c.regionFlight[key] = call
	c.regionMu.Unlock()

	probe, records := c.queryRegions(ctx, measure)
	c.regionMu.Lock()
	probe = c.storeRegions(measure, probe, records)
	call.probe = probe
	delete(c.regionFlight, key)
	close(call.done)
	c.regionMu.Unlock()
	return probe
}

func (c *Client) lookupCached(measure, force bool) (RegionProbe, bool) {
	if measure {
		if force || c.regionProbedAt.IsZero() || time.Since(c.regionProbedAt) >= regionCacheTTL || len(c.regionProbe.Regions) == 0 {
			return RegionProbe{}, false
		}
		probe := c.regionProbe
		probe.Cached = true
		return probe, true
	}
	if force || c.regionClustersAt.IsZero() || time.Since(c.regionClustersAt) >= regionCacheTTL || len(c.regionClusters) == 0 {
		return RegionProbe{}, false
	}
	if !c.regionProbedAt.IsZero() && time.Since(c.regionProbedAt) < regionCacheTTL && len(c.regionProbe.Regions) > 0 {
		probe := c.regionProbe
		probe.Cached = true
		return probe, true
	}
	return probeFrom(c.regionClusters, false, false), true
}

func (c *Client) storeRegions(measure bool, probe RegionProbe, records []clusterRecord) RegionProbe {
	if len(records) > 0 {
		c.regionClusters = records
		c.regionClustersAt = time.Now()
		if measure {
			c.regionProbe = probe
			c.regionProbedAt = time.Now()
		} else if !c.regionProbedAt.IsZero() && time.Since(c.regionProbedAt) < regionCacheTTL && len(c.regionProbe.Regions) > 0 {
			return c.regionProbe
		}
		return probe
	}
	if len(c.regionClusters) == 0 {
		if probe.Regions == nil {
			probe.Regions = []Region{}
		}
		return probe
	}
	kept := probeFrom(c.regionClusters, false, false)
	kept.State = "error"
	kept.Detail = probe.Detail
	if measure {
		for i := range kept.Regions {
			kept.Regions[i].Status = "error"
			kept.Regions[i].LatencyMs = nil
		}
	}
	return kept
}

func (c *Client) queryRegions(ctx context.Context, measure bool) (RegionProbe, []clusterRecord) {
	path, _, err := c.resolve()
	if err != nil {
		return RegionProbe{State: "error", Detail: "dev tunnel cli is not installed", Regions: []Region{}}, nil
	}
	args := []string{"clusters", "-j"}
	if measure {
		args = []string{"clusters", "--ping", "-j"}
	}
	started := time.Now()
	out, err := c.run(ctx, path, args...)
	elapsed := time.Since(started)
	if err != nil {
		detail := clip(strings.TrimSpace(out+" "+err.Error()), 240)
		slog.Info("dev tunnel region probe", "measure", measure, "ms", elapsed.Milliseconds(), "ok", false, "err", detail)
		return RegionProbe{State: "error", Detail: detail, Regions: []Region{}}, nil
	}
	records, parseErr := parseClusterOutput(out, measure)
	if parseErr != nil {
		slog.Info("dev tunnel region probe", "measure", measure, "ms", elapsed.Milliseconds(), "ok", false, "err", parseErr.Error())
		return RegionProbe{State: "error", Detail: parseErr.Error(), Regions: []Region{}}, nil
	}
	probe := probeFrom(records, measure, false)
	if measure && probe.State == "error" {
		probe.Detail = "dev tunnel latency probe failed"
	}
	slog.Info("dev tunnel region probe", "measure", measure, "ms", elapsed.Milliseconds(), "ok", true, "state", probe.State, "regions", len(probe.Regions))
	return probe, records
}

func (c *Client) serviceURI(ctx context.Context, region string) (string, error) {
	region, err := NormalizeRegion(region)
	if err != nil {
		return "", err
	}
	if region == "" {
		return "", fmt.Errorf("unsupported dev tunnel region")
	}
	if uri, ok := c.cachedServiceURI(region); ok {
		return uri, nil
	}
	probe := c.LoadRegions(ctx, false, true)
	if uri, ok := c.cachedServiceURI(region); ok {
		return uri, nil
	}
	if probe.Detail != "" {
		return "", fmt.Errorf("%s", probe.Detail)
	}
	return "", fmt.Errorf("unsupported dev tunnel region")
}

func (c *Client) cachedServiceURI(region string) (string, bool) {
	c.regionMu.Lock()
	defer c.regionMu.Unlock()
	for _, rec := range c.regionClusters {
		if rec.ID == region && rec.URI != "" {
			return rec.URI, true
		}
	}
	return "", false
}

func (c *Client) confirmCreatedRegion(ctx context.Context, id, region string) (string, error) {
	if region == "" {
		return id, nil
	}
	qualified := id
	actual := clusterFromID(id)
	if actual == "" {
		detail := c.GetResource(ctx, id)
		if detail.Resource.ID != "" {
			qualified = detail.Resource.ID
			actual = clusterFromID(qualified)
		}
	}
	if strings.EqualFold(actual, region) {
		return qualified, nil
	}
	if actual == "" {
		actual = "unknown"
	}
	cleanupErr := c.DeleteRemote(context.Background(), qualified)
	if cleanupErr != nil && qualified != id {
		cleanupErr = c.DeleteRemote(context.Background(), id)
	}
	if cleanupErr != nil {
		slog.Warn("dev tunnel region mismatch left a new tunnel", "requested", region, "actual", actual, "err", cleanupErr)
		return "", fmt.Errorf("created in %s instead of %s; the new tunnel could not be removed", actual, region)
	}
	slog.Warn("dev tunnel region mismatch", "requested", region, "actual", actual)
	return "", fmt.Errorf("created in %s instead of %s; the new tunnel was removed", actual, region)
}

func clusterFromID(id string) string {
	_, cluster, ok := strings.Cut(strings.ToLower(strings.TrimSpace(id)), ".")
	if !ok || !clusterIDPattern.MatchString(cluster) {
		return ""
	}
	return cluster
}

func probeFrom(records []clusterRecord, measured, cached bool) RegionProbe {
	regions := make([]Region, 0, len(records))
	okCount := 0
	for _, rec := range records {
		item := Region{ID: rec.ID, Name: rec.Name}
		if measured {
			item.Status = rec.Status
			item.LatencyMs = rec.Latency
			if item.Status == "ok" {
				okCount++
			}
		}
		regions = append(regions, item)
	}
	probe := RegionProbe{Regions: regions, Cached: cached}
	if !measured {
		probe.State = "idle"
		return probe
	}
	probe.MeasuredAt = time.Now().UTC().Format(time.RFC3339)
	switch {
	case okCount == len(regions) && okCount > 0:
		probe.State = "success"
	case okCount == 0:
		probe.State = "error"
	default:
		probe.State = "partial"
	}
	return probe
}

func parseClusterOutput(output string, measured bool) ([]clusterRecord, error) {
	raw, err := decodeCLIJSON(output)
	if err != nil {
		return nil, err
	}
	var nodes []map[string]any
	switch typed := raw.(type) {
	case []any:
		nodes = mapsOnly(typed)
	case map[string]any:
		if list, ok := firstArray(typed, "clusters", "items"); ok {
			nodes = mapsOnly(list)
		}
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("dev tunnel clusters did not return regions")
	}
	records := make([]clusterRecord, 0, len(nodes))
	seen := map[string]bool{}
	for _, node := range nodes {
		id := strings.ToLower(firstString(node, "clusterId", "id"))
		trusted, ok := trustedServiceURI(id, firstString(node, "uri", "serviceUri"))
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		name := regionDisplayName(firstString(node, "azureLocation", "name"))
		if name == "" {
			name = id
		}
		rec := clusterRecord{ID: id, Name: name, URI: trusted}
		if measured {
			rec.Latency, rec.Status = latencyFrom(node)
		}
		records = append(records, rec)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("dev tunnel clusters did not return regions")
	}
	return records, nil
}

func latencyFrom(node map[string]any) (*int, string) {
	raw, ok := node["ping"]
	if !ok || raw == nil {
		return nil, "timeout"
	}
	number, ok := raw.(float64)
	if !ok || number <= 0 || number > 600000 {
		return nil, "timeout"
	}
	ms := int(number)
	return &ms, "ok"
}

func trustedServiceURI(id, raw string) (string, bool) {
	if !clusterIDPattern.MatchString(id) {
		return "", false
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Port() != "" {
		return "", false
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != id+".rel.tunnels.api.visualstudio.com" {
		return "", false
	}
	return "https://" + host, true
}

func regionDisplayName(azure string) string {
	azure = strings.TrimSpace(azure)
	if azure == "" {
		return ""
	}
	var b strings.Builder
	runes := []rune(azure)
	for i, r := range runes {
		if i > 0 {
			prev := runes[i-1]
			if unicode.IsDigit(r) && !unicode.IsDigit(prev) {
				b.WriteByte(' ')
			} else if unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)) {
				b.WriteByte(' ')
			}
		}
		b.WriteRune(r)
	}
	text := strings.ReplaceAll(b.String(), "Us", "US")
	return strings.ReplaceAll(text, "Uk", "UK")
}
