package model

import (
	"testing"
	"time"
)

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"lowercases scheme and host", "HTTPS://Example.COM/Path", "https://example.com/Path"},
		{"drops a bare trailing slash", "https://example.com/", "https://example.com"},
		{"keeps a meaningful trailing slash", "https://example.com/docs/", "https://example.com/docs/"},
		{"strips utm parameters", "https://example.com/a?utm_source=x&id=7", "https://example.com/a?id=7"},
		{"strips fbclid and gclid", "https://example.com/a?fbclid=1&gclid=2", "https://example.com/a"},
		{"keeps the fragment", "https://example.com/app#/route", "https://example.com/app#/route"},
		// Review Focus 2: the most common clipboard paste.
		{"prefixes https on a bare host", "example.com", "https://example.com"},
		{"prefixes https on a bare host with a path", "example.com/docs", "https://example.com/docs"},
		{"trims surrounding whitespace", "  https://example.com  ", "https://example.com"},
		// A shell-dwelling developer pasting a local dev server.
		{"accepts a bare host with a port", "localhost:8080", "https://localhost:8080"},
		{"accepts an IP with a port", "127.0.0.1:3000", "https://127.0.0.1:3000"},
		{"accepts a dotted host with a port", "example.com:8443", "https://example.com:8443"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeURL(tt.in)
			if err != nil {
				t.Fatalf("NormalizeURL(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("NormalizeURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeURLRejectsNonURLs(t *testing.T) {
	for _, in := range []string{
		"", "   ", "hello world", "ftp://example.com", "file:///etc/passwd", "not a url at all",
		// A scheme-like string must not be mangled into an https host.
		"mailto:a@b.com", "localhost:", "localhost:abc",
	} {
		if got, err := NormalizeURL(in); err == nil {
			t.Errorf("NormalizeURL(%q) = %q, want an error", in, got)
		}
	}
}

func TestNormalizeTags(t *testing.T) {
	got := NormalizeTags([]string{" Rust ", "rust", "Go Lang", "", "  ", "Web/Dev"})
	want := []string{"go-lang", "rust", "web/dev"}
	if len(got) != len(want) {
		t.Fatalf("NormalizeTags() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("NormalizeTags() = %v, want %v", got, want)
		}
	}
}

func TestNewIDIsEightLowercaseChars(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := NewID()
		if len(id) != 8 {
			t.Fatalf("NewID() = %q, want 8 characters", id)
		}
		if id != lower(id) {
			t.Fatalf("NewID() = %q, want lowercase", id)
		}
		if seen[id] {
			t.Fatalf("NewID() returned a duplicate: %q", id)
		}
		seen[id] = true
	}
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

func TestAddAssignsIDAndTimestamp(t *testing.T) {
	c := &Collection{Version: Version}

	got, merged, err := c.Add(Bookmark{URL: "https://example.com", Tags: []string{"Rust"}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if merged {
		t.Error("Add() merged = true, want false for a new bookmark")
	}
	if len(got.ID) != 8 {
		t.Errorf("Add() ID = %q, want 8 characters", got.ID)
	}
	if got.Added.IsZero() {
		t.Error("Add() left Added zero")
	}
	if len(got.Tags) != 1 || got.Tags[0] != "rust" {
		t.Errorf("Add() Tags = %v, want [rust]", got.Tags)
	}
	if len(c.Bookmarks) != 1 {
		t.Errorf("len(Bookmarks) = %d, want 1", len(c.Bookmarks))
	}
}

func TestAddMergesTagsIntoAnExistingURL(t *testing.T) {
	c := &Collection{Version: Version}
	first, _, _ := c.Add(Bookmark{URL: "https://example.com/a", Tags: []string{"rust"}, Title: "A"})

	got, merged, err := c.Add(Bookmark{URL: "HTTPS://Example.com/a?utm_source=x", Tags: []string{"async"}, Notes: "later"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if !merged {
		t.Fatal("Add() merged = false, want true for a duplicate URL")
	}
	if got.ID != first.ID {
		t.Errorf("Add() ID = %q, want the existing %q", got.ID, first.ID)
	}
	if len(c.Bookmarks) != 1 {
		t.Fatalf("len(Bookmarks) = %d, want 1", len(c.Bookmarks))
	}
	if len(got.Tags) != 2 || got.Tags[0] != "async" || got.Tags[1] != "rust" {
		t.Errorf("Add() Tags = %v, want [async rust]", got.Tags)
	}
	if got.Notes != "later" {
		t.Errorf("Add() Notes = %q, want %q", got.Notes, "later")
	}
	if got.Title != "A" {
		t.Errorf("Add() Title = %q, want the existing %q", got.Title, "A")
	}
}

func TestFindAndDelete(t *testing.T) {
	c := &Collection{Version: Version}
	b, _, _ := c.Add(Bookmark{URL: "https://example.com"})

	if _, ok := c.Find(b.ID); !ok {
		t.Fatal("Find() ok = false, want true")
	}
	if _, ok := c.Find("nosuchid"); ok {
		t.Error("Find(unknown) ok = true, want false")
	}
	if !c.Delete(b.ID) {
		t.Error("Delete() = false, want true")
	}
	if len(c.Bookmarks) != 0 {
		t.Errorf("len(Bookmarks) = %d, want 0", len(c.Bookmarks))
	}
	if c.Delete(b.ID) {
		t.Error("Delete() on an already-deleted id = true, want false")
	}
}

func TestTagCounts(t *testing.T) {
	c := &Collection{Version: Version}
	c.Add(Bookmark{URL: "https://a.example", Tags: []string{"rust", "web"}})
	c.Add(Bookmark{URL: "https://b.example", Tags: []string{"rust"}})

	got := c.TagCounts()
	if got["rust"] != 2 {
		t.Errorf("TagCounts()[rust] = %d, want 2", got["rust"])
	}
	if got["web"] != 1 {
		t.Errorf("TagCounts()[web] = %d, want 1", got["web"])
	}
}

func TestAddPreservesExplicitTimestamps(t *testing.T) {
	c := &Collection{Version: Version}
	when := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

	got, _, err := c.Add(Bookmark{URL: "https://example.com", Added: when})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Added.Equal(when) {
		t.Errorf("Add() Added = %v, want %v", got.Added, when)
	}
}

// Titles and notes reach the vault from places that are not the user: a page's
// own <title>, a browser tab, and - from Task 12 - an imported bookmarks file.
// All three are printed straight to a terminal afterwards by ls, by the picker
// and by the saved line, so an ESC in one of them is a terminal-injection
// vector: it can move the cursor, clear the screen, recolour everything that
// follows, or on some terminals set the window title. Add is where every title
// enters, so it is where they are stripped.
func TestAddStripsControlCharactersFromATitle(t *testing.T) {
	c := &Collection{Version: Version}

	got, _, err := c.Add(Bookmark{
		URL:   "https://example.com",
		Title: "\x1b[31mRed\x1b[0m\x07 and \x1bc reset\x7f\u009b2J",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "[31mRed[0m and c reset2J"; got.Title != want {
		t.Errorf("Add() Title = %q, want %q", got.Title, want)
	}
	for _, r := range got.Title {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Errorf("Add() Title = %q, still contains control character U+%04X", got.Title, r)
		}
	}
}

// A title is one line. The whitespace controls become the space they were
// standing in for rather than vanishing, so two words are never jammed
// together, and runs collapse.
func TestAddCollapsesWhitespaceInATitle(t *testing.T) {
	c := &Collection{Version: Version}

	got, _, err := c.Add(Bookmark{URL: "https://example.com", Title: "Two\n\tLines  Here\r\n"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "Two Lines Here"; got.Title != want {
		t.Errorf("Add() Title = %q, want %q", got.Title, want)
	}
}

// Notes are the one field that is legitimately multi-line - Add itself joins a
// merged note onto an existing one with a newline - so line breaks and tabs
// survive and everything else in the control ranges does not. A CRLF becomes a
// plain LF rather than leaving a stray carriage return behind.
func TestAddStripsControlCharactersFromNotesButKeepsLineBreaks(t *testing.T) {
	c := &Collection{Version: Version}

	got, _, err := c.Add(Bookmark{
		URL:   "https://example.com",
		Notes: "first\x1b[31m line\r\n\tsecond\x00 line\u009b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "first[31m line\n\tsecond line"; got.Notes != want {
		t.Errorf("Add() Notes = %q, want %q", got.Notes, want)
	}
}

// The merge branch writes a title and appends a note of its own, so it needs the
// same treatment. A fix applied to only the new-bookmark branch would leave
// re-adding a URL as an open door.
func TestAddStripsControlCharactersWhenMerging(t *testing.T) {
	c := &Collection{Version: Version}
	if _, _, err := c.Add(Bookmark{URL: "https://example.com"}); err != nil {
		t.Fatal(err)
	}

	got, merged, err := c.Add(Bookmark{
		URL:   "https://example.com",
		Title: "\x1bcWiped",
		Notes: "\x1b[2Jcleared",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !merged {
		t.Fatal("Add() merged = false, want true")
	}
	if want := "cWiped"; got.Title != want {
		t.Errorf("merged Title = %q, want %q", got.Title, want)
	}
	if want := "[2Jcleared"; got.Notes != want {
		t.Errorf("merged Notes = %q, want %q", got.Notes, want)
	}
}

// NormalizeTags joins on strings.Fields, which splits on unicode.IsSpace - and
// ESC, NUL, BEL, DEL and CSI are not space. So the tag path did NOT already
// strip these, whatever it looks like it does, and a tag is printed by ls, by
// the picker and by tag mode's own rows.
func TestNormalizeTagsStripsControlCharacters(t *testing.T) {
	got := NormalizeTags([]string{"\x1b[31mred\x1b[0m", "two\x00words"})

	for _, tag := range got {
		for _, r := range tag {
			if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
				t.Errorf("NormalizeTags() = %q, tag %q still contains U+%04X", got, tag, r)
			}
		}
	}
	if len(got) != 2 {
		t.Fatalf("NormalizeTags() = %q, want 2 tags", got)
	}
	if got[0] != "[31mred[0m" {
		t.Errorf("NormalizeTags()[0] = %q, want %q", got[0], "[31mred[0m")
	}
	if got[1] != "twowords" {
		t.Errorf("NormalizeTags()[1] = %q, want %q", got[1], "twowords")
	}
}

// Control characters are the line, and nothing else moves. Right-to-left marks
// and zero-width characters can make a title display confusingly, but they are
// legitimate text in real page titles - stripping them would corrupt titles for
// a large fraction of the web, including every Arabic and Hebrew page.
func TestAddLeavesOrdinaryUnicodeAlone(t *testing.T) {
	c := &Collection{Version: Version}
	title := "\u200fشبكة\u200e \u200bزero-width\u00a0nbsp — Go 日本語 🦀"

	got, _, err := c.Add(Bookmark{URL: "https://example.com", Title: title})
	if err != nil {
		t.Fatal(err)
	}
	// The nbsp is whitespace to strings.Fields, so it collapses like any other
	// space; every other rune survives untouched.
	if want := "\u200fشبكة\u200e \u200bزero-width nbsp — Go 日本語 🦀"; got.Title != want {
		t.Errorf("Add() Title = %q, want %q", got.Title, want)
	}
}

// CleanTitle is exported because the tab picker shows a browser-supplied title
// before anything is saved, and Find hands out a writable *Bookmark. Both need
// the same rule, and there must be exactly one of it.
func TestCleanTitleIsIdempotent(t *testing.T) {
	once := CleanTitle("\x1b[31m  Red \n Thing \x07")
	if twice := CleanTitle(once); twice != once {
		t.Errorf("CleanTitle(CleanTitle(x)) = %q, want %q", twice, once)
	}
	if once != "[31m Red Thing" {
		t.Errorf("CleanTitle() = %q, want %q", once, "[31m Red Thing")
	}
}

// The C1 range has two spellings and only one of them is a control character.
// A valid UTF-8 U+009B is CSI and survives a JSON round trip untouched, so it
// really would reach the vault and the terminal: that is what isControl is for.
// A lone 0x9B byte is not valid UTF-8; it decodes as U+FFFD, no terminal in
// UTF-8 mode acts on it, and encoding/json replaces it on the way into the vault
// regardless. It is ordinary text as far as this package is concerned, and is
// left alone rather than being guessed at.
func TestAnInvalidByteIsLeftAloneRatherThanTreatedAsC1(t *testing.T) {
	c := &Collection{Version: Version}

	got, _, err := c.Add(Bookmark{URL: "https://example.com", Title: "a" + string([]byte{0x9b}) + "b"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "a\ufffdb"; got.Title != want {
		t.Errorf("Add() Title = %q, want %q", got.Title, want)
	}
}
