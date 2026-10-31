package cloudflared

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"loris-tunnel/internal/climaint"
	"os"
	"path/filepath"
	"strings"
)

const maxDownloadBytes = 80 << 20

// Install downloads cloudflared into ~/.loris-tunnel/bin.
// A CLI already present on PATH is left untouched.
func (c *Client) Install(ctx context.Context) error {
	if strings.TrimSpace(c.Binary) != "" {
		return fmt.Errorf("cloudflared install is disabled for a pinned binary")
	}
	c.mu.Lock()
	if c.installing {
		c.mu.Unlock()
		return fmt.Errorf("cloudflared install is already running")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	c.installing = true
	c.installCancel = cancel
	c.mu.Unlock()
	defer func() {
		cancel()
		c.mu.Lock()
		c.installing = false
		c.installCancel = nil
		c.mu.Unlock()
	}()

	manager := climaint.New(c.MaintenanceSpec())
	status, err := manager.Check(ctx, true)
	if err != nil {
		return err
	}
	_, err = manager.Apply(ctx, status.CandidateID, func() (func(), error) { return func() {}, nil })
	return err
}

// CancelInstall stops an in-progress download.
func (c *Client) CancelInstall() {
	c.mu.Lock()
	cancel := c.installCancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// reqURL replaces the release URL in tests.
var reqURL string

func materializeCLI(downloaded, dir string) (string, error) {
	file, err := os.Open(downloaded)
	if err != nil {
		return "", err
	}
	header := make([]byte, 2)
	_, _ = file.Read(header)
	file.Close()
	switch string(header) {
	case "PK":
		return unpackZip(downloaded, dir)
	default:
		if header[0] == 0x1f && header[1] == 0x8b {
			return unpackTarGz(downloaded, dir)
		}
	}
	info, err := os.Stat(downloaded)
	if err != nil || info.Size() == 0 {
		return "", fmt.Errorf("downloaded cloudflared is empty")
	}
	return downloaded, nil
}

func unpackTarGz(downloaded, dir string) (string, error) {
	file, err := os.Open(downloaded)
	if err != nil {
		return "", err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return "", fmt.Errorf("downloaded cloudflared is not a recognized archive")
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	destPath := filepath.Join(dir, ".cloudflared-unpack")
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if header.FileInfo().IsDir() || filepath.Base(header.Name) != "cloudflared" {
			continue
		}
		if err := writeLimited(destPath, reader); err != nil {
			return "", err
		}
		return destPath, nil
	}
	return "", fmt.Errorf("downloaded archive does not contain cloudflared")
}

func unpackZip(downloaded, dir string) (string, error) {
	reader, err := zip.OpenReader(downloaded)
	if err != nil {
		return "", fmt.Errorf("downloaded cloudflared is not a recognized archive")
	}
	defer reader.Close()
	for _, item := range reader.File {
		name := filepath.Base(item.Name)
		if item.FileInfo().IsDir() || (name != binaryName() && name != "cloudflared" && name != "cloudflared.exe") {
			continue
		}
		source, err := item.Open()
		if err != nil {
			return "", err
		}
		destPath := filepath.Join(dir, ".cloudflared-unpack")
		err = writeLimited(destPath, source)
		source.Close()
		if err != nil {
			return "", err
		}
		return destPath, nil
	}
	return "", fmt.Errorf("downloaded archive does not contain %s", binaryName())
}

func writeLimited(destPath string, source io.Reader) error {
	dest, err := os.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dest, io.LimitReader(source, maxDownloadBytes)); err != nil {
		dest.Close()
		os.Remove(destPath)
		return err
	}
	if err := dest.Close(); err != nil {
		os.Remove(destPath)
		return err
	}
	return nil
}
