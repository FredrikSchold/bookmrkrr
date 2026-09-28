package config

import (
	"os"
	"path/filepath"
	"strings"
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

// A misspelled key is valid TOML, so it parses cleanly and leaves Network at its
// default. Without the metadata check that is a kill switch that does nothing,
// with no feedback at all - the same defect as a malformed file, by a route that
// looks like success.
func TestLoadReportsAnUnrecognizedKey(t *testing.T) {
	writeConfig(t, "netwrok = \"off\"\n")

	cfg := Load()
	if !cfg.NetworkEnabled() {
		t.Error("NetworkEnabled() = false; a misspelled key cannot be honored, so the default must stand")
	}
	if !strings.Contains(cfg.Problem, "netwrok") {
		t.Errorf("Problem = %q, want it to name the unrecognized key", cfg.Problem)
	}
}

// An unrecognized key is a reported problem, not an error, so everything the
// file got right still applies.
func TestLoadKeepsValidSettingsAlongsideAnUnrecognizedKey(t *testing.T) {
	writeConfig(t, "network = \"off\"\nnetwrok = \"on\"\n")

	cfg := Load()
	if cfg.NetworkEnabled() {
		t.Error("NetworkEnabled() = true, want false: network = \"off\" is still a valid setting")
	}
	if !strings.Contains(cfg.Problem, "netwrok") {
		t.Errorf("Problem = %q, want it to name the unrecognized key", cfg.Problem)
	}
}

func TestLoadReportsNoProblemForAConfigItFullyUnderstands(t *testing.T) {
	writeConfig(t, "network = \"off\"\n")

	if p := Load().Problem; p != "" {
		t.Errorf("Problem = %q, want none for a config with nothing wrong with it", p)
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
