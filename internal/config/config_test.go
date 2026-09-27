package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsToNetworkOn(t *testing.T) {
	t.Setenv("BKMR_DATA_DIR", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())

	cfg := Load()
	if !cfg.NetworkEnabled() {
		t.Error("NetworkEnabled() = false, want true by default")
	}
}

func TestLoadReadsNetworkOff(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BKMR_DATA_DIR", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)

	path, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("network = \"off\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if Load().NetworkEnabled() {
		t.Error("NetworkEnabled() = true, want false when the config says off")
	}
}

func TestLoadIgnoresAMalformedConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BKMR_DATA_DIR", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)

	path, _ := ConfigPath()
	os.MkdirAll(filepath.Dir(path), 0o700)
	if err := os.WriteFile(path, []byte("this is not = = toml ["), 0o600); err != nil {
		t.Fatal(err)
	}

	if !Load().NetworkEnabled() {
		t.Error("a malformed config must fall back to the default, not disable the network silently")
	}
}
