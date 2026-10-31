package conf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManualStopConfigCompatibilityAndRoundTrip(t *testing.T) {
	for _, field := range []string{"", "manually_stopped = false", "manually_stopped = true"} {
		t.Run(field, func(t *testing.T) {
			raw := "[[tunnels]]\nid = 1\nname = \"test\"\nauto_start = true\nstatus = \"stopped\"\n" + field + "\n"
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			storage, err := NewStorage(path)
			if err != nil {
				t.Fatal(err)
			}
			c, err := storage.Load()
			if err != nil {
				t.Fatal(err)
			}
			want := strings.HasSuffix(field, "true")
			if c.Tunnels[0].ManuallyStopped != want || !c.Tunnels[0].AutoStart {
				t.Fatal("incorrect legacy/default value")
			}
			if _, err := storage.Update(func(*Config) error { return nil }); err != nil {
				t.Fatal(err)
			}
			loaded, err := storage.Load()
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Tunnels[0].ManuallyStopped != want {
				t.Fatal("save/load lost flag")
			}
			exported := ExportTOML(loaded)
			if !strings.Contains(string(exported), "manually_stopped") {
				t.Fatal("export omitted local flag")
			}
			imported, err := ParseConfigTOML(exported)
			if err != nil {
				t.Fatal(err)
			}
			if imported.Tunnels[0].ManuallyStopped != want {
				t.Fatal("export/import lost flag")
			}
			data, err := json.Marshal(imported.Tunnels[0])
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			if fields["manuallyStopped"] != want {
				t.Fatal("backend JSON omitted flag")
			}
		})
	}
}
