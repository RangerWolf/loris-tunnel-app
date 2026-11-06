package devtunnel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseClusterOutput(t *testing.T) {
	output := `{
	  "clusters": [
	    {"clusterId":"usw3","azureLocation":"WestUs3","uri":"https://usw3.rel.tunnels.api.visualstudio.com","ping":168},
	    {"clusterId":"euw","azureLocation":"WestEurope","uri":"https://euw.rel.tunnels.api.visualstudio.com"},
	    {"clusterId":"uks1","azureLocation":"UkSouth","uri":"https://uks1.rel.tunnels.api.visualstudio.com","ping":0},
	    {"clusterId":"evil","azureLocation":"Evil","uri":"https://evil.example/hook","ping":1}
	  ]
	}`
	records, err := parseClusterOutput(output, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("records = %+v", records)
	}
	if records[0].Name != "West US 3" || records[0].Status != "ok" || records[0].Latency == nil || *records[0].Latency != 168 {
		t.Fatalf("usw3 = %+v", records[0])
	}
	if records[0].URI != "https://usw3.rel.tunnels.api.visualstudio.com" {
		t.Fatal(records[0].URI)
	}
	if records[1].Name != "West Europe" || records[1].Status != "timeout" || records[1].Latency != nil {
		t.Fatalf("euw = %+v", records[1])
	}
	if records[2].Name != "UK South" || records[2].Status != "timeout" {
		t.Fatalf("uks = %+v", records[2])
	}
	probe := probeFrom(records, true, false)
	if probe.State != "partial" || probe.Regions[1].LatencyMs != nil {
		t.Fatalf("probe = %+v", probe)
	}
	if _, err := parseClusterOutput("not json", true); err == nil {
		t.Fatal("invalid output was accepted")
	}
	if _, err := parseClusterOutput(`{"clusters":[]}`, true); err == nil {
		t.Fatal("empty output was accepted")
	}
}

func TestRedactSecrets(t *testing.T) {
	got := redactSecrets("Token: ghu_exampletokenvalue\nAuthorization: github ghu_exampletokenvalue")
	if strings.Contains(got, "ghu_") || !strings.Contains(got, "[redacted]") {
		t.Fatal(got)
	}
}

func TestRegionNames(t *testing.T) {
	cases := map[string]string{
		"EastUs2":            "East US 2",
		"AustraliaSouthEast": "Australia South East",
		"JapanEast":          "Japan East",
	}
	for input, want := range cases {
		if got := regionDisplayName(input); got != want {
			t.Fatalf("%s = %q", input, got)
		}
	}
}

func TestCreatePersistentRegion(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "args.txt")
	client := NewClient(regionScript(t, logPath, "match"))
	id, err := client.CreatePersistent(context.Background(), false, "1h", "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	if id != "abc123.usw3" {
		t.Fatal(id)
	}
	args := readRegionLog(t, logPath)
	if strings.Contains(args, "service-uri") || strings.Contains(args, "clusters") || strings.Contains(args, "access-token") || strings.Contains(args, "user show") {
		t.Fatalf("auto create used a region: %s", args)
	}

	os.WriteFile(logPath, nil, 0600)
	id, err = client.CreatePersistent(context.Background(), true, "1h", "usw3", "")
	if err != nil {
		t.Fatal(err)
	}
	if id != "abc123.usw3" {
		t.Fatal(id)
	}
	args = readRegionLog(t, logPath)
	if !strings.Contains(args, "--service-uri https://usw3.rel.tunnels.api.visualstudio.com") {
		t.Fatalf("service uri missing: %s", args)
	}
	if !strings.Contains(args, "--access-token -") || !strings.Contains(args, "stdin:test-token") {
		t.Fatalf("regional create did not reuse the login: %s", args)
	}
	if !strings.Contains(args, "--allow-anonymous") {
		t.Fatal(args)
	}
	if strings.Contains(args[:strings.Index(args, "user show -v")], "access-token") {
		t.Fatal(args)
	}

	if _, err := client.CreatePersistent(context.Background(), false, "1h", "https://evil.example", ""); err == nil {
		t.Fatal("arbitrary region was accepted")
	}
}

func TestCreatePersistentRejectsRegionMismatch(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "args.txt")
	client := NewClient(regionScript(t, logPath, "mismatch"))
	_, err := client.CreatePersistent(context.Background(), false, "1h", "euw", "")
	if err == nil || !strings.Contains(err.Error(), "created in eun1 instead of euw; the new tunnel was removed") {
		t.Fatal(err)
	}
	args := readRegionLog(t, logPath)
	if !strings.Contains(args, "delete newid.eun1") && !strings.Contains(args, "delete newid") {
		t.Fatalf("mismatch was not removed: %s", args)
	}
}

func TestRegionProbeIsSharedAndCached(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "args.txt")
	client := NewClient(regionScript(t, logPath, "match"))
	var wg sync.WaitGroup
	results := make([]RegionProbe, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = client.LoadRegions(context.Background(), true, false)
		}(i)
	}
	wg.Wait()
	if results[0].State != "partial" || results[1].State != "partial" {
		t.Fatalf("%+v %+v", results[0], results[1])
	}
	if strings.Count(readRegionLog(t, logPath), "clusters --ping -j") != 1 {
		t.Fatalf("duplicate probe: %s", readRegionLog(t, logPath))
	}
	os.WriteFile(logPath, nil, 0600)
	cached := client.LoadRegions(context.Background(), true, false)
	if !cached.Cached || cached.Regions[0].ID != "usw3" {
		t.Fatalf("cache = %+v", cached)
	}
	if readRegionLog(t, logPath) != "" {
		t.Fatalf("cache still called the cli: %s", readRegionLog(t, logPath))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	client.regionMu.Lock()
	client.regionProbedAt = time.Time{}
	client.regionMu.Unlock()
	failed := client.LoadRegions(ctx, true, true)
	if failed.State != "error" || len(failed.Regions) == 0 || failed.Regions[0].Status != "error" {
		t.Fatalf("timeout did not keep known regions: %+v", failed)
	}
}

func regionScript(t *testing.T, logPath, mode string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "devtunnel")
	script := `#!/bin/sh
echo "$@" >> "` + logPath + `"
if [ "$1" = "clusters" ]; then
  if [ "$2" = "--ping" ]; then
    sleep 0.3
    echo '{"clusters":[{"clusterId":"usw3","azureLocation":"WestUs3","uri":"https://usw3.rel.tunnels.api.visualstudio.com","ping":168},{"clusterId":"euw","azureLocation":"WestEurope","uri":"https://euw.rel.tunnels.api.visualstudio.com"}]}'
    exit 0
  fi
  echo '{"clusters":[{"clusterId":"usw3","azureLocation":"WestUs3","uri":"https://usw3.rel.tunnels.api.visualstudio.com"},{"clusterId":"euw","azureLocation":"WestEurope","uri":"https://euw.rel.tunnels.api.visualstudio.com"}]}'
  exit 0
fi
if [ "$1" = "user" ]; then
  echo "Token: test-token"
  exit 0
fi
if [ "$1" = "create" ]; then
  case " $* " in
    *" --access-token - "*)
      IFS= read -r token || true
      echo "stdin:$token" >> "` + logPath + `"
      ;;
  esac
  case "` + mode + `" in
    mismatch) echo '{"tunnelId":"newid","clusterId":"eun1"}' ;;
    *) echo '{"tunnelId":"abc123","clusterId":"usw3"}' ;;
  esac
  exit 0
fi
if [ "$1" = "delete" ]; then
  exit 0
fi
echo "unexpected $*" >&2
exit 1
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return binary
}

func readRegionLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(data)
}
