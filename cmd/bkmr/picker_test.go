package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FredrikSchold/bookmrkrr/internal/store"
	"github.com/FredrikSchold/bookmrkrr/internal/tui"
)

// onlyBookmark returns the vault's single bookmark as a picker row, which is
// what applyPickerAction is handed once the picker closes.
func onlyBookmark(t *testing.T) tui.Item {
	t.Helper()
	all := bookmarksInVault(t)
	if len(all) != 1 {
		t.Fatalf("len(Bookmarks) = %d, want exactly 1", len(all))
	}
	return tui.Item{ID: all[0].ID, Label: label(all[0]), Detail: all[0].URL}
}

// vaultForPickerTest builds a vault holding one bookmark and returns the data
// directory, a handle, and that bookmark as a picker row.
func vaultForPickerTest(t *testing.T, url string) (string, *store.Vault, tui.Item) {
	t.Helper()
	dir := newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", url}) })

	item := onlyBookmark(t)
	v, err := openVault()
	if err != nil {
		t.Fatalf("openVault() error = %v", err)
	}
	return dir, v, item
}

func TestPickerDeleteRemovesTheBookmarkAndSaysSo(t *testing.T) {
	_, v, item := vaultForPickerTest(t, "https://doomed.example")

	stdout := capture(t, func() {
		if err := applyPickerAction(v, item, tui.ActionDelete); err != nil {
			t.Fatalf("applyPickerAction(delete) error = %v", err)
		}
	})

	if !strings.Contains(stdout, item.ID) {
		t.Errorf("stdout = %q, want it to name the deleted bookmark %q", stdout, item.ID)
	}
	if got := bookmarksInVault(t); len(got) != 0 {
		t.Errorf("len(Bookmarks) = %d after the delete, want 0", len(got))
	}
}

// This is the test that pins the reporting order.
//
// store.Mutate runs its closure and only then writes the file, so a write that
// fails after the closure has already deleted the bookmark from the in-memory
// collection is the case where reporting from inside the closure would announce
// "deleted" on stdout and then fail on stderr, with the bookmark still in the
// vault.
//
// Reaching that needs a failure after the closure, which a busy vault is not:
// Mutate takes the lock before anything else, so ErrBusy never gets as far as
// running the closure (TestPickerDeleteOnABusyVaultChangesNothing covers that
// separately, and it passes either way). The portable post-closure failure is
// Vault.write's backup step, which creates vault.bkmr.bak.tmp - a directory of
// that name makes the create fail on every platform, and by then the closure has
// run and the vault file is still untouched on disk.
func TestPickerDeleteReportsNothingWhenTheWriteFails(t *testing.T) {
	dir, v, item := vaultForPickerTest(t, "https://survives.example")

	blocker := filepath.Join(dir, store.FileName+".bak.tmp")
	if err := os.Mkdir(blocker, 0o700); err != nil {
		t.Fatalf("Mkdir(%s) error = %v", blocker, err)
	}
	t.Cleanup(func() { os.Remove(blocker) })

	stdout, stderr := bothStreams(t, func() {
		if err := applyPickerAction(v, item, tui.ActionDelete); err == nil {
			t.Fatal("applyPickerAction(delete) error = nil, want the write to have failed")
		}
	})

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing; a delete that was never written must not claim it deleted anything", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing; dispatch prints the returned error", stderr)
	}
	if got := bookmarksInVault(t); len(got) != 1 {
		t.Errorf("len(Bookmarks) = %d after the failed delete, want 1 - the bookmark must survive", len(got))
	}
}

// A delete refused before it began must also say nothing and change nothing.
func TestPickerDeleteOnABusyVaultChangesNothing(t *testing.T) {
	dir, v, item := vaultForPickerTest(t, "https://busy.example")

	// store.Mutate takes the write lock before it does anything else, so an
	// existing lock file is the one way to reach ErrBusy without a second
	// process.
	lock := filepath.Join(dir, "vault.lock")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", lock, err)
	}
	t.Cleanup(func() { os.Remove(lock) })

	stdout, stderr := bothStreams(t, func() {
		err := applyPickerAction(v, item, tui.ActionDelete)
		if !errors.Is(err, store.ErrBusy) {
			t.Fatalf("applyPickerAction(delete) error = %v, want store.ErrBusy", err)
		}
		// Routed through explainVaultError, so store's bare text is not what
		// the user is left holding.
		if !strings.Contains(err.Error(), "try again") {
			t.Errorf("error = %q, want it to tell the user what to do about it", err)
		}
	})

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing; dispatch prints the returned error", stderr)
	}

	os.Remove(lock)
	if got := bookmarksInVault(t); len(got) != 1 {
		t.Errorf("len(Bookmarks) = %d, want 1 - a refused delete must change nothing", len(got))
	}
}

// Quitting the picker is not an error and deserves no output.
func TestPickerQuittingWritesNothing(t *testing.T) {
	_, v, item := vaultForPickerTest(t, "https://kept.example")

	stdout, stderr := bothStreams(t, func() {
		if err := applyPickerAction(v, item, tui.ActionNone); err != nil {
			t.Errorf("applyPickerAction(none) error = %v, want nil", err)
		}
	})

	if stdout != "" || stderr != "" {
		t.Errorf("quitting wrote stdout %q and stderr %q, want nothing", stdout, stderr)
	}
	if got := bookmarksInVault(t); len(got) != 1 {
		t.Errorf("len(Bookmarks) = %d, want 1 - quitting must change nothing", len(got))
	}
}

// Bare bkmr into a pipe must behave like ls, and it must decide that before it
// touches the vault.
//
// In the other order runPicker decrypts the vault, discovers there is no
// terminal, and hands off to runLs, which decrypts it again - and with no key
// cached that is two password prompts for one command. The prompt count is what
// makes the ordering visible; asserting only on the rows would pass either way.
func TestPipedPickerBehavesLikeLsAndAsksForThePasswordOnce(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://piped.example"}) })
	lockForTest(t)

	prompts := 0
	old := readPassword
	readPassword = func(string) ([]byte, error) { prompts++; return []byte("pw"), nil }
	t.Cleanup(func() { readPassword = old })

	// go test's stdout is never a terminal, which is exactly the branch here.
	stdout, stderr := bothStreams(t, func() {
		if err := runPicker(nil); err != nil {
			t.Fatalf("runPicker() error = %v", err)
		}
	})

	if !strings.Contains(stdout, "https://piped.example") {
		t.Errorf("stdout = %q, want an ls row", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	if prompts != 1 {
		t.Errorf("password prompts = %d, want 1 - the terminal check belongs before the vault is opened", prompts)
	}
}
