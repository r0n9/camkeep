package task

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareGo2rtcConfigMigratesLegacyFile(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "go2rtc.yaml")
	configPath := filepath.Join(dir, "config", "go2rtc.yaml")
	legacyContent := []byte("streams:\n  old: rtsp://example/live\n")

	if err := os.WriteFile(legacyPath, legacyContent, 0600); err != nil {
		t.Fatal(err)
	}

	if err := prepareGo2rtcConfig(legacyPath, configPath); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(legacyContent) {
		t.Fatalf("expected migrated content %q, got %q", legacyContent, got)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("expected migrated mode 0600, got %v", info.Mode().Perm())
	}
}

func TestPrepareGo2rtcConfigDoesNotOverwriteExistingConfig(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "go2rtc.yaml")
	configPath := filepath.Join(dir, "config", "go2rtc.yaml")

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte("legacy\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("current\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := prepareGo2rtcConfig(legacyPath, configPath); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "current\n" {
		t.Fatalf("expected existing config to be preserved, got %q", got)
	}
}

func TestPrepareGo2rtcConfigAllowsMissingLegacyFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config", "go2rtc.yaml")

	if err := prepareGo2rtcConfig(filepath.Join(dir, "go2rtc.yaml"), configPath); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Dir(configPath)); err != nil {
		t.Fatalf("expected config directory to exist: %v", err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("expected config file to remain absent, got err=%v", err)
	}
}
