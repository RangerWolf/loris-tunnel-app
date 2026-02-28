package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultGithubLatestReleaseAPIURL = "https://api.github.com/repos/RangerWolf/loris-tunnel-app/releases/latest"
	defaultGithubReleasesPageURL     = "https://github.com/RangerWolf/loris-tunnel-app/releases"
	githubAPIRequestTimeout          = 10 * time.Second
)

var (
	semverExtractRe = regexp.MustCompile(`(?i)v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?`)
	semverParseRe   = regexp.MustCompile(`(?i)^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$`)
)

type GithubReleaseProvider struct {
	latestReleaseAPIURL string
	releasesPageURL     string
	httpClient          *http.Client
}

type githubLatestRelease struct {
	TagName string               `json:"tag_name"`
	Name    string               `json:"name"`
	Body    string               `json:"body"`
	HTMLURL string               `json:"html_url"`
	URL     string               `json:"url"`
	Assets  []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type githubErrorResponse struct {
	Message string `json:"message"`
}

type semVersion struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease []string
}

func NewGithubReleaseProvider() *GithubReleaseProvider {
	return &GithubReleaseProvider{
		latestReleaseAPIURL: firstNonEmpty(
			strings.TrimSpace(getenv("LORIS_TUNNEL_GITHUB_LATEST_RELEASE_API_URL")),
			defaultGithubLatestReleaseAPIURL,
		),
		releasesPageURL: firstNonEmpty(
			strings.TrimSpace(getenv("LORIS_TUNNEL_GITHUB_RELEASES_PAGE_URL")),
			defaultGithubReleasesPageURL,
		),
		httpClient: &http.Client{Timeout: githubAPIRequestTimeout},
	}
}

func (p *GithubReleaseProvider) Check(ctx context.Context, currentVersion, osName, archName string) (Result, error) {
	if p == nil {
		return Result{}, fmt.Errorf("GitHub updater provider is nil")
	}
	release, err := p.fetchLatestRelease(ctx)
	if err != nil {
		return Result{}, err
	}

	latestVersion := strings.TrimSpace(release.TagName)
	if latestVersion == "" {
		latestVersion = strings.TrimSpace(release.Name)
	}
	if latestVersion == "" {
		return Result{}, fmt.Errorf("GitHub release response missing version field (tag_name/name)")
	}

	releasePageURL := firstNonEmpty(strings.TrimSpace(p.releasesPageURL), strings.TrimSpace(release.HTMLURL))
	return Result{
		HasUpdate:      isRemoteVersionNewer(currentVersion, latestVersion),
		LatestVersion:  latestVersion,
		ReleaseNotes:   strings.TrimSpace(release.Body),
		DownloadURL:    pickReleaseDownloadURL(release, osName, archName),
		ReleasePageURL: releasePageURL,
	}, nil
}

func (p *GithubReleaseProvider) fetchLatestRelease(ctx context.Context) (githubLatestRelease, error) {
	requestCtx := ctx
	if requestCtx == nil {
		requestCtx = context.Background()
	}

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, p.latestReleaseAPIURL, nil)
	if err != nil {
		return githubLatestRelease{}, fmt.Errorf("create GitHub request failed: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "loris-tunnel-updater")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return githubLatestRelease{}, fmt.Errorf("connect GitHub Releases API failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return githubLatestRelease{}, fmt.Errorf("read GitHub response failed: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload githubErrorResponse
		if err := json.Unmarshal(body, &payload); err == nil && strings.TrimSpace(payload.Message) != "" {
			return githubLatestRelease{}, fmt.Errorf("GitHub API error: %s (HTTP %d)", payload.Message, resp.StatusCode)
		}
		return githubLatestRelease{}, fmt.Errorf("GitHub API request failed (HTTP %d)", resp.StatusCode)
	}

	var latest githubLatestRelease
	if err := json.Unmarshal(body, &latest); err != nil {
		return githubLatestRelease{}, fmt.Errorf("parse GitHub release JSON failed: %w", err)
	}
	return latest, nil
}

func pickReleaseDownloadURL(release githubLatestRelease, osName, archName string) string {
	targetOS := normalizeTargetOS(osName)
	targetArch := normalizeTargetArch(archName)

	bestURL := ""
	bestScore := -1
	for _, asset := range release.Assets {
		if strings.TrimSpace(asset.BrowserDownloadURL) == "" {
			continue
		}
		score := scoreAssetByTarget(asset.Name, targetOS, targetArch)
		if score > bestScore {
			bestScore = score
			bestURL = strings.TrimSpace(asset.BrowserDownloadURL)
		}
	}

	if bestScore > 0 && bestURL != "" {
		return bestURL
	}
	if len(release.Assets) == 1 && strings.TrimSpace(release.Assets[0].BrowserDownloadURL) != "" {
		return strings.TrimSpace(release.Assets[0].BrowserDownloadURL)
	}
	if strings.TrimSpace(release.HTMLURL) != "" {
		return strings.TrimSpace(release.HTMLURL)
	}
	return strings.TrimSpace(release.URL)
}

func scoreAssetByTarget(assetName, osName, archName string) int {
	name := strings.ToLower(strings.TrimSpace(assetName))
	if name == "" {
		return 0
	}

	score := 0
	switch osName {
	case "mac":
		if strings.Contains(name, "darwin") || strings.Contains(name, "mac") || strings.Contains(name, "macos") || strings.Contains(name, "osx") {
			score += 10
		}
		if strings.HasSuffix(name, ".dmg") || strings.HasSuffix(name, ".pkg") || strings.HasSuffix(name, ".zip") {
			score += 2
		}
	case "windows":
		if strings.Contains(name, "windows") || strings.Contains(name, "win32") || strings.Contains(name, "win64") || strings.Contains(name, "win") {
			score += 10
		}
		if strings.HasSuffix(name, ".exe") || strings.HasSuffix(name, ".msi") || strings.HasSuffix(name, ".zip") {
			score += 2
		}
	case "linux":
		if strings.Contains(name, "linux") {
			score += 10
		}
		if strings.HasSuffix(name, ".appimage") || strings.HasSuffix(name, ".deb") || strings.HasSuffix(name, ".rpm") || strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".zip") {
			score += 2
		}
	}

	switch archName {
	case "arm64":
		if strings.Contains(name, "arm64") || strings.Contains(name, "aarch64") {
			score += 6
		}
	case "amd64":
		if strings.Contains(name, "amd64") || strings.Contains(name, "x86_64") || strings.Contains(name, "x64") {
			score += 6
		}
	}
	return score
}

func normalizeTargetOS(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "darwin", "mac", "macos", "osx":
		return "mac"
	case "windows", "win":
		return "windows"
	case "linux":
		return "linux"
	default:
		return ""
	}
}

func normalizeTargetArch(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "arm64", "aarch64":
		return "arm64"
	case "amd64", "x86_64", "x64":
		return "amd64"
	default:
		return ""
	}
}

func isRemoteVersionNewer(currentVersion, remoteVersion string) bool {
	current := normalizeVersionInput(currentVersion)
	remote := normalizeVersionInput(remoteVersion)
	if remote == "" {
		return false
	}
	if current == "" {
		return true
	}
	if current == remote {
		return false
	}

	currentParsed, okCurrent := parseSemver(current)
	remoteParsed, okRemote := parseSemver(remote)
	if !okCurrent || !okRemote {
		return current != remote
	}
	return compareSemver(remoteParsed, currentParsed) > 0
}

func normalizeVersionInput(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	match := semverExtractRe.FindString(value)
	if match != "" {
		return match
	}
	return value
}

func parseSemver(raw string) (semVersion, bool) {
	normalized := strings.TrimSpace(normalizeVersionInput(raw))
	if normalized == "" {
		return semVersion{}, false
	}
	if idx := strings.Index(normalized, "+"); idx >= 0 {
		normalized = normalized[:idx]
	}

	matches := semverParseRe.FindStringSubmatch(normalized)
	if len(matches) != 5 {
		return semVersion{}, false
	}

	major, err := strconv.Atoi(matches[1])
	if err != nil {
		return semVersion{}, false
	}
	minor, err := strconv.Atoi(matches[2])
	if err != nil {
		return semVersion{}, false
	}
	patch, err := strconv.Atoi(matches[3])
	if err != nil {
		return semVersion{}, false
	}

	var prerelease []string
	if strings.TrimSpace(matches[4]) != "" {
		prerelease = strings.Split(matches[4], ".")
	}
	return semVersion{
		Major:      major,
		Minor:      minor,
		Patch:      patch,
		Prerelease: prerelease,
	}, true
}

func compareSemver(a, b semVersion) int {
	if a.Major != b.Major {
		if a.Major > b.Major {
			return 1
		}
		return -1
	}
	if a.Minor != b.Minor {
		if a.Minor > b.Minor {
			return 1
		}
		return -1
	}
	if a.Patch != b.Patch {
		if a.Patch > b.Patch {
			return 1
		}
		return -1
	}

	aHasPre := len(a.Prerelease) > 0
	bHasPre := len(b.Prerelease) > 0
	if !aHasPre && !bHasPre {
		return 0
	}
	if !aHasPre {
		return 1
	}
	if !bHasPre {
		return -1
	}

	maxLen := len(a.Prerelease)
	if len(b.Prerelease) > maxLen {
		maxLen = len(b.Prerelease)
	}
	for i := 0; i < maxLen; i++ {
		if i >= len(a.Prerelease) {
			return -1
		}
		if i >= len(b.Prerelease) {
			return 1
		}
		diff := comparePrereleaseIdentifier(a.Prerelease[i], b.Prerelease[i])
		if diff != 0 {
			return diff
		}
	}
	return 0
}

func comparePrereleaseIdentifier(a, b string) int {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == b {
		return 0
	}

	aInt, aErr := strconv.Atoi(a)
	bInt, bErr := strconv.Atoi(b)
	if aErr == nil && bErr == nil {
		if aInt > bInt {
			return 1
		}
		return -1
	}
	if aErr == nil {
		return -1
	}
	if bErr == nil {
		return 1
	}
	if a > b {
		return 1
	}
	return -1
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
