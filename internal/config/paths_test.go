package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataDirHonorsEnvOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "nested", "vaultdir")
	t.Setenv("BKMR_DATA_DIR", want)

	got, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir() error = %v", err)
	}
	if got != want {
		t.Errorf("DataDir() = %q, want %q", got, want)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("DataDir() did not create the directory: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("DataDir() is not a directory")
	}
}

func TestDataDirRejectsOverrideThatIsAFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BKMR_DATA_DIR", f)

	if _, err := DataDir(); err == nil {
		t.Fatal("DataDir() error = nil, want an error when the path is a file")
	}
}

func TestPlatformDataBaseIsNotEmpty(t *testing.T) {
	got, err := platformDataBase()
	if err != nil {
		t.Fatalf("platformDataBase() error = %v", err)
	}
	if got == "" {
		t.Error("platformDataBase() = empty string, want a platform default")
	}
}
