package main

import (
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/FredrikSchold/bookmrkrr/internal/model"
)

func init() {
	register(command{
		Name:    "ls",
		Summary: "print bookmarks to stdout, newest first",
		Usage:   "bkmr ls [--tag name]",
		Run:     runLs,
	})
}

// runLs writes bookmark rows and nothing else to out. Someone will pipe this
// into grep, so no progress, no warnings and no diagnostics may join them.
//
// The "no bookmarks yet" notice is the one non-row line allowed on out: it is
// program output, and it is fixed text that cannot collide with whatever the
// reader is searching for. The filtered notice below is not allowed there,
// because it echoes the user's own tag - 'bkmr ls --tag rust | grep rust' would
// match the notice and report a bookmark that does not exist.
func runLs(args []string) error {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	tag := fs.String("tag", "", "only bookmarks carrying this tag")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(errOut, "bkmr: %v\n", err)
		return errUsage
	}

	v, err := openVault()
	if err != nil {
		return err
	}
	c, err := v.Load()
	if err != nil {
		return explainVaultError(err)
	}

	rows := filterByTag(c.Bookmarks, *tag)
	if len(rows) == 0 {
		if *tag != "" {
			// A diagnostic, not output, and not an error either: asking for a
			// tag nobody has used is a fair question with an empty answer, so
			// the exit status stays 0 and the pipe stays empty.
			fmt.Fprintf(errOut, "bkmr: no bookmarks tagged %q\n", *tag)
		} else {
			fmt.Fprintln(out, "no bookmarks yet - add one with 'bkmr add <url>'")
		}
		return nil
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Added.After(rows[j].Added) })
	for _, b := range rows {
		fmt.Fprintln(out, formatRow(b))
	}
	return nil
}

// filterByTag keeps the bookmarks carrying tag, or copies the lot when tag is
// empty. The copy matters: runLs sorts the result, and sorting the collection's
// own slice in place would reorder what a later writer serializes.
func filterByTag(all []model.Bookmark, tag string) []model.Bookmark {
	if tag == "" {
		return append([]model.Bookmark(nil), all...)
	}
	// Normalized the same way stored tags were, so --tag "Async" matches the
	// "async" on disk.
	want := model.NormalizeTags([]string{tag})
	if len(want) == 0 {
		return nil
	}
	var rows []model.Bookmark
	for _, b := range all {
		for _, t := range b.Tags {
			if t == want[0] {
				rows = append(rows, b)
				break
			}
		}
	}
	return rows
}

// formatRow renders one bookmark as a single stdout line.
//
// A bookmark with no title gets two columns rather than three. label() falls
// back to the URL and the row ends with the URL, so the third column would have
// been the second one again - and a titleless bookmark is not rare: --no-fetch,
// a fetch that failed, and a browser import all produce them. The tab picker's
// save line made the same call.
func formatRow(b model.Bookmark) string {
	row := fmt.Sprintf("%s  %s", b.ID, label(b))
	if b.Title != "" {
		row += "  " + b.URL
	}
	if len(b.Tags) > 0 {
		row += "  [" + strings.Join(b.Tags, " ") + "]"
	}
	return row
}
