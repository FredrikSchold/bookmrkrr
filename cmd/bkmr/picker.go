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
	// Not a terminal: behave like ls so the command stays pipe-safe. An
	// alt-screen program writing into a pipe produces escape sequences nobody
	// asked for, and 'bkmr | grep rust' is a reasonable thing to type.
	//
	// Checked before the vault is touched. In the other order 'bkmr | grep'
	// decrypts the vault, discovers stdout is not a terminal, and hands off to
	// runLs, which decrypts it a second time. With no key cached that is two
	// password prompts for one command, not one: the prompt reads stdin, which
	// is still a terminal in a pipeline, so nothing refuses the second one.
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return runLs(nil)
	}

	v, err := openVault()
	if err != nil {
		return err
	}
	c, err := v.Load()
	if err != nil {
		return explainVaultError(err)
	}

	item, action, err := tui.Run(tui.New(itemsFor(c.Bookmarks), "search"))
	if err != nil {
		return err
	}
	return applyPickerAction(v, item, action)
}

// applyPickerAction carries out what the picker's user asked for. It is split
// out from runPicker because runPicker needs a terminal and this does not: the
// writes are all here, so this is the half a test can drive.
func applyPickerAction(v *store.Vault, item tui.Item, action tui.Action) error {
	switch action {
	case tui.ActionOpen:
		return openByID(v, item.ID)

	case tui.ActionCopy:
		// item.Detail is the URL the picker had on screen, which is the URL the
		// user was looking at when they pressed the key. Looking it up again in
		// the collection loaded before the picker opened would be no fresher
		// and could be staler - a concurrent 'bkmr edit' would put the old URL
		// on the clipboard - and the lookup could not fail anyway, since the
		// item came from that same snapshot.
		if err := clipboard.WriteAll(item.Detail); err != nil {
			return err
		}
		fmt.Fprintln(out, "copied", item.Detail)
		return nil

	case tui.ActionDelete:
		// Mutate, so an add made while the picker was open is not discarded,
		// and the existence check runs inside the mutation against the
		// collection as it is now rather than against what the picker showed.
		if err := v.Mutate(func(c *model.Collection) error {
			if !c.Delete(item.ID) {
				return fmt.Errorf("bookmark %s no longer exists", item.ID)
			}
			return nil
		}); err != nil {
			return explainVaultError(err)
		}
		// Reported only once Mutate has returned. Mutate writes the file after
		// its closure returns, so saying this from inside would announce
		// "deleted" on stdout and then fail on stderr with the bookmark still
		// in the vault - the same dishonesty 'bkmr lock' was fixed for.
		fmt.Fprintln(out, "deleted", item.ID)
		return nil
	}
	// ActionNone: the user quit without choosing, which is not an error and
	// deserves no output.
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
	// Called after Mutate has returned, so no browser starts while the write
	// lock is held. A launch that then fails leaves the visit recorded, which
	// is the lesser of the two wrongs available: recording afterwards instead
	// would lose the visit on every failed write, and a ranking short one open
	// is a smaller lie than an open that never counted at all.
	return tui.Open(url)
}
