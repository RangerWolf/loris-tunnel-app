package devtunnel

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializeCLIArchiveKeepsExecutableName(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "download")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create(binaryName())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("cli")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	path, err := materializeCLI(archive, dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != ".unpack-"+binaryName() {
		t.Fatalf("unexpected executable name: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "cli" {
		t.Fatalf("unexpected extracted content: %q", data)
	}
}
