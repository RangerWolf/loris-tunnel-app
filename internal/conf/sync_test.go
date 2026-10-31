package conf

import (
	"loris-tunnel/internal/model"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveDoesNotGenerateSyncIDs(t *testing.T) {
	storage, _ := NewStorage(filepath.Join(t.TempDir(), "config.toml"))
	c, e := storage.Update(func(c *Config) error {
		c.Jumpers = []model.Jumper{{ID: 1, Name: "a"}, {ID: 2, Name: "b", SyncID: "existing"}}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if c.Jumpers[0].SyncID != "" || c.Jumpers[1].SyncID != "existing" {
		t.Fatal("save generated or modified identity")
	}
}
func TestCloneDetachesTunnelReferences(t *testing.T) {
	c := DefaultConfig()
	c.Tunnels = []model.Tunnel{{JumperIDs: []int{1, 2}}}
	clone := c.Clone()
	clone.Tunnels[0].JumperIDs[0] = 99
	if c.Tunnels[0].JumperIDs[0] != 1 {
		t.Fatal("clone shares nested reference")
	}
}

func TestEnvironmentBaselinesPersistCloneAndExport(t *testing.T) {
	s, err := NewStorage(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Update(func(c *Config) error {
		c.SetSyncBaseline("dev", SyncBaseline{Revision: 2})
		c.SetSyncBaseline("prod", SyncBaseline{Revision: 9})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SyncBaselines["dev"].Revision != 2 || loaded.SyncBaselines["prod"].Revision != 9 {
		t.Fatal("environment baselines not persisted")
	}
	clone := loaded.Clone()
	clone.SetSyncBaseline("prod", SyncBaseline{})
	if loaded.SyncBaselines["prod"].Revision != 9 {
		t.Fatal("clone shares baseline map")
	}
	if strings.Contains(string(ExportTOML(loaded)), "sync_baseline") {
		t.Fatal("export leaked environment baselines")
	}
}
