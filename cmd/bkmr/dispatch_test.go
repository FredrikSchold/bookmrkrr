package main

import (
	"errors"
	"strings"
	"testing"
)

// Two probe commands, registered only in the test binary, so the errUsage and
// generic-error branches of dispatch have real commands to exercise them. The
// live-slice tests in help_test.go absorb these registrations unchanged.
func init() {
	register(command{
		Name:    "probe-usage",
		Summary: "probe: returns errUsage",
		Usage:   "bkmr probe-usage <arg>",
		Run:     func([]string) error { return errUsage },
	})
	register(command{
		Name:    "probe-fail",
		Summary: "probe: returns a plain error",
		Usage:   "bkmr probe-fail",
		Run:     func([]string) error { return errors.New("the vault is locked") },
	})
}

// bothStreams runs fn and returns what it wrote to stdout and to stderr.
func bothStreams(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	stderr = captureErr(t, func() { stdout = capture(t, fn) })
	return stdout, stderr
}

func TestErrUsagePrintsTheUsageLineToStderr(t *testing.T) {
	stdout, stderr := bothStreams(t, func() {
		if code := dispatch([]string{"probe-usage"}); code != 2 {
			t.Errorf("dispatch(probe-usage) = %d, want 2", code)
		}
	})

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing; a usage error must not land in the pipe", stdout)
	}
	c, ok := find("probe-usage")
	if !ok {
		t.Fatal("the probe-usage command is not registered")
	}
	if !strings.Contains(stderr, "usage: "+c.Usage) {
		t.Errorf("stderr = %q, want it to contain the usage line %q", stderr, "usage: "+c.Usage)
	}
}

func TestAPlainErrorGoesToStderrAndExitsOne(t *testing.T) {
	stdout, stderr := bothStreams(t, func() {
		if code := dispatch([]string{"probe-fail"}); code != 1 {
			t.Errorf("dispatch(probe-fail) = %d, want 1", code)
		}
	})

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing; an error must not land in the pipe", stdout)
	}
	if !strings.Contains(stderr, "the vault is locked") {
		t.Errorf("stderr = %q, want it to contain the error message", stderr)
	}
	if !strings.HasPrefix(stderr, "bkmr:") {
		t.Errorf("stderr = %q, want it to be prefixed with the binary name", stderr)
	}
}

func TestLoneHelpFlagAfterACommandShowsThatCommandsUsage(t *testing.T) {
	c, ok := find("probe-usage")
	if !ok {
		t.Fatal("the probe-usage command is not registered")
	}

	for _, flag := range []string{"--help", "-h"} {
		// probe-usage's Run returns errUsage, so if the flag were passed
		// through instead of intercepted this would exit 2 and write to stderr.
		stdout, stderr := bothStreams(t, func() {
			if code := dispatch([]string{"probe-usage", flag}); code != 0 {
				t.Errorf("dispatch(probe-usage %s) = %d, want 0", flag, code)
			}
		})

		if stderr != "" {
			t.Errorf("probe-usage %s wrote %q to stderr, want nothing", flag, stderr)
		}
		if !strings.Contains(stdout, c.Usage) {
			t.Errorf("probe-usage %s = %q, want it to contain the usage line %q", flag, stdout, c.Usage)
		}
		if !strings.Contains(stdout, c.Summary) {
			t.Errorf("probe-usage %s = %q, want it to contain the summary %q", flag, stdout, c.Summary)
		}
	}
}

func TestHelpFlagAmongOtherArgumentsIsNotIntercepted(t *testing.T) {
	// More than a lone flag is a real invocation, so Run must fire and its
	// errUsage must surface rather than dispatch swallowing the flag.
	stdout, stderr := bothStreams(t, func() {
		if code := dispatch([]string{"probe-usage", "--help", "extra"}); code != 2 {
			t.Errorf("dispatch(probe-usage --help extra) = %d, want 2", code)
		}
	})

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if !strings.Contains(stderr, "usage: bkmr probe-usage") {
		t.Errorf("stderr = %q, want the command's own errUsage to have surfaced", stderr)
	}
}

func TestHelpFlagOnAnUnknownCommandStillFails(t *testing.T) {
	stdout, stderr := bothStreams(t, func() {
		if code := dispatch([]string{"nosuchcommand", "--help"}); code != 2 {
			t.Errorf("dispatch(nosuchcommand --help) = %d, want 2", code)
		}
	})

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if !strings.Contains(stderr, "nosuchcommand") {
		t.Errorf("stderr = %q, want it to name the unknown command", stderr)
	}
}

// Flag permutation. Go's flag package stops parsing at the first non-flag word,
// so without parsePermuted every one of these is a usage error - including
// 'bkmr add <url> -t rust', which is the tool's primary command in the form
// almost everybody types it. These go through dispatch rather than calling the
// run functions directly, so the real argument path is what is under test.

func TestAddAcceptsFlagsAfterTheURL(t *testing.T) {
	newVaultForTest(t, "pw")

	capture(t, func() {
		if code := dispatch([]string{"add", "https://a.example", "-t", "rust", "--title", "Rust", "--note", "a note", "--no-fetch"}); code != 0 {
			t.Fatalf("dispatch(add) = %d, want 0", code)
		}
	})

	got := bookmarksInVault(t)
	if len(got) != 1 {
		t.Fatalf("len(Bookmarks) = %d, want 1", len(got))
	}
	if got[0].URL != "https://a.example" {
		t.Errorf("URL = %q, want %q", got[0].URL, "https://a.example")
	}
	if strings.Join(got[0].Tags, ",") != "rust" {
		t.Errorf("Tags = %v, want [rust] - a tag after the URL must still be a tag", got[0].Tags)
	}
	if got[0].Title != "Rust" {
		t.Errorf("Title = %q, want %q", got[0].Title, "Rust")
	}
	if got[0].Notes != "a note" {
		t.Errorf("Notes = %q, want %q", got[0].Notes, "a note")
	}
}

func TestAddStillAcceptsFlagsBeforeTheURL(t *testing.T) {
	newVaultForTest(t, "pw")

	capture(t, func() {
		if code := dispatch([]string{"add", "--no-fetch", "-t", "rust", "https://a.example"}); code != 0 {
			t.Fatalf("dispatch(add) = %d, want 0", code)
		}
	})

	got := bookmarksInVault(t)
	if len(got) != 1 || strings.Join(got[0].Tags, ",") != "rust" {
		t.Errorf("Bookmarks = %+v, want one bookmark tagged rust", got)
	}
}

// The permuting parser must not turn a flag's value into a positional argument.
// If it did, this add would look like two URLs and be refused - and a naive
// "collect everything that does not start with a dash" implementation does
// exactly that.
func TestAddDoesNotMistakeAFlagValueForTheURL(t *testing.T) {
	newVaultForTest(t, "pw")

	capture(t, func() {
		if code := dispatch([]string{"add", "--no-fetch", "--title", "https://not-a-url", "https://real.example"}); code != 0 {
			t.Fatalf("dispatch(add) = %d, want 0", code)
		}
	})

	got := bookmarksInVault(t)
	if len(got) != 1 {
		t.Fatalf("len(Bookmarks) = %d, want 1", len(got))
	}
	if got[0].URL != "https://real.example" {
		t.Errorf("URL = %q, want %q - the --title value is not the URL", got[0].URL, "https://real.example")
	}
	if got[0].Title != "https://not-a-url" {
		t.Errorf("Title = %q, want %q", got[0].Title, "https://not-a-url")
	}
}

// Permuting must not cost add its arity check: two URLs is still a mistake, and
// still one that saves nothing.
func TestAddStillRefusesTwoURLsWithFlagsBetweenThem(t *testing.T) {
	newVaultForTest(t, "pw")

	stdout, stderr := bothStreams(t, func() {
		if code := dispatch([]string{"add", "--no-fetch", "https://a.example", "-t", "rust", "https://b.example"}); code != 2 {
			t.Errorf("dispatch(add) = %d, want 2", code)
		}
	})

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if !strings.Contains(stderr, "at most one URL") {
		t.Errorf("stderr = %q, want it to explain that add takes at most one URL", stderr)
	}
	if got := bookmarksInVault(t); len(got) != 0 {
		t.Errorf("len(Bookmarks) = %d, want 0 - a usage error must save nothing", len(got))
	}
}

func TestEditTakesItsIDBeforeOrAfterTheFlags(t *testing.T) {
	newVaultForTest(t, "pw")
	addForTest(t, "--no-fetch", "https://a.example")
	id := bookmarksInVault(t)[0].ID

	capture(t, func() {
		if code := dispatch([]string{"edit", id, "--title", "Trailing"}); code != 0 {
			t.Fatalf("dispatch(edit <id> --title) = %d, want 0", code)
		}
	})
	if got := bookmarksInVault(t)[0].Title; got != "Trailing" {
		t.Errorf("Title = %q, want %q", got, "Trailing")
	}

	capture(t, func() {
		if code := dispatch([]string{"edit", "--title", "Leading", id}); code != 0 {
			t.Fatalf("dispatch(edit --title <id>) = %d, want 0", code)
		}
	})
	if got := bookmarksInVault(t)[0].Title; got != "Leading" {
		t.Errorf("Title = %q, want %q - both argument orders must mean the same thing", got, "Leading")
	}
}

func TestEditStillRefusesTwoIDs(t *testing.T) {
	newVaultForTest(t, "pw")
	addForTest(t, "--no-fetch", "--title", "Keep", "https://a.example")
	id := bookmarksInVault(t)[0].ID

	_, stderr := bothStreams(t, func() {
		if code := dispatch([]string{"edit", id, "--title", "x", id}); code != 2 {
			t.Errorf("dispatch(edit) = %d, want 2", code)
		}
	})
	if !strings.Contains(stderr, "one bookmark id") {
		t.Errorf("stderr = %q, want it to explain that edit takes one id", stderr)
	}
	if got := bookmarksInVault(t)[0].Title; got != "Keep" {
		t.Errorf("Title = %q, want it untouched by a usage error", got)
	}
}

func TestRmAcceptsForceAfterTheID(t *testing.T) {
	newVaultForTest(t, "pw")
	addForTest(t, "--no-fetch", "https://a.example")
	id := bookmarksInVault(t)[0].ID

	old := confirm
	confirm = func(string) (bool, error) {
		t.Fatal("--force must not ask, wherever it appears")
		return false, nil
	}
	defer func() { confirm = old }()

	capture(t, func() {
		if code := dispatch([]string{"rm", id, "--force"}); code != 0 {
			t.Fatalf("dispatch(rm <id> --force) = %d, want 0", code)
		}
	})
	if got := bookmarksInVault(t); len(got) != 0 {
		t.Errorf("len(Bookmarks) = %d, want 0", len(got))
	}
}
