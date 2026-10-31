package devtunnel

import "testing"

func TestParseAccountExpiredJSON(t *testing.T) {
	state, label, detail := ParseAccount("{\n  \"status\": \"Login token expired\"\n}\n", nil)
	if state != "expired" || label != "" || detail != "Login token expired" {
		t.Fatalf("state=%s label=%s detail=%s", state, label, detail)
	}
}

func TestParseAccountSignedIn(t *testing.T) {
	state, label, _ := ParseAccount(`{"login":"octo","provider":"github"}`, nil)
	if state != "signed_in" || label != "octo · github" {
		t.Fatalf("state=%s label=%s", state, label)
	}
}

func TestParseAccountLoggedInStatus(t *testing.T) {
	state, label, detail := ParseAccount(`{"status":"Logged in","provider":"github","username":"RangerWolf"}`, nil)
	if state != "signed_in" || label != "RangerWolf · github" || detail != "" {
		t.Fatalf("state=%s label=%s detail=%s", state, label, detail)
	}
}

func TestClassifyLoginLine(t *testing.T) {
	phase, url := ClassifyLoginLine("Please complete login in the browser: https://login.microsoft.com/device")
	if phase != "browser" || url != "https://login.microsoft.com/device" {
		t.Fatalf("phase=%s url=%s", phase, url)
	}
	phase, url = ClassifyLoginLine("Logged in as RangerWolf")
	if phase != "verifying" || url != "" {
		t.Fatalf("phase=%s url=%s", phase, url)
	}
	phase, _ = ClassifyLoginLine("Not logged in")
	if phase != "" {
		t.Fatal(phase)
	}
}

func TestParseAccountQueryFailureStaysUnknown(t *testing.T) {
	state, _, _ := ParseAccount("", errString("dial timeout"))
	if state != "unknown" {
		t.Fatal(state)
	}
}

func TestParseHostLines(t *testing.T) {
	url, ready, auth := ParseHostLine("Connect via browser: https://abc-3000.devtunnels.ms")
	if url != "https://abc-3000.devtunnels.ms" || ready || auth {
		t.Fatalf("url line: %s %v %v", url, ready, auth)
	}
	url, _, _ = ParseHostLine("Connect via browser: https://abc.usw3.devtunnels.ms:8080, https://abc-8080.usw3.devtunnels.ms")
	if url != "https://abc-8080.usw3.devtunnels.ms" {
		t.Fatalf("preferred url: %s", url)
	}
	_, ready, auth = ParseHostLine("Ready to accept connections for tunnel: abc")
	if !ready || auth {
		t.Fatalf("ready=%v auth=%v", ready, auth)
	}
	_, _, auth = ParseHostLine("Login token expired")
	if !auth {
		t.Fatal("expected auth")
	}
}

func TestParseTunnelID(t *testing.T) {
	if got := ParseTunnelID("noise {\"tunnel\":{\"tunnelId\":\"abc123\"}}"); got != "abc123" {
		t.Fatal(got)
	}
}

func TestPortAlreadyConfigured(t *testing.T) {
	conflict := "Tunnel service error: Conflict with existing entity. Tunnel port number conflicts with an existing port in the tunnel."
	if !portAlreadyConfigured(conflict) {
		t.Fatal("port conflict was treated as a new failure")
	}
	if portAlreadyConfigured("tunnel service unavailable") {
		t.Fatal("unrelated error matched")
	}
}

func TestParseVersion(t *testing.T) {
	got := ParseVersion("Tunnel CLI version: 1.0.2115+e16833f284\n")
	if got != "1.0.2115+e16833f284" {
		t.Fatal(got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestParseHostURLsRetainsBothAddressForms(t *testing.T) {
	urls := ParseHostURLs("Connect via browser: https://abc.usw3.devtunnels.ms:8080, https://abc-8080.usw3.devtunnels.ms, https://abc-8080.usw3.devtunnels.ms")
	if len(urls) != 2 || urls[0] != "https://abc-8080.usw3.devtunnels.ms" || urls[1] != "https://abc.usw3.devtunnels.ms:8080" {
		t.Fatalf("URLs: %+v", urls)
	}
	if urls := ParseHostURLs("inspect: https://abc-inspect.usw3.devtunnels.ms"); len(urls) != 0 {
		t.Fatalf("inspection URL included: %+v", urls)
	}
}
