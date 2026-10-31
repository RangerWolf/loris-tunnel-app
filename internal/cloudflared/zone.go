package cloudflared

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
)

const cloudflareAPIRoot = "https://api.cloudflare.com/client/v4"

// Zone is one domain authorized by the local cert.pem. The API token is not included.
type Zone struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	State          string `json:"state,omitempty"`
	LinkedTunnels  int    `json:"linkedTunnels,omitempty"`
	PendingCleanup bool   `json:"pendingCleanup,omitempty"`
}

// ZoneList is the authorized-domain lookup result.
type ZoneList struct {
	State  string `json:"state"`
	Detail string `json:"detail"`
	Items  []Zone `json:"items"`
}

type originCert struct {
	ZoneID    string `json:"zoneID"`
	AccountID string `json:"accountID"`
	APIToken  string `json:"apiToken"`
	Endpoint  string `json:"endpoint"`
}

// ListZones returns every domain accessible to the current local credential.
func (c *Client) ListZones(ctx context.Context) ZoneList {
	return c.ListAuthorizations(ctx)
}

func (c *Client) readOriginCert() (originCert, error) {
	path, err := c.certPath()
	if err != nil {
		return originCert{}, err
	}
	if c.certFile != "" {
		path = c.certFile
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return originCert{}, err
	}
	return decodeOriginCert(raw)
}

func decodeOriginCert(raw []byte) (originCert, error) {
	var cert originCert
	rest := raw
	for {
		block, next := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = next
		if block.Type != "ARGO TUNNEL TOKEN" {
			continue
		}
		if err := json.Unmarshal(block.Bytes, &cert); err != nil {
			return originCert{}, err
		}
	}
	if strings.TrimSpace(cert.ZoneID) == "" || strings.TrimSpace(cert.APIToken) == "" {
		return originCert{}, fmt.Errorf("origin certificate has no zone")
	}
	return cert, nil
}

var (
	errZoneAuth    = fmt.Errorf("Cloudflare authentication is required.")
	errZoneNetwork = fmt.Errorf("Cloudflare could not be reached.")
	errZoneFailed  = fmt.Errorf("Could not read the authorized domain.")
)

// CacheIdentity isolates metadata by the locally authorized account and zone.
// Credentials themselves are never copied into the metadata cache.
func (c *Client) legacyIdentity() string {
	path, err := c.certPath()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

// CacheIdentity changes whenever the local authorization set or credential content changes.
func (c *Client) CacheIdentity() string {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	store, err := c.loadAuthorizationsLocked()
	if err != nil {
		return c.legacyIdentity()
	}
	raw, _ := json.Marshal(store)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
