package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProbeStorageWritable_Success(t *testing.T) {
	tempDir := t.TempDir()
	health := ProbeStorageWritable(tempDir)
	if !health.Writable {
		t.Fatalf("expected writable directory to be true, got error: %s", health.Error)
	}
	if health.Error != "" {
		t.Fatalf("expected empty error, got: %s", health.Error)
	}
}

func TestProbeStorageWritable_ReadOnly(t *testing.T) {
	tempDir := t.TempDir()
	readOnlyDir := filepath.Join(tempDir, "readonly")
	if err := os.MkdirAll(readOnlyDir, 0555); err != nil {
		t.Skipf("cannot set read-only permissions on this filesystem: %v", err)
	}
	defer os.Chmod(readOnlyDir, 0755)

	// In some environments (e.g. running as root), 0555 is still writable
	// We check if WriteFile fails first
	testFile := filepath.Join(readOnlyDir, "test.tmp")
	if err := os.WriteFile(testFile, []byte("x"), 0644); err == nil {
		_ = os.Remove(testFile)
		t.Skip("skipping read-only test: process runs with override write permissions (e.g. root)")
	}

	health := ProbeStorageWritable(readOnlyDir)
	if health.Writable {
		t.Fatal("expected read-only directory to not be writable")
	}
	if health.Error == "" {
		t.Fatal("expected non-empty error for read-only directory")
	}
}

func TestCheckStorageHealth_Cache(t *testing.T) {
	ResetStorageHealthCache()
	defer ResetStorageHealthCache()

	h1 := CheckStorageHealth()
	if !h1.Writable && h1.Error == "" {
		t.Fatal("invalid storage health response")
	}

	// Should return cached within TTL
	h2 := CheckStorageHealth()
	if !h1.CheckedAt.Equal(h2.CheckedAt) {
		t.Fatalf("expected cached CheckedAt %v, got %v", h1.CheckedAt, h2.CheckedAt)
	}

	// Force check should update CheckedAt
	time.Sleep(10 * time.Millisecond)
	h3 := ForceCheckStorageHealth()
	if h3.CheckedAt.Equal(h1.CheckedAt) {
		t.Fatal("expected ForceCheckStorageHealth to refresh checked timestamp")
	}
}
