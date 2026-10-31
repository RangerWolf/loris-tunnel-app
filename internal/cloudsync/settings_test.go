package cloudsync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"loris-tunnel/internal/conf"
)

type accountVault map[string]string

func (v accountVault) Get(account string) (string, error) { return v[account], nil }
func (v accountVault) Set(account, token string) error    { v[account] = token; return nil }
func (v accountVault) Delete(account string) error        { delete(v, account); return nil }

func TestSettingsMigrationRetainsVaultReferenceAndIsolatesDirectories(t *testing.T) {
	legacy := filepath.Join(t.TempDir(), SettingsFileName)
	server, device := "http://127.0.0.1:8000", "device"
	if err := (&Service{path: legacy}).persist(Options{ServerURL: server, SpaceID: "space"}); err != nil {
		t.Fatal(err)
	}
	// Previously saved Email metadata must not invalidate the Token connection.
	legacyData, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(legacyData, &metadata); err != nil {
		t.Fatal(err)
	}
	metadata["email"] = "unused@example.com"
	legacyData, err = json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, legacyData, 0600); err != nil {
		t.Fatal(err)
	}
	vault := accountVault{credentialAccount(legacy, device): "secret-token"}
	baseline := conf.SyncBaseline{SpaceID: "space", ServerURL: server}
	local := &localStore{c: &conf.Config{SyncBaseline: baseline}}
	path := filepath.Join(t.TempDir(), SettingsFileName)
	if err := MigrateLegacy(path, legacy, device, server, baseline); err != nil {
		t.Fatal(err)
	}
	s, err := New(path, device, "Office", vault, local.hooks())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if token, err := s.Token(); err != nil || token != "secret-token" {
		t.Fatal("migration lost token", err)
	}
	// A subsequent directory move carries the reference, not a plaintext token.
	moved := filepath.Join(t.TempDir(), SettingsFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(moved, data, 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(moved, device, "Office", vault, local.hooks())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if token, err := reopened.Token(); err != nil || token != "secret-token" {
		t.Fatal("directory move lost token", err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatal("legacy settings removed", err)
	}
	// A new profile has no baseline and must not inherit the shared connection.
	otherPath := filepath.Join(t.TempDir(), SettingsFileName)
	if err := MigrateLegacy(otherPath, legacy, device, server, conf.SyncBaseline{}); err != nil {
		t.Fatal(err)
	}
	other, err := New(otherPath, device, "Office", vault, (&localStore{c: &conf.Config{}}).hooks())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if other.Status().Connected || other.account == s.account {
		t.Fatal("new directory inherited connection")
	}
	if err := other.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if token, _ := reopened.Token(); token != "secret-token" {
		t.Fatal("other profile deleted token")
	}
	// A different space or server never imports the legacy connection.
	for _, b := range []conf.SyncBaseline{{SpaceID: "other", ServerURL: server}, {SpaceID: "space", ServerURL: "https://other.example.com"}} {
		p := filepath.Join(t.TempDir(), SettingsFileName)
		if err := MigrateLegacy(p, legacy, device, server, b); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatal("mismatched baseline imported", err)
		}
	}
	// Existing target settings always win over legacy settings.
	if err := os.WriteFile(path, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateLegacy(path, legacy, device, server, baseline); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "existing" {
		t.Fatal("existing profile overwritten")
	}
	s.Suspend()
	if err := s.SetPreferences(true, "Office"); err == nil {
		t.Fatal("stopped service rewrote relocated settings")
	}
	if data, _ := os.ReadFile(path); string(data) != "existing" {
		t.Fatal("late settings write changed old directory")
	}
}

func TestEnvironmentSettingsIsolateCredentialsAndBaselines(t *testing.T) {
	dir := t.TempDir()
	vault := accountVault{}
	local := conf.DefaultConfig()
	services := map[string]*Service{}
	for _, environment := range []string{"dev", "prod"} {
		environment := environment
		path := filepath.Join(dir, SettingsFileForEnvironment(environment))
		server := "https://sync.example.com"
		if environment == "dev" {
			server = "http://127.0.0.1:8000"
		}
		account := credentialAccount(path, "device")
		vault[account] = environment + "-token"
		options := Options{Enabled: true, ServerURL: server, SpaceID: environment, Automatic: environment == "prod", Protocol: SchemaVersion, Baseline: conf.SyncBaseline{SpaceID: environment, ServerURL: server, Revision: 3, Fingerprint: "hash"}}
		if err := (&Service{path: path}).persist(options); err != nil {
			t.Fatal(err)
		}
		local.SetSyncBaseline(environment, conf.SyncBaseline{SpaceID: environment, ServerURL: server, Revision: 3, Fingerprint: "hash"})
		service, err := New(path, "device", "Device", vault, Hooks{Snapshot: func() ([]byte, error) { return conf.ExportTOML(local), nil }})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(service.Close)
		if err := service.ConfigureEnvironment(server, "Device"); err != nil {
			t.Fatal(err)
		}
		if service.Status().Error != "" || service.Status().Revision != 3 {
			t.Fatal(service.Status())
		}
		services[environment] = service
	}
	if services["dev"].account == services["prod"].account {
		t.Fatal("environments share credential account")
	}
	if err := services["dev"].Disconnect(); err != nil {
		t.Fatal(err)
	}
	if services["dev"].options.Baseline.Revision != 0 || services["prod"].options.Baseline.Revision != 3 {
		t.Fatal("disconnect changed other baseline")
	}
	if token, err := services["prod"].Token(); err != nil || token != "prod-token" {
		t.Fatal("disconnect deleted other token", err)
	}
	if !services["prod"].Status().Automatic {
		t.Fatal("disconnect changed other preferences")
	}
}

func TestColocatedLegacyConnectionMigratesOnlyMatchingEnvironment(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, SettingsFileName)
	server := "https://sync.example.com"
	if err := (&Service{path: legacy}).persist(Options{SpaceID: "space", ServerURL: server}); err != nil {
		t.Fatal(err)
	}
	dev := filepath.Join(dir, DevSettingsFileName)
	if err := MigrateLegacy(dev, legacy, "device", "http://127.0.0.1:8000", conf.SyncBaseline{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dev); !os.IsNotExist(err) {
		t.Fatal("dev inherited prod connection")
	}
	prod := filepath.Join(dir, ProdSettingsFileName)
	if err := MigrateLegacy(prod, legacy, "device", server, conf.SyncBaseline{}); err != nil {
		t.Fatal(err)
	}
	vault := accountVault{credentialAccount(legacy, "device"): "original-token"}
	service, err := New(prod, "device", "Device", vault, (&localStore{c: conf.DefaultConfig()}).hooks())
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if token, err := service.Token(); err != nil || token != "original-token" {
		t.Fatal("migration lost unsynced connection token", err)
	}
	before, err := os.ReadFile(prod)
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateLegacy(prod, legacy, "other-device", server, conf.SyncBaseline{}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(prod)
	if err != nil || string(before) != string(after) {
		t.Fatal("existing profile overwritten", err)
	}
}

func TestBaselineOnlyFileDoesNotBlockLegacyConnectionMigration(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, SettingsFileName)
	target := filepath.Join(dir, ProdSettingsFileName)
	server := "https://sync.example.com"
	baseline := conf.SyncBaseline{SpaceID: "space", ServerURL: server, Revision: 3}
	if e := (&Service{path: legacy}).persist(Options{ServerURL: server, SpaceID: "space", CredentialAccount: "original-account"}); e != nil {
		t.Fatal(e)
	}
	if e := MigrateBaseline(target, baseline); e != nil {
		t.Fatal(e)
	}
	if e := MigrateLegacy(target, legacy, "device", server, baseline); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(target)
	var options Options
	if e := json.Unmarshal(data, &options); e != nil {
		t.Fatal(e)
	}
	if options.ServerURL != server || options.CredentialAccount != "original-account" || options.Baseline.Revision != 3 {
		t.Fatal("legacy connection or baseline lost")
	}
}
