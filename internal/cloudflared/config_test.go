package cloudflared

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRuntimeConfigHTTPAndHTTPS(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	cred := filepath.Join(home, ".cloudflared", "03e12345-6789-4abc-8def-0123456789ab.json")
	path, err := WriteRuntimeConfig(7, RuntimeSpec{
		TunnelID:        "03e12345-6789-4abc-8def-0123456789ab",
		CredentialsFile: cred,
		Hostname:        "devspace.example.com",
		Protocol:        "http",
		LocalHost:       "127.0.0.1",
		LocalPort:       3000,
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v %v", info, err)
	}
	var decoded runtimeFile
	raw, _ := os.ReadFile(path)
	if err := yaml.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Ingress[0].Service != "http://127.0.0.1:3000" || decoded.Ingress[1].Service != "http_status:404" {
		t.Fatalf("ingress = %+v", decoded.Ingress)
	}
	httpsPath, err := WriteRuntimeConfig(8, RuntimeSpec{
		TunnelID:        "03e12345-6789-4abc-8def-0123456789ab",
		CredentialsFile: cred,
		Hostname:        "secure.example.com",
		Protocol:        "https",
		LocalHost:       "127.0.0.1",
		LocalPort:       8443,
	})
	if err != nil {
		t.Fatal(err)
	}
	httpsRaw, _ := os.ReadFile(httpsPath)
	if !strings.Contains(string(httpsRaw), "https://127.0.0.1:8443") {
		t.Fatalf("https config = %s", httpsRaw)
	}
	if _, err := WriteRuntimeConfig(9, RuntimeSpec{
		TunnelID:        "03e12345-6789-4abc-8def-0123456789ab",
		CredentialsFile: cred,
		Hostname:        "bad.example.com\n  - service: http://evil",
		Protocol:        "http",
		LocalHost:       "127.0.0.1",
		LocalPort:       1,
	}); err == nil {
		t.Fatal("yaml injection hostname was accepted")
	}
}
