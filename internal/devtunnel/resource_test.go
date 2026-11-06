package devtunnel

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseResourceListQualifiesClusterAndPorts(t *testing.T) {
	items, err := ParseResourceList(`[
		{"tunnelId":"abc123","clusterId":"usw3","hostConnections":0,"accessControl":{"entries":[{"type":"Anonymous"}]},"expiration":"7d","ports":[{"portNumber":8080,"protocol":"http","uri":"https://abc123-8080.usw3.devtunnels.ms"}]}
	]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items=%d", len(items))
	}
	item := items[0]
	if item.ID != "abc123.usw3" || item.Access != "public" || item.Expiration != "7d" || item.HostConnections != 0 {
		t.Fatalf("%+v", item)
	}
	if len(item.Ports) != 1 || item.Ports[0].Number != 8080 || item.Ports[0].Protocol != "http" {
		t.Fatalf("ports=%+v", item.Ports)
	}
	if len(item.URLs) != 1 || item.URLs[0] != "https://abc123-8080.usw3.devtunnels.ms" {
		t.Fatalf("urls=%v", item.URLs)
	}
}

func TestParseResourceAccessControls(t *testing.T) {
	cases := []struct {
		name, output, want string
	}{
		{"show array", `{"tunnel":{"tunnelId":"abc.usw3","accessControl":[{"type":"Anonymous","scopes":["connect"]}]}}`, "public"},
		{"list entries", `{"tunnelId":"abc.usw3","accessControlEntries":[{"type":"Anonymous","scopes":["connect"]}]}`, "public"},
		{"legacy entries", `{"tunnelId":"abc.usw3","accessControl":{"entries":[{"type":"Anonymous"}]}}`, "public"},
		{"non-connect anonymous", `{"tunnelId":"abc.usw3","accessControl":[{"type":"Anonymous","scopes":["manage"]}]}`, "private"},
		{"empty ACL", `{"tunnelId":"abc.usw3","accessControl":[]}`, "private"},
		{"missing ACL", `{"tunnelId":"abc.usw3"}`, "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resource, err := parseResourceOutput(tc.output)
			if err != nil || resource.Access != tc.want {
				t.Fatalf("access = %q, want %q; err = %v", resource.Access, tc.want, err)
			}
		})
	}
}

func TestParseResourceDoesNotInventURL(t *testing.T) {
	items, err := ParseResourceList(`[{"tunnelId":"abc123.usw3","ports":[{"portNumber":8080,"protocol":"https"}]}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || len(items[0].URLs) != 0 {
		t.Fatalf("%+v", items)
	}
}

func TestParsePortListEmpty(t *testing.T) {
	ports, urls, err := ParsePortList(`[]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 0 || len(urls) != 0 {
		t.Fatalf("ports=%v urls=%v", ports, urls)
	}
}

func TestSameTunnelID(t *testing.T) {
	if !SameTunnelID("abc123", "abc123.usw3") {
		t.Fatal("short id did not match qualified id")
	}
	if SameTunnelID("abc123.usw2", "abc123.usw3") {
		t.Fatal("different clusters matched")
	}
	if SameTunnelID("", "abc123") {
		t.Fatal("empty id matched")
	}
}

func TestMatchPort(t *testing.T) {
	resource := Resource{Ports: []Port{{Number: 8080, Protocol: "http"}}}
	if err := MatchPort(resource, 8080, "HTTP"); err != nil {
		t.Fatal(err)
	}
	if err := MatchPort(resource, 3000, "http"); err == nil {
		t.Fatal("different port was accepted")
	}
	resource.Ports = append(resource.Ports, Port{Number: 3000, Protocol: "https"})
	if err := MatchPort(resource, 8080, "http"); err != nil {
		t.Fatal(err)
	}
	if err := MatchPort(Resource{}, 8080, "http"); err != ErrNoPorts {
		t.Fatal(err)
	}
}

func TestClassifyCLIFailure(t *testing.T) {
	state, _ := classifyCLIFailure("Login required.", errString("exit status 1"))
	if state != ResourceAuth {
		t.Fatal(state)
	}
	state, _ = classifyCLIFailure("Tunnel not found", errString("exit status 1"))
	if state != ResourceNotFound {
		t.Fatal(state)
	}
	state, _ = classifyCLIFailure("dial tcp: i/o timeout", errString("exit status 1"))
	if state != ResourceNetwork {
		t.Fatal(state)
	}
	state, _ = classifyCLIFailure(`{"tunnelId":"abc"}`, nil)
	if state != "" {
		t.Fatal(state)
	}
}

func TestParseTunnelIDKeepsCluster(t *testing.T) {
	if got := ParseTunnelID(`{"tunnelId":"abc123","clusterId":"usw3"}`); got != "abc123.usw3" {
		t.Fatal(got)
	}
}

func TestListResourcesFetchesMissingPorts(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "devtunnel")
	script := `#!/bin/sh
if [ "$1" = "list" ]; then
 echo '[{"tunnelId":"configured","clusterId":"usw3"},{"tunnelId":"empty","clusterId":"usw3"},{"tunnelId":"failed","clusterId":"usw3"},{"tunnelId":"known","clusterId":"usw3","ports":[]}]'
 exit 0
fi
case "$3" in
 configured.usw3) echo '[{"portNumber":8080,"protocol":"http"}]' ;;
 empty.usw3) echo '[]' ;;
 failed.usw3) echo 'network unavailable' >&2; exit 1 ;;
 *) echo 'unexpected port query' >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	result := NewClient(binary).ListResources(context.Background())
	if result.State != ResourceReady || len(result.Items) != 4 {
		t.Fatalf("%+v", result)
	}
	configured, empty, failed, known := result.Items[0], result.Items[1], result.Items[2], result.Items[3]
	if !configured.PortsKnown || len(configured.Ports) != 1 || configured.Ports[0].Number != 8080 {
		t.Fatalf("configured: %+v", configured)
	}
	if !empty.PortsKnown || len(empty.Ports) != 0 {
		t.Fatalf("empty: %+v", empty)
	}
	if failed.PortsKnown || failed.PortsError == "" {
		t.Fatalf("failed: %+v", failed)
	}
	if !known.PortsKnown || known.PortsError != "" {
		t.Fatalf("known: %+v", known)
	}
}

func TestListResourcesReadsAccessForKnownPorts(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "devtunnel")
	script := `#!/bin/sh
if [ "$1" = "list" ]; then
 echo '[{"tunnelId":"public","ports":[]},{"tunnelId":"private","ports":[]},{"tunnelId":"legacy","ports":[]},{"tunnelId":"failed","ports":[]}]'
 exit 0
fi
if [ "$1 $2" = "access list" ]; then
 case "$3" in
 public) echo '{"accessControlEntries":[{"type":"Anonymous","scopes":["connect"]}]}' ;;
 private) echo '{"accessControlEntries":[]}' ;;
 legacy) echo '[{"type":"Anonymous"}]' ;;
 failed) exit 1 ;;
 esac
 exit $?
fi
exit 1
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	result := NewClient(binary).ListResources(context.Background())
	if result.State != ResourceReady || len(result.Items) != 4 {
		t.Fatalf("%+v", result)
	}
	for i, want := range []string{"public", "private", "public", "unknown"} {
		if result.Items[i].Access != want {
			t.Fatalf("item %d: %+v", i, result.Items[i])
		}
	}
}

func TestServiceURLsAreScopedToPublishedPort(t *testing.T) {
	urls := []string{"https://abc-8080.usw3.devtunnels.ms", " https://abc.usw3.devtunnels.ms:8080", "https://abc-9090.usw3.devtunnels.ms"}
	got := URLsForPort(urls, 8080)
	if len(got) != 2 || got[1] != "https://abc.usw3.devtunnels.ms:8080" {
		t.Fatal(got)
	}
	if len(URLsForPort(urls, 3000)) != 0 {
		t.Fatal("unrelated service URL leaked")
	}
}
