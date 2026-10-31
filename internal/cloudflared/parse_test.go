package cloudflared

import (
	"errors"
	"strings"
	"testing"
)

const listFixture = `
2024-06-01T00:00:00Z INF Starting tunnel list
[
  {
    "id": "f70f4855-9c4d-4c3e-8b1a-1234567890ab",
    "name": "loris-devspace",
    "created_at": "2024-06-01T12:00:00Z",
    "deleted_at": "0001-01-01T00:00:00Z",
    "connections": [
      {"id": "11111111-1111-4111-8111-111111111111", "origin_ip": "203.0.113.4", "opened_at": "2024-06-02T12:00:00Z"}
    ]
  },
  {
    "id": "a9b21111-2222-4333-8444-555566667777",
    "name": "tokenbank",
    "createdAt": "2025-01-01T00:00:00Z",
    "conns": []
  }
]
`

func TestParseTunnelListFixture(t *testing.T) {
	items, err := ParseTunnelList(listFixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Name != "loris-devspace" || items[0].Connections != 1 || items[0].CreatedAt == "" {
		t.Fatalf("first = %+v", items[0])
	}
	if items[1].Connections != 0 || items[1].Name != "tokenbank" {
		t.Fatalf("second = %+v", items[1])
	}
	empty, err := ParseTunnelList("[]")
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty = %+v %v", empty, err)
	}
	none, err := ParseTunnelList("null\n")
	if err != nil || len(none) != 0 {
		t.Fatalf("null list = %+v %v", none, err)
	}
	if _, err := ParseTunnelList("not json"); err == nil {
		t.Fatal("malformed JSON was accepted")
	}
}

func TestParseTunnelListWithTrailingDiagnostics(t *testing.T) {
	for _, suffix := range []string{
		`2026-10-22T00:00:00Z WRN Please upgrade cloudflared`,
		`{"level":"warn","message":"Please upgrade cloudflared"}`,
	} {
		items, err := ParseTunnelList(listFixture + suffix)
		if err != nil || len(items) != 2 || items[0].Connections != 1 {
			t.Fatalf("list with trailing diagnostics = %+v, %v", items, err)
		}
	}
	for _, output := range []string{`[{"id":`, `[{"id":"broken",}]`, `not json`} {
		if _, err := ParseTunnelList(output); err == nil {
			t.Fatalf("invalid list accepted: %s", output)
		}
	}
}

func TestParseCreatedTunnelAndReady(t *testing.T) {
	out := "Tunnel credentials written to /Users/x/.cloudflared/03e12345-6789-4abc-8def-0123456789ab.json\nCreated tunnel loris-devspace with id 03e12345-6789-4abc-8def-0123456789ab\n"
	if got := ParseCreatedTunnelID(out); got != "03e12345-6789-4abc-8def-0123456789ab" {
		t.Fatalf("id = %s", got)
	}
	if !ReadyLine("INF Registered tunnel connection connIndex=0") || !ReadyLine("connection registered") {
		t.Fatal("ready lines were missed")
	}
	if ReadyLine("connection disconnected") {
		t.Fatal("disconnect was treated as ready")
	}
}

func TestClassifyCLIFailure(t *testing.T) {
	cases := []struct {
		output string
		state  string
	}{
		{"You need to login. cert.pem not found", ResourceAuth},
		{"lookup api.cloudflare.com: no such host", ResourceNetwork},
		{"i/o timeout", ResourceNetwork},
		{"permission denied", ResourcePermission},
		{"tunnel not found", ResourceNotFound},
		{"An A, AAAA, or CNAME record with that host already exists. code: 1003", ResourceError},
	}
	for _, item := range cases {
		state, detail := classifyCLIFailure(item.output, errors.New("exit status 1"))
		if state != item.state || detail == "" || strings.Contains(detail, "exit status 1") && item.state == ResourceAuth {
			t.Fatalf("%q -> %s %q", item.output, state, detail)
		}
	}
}

func TestNormalizeHostname(t *testing.T) {
	got, err := NormalizeHostname(" HTTPS://DevSpace.Example.com/ ")
	if err != nil || got != "devspace.example.com" {
		t.Fatalf("hostname = %q %v", got, err)
	}
	if _, err := NormalizeHostname("https://devspace.example.com/path"); err == nil {
		t.Fatal("path was accepted")
	}
	if _, err := NormalizeHostname(""); err == nil {
		t.Fatal("empty hostname was accepted")
	}
}
