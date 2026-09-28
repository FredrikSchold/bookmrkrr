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

// Features is the set of things a picker lets the user do. A caller declares
// the set once and both Update and the help line read that one declaration, so
// a key the caller cannot act on is neither accepted nor advertised.
//
// Task 10 left this open deliberately: the help line was a hard-coded string
// while Update decided what to accept from the mode it was in, and
// parameterizing only the wording would have let the two disagree. The first
// caller that needs a reduced set is Task 11's tab picker - picking a tab means
// "choose this one", so copy, delete and tag browsing are all meaningless
// there, and tabs carry no tags for tag mode to list.
//
// A set rather than a Feature-per-field struct because the members are
// independent, unordered, and only ever tested for membership; and a bitmask
// rather than a map because Model is copied by value on every Update and a map
// would be shared between those copies.
type Features uint

const (
	// FeatureOpen is enter: choose the highlighted row (ActionOpen).
	FeatureOpen Features = 1 << iota
	// FeatureCopy is ctrl+y (ActionCopy).
	FeatureCopy
	// FeatureDelete is ctrl+d (ActionDelete).
	FeatureDelete
	// FeatureTags is tab: browse the tags and filter by one. It is a mode the
	// picker handles itself rather than an Action it hands back, which is why
	// it is a Feature and not an Action.
	FeatureTags
)

// AllFeatures is what New gives a picker, so the common caller needs no
// ceremony and every picker written before this existed behaves as it did.
const AllFeatures = FeatureOpen | FeatureCopy | FeatureDelete | FeatureTags

// featureHelp is the help line, in display order. Nothing else may word these:
// View renders the line by filtering this table through Supports, which is the
// same set Update consults, so the line cannot name a key that does nothing.
//
// "choose" rather than "open", because enter does not open anything in a tab
// picker - it picks the tab to save. It is the one word that is honest for
// every caller, and it is already this package's own name for what enter does
// (see Chosen).
// rowOnly marks a feature that needs the highlighted row to be one of the
// caller's own items, so it is not advertised in tag mode, where the rows are
// tags. Enter is not one of them: in tag mode it chooses the highlighted tag,
// which is still choosing. Tab is not either - it leaves tag mode.
var featureHelp = []struct {
	feature Features
	text    string
	rowOnly bool
}{
	{FeatureOpen, "enter choose", false},
	{FeatureTags, "tab tags", false},
	{FeatureCopy, "ctrl+y copy", true},
	{FeatureDelete, "ctrl+d delete", true},
}

// featureFor is the action-to-feature mapping act needs. ActionNone maps to no
// feature, so quitting can never be switched off.
func featureFor(a Action) Features {
	switch a {
	case ActionOpen:
		return FeatureOpen
	case ActionCopy:
		return FeatureCopy
	case ActionDelete:
		return FeatureDelete
	}
	return 0
}

// Item is one row. Filter is the haystack fuzzy matching runs against, and
// Tags feeds tag mode.
type Item struct {
	ID     string
	Label  string
	Detail string
	Filter string
	Tags   []string
}

// The styles are the Render methods rather than the Style values, so a test can
// swap in renderers that actually emit escape sequences. It has to be able to:
// lipgloss's default renderer sees that go test's stdout is not a terminal and
// strips every attribute, so a row rendered with these under test contains no
// escapes at all, and an assertion about severing one would pass however broken
// the clipping was.
//
// They are never reassigned outside tests, and a test that swaps them must not
// call t.Parallel() or run alongside anything else in this package: these are
// plain package-level variables with no synchronisation, so concurrent use would
// be a genuine data race the moment CI runs with -race. internal/store carries
// the same warning on its renameFile seam, for the same reason.
var (
	styleSelected = lipgloss.NewStyle().Bold(true).Reverse(true).Render
	styleDetail   = lipgloss.NewStyle().Faint(true).Render
	styleHelp     = lipgloss.NewStyle().Faint(true).Render
)

// Model is the picker's bubbletea model.
type Model struct {
	all     []Item
	visible []Item
	cursor  int
	input   textinput.Model
	width   int
	height  int
	chosen  Item
	action  Action
	done    bool

	// emptyMessage and tagNoun are the only words in this package that name
	// what is being picked, and both are replaceable. Nothing else here may
	// mention bookmarks: Task 11 reuses this picker for browser tabs, and a
	// tab picker telling someone to run 'bkmr add <url>' would be nonsense.
	emptyMessage string
	tagNoun      string

	// features is what this picker's caller can act on. See Features.
	features Features

	tagMode   bool
	activeTag string
	tagRows   []Item
}

// New builds a picker over items.
func New(items []Item, prompt string) Model {
	in := textinput.New()
	in.Prompt = prompt + " "
	in.Focus()

	m := Model{
		all:    items,
		input:  in,
		width:  80,
		height: 24,
		// The defaults name bookmarks, because that is the caller this package
		// was written for and the common case should need no ceremony.
		emptyMessage: "no bookmarks yet - add one with 'bkmr add <url>'",
		tagNoun:      "bookmarks",
		// Everything, so a caller that says nothing gets the full picker.
		features: AllFeatures,
	}
	m.buildTagRows()
	m.refilter()
	return m
}

// WithEmptyMessage replaces the line shown when there is nothing at all to pick.
func (m Model) WithEmptyMessage(msg string) Model {
	m.emptyMessage = msg
	return m
}

// WithTagNoun names what a tag's count counts, for tag mode's "3 bookmarks". It
// rebuilds the tag rows, so it may be called at any point after New.
func (m Model) WithTagNoun(noun string) Model {
	m.tagNoun = noun
	m.buildTagRows()
	m.refilter()
	return m
}

// WithFeatures replaces the set of things this picker supports. It is a
// replacement rather than an addition: a caller that lists what it can do is
// stating the whole of it, and a picker that quietly kept a default it was not
// told to keep is how the help line and the behaviour drift apart again.
func (m Model) WithFeatures(f Features) Model {
	m.features = f
	return m
}

// Supports reports whether every feature in f is supported.
func (m Model) Supports(f Features) bool { return m.features&f == f }

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
			Detail: fmt.Sprintf("%d %s", counts[name], m.tagNoun),
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

// act finishes with a, or does nothing at all.
//
// Two checks, not one. The feature set answers "can this caller act on a row
// this way", which is what stops a tab picker being handed ActionCopy. The mode
// answers "is the highlighted row one of the caller's items at all", which the
// feature set cannot: the bookmark picker supports copy and delete and still
// must not fire either on a tag row. Task 10 shipped that regression once -
// ctrl+y put a tag row's "2 bookmarks" count on the clipboard and reported it
// as a success - so both stay.
func (m Model) act(a Action) (tea.Model, tea.Cmd) {
	f := featureFor(a)
	if f == 0 || !m.Supports(f) || m.tagMode {
		return m, nil
	}
	return m.finish(a)
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
			if !m.Supports(FeatureTags) {
				return m, nil
			}
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
			// In tag mode enter commits the highlighted tag as a filter. That
			// belongs to FeatureTags, not to FeatureOpen, and it is only
			// reachable at all when FeatureTags let the picker into tag mode.
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
			return m.act(ActionOpen)
		case tea.KeyCtrlY:
			return m.act(ActionCopy)
		case tea.KeyCtrlD:
			return m.act(ActionDelete)
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

	banner := 0
	switch {
	case m.tagMode:
		b.WriteString(styleHelp("tag mode - enter to filter by a tag, tab or esc to go back") + "\n")
		banner = 1
	case m.activeTag != "":
		b.WriteString(styleHelp("filtering by tag: "+m.activeTag+" (tab to clear)") + "\n")
		banner = 1
	}

	if len(m.all) == 0 {
		b.WriteString(m.emptyMessage + "\n")
		return b.String()
	}

	// Reserve the query line, the help line, one blank, and the mode banner
	// only when one was actually printed, so the common case does not give up a
	// row to a banner that is not there.
	rows := m.height - 3 - banner
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
		b.WriteString(renderRow(m.visible[i], i == m.cursor, m.width) + "\n")
	}
	if len(m.visible) == 0 {
		b.WriteString("no matches\n")
	}
	b.WriteString(styleHelp(m.helpLine()))
	return b.String()
}

// helpLine advertises exactly the features this picker supports, and nothing
// else. Esc is not in the table: quitting is always available, so there is no
// state in which naming it would be a lie.
func (m Model) helpLine() string {
	parts := make([]string, 0, len(featureHelp)+1)
	for _, fh := range featureHelp {
		if fh.rowOnly && m.tagMode {
			// act refuses these on a tag row, so advertising them here would
			// be the same lie the hard-coded line used to tell.
			continue
		}
		if m.Supports(fh.feature) {
			parts = append(parts, fh.text)
		}
	}
	parts = append(parts, "esc quit")
	return strings.Join(parts, " · ")
}

// renderRow lays one row out in plain text, clips it to the terminal's width,
// and only then applies the styles.
//
// The order is the whole point. Clipping a string that has already been styled
// cuts the trailing reset off first, because lipgloss.Width measures display
// columns while the cut counts runes and an escape sequence's bytes are runes.
// Reverse video or faint then bleeds into every following line of the frame,
// and a cut narrow enough to land inside the leading CSI takes the newline with
// it. The trigger is any terminal narrower than the widest rendered row - a
// 40-column split pane against an 80-character URL truncates every row - so it
// is the ordinary case, not an edge one. Styling last makes the escapes
// structurally uncuttable: clip never sees one.
func renderRow(it Item, selected bool, width int) string {
	label, gap, detail := it.Label, "  ", it.Detail
	if selected {
		label, gap = " "+label+" ", " "
	}

	// A width of zero means nobody has said how wide the terminal is, so
	// clipping would be a guess. Bubbletea sends a real size before the first
	// paint on a real terminal.
	if width > 0 {
		label = clip(label, width)
		if room := width - lipgloss.Width(label) - len(gap); room <= 0 {
			gap, detail = "", ""
		} else {
			detail = clip(detail, room)
		}
	}

	if selected {
		label = styleSelected(label)
	}
	if detail == "" {
		return label
	}
	return label + gap + styleDetail(detail)
}

// clip cuts plain text down to width display columns. It must only ever be
// handed text with no escape sequences in it - see renderRow - and it counts
// columns rather than runes, so a wide rune cannot overrun the budget either.
//
// That precondition now has an owner rather than being a convention: control
// characters are stripped from every title, note and tag by internal/model,
// which is where all of them enter the vault, and cmd/bkmr applies the same
// model.CleanTitle to a browser tab's title before this package ever sees it.
// Do not add a second pass here - one owner, at the boundary where data enters.
func clip(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := lipgloss.Width(string(r))
		if used+w > width {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String()
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
