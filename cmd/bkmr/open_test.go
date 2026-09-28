package main

import (
	"strings"
	"testing"

	"github.com/FredrikSchold/bookmrkrr/internal/tui"
)

// stubOpen replaces the browser launcher for one test and returns a pointer to
// the URL it was handed.
//
// Every test in this file installs it, including the two that are not supposed
// to reach it. Leaving those two to an early return would make "go test never
// opens a browser window" an accident of the current control flow rather than a
// property of the suite: a change to ranking, or a default title that made a
// query match something, would silently start launching a real browser on
// whoever ran the tests.
func stubOpen(t *testing.T) *string {
	t.Helper()
	var opened string
	old := tui.Open
	tui.Open = func(url string) error { opened = url; return nil }
	t.Cleanup(func() { tui.Open = old })
	return &opened
}

func TestOpenFindsTheBestMatchAndOpensIt(t *testing.T) {
	newVaultForTest(t, "pw")
	// capture, because runAdd reports each save on out and the test binary's
	// stdout is not the place for it. This test asserts on the opened URL, not
	// on output, so silencing the adds hides nothing.
	capture(t, func() {
		runAdd([]string{"--no-fetch", "--title", "Rust Book", "https://doc.rust-lang.org/book/"})
		runAdd([]string{"--no-fetch", "--title", "Go Docs", "https://go.dev/doc/"})
	})
	opened := stubOpen(t)

	if err := runOpen([]string{"rust"}); err != nil {
		t.Fatalf("runOpen() error = %v", err)
	}
	if *opened != "https://doc.rust-lang.org/book/" {
		t.Errorf("opened %q, want the Rust Book URL", *opened)
	}
}

func TestOpenAcceptsAnID(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://byid.example"}) })

	v, _ := openVault()
	c, _ := v.Load()
	id := c.Bookmarks[0].ID
	opened := stubOpen(t)

	if err := runOpen([]string{id}); err != nil {
		t.Fatalf("runOpen() error = %v", err)
	}
	if *opened != "https://byid.example" {
		t.Errorf("opened %q, want https://byid.example", *opened)
	}
}

func TestOpenRecordsTheVisit(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://visited.example"}) })
	stubOpen(t)

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
	opened := stubOpen(t)

	err := runOpen([]string{"nothingmatchesthis"})
	if err == nil || !strings.Contains(err.Error(), "no bookmark") {
		t.Errorf("runOpen() error = %v, want a no-match error", err)
	}
	if *opened != "" {
		t.Errorf("opened %q, want nothing; there was no match to open", *opened)
	}
}

func TestOpenWithNoArgumentIsAUsageError(t *testing.T) {
	newVaultForTest(t, "pw")
	opened := stubOpen(t)

	if err := runOpen(nil); err == nil {
		t.Error("runOpen(nil) error = nil, want a usage error")
	}
	if *opened != "" {
		t.Errorf("opened %q, want nothing; there was no query at all", *opened)
	}
}
