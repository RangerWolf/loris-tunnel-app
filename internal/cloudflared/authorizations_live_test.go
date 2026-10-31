package cloudflared

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	"loris-tunnel/internal/conf"
)

// Opt-in only: this creates uniquely named test tunnels and DNS records and cleans them up.
// Certificates are copied into t.TempDir, never into the application's authorization library.
func TestLiveMultiDomainAuthorizations(t *testing.T) {
	dir := os.Getenv("LORIS_CF_LIVE_CERT_DIR")
	if dir == "" {
		t.Skip("set LORIS_CF_LIVE_CERT_DIR and LORIS_CF_LIVE_BINARY for an explicitly authorized live test")
	}
	binary := os.Getenv("LORIS_CF_LIVE_BINARY")
	if binary == "" {
		t.Fatal("LORIS_CF_LIVE_BINARY is required")
	}
	client := NewClient(binary)
	client.ConfigHome = t.TempDir()
	certA, err := os.ReadFile(filepath.Join(dir, "domain-a.pem"))
	if err != nil {
		t.Fatal(err)
	}
	certB, err := os.ReadFile(filepath.Join(dir, "domain-b.pem"))
	if err != nil {
		t.Fatal(err)
	}
	defaultCert := filepath.Join(client.ConfigHome, "cert.pem")
	if err := conf.WritePrivateAtomic(defaultCert, certA); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 7*time.Minute)
	defer cancel()
	if zones := client.ListZones(ctx); zones.State != ResourceReady || len(zones.Items) != 1 || zones.Items[0].State != ResourceReady {
		t.Fatal(zones)
	}
	if err := conf.WritePrivateAtomic(defaultCert, certB); err != nil {
		t.Fatal(err)
	}
	if _, err := client.acceptAuthorization(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := conf.WritePrivateAtomic(defaultCert, certA); err != nil {
		t.Fatal(err)
	}
	zones := client.ListZones(ctx)
	if len(zones.Items) != 2 {
		t.Fatal(zones)
	}
	const marker = "loris-multi-domain-live-origin"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, marker) }))
	defer origin.Close()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 36)
	for index, zone := range zones.Items {
		if zone.State != ResourceReady {
			t.Fatal(zone)
		}
		t.Run(zone.Name, func(t *testing.T) {
			name := fmt.Sprintf("loris-check-%s-%d", stamp, index)
			hostname := name + "." + zone.Name
			created := client.CreateResource(ctx, CreateResourceInput{Name: name})
			if created.State != ResourceReady {
				t.Fatal(created)
			}
			id := created.Resource.ID
			var runtime *Host
			t.Cleanup(func() {
				if runtime != nil {
					if err := runtime.Stop(); err != nil {
						t.Error(err)
					}
				}
				cleanup, done := context.WithTimeout(context.Background(), time.Minute)
				defer done()
				records, err := client.ListCNAMEsByName(cleanup, hostname)
				if err != nil {
					t.Errorf("cleanup lookup for %s failed: %v", hostname, err)
				}
				for _, record := range records {
					if CNAMEMatches(record, id) {
						if err := client.DeleteDNSRecord(cleanup, record.ZoneID, record.ID); err != nil {
							t.Errorf("cleanup DNS %s: %v", hostname, err)
						}
					}
				}
				// These IDs were created by this test and their local processes are stopped.
				// Clear the remote connector registrations before deleting test resources.
				if _, err := client.runAccount(cleanup, binary, "tunnel", "cleanup", id); err != nil {
					t.Errorf("cleanup connector %s: %v", id, err)
				}
				if err := client.DeleteRemote(cleanup, id); err != nil {
					t.Errorf("cleanup tunnel %s: %v", id, err)
				}
				if detail := client.GetResource(cleanup, id); detail.State != ResourceNotFound {
					t.Errorf("test tunnel still present: %s (%s)", id, detail.State)
				}
				records, err = client.ListCNAMEsByName(cleanup, hostname)
				if err != nil || len(records) != 0 {
					t.Errorf("test DNS cleanup not confirmed for %s: %v", hostname, err)
				}
			})
			if err := client.EnsureDNSRoute(ctx, id, hostname); err != nil {
				t.Fatal(err)
			}
			records, err := client.ListCNAMEsByName(ctx, hostname)
			if err != nil || len(records) != 1 || !CNAMEMatches(records[0], id) {
				t.Fatalf("DNS route not verified: %+v %v", records, err)
			}
			credential, err := client.CredentialPath(id)
			if err != nil || !client.CredentialExists(id) {
				t.Fatalf("tunnel JSON credential not preserved: %v", err)
			}
			body, _ := yaml.Marshal(runtimeFile{Tunnel: id, CredentialsFile: credential, Ingress: []ingressRule{{Hostname: hostname, Service: origin.URL}, {Service: "http_status:404"}}})
			config := filepath.Join(t.TempDir(), "runtime.yml")
			if err := conf.WritePrivateAtomic(config, body); err != nil {
				t.Fatal(err)
			}
			runtime = client.NewHost(id, config, hostname)
			if err := runtime.Start(); err != nil {
				t.Fatal(err)
			}
			httpClient := &http.Client{Timeout: 10 * time.Second}
			deadline := time.Now().Add(65 * time.Second)
			var last string
			for time.Now().Before(deadline) {
				response, err := httpClient.Get("https://" + hostname)
				if err == nil {
					raw, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
					response.Body.Close()
					if response.StatusCode == http.StatusOK && readErr == nil && string(raw) == marker {
						t.Logf("verified HTTPS origin through %s; tunnel %s", hostname, id)
						return
					}
					last = fmt.Sprintf("HTTP %d", response.StatusCode)
				} else {
					last = err.Error()
				}
				time.Sleep(2 * time.Second)
			}
			t.Fatalf("public HTTPS origin did not become reachable: %s", last)
		})
	}
}
