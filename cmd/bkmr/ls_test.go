package main

import (
	"strings"
	"testing"
	"time"

	"github.com/FredrikSchold/bookmrkrr/internal/model"
)

func TestLsPrintsOneLinePerBookmark(t *testing.T) {
	newVaultForTest(t, "pw")
	addForTest(t, "-t", "rust", "--title", "Rust Book", "https://doc.rust-lang.org/book/")
	addForTest(t, "-t", "go", "--title", "Go Docs", "https://go.dev/doc/")

	got := capture(t, func() {
		if err := runLs(nil); err != nil {
			t.Fatalf("runLs() error = %v", err)
		}
	})

	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("runLs() printed %d lines, want 2:\n%s", len(lines), got)
	}
	if !strings.Contains(got, "Rust Book") || !strings.Contains(got, "https://go.dev/doc/") {
		t.Errorf("runLs() = %q, want both bookmarks", got)
	}
}

func TestLsFiltersByTag(t *testing.T) {
	newVaultForTest(t, "pw")
	addForTest(t, "-t", "rust", "https://a.example")
	addForTest(t, "-t", "go", "https://b.example")

	got := capture(t, func() {
		if err := runLs([]string{"--tag", "rust"}); err != nil {
			t.Fatalf("runLs() error = %v", err)
		}
	})

	if !strings.Contains(got, "a.example") {
		t.Errorf("runLs(--tag rust) = %q, want the rust bookmark", got)
	}
	if strings.Contains(got, "b.example") {
		t.Errorf("runLs(--tag rust) = %q, want the go bookmark excluded", got)
	}
}

func TestLsNormalizesTheTagItFiltersOn(t *testing.T) {
	newVaultForTest(t, "pw")
	// Stored tags are normalized on the way in, so the query has to be
	// normalized the same way or --tag would only ever match what the user
	// happened to type in lower case.
	addForTest(t, "-t", "Async Rust", "https://a.example")

	got := capture(t, func() {
		if err := runLs([]string{"--tag", "Async RUST"}); err != nil {
			t.Fatalf("runLs() error = %v", err)
		}
	})

	if !strings.Contains(got, "a.example") {
		t.Errorf("runLs(--tag %q) = %q, want the bookmark tagged %q", "Async RUST", got, "async-rust")
	}
}

func TestLsWithATagNobodyHasUsedSaysSoOffTheStream(t *testing.T) {
	newVaultForTest(t, "pw")
	addForTest(t, "-t", "go", "https://b.example")

	stdout, stderr := bothStreams(t, func() {
		if err := runLs([]string{"--tag", "rust"}); err != nil {
			t.Fatalf("runLs() error = %v", err)
		}
		// Through dispatch too, because the exit status is the half a script
		// reads: an empty answer to a fair question is success, not a failure.
		if code := dispatch([]string{"ls", "--tag", "rust"}); code != 0 {
			t.Errorf("dispatch(ls --tag rust) = %d, want 0", code)
		}
	})

	// The notice quotes the user's own tag, so on stdout it would make
	// 'bkmr ls --tag rust | grep rust' match and report a bookmark that is not
	// there. The pipe must stay empty when the answer is empty.
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing; the notice echoes the query and would be a false positive in a pipe", stdout)
	}
	if !strings.Contains(stderr, "rust") {
		t.Errorf("stderr = %q, want it to name the tag that matched nothing", stderr)
	}
}

func TestLsOnAnEmptyVaultSaysSo(t *testing.T) {
	newVaultForTest(t, "pw")

	stdout, stderr := bothStreams(t, func() {
		if err := runLs(nil); err != nil {
			t.Fatalf("runLs() error = %v", err)
		}
	})

	if strings.TrimSpace(stdout) == "" {
		t.Error("runLs() on an empty vault printed nothing; want a friendly message")
	}
	// Somebody is going to write 'bkmr ls | grep rust'. The empty-vault notice
	// is program output and belongs on stdout, but nothing else may join it -
	// and no diagnostic may appear either, because there is no fault here.
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing; an empty vault is not a fault", stderr)
	}
}

func TestLsPrintsNewestFirst(t *testing.T) {
	newVaultForTest(t, "pw")
	v, err := openVault()
	if err != nil {
		t.Fatalf("openVault() error = %v", err)
	}
	// The Added times are set here rather than by two runAdd calls: Windows'
	// clock is coarse enough that two adds in a row can share a timestamp, which
	// would make the ordering this command promises untestable.
	if err := v.Mutate(func(c *model.Collection) error {
		c.Bookmarks = []model.Bookmark{
			{ID: "aaaaaaaa", URL: "https://older.example", Added: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
			{ID: "bbbbbbbb", URL: "https://newer.example", Added: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)},
		}
		return nil
	}); err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}

	got := capture(t, func() {
		if err := runLs(nil); err != nil {
			t.Fatalf("runLs() error = %v", err)
		}
	})

	if !strings.HasPrefix(got, "bbbbbbbb") {
		t.Errorf("runLs() = %q, want the newer bookmark first", got)
	}
}

func TestLsShowsTheURLWhenThereIsNoTitle(t *testing.T) {
	newVaultForTest(t, "pw")
	addForTest(t, "https://untitled.example/page")

	got := capture(t, func() {
		if err := runLs(nil); err != nil {
			t.Fatalf("runLs() error = %v", err)
		}
	})

	if !strings.Contains(got, "https://untitled.example/page") {
		t.Errorf("runLs() = %q, want the URL used as the label", got)
	}
}
