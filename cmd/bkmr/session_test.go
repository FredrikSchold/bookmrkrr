package main

import (
	"context"
	"errors"
	"fmt"
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

	// No test in this package may reach a host it did not start itself, and the
	// add tests pass URLs like https://example.com/a without --no-fetch. With
	// the real fetcher wired in they would make live outbound requests from CI
	// on three platforms; a privacy tool whose own suite phones out is exactly
	// the wrong look. So every test gets a fetcher that cannot reach anything,
	// and the handful in fetch_test.go that want the real one call
	// useRealFetcher and point it at their own httptest server.
	//
	// Do not "fix" this to return an error. A stub that failed would make
	// resolveTitle write its warning to errOut on nearly every add in the
	// suite, which both dirties the output and breaks
	// TestAddExplainsABusyVaultAndSavesNothing, whose point is that a refused
	// add says nothing on stderr. An empty title with a nil error is just as
	// hermetic - the function makes no call of any kind - and a test that
	// secretly depended on a fetched title still fails on the empty string.
	oldFetch := fetchTitle
	fetchTitle = func(context.Context, string) (string, error) { return "", nil }
	t.Cleanup(func() { fetchTitle = oldFetch })

	// runInit reports where it put the vault, and a helper the whole suite calls
	// has no business spraying that over the test binary's stdout. capture
	// swallows it and restores out the moment runInit returns, so the silence is
	// scoped to this one call and no later test inherits a dead writer. errOut
	// is deliberately left alone: an unexpected diagnostic showing up in test
	// output is information, not noise.
	var initErr error
	capture(t, func() { initErr = runInit(nil) })
	if initErr != nil {
		t.Fatalf("runInit() error = %v", initErr)
	}
	return dir
}

// lockForTest drops the cached key as setup for a test about something else,
// without runLock's report reaching the suite's output. Where that report is
// itself the subject, capture it and assert on it instead - see
// TestLockDropsTheCachedKeyAndUnlockRestoresIt.
func lockForTest(t *testing.T) {
	t.Helper()
	capture(t, func() {
		if err := runLock(nil); err != nil {
			t.Fatalf("runLock() error = %v", err)
		}
	})
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

	// These two calls are the only place in the suite where lock's and unlock's
	// own reports are the subject, so they are asserted rather than discarded:
	// a command whose entire visible effect is one line owes the user that line.
	locked := capture(t, func() {
		if err := runLock(nil); err != nil {
			t.Fatalf("runLock() error = %v", err)
		}
	})
	if want := "Vault locked.\n"; locked != want {
		t.Errorf("runLock() printed %q, want %q", locked, want)
	}
	if _, err := keyring.Get(); !errors.Is(err, keyring.ErrNoKey) {
		t.Errorf("keyring.Get() after lock error = %v, want ErrNoKey", err)
	}

	unlocked := capture(t, func() {
		if err := runUnlock(nil); err != nil {
			t.Fatalf("runUnlock() error = %v", err)
		}
	})
	if want := "Vault unlocked.\n"; unlocked != want {
		t.Errorf("runUnlock() printed %q, want %q", unlocked, want)
	}
	if _, err := keyring.Get(); err != nil {
		t.Errorf("keyring.Get() after unlock error = %v, want the key cached", err)
	}
}

func TestOpenVaultFallsBackToAPromptWhenNoKeyIsCached(t *testing.T) {
	newVaultForTest(t, "hunter2")
	lockForTest(t)

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
	lockForTest(t)
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
	lockForTest(t)
	readPassword = func(string) ([]byte, error) { return []byte("wrong"), nil }

	if err := runUnlock(nil); err == nil {
		t.Fatal("runUnlock() error = nil with a wrong password, want an error")
	}
	if _, err := keyring.Get(); !errors.Is(err, keyring.ErrNoKey) {
		t.Error("runUnlock() cached a key derived from a wrong password")
	}
}

func TestInitRefusesMismatchedPasswordsAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BKMR_DATA_DIR", dir)
	kr.MockInit()

	// The confirmation guard is the only thing standing between a typo and a
	// vault nobody can ever open, so it needs a prompt that answers differently
	// the second time rather than the same bytes twice.
	var calls int
	old := readPassword
	readPassword = func(string) ([]byte, error) {
		calls++
		if calls == 1 {
			return []byte("hunter2"), nil
		}
		return []byte("hunterZ"), nil
	}
	t.Cleanup(func() { readPassword = old })

	stdout, stderr := bothStreams(t, func() {
		if err := runInit(nil); err == nil {
			t.Error("runInit() error = nil with mismatched passwords, want an error")
		}
	})

	if calls != 2 {
		t.Errorf("runInit() prompted %d times, want 2: it must ask for a confirmation", calls)
	}
	// The error matters, but so does the absence of any residue: a vault sealed
	// with a typo'd password is unrecoverable, and a cached key from one is a
	// lie the next command would believe.
	if store.Exists(dir) {
		t.Error("runInit() sealed a vault from mismatched passwords")
	}
	if entries, err := os.ReadDir(dir); err != nil {
		t.Fatalf("ReadDir(%s) error = %v", dir, err)
	} else if len(entries) != 0 {
		t.Errorf("runInit() left %d files behind in the data directory, want none: %v", len(entries), entries)
	}
	if _, err := keyring.Get(); !errors.Is(err, keyring.ErrNoKey) {
		t.Errorf("keyring.Get() = %v, want ErrNoKey: nothing was derived to cache", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing; init did not create anything to report", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing; dispatch prints the returned error", stderr)
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

	stdout, stderr := bothStreams(t, func() {
		if err := runInit(nil); err != nil {
			t.Errorf("runInit() error = %v, want nil: the vault itself was created", err)
		}
	})

	if !store.Exists(dir) {
		t.Fatal("runInit() left no vault behind")
	}
	if !strings.Contains(stdout, "Vault created") {
		t.Errorf("init output = %q, want it to report the vault it created", stdout)
	}
	// The caching failure is a diagnostic, so it belongs on stderr with the rest
	// of them, not in a pipe somebody may be reading.
	if strings.Contains(stdout, errNoKeychain.Error()) {
		t.Errorf("init put the keychain failure on stdout: %q", stdout)
	}
	if !strings.Contains(stderr, errNoKeychain.Error()) {
		t.Errorf("init diagnostics = %q, want them to name why caching failed", stderr)
	}
	if !strings.Contains(stderr, "prompted") {
		t.Errorf("init diagnostics = %q, want a warning that the password will be prompted for each time", stderr)
	}
	if !strings.HasPrefix(stderr, "bkmr:") {
		t.Errorf("init diagnostics = %q, want them prefixed with the binary name", stderr)
	}
}

func TestLockWithAnUnreachableKeychainDoesNotClaimTheVaultIsLocked(t *testing.T) {
	kr.MockInitWithError(errNoKeychain)
	t.Cleanup(kr.MockInit)

	// keyring.Get cannot be consulted to soften this: it answers ErrNoKey for a
	// keychain that is absent and for one that is merely locked alike, so a
	// locked keychain still holding the key looks identical to an empty one.
	// lock must therefore report a failure rather than a lock it cannot prove.
	if err := runLock(nil); err == nil {
		t.Fatal("runLock() error = nil with an unreachable keychain, want an error: the key may still be cached")
	} else if !errors.Is(err, errNoKeychain) {
		t.Errorf("runLock() error = %v, want it to carry the keychain's own error", err)
	}

	stdout, stderr := bothStreams(t, func() {
		if code := dispatch([]string{"lock"}); code != 1 {
			t.Errorf("dispatch(lock) = %d, want 1", code)
		}
	})

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing; a failed lock must not land in the pipe", stdout)
	}
	if strings.Contains(strings.ToLower(stdout+stderr), "vault locked") {
		t.Errorf("lock claimed the vault is locked (%q / %q) when it could not drop the key", stdout, stderr)
	}
	if !strings.Contains(stderr, errNoKeychain.Error()) {
		t.Errorf("lock diagnostics = %q, want them to name the keychain failure", stderr)
	}
	if !strings.Contains(stderr, "may still be present") {
		t.Errorf("lock diagnostics = %q, want them to say the cached key may have survived", stderr)
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
	errInUse := errors.New("The process cannot access the file because it is being used by another process.")
	link := &os.LinkError{
		Op:  "rename",
		Old: `C:\vault\vault.bkmr.tmp`,
		New: `C:\vault\vault.bkmr`,
		Err: errInUse,
	}
	got := explainVaultError(fmt.Errorf("save: %w", link))
	if strings.Contains(got.Error(), ".tmp") {
		t.Errorf("explainVaultError(*os.LinkError) = %q, want the temp file left out of it", got)
	}
	if !strings.Contains(got.Error(), "used by another process") {
		t.Errorf("explainVaultError(*os.LinkError) = %q, want the platform's own reason kept", got)
	}
	if !errors.Is(got, errInUse) {
		t.Errorf("explainVaultError(*os.LinkError) = %v, want the underlying error still matchable", got)
	}

	other := errors.New("disk on fire")
	if got := explainVaultError(other); got != other {
		t.Errorf("explainVaultError(%v) = %v, want it passed through untouched", other, got)
	}
}
