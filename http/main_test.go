package main

import (
	"os"
	"path/filepath"
	"testing"

	kvix "kvix"
	common "kvix/common"
)

func TestResolveDataDirUsesConfiguredDir(t *testing.T) {
	configuredDir := filepath.Join(t.TempDir(), "kvix-data")
	if err := os.MkdirAll(configuredDir, 0o755); err != nil {
		t.Fatalf("failed to create configured dir: %v", err)
	}
	t.Setenv(httpDataDirEnvKey, configuredDir)

	dir, cleanup, err := resolveDataDir()
	if err != nil {
		t.Fatalf("resolveDataDir returned error: %v", err)
	}
	if dir != configuredDir {
		t.Fatalf("expected configured dir %q, got %q", configuredDir, dir)
	}

	cleanup()
	if _, err := os.Stat(configuredDir); err != nil {
		t.Fatalf("configured dir should remain after cleanup, got err=%v", err)
	}
}

func TestResolveDataDirCreatesTempDirByDefault(t *testing.T) {
	t.Setenv(httpDataDirEnvKey, "")

	dir, cleanup, err := resolveDataDir()
	if err != nil {
		t.Fatalf("resolveDataDir returned error: %v", err)
	}
	if dir == "" {
		t.Fatal("expected non-empty temp dir")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected temp dir to exist, got err=%v", err)
	}

	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected temp dir to be removed after cleanup, got err=%v", err)
	}
}

func TestOpenConfiguredDBPersistsDataWithConfiguredDir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "persistent-data")
	t.Setenv(httpDataDirEnvKey, dataDir)

	db, cleanup, err := openConfiguredDB()
	if err != nil {
		t.Fatalf("openConfiguredDB returned error: %v", err)
	}

	if err := db.Put([]byte("name"), []byte("alice")); err != nil {
		t.Fatalf("failed to write test key: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("failed to close configured db: %v", err)
	}

	cleanup()

	reopenOptions := common.DefaultOptions
	reopenOptions.DirPath = dataDir
	reopened, err := kvix.Open(reopenOptions)
	if err != nil {
		t.Fatalf("failed to reopen persistent dir: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	value, err := reopened.Get([]byte("name"))
	if err != nil {
		t.Fatalf("failed to read persisted key: %v", err)
	}
	if string(value) != "alice" {
		t.Fatalf("expected persisted value %q, got %q", "alice", string(value))
	}
}
