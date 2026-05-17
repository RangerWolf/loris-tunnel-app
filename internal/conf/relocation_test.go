package conf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicCopyFileWithMD5Verify(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.toml")
	dst := filepath.Join(dir, "sub", "b.toml")
	content := []byte("version = 1\njumpers = []\n")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AtomicCopyFileWithMD5Verify(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("content mismatch")
	}
}

func TestWriteAndRemoveConfigRootPointer(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "data")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	anchor := filepath.Join(dir, "anchor")
	if err := WriteConfigRootPointer(anchor, target); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(anchor, ConfigRootFileName)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		t.Fatal(err)
	}
	gotLine := strings.TrimSpace(string(data))
	if filepath.Clean(gotLine) != filepath.Clean(absTarget) {
		t.Fatalf("unexpected pointer content: %q want %q", gotLine, absTarget)
	}
	if err := RemoveConfigRootPointer(anchor); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("pointer should be removed")
	}
}
