package updater

import (
	"context"
	"fmt"
	"runtime"
)

type Result struct {
	HasUpdate      bool   `json:"hasUpdate"`
	LatestVersion  string `json:"latestVersion"`
	ReleaseNotes   string `json:"releaseNotes"`
	DownloadURL    string `json:"downloadUrl"`
	ReleasePageURL string `json:"releasePageUrl"`
}

type Provider interface {
	Check(ctx context.Context, currentVersion, osName, archName string) (Result, error)
}

type Service struct {
	provider Provider
}

func NewService(provider Provider) *Service {
	return &Service{provider: provider}
}

func NewDefaultService() *Service {
	return NewService(NewGithubReleaseProvider())
}

func (s *Service) Check(ctx context.Context, currentVersion string) (Result, error) {
	if s == nil || s.provider == nil {
		return Result{}, fmt.Errorf("update service is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return s.provider.Check(ctx, currentVersion, runtime.GOOS, runtime.GOARCH)
}
