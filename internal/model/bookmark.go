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
	if strings.ContainsAny(raw, " \t\n") {
		return "", fmt.Errorf("not a URL: %q", raw)
	}
	if !strings.Contains(raw, "://") {
		if !strings.Contains(raw, ".") {
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

// NormalizeTags lowercases, hyphenates internal whitespace, removes blanks
// and duplicates, and sorts the result.
func NormalizeTags(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.Join(strings.Fields(strings.ToLower(t)), "-")
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
	key, err := NormalizeURL(b.URL)
	if err != nil {
		return Bookmark{}, false, err
	}
	b.Tags = NormalizeTags(b.Tags)

	for i := range c.Bookmarks {
		existing, err := NormalizeURL(c.Bookmarks[i].URL)
		if err != nil || existing != key {
			continue
		}
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
	c.Bookmarks = append(c.Bookmarks, b)
	return b, false, nil
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
