package main

import (
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"
)

func init() {
	register(command{
		Name:    "help",
		Summary: "list commands, or show one command's usage",
		Usage:   "bkmr help [command]",
		Run:     runHelp,
	})
}

func runHelp(args []string) error {
	if len(args) > 0 {
		c, ok := find(args[0])
		if !ok {
			// A name nobody registered is a usage error, so it exits 2 like any
			// other unknown command rather than 1. Per the errUsage contract,
			// the explanation is printed here and errUsage returned bare.
			fmt.Fprintf(errOut, "bkmr: unknown command %q\n", args[0])
			return errUsage
		}
		fmt.Fprintf(out, "%s\n\nusage: %s\n", c.Summary, c.Usage)
		return nil
	}

	// Listed by name, not in registration order: registration order follows
	// source filenames, which is no business of the user's. Sort a copy, since
	// the registration order of commands itself is left alone.
	listing := slices.Clone(commands)
	slices.SortFunc(listing, func(a, b command) int { return strings.Compare(a.Name, b.Name) })

	fmt.Fprint(out, "bkmr - terminal-first bookmarks in a locally encrypted vault\n\nusage: bkmr <command> [arguments]\n\ncommands:\n")
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, c := range listing {
		fmt.Fprintf(w, "  %s\t%s\n", c.Name, c.Summary)
	}
	w.Flush()
	fmt.Fprint(out, "\nRun 'bkmr help <command>' for one command's usage.\n")
	return nil
}
