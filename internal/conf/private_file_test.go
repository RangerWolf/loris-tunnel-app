package conf

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPrivateRestoreKeepsRawFileAndCleansFailures(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	s, _ := NewStorage(path)
	original := []byte("# original comment\nversion=1\n")
	if e := WritePrivateAtomic(path, original); e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(original)
	hash := hex.EncodeToString(h[:])
	restored := []byte("# restored\nversion=2\n[license]\ncode='secret'\n")
	if e := s.RestoreRaw(restored, hash); e != nil {
		t.Fatal(e)
	}
	got, _ := s.ReadRaw()
	if !bytes.Equal(got, restored) {
		t.Fatal("raw bytes modified")
	}
	backup, _ := os.ReadFile(path + ".before-restore")
	if !bytes.Equal(backup, original) {
		t.Fatal("backup changed")
	}
	if runtime.GOOS != "windows" {
		for _, name := range []string{path, path + ".before-restore"} {
			info, _ := os.Stat(name)
			if info.Mode().Perm() != 0600 {
				t.Fatal("plaintext permissions", info.Mode())
			}
		}
	}
	// Force replacement failure. Temporary plaintext must not survive it.
	blocked := filepath.Join(dir, "blocked")
	os.Mkdir(blocked, 0700)
	if e := WritePrivateAtomic(blocked, []byte("secret")); e == nil {
		t.Fatal("write failure hidden")
	}
	matches, _ := filepath.Glob(filepath.Join(dir, ".loris-private-*"))
	if len(matches) > 0 {
		t.Fatal("plaintext temp left behind")
	}
	// Simulate an editor changing the file after the replacement temp was flushed.
	if e := writePrivateChecked(path, original, func() error { os.WriteFile(path, []byte("version=3\n"), 0600); return fmt.Errorf("file changed") }); e == nil {
		t.Fatal("late change ignored")
	}
	actual, _ := s.ReadRaw()
	if string(actual) != "version=3\n" {
		t.Fatal("late external edit overwritten")
	}
	if e := s.RestoreRaw(restored, hash); e == nil {
		t.Fatal("stale hash accepted")
	}
}
func TestRuntimeUpdatesNeverRewriteTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	s, _ := NewStorage(path)
	data := []byte("# byte stable\nversion=1\n[[tunnels]]\nid=1\nname='test'\nstatus='stopped'\n")
	if e := WritePrivateAtomic(path, data); e != nil {
		t.Fatal(e)
	}
	if _, e := s.UpdateRuntime(func(c *Config) error { c.Tunnels[0].Status = "running"; return nil }); e != nil {
		t.Fatal(e)
	}
	c, _ := s.Load()
	if c.Tunnels[0].Status != "running" {
		t.Fatal("runtime state lost")
	}
	actual, _ := s.ReadRaw()
	if !bytes.Equal(data, actual) {
		t.Fatal("runtime rewrote file")
	}
	if _, e := s.Update(func(c *Config) error {
		c.Tunnels[0].Status = "stopped"
		c.Tunnels[0].ManuallyStopped = true
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	c, _ = s.Load()
	if c.Tunnels[0].Status != "stopped" || !c.Tunnels[0].ManuallyStopped {
		t.Fatal("runtime overlay masked persisted user choice")
	}

}
