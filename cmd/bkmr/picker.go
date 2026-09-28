package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"golang.org/x/term"

	"github.com/FredrikSchold/bookmrkrr/internal/model"
	"github.com/FredrikSchold/bookmrkrr/internal/store"
	"github.com/FredrikSchold/bookmrkrr/internal/tui"
)

func init() {
	register(command{
		Name:    "picker",
		Summary: "open the interactive picker (this is what bare 'bkmr' runs)",
		Usage:   "bkmr",
		Run:     runPicker,
	})
}

func runPicker([]string) error {
	v, err := openVault()
	if err != nil {
		return err
	}
	c, err := v.Load()
	if err != nil {
		return explainVaultError(err)
	}

	// Not a terminal: behave like ls so the command stays pipe-safe. An
	// alt-screen program writing into a pipe produces escape sequences nobody
	// asked for, and 'bkmr | grep rust' is a reasonable thing to type.
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return runLs(nil)
	}

	item, action, err := tui.Run(tui.New(itemsFor(c.Bookmarks), "search"))
	if err != nil {
		return err
	}
	switch action {
	case tui.ActionNone:
		return nil
	case tui.ActionOpen:
		return openByID(v, item.ID)
	case tui.ActionCopy:
		b, ok := c.Find(item.ID)
		if !ok {
			return fmt.Errorf("bookmark %s vanished", item.ID)
		}
		if err := clipboard.WriteAll(b.URL); err != nil {
			return err
		}
		fmt.Fprintln(out, "copied", b.URL)
		return nil
	case tui.ActionDelete:
		// Mutate, so an add made while the picker was open is not discarded,
		// and the existence check happens inside the mutation against the
		// collection as it is now rather than against what the picker showed.
		return explainVaultError(v.Mutate(func(c *model.Collection) error {
			if !c.Delete(item.ID) {
				return fmt.Errorf("bookmark %s no longer exists", item.ID)
			}
			fmt.Fprintln(out, "deleted", item.ID)
			return nil
		}))
	}
	return nil
}

// itemsFor ranks bookmarks by recency of use, then by when they were added,
// so the picker's unfiltered order is already useful.
func itemsFor(all []model.Bookmark) []tui.Item {
	rows := append([]model.Bookmark(nil), all...)
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Opens != rows[j].Opens {
			return rows[i].Opens > rows[j].Opens
		}
		return rows[i].Added.After(rows[j].Added)
	})

	items := make([]tui.Item, 0, len(rows))
	for _, b := range rows {
		items = append(items, tui.Item{
			ID:     b.ID,
			Label:  label(b),
			Detail: b.URL,
			Filter: strings.ToLower(strings.Join([]string{b.Title, b.URL, strings.Join(b.Tags, " ")}, " ")),
			Tags:   b.Tags,
		})
	}
	return items
}

// openByID opens a bookmark and records the visit. The record is written
// through Mutate, so a concurrent add is never clobbered.
//
// The *Bookmark that Find returns points into the collection's own slice,
// which is exactly what the in-place Visited and Opens bump needs. It is not
// held past the end of this function: a later Add could reallocate the slice
// and leave the pointer addressing a copy nobody saves.
func openByID(v *store.Vault, id string) error {
	var url string
	if err := v.Mutate(func(c *model.Collection) error {
		b, ok := c.Find(id)
		if !ok {
			return fmt.Errorf("bookmark %s no longer exists", id)
		}
		url = b.URL
		now := time.Now().UTC()
		b.Visited = &now
		b.Opens++
		return nil
	}); err != nil {
		return explainVaultError(err)
	}
	return tui.Open(url)
}
