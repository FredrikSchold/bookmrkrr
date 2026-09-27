package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/atotto/clipboard"

	"github.com/FredrikSchold/bookmrkrr/internal/config"
	"github.com/FredrikSchold/bookmrkrr/internal/fetch"
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
	norm, err := model.NormalizeURL(raw)
	if err != nil {
		return fmt.Errorf("%s is not a URL: %q", source(fromClipboard), raw)
	}

	v, err := openVault()
	if err != nil {
		return err
	}

	b := model.Bookmark{URL: raw, Title: *title, Notes: *note, Tags: tags}
	// Fetching happens here, before Mutate takes the write lock, so a slow or
	// hanging site cannot keep another bkmr out of the vault. The accepted cost:
	// re-adding a URL the vault already holds fetches a title that the merge
	// below then discards, because Add keeps the existing one. Checking first
	// would mean a Load outside the lock - a TOCTOU that buys one avoided
	// request - so the waste stays.
	//
	// norm, not raw. What gets stored is what the user typed, which was Task 8's
	// decision, but what gets requested is the normalized URL: it has the
	// tracking parameters stripped, so utm_source and fbclid are not handed to
	// the site we had already decided not to keep them for, and it has a scheme,
	// without which a supported input like "example.com" could never be fetched
	// at all.
	if !*noFetch {
		b.Title = resolveTitle(b.Title, norm)
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

// fetchTitle is a seam so no test in this package can reach the network by
// accident.
//
// cmd/bkmr must not import net/http - internal/fetch is the tool's whole
// network surface, and a CI check enforces that - so this is a function
// variable rather than an injected client. It is called from exactly one place,
// below; the test suite replaces it with a fetcher that cannot reach anything,
// and the few tests that want the real one point it at their own httptest
// server. Without this, the add tests written before fetching existed would
// start making live requests to example.com from CI.
var fetchTitle = fetch.Title

// fetchTimeout bounds one title fetch. Short on purpose: a title is a
// convenience, and nobody typing 'bkmr add' wants to wait on a dead host.
const fetchTimeout = 3 * time.Second

// resolveTitle returns the given title, or fetches one when the title is
// empty and the network is enabled. fetchURL must be the normalized URL - see
// the call site. A fetch failure is reported on errOut and otherwise ignored:
// losing a bookmark because a site was down is never acceptable.
func resolveTitle(given, fetchURL string) string {
	if given != "" {
		return given
	}
	cfg := config.Load()
	// Load never fails, so a broken config.toml would otherwise disable a kill
	// switch in complete silence. This is the one place the config is read, so
	// it is the one place that can say so.
	if cfg.Problem != "" {
		fmt.Fprintf(errOut, "bkmr: %s\n", cfg.Problem)
	}
	if !cfg.NetworkEnabled() {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	title, err := fetchTitle(ctx, fetchURL)
	if err != nil {
		fmt.Fprintf(errOut, "bkmr: could not read the page title (%v); saving without one\n", err)
		return ""
	}
	return title
}
