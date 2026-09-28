package main

import (
	"fmt"
	"sort"
	"text/tabwriter"
)

func init() {
	register(command{
		Name:    "tags",
		Summary: "list your tags with a count of bookmarks each",
		Usage:   "bkmr tags",
		Run:     runTags,
	})
}

func runTags([]string) error {
	v, err := openVault()
	if err != nil {
		return err
	}
	c, err := v.Load()
	if err != nil {
		return explainVaultError(err)
	}

	counts := c.TagCounts()
	if len(counts) == 0 {
		fmt.Fprintln(out, "no tags yet - add one with 'bkmr add <url> -t name'")
		return nil
	}

	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	// Most-used first, alphabetical within a count.
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, name := range names {
		fmt.Fprintf(w, "%s\t%d\n", name, counts[name])
	}
	return w.Flush()
}
