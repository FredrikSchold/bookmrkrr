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
