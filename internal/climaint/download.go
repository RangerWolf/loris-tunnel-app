package climaint

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func docsURL(provider string) string {
	switch provider {
	case "devtunnel":
		return "https://learn.microsoft.com/en-us/azure/developer/dev-tunnels/get-started"
	case "cloudflare":
		return "https://github.com/cloudflare/cloudflared/releases"
	case "frp":
		return "https://github.com/fatedier/frp/releases"
	}
	return ""
}
func devURL() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		return "https://aka.ms/TunnelsCliDownload/osx-arm64-zip", nil
	case "darwin/amd64":
		return "https://aka.ms/TunnelsCliDownload/osx-x64-zip", nil
	case "windows/amd64":
		return "https://aka.ms/TunnelsCliDownload/win-x64", nil
	case "linux/amd64":
		return "https://aka.ms/TunnelsCliDownload/linux-x64", nil
	default:
		return "", fmt.Errorf("Automatic CLI downloads are unavailable for this platform; import a local executable")
	}
}
func assetName(provider, version string) (string, error) {
	osName, arch := runtime.GOOS, runtime.GOARCH
	if arch != "arm64" && arch != "amd64" {
		return "", fmt.Errorf("Automatic CLI downloads are unavailable for this architecture")
	}
	switch provider {
	case "cloudflare":
		switch osName {
		case "darwin":
			return "cloudflared-darwin-" + arch + ".tgz", nil
		case "windows":
			return "cloudflared-windows-" + arch + ".exe", nil
		case "linux":
			return "cloudflared-linux-" + arch, nil
		}
	case "frp":
		ext := "tar.gz"
		if osName == "windows" {
			ext = "zip"
		}
		return fmt.Sprintf("frp_%s_%s_%s.%s", version, osName, arch, ext), nil
	}
	return "", fmt.Errorf("Unsupported CLI platform")
}
func request(ctx context.Context, method, address string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "loris-tunnel-cli-maintenance")
	resp, err := (&http.Client{Timeout: 3 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			return nil, fmt.Errorf("Update check was denied or rate limited; retry later")
		}
		return nil, fmt.Errorf("CLI download/check returned HTTP %d", resp.StatusCode)
	}
	return resp, nil
}
func (m *Manager) latest(ctx context.Context, cached candidate) (candidate, error) {
	item := candidate{ID: newID(), Kind: "release", CheckedAt: time.Now()}
	if m.Spec.DirectURL != "" && m.Spec.Provider != "devtunnel" {
		item.URL = m.Spec.DirectURL
		if err := m.prepare(ctx, &item); err != nil {
			return item, err
		}
		return item, nil
	}
	if m.Spec.Provider == "devtunnel" {
		address := m.Spec.DirectURL
		if address == "" {
			var err error
			address, err = devURL()
			if err != nil {
				return item, err
			}
		}
		item.URL = address
		// ETag identifies a previously inspected official package, not a version number.
		resp, err := request(ctx, http.MethodHead, address)
		if err == nil {
			item.ETag = resp.Header.Get("ETag")
			resp.Body.Close()
			if cached.Kind == "release" && cached.URL == address && item.ETag != "" && item.ETag == cached.ETag {
				if _, e := m.preparedPath(cached); e == nil {
					cached.CheckedAt = item.CheckedAt
					return cached, nil
				}
			}
		}
		if err = m.prepare(ctx, &item); err != nil {
			return item, err
		}
		return item, nil
	}
	repo := "cloudflare/cloudflared"
	if m.Spec.Provider == "frp" {
		repo = "fatedier/frp"
	}
	apiURL := m.Spec.ReleaseAPI
	if apiURL == "" {
		apiURL = "https://api.github.com/repos/" + repo + "/releases/latest"
	}
	resp, err := request(ctx, http.MethodGet, apiURL)
	if err != nil {
		return item, err
	}
	defer resp.Body.Close()
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&release) != nil || release.Tag == "" || release.Draft || release.Prerelease {
		return item, fmt.Errorf("No valid stable CLI release is available")
	}
	item.Version = strings.TrimPrefix(release.Tag, "v")
	item.Asset, err = assetName(m.Spec.Provider, item.Version)
	if err != nil {
		return item, err
	}
	for _, asset := range release.Assets {
		// Only the official repository's versioned downloads may be selected.
		if m.Spec.ReleaseAPI == "" && !strings.HasPrefix(asset.URL, "https://github.com/"+repo+"/releases/download/") {
			continue
		}
		if asset.Name == item.Asset {
			item.URL = asset.URL
			item.Checksum = strings.TrimPrefix(asset.Digest, "sha256:")
			if !strings.HasPrefix(asset.Digest, "sha256:") {
				item.Checksum = ""
			}
		}
		if asset.Name == "frp_sha256_checksums.txt" || asset.Name == "checksums.txt" || asset.Name == "cloudflared-checksums.txt" {
			item.ChecksumURL = asset.URL
		}
	}
	if item.URL == "" {
		return item, fmt.Errorf("The latest release has no download for this system and architecture")
	}
	if m.Spec.Provider == "frp" && item.Checksum == "" && item.ChecksumURL == "" {
		return item, fmt.Errorf("Official FRP checksums are missing")
	}
	return item, nil
}
func (m *Manager) prepare(ctx context.Context, item *candidate) error {
	if item.ChecksumURL != "" && item.Checksum == "" {
		resp, err := request(ctx, http.MethodGet, item.ChecksumURL)
		if err != nil {
			return err
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
		resp.Body.Close()
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == item.Asset {
				item.Checksum = fields[0]
				break
			}
		}
		if len(item.Checksum) != 64 {
			return fmt.Errorf("Official checksum for the selected CLI download is missing")
		}
	}
	if err := os.MkdirAll(directory(m.Spec.Target), 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(directory(m.Spec.Target), ".download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	m.phase("downloading")
	resp, err := request(ctx, http.MethodGet, item.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return fmt.Errorf("The official CLI download returned a web page instead of an executable")
	}
	if m.Spec.Provider == "devtunnel" && resp.Header.Get("ETag") != "" {
		// The GET may observe a newer deployment than the preceding HEAD.
		item.ETag = resp.Header.Get("ETag")
	}
	if resp.ContentLength > maxBytes {
		return fmt.Errorf("CLI download exceeds the size limit")
	}
	pattern := "package-*"
	if filepath.Ext(m.Spec.Target) == ".exe" {
		pattern += ".exe"
	}
	file, err := os.CreateTemp(stage, pattern)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.status.Total = resp.ContentLength
	m.mu.Unlock()
	written, copyErr := io.Copy(file, io.LimitReader(&progressReader{reader: resp.Body, manager: m}, maxBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written == 0 || written > maxBytes {
		return fmt.Errorf("CLI download is empty or too large")
	}
	raw, err := os.ReadFile(file.Name())
	if err != nil {
		return err
	}
	if item.Checksum != "" && !strings.EqualFold(item.Checksum, digest(raw)) {
		return fmt.Errorf("CLI download checksum mismatch")
	}
	m.phase("validating")
	binary, err := m.Spec.Unpack(file.Name(), stage)
	if err != nil {
		return err
	}
	info, err := os.Stat(binary)
	if err != nil {
		return err
	}
	if info.Size() >= maxBytes {
		return fmt.Errorf("Unpacked CLI executable exceeds the size limit")
	}
	if err = os.Chmod(binary, 0700); err != nil {
		return err
	}
	actual, err := m.Spec.Inspect(ctx, binary)
	if err != nil {
		return err
	}
	if item.Version != "" && actual != item.Version {
		return fmt.Errorf("Downloaded CLI version differs from the selected release; check again")
	}
	item.Version = actual
	raw, err = readBinary(binary)
	if err != nil {
		return err
	}
	item.Digest = digest(raw)
	item.File = "candidate-" + item.ID + filepath.Ext(m.Spec.Target)
	if err = ctx.Err(); err != nil {
		return err
	}
	return writeExecutable(filepath.Join(directory(m.Spec.Target), item.File), raw)
}

type progressReader struct {
	reader  io.Reader
	manager *Manager
}

func (p *progressReader) Read(raw []byte) (int, error) {
	n, err := p.reader.Read(raw)
	p.manager.mu.Lock()
	p.manager.status.Downloaded += int64(n)
	p.manager.mu.Unlock()
	return n, err
}
