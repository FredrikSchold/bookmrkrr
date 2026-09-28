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

// testParams is cheap on purpose: these tests derive real keys and Argon2id at
// production cost would dominate the package's runtime.
var testParams = crypto.Params{Time: 1, MemoryKiB: 8 * 1024, Threads: 1}

func newTestVault(t *testing.T) (*Vault, string) {
	t.Helper()
	dir := t.TempDir()
	salt, err := crypto.NewSalt()
	if err != nil {
		t.Fatal(err)
	}
	key := crypto.DeriveKey([]byte("pw"), salt, testParams)
	if err := Create(dir, key, salt, testParams); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return New(dir, key), dir
}

func TestCreateThenLoadGivesAnEmptyCollection(t *testing.T) {
	v, dir := newTestVault(t)

	if !Exists(dir) {
		t.Fatal("Exists() = false after Create()")
	}
	if Exists(t.TempDir()) {
		t.Error("Exists() = true for a directory with no vault in it")
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

	if err := Create(dir, key, salt, testParams); err == nil {
		t.Fatal("Create() error = nil, want an error on an existing vault")
	}
}

// An init that dies before it publishes must leave nothing behind. A
// half-written vault file would be worse than no vault at all: every command
// would report it corrupt, and bkmr init - which only checks Exists() - would
// refuse to replace it, leaving the user with no way forward but deleting a file
// by hand.
func TestAnInterruptedCreateLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	salt, err := crypto.NewSalt()
	if err != nil {
		t.Fatal(err)
	}
	key := crypto.DeriveKey([]byte("pw"), salt, testParams)

	old := renameFile
	renameFile = func(string, string) error { return errors.New("injected rename failure") }
	err = Create(dir, key, salt, testParams)
	renameFile = old
	if err == nil {
		t.Fatal("Create() error = nil, want the injected failure")
	}

	if Exists(dir) {
		t.Error("Exists() = true after an interrupted Create(); a vault that cannot be opened must not block init")
	}
	if _, err := os.Stat(filepath.Join(dir, FileName+".tmp")); !os.IsNotExist(err) {
		t.Error("Create() left a .tmp file behind")
	}

	// And the failure must not be terminal: init has to be able to try again.
	if err := Create(dir, key, salt, testParams); err != nil {
		t.Fatalf("Create() after an interrupted Create error = %v", err)
	}
	v := New(dir, key)
	if _, err := v.Load(); err != nil {
		t.Fatalf("Load() after the retry error = %v", err)
	}
}

// The remaining window is one rename wide: the reservation that stops two
// writers creating a vault at once is an empty file, and being killed between
// making it and renaming the sealed vault over it leaves that empty file on
// disk. It is not a vault - Seal cannot produce zero bytes - so nothing may
// treat it as one.
func TestAnEmptyVaultFileIsNotAVault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if Exists(dir) {
		t.Error("Exists() = true for a zero-byte vault file, which no Seal can produce")
	}

	salt, _ := crypto.NewSalt()
	key := crypto.DeriveKey([]byte("pw"), salt, testParams)
	if err := Create(dir, key, salt, testParams); err != nil {
		t.Fatalf("Create() over a zero-byte vault file error = %v, want it reclaimed", err)
	}
	if _, err := New(dir, key).Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestMutateLoadRoundTrip(t *testing.T) {
	v, _ := newTestVault(t)

	if err := v.Mutate(func(c *model.Collection) error {
		c.Add(model.Bookmark{URL: "https://example.com", Tags: []string{"rust"}})
		return nil
	}); err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	got, err := v.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got.Bookmarks) != 1 || got.Bookmarks[0].URL != "https://example.com" {
		t.Fatalf("Load() = %+v, want one bookmark for example.com", got.Bookmarks)
	}
}

func TestMutateLeavesTheOldVaultAsBackup(t *testing.T) {
	v, dir := newTestVault(t)
	if err := v.Mutate(func(c *model.Collection) error {
		c.Add(model.Bookmark{URL: "https://first.example"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	afterFirst, err := os.ReadFile(v.Path())
	if err != nil {
		t.Fatal(err)
	}

	if err := v.Mutate(func(c *model.Collection) error {
		c.Add(model.Bookmark{URL: "https://second.example"})
		return nil
	}); err != nil {
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
		t.Error("Mutate() left a .tmp file behind")
	}
	if _, err := os.Stat(v.Path() + ".bak.tmp"); !os.IsNotExist(err) {
		t.Error("Mutate() left a .bak.tmp file behind")
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

func TestMutateReportsBusyWhenTheLockIsHeld(t *testing.T) {
	v, dir := newTestVault(t)
	lock, err := os.OpenFile(filepath.Join(dir, "vault.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(lock.Name())
	lock.Close()

	if err := v.Mutate(func(*model.Collection) error { return nil }); !errors.Is(err, ErrBusy) {
		t.Errorf("Mutate() error = %v, want ErrBusy", err)
	}
}

// A write that dies on the final rename must leave the vault openable. If it
// left no vault file at all, the next command would report "no vault found"
// and bkmr init would create a fresh empty one on top of the user's data.
func TestFailedWriteLeavesAnOpenableVault(t *testing.T) {
	v, _ := newTestVault(t)
	if err := v.Mutate(func(c *model.Collection) error {
		c.Add(model.Bookmark{URL: "https://original.example"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	old := renameFile
	renameFile = func(string, string) error { return errors.New("injected rename failure") }
	defer func() { renameFile = old }()

	if err := v.Mutate(func(c *model.Collection) error {
		c.Add(model.Bookmark{URL: "https://doomed.example"})
		return nil
	}); err == nil {
		t.Fatal("Mutate() error = nil, want the injected failure")
	}

	renameFile = old
	got, err := v.Load()
	if err != nil {
		t.Fatalf("vault is not loadable after a failed write: %v", err)
	}
	if len(got.Bookmarks) != 1 || got.Bookmarks[0].URL != "https://original.example" {
		t.Errorf("Bookmarks = %+v, want only the original bookmark", got.Bookmarks)
	}
	if _, err := os.Stat(v.Path() + ".tmp"); !os.IsNotExist(err) {
		t.Error("the failed write left a .tmp file behind")
	}

	// The failure must not be terminal: the next write has to work.
	if err := v.Mutate(func(c *model.Collection) error {
		c.Add(model.Bookmark{URL: "https://recovered.example"})
		return nil
	}); err != nil {
		t.Fatalf("Mutate() after a failed write error = %v", err)
	}
	again, err := v.Load()
	if err != nil {
		t.Fatalf("Load() after recovering error = %v", err)
	}
	if len(again.Bookmarks) != 2 {
		t.Errorf("len(Bookmarks) = %d, want 2 after a successful write follows a failed one", len(again.Bookmarks))
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
