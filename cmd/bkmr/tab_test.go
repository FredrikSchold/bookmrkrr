package main

import (
	"errors"
	"strings"
	"testing"
)

func TestTabSavesTheChosenTabWithoutFetching(t *testing.T) {
	newVaultForTest(t, "pw")
	old := chooseTab
	chooseTab = func(items []tabChoice) (tabChoice, bool, error) {
		if len(items) != 2 {
			t.Fatalf("chooseTab got %d tabs, want 2", len(items))
		}
		return items[1], true, nil
	}
	defer func() { chooseTab = old }()
	stubTabs(t, "https://one.example", "One", "https://two.example", "Two")

	got := capture(t, func() {
		if err := runTab([]string{"-t", "reading"}); err != nil {
			t.Fatalf("runTab() error = %v", err)
		}
	})
	if !strings.Contains(got, "two.example") {
		t.Errorf("runTab() output = %q, want it to name the saved tab", got)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 1 {
		t.Fatalf("len(Bookmarks) = %d, want 1", len(c.Bookmarks))
	}
	if c.Bookmarks[0].Title != "Two" {
		t.Errorf("Title = %q, want %q taken from the browser", c.Bookmarks[0].Title, "Two")
	}
	if strings.Join(c.Bookmarks[0].Tags, ",") != "reading" {
		t.Errorf("Tags = %v, want [reading]", c.Bookmarks[0].Tags)
	}
}

func TestTabCancelledSavesNothing(t *testing.T) {
	newVaultForTest(t, "pw")
	old := chooseTab
	chooseTab = func([]tabChoice) (tabChoice, bool, error) { return tabChoice{}, false, nil }
	defer func() { chooseTab = old }()
	stubTabs(t, "https://one.example", "One")

	if err := runTab(nil); err != nil {
		t.Fatalf("runTab() error = %v", err)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 0 {
		t.Errorf("len(Bookmarks) = %d, want 0 after cancelling", len(c.Bookmarks))
	}
}

func TestTabWithNoBrowserPrintsTheHint(t *testing.T) {
	newVaultForTest(t, "pw")
	stubNoBrowser(t)

	err := runTab(nil)
	if err == nil {
		t.Fatal("runTab() error = nil, want an error when no browser answers")
	}
	if !strings.Contains(err.Error(), "--remote-debugging-port=9222") {
		t.Errorf("runTab() error = %q, want it to include the debug port hint", err)
	}
}

func TestTabWithNoOpenPagesSaysSo(t *testing.T) {
	newVaultForTest(t, "pw")
	stubTabs(t)

	got := capture(t, func() {
		if err := runTab(nil); err != nil {
			t.Fatalf("runTab() error = %v", err)
		}
	})
	if !strings.Contains(strings.ToLower(got), "no open tabs") {
		t.Errorf("runTab() output = %q, want it to report no open tabs", got)
	}
}

// 'bkmr tab' takes no positional argument, and somebody who types 'bkmr tab
// https://...' expecting 'bkmr add' must not have the URL silently dropped and
// an unrelated tab saved instead. The refusal happens before anything else, so
// neither the vault nor the browser is touched.
func TestTabRefusesAPositionalArgument(t *testing.T) {
	newVaultForTest(t, "pw")
	stubNoBrowser(t)

	var err error
	stderr := captureErr(t, func() { err = runTab([]string{"https://example.com"}) })

	if !errors.Is(err, errUsage) {
		t.Fatalf("runTab() error = %v, want errUsage", err)
	}
	if !strings.Contains(stderr, "bkmr:") {
		t.Errorf("stderr = %q, want a line explaining the refusal", stderr)
	}
}

// hasControls reports whether s contains a character a terminal would act on.
func hasControls(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return true
		}
	}
	return false
}

// The same rule, shown closed at two different doors. A page controls its own
// document.title, so a browser tab and a fetched page are both attacker-
// influenced; --title is the user's own, but it is the third way in and the rule
// has one owner, not three.
//
// The tab case also covers the picker: the title is cleaned when the choices are
// built, before anything is displayed, so an ESC cannot reach the frame the
// picker draws either.
func TestATitleWithEscapesIsStrippedAtEveryDoor(t *testing.T) {
	const nasty = "\x1b[2JCleared\x07\u009b31m"

	t.Run("through bkmr tab", func(t *testing.T) {
		newVaultForTest(t, "pw")
		var shown []tabChoice
		old := chooseTab
		chooseTab = func(items []tabChoice) (tabChoice, bool, error) {
			shown = items
			return items[0], true, nil
		}
		defer func() { chooseTab = old }()
		stubTabs(t, "https://nasty.example", nasty)

		capture(t, func() {
			if err := runTab(nil); err != nil {
				t.Fatalf("runTab() error = %v", err)
			}
		})

		if len(shown) != 1 {
			t.Fatalf("chooseTab got %d choices, want 1", len(shown))
		}
		if hasControls(shown[0].Title) {
			t.Errorf("the picker was handed %q, which still contains a control character", shown[0].Title)
		}

		v, _ := openVault()
		c, _ := v.Load()
		if len(c.Bookmarks) != 1 {
			t.Fatalf("len(Bookmarks) = %d, want 1", len(c.Bookmarks))
		}
		if hasControls(c.Bookmarks[0].Title) {
			t.Errorf("stored Title = %q, want no control characters", c.Bookmarks[0].Title)
		}
		if want := "[2JCleared31m"; c.Bookmarks[0].Title != want {
			t.Errorf("stored Title = %q, want %q", c.Bookmarks[0].Title, want)
		}
	})

	t.Run("through the --title flag", func(t *testing.T) {
		newVaultForTest(t, "pw")

		capture(t, func() {
			if err := runAdd([]string{"--no-fetch", "--title", nasty, "https://nasty.example"}); err != nil {
				t.Fatalf("runAdd() error = %v", err)
			}
		})

		v, _ := openVault()
		c, _ := v.Load()
		if len(c.Bookmarks) != 1 {
			t.Fatalf("len(Bookmarks) = %d, want 1", len(c.Bookmarks))
		}
		if hasControls(c.Bookmarks[0].Title) {
			t.Errorf("stored Title = %q, want no control characters", c.Bookmarks[0].Title)
		}
		if want := "[2JCleared31m"; c.Bookmarks[0].Title != want {
			t.Errorf("stored Title = %q, want %q", c.Bookmarks[0].Title, want)
		}
	})

	t.Run("through the --note flag", func(t *testing.T) {
		newVaultForTest(t, "pw")

		capture(t, func() {
			if err := runAdd([]string{"--no-fetch", "--note", nasty, "https://nasty.example"}); err != nil {
				t.Fatalf("runAdd() error = %v", err)
			}
		})

		v, _ := openVault()
		c, _ := v.Load()
		if len(c.Bookmarks) != 1 {
			t.Fatalf("len(Bookmarks) = %d, want 1", len(c.Bookmarks))
		}
		if hasControls(c.Bookmarks[0].Notes) {
			t.Errorf("stored Notes = %q, want no control characters", c.Bookmarks[0].Notes)
		}
	})
}
