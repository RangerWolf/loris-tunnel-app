package main

import (
	"os"
	"path/filepath"
	"testing"

	"loris-tunnel/internal/biz"
	"loris-tunnel/internal/cloudsync"
	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/model"
)

func syncTestApp(t *testing.T) (*App, *conf.Config) {
	t.Helper()
	storage, e := conf.NewStorage(filepath.Join(t.TempDir(), "config.toml"))
	if e != nil {
		t.Fatal(e)
	}
	c, e := storage.Update(func(c *conf.Config) error {
		c.Jumpers = []model.Jumper{
			{
				ID:                  1,
				Name:                "SSH",
				Host:                "host",
				Port:                22,
				User:                "user",
				AuthType:            "password",
				Password:            "local-secret",
				KeepAliveIntervalMs: 5000,
				TimeoutMs:           10000,
			},
		}
		c.Groups = []model.TunnelGroup{{ID: 1, Name: "Work"}}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return &App{
		storage: storage,
		jumper:  biz.NewJumperBiz(storage),
		group:   biz.NewGroupBiz(storage),
		tunnel:  biz.NewTunnelBiz(storage),
	}, c
}

func TestWholeFileRestoreAndRace(t *testing.T) {
	a, _ := syncTestApp(t)
	defer a.tunnel.Shutdown()
	before, e := a.storage.ReadRaw()
	if e != nil {
		t.Fatal(e)
	}
	data := []byte("# preserved comment\nversion = 1\n[[jumpers]]\nid=1\nname='SSH'\nhost='new-host'\nport=22\nuser='user'\nauth_type='password'\npassword='restored-secret'\nkey_path='C:\\keys\\id_rsa'\n")
	if e = a.applyCloudConfig(data, cloudsync.Fingerprint(before)); e != nil {
		t.Fatal(e)
	}
	actual, _ := a.storage.ReadRaw()
	if string(actual) != string(data) {
		t.Fatal("raw TOML changed")
	}
	backup, _ := os.ReadFile(a.storage.Path() + ".before-restore")
	if string(backup) != string(before) {
		t.Fatal("before-restore copy missing")
	}
	if e = a.applyCloudConfig(before, cloudsync.Fingerprint(before)); e == nil {
		t.Fatal("stale confirmation accepted")
	}
	actual, _ = a.storage.ReadRaw()
	if string(actual) != string(data) {
		t.Fatal("failed restore changed active file")
	}
}

func TestRestoringConfigDoesNotChangeCloudSyncPreference(t *testing.T) {
	a, _ := syncTestApp(t)
	defer a.tunnel.Shutdown()
	path := filepath.Join(t.TempDir(), cloudsync.DevSettingsFileName)
	service, err := cloudsync.New(path, "device", "Office", nil, cloudsync.Hooks{Snapshot: a.storage.ReadRaw})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	a.cloudSync = service
	for _, enabled := range []bool{true, false} {
		if _, err := service.SetEnabled(enabled); err != nil {
			t.Fatal(err)
		}
		settingsBefore, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before, err := a.storage.ReadRaw()
		if err != nil {
			t.Fatal(err)
		}
		data := []byte("# restored config\nversion=1\n")
		if enabled {
			if err := a.applyCloudConfig(data, cloudsync.Fingerprint(before)); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := a.applyCloudConfig(data, cloudsync.Fingerprint(before)); err == nil {
				t.Fatal("disabled cloud restore accepted")
			}
			// Local restoration is still available independently of Cloud Sync.
			if err := a.storage.RestoreRaw(data, cloudsync.Fingerprint(before)); err != nil {
				t.Fatal(err)
			}
		}
		settingsAfter, err := os.ReadFile(path)
		if err != nil || string(settingsBefore) != string(settingsAfter) || service.Status().Enabled != enabled {
			t.Fatal("config restore changed local cloud sync preference", err)
		}
	}
}
