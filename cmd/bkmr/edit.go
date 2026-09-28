package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/FredrikSchold/bookmrkrr/internal/model"
)

func init() {
	register(command{
		Name:    "edit",
		Summary: "change a bookmark's title, note or tags",
		Usage:   "bkmr edit <id> [--title text] [--note text] [-t tag]...",
		Run:     runEdit,
	})
}

func runEdit(args []string) error {
	var tags tagList
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Var(&tags, "t", "replacement tag (repeatable)")
	fs.Var(&tags, "tag", "replacement tag (repeatable)")
	title := fs.String("title", "", "new title")
	note := fs.String("note", "", "new note")
	// parsePermuted so the id can come before or after the flags; see main.go.
	rest, err := parsePermuted(fs, args)
	if err != nil {
		fmt.Fprintf(errOut, "bkmr: %v\n", err)
		return errUsage
	}
	if len(rest) != 1 {
		fmt.Fprintln(errOut, "bkmr: edit takes exactly one bookmark id")
		return errUsage
	}
	id := rest[0]

	// Distinguish "flag absent" from "flag set to empty". Clearing a title with
	// --title "" has to be possible, so the zero value cannot mean "leave it".
	setTitle, setNote := false, false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "title":
			setTitle = true
		case "note":
			setNote = true
		}
	})
	if !setTitle && !setNote && len(tags) == 0 {
		// Every field is optional, so this is reachable, and re-encrypting the
		// whole vault to change nothing is worse than saying so.
		fmt.Fprintln(errOut, "bkmr: nothing to change - give --title, --note or -t")
		return errUsage
	}

	v, err := openVault()
	if err != nil {
		return err
	}

	var updated model.Bookmark
	if err := v.Mutate(func(c *model.Collection) error {
		b, ok := c.Find(id)
		if !ok {
			return fmt.Errorf("no bookmark with id %q", id)
		}
		if setTitle {
			// model.CleanTitle explicitly, even though the write path cleans
			// the collection on its way to disk: the line below prints this
			// title to a terminal before that happens, and an ESC in a title is
			// the whole reason the rule exists. Cleaning at the gate protects
			// the vault, not this process's stdout.
			b.Title = model.CleanTitle(*title)
		}
		if setNote {
			// No explicit clean: a note is not printed here, and the write path
			// is what has to be trusted for it. The test that puts an escape in
			// --note is asserting exactly that.
			b.Notes = *note
		}
		if len(tags) > 0 {
			b.Tags = model.NormalizeTags(tags)
		}
		updated = *b
		return nil
	}); err != nil {
		return explainVaultError(err)
	}

	// Reported after the write, not inside the callback: a Mutate that failed on
	// the lock or on the publishing rename changed nothing, and must not have
	// already said it did.
	fmt.Fprintf(out, "updated %s  %s\n", updated.ID, label(updated))
	return nil
}
