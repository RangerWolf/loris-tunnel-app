package cloudflared

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"loris-tunnel/internal/conf"
)

// Only this device stores these files. Neither PEM data nor paths are returned to the UI.
type authorizationStore struct {
	AccountID string               `json:"accountId"`
	Removed   []string             `json:"removed,omitempty"`
	LegacyIDs []string             `json:"legacyIds,omitempty"`
	Entries   []authorizationEntry `json:"entries"`
}

type authorizationEntry struct {
	Zone Zone   `json:"zone"`
	File string `json:"file"`
}

func (c *Client) authorizationDir() (string, error) {
	dir, err := c.configDir()
	return filepath.Join(dir, "loris-tunnel", "authorizations"), err
}

func (c *Client) loadAuthorizationsLocked() (authorizationStore, error) {
	dir, err := c.authorizationDir()
	if err != nil {
		return authorizationStore{}, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err == nil {
		var store authorizationStore
		if json.Unmarshal(raw, &store) != nil {
			return store, fmt.Errorf("cloudflare_authorizations_invalid")
		}
		return store, nil
	}
	if !os.IsNotExist(err) {
		return authorizationStore{}, err
	}
	path, err := c.certPath()
	if err != nil {
		return authorizationStore{}, err
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		return authorizationStore{}, err
	}
	cert, err := decodeOriginCert(raw)
	if err != nil || cert.AccountID == "" {
		return authorizationStore{}, ErrAuthRequired
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return authorizationStore{}, err
	}
	file := fmt.Sprintf("%x.pem", sha256.Sum256(raw))
	if err := conf.WritePrivateAtomic(filepath.Join(dir, file), raw); err != nil {
		return authorizationStore{}, err
	}
	store := authorizationStore{AccountID: cert.AccountID, LegacyIDs: []string{fmt.Sprintf("%x", sha256.Sum256(raw))}, Entries: []authorizationEntry{{Zone: Zone{ID: cert.ZoneID}, File: file}}}
	return store, c.saveAuthorizationsLocked(store)
}

func (c *Client) saveAuthorizationsLocked(store authorizationStore) error {
	dir, err := c.authorizationDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(store)
	if err != nil {
		return err
	}
	return conf.WritePrivateAtomic(filepath.Join(dir, "index.json"), raw)
}

func (c *Client) authorizationPath(file string) (string, error) {
	if len(file) != 68 || filepath.Base(file) != file || !strings.HasSuffix(file, ".pem") {
		return "", ErrAuthRequired
	}
	for _, ch := range file[:64] {
		if !strings.ContainsRune("0123456789abcdef", ch) {
			return "", ErrAuthRequired
		}
	}
	dir, err := c.authorizationDir()
	return filepath.Join(dir, file), err
}

func (c *Client) authorizedClient(file string) (*Client, error) {
	path, err := c.authorizationPath(file)
	if err != nil {
		return nil, err
	}
	return &Client{Binary: c.Binary, ConfigHome: c.ConfigHome, ZoneAPI: c.ZoneAPI, certFile: path}, nil
}

func (c *Client) accountCertPaths() []string {
	if c.certFile != "" {
		return []string{c.certFile}
	}
	c.authMu.Lock()
	defer c.authMu.Unlock()
	store, err := c.loadAuthorizationsLocked()
	if err != nil {
		// An unparseable legacy certificate remains usable by the official CLI.
		if path, e := c.certPath(); e == nil && (errors.Is(err, ErrAuthRequired) || os.IsNotExist(err)) {
			return []string{path}
		}
		return nil
	}
	var paths []string
	seen := map[string]bool{}
	for _, entry := range store.Entries {
		path, e := c.authorizationPath(entry.File)
		if e == nil && !seen[path] {
			paths = append(paths, path)
			seen[path] = true
		}
	}
	return paths
}

func (c *Client) zoneClient(zoneID string) (*Client, error) {
	if c.certFile != "" {
		return c, nil
	}
	c.authMu.Lock()
	defer c.authMu.Unlock()
	store, err := c.loadAuthorizationsLocked()
	if err != nil {
		return nil, ErrDNSPermission
	}
	for _, entry := range store.Entries {
		if entry.Zone.ID == zoneID {
			return c.authorizedClient(entry.File)
		}
	}
	return nil, ErrDNSPermission
}

func (c *Client) hostnameClient(ctx context.Context, host string) (*Client, error) {
	result := c.ListAuthorizations(ctx)
	if result.State != ResourceReady {
		return nil, ErrDNSPermission
	}
	zones := result.Items
	var best Zone
	for _, zone := range zones {
		if (host == zone.Name || strings.HasSuffix(host, "."+zone.Name)) && len(zone.Name) > len(best.Name) {
			best = zone
		}
	}
	if best.ID == "" || best.State != ResourceReady {
		return nil, ErrDNSPermission
	}
	return c.zoneClient(best.ID)
}

func (c *Client) acceptAuthorization(ctx context.Context, expectedZone string) (string, error) {
	path, err := c.certPath()
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	cert, err := decodeOriginCert(raw)
	if err != nil || cert.AccountID == "" {
		return "", fmt.Errorf("cloudflare_authorization_invalid")
	}
	probe := &Client{Binary: c.Binary, ConfigHome: c.ConfigHome, ZoneAPI: c.ZoneAPI, certFile: path}
	zones, err := probe.listManagedZonesSingle(ctx)
	if err != nil {
		return "", fmt.Errorf("cloudflare_authorization_verify_failed")
	}
	if expectedZone != "" && cert.ZoneID != expectedZone {
		return "", fmt.Errorf("cloudflare_authorization_domain_mismatch")
	}
	var selected []Zone
	for _, zone := range zones {
		zone.Name, err = NormalizeHostname(zone.Name)
		if err != nil {
			return "", fmt.Errorf("cloudflare_authorization_invalid")
		}
		if zone.ID == cert.ZoneID {
			selected = append(selected, zone)
		}
	}
	if len(selected) == 0 {
		return "", fmt.Errorf("cloudflare_authorization_domain_mismatch")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loginPhase == "cancelled" {
		return "", fmt.Errorf("cloudflare_authorization_cancelled")
	}
	c.authMu.Lock()
	defer c.authMu.Unlock()
	store, err := c.loadAuthorizationsLocked()
	if err != nil {
		return "", err
	}
	if store.AccountID != "" && store.AccountID != cert.AccountID {
		return "", fmt.Errorf("cloudflare_authorization_account_mismatch")
	}

	store.AccountID = cert.AccountID
	keptRemoved := store.Removed[:0]
	for _, id := range store.Removed {
		if id != cert.ZoneID {
			keptRemoved = append(keptRemoved, id)
		}
	}
	store.Removed = keptRemoved
	file := fmt.Sprintf("%x.pem", sha256.Sum256(raw))
	dir, _ := c.authorizationDir()
	if err := conf.WritePrivateAtomic(filepath.Join(dir, file), raw); err != nil {
		return "", fmt.Errorf("cloudflare_authorization_save_failed")
	}
	result := "added"
	for _, zone := range selected {
		updated := false
		for i := range store.Entries {
			if store.Entries[i].Zone.ID == zone.ID {
				store.Entries[i] = authorizationEntry{Zone: zone, File: file}
				updated = true
				result = "updated"
				break
			}
		}
		if !updated {
			store.Entries = append(store.Entries, authorizationEntry{Zone: zone, File: file})
		}
	}
	if err := c.saveAuthorizationsLocked(store); err != nil {
		return "", fmt.Errorf("cloudflare_authorization_save_failed")
	}
	return result, nil
}

func (c *Client) RemoveAuthorization(zoneID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loginActive {
		return fmt.Errorf("cloudflare_authorization_busy")
	}
	c.authMu.Lock()
	defer c.authMu.Unlock()
	store, err := c.loadAuthorizationsLocked()
	if err != nil {
		return err
	}
	kept := make([]authorizationEntry, 0, len(store.Entries))
	for _, entry := range store.Entries {
		if entry.Zone.ID != zoneID {
			kept = append(kept, entry)
		}
	}
	store.Entries = kept
	store.Removed = append(store.Removed, zoneID)
	if len(kept) == 0 {
		store.AccountID = ""
		store.LegacyIDs = nil
	}
	if err := c.saveAuthorizationsLocked(store); err != nil {
		return err
	}
	// Only remove unreferenced managed PEMs after the index commit.
	dir, _ := c.authorizationDir()
	files, _ := filepath.Glob(filepath.Join(dir, "*.pem"))
	used := map[string]bool{}
	for _, entry := range kept {
		used[entry.File] = true
	}
	for _, file := range files {
		if !used[filepath.Base(file)] {
			_ = os.Remove(file)
		}
	}
	return nil
}

func (c *Client) ListAuthorizations(ctx context.Context) ZoneList {
	c.authMu.Lock()
	store, err := c.loadAuthorizationsLocked()
	c.authMu.Unlock()
	if err != nil {
		return ZoneList{State: ResourceAuth, Detail: errZoneAuth.Error(), Items: []Zone{}}
	}
	original := store
	original.Entries = append([]authorizationEntry{}, store.Entries...)
	blocked := map[string]bool{}
	for _, id := range store.Removed {
		blocked[id] = true
	}
	seen := map[string]bool{}
	for _, entry := range store.Entries {
		seen[entry.Zone.ID] = true
	}

	type lookup struct {
		zones []Zone
		err   error
	}
	lookups := map[string]*lookup{}
	slots := make(chan struct{}, 4)
	var queries sync.WaitGroup
	for _, entry := range store.Entries {
		if lookups[entry.File] != nil {
			continue
		}
		result := &lookup{}
		lookups[entry.File] = result
		queries.Add(1)
		go func(file string) {
			defer queries.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			client, err := c.authorizedClient(file)
			if err != nil {
				result.err = err
				return
			}
			result.zones, result.err = client.listManagedZonesSingle(ctx)
		}(entry.File)
	}
	queries.Wait()
	pendingPEMs := map[string][]byte{}
	items := make([]Zone, 0, len(store.Entries))
	for i, entry := range store.Entries {
		zone := entry.Zone
		zone.State = ResourceReady
		found, e := lookups[entry.File].zones, lookups[entry.File].err

		if e != nil {
			zone.State = ResourceError
			if e == ErrDNSPermission {
				zone.State = ResourceAuth
			} else if e == ErrDNSNetwork {
				zone.State = ResourceNetwork
			}
		} else {
			zone.State = ResourceAuth
			for _, current := range found {
				if current.ID == zone.ID {
					zone.Name, e = NormalizeHostname(current.Name)
					if e == nil {
						zone.State = ResourceReady
					}
					break
				}
			}
		}

		if e == nil {
			for _, current := range found {
				if seen[current.ID] || blocked[current.ID] {
					continue
				}
				name, nameErr := NormalizeHostname(current.Name)
				if nameErr != nil {
					continue
				}
				current.Name, current.State = name, ResourceReady
				// The CLI's DNS route uses the zone embedded in its PEM, even when
				// the same API token is permitted to list more than one zone.
				file := entry.File
				client, clientErr := c.authorizedClient(entry.File)
				if clientErr != nil {
					continue
				}
				cert, certErr := client.readOriginCert()
				if certErr != nil {
					continue
				}
				if cert.ZoneID != current.ID {
					cert.ZoneID = current.ID
					body, marshalErr := json.Marshal(cert)
					if marshalErr != nil {
						continue
					}
					raw := pem.EncodeToMemory(&pem.Block{Type: "ARGO TUNNEL TOKEN", Bytes: body})
					file = fmt.Sprintf("%x.pem", sha256.Sum256(raw))
					pendingPEMs[file] = raw
				}
				store.Entries = append(store.Entries, authorizationEntry{Zone: Zone{ID: current.ID, Name: name}, File: file})
				items = append(items, current)
				seen[current.ID] = true
			}
		}
		store.Entries[i].Zone.Name = zone.Name
		items = append(items, zone)
	}
	// Update discovered names only if authorization membership has not changed in flight.
	c.authMu.Lock()
	current, e := c.loadAuthorizationsLocked()
	if e == nil && sameAuthorizationFiles(current, original) {
		for file, raw := range pendingPEMs {
			path, pathErr := c.authorizationPath(file)
			if pathErr != nil {
				e = pathErr
				break
			}
			if e = conf.WritePrivateAtomic(path, raw); e != nil {
				break
			}
		}
		if e == nil {
			e = c.saveAuthorizationsLocked(store)
		}
	} else if e == nil {
		e = fmt.Errorf("authorization changed during lookup")
	}
	c.authMu.Unlock()
	if e != nil {
		return ZoneList{State: ResourceError, Detail: "Could not update local domain metadata. Please refresh.", Items: []Zone{}}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return ZoneList{State: ResourceReady, Items: items}
}

func sameAuthorizationFiles(a, b authorizationStore) bool {
	if a.AccountID != b.AccountID || len(a.Entries) != len(b.Entries) {
		return false
	}
	for i := range a.Entries {
		if a.Entries[i].File != b.Entries[i].File || a.Entries[i].Zone.ID != b.Entries[i].Zone.ID {
			return false
		}
	}
	return true
}

func (c *Client) AccountIdentity() string {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	store, err := c.loadAuthorizationsLocked()
	if err != nil {
		return c.legacyIdentity()
	}
	return store.AccountID
}

func (c *Client) MatchesAccount(identity string) bool {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	store, err := c.loadAuthorizationsLocked()
	if err != nil {
		return (errors.Is(err, ErrAuthRequired) || os.IsNotExist(err)) && identity == "cf:"+c.legacyIdentity()
	}
	if store.AccountID == "" {
		return false
	}
	if identity == "cf:"+store.AccountID {
		return true
	}
	for _, id := range store.LegacyIDs {
		if identity == "cf:"+id {
			return true
		}
	}
	return false
}
