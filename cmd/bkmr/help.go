package main

import (
	"fmt"
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
			return fmt.Errorf("unknown command %q", args[0])
		}
		fmt.Fprintf(out, "%s\n\nusage: %s\n", c.Summary, c.Usage)
		return nil
	}

	fmt.Fprint(out, "bkmr - terminal-first bookmarks in a locally encrypted vault\n\nusage: bkmr <command> [arguments]\n\ncommands:\n")
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, c := range commands {
		fmt.Fprintf(w, "  %s\t%s\n", c.Name, c.Summary)
	}
	w.Flush()
	fmt.Fprint(out, "\nRun 'bkmr help <command>' for one command's usage.\n")
	return nil
}
