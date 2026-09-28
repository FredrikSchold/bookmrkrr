package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func tagged() []Item {
	return []Item{
		{ID: "aaa", Label: "Rust Book", Detail: "https://doc.rust-lang.org/book/", Filter: "rust book", Tags: []string{"rust", "reading"}},
		{ID: "bbb", Label: "Go Docs", Detail: "https://go.dev/doc/", Filter: "go docs", Tags: []string{"go"}},
		{ID: "ccc", Label: "Ratatui", Detail: "https://ratatui.rs", Filter: "ratatui", Tags: []string{"rust"}},
		{ID: "ddd", Label: "Untagged", Detail: "https://plain.example", Filter: "untagged"},
	}
}

func press(m Model, k tea.KeyType) Model {
	next, _ := m.Update(tea.KeyMsg{Type: k})
	return next.(Model)
}

func TestTabEntersTagModeListingTagsByCount(t *testing.T) {
	m := press(sizeIt(New(tagged(), "search"), 80, 24), tea.KeyTab)

	if !m.TagMode() {
		t.Fatal("TagMode() = false after Tab, want true")
	}
	vis := m.Visible()
	if len(vis) != 3 {
		t.Fatalf("Visible() = %d tag rows, want 3 (rust, go, reading):\n%+v", len(vis), vis)
	}
	if vis[0].Label != "rust" {
		t.Errorf("Visible()[0] = %q, want rust first - it has the highest count", vis[0].Label)
	}
	if !strings.Contains(vis[0].Detail, "2") {
		t.Errorf("Visible()[0].Detail = %q, want it to show a count of 2", vis[0].Detail)
	}
}

func TestChoosingATagFiltersTheBookmarkList(t *testing.T) {
	m := press(sizeIt(New(tagged(), "search"), 80, 24), tea.KeyTab)
	m = press(m, tea.KeyEnter) // pick "rust"

	if m.TagMode() {
		t.Error("TagMode() = true after choosing a tag, want false")
	}
	if m.ActiveTag() != "rust" {
		t.Errorf("ActiveTag() = %q, want %q", m.ActiveTag(), "rust")
	}
	if m.Done() {
		t.Error("Done() = true after choosing a tag; choosing a tag must not end the picker")
	}

	vis := m.Visible()
	if len(vis) != 2 {
		t.Fatalf("Visible() = %d items, want the 2 rust bookmarks:\n%+v", len(vis), vis)
	}
	for _, it := range vis {
		if it.ID == "bbb" || it.ID == "ddd" {
			t.Errorf("Visible() included %q, which is not tagged rust", it.Label)
		}
	}
}

func TestTagFilterAndTypedQueryCombine(t *testing.T) {
	m := press(sizeIt(New(tagged(), "search"), 80, 24), tea.KeyTab)
	m = press(m, tea.KeyEnter) // rust
	m = typeRunes(m, "rata")

	vis := m.Visible()
	if len(vis) != 1 || vis[0].ID != "ccc" {
		t.Errorf("Visible() = %+v, want only Ratatui", vis)
	}
}

func TestTabAgainClearsAnActiveTagFilter(t *testing.T) {
	m := press(sizeIt(New(tagged(), "search"), 80, 24), tea.KeyTab)
	m = press(m, tea.KeyEnter) // rust
	m = press(m, tea.KeyTab)   // clear it

	if m.ActiveTag() != "" {
		t.Errorf("ActiveTag() = %q, want it cleared", m.ActiveTag())
	}
	if m.TagMode() {
		t.Error("TagMode() = true, want false - Tab on an active filter clears it rather than re-entering tag mode")
	}
	if len(m.Visible()) != 4 {
		t.Errorf("Visible() = %d items, want all 4 back", len(m.Visible()))
	}
}

func TestEscLeavesTagModeWithoutQuitting(t *testing.T) {
	m := press(sizeIt(New(tagged(), "search"), 80, 24), tea.KeyTab)
	m = press(m, tea.KeyEsc)

	if m.TagMode() {
		t.Error("TagMode() = true after Esc, want false")
	}
	if m.Done() {
		t.Error("Done() = true after Esc in tag mode; Esc must only leave tag mode")
	}
	if m.ActiveTag() != "" {
		t.Errorf("ActiveTag() = %q, want none applied", m.ActiveTag())
	}
}

func TestEnterInTagModeOnAnEmptyTagListDoesNothing(t *testing.T) {
	m := press(sizeIt(New([]Item{{ID: "x", Label: "No tags", Filter: "no tags"}}, "search"), 80, 24), tea.KeyTab)

	if len(m.Visible()) != 0 {
		t.Fatalf("Visible() = %d tag rows, want 0", len(m.Visible()))
	}
	m = press(m, tea.KeyEnter)
	if m.ActiveTag() != "" {
		t.Errorf("ActiveTag() = %q, want none", m.ActiveTag())
	}
	if m.Done() {
		t.Error("Done() = true, want false")
	}
}

// Tag mode's rows are tags, not bookmarks, so the row-level actions must not
// fire there at all.
//
// This closes a class rather than an instance. Reaching applyPickerAction with a
// tag row made the copy path write the tag's own Detail to the clipboard and
// report "copied 2 bookmarks" - a success message for something that did not
// happen. Guarding inside Update means no caller can be handed a tag row as
// though it were a bookmark, whatever it chooses to do with one.
func TestRowActionsDoNothingInTagMode(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyCtrlY, tea.KeyCtrlD} {
		m := press(sizeIt(New(tagged(), "search"), 80, 24), tea.KeyTab)
		if !m.TagMode() {
			t.Fatal("TagMode() = false after Tab, want true")
		}

		m = press(m, key)
		item, action := m.Chosen()
		if action != ActionNone {
			t.Errorf("key %v in tag mode chose %v on row %+v, want ActionNone", key, action, item)
		}
		if m.Done() {
			t.Errorf("Done() = true after %v in tag mode, want false", key)
		}
		if !m.TagMode() {
			t.Errorf("TagMode() = false after %v, want it to stay in tag mode", key)
		}
	}
}
