package cloudflared

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func authorizationAPI(t *testing.T, rejectB *atomic.Bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if key == "b" && rejectB.Load() {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		name, id := "a.example", "zone-a"
		if key == "b" || key == "b2" {
			name, id = "b.example", "zone-b"
		}
		if key == "child" {
			name, id = "sub.a.example", "zone-child"
		}
		if r.URL.Path == "/zones" {
			fmt.Fprintf(w, `{"success":true,"result":[{"id":%q,"name":%q}],"result_info":{"total_pages":1}}`, id, name)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/zones/"+id+"/dns_records") {
			t.Errorf("wrong credential %s for %s", key, r.URL.Path)
			w.WriteHeader(403)
			return
		}
		if r.Method == http.MethodDelete {
			_, _ = w.Write([]byte(`{"success":true}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/record") {
			fmt.Fprintf(w, `{"success":true,"result":{"id":"record","name":%q,"type":"CNAME","content":%q}}`, name, TunnelCNAMETarget("03e12345-6789-4abc-8def-0123456789ab"))
			return
		}
		fmt.Fprintf(w, `{"success":true,"result":[{"id":"record","name":%q,"type":"CNAME","content":%q}],"result_info":{"total_pages":1}}`, name, TunnelCNAMETarget("03e12345-6789-4abc-8def-0123456789ab"))
	}))
	t.Cleanup(server.Close)
	return server
}

func authorizeFixture(t *testing.T, client *Client, token, zone, account, expected string) string {
	t.Helper()
	fixture := filepath.Join(t.TempDir(), "incoming.pem")
	raw, _ := json.Marshal(originCert{APIToken: token, ZoneID: zone, AccountID: account})
	if err := os.WriteFile(fixture, pem.EncodeToMemory(&pem.Block{Type: "ARGO TUNNEL TOKEN", Bytes: raw}), 0600); err != nil {
		t.Fatal(err)
	}
	cert, _ := client.certPath()
	script := fmt.Sprintf("#!/bin/sh\ncp '%s' '%s'\n", fixture, cert)
	client.Binary = filepath.Join(t.TempDir(), "cloudflared")
	if err := os.WriteFile(client.Binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	if err := client.StartZoneAuthorization(expected, nil, func() { close(done) }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("authorization timed out")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.loginError
}

func TestAuthorizationsAddReauthorizeRestoreAndRestart(t *testing.T) {
	var rejectB atomic.Bool
	client := NewClient("")
	client.ConfigHome = t.TempDir()
	client.ZoneAPI = authorizationAPI(t, &rejectB).URL
	if got := authorizeFixture(t, client, "a", "zone-a", "account", ""); got != "" {
		t.Fatal(got)
	}
	original, _ := os.ReadFile(filepath.Join(client.ConfigHome, "cert.pem"))
	identity, account := client.CacheIdentity(), client.AccountIdentity()
	if got := authorizeFixture(t, client, "b", "zone-b", "account", ""); got != "" {
		t.Fatal(got)
	}
	if identity == client.CacheIdentity() || account != client.AccountIdentity() {
		t.Fatal("account and authorization identities were conflated")
	}
	restored, _ := os.ReadFile(filepath.Join(client.ConfigHome, "cert.pem"))
	if string(original) != string(restored) {
		t.Fatal("default certificate was overwritten")
	}
	restarted := NewClient("")
	restarted.ConfigHome, restarted.ZoneAPI = client.ConfigHome, client.ZoneAPI
	list := restarted.ListZones(t.Context())
	if len(list.Items) != 2 || list.Items[0].Name != "a.example" || list.Items[1].Name != "b.example" {
		t.Fatal(list)
	}
	if got := authorizeFixture(t, client, "b2", "zone-b", "account", "zone-b"); got != "" {
		t.Fatal(got)
	}
	if client.loginResult != "updated" || len(client.ListZones(t.Context()).Items) != 2 {
		t.Fatal("duplicate authorization was not updated")
	}
	for _, tc := range []struct{ account, expected, want string }{{"other-account", "", "account_mismatch"}, {"account", "zone-a", "domain_mismatch"}} {
		if got := authorizeFixture(t, client, "b", "zone-b", tc.account, tc.expected); got != "cloudflare_authorization_"+tc.want {
			t.Fatal(got)
		}
		current, _ := os.ReadFile(filepath.Join(client.ConfigHome, "cert.pem"))
		if string(current) != string(original) {
			t.Fatal("failed authorization lost the default credential")
		}
	}
	if len(client.ListZones(t.Context()).Items) != 2 {
		t.Fatal("failed authorization replaced existing domains")
	}
}

func TestAuthorizationsPartialFailureDNSSelectionAndRemoval(t *testing.T) {
	var rejectB atomic.Bool
	client := NewClient("")
	client.ConfigHome = t.TempDir()
	client.ZoneAPI = authorizationAPI(t, &rejectB).URL
	writeOriginCert(t, client.ConfigHome, "a", "zone-a")
	legacy := client.legacyIdentity()
	if list := client.ListZones(t.Context()); len(list.Items) != 1 {
		t.Fatal(list)
	}
	if got := authorizeFixture(t, client, "b", "zone-b", "account", ""); got != "" {
		t.Fatal(got)
	}
	if !client.MatchesAccount("cf:" + legacy) {
		t.Fatal("legacy cleanup account was lost")
	}
	if got := authorizeFixture(t, client, "child", "zone-child", "account", ""); got != "" {
		t.Fatal(got)
	}
	selected, err := client.hostnameClient(t.Context(), "dev.sub.a.example")
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := selected.readOriginCert()
	if cert.ZoneID != "zone-child" {
		t.Fatal("longest zone match not selected")
	}
	for _, zone := range []string{"zone-a", "zone-b"} {
		if _, err := client.GetDNSRecord(t.Context(), zone, "record"); err != nil {
			t.Fatal(err)
		}
		if err := client.DeleteDNSRecord(t.Context(), zone, "record"); err != nil {
			t.Fatal(err)
		}
	}
	rejectB.Store(true)
	list := client.ListZones(t.Context())
	if list.State != ResourceReady || len(list.Items) != 3 {
		t.Fatal(list)
	}
	for _, zone := range list.Items {
		if zone.ID == "zone-b" && zone.State != ResourceAuth {
			t.Fatal(zone)
		}
	}
	if _, err := client.hostnameClient(t.Context(), "dev.a.example"); err != nil {
		t.Fatal("unrelated failed authorization blocked a working domain", err)
	}
	if _, err := client.hostnameClient(t.Context(), "evil-a.example"); err == nil {
		t.Fatal("matched a non-domain suffix")
	}
	if _, err := client.ListTunnelCNAMEs(t.Context(), "03e12345-6789-4abc-8def-0123456789ab"); err == nil {
		t.Fatal("partial DNS scan reported complete")
	}
	for _, id := range []string{"zone-a", "zone-b", "zone-child"} {
		if err := client.RemoveAuthorization(id); err != nil {
			t.Fatal(err)
		}
	}
	list = client.ListZones(t.Context())
	if len(list.Items) != 0 || client.accountCredentialExists() {
		t.Fatal("removed domain was reimported from the default certificate")
	}
	if _, err := os.Stat(filepath.Join(client.ConfigHome, "cert.pem")); err != nil {
		t.Fatal("external default certificate was deleted")
	}
}

func TestAuthorizationCancelDuringVerificationAndCrashRecovery(t *testing.T) {
	home := t.TempDir()
	writeOriginCert(t, home, "a", "zone-a")
	original, _ := os.ReadFile(filepath.Join(home, "cert.pem"))
	client := NewClient("")
	client.ConfigHome = home
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer server.Close()
	client.ZoneAPI = server.URL
	done := make(chan string, 1)
	go func() { done <- authorizeFixture(t, client, "b", "zone-b", "account", "") }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("verification not started")
	}
	client.CancelLogin()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not stop verification")
	}
	raw, _ := os.ReadFile(filepath.Join(home, "cert.pem"))
	if string(raw) != string(original) {
		t.Fatal("cancel did not restore original")
	}
	if len(client.accountCertPaths()) != 1 {
		t.Fatal("cancel committed new authorization")
	}
	backup, err := client.parkCert()
	if err != nil {
		t.Fatal(err)
	}
	writeOriginCert(t, home, "b", "zone-b")
	restarted := NewClient("")
	restarted.ConfigHome = home
	restarted.recoverLoginBackup()
	raw, _ = os.ReadFile(filepath.Join(home, "cert.pem"))
	if string(raw) != string(original) {
		t.Fatal("restart did not restore backup")
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatal("backup not retired after recovery")
	}
}

func TestDNSRouteUsesSelectedCertificate(t *testing.T) {
	var rejectB atomic.Bool
	client := NewClient("")
	client.ConfigHome = t.TempDir()
	client.ZoneAPI = authorizationAPI(t, &rejectB).URL
	writeOriginCert(t, client.ConfigHome, "a", "zone-a")
	if _, err := client.ListManagedZones(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := authorizeFixture(t, client, "b", "zone-b", "account", ""); got != "" {
		t.Fatal(got)
	}
	log := filepath.Join(t.TempDir(), "args")
	if err := os.WriteFile(client.Binary, []byte(fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > '%s'\n", log)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := client.EnsureDNSRoute(t.Context(), "03e12345-6789-4abc-8def-0123456789ab", "dev.b.example"); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(log)
	parts := strings.Split(strings.TrimSpace(string(args)), "\n")
	if len(parts) < 5 || parts[3] != "--origincert" {
		t.Fatal(string(args))
	}
	raw, _ := os.ReadFile(parts[4])
	cert, _ := decodeOriginCert(raw)
	if cert.ZoneID != "zone-b" {
		t.Fatal("DNS route used the wrong domain credential")
	}
}
