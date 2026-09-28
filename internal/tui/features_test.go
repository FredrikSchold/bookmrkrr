package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// allKeys is every key the picker acts on. The tag-row test below presses all of
// them, so a key added to Update without a thought for tag mode fails there
// rather than shipping.
var allKeys = []tea.KeyType{
	tea.KeyEnter, tea.KeyTab, tea.KeyCtrlY, tea.KeyCtrlD,
	tea.KeyEsc, tea.KeyCtrlC, tea.KeyUp, tea.KeyDown, tea.KeyCtrlP, tea.KeyCtrlN,
}

func TestTheDefaultPickerSupportsEverything(t *testing.T) {
	m := New(sample(), "search")
	for _, f := range []Features{FeatureOpen, FeatureCopy, FeatureDelete, FeatureTags} {
		if !m.Supports(f) {
			t.Errorf("Supports(%v) = false on a default picker, want true", f)
		}
	}
}

// The help line is rendered from the same set Update consults, so it can never
// advertise a key that does nothing. This is the loose end Task 10 left: the
// line used to be a hard-coded string.
func TestTheHelpLineListsOnlyTheSupportedFeatures(t *testing.T) {
	full := sizeIt(New(sample(), "search"), 80, 24).View()
	for _, want := range []string{"enter", "tab tags", "ctrl+y", "ctrl+d", "esc quit"} {
		if !strings.Contains(full, want) {
			t.Errorf("default View() = %q, want it to advertise %q", full, want)
		}
	}

	only := sizeIt(New(sample(), "search").WithFeatures(FeatureOpen), 80, 24).View()
	for _, unwanted := range []string{"tab tags", "ctrl+y", "ctrl+d"} {
		if strings.Contains(only, unwanted) {
			t.Errorf("View() = %q, want no mention of %q on a picker that does not support it", only, unwanted)
		}
	}
	if !strings.Contains(only, "enter") || !strings.Contains(only, "esc quit") {
		t.Errorf("View() = %q, want it to still advertise enter and esc", only)
	}
}

// An unsupported action's key is refused by Update itself, not by whatever the
// caller does with the result: a caller that cannot copy must not be handed
// ActionCopy at all.
func TestAnUnsupportedKeyDoesNothing(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyCtrlY, tea.KeyCtrlD, tea.KeyTab} {
		m := press(sizeIt(New(tagged(), "search").WithFeatures(FeatureOpen), 80, 24), key)

		item, action := m.Chosen()
		if action != ActionNone {
			t.Errorf("key %v chose %v on row %+v, want ActionNone when the feature is off", key, action, item)
		}
		if m.Done() {
			t.Errorf("Done() = true after %v, want false when the feature is off", key)
		}
		if m.TagMode() {
			t.Errorf("TagMode() = true after %v, want false when tag mode is off", key)
		}
	}
}

// Turning features off must not break the one that is left on.
func TestASupportedFeatureStillFiresWhenTheOthersAreOff(t *testing.T) {
	m := press(sizeIt(New(sample(), "search").WithFeatures(FeatureOpen), 80, 24), tea.KeyEnter)

	item, action := m.Chosen()
	if action != ActionOpen {
		t.Fatalf("Chosen() action = %v, want ActionOpen", action)
	}
	if item.ID != sample()[0].ID {
		t.Errorf("Chosen() item = %+v, want the first row", item)
	}
	if !m.Done() {
		t.Error("Done() = false after enter, want true")
	}
}

// Copy and delete stay off when only tag browsing is added, so a caller can
// have a read-only list that still filters by tag.
func TestFeaturesCombine(t *testing.T) {
	m := New(tagged(), "search").WithFeatures(FeatureOpen | FeatureTags)
	if !m.Supports(FeatureTags) || !m.Supports(FeatureOpen) {
		t.Fatal("Supports() = false for a feature that was asked for")
	}
	if m.Supports(FeatureCopy) || m.Supports(FeatureDelete) {
		t.Error("Supports() = true for a feature that was not asked for")
	}
	if tagged := press(sizeIt(m, 80, 24), tea.KeyTab); !tagged.TagMode() {
		t.Error("TagMode() = false after Tab, want true when tag browsing is supported")
	}
}

// Tag mode's rows are tags, not the caller's items, so no key at all may end
// the picker with one selected. The action set does not give this on its own -
// the bookmark picker supports copy and delete and still must not fire either
// on a tag row - so Update checks the mode as well, and this test covers the
// whole keymap rather than the two keys that once regressed.
func TestNoKeyHandsBackATagRow(t *testing.T) {
	for _, key := range allKeys {
		m := press(sizeIt(New(tagged(), "search"), 80, 24), tea.KeyTab)
		if !m.TagMode() {
			t.Fatal("TagMode() = false after Tab, want true")
		}
		if len(m.Visible()) == 0 {
			t.Fatal("Visible() = 0 tag rows, want some to highlight")
		}

		m = press(m, key)
		item, action := m.Chosen()
		if action != ActionNone {
			t.Errorf("key %v in tag mode chose %v on tag row %+v, want ActionNone", key, action, item)
		}
	}
}

// The feature set is not the whole of what Update checks - it refuses copy and
// delete on a tag row too - so the help line has to reflect that as well, or it
// advertises two keys that do nothing the moment tag mode opens.
func TestTheHelpLineDropsRowActionsInTagMode(t *testing.T) {
	m := press(sizeIt(New(tagged(), "search"), 80, 24), tea.KeyTab)
	if !m.TagMode() {
		t.Fatal("TagMode() = false after Tab, want true")
	}

	got := m.View()
	for _, unwanted := range []string{"ctrl+y", "ctrl+d"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("View() in tag mode = %q, want no mention of %q - it does nothing on a tag row", got, unwanted)
		}
	}
	// Enter still chooses the highlighted tag, and tab still leaves tag mode.
	for _, want := range []string{"enter", "tab tags", "esc"} {
		if !strings.Contains(got, want) {
			t.Errorf("View() in tag mode = %q, want it to still advertise %q", got, want)
		}
	}
}
