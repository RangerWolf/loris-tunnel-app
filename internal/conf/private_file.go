package conf

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WritePrivateAtomic never truncates the destination, and cleans plaintext temporary files on failure.
func WritePrivateAtomic(path string, data []byte) error { return writePrivateChecked(path, data, nil) }
func writePrivateChecked(path string, data []byte, check func() error) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".loris-private-*")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = protectPrivate(tmp); e != nil {
		f.Close()
		return e
	}
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if check != nil {
		if e = check(); e != nil {
			return e
		}
	}
	return replacePrivate(tmp, path)
}
func (r *Storage) ReadRaw() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, e := os.Open(r.path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	data, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if e != nil {
		return nil, e
	}
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("TOML exceeds 1 MiB")
	}
	return data, nil
}
func (r *Storage) RestoreRaw(data []byte, expected string) error {
	if _, e := ParseConfigTOML(data); e != nil {
		return e
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, e := os.ReadFile(r.path)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(current)
	if hex.EncodeToString(sum[:]) != expected {
		return fmt.Errorf("local configuration changed; confirm again")
	}
	if e = WritePrivateAtomic(r.path+".before-restore", current); e != nil {
		return e
	}
	// The final hash check happens after flushing the temporary file, immediately before replacement.
	check := func() error {
		latest, e := os.ReadFile(r.path)
		if e != nil {
			return e
		}
		sum := sha256.Sum256(latest)
		if hex.EncodeToString(sum[:]) != expected {
			return fmt.Errorf("local configuration changed; confirm again")
		}
		return nil
	}
	if e = writePrivateChecked(r.path, data, check); e != nil {
		return e
	}
	r.runtime = nil
	return nil
}
