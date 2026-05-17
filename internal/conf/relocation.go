package conf

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// FileMD5Hex returns the hex-encoded MD5 of the file contents.
func FileMD5Hex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// AtomicCopyFileWithMD5Verify copies src to dst via a temp file next to dst,
// then verifies MD5(src) == MD5(dst) before returning. On verification failure
// the destination temp/partial file is removed.
func AtomicCopyFileWithMD5Verify(srcPath, dstPath string) error {
	srcPath = strings.TrimSpace(srcPath)
	dstPath = strings.TrimSpace(dstPath)
	if srcPath == "" || dstPath == "" {
		return fmt.Errorf("source or destination path is empty")
	}

	srcData, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read source config: %w", err)
	}

	dir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create destination dir: %w", err)
	}

	tmpPath := dstPath + ".relocate.tmp"
	if err := os.WriteFile(tmpPath, srcData, 0o644); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}

	want, err := FileMD5Hex(srcPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("md5 source: %w", err)
	}
	got, err := FileMD5Hex(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("md5 temp copy: %w", err)
	}
	if want != got {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("md5 mismatch after copy (source vs temp)")
	}

	if err := os.Rename(tmpPath, dstPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp to destination: %w", err)
	}

	gotFinal, err := FileMD5Hex(dstPath)
	if err != nil {
		return fmt.Errorf("md5 destination: %w", err)
	}
	if want != gotFinal {
		return fmt.Errorf("md5 mismatch after rename")
	}

	return nil
}

// WriteConfigRootPointer creates or overwrites config.root under anchorDir.
// targetConfigDir must be an absolute directory path.
func WriteConfigRootPointer(anchorDir, targetConfigDir string) error {
	anchorDir = strings.TrimSpace(anchorDir)
	targetConfigDir = strings.TrimSpace(targetConfigDir)
	if anchorDir == "" || targetConfigDir == "" {
		return fmt.Errorf("anchor or target directory is empty")
	}
	absTarget, err := filepath.Abs(targetConfigDir)
	if err != nil {
		return fmt.Errorf("abs target dir: %w", err)
	}
	if !filepath.IsAbs(absTarget) {
		return fmt.Errorf("target config dir must be absolute")
	}
	absTarget = filepath.Clean(absTarget)

	if err := os.MkdirAll(anchorDir, 0o755); err != nil {
		return fmt.Errorf("create anchor dir: %w", err)
	}

	p := filepath.Join(anchorDir, ConfigRootFileName)
	content := absTarget + "\n"
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write config.root temp: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace config.root: %w", err)
	}
	return nil
}

// RemoveConfigRootPointer deletes anchorDir/config.root if it exists.
func RemoveConfigRootPointer(anchorDir string) error {
	anchorDir = strings.TrimSpace(anchorDir)
	if anchorDir == "" {
		return fmt.Errorf("anchor directory is empty")
	}
	p := filepath.Join(anchorDir, ConfigRootFileName)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove config.root: %w", err)
	}
	return nil
}

// TryRemoveFile removes path if it exists; ignores not exist.
func TryRemoveFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
