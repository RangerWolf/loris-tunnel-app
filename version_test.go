package main

import (
	"testing"

	"loris-tunnel/internal/license"
)

func TestParseProductVersion(t *testing.T) {
	if got := parseProductVersion(wailsConfigJSON); got == "" || got == "unknown" {
		t.Fatalf("embedded product version = %q", got)
	}
	if got := parseProductVersion([]byte(`{"info":{"productVersion":" 9.8.7.6 "}}`)); got != "9.8.7.6" {
		t.Fatalf("trimmed version = %q", got)
	}
	if got := parseProductVersion([]byte(`{"info":{}}`)); got != "unknown" {
		t.Fatalf("missing version = %q", got)
	}
}

func TestUsageAppVersion(t *testing.T) {
	prod := &App{license: license.NewClientByBuildType("production")}
	if got := prod.usageAppVersion(); got != productVersion {
		t.Fatalf("prod usage version = %q, want %q", got, productVersion)
	}

	dev := &App{license: license.NewClientByBuildType("dev")}
	if got := dev.usageAppVersion(); got != productVersion+"-dev" {
		t.Fatalf("dev usage version = %q, want %q", got, productVersion+"-dev")
	}

	if got := (&App{}).usageAppVersion(); got != productVersion {
		t.Fatalf("nil license usage version = %q, want %q", got, productVersion)
	}
}
