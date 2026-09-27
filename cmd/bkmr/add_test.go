package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	kr "github.com/zalando/go-keyring"

	"github.com/FredrikSchold/bookmrkrr/internal/model"
	"github.com/FredrikSchold/bookmrkrr/internal/store"
)

// addForTest saves a bookmark as setup for a test about something else, without
// add's report reaching the suite's output. Where the report is itself the
// subject, capture it and assert on it instead - see
// TestAddStoresAURLWithTags and TestAddMergesADuplicateAndSaysSo.
func addForTest(t *testing.T, args ...string) {
	t.Helper()
	capture(t, func() {
		if err := runAdd(args); err != nil {
			t.Fatalf("runAdd(%v) error = %v", args, err)
		}
	})
}

// bookmarksInVault reopens the vault and returns what is actually stored, which
// is the only thing that settles whether a rejected add wrote anything.
func bookmarksInVault(t *testing.T) []model.Bookmark {
	t.Helper()
	v, err := openVault()
	if err != nil {
		t.Fatalf("openVault() error = %v", err)
	}
	c, err := v.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return c.Bookmarks
}

func TestAddStoresAURLWithTags(t *testing.T) {
	newVaultForTest(t, "pw")

	got := capture(t, func() {
		if err := runAdd([]string{"-t", "rust", "-t", "Async", "https://example.com/a"}); err != nil {
			t.Fatalf("runAdd() error = %v", err)
		}
	})
	if !strings.Contains(got, "https://example.com/a") {
		t.Errorf("runAdd() output = %q, want it to echo the URL", got)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 1 {
		t.Fatalf("len(Bookmarks) = %d, want 1", len(c.Bookmarks))
	}
	tags := strings.Join(c.Bookmarks[0].Tags, ",")
	if tags != "async,rust" {
		t.Errorf("Tags = %q, want %q", tags, "async,rust")
	}
}

func TestAddWithNoArgumentReadsTheClipboard(t *testing.T) {
	newVaultForTest(t, "pw")
	old := readClipboard
	readClipboard = func() (string, error) { return "https://clipboard.example/page", nil }
	defer func() { readClipboard = old }()

	capture(t, func() {
		if err := runAdd(nil); err != nil {
			t.Fatalf("runAdd() error = %v", err)
		}
	})

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 1 || c.Bookmarks[0].URL != "https://clipboard.example/page" {
		t.Fatalf("Bookmarks = %+v, want the clipboard URL", c.Bookmarks)
	}
}

func TestAddWithAnArgumentNeverReadsTheClipboard(t *testing.T) {
	newVaultForTest(t, "pw")
	// The real clipboard shells out to a platform tool, so reaching it from a
	// test is both slow and visible to whoever is at the keyboard. An argument
	// must settle the question outright.
	old := readClipboard
	readClipboard = func() (string, error) {
		t.Error("runAdd read the clipboard although a URL was given on the command line")
		return "", nil
	}
	defer func() { readClipboard = old }()

	addForTest(t, "https://example.com/a")

	if got := bookmarksInVault(t); len(got) != 1 {
		t.Errorf("len(Bookmarks) = %d, want 1", len(got))
	}
}

func TestAddRejectsAClipboardThatIsNotAURL(t *testing.T) {
	newVaultForTest(t, "pw")
	old := readClipboard
	readClipboard = func() (string, error) { return "just some copied prose", nil }
	defer func() { readClipboard = old }()

	err := runAdd(nil)
	if err == nil {
		t.Fatal("runAdd() error = nil, want an error for non-URL clipboard contents")
	}
	// The refusal has to name both halves - where the text came from and what
	// the text was - or the user cannot tell a stale clipboard from a bug.
	if !strings.Contains(err.Error(), "clipboard") {
		t.Errorf("runAdd() error = %q, want it to name the clipboard as the source", err)
	}
	if !strings.Contains(err.Error(), "just some copied prose") {
		t.Errorf("runAdd() error = %q, want it to quote what it found", err)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 0 {
		t.Errorf("len(Bookmarks) = %d, want 0 - nothing should have been saved", len(c.Bookmarks))
	}
}

func TestAddRejectsAnEmptyClipboard(t *testing.T) {
	newVaultForTest(t, "pw")
	old := readClipboard
	readClipboard = func() (string, error) { return "   ", nil }
	defer func() { readClipboard = old }()

	if err := runAdd(nil); err == nil {
		t.Error("runAdd() error = nil, want an error for an empty clipboard")
	}
	if got := bookmarksInVault(t); len(got) != 0 {
		t.Errorf("len(Bookmarks) = %d, want 0 - nothing should have been saved", len(got))
	}
}

func TestAddSurfacesAClipboardThatCannotBeRead(t *testing.T) {
	newVaultForTest(t, "pw")
	// What a Linux box with no xclip, or a locked-down Windows session, gives.
	clipErr := errors.New("exec: \"xclip\": executable file not found in $PATH")
	old := readClipboard
	readClipboard = func() (string, error) { return "", clipErr }
	defer func() { readClipboard = old }()

	err := runAdd(nil)
	if !errors.Is(err, clipErr) {
		t.Errorf("runAdd() error = %v, want it to carry the clipboard's own error", err)
	}
	if got := bookmarksInVault(t); len(got) != 0 {
		t.Errorf("len(Bookmarks) = %d, want 0 - nothing should have been saved", len(got))
	}
}

func TestAddMergesADuplicateAndSaysSo(t *testing.T) {
	newVaultForTest(t, "pw")
	addForTest(t, "-t", "rust", "https://example.com/a")

	got := capture(t, func() {
		if err := runAdd([]string{"-t", "async", "https://example.com/a?utm_source=news"}); err != nil {
			t.Fatalf("runAdd() error = %v", err)
		}
	})
	if !strings.Contains(strings.ToLower(got), "already") {
		t.Errorf("runAdd() output = %q, want it to say the bookmark already existed", got)
	}
	// Saying "already saved" without saying what the tags now are leaves the
	// user unable to tell whether their new tag landed.
	if !strings.Contains(got, "async") || !strings.Contains(got, "rust") {
		t.Errorf("runAdd() output = %q, want it to report the merged tag set", got)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 1 {
		t.Fatalf("len(Bookmarks) = %d, want 1", len(c.Bookmarks))
	}
	if strings.Join(c.Bookmarks[0].Tags, ",") != "async,rust" {
		t.Errorf("Tags = %v, want [async rust]", c.Bookmarks[0].Tags)
	}
}

func TestAddAcceptsTitleAndNoteFlags(t *testing.T) {
	newVaultForTest(t, "pw")

	addForTest(t, "--title", "The Page", "--note", "read later", "https://example.com")

	v, _ := openVault()
	c, _ := v.Load()
	if c.Bookmarks[0].Title != "The Page" {
		t.Errorf("Title = %q, want %q", c.Bookmarks[0].Title, "The Page")
	}
	if c.Bookmarks[0].Notes != "read later" {
		t.Errorf("Notes = %q, want %q", c.Bookmarks[0].Notes, "read later")
	}
}

func TestAddAcceptsTheLongTagFlag(t *testing.T) {
	newVaultForTest(t, "pw")

	// Both spellings are registered, and ls takes --tag, so a user who writes
	// --tag on add too must not be told it is an unknown flag.
	addForTest(t, "--tag", "rust", "--tag", "cli", "https://example.com/a")

	got := bookmarksInVault(t)
	if len(got) != 1 {
		t.Fatalf("len(Bookmarks) = %d, want 1", len(got))
	}
	if tags := strings.Join(got[0].Tags, ","); tags != "cli,rust" {
		t.Errorf("Tags = %q, want %q", tags, "cli,rust")
	}
}

func TestAddWithoutAVaultReportsTheInitHint(t *testing.T) {
	t.Setenv("BKMR_DATA_DIR", t.TempDir())
	// add refuses before it ever asks for a key, so this never reaches the
	// keychain - but the mock is installed anyway rather than left to depend on
	// whichever test ran last, because reaching the real credential store from
	// a test would be a genuine side effect on the machine.
	kr.MockInit()

	err := runAdd([]string{"https://example.com"})
	if err == nil || !strings.Contains(err.Error(), "bkmr init") {
		t.Errorf("runAdd() error = %v, want it to mention 'bkmr init'", err)
	}
}

func TestAddRejectsTwoURLs(t *testing.T) {
	newVaultForTest(t, "pw")

	stdout, stderr := bothStreams(t, func() {
		if err := runAdd([]string{"https://a.example", "https://b.example"}); !errors.Is(err, errUsage) {
			t.Errorf("runAdd() error = %v, want errUsage", err)
		}
	})

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing; a usage error must not land in the pipe", stdout)
	}
	// errUsage.Error() is the single word "usage", so dispatch prints only the
	// usage line. Why the usage was wrong has to come from the command itself.
	if !strings.Contains(stderr, "at most one URL") {
		t.Errorf("stderr = %q, want it to explain that add takes at most one URL", stderr)
	}
	if got := bookmarksInVault(t); len(got) != 0 {
		t.Errorf("len(Bookmarks) = %d, want 0 - a usage error must save nothing", len(got))
	}
}

func TestAddRejectsAnUnknownFlagAndExplainsWhy(t *testing.T) {
	newVaultForTest(t, "pw")

	stdout, stderr := bothStreams(t, func() {
		if err := runAdd([]string{"--nope", "https://a.example"}); !errors.Is(err, errUsage) {
			t.Errorf("runAdd() error = %v, want errUsage", err)
		}
	})

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing; a usage error must not land in the pipe", stdout)
	}
	if !strings.Contains(stderr, "nope") {
		t.Errorf("stderr = %q, want it to name the flag it did not recognise", stderr)
	}
	if got := bookmarksInVault(t); len(got) != 0 {
		t.Errorf("len(Bookmarks) = %d, want 0", len(got))
	}
}

func TestAddExplainsABusyVaultAndSavesNothing(t *testing.T) {
	dir := newVaultForTest(t, "pw")

	// store.Mutate takes the write lock before it does anything else, so an
	// existing lock file is the one way to reach ErrBusy without a second
	// process. The CLI owes that error an explanation rather than store's bare
	// text, which is what explainVaultError is for.
	lock := filepath.Join(dir, "vault.lock")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", lock, err)
	}
	t.Cleanup(func() { os.Remove(lock) })

	stdout, stderr := bothStreams(t, func() {
		err := runAdd([]string{"https://example.com/a"})
		if !errors.Is(err, store.ErrBusy) {
			t.Fatalf("runAdd() error = %v, want store.ErrBusy", err)
		}
		if !strings.Contains(err.Error(), "try again") {
			t.Errorf("runAdd() error = %q, want it to tell the user what to do about it", err)
		}
	})

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing; a failed add must not claim it saved anything", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing; dispatch prints the returned error", stderr)
	}
	if got := bookmarksInVault(t); len(got) != 0 {
		t.Errorf("len(Bookmarks) = %d, want 0 - a busy vault must save nothing", len(got))
	}
}
