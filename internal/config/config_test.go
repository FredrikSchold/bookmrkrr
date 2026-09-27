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
	writeConfig(t, "this is not = = toml [")

	cfg := Load()
	if !cfg.NetworkEnabled() {
		t.Error("a malformed config must fall back to the default, not disable the network silently")
	}
	// Falling back is the right call, but doing it in silence means someone who
	// had network = "off" and then broke the file gets the network back with no
	// way to find out.
	if cfg.Problem == "" {
		t.Error("Problem = \"\", want Load to say why it fell back to the defaults")
	}
}

// writeConfig points every config location at one temp dir and writes body to
// config.toml there.
func writeConfig(t *testing.T, body string) {
	t.Helper()
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
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// network = false is what a person writes in TOML when they want it off. As a
// string field it was a type error, so Load fell back to the default and the
// network came back on.
func TestLoadAcceptsABooleanNetworkSetting(t *testing.T) {
	writeConfig(t, "network = false\n")

	cfg := Load()
	if cfg.NetworkEnabled() {
		t.Error("NetworkEnabled() = true, want false for network = false")
	}
	if cfg.Problem != "" {
		t.Errorf("Problem = %q, want none: a boolean is a valid setting, not a mistake", cfg.Problem)
	}
}

func TestLoadAcceptsABooleanTrue(t *testing.T) {
	writeConfig(t, "network = true\n")

	if !Load().NetworkEnabled() {
		t.Error("NetworkEnabled() = false, want true for network = true")
	}
}

// A kill switch that only answers to one exact spelling is a kill switch that
// fails the wrong way for anyone who writes "Off" or "no".
func TestNetworkEnabledMatchesOffCaseInsensitively(t *testing.T) {
	off := []string{"off", "OFF", "Off", " off ", "false", "FALSE", "no", "No"}
	for _, v := range off {
		if (Config{Network: networkMode(v)}).NetworkEnabled() {
			t.Errorf("NetworkEnabled() with network = %q = true, want false", v)
		}
	}
	on := []string{"", "on", "ON", "yes", "true", "something else"}
	for _, v := range on {
		if !(Config{Network: networkMode(v)}).NetworkEnabled() {
			t.Errorf("NetworkEnabled() with network = %q = false, want true", v)
		}
	}
}

func TestLoadReadsAnUppercaseOffFromTheFile(t *testing.T) {
	writeConfig(t, "network = \"OFF\"\n")

	if Load().NetworkEnabled() {
		t.Error("NetworkEnabled() = true, want false for network = \"OFF\"")
	}
}

// A file that does not exist is the ordinary case, not a mistake, so it must not
// produce a warning on every single add.
func TestLoadReportsNoProblemWhenThereIsNoFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BKMR_DATA_DIR", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)

	cfg := Load()
	if cfg.Problem != "" {
		t.Errorf("Problem = %q, want none when no config file exists", cfg.Problem)
	}
	if !cfg.NetworkEnabled() {
		t.Error("NetworkEnabled() = false, want true by default")
	}
}
