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
