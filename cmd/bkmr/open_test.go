package main

import (
	"strings"
	"testing"

	"github.com/FredrikSchold/bookmrkrr/internal/tui"
)

func TestOpenFindsTheBestMatchAndOpensIt(t *testing.T) {
	newVaultForTest(t, "pw")
	// capture, because runAdd reports each save on out and the test binary's
	// stdout is not the place for it. The brief's other open tests wrap their
	// adds the same way; this one asserts on the opened URL, not on output, so
	// nothing is hidden by silencing it.
	capture(t, func() {
		runAdd([]string{"--no-fetch", "--title", "Rust Book", "https://doc.rust-lang.org/book/"})
		runAdd([]string{"--no-fetch", "--title", "Go Docs", "https://go.dev/doc/"})
	})

	var opened string
	old := tui.Open
	tui.Open = func(url string) error { opened = url; return nil }
	defer func() { tui.Open = old }()

	if err := runOpen([]string{"rust"}); err != nil {
		t.Fatalf("runOpen() error = %v", err)
	}
	if opened != "https://doc.rust-lang.org/book/" {
		t.Errorf("opened %q, want the Rust Book URL", opened)
	}
}

func TestOpenAcceptsAnID(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://byid.example"}) })

	v, _ := openVault()
	c, _ := v.Load()
	id := c.Bookmarks[0].ID

	var opened string
	old := tui.Open
	tui.Open = func(url string) error { opened = url; return nil }
	defer func() { tui.Open = old }()

	if err := runOpen([]string{id}); err != nil {
		t.Fatalf("runOpen() error = %v", err)
	}
	if opened != "https://byid.example" {
		t.Errorf("opened %q, want https://byid.example", opened)
	}
}

func TestOpenRecordsTheVisit(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://visited.example"}) })

	old := tui.Open
	tui.Open = func(string) error { return nil }
	defer func() { tui.Open = old }()

	if err := runOpen([]string{"visited"}); err != nil {
		t.Fatal(err)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if c.Bookmarks[0].Opens != 1 {
		t.Errorf("Opens = %d, want 1", c.Bookmarks[0].Opens)
	}
	if c.Bookmarks[0].Visited == nil {
		t.Error("Visited = nil, want a timestamp after opening")
	}
}

func TestOpenWithNoMatchFails(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://example.com"}) })

	err := runOpen([]string{"nothingmatchesthis"})
	if err == nil || !strings.Contains(err.Error(), "no bookmark") {
		t.Errorf("runOpen() error = %v, want a no-match error", err)
	}
}

func TestOpenWithNoArgumentIsAUsageError(t *testing.T) {
	newVaultForTest(t, "pw")

	if err := runOpen(nil); err == nil {
		t.Error("runOpen(nil) error = nil, want a usage error")
	}
}
