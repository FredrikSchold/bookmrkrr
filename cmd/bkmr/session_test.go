package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	kr "github.com/zalando/go-keyring"

	"github.com/FredrikSchold/bookmrkrr/internal/crypto"
	"github.com/FredrikSchold/bookmrkrr/internal/keyring"
	"github.com/FredrikSchold/bookmrkrr/internal/model"
	"github.com/FredrikSchold/bookmrkrr/internal/store"
)

// errNoKeychain stands in for the platform failure a headless Linux box gives
// when there is no Secret Service to talk to. It is the shape of error the CLI
// must never hand to the user unexplained.
var errNoKeychain = errors.New("dbus: couldn't determine address of session bus")

// newVaultForTest points BKMR_DATA_DIR at a temp dir, mocks the keychain,
// and creates a vault with the given password.
func newVaultForTest(t *testing.T, password string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("BKMR_DATA_DIR", dir)
	kr.MockInit()

	old := readPassword
	readPassword = func(string) ([]byte, error) { return []byte(password), nil }
	t.Cleanup(func() { readPassword = old })

	// Every command reports to out, so a helper the whole suite calls would
	// otherwise spray "Vault created in ..." over the test binary's stdout.
	// Park out on io.Discard for the rest of the test; capture() saves and
	// restores out itself, so a test that asserts on output still works.
	// errOut is deliberately left alone: an unexpected diagnostic showing up
	// in test output is information, not noise.
	oldOut := out
	out = io.Discard
	t.Cleanup(func() { out = oldOut })

	if err := runInit(nil); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
	return dir
}

func TestInitCreatesAVaultAndCachesTheKey(t *testing.T) {
	newVaultForTest(t, "hunter2")

	if _, err := keyring.Get(); err != nil {
		t.Errorf("keyring.Get() error = %v, want the key cached after init", err)
	}
	v, err := openVault()
	if err != nil {
		t.Fatalf("openVault() error = %v", err)
	}
	c, err := v.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(c.Bookmarks) != 0 {
		t.Errorf("a fresh vault holds %d bookmarks, want 0", len(c.Bookmarks))
	}
}

func TestInitRefusesToRunTwice(t *testing.T) {
	newVaultForTest(t, "hunter2")

	if err := runInit(nil); err == nil {
		t.Error("runInit() error = nil on a second run, want an error")
	}
}

func TestOpenVaultWithoutAVaultTellsTheUserToInit(t *testing.T) {
	t.Setenv("BKMR_DATA_DIR", t.TempDir())
	kr.MockInit()

	_, err := openVault()
	if err == nil {
		t.Fatal("openVault() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "bkmr init") {
		t.Errorf("openVault() error = %q, want it to mention 'bkmr init'", err)
	}
}

func TestLockDropsTheCachedKeyAndUnlockRestoresIt(t *testing.T) {
	newVaultForTest(t, "hunter2")

	if err := runLock(nil); err != nil {
		t.Fatalf("runLock() error = %v", err)
	}
	if _, err := keyring.Get(); !errors.Is(err, keyring.ErrNoKey) {
		t.Errorf("keyring.Get() after lock error = %v, want ErrNoKey", err)
	}

	if err := runUnlock(nil); err != nil {
		t.Fatalf("runUnlock() error = %v", err)
	}
	if _, err := keyring.Get(); err != nil {
		t.Errorf("keyring.Get() after unlock error = %v, want the key cached", err)
	}
}

func TestOpenVaultFallsBackToAPromptWhenNoKeyIsCached(t *testing.T) {
	newVaultForTest(t, "hunter2")
	if err := runLock(nil); err != nil {
		t.Fatal(err)
	}

	v, err := openVault()
	if err != nil {
		t.Fatalf("openVault() error = %v", err)
	}
	if _, err := v.Load(); err != nil {
		t.Errorf("Load() after a prompted unlock error = %v", err)
	}
	// Prompting must not silently cache the key; only unlock does that.
	if _, err := keyring.Get(); !errors.Is(err, keyring.ErrNoKey) {
		t.Error("openVault() cached the key; only 'bkmr unlock' should do that")
	}
}

func TestOpenVaultWithTheWrongPasswordFailsToLoad(t *testing.T) {
	newVaultForTest(t, "hunter2")
	if err := runLock(nil); err != nil {
		t.Fatal(err)
	}
	readPassword = func(string) ([]byte, error) { return []byte("wrong"), nil }

	v, err := openVault()
	if err != nil {
		t.Fatalf("openVault() error = %v", err)
	}
	if _, err := v.Load(); !errors.Is(err, crypto.ErrBadVault) {
		t.Errorf("Load() error = %v, want ErrBadVault", err)
	}
}

func TestUnlockWithTheWrongPasswordDoesNotCacheAKey(t *testing.T) {
	newVaultForTest(t, "hunter2")
	if err := runLock(nil); err != nil {
		t.Fatal(err)
	}
	readPassword = func(string) ([]byte, error) { return []byte("wrong"), nil }

	if err := runUnlock(nil); err == nil {
		t.Fatal("runUnlock() error = nil with a wrong password, want an error")
	}
	if _, err := keyring.Get(); !errors.Is(err, keyring.ErrNoKey) {
		t.Error("runUnlock() cached a key derived from a wrong password")
	}
}

func TestInitSeedsTheCollectionVersion(t *testing.T) {
	newVaultForTest(t, "hunter2")
	v, _ := openVault()
	c, _ := v.Load()

	if c.Version != model.Version {
		t.Errorf("Version = %d, want %d", c.Version, model.Version)
	}
}

// The tests below cover the presentation duties the CLI layer owns: a keychain
// that cannot be written to, a keychain that cannot be deleted from, and the
// vault errors whose default text would leave a user stuck.

func TestUnlockSurfacesAKeychainThatCannotCacheTheKey(t *testing.T) {
	newVaultForTest(t, "hunter2")
	kr.MockInitWithError(errNoKeychain)
	t.Cleanup(kr.MockInit)

	err := runUnlock(nil)
	if err == nil {
		t.Fatal("runUnlock() error = nil with an unusable keychain, want an error")
	}
	if !errors.Is(err, errNoKeychain) {
		t.Errorf("runUnlock() error = %v, want it to carry the keychain's own error", err)
	}
	if !strings.Contains(err.Error(), "prompt") {
		t.Errorf("runUnlock() error = %q, want it to warn that the password will be prompted for each time", err)
	}
}

func TestInitWithAnUnusableKeychainStillCreatesTheVault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BKMR_DATA_DIR", dir)
	kr.MockInitWithError(errNoKeychain)
	t.Cleanup(kr.MockInit)
	old := readPassword
	readPassword = func(string) ([]byte, error) { return []byte("hunter2"), nil }
	t.Cleanup(func() { readPassword = old })

	stdout, _ := bothStreams(t, func() {
		if err := runInit(nil); err != nil {
			t.Errorf("runInit() error = %v, want nil: the vault itself was created", err)
		}
	})

	if !store.Exists(dir) {
		t.Fatal("runInit() left no vault behind")
	}
	if !strings.Contains(stdout, errNoKeychain.Error()) {
		t.Errorf("init output = %q, want it to name why caching failed", stdout)
	}
	if !strings.Contains(stdout, "prompted") {
		t.Errorf("init output = %q, want it to warn that the password will be prompted for each time", stdout)
	}
}

func TestLockWithAnUnusableKeychainStillReportsTheVaultLocked(t *testing.T) {
	kr.MockInitWithError(errNoKeychain)
	t.Cleanup(kr.MockInit)

	stdout, stderr := bothStreams(t, func() {
		if err := runLock(nil); err != nil {
			t.Errorf("runLock() error = %v, want nil: no cached key is reachable", err)
		}
	})

	if !strings.Contains(stdout, "locked") {
		t.Errorf("lock output = %q, want it to report the vault locked", stdout)
	}
	if !strings.Contains(stderr, "keychain") {
		t.Errorf("lock diagnostics = %q, want an explanation of the keychain failure", stderr)
	}
	if !strings.HasPrefix(stderr, "bkmr:") {
		t.Errorf("lock diagnostics = %q, want them prefixed with the binary name", stderr)
	}
}

func TestVaultErrorsAreExplainedRatherThanDumped(t *testing.T) {
	if got := explainVaultError(nil); got != nil {
		t.Errorf("explainVaultError(nil) = %v, want nil", got)
	}

	busy := explainVaultError(fmt.Errorf("save: %w", store.ErrBusy))
	if !errors.Is(busy, store.ErrBusy) {
		t.Errorf("explainVaultError dropped ErrBusy from the chain: %v", busy)
	}
	if !strings.Contains(busy.Error(), "try again") {
		t.Errorf("ErrBusy = %q, want it to tell the user what to do about it", busy)
	}

	// What a Windows sharing violation on the publishing rename looks like.
	link := &os.LinkError{
		Op:  "rename",
		Old: `C:\vault\vault.bkmr.tmp`,
		New: `C:\vault\vault.bkmr`,
		Err: errors.New("The process cannot access the file because it is being used by another process."),
	}
	got := explainVaultError(link)
	if strings.Contains(got.Error(), ".tmp") {
		t.Errorf("explainVaultError(*os.LinkError) = %q, want the temp file left out of it", got)
	}
	if !strings.Contains(got.Error(), "used by another process") {
		t.Errorf("explainVaultError(*os.LinkError) = %q, want the platform's own reason kept", got)
	}

	other := errors.New("disk on fire")
	if got := explainVaultError(other); got != other {
		t.Errorf("explainVaultError(%v) = %v, want it passed through untouched", other, got)
	}
}
