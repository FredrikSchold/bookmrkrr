// Package tui holds one reusable fuzzy picker. Both bookmark retrieval and
// browser-tab capture use it, so there is exactly one list widget to maintain.
//
// It knows nothing about vaults or bookmarks: callers hand it Item values and
// read back the row the user settled on. That is what keeps it reusable, and
// it is why this package imports neither internal/store nor internal/model.
package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

// Action is what the user asked to do with the row they chose.
type Action int

const (
	ActionNone Action = iota
	ActionOpen
	ActionCopy
	ActionDelete
)

// Item is one row. Filter is the haystack fuzzy matching runs against, and
// Tags feeds tag mode.
type Item struct {
	ID     string
	Label  string
	Detail string
	Filter string
	Tags   []string
}

var (
	styleSelected = lipgloss.NewStyle().Bold(true).Reverse(true)
	styleDetail   = lipgloss.NewStyle().Faint(true)
	styleHelp     = lipgloss.NewStyle().Faint(true)
)

// Model is the picker's bubbletea model.
type Model struct {
	prompt  string
	all     []Item
	visible []Item
	cursor  int
	input   textinput.Model
	width   int
	height  int
	chosen  Item
	action  Action
	done    bool

	tagMode   bool
	activeTag string
	tagRows   []Item
}

// New builds a picker over items.
func New(items []Item, prompt string) Model {
	in := textinput.New()
	in.Prompt = prompt + " "
	in.Focus()

	m := Model{prompt: prompt, all: items, input: in, width: 80, height: 24}
	m.buildTagRows()
	m.refilter()
	return m
}

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd { return textinput.Blink }

// Visible returns the current filtered, ranked rows.
func (m Model) Visible() []Item { return m.visible }

// Done reports whether the picker has finished.
func (m Model) Done() bool { return m.done }

// Chosen returns the selected row and the action requested. The action is
// ActionNone when the user quit without choosing.
func (m Model) Chosen() (Item, Action) { return m.chosen, m.action }

// TagMode reports whether the picker is showing tags rather than bookmarks.
func (m Model) TagMode() bool { return m.tagMode }

// ActiveTag is the tag currently narrowing the list, or "".
func (m Model) ActiveTag() string { return m.activeTag }

// buildTagRows lists every tag with its count, most-used first and
// alphabetical within a count.
func (m *Model) buildTagRows() {
	counts := map[string]int{}
	for _, it := range m.all {
		for _, t := range it.Tags {
			counts[t]++
		}
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})

	m.tagRows = make([]Item, 0, len(names))
	for _, name := range names {
		m.tagRows = append(m.tagRows, Item{
			ID:     name,
			Label:  name,
			Detail: fmt.Sprintf("%d bookmarks", counts[name]),
			Filter: name,
		})
	}
}

func (m *Model) refilter() {
	pool := m.all
	if m.tagMode {
		pool = m.tagRows
	} else if m.activeTag != "" {
		// pool[:0:0] shares no backing array with m.all, so filtering never
		// overwrites the caller's items.
		pool = pool[:0:0]
		for _, it := range m.all {
			for _, t := range it.Tags {
				if t == m.activeTag {
					pool = append(pool, it)
					break
				}
			}
		}
	}

	q := strings.TrimSpace(m.input.Value())
	if q == "" {
		m.visible = pool
	} else {
		hay := make([]string, len(pool))
		for i, it := range pool {
			hay[i] = it.Filter
		}
		matches := fuzzy.Find(q, hay)
		m.visible = make([]Item, 0, len(matches))
		for _, mt := range matches {
			m.visible = append(m.visible, pool[mt.Index])
		}
	}

	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m Model) finish(a Action) (tea.Model, tea.Cmd) {
	if a != ActionNone && m.cursor < len(m.visible) {
		m.chosen = m.visible[m.cursor]
		m.action = a
	}
	m.done = true
	return m, tea.Quit
}

// Update satisfies tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m.finish(ActionNone)
		case tea.KeyEsc:
			// In tag mode Esc only leaves tag mode; otherwise it quits.
			if m.tagMode {
				m.tagMode = false
				m.input.SetValue("")
				m.cursor = 0
				m.refilter()
				return m, nil
			}
			return m.finish(ActionNone)
		case tea.KeyTab:
			if m.activeTag != "" {
				m.activeTag = ""
			} else {
				m.tagMode = !m.tagMode
			}
			m.input.SetValue("")
			m.cursor = 0
			m.refilter()
			return m, nil
		case tea.KeyEnter:
			if m.tagMode {
				if m.cursor < len(m.visible) {
					m.activeTag = m.visible[m.cursor].ID
					m.tagMode = false
					m.input.SetValue("")
					m.cursor = 0
					m.refilter()
				}
				return m, nil
			}
			return m.finish(ActionOpen)
		case tea.KeyCtrlY:
			return m.finish(ActionCopy)
		case tea.KeyCtrlD:
			return m.finish(ActionDelete)
		case tea.KeyUp, tea.KeyCtrlP:
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case tea.KeyDown, tea.KeyCtrlN:
			if m.cursor < len(m.visible)-1 {
				m.cursor++
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.refilter()
	return m, cmd
}

// View satisfies tea.Model. It must tolerate a degenerate window size.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(m.input.View())
	b.WriteString("\n")

	if m.tagMode {
		b.WriteString(styleHelp.Render("tag mode - enter to filter by a tag, tab or esc to go back") + "\n")
	} else if m.activeTag != "" {
		b.WriteString(styleHelp.Render("filtering by tag: "+m.activeTag+" (tab to clear)") + "\n")
	}

	if len(m.all) == 0 {
		b.WriteString("no bookmarks yet - add one with 'bkmr add <url>'\n")
		return b.String()
	}

	// Reserve the query line, the mode banner, the help line, and one blank.
	rows := m.height - 4
	if rows < 1 {
		rows = 1
	}
	if rows > len(m.visible) {
		rows = len(m.visible)
	}

	start := 0
	if m.cursor >= rows {
		start = m.cursor - rows + 1
	}
	for i := start; i < start+rows && i < len(m.visible); i++ {
		it := m.visible[i]
		line := fmt.Sprintf("%s  %s", it.Label, styleDetail.Render(it.Detail))
		if i == m.cursor {
			line = styleSelected.Render(" "+it.Label+" ") + " " + styleDetail.Render(it.Detail)
		}
		b.WriteString(truncate(line, m.width) + "\n")
	}
	if len(m.visible) == 0 {
		b.WriteString("no matches\n")
	}
	b.WriteString(styleHelp.Render("enter open · tab tags · ctrl+y copy · ctrl+d delete · esc quit"))
	return b.String()
}

func truncate(s string, width int) string {
	if width <= 0 {
		return s
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	r := []rune(s)
	if len(r) > width {
		r = r[:width]
	}
	return string(r)
}

// Run displays the picker and returns what the user chose.
func Run(m Model) (Item, Action, error) {
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return Item{}, ActionNone, err
	}
	fm, ok := final.(Model)
	if !ok {
		return Item{}, ActionNone, fmt.Errorf("unexpected final model %T", final)
	}
	item, action := fm.Chosen()
	return item, action, nil
}
