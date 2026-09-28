package main

import (
	"strings"
	"testing"
)

func TestTagsListsCountsSorted(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() {
		runAdd([]string{"--no-fetch", "-t", "rust", "-t", "web", "https://a.example"})
		runAdd([]string{"--no-fetch", "-t", "rust", "https://b.example"})
	})

	got := capture(t, func() {
		if err := runTags(nil); err != nil {
			t.Fatalf("runTags() error = %v", err)
		}
	})

	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("runTags() printed %d lines, want 2:\n%s", len(lines), got)
	}
	if !strings.Contains(lines[0], "rust") || !strings.Contains(lines[0], "2") {
		t.Errorf("first line = %q, want rust with a count of 2", lines[0])
	}
	if !strings.Contains(lines[1], "web") {
		t.Errorf("second line = %q, want web", lines[1])
	}
}

func TestTagsOnAnUntaggedVaultSaysSo(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://a.example"}) })

	got := capture(t, func() { runTags(nil) })
	if !strings.Contains(strings.ToLower(got), "no tags") {
		t.Errorf("runTags() = %q, want it to report no tags", got)
	}
}
