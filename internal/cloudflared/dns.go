package cloudflared

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DNSRecord is one Cloudflare DNS row. Tokens are never copied into it.
type DNSRecord struct {
	ID      string `json:"id"`
	ZoneID  string `json:"zoneId"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

var (
	ErrDNSAbsent     = errors.New("dns record is absent")
	ErrDNSPermission = errors.New("cloudflare dns permission denied")
	ErrDNSNetwork    = errors.New("cloudflare dns could not be reached")
	ErrDNSFailed     = errors.New("cloudflare dns request failed")
)

// TunnelCNAMETarget is the hostname a tunnel CNAME must point at.
func TunnelCNAMETarget(id string) string {
	return strings.ToLower(strings.TrimSpace(id)) + ".cfargotunnel.com"
}

// CNAMEMatches reports a record that still belongs to this tunnel.
func CNAMEMatches(record DNSRecord, tunnelID string) bool {
	if !strings.EqualFold(record.Type, "CNAME") {
		return false
	}
	return strings.EqualFold(strings.TrimSuffix(record.Content, "."), TunnelCNAMETarget(tunnelID))
}

// ListManagedZones reads every zone the current certificate can manage.
func (c *Client) ListManagedZones(ctx context.Context) ([]Zone, error) {
	if c.certFile != "" {
		return c.listManagedZonesSingle(ctx)
	}
	result := c.ListAuthorizations(ctx)
	if result.State != ResourceReady {
		return nil, ErrDNSPermission
	}
	for _, zone := range result.Items {
		if zone.State == ResourceNetwork {
			return nil, ErrDNSNetwork
		}
		if zone.State != ResourceReady {
			return nil, ErrDNSPermission
		}
	}
	return result.Items, nil
}

func (c *Client) listManagedZonesSingle(ctx context.Context) ([]Zone, error) {
	cert, root, err := c.apiSession(ctx)
	if err != nil {
		return nil, err
	}
	var zones []Zone
	for page := 1; page < 50; page++ {
		endpoint := fmt.Sprintf("%s/zones?page=%d&per_page=50", root, page)
		body, status, err := c.apiGet(ctx, cert, endpoint)
		if err != nil {
			return nil, err
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return nil, ErrDNSPermission
		}
		var payload struct {
			Success bool `json:"success"`
			Result  []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"result"`
			ResultInfo struct {
				TotalPages int `json:"total_pages"`
			} `json:"result_info"`
		}
		if json.Unmarshal(body, &payload) != nil || !payload.Success {
			return nil, ErrDNSFailed
		}
		for _, zone := range payload.Result {
			if strings.TrimSpace(zone.ID) == "" {
				continue
			}
			zones = append(zones, Zone{ID: zone.ID, Name: zone.Name})
		}
		if page >= payload.ResultInfo.TotalPages || len(payload.Result) == 0 {
			break
		}
	}
	if zones == nil {
		zones = []Zone{}
	}
	return zones, nil
}

// ListTunnelCNAMEs returns CNAME rows in managed zones that point at this tunnel.
func (c *Client) ListTunnelCNAMEs(ctx context.Context, tunnelID string) ([]DNSRecord, error) {
	if !ValidTunnelID(tunnelID) {
		return nil, ErrTunnelNotFound
	}
	zones, err := c.ListManagedZones(ctx)
	if err != nil {
		return nil, err
	}
	target := TunnelCNAMETarget(tunnelID)
	var matched []DNSRecord
	for _, zone := range zones {
		records, err := c.listDNS(ctx, zone.ID, url.Values{"type": {"CNAME"}, "content": {target}})
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			if CNAMEMatches(record, tunnelID) {
				matched = append(matched, record)
			}
		}
	}
	if matched == nil {
		matched = []DNSRecord{}
	}
	return matched, nil
}

// ListCNAMEsByName reads CNAME rows for one exact hostname.
func (c *Client) ListCNAMEsByName(ctx context.Context, hostname string) ([]DNSRecord, error) {
	host, err := NormalizeHostname(hostname)
	if err != nil {
		return nil, err
	}
	client, err := c.hostnameClient(ctx, host)
	if err != nil {
		return nil, err
	}
	zones, err := client.listManagedZonesSingle(ctx)
	if err != nil {
		return nil, err
	}
	var matched []DNSRecord
	for _, zone := range zones {
		records, err := c.listDNS(ctx, zone.ID, url.Values{"type": {"CNAME"}, "name": {host}})
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			name, nameErr := NormalizeHostname(record.Name)
			if nameErr != nil || name != host || !strings.EqualFold(record.Type, "CNAME") {
				continue
			}
			matched = append(matched, record)
		}
	}
	if matched == nil {
		matched = []DNSRecord{}
	}
	return matched, nil
}

// GetDNSRecord reads one record again before deleting it.
func (c *Client) GetDNSRecord(ctx context.Context, zoneID, recordID string) (DNSRecord, error) {
	cert, root, err := c.apiSessionForZone(ctx, zoneID)
	if err != nil {
		return DNSRecord{}, err
	}
	endpoint := fmt.Sprintf("%s/zones/%s/dns_records/%s", root, url.PathEscape(zoneID), url.PathEscape(recordID))
	body, status, err := c.apiGet(ctx, cert, endpoint)
	if err != nil {
		return DNSRecord{}, err
	}
	if status == http.StatusNotFound {
		return DNSRecord{}, ErrDNSAbsent
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return DNSRecord{}, ErrDNSPermission
	}
	record, err := decodeDNSRecord(body, zoneID)
	if err != nil {
		return DNSRecord{}, err
	}
	return record, nil
}

// DeleteDNSRecord removes one record. A missing record is success.
func (c *Client) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	cert, root, err := c.apiSessionForZone(ctx, zoneID)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/zones/%s/dns_records/%s", root, url.PathEscape(zoneID), url.PathEscape(recordID))
	status, err := c.apiSend(ctx, cert, http.MethodDelete, endpoint)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return nil
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return ErrDNSPermission
	}
	if status < 200 || status >= 300 {
		return ErrDNSFailed
	}
	return nil
}

func (c *Client) listDNS(ctx context.Context, zoneID string, query url.Values) ([]DNSRecord, error) {
	cert, root, err := c.apiSessionForZone(ctx, zoneID)
	if err != nil {
		return nil, err
	}
	var records []DNSRecord
	for page := 1; page < 50; page++ {
		values := url.Values{}
		for key, items := range query {
			values[key] = items
		}
		values.Set("page", fmt.Sprintf("%d", page))
		values.Set("per_page", "100")
		endpoint := fmt.Sprintf("%s/zones/%s/dns_records?%s", root, url.PathEscape(zoneID), values.Encode())
		body, status, err := c.apiGet(ctx, cert, endpoint)
		if err != nil {
			return nil, err
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return nil, ErrDNSPermission
		}
		var payload struct {
			Success bool `json:"success"`
			Result  []struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Type    string `json:"type"`
				Content string `json:"content"`
			} `json:"result"`
			ResultInfo struct {
				TotalPages int `json:"total_pages"`
			} `json:"result_info"`
		}
		if json.Unmarshal(body, &payload) != nil || !payload.Success {
			return nil, ErrDNSFailed
		}
		for _, item := range payload.Result {
			records = append(records, DNSRecord{
				ID: item.ID, ZoneID: zoneID, Name: item.Name, Type: item.Type, Content: item.Content,
			})
		}
		if page >= payload.ResultInfo.TotalPages || len(payload.Result) == 0 {
			break
		}
	}
	return records, nil
}

func decodeDNSRecord(body []byte, zoneID string) (DNSRecord, error) {
	var payload struct {
		Success bool `json:"success"`
		Result  struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Type    string `json:"type"`
			Content string `json:"content"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &payload) != nil || !payload.Success {
		return DNSRecord{}, ErrDNSFailed
	}
	return DNSRecord{
		ID: payload.Result.ID, ZoneID: zoneID, Name: payload.Result.Name,
		Type: payload.Result.Type, Content: payload.Result.Content,
	}, nil
}

func (c *Client) apiSessionForZone(ctx context.Context, zoneID string) (originCert, string, error) {
	client, err := c.zoneClient(zoneID)
	if err != nil {
		return originCert{}, "", err
	}
	return client.apiSession(ctx)
}

func (c *Client) apiSession(ctx context.Context) (originCert, string, error) {
	if !c.certExists() {
		return originCert{}, "", ErrDNSPermission
	}
	cert, err := c.readOriginCert()
	if err != nil {
		return originCert{}, "", ErrDNSPermission
	}
	root := cloudflareAPIRoot
	if strings.EqualFold(strings.TrimSpace(cert.Endpoint), "fed") {
		root = "https://api.fed.cloudflare.com/client/v4"
	}
	if strings.TrimSpace(c.ZoneAPI) != "" {
		root = strings.TrimRight(c.ZoneAPI, "/")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return cert, root, nil
}

func (c *Client) apiGet(ctx context.Context, cert originCert, endpoint string) ([]byte, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, ErrDNSFailed
	}
	req.Header.Set("Authorization", "Bearer "+cert.APIToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "loris-tunnel")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, ErrDNSNetwork
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, ErrDNSNetwork
	}
	return body, resp.StatusCode, nil
}

func (c *Client) apiSend(ctx context.Context, cert originCert, method, endpoint string) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return 0, ErrDNSFailed
	}
	req.Header.Set("Authorization", "Bearer "+cert.APIToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "loris-tunnel")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, ErrDNSNetwork
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, nil
}
