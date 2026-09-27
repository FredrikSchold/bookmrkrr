package store

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/FredrikSchold/bookmrkrr/internal/crypto"
	"github.com/FredrikSchold/bookmrkrr/internal/model"
)

func newTestVault(t *testing.T) (*Vault, string) {
	t.Helper()
	dir := t.TempDir()
	salt, err := crypto.NewSalt()
	if err != nil {
		t.Fatal(err)
	}
	key := crypto.DeriveKey([]byte("pw"), salt, crypto.Params{Time: 1, MemoryKiB: 8 * 1024, Threads: 1})
	if err := Create(dir, key, salt); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return New(dir, key), dir
}

func TestCreateThenLoadGivesAnEmptyCollection(t *testing.T) {
	v, dir := newTestVault(t)

	if !Exists(dir) {
		t.Fatal("Exists() = false after Create()")
	}
	c, err := v.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.Version != model.Version {
		t.Errorf("Version = %d, want %d", c.Version, model.Version)
	}
	if len(c.Bookmarks) != 0 {
		t.Errorf("len(Bookmarks) = %d, want 0", len(c.Bookmarks))
	}
}

func TestCreateRefusesToOverwriteAnExistingVault(t *testing.T) {
	_, dir := newTestVault(t)
	salt, _ := crypto.NewSalt()
	key := make([]byte, crypto.KeyLen)

	if err := Create(dir, key, salt); err == nil {
		t.Fatal("Create() error = nil, want an error on an existing vault")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	v, _ := newTestVault(t)
	c, _ := v.Load()
	c.Add(model.Bookmark{URL: "https://example.com", Tags: []string{"rust"}})

	if err := v.Save(c); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := v.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got.Bookmarks) != 1 || got.Bookmarks[0].URL != "https://example.com" {
		t.Fatalf("Load() = %+v, want one bookmark for example.com", got.Bookmarks)
	}
}

func TestSaveLeavesTheOldVaultAsBackup(t *testing.T) {
	v, dir := newTestVault(t)
	c, _ := v.Load()
	c.Add(model.Bookmark{URL: "https://first.example"})
	if err := v.Save(c); err != nil {
		t.Fatal(err)
	}
	afterFirst, err := os.ReadFile(v.Path())
	if err != nil {
		t.Fatal(err)
	}

	c.Add(model.Bookmark{URL: "https://second.example"})
	if err := v.Save(c); err != nil {
		t.Fatal(err)
	}

	backup, err := os.ReadFile(filepath.Join(dir, "vault.bkmr.bak"))
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != string(afterFirst) {
		t.Error("backup does not match the previous vault contents")
	}
	if _, err := os.Stat(v.Path() + ".tmp"); !os.IsNotExist(err) {
		t.Error("Save() left a .tmp file behind")
	}
}

func TestLoadWithTheWrongKeyFails(t *testing.T) {
	v, dir := newTestVault(t)
	wrong := New(dir, make([]byte, crypto.KeyLen))

	if _, err := wrong.Load(); !errors.Is(err, crypto.ErrBadVault) {
		t.Errorf("Load() error = %v, want ErrBadVault", err)
	}
	_ = v
}

func TestLoadOnATruncatedVaultFails(t *testing.T) {
	v, _ := newTestVault(t)
	if err := os.WriteFile(v.Path(), []byte("BMRK1"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := v.Load(); !errors.Is(err, crypto.ErrBadVault) {
		t.Errorf("Load() error = %v, want ErrBadVault", err)
	}
}

func TestSaveReportsBusyWhenTheLockIsHeld(t *testing.T) {
	v, dir := newTestVault(t)
	lock, err := os.OpenFile(filepath.Join(dir, "vault.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(lock.Name())
	lock.Close()

	c := &model.Collection{Version: model.Version}
	if err := v.Save(c); !errors.Is(err, ErrBusy) {
		t.Errorf("Save() error = %v, want ErrBusy", err)
	}
}

// Review Focus 5: two writers must not lose each other's bookmark.
func TestMutateSerializesConcurrentWriters(t *testing.T) {
	v, _ := newTestVault(t)

	var wg sync.WaitGroup
	urls := []string{"https://a.example", "https://b.example", "https://c.example", "https://d.example"}
	for _, u := range urls {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			for attempt := 0; attempt < 50; attempt++ {
				err := v.Mutate(func(c *model.Collection) error {
					_, _, err := c.Add(model.Bookmark{URL: u})
					return err
				})
				if err == nil {
					return
				}
				if !errors.Is(err, ErrBusy) {
					t.Errorf("Mutate() error = %v", err)
					return
				}
			}
			t.Errorf("Mutate(%s) never acquired the lock", u)
		}(u)
	}
	wg.Wait()

	c, err := v.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Bookmarks) != len(urls) {
		t.Errorf("len(Bookmarks) = %d, want %d - a concurrent write was lost", len(c.Bookmarks), len(urls))
	}
}
