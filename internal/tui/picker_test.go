package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func sample() []Item {
	return []Item{
		{ID: "aaa", Label: "Rust Book", Detail: "https://doc.rust-lang.org/book/", Filter: "rust book doc.rust-lang.org rust"},
		{ID: "bbb", Label: "Go Docs", Detail: "https://go.dev/doc/", Filter: "go docs go.dev golang"},
		{ID: "ccc", Label: "Ratatui", Detail: "https://ratatui.rs", Filter: "ratatui ratatui.rs rust tui"},
	}
}

func typeRunes(m Model, s string) Model {
	for _, r := range s {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}
	return m
}

func sizeIt(m Model, w, h int) Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(Model)
}

func TestAllItemsVisibleBeforeTyping(t *testing.T) {
	m := sizeIt(New(sample(), "search"), 80, 24)

	if len(m.Visible()) != 3 {
		t.Errorf("Visible() = %d items, want 3", len(m.Visible()))
	}
}

func TestTypingFiltersFuzzily(t *testing.T) {
	m := typeRunes(sizeIt(New(sample(), "search"), 80, 24), "rst")

	vis := m.Visible()
	if len(vis) == 0 {
		t.Fatal("Visible() = 0 items, want the fuzzy matches for 'rst'")
	}
	for _, it := range vis {
		if !strings.Contains(it.Filter, "rust") && !strings.Contains(it.Filter, "ratatui") {
			t.Errorf("Visible() included %q, which does not fuzzy-match 'rst'", it.Label)
		}
	}
}

func TestFilteringToNothingLeavesNoSelection(t *testing.T) {
	m := typeRunes(sizeIt(New(sample(), "search"), 80, 24), "zzzzzz")

	if len(m.Visible()) != 0 {
		t.Fatalf("Visible() = %d items, want 0", len(m.Visible()))
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if _, action := m.Chosen(); action != ActionNone {
		t.Errorf("Enter on an empty list chose an action %v, want ActionNone", action)
	}
}

func TestEnterChoosesTheHighlightedItemToOpen(t *testing.T) {
	m := sizeIt(New(sample(), "search"), 80, 24)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	item, action := m.Chosen()
	if action != ActionOpen {
		t.Errorf("action = %v, want ActionOpen", action)
	}
	if item.ID != "bbb" {
		t.Errorf("chosen ID = %q, want %q after one Down", item.ID, "bbb")
	}
	if !m.Done() {
		t.Error("Done() = false after Enter, want true")
	}
}

func TestCtrlCAndEscQuitWithoutChoosing(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyEsc, tea.KeyCtrlC} {
		m := sizeIt(New(sample(), "search"), 80, 24)
		next, _ := m.Update(tea.KeyMsg{Type: key})
		m = next.(Model)

		if _, action := m.Chosen(); action != ActionNone {
			t.Errorf("key %v chose an action, want ActionNone", key)
		}
		if !m.Done() {
			t.Errorf("Done() = false after %v, want true", key)
		}
	}
}

func TestCopyAndDeleteActions(t *testing.T) {
	m := sizeIt(New(sample(), "search"), 80, 24)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	if _, action := next.(Model).Chosen(); action != ActionCopy {
		t.Errorf("ctrl+y action = %v, want ActionCopy", action)
	}

	m = sizeIt(New(sample(), "search"), 80, 24)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if _, action := next.(Model).Chosen(); action != ActionDelete {
		t.Errorf("ctrl+d action = %v, want ActionDelete", action)
	}
}

func TestSelectionStaysInBounds(t *testing.T) {
	m := sizeIt(New(sample(), "search"), 80, 24)
	for i := 0; i < 10; i++ {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(Model)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if item, _ := next.(Model).Chosen(); item.ID != "ccc" {
		t.Errorf("chosen ID = %q, want the last item after many Downs", item.ID)
	}

	m = sizeIt(New(sample(), "search"), 80, 24)
	for i := 0; i < 10; i++ {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = next.(Model)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if item, _ := next.(Model).Chosen(); item.ID != "aaa" {
		t.Errorf("chosen ID = %q, want the first item after many Ups", item.ID)
	}
}

// Review Focus 4: a zero-height or absurdly small window must not panic.
func TestViewSurvivesADegenerateWindow(t *testing.T) {
	for _, size := range [][2]int{{0, 0}, {1, 1}, {80, 2}, {3, 0}} {
		m := sizeIt(New(sample(), "search"), size[0], size[1])
		if got := m.View(); got == "" && size[0] > 3 {
			t.Errorf("View() at %dx%d = empty", size[0], size[1])
		}
	}
}

func TestViewOnAnEmptyCollectionExplainsItself(t *testing.T) {
	m := sizeIt(New(nil, "search"), 80, 24)

	got := m.View()
	if !strings.Contains(strings.ToLower(got), "no bookmarks") {
		t.Errorf("View() with no items = %q, want it to say there are no bookmarks", got)
	}
}

func TestBackspaceRestoresFilteredItems(t *testing.T) {
	m := typeRunes(sizeIt(New(sample(), "search"), 80, 24), "go")
	if len(m.Visible()) == 3 {
		t.Fatal("typing 'go' did not narrow the list")
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(Model)

	if len(m.Visible()) != 3 {
		t.Errorf("Visible() = %d after clearing the query, want 3", len(m.Visible()))
	}
}
