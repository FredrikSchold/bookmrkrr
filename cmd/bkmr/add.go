package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/atotto/clipboard"

	"github.com/FredrikSchold/bookmrkrr/internal/model"
)

// readClipboard is a seam so tests do not touch the real clipboard.
//
// clipboard.ReadAll shells out to a platform tool - xclip, pbpaste, PowerShell -
// so a stray call in a test is slow, visible to whoever is at the keyboard, and
// dependent on what they happened to copy last. It is called from exactly one
// place, below, and every test that reaches that place replaces this variable.
var readClipboard = clipboard.ReadAll

// tagList collects a repeatable tag flag. Normalization is model's job, so
// nothing here lowercases, sorts or deduplicates.
type tagList []string

func (t *tagList) String() string     { return strings.Join(*t, ",") }
func (t *tagList) Set(v string) error { *t = append(*t, v); return nil }

func init() {
	register(command{
		Name:    "add",
		Summary: "save a bookmark; with no URL, read the clipboard",
		Usage:   "bkmr add [url] [-t tag]... [--title text] [--note text] [--no-fetch]",
		Run:     runAdd,
	})
}

func runAdd(args []string) error {
	var tags tagList
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	// Discarded so that flag does not write its own complaint straight to the
	// process's standard error, bypassing errOut. What went wrong is reported
	// below instead.
	fs.SetOutput(io.Discard)
	fs.Var(&tags, "t", "tag (repeatable)")
	fs.Var(&tags, "tag", "tag (repeatable)")
	title := fs.String("title", "", "title")
	note := fs.String("note", "", "note")
	noFetch := fs.Bool("no-fetch", false, "do not fetch the page title")
	if err := fs.Parse(args); err != nil {
		// Per the errUsage contract: say what was wrong here, then return the
		// sentinel bare so dispatch prints the usage line and exits 2.
		fmt.Fprintf(errOut, "bkmr: %v\n", err)
		return errUsage
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(errOut, "bkmr: add takes at most one URL")
		return errUsage
	}

	raw := ""
	fromClipboard := false
	if fs.NArg() == 1 {
		raw = fs.Arg(0)
	} else {
		var err error
		raw, err = readClipboard()
		if err != nil {
			return fmt.Errorf("read clipboard: %w", err)
		}
		fromClipboard = true
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("nothing to add: %s", source(fromClipboard))
	}
	if _, err := model.NormalizeURL(raw); err != nil {
		return fmt.Errorf("%s is not a URL: %q", source(fromClipboard), raw)
	}

	v, err := openVault()
	if err != nil {
		return err
	}

	b := model.Bookmark{URL: raw, Title: *title, Notes: *note, Tags: tags}
	if !*noFetch {
		b.Title = resolveTitle(b.Title, raw)
	}

	// Mutate, not Load-then-Save: it reloads under the write lock, so a
	// bookmark another bkmr added in between cannot be discarded here.
	var stored model.Bookmark
	var merged bool
	if err := v.Mutate(func(c *model.Collection) error {
		var err error
		stored, merged, err = c.Add(b)
		return err
	}); err != nil {
		// The two errors a write can fail with - a lock another bkmr holds, and
		// a sharing violation on the publishing rename - are unreadable as
		// store words them, so they go through the CLI's explainer.
		return explainVaultError(err)
	}

	if merged {
		fmt.Fprintf(out, "already saved as %s; tags are now %s\n", stored.ID, tagsOrNone(stored.Tags))
		return nil
	}
	fmt.Fprintf(out, "saved %s  %s\n", stored.ID, label(stored))
	return nil
}

func source(fromClipboard bool) string {
	if fromClipboard {
		return "the clipboard"
	}
	return "that argument"
}

func tagsOrNone(tags []string) string {
	if len(tags) == 0 {
		return "(none)"
	}
	return strings.Join(tags, ", ")
}

func label(b model.Bookmark) string {
	if b.Title != "" {
		return b.Title
	}
	return b.URL
}

// resolveTitle is replaced with a real implementation in Task 9. Until then a
// supplied title is kept and nothing is fetched.
func resolveTitle(given, _ string) string { return given }
