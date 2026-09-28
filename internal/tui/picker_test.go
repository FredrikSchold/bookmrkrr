package tui

import (
	"slices"
	"sort"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

	// The exact set, not merely the absence of wrong rows: an implementation
	// that matched nothing at all would satisfy "no visible row lacks rust or
	// ratatui". "Go Docs" is the row that has to be gone, and it is the only
	// one of the three with no r in it anywhere.
	var got []string
	for _, it := range m.Visible() {
		got = append(got, it.ID)
	}
	sort.Strings(got)
	if want := []string{"aaa", "ccc"}; !slices.Equal(got, want) {
		t.Errorf("Visible() IDs for 'rst' = %v, want %v (Rust Book and Ratatui)", got, want)
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

		// Asserted at every size rather than only the wide one. A guard on
		// either dimension would exempt three of these four - all four are
		// under four rows tall - leaving them testing nothing beyond the
		// absence of a panic. View always writes the query line, so "not empty,
		// and the query line comes first" is a real assertion at any size.
		got := m.View()
		if got == "" {
			t.Errorf("View() at %dx%d = empty", size[0], size[1])
			continue
		}
		if !strings.HasPrefix(got, "search") {
			t.Errorf("View() at %dx%d = %q, want the query line first", size[0], size[1], got)
		}
	}
}

// esc is written this way rather than as an escape sequence so that grepping the
// package for a literal escape finds only the styles that emit one.
var esc = string(rune(27))

// withVisibleStyles makes the picker's styles emit escape sequences for the
// duration of one test.
//
// It has to. lipgloss's default renderer sees that go test's stdout is not a
// terminal and strips every attribute, so under test the real styles render
// plain text - styleSelected("x") is just "x" - and an assertion about a severed
// escape would pass however broken the clipping was. These stand-ins emit the
// same shape the real ones do, one opener per styled run and one reset to close
// it, so the counting below is exact.
func withVisibleStyles(t *testing.T) {
	t.Helper()
	oldSelected, oldDetail, oldHelp := styleSelected, styleDetail, styleHelp
	emit := func(code string) func(...string) string {
		return func(parts ...string) string {
			return esc + "[" + code + "m" + strings.Join(parts, "") + esc + "[0m"
		}
	}
	styleSelected, styleDetail, styleHelp = emit("7"), emit("2"), emit("2")
	t.Cleanup(func() {
		styleSelected, styleDetail, styleHelp = oldSelected, oldDetail, oldHelp
	})
}

// A terminal narrower than the widest row must not leave a severed escape
// sequence behind. Styling happens after clipping precisely so that it cannot:
// cut a styled string and the trailing reset is the first thing lost, after
// which reverse video or faint bleeds into the rest of the frame.
func TestANarrowWindowNeverSeversAnEscapeSequence(t *testing.T) {
	withVisibleStyles(t)

	// Every row here is far wider than the widths below, which is the ordinary
	// case: a 40-column split pane against an 80-character URL clips all of them.
	items := []Item{
		{ID: "aaa", Label: "Rust Book", Detail: "https://doc.rust-lang.org/book/" + strings.Repeat("x", 60)},
		{ID: "bbb", Label: "Go Docs", Detail: "https://go.dev/doc/" + strings.Repeat("y", 60)},
	}

	for _, width := range []int{1, 2, 4, 7, 12, 40} {
		m := sizeIt(New(items, "search"), width, 24)
		for _, line := range strings.Split(m.View(), "\n") {
			opens := strings.Count(line, esc+"[7m") + strings.Count(line, esc+"[2m")
			closes := strings.Count(line, esc+"[0m")
			if opens != closes {
				t.Errorf("at width %d, line %q opens %d styled runs but closes %d", width, line, opens, closes)
			}
			if total := strings.Count(line, esc); total != opens+closes {
				t.Errorf("at width %d, line %q holds %d escapes but only %d complete sequences - one was cut", width, line, total, opens+closes)
			}
		}
	}
}

// And the visible text still has to fit, which is what the clipping is for.
func TestRowsFitTheTerminalWidth(t *testing.T) {
	withVisibleStyles(t)

	it := Item{ID: "aaa", Label: "Rust Book", Detail: "https://doc.rust-lang.org/book/"}
	for _, width := range []int{1, 2, 4, 7, 12, 40} {
		for _, selected := range []bool{false, true} {
			if got := lipgloss.Width(renderRow(it, selected, width)); got > width {
				t.Errorf("renderRow(selected=%v, width=%d) is %d columns wide, want at most %d", selected, width, got, width)
			}
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

// Nothing in this package may hard-code words that only make sense for
// bookmarks: Task 11 reuses this picker for browser tabs, and a tab picker
// telling someone to run 'bkmr add <url>' would be nonsense.
func TestACallerCanReplaceTheBookmarkWording(t *testing.T) {
	m := sizeIt(New(nil, "search").WithEmptyMessage("no open tabs"), 80, 24)
	got := m.View()
	if !strings.Contains(got, "no open tabs") {
		t.Errorf("View() = %q, want the caller's own empty message", got)
	}
	if strings.Contains(got, "bkmr add") {
		t.Errorf("View() = %q, want nothing about bkmr add once the caller set its own message", got)
	}

	onTabs := press(sizeIt(New(tagged(), "search").WithTagNoun("tabs"), 80, 24), tea.KeyTab)
	vis := onTabs.Visible()
	if len(vis) == 0 {
		t.Fatal("Visible() = 0 tag rows, want three")
	}
	if want := "2 tabs"; vis[0].Detail != want {
		t.Errorf("tag row Detail = %q, want %q", vis[0].Detail, want)
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
