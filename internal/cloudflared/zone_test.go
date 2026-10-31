package cloudflared

import (
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeOriginCert(t *testing.T, dir, token, zoneID string) {
	t.Helper()
	body, err := json.Marshal(originCert{ZoneID: zoneID, APIToken: token, AccountID: "account"})
	if err != nil {
		t.Fatal(err)
	}
	raw := pem.EncodeToMemory(&pem.Block{Type: "ARGO TUNNEL TOKEN", Bytes: body})
	if err := os.WriteFile(filepath.Join(dir, "cert.pem"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestListZonesReadsDomainWithoutReturningToken(t *testing.T) {
	const token = "zone-secret-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zones" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("authorization missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"zone-1","name":"Example.COM"},{"id":"zone-2","name":"Second.COM"}],"result_info":{"total_pages":1}}`))
	}))
	t.Cleanup(server.Close)
	home := t.TempDir()
	writeOriginCert(t, home, token, "zone-1")
	client := NewClient("")
	client.ConfigHome = home
	client.ZoneAPI = server.URL
	list := client.ListZones(t.Context())
	if list.State != ResourceReady || len(list.Items) != 2 || list.Items[0].Name != "example.com" || list.Items[1].Name != "second.com" {
		t.Fatalf("zones = %+v", list)
	}
	encoded, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), token) {
		t.Fatal("zone result included the certificate token")
	}
}

func TestListZonesRequiresCertAndHidesAPIFailure(t *testing.T) {
	client := NewClient("")
	client.ConfigHome = t.TempDir()
	if list := client.ListZones(t.Context()); list.State != ResourceAuth {
		t.Fatalf("missing cert = %+v", list)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"message":"forbidden"}]}`))
	}))
	t.Cleanup(server.Close)
	home := t.TempDir()
	writeOriginCert(t, home, "zone-secret-token", "zone-1")
	client.ConfigHome = home
	client.ZoneAPI = server.URL
	list := client.ListZones(t.Context())
	if list.State != ResourceReady || len(list.Items) != 1 || list.Items[0].State != ResourceAuth || strings.Contains(list.Detail, "zone-secret-token") {
		t.Fatalf("forbidden = %+v", list)
	}
}
