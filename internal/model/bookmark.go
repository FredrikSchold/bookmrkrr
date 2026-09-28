// Package model holds the bookmark type and the rules for normalizing and
// deduplicating bookmarks. It performs no I/O.
package model

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Version is the plaintext document version stored inside the vault.
const Version = 1

// Bookmark is one saved link.
type Bookmark struct {
	ID      string     `json:"id"`
	URL     string     `json:"url"`
	Title   string     `json:"title,omitempty"`
	Tags    []string   `json:"tags,omitempty"`
	Notes   string     `json:"notes,omitempty"`
	Added   time.Time  `json:"added"`
	Visited *time.Time `json:"visited,omitempty"`
	Opens   int        `json:"opens,omitempty"`
}

// Collection is the whole vault plaintext.
type Collection struct {
	Version   int        `json:"version"`
	Bookmarks []Bookmark `json:"bookmarks"`
}

var idEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewID returns a random 8-character lowercase identifier.
func NewID() string {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		panic("bkmr: system randomness unavailable: " + err.Error())
	}
	return strings.ToLower(idEncoding.EncodeToString(b))
}

var trackingParams = map[string]bool{"fbclid": true, "gclid": true, "mc_eid": true}

// NormalizeURL returns a canonical form used only for duplicate detection.
// The original URL is always what gets stored. A bare host gains an https
// scheme, because that is the overwhelmingly common clipboard paste.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty URL")
	}
	// A control character in a URL is refused, not cleaned. Every other string
	// this package stores gets the characters stripped out of it, because a title
	// with one fewer byte is still the same title - but a URL with one fewer byte
	// points somewhere else, and where it points is the only thing a bookmark is
	// for. Refusing also keeps such an entry out of the vault entirely, which is
	// what an import wants: it counts what it skipped and says so.
	//
	// net/url already refuses C0 and DEL, so on its own this closes the C1 range
	// - and C1 is the range that matters here. url.Parse accepts U+009B and
	// u.String() percent-encodes it, so the *normalized* URL looks harmless; but
	// what gets stored is the raw URL the user or the import file gave, and the
	// raw URL is what ls prints and what label() prints for every titleless
	// bookmark. U+009B is CSI, a single-byte "ESC[".
	//
	// %q, so the refusal cannot print the escape it is refusing: it renders a
	// control character as an escape sequence spelled out in ASCII rather than
	// sending it to the terminal.
	if i := strings.IndexFunc(raw, isControl); i >= 0 {
		return "", fmt.Errorf("URL contains a control character at byte %d: %q", i, raw)
	}
	// A space only. Tab and newline were the other two this used to test for, and
	// they are control characters, so the check above has already refused them.
	if strings.Contains(raw, " ") {
		return "", fmt.Errorf("not a URL: %q", raw)
	}
	if !strings.Contains(raw, "://") {
		// A schemeless input is accepted when its host part carries either a
		// dot or a numeric port, so that a developer can paste "localhost:3000"
		// straight from the shell. Requiring the text after the colon to be all
		// digits is what stops scheme-like strings such as "mailto:a@b.com"
		// from being mangled into an https host ("mailto:a" would otherwise
		// parse as userinfo on host "b.com").
		host := raw
		if i := strings.Index(host, "/"); i >= 0 {
			host = host[:i]
		}
		hasPort := false
		if i := strings.LastIndex(host, ":"); i >= 0 {
			if !allDigits(host[i+1:]) {
				return "", fmt.Errorf("not a URL: %q", raw)
			}
			host, hasPort = host[:i], true
		}
		if !hasPort && !strings.Contains(host, ".") {
			return "", fmt.Errorf("not a URL: %q", raw)
		}
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("not a URL: %q", raw)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("only http and https are supported, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("not a URL: %q", raw)
	}
	u.Host = strings.ToLower(u.Host)

	q := u.Query()
	for k := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "utm_") || trackingParams[lk] {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	if u.Path == "/" {
		u.Path = ""
	}
	return u.String(), nil
}

// allDigits reports whether s is non-empty and made up only of ASCII digits.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// NormalizeTags strips control characters, lowercases, hyphenates internal
// whitespace, removes blanks and duplicates, and sorts the result.
//
// The dropControls call is not redundant with the Fields join below, which is
// what it looks like. strings.Fields splits on unicode.IsSpace, and ESC, NUL,
// BEL, DEL and CSI are not space: before this, a tag really could carry an
// escape sequence into ls, into the picker, and into tag mode's own rows.
func NormalizeTags(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.Join(strings.Fields(dropControls(strings.ToLower(t), false)), "-")
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Add stores a bookmark, or merges it into an existing entry with the same
// normalized URL. The second return value reports whether a merge happened.
func (c *Collection) Add(b Bookmark) (Bookmark, bool, error) {
	return c.add(b, c.index())
}

// AddAll stores many bookmarks, merging each into an existing entry - or into an
// earlier entry of the same batch - with the same normalized URL. It reports how
// many were added, how many merged, and how many were skipped because their URL
// was not one this tool stores.
//
// It exists because Add is O(n) in what the vault already holds: it normalizes
// every stored URL to find a duplicate, which is nothing for one interactive add
// and quadratic for a bulk import. A browser export of tens of thousands of
// bookmarks put through Add in a loop takes minutes. The index is built once
// here and carried through the whole batch instead.
//
// A URL that will not normalize is skipped rather than fatal: one bad line in
// somebody's twenty-thousand-line export must not throw the other nineteen
// thousand away.
func (c *Collection) AddAll(in []Bookmark) (added, merged, skipped int) {
	idx := c.index()
	for _, b := range in {
		_, wasMerged, err := c.add(b, idx)
		switch {
		case err != nil:
			skipped++
		case wasMerged:
			merged++
		default:
			added++
		}
	}
	return added, merged, skipped
}

// index maps each stored bookmark's normalized URL to its position. A URL that
// will not normalize has no duplicates to find and is left out. The first entry
// wins where two stored bookmarks somehow normalize alike, which is what the
// linear scan this replaced did.
func (c *Collection) index() map[string]int {
	idx := make(map[string]int, len(c.Bookmarks))
	for i := range c.Bookmarks {
		key, err := NormalizeURL(c.Bookmarks[i].URL)
		if err != nil {
			continue
		}
		if _, dup := idx[key]; !dup {
			idx[key] = i
		}
	}
	return idx
}

// add is the shared body of Add and AddAll. idx must describe c.Bookmarks as it
// is now; add keeps it that way, so a batch can reuse one map.
//
// Every title and note that enters the vault through an insert passes through
// here, so this is where they are cleaned - once, before either branch below, so
// the merge path cannot be the door the new-bookmark path is not. An insert is
// not the only door, though: Find hands out a writable *Bookmark. Clean is what
// covers the rest. See isControl.
func (c *Collection) add(b Bookmark, idx map[string]int) (Bookmark, bool, error) {
	key, err := NormalizeURL(b.URL)
	if err != nil {
		return Bookmark{}, false, err
	}
	b.Tags = NormalizeTags(b.Tags)
	b.Title = CleanTitle(b.Title)
	b.Notes = cleanNotes(b.Notes)

	if i, ok := idx[key]; ok {
		c.Bookmarks[i].Tags = NormalizeTags(append(c.Bookmarks[i].Tags, b.Tags...))
		if c.Bookmarks[i].Title == "" {
			c.Bookmarks[i].Title = b.Title
		}
		switch {
		case b.Notes == "":
		case c.Bookmarks[i].Notes == "":
			c.Bookmarks[i].Notes = b.Notes
		default:
			c.Bookmarks[i].Notes += "\n" + b.Notes
		}
		return c.Bookmarks[i], true, nil
	}

	if b.ID == "" {
		b.ID = NewID()
	}
	if b.Added.IsZero() {
		b.Added = time.Now().UTC()
	}
	idx[key] = len(c.Bookmarks)
	c.Bookmarks = append(c.Bookmarks, b)
	return b, false, nil
}

// Clean applies this package's storage rules to every bookmark it holds, so that
// a collection about to be persisted carries no control characters and no
// malformed tags however it was assembled. It is idempotent.
//
// add cleans one bookmark on the way in, which was the whole rule while an
// insert was the only way text reached the vault. It is not: Find returns a
// writable *Bookmark, and 'bkmr edit' sets a title through it without passing
// through add at all. CleanTitle is exported so an editing path can reach the
// rule, but that makes the rule a convention - it holds for as long as every
// future writer remembers it, which is another way of saying it will lapse.
//
// So internal/store calls this on the write path, the one place every writer has
// to pass through. A command that prints what it wrote before the write happens
// still has to clean that text itself, because this guards what is stored rather
// than what is printed.
func (c *Collection) Clean() {
	for i := range c.Bookmarks {
		c.Bookmarks[i].Title = CleanTitle(c.Bookmarks[i].Title)
		c.Bookmarks[i].Notes = cleanNotes(c.Bookmarks[i].Notes)
		c.Bookmarks[i].Tags = NormalizeTags(c.Bookmarks[i].Tags)
	}
}

// Find returns a pointer to the bookmark with the given id.
func (c *Collection) Find(id string) (*Bookmark, bool) {
	for i := range c.Bookmarks {
		if c.Bookmarks[i].ID == id {
			return &c.Bookmarks[i], true
		}
	}
	return nil, false
}

// Delete removes a bookmark by id, reporting whether it existed.
func (c *Collection) Delete(id string) bool {
	for i := range c.Bookmarks {
		if c.Bookmarks[i].ID == id {
			c.Bookmarks = append(c.Bookmarks[:i], c.Bookmarks[i+1:]...)
			return true
		}
	}
	return false
}

// TagCounts returns how many bookmarks carry each tag.
func (c *Collection) TagCounts() map[string]int {
	counts := map[string]int{}
	for _, b := range c.Bookmarks {
		for _, t := range b.Tags {
			counts[t]++
		}
	}
	return counts
}

// Control characters are stripped from every string this package stores, and
// this is the only place that rule lives.
//
// Titles, notes and tags all end up printed straight to a terminal - by ls, by
// the picker, by tag mode's rows, by the line each save reports - and none of
// them is necessarily the user's own text. A page controls its own
// document.title, so it controls what a fetched title and a captured browser tab
// contain; Task 12's importer reads titles out of a file somebody else wrote. An
// ESC in any of those is a terminal-injection vector: it can clear the screen,
// move the cursor, recolour everything printed afterwards, or on some terminals
// set the window title or push text back into the input buffer.
//
// The rule is the control ranges and nothing else. C0 (U+0000-U+001F) includes
// ESC; DEL (U+007F) is honored by some terminals; and C1 (U+0080-U+009F) is
// honored directly by others, which is why U+009B - CSI, a single-byte "ESC[" -
// is stripped too.
//
// Ordinary Unicode is left entirely alone, deliberately. Right-to-left marks and
// zero-width characters can make a title display confusingly, and they are also
// legitimate text in real page titles: stripping them would corrupt the title of
// every Arabic and Hebrew page on the web. Control characters are the line.
//
// This is also the owner of an invariant internal/tui only documented:
// tui.clip must be handed text with no escape sequences in it, which until now
// was enforced by convention.
func isControl(r rune) bool {
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

// dropControls removes every control character. The five that are whitespace -
// tab, newline, vertical tab, form feed and carriage return - are the exception:
// they become the space they were standing in for, so that removing one cannot
// jam two words together. That is the whole reason the exception exists, so it
// has to cover all five: leaving vertical tab and form feed to be deleted turned
// "two\vwords" into "twowords" and, in NormalizeTags, cost a hyphen that the
// same string used to get from strings.Fields.
//
// A caller that wants real line breaks kept - a note - passes keepLines. Only
// tab and newline are kept then: a CRLF becomes a plain LF rather than leaving a
// stray carriage return behind, and vertical tab and form feed are not line
// breaks worth preserving in a note.
func dropControls(s string, keepLines bool) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\t':
			if keepLines {
				return r
			}
			return ' '
		case '\v', '\f':
			return ' '
		case '\r':
			if keepLines {
				return -1
			}
			return ' '
		}
		if isControl(r) {
			return -1
		}
		return r
	}, s)
}

// CleanTitle makes one line of text safe to store and to print: control
// characters go, and the whitespace that is left collapses to single spaces.
//
// It is exported for the two places that need the rule before storage can apply
// it. The tab picker shows a browser-supplied title on screen before anything is
// saved, so cleaning at storage time would be too late for the frame the picker
// draws; and 'bkmr edit' echoes the new title back on the line that reports the
// change, which is also before the write. What gets stored is Clean's job, not
// this function's. It is idempotent, so applying it early costs nothing.
func CleanTitle(s string) string {
	return strings.Join(strings.Fields(dropControls(s, false)), " ")
}

// cleanNotes is CleanTitle's multi-line counterpart. Notes are the one field
// that is legitimately more than one line - Add itself joins a merged note onto
// an existing one with a newline - so line breaks and tabs survive and the
// whitespace is not collapsed. It is unexported because nothing outside this
// package writes a note without going through Add.
func cleanNotes(s string) string { return dropControls(s, true) }
