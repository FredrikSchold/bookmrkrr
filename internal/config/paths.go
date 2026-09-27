package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const appDir = "bookmrkrr"

// DataDir returns the directory holding the vault, creating it if needed.
// BKMR_DATA_DIR overrides the platform default; the test suite relies on it.
func DataDir() (string, error) {
	dir := os.Getenv("BKMR_DATA_DIR")
	if dir == "" {
		base, err := platformDataBase()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(base, appDir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create data directory %s: %w", dir, err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is a file, not a directory", dir)
	}
	return dir, nil
}

// ConfigPath returns the full path to config.toml.
func ConfigPath() (string, error) {
	if runtime.GOOS == "linux" {
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".config")
		}
		return filepath.Join(base, appDir, "config.toml"), nil
	}
	if runtime.GOOS == "windows" {
		if base := os.Getenv("APPDATA"); base != "" {
			return filepath.Join(base, appDir, "config.toml"), nil
		}
	}
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

func platformDataBase() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			return base, nil
		}
	case "linux":
		if base := os.Getenv("XDG_DATA_HOME"); base != "" {
			return base, nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	}
	return os.UserConfigDir()
}
