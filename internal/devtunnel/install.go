package devtunnel

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"loris-tunnel/internal/climaint"
	"os"
	"path/filepath"
	"strings"
)

// Install downloads the official CLI into the user data directory.
// A CLI already present on PATH is left untouched.
func (c *Client) Install(ctx context.Context) error {
	if strings.TrimSpace(c.Binary) != "" {
		return fmt.Errorf("dev tunnel cli install is disabled for a pinned binary")
	}
	c.mu.Lock()
	if c.installing {
		c.mu.Unlock()
		return fmt.Errorf("dev tunnel cli install is already running")
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

func materializeCLI(downloaded, dir string) (string, error) {
	file, err := os.Open(downloaded)
	if err != nil {
		return "", err
	}
	header := make([]byte, 2)
	_, _ = file.Read(header)
	file.Close()
	if string(header) != "PK" {
		return downloaded, nil
	}
	reader, err := zip.OpenReader(downloaded)
	if err != nil {
		return "", fmt.Errorf("downloaded dev tunnel cli is not a recognized archive")
	}
	defer reader.Close()
	var match *zip.File
	for _, item := range reader.File {
		if filepath.Base(item.Name) == binaryName() && !item.FileInfo().IsDir() {
			match = item
			break
		}
	}
	if match == nil {
		return "", fmt.Errorf("downloaded archive does not contain %s", binaryName())
	}
	source, err := match.Open()
	if err != nil {
		return "", err
	}
	defer source.Close()
	destPath := filepath.Join(dir, ".unpack-"+binaryName())
	dest, err := os.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(dest, io.LimitReader(source, 80<<20)); err != nil {
		dest.Close()
		os.Remove(destPath)
		return "", err
	}
	if err := dest.Close(); err != nil {
		os.Remove(destPath)
		return "", err
	}
	return destPath, nil
}
