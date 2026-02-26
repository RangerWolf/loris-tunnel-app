package conf

import (
	"os"
	"path/filepath"
	"testing"

	"loris-tunnel/internal/model"
)

func TestStorage_LoadUpdate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "loris-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.toml")
	s, err := NewStorage(configPath)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Initial load (should create default)
	cfg, err := s.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Version != currentConfigVersion {
		t.Errorf("expected version %d, got %d", currentConfigVersion, cfg.Version)
	}

	// 2. Update config
	_, err = s.Update(func(c *Config) error {
		c.Jumpers = append(c.Jumpers, model.Jumper{
			ID:   1,
			Name: "Test Jumper",
			Host: "localhost",
		})
		return nil
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// 3. Load again and verify
	cfg2, err := s.Load()
	if err != nil {
		t.Fatalf("Load 2 failed: %v", err)
	}
	if len(cfg2.Jumpers) != 1 {
		t.Errorf("expected 1 jumper, got %d", len(cfg2.Jumpers))
	}
	if cfg2.Jumpers[0].Name != "Test Jumper" {
		t.Errorf("expected 'Test Jumper', got %s", cfg2.Jumpers[0].Name)
	}

	// 4. Verify file content manually (basic check)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !contains(content, "[[jumpers]]") {
		t.Errorf("file missing [[jumpers]] section: %s", content)
	}
	if !contains(content, "name = \"Test Jumper\"") {
		t.Errorf("file missing jumper name: %s", content)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
