package frp

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestImportFormats(t *testing.T) {
	inputs := map[string]string{
		"toml": "serverAddr='frp.example.com'\nserverPort=7000\nuser='alice'\n[auth]\ntoken='secret'\n[transport.tls]\nenable=true\n[[proxies]]\nname='ssh'\ntype='tcp'\nlocalPort=22\nremotePort=6000\n[[proxies]]\nname='web'\ntype='http'\nlocalPort=8080\ncustomDomains=['web.example.com']",
		"json": `{"serverAddr":"frp.example.com","serverPort":7000,"user":"alice","auth":{"token":"secret"},"transport":{"tls":{"enable":true}},"proxies":[{"name":"ssh","type":"tcp","localPort":22,"remotePort":6000},{"name":"web","type":"http","localPort":8080,"customDomains":["web.example.com"]}]}`,
		"yaml": "serverAddr: frp.example.com\nserverPort: 7000\nuser: alice\nauth:\n  token: secret\ntransport:\n  tls:\n    enable: true\nproxies:\n  - name: ssh\n    type: tcp\n    localPort: 22\n    remotePort: 6000\n  - name: web\n    type: http\n    localPort: 8080\n    customDomains: [web.example.com]",
		"ini":  "[common]\nserver_addr=frp.example.com\nserver_port=7000\nuser=alice\ntoken=secret\ntls_enable=true\n[ssh]\ntype=tcp\nlocal_port=22\nremote_port=6000\n[web]\ntype=http\nlocal_port=8080\ncustom_domains=web.example.com",
	}
	var expected *ImportResult
	for _, format := range []string{"toml", "json", "yaml", "ini"} {
		for _, hint := range []string{format, ""} {
			got, err := ParseImport(inputs[format], hint)
			if err != nil {
				t.Fatal(format, err)
			}
			if got.Format != format || len(got.Errors) != 0 || len(got.Proxies) != 2 {
				t.Fatalf("%s: %+v", format, got)
			}
			for _, proxy := range got.Proxies {
				if len(proxy.Errors) > 0 {
					t.Fatalf("%s: %+v", format, proxy)
				}
			}
			if expected == nil {
				expected = got
			} else if !reflect.DeepEqual(expected.Server, got.Server) || !reflect.DeepEqual(expected.Proxies, got.Proxies) {
				t.Fatalf("%s not equivalent", format)
			}
		}
	}
}

func TestImportRejectsUnsupportedAndMalformed(t *testing.T) {
	for _, content := range []string{
		`{"serverAddr":"host","auth":{"token":123},"proxies":[]}`,
		`{"serverAddr":"host","auth":{"method":"oidc"},"proxies":[]}`,
		`{"serverAddr":"host","transport":{"tls":{"certFile":"private"}},"proxies":[]}`,
		`{"serverAddr":"host","includes":["other.toml"],"proxies":[]}`,
		`{"serverAddr":"host","transport":"tls","proxies":[]}`,
	} {
		got, err := ParseImport(content, "json")
		if err != nil || len(got.Errors) == 0 {
			t.Fatalf("expected client error: %v %+v", err, got)
		}
	}
	for _, content := range []string{"", "token = 'secret", "{{ .Envs.SECRET }}", "serverAddr: host\n---\nserverAddr: other"} {
		_, err := ParseImport(content, "")
		if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe/missing error: %v", err)
		}
	}
	got, err := ParseImport(`{"serverAddr":"host","proxies":[
 {"name":"ok","localPort":22,"remotePort":6000},
 {"name":"other","localPort":22,"remotePort":6001},
 {"name":"dup","localPort":22,"remotePort":6000},
 {"name":"udp","type":"udp","localPort":53,"remotePort":6002},
 {"name":"web","type":"http","localPort":80,"customDomains":["a.example.com","b.example.com"]},
 {"name":"plugin","localPort":80,"remotePort":6003,"plugin":{"type":"http_proxy"}},
 {"name":"bad","localPort":22,"remotePort":6004,"type":42}
 ]}`, "json")
	if err != nil {
		t.Fatal(err)
	}
	for i, proxy := range got.Proxies {
		if (i < 2) != (len(proxy.Errors) == 0) {
			t.Fatalf("proxy %d: %+v", i, proxy)
		}
	}
}

func TestImportMatchingKeepsCredentialsLocal(t *testing.T) {
	store := &Store{Path: filepath.Join(t.TempDir(), "frp-local.json")}
	original := Server{Name: "one", Host: "frp.example.com", Port: 7000, User: "alice", Token: "secret", TLS: true}
	saved, err := store.SaveServer(original)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Token != "" {
		t.Fatal("token in saved response")
	}
	original.Host = "FRP.example.com"
	id, err := store.MatchImport(original)
	if err != nil || len(id) != 1 || id[0] != saved.ID {
		t.Fatal(id, err)
	}
	for _, changed := range []Server{
		{Host: original.Host, Port: 7000, User: "alice", Token: "different", TLS: true},
		{Host: original.Host, Port: 7000, User: "bob", Token: "secret", TLS: true},
		{Host: original.Host, Port: 7000, User: "alice", Token: "secret", TLS: false},
		{Host: original.Host, Port: 7000},
	} {
		id, err := store.MatchImport(changed)
		if err != nil || len(id) != 1 || id[0] != saved.ID {
			t.Fatal("incorrect reuse", id, err)
		}
	}
	for _, changed := range []Server{{Host: "other.example.com", Port: 7000}, {Host: original.Host, Port: 7001}} {
		id, err := store.MatchImport(changed)
		if err != nil || len(id) != 0 {
			t.Fatal("matched different endpoint", id, err)
		}
	}
	settings, err := store.Settings()
	if err != nil || settings.Servers[0].Token != "" || !settings.Servers[0].HasToken {
		t.Fatal("credential exposure")
	}
}
