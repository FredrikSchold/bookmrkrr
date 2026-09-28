package main

import (
	"fmt"
	"strings"

	"github.com/sahilm/fuzzy"
)

func init() {
	register(command{
		Name:    "open",
		Summary: "open the best match for a query, or a bookmark id",
		Usage:   "bkmr open <id|query>",
		Run:     runOpen,
	})
}

func runOpen(args []string) error {
	if len(args) == 0 {
		// Nothing to explain beyond the usage line dispatch is about to print,
		// so the sentinel goes back bare and errOut stays quiet.
		return errUsage
	}
	query := strings.Join(args, " ")

	v, err := openVault()
	if err != nil {
		return err
	}
	c, err := v.Load()
	if err != nil {
		return explainVaultError(err)
	}

	// An exact ID wins outright: it is what 'bkmr ls' prints, so anyone who
	// pasted one means that bookmark and not whatever fuzzy-matches best.
	if b, ok := c.Find(query); ok {
		return openByID(v, b.ID)
	}

	items := itemsFor(c.Bookmarks)
	hay := make([]string, len(items))
	for i, it := range items {
		hay[i] = it.Filter
	}
	// query as typed: sahilm/fuzzy compares with equalFold, so it is already
	// case-insensitive and lowercasing one side only invites someone to
	// "correct" the asymmetry on the wrong side later.
	matches := fuzzy.Find(query, hay)
	if len(matches) == 0 {
		return fmt.Errorf("no bookmark matches %q", query)
	}
	return openByID(v, items[matches[0].Index].ID)
}
