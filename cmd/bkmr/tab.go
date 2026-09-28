package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/FredrikSchold/bookmrkrr/internal/capture/browser"
	"github.com/FredrikSchold/bookmrkrr/internal/model"
	"github.com/FredrikSchold/bookmrkrr/internal/tui"
)

type tabChoice struct {
	Title string
	URL   string
}

// chooseTab is a seam so tests do not need a terminal.
//
// tui.Run starts a bubbletea program on the alt screen, so a test that reached
// the real one would take over the terminal of whoever ran the suite - the same
// reason tui.Open is a variable. Every test in this package replaces it.
var chooseTab = func(choices []tabChoice) (tabChoice, bool, error) {
	items := make([]tui.Item, len(choices))
	for i, c := range choices {
		items[i] = tui.Item{
			ID:     fmt.Sprint(i),
			Label:  c.Title,
			Detail: c.URL,
			Filter: strings.ToLower(c.Title + " " + c.URL),
		}
	}
	// Picking a tab means "choose this one", so copy, delete and tag browsing
	// are all switched off: tabs carry no tags for tag mode to list, and there
	// is nothing here to copy or delete. The picker's help line is rendered
	// from that same set, so it advertises only enter and esc. The empty
	// message is unreachable - runTab reports an empty tab list itself, below -
	// but the default names bookmarks and 'bkmr add', which would be nonsense
	// on a tab picker, so it is not left to chance.
	m := tui.New(items, "which tab?").
		WithEmptyMessage("no open tabs").
		WithFeatures(tui.FeatureOpen)
	item, action, err := tui.Run(m)
	if err != nil || action != tui.ActionOpen {
		return tabChoice{}, false, err
	}
	for i, c := range choices {
		if fmt.Sprint(i) == item.ID {
			return c, true, nil
		}
	}
	return tabChoice{}, false, nil
}

func init() {
	register(command{
		Name:    "tab",
		Summary: "pick one of your open browser tabs and save it",
		Usage:   "bkmr tab [-t tag]... [--note text]",
		Run:     runTab,
	})
}

func runTab(args []string) error {
	var tags tagList
	fs := flag.NewFlagSet("tab", flag.ContinueOnError)
	// Discarded so flag does not write its complaint straight to the process's
	// standard error, bypassing errOut. See runAdd.
	fs.SetOutput(io.Discard)
	fs.Var(&tags, "t", "tag (repeatable)")
	fs.Var(&tags, "tag", "tag (repeatable)")
	note := fs.String("note", "", "note")
	if err := fs.Parse(args); err != nil {
		// Per the errUsage contract: say what was wrong here, then return the
		// sentinel bare so dispatch prints the usage line and exits 2.
		fmt.Fprintf(errOut, "bkmr: %v\n", err)
		return errUsage
	}
	// Refused rather than ignored. Somebody who types 'bkmr tab https://...'
	// expecting 'bkmr add' would otherwise have the URL silently dropped and
	// whichever tab they then picked saved in its place.
	if fs.NArg() > 0 {
		fmt.Fprintln(errOut, "bkmr: tab takes no arguments - it saves a tab you pick from the list")
		return errUsage
	}

	v, err := openVault()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tabs, err := browser.Tabs(ctx)
	if err != nil {
		// browser.Tabs has already put the platform's own command in the error
		// text, so dispatch printing it is all the guidance the user needs.
		return err
	}
	if len(tabs) == 0 {
		fmt.Fprintln(out, "no open tabs to save")
		return nil
	}

	choices := make([]tabChoice, len(tabs))
	for i, t := range tabs {
		choices[i] = tabChoice{Title: t.Title, URL: t.URL}
	}
	picked, ok, err := chooseTab(choices)
	if err != nil {
		return err
	}
	if !ok {
		// The user quit the picker. Not an error, and nothing to report.
		return nil
	}

	// The title comes from the browser, so this path never touches the network:
	// there is nothing for internal/fetch to add that the browser has not
	// already told us.
	var stored model.Bookmark
	var merged bool
	if err := v.Mutate(func(c *model.Collection) error {
		var err error
		stored, merged, err = c.Add(model.Bookmark{URL: picked.URL, Title: picked.Title, Notes: *note, Tags: tags})
		return err
	}); err != nil {
		return explainVaultError(err)
	}

	if merged {
		fmt.Fprintf(out, "already saved as %s (%s); tags are now %s\n", stored.ID, stored.URL, tagsOrNone(stored.Tags))
		return nil
	}
	fmt.Fprintf(out, "saved %s  %s  %s\n", stored.ID, stored.Title, stored.URL)
	return nil
}
