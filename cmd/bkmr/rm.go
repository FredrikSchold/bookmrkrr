package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/FredrikSchold/bookmrkrr/internal/model"
)

// confirm asks a yes/no question. A seam so tests do not need stdin.
var confirm = askYesNo

// askYesNo prints a question and reads an answer. Anything but y or yes is no,
// including an empty line and an unreadable stdin, because the one caller is a
// deletion: a question nobody answered has to mean "do not".
//
// The prompt goes to errOut, with the diagnostics, so that 'bkmr rm' has no way
// of writing it into somebody's pipe. The answer is read straight from os.Stdin
// rather than through a seam, because confirm is the seam - no test in this
// package reaches this function.
func askYesNo(question string) (bool, error) {
	fmt.Fprintf(errOut, "%s [y/N] ", question)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func init() {
	register(command{
		Name:    "rm",
		Summary: "delete a bookmark",
		// Flags first: see the note on edit's usage line.
		Usage: "bkmr rm [--force] <id>",
		Run:   runRm,
	})
}

func runRm(args []string) error {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	force := fs.Bool("force", false, "delete without confirming")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(errOut, "bkmr: %v\n", err)
		return errUsage
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(errOut, "bkmr: rm takes exactly one bookmark id, after the flags")
		return errUsage
	}
	id := fs.Arg(0)

	v, err := openVault()
	if err != nil {
		return err
	}
	// Loaded outside the lock so the question can name the bookmark, and so an
	// id that resolves to nothing is refused before anyone is asked about it.
	// Nothing is trusted from this read: Mutate reloads under the lock and the
	// delete below is what actually decides.
	c, err := v.Load()
	if err != nil {
		return explainVaultError(err)
	}
	b, ok := c.Find(id)
	if !ok {
		return fmt.Errorf("no bookmark with id %q", id)
	}

	if !*force {
		ok, err := confirm(fmt.Sprintf("Delete %s (%s)?", b.ID, b.URL))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(out, "kept", b.ID)
			return nil
		}
	}

	if err := v.Mutate(func(c *model.Collection) error {
		if !c.Delete(id) {
			return fmt.Errorf("bookmark %s no longer exists", id)
		}
		return nil
	}); err != nil {
		return explainVaultError(err)
	}
	fmt.Fprintln(out, "deleted", id)
	return nil
}
