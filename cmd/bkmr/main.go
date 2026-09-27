// Command bkmr is a terminal-first bookmark manager with a locally encrypted
// vault.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

const version = "0.1.0"

// out is where all user-facing output goes, so tests can capture it.
var out io.Writer = os.Stdout

type command struct {
	Name    string
	Summary string
	Usage   string
	Run     func(args []string) error
}

// commands is the whole command surface. Each command file appends to it in
// its own init function.
var commands []command

func register(c command) { commands = append(commands, c) }

func find(name string) (command, bool) {
	for _, c := range commands {
		if c.Name == name {
			return c, true
		}
	}
	return command{}, false
}

func init() {
	register(command{
		Name:    "version",
		Summary: "print the bkmr version",
		Usage:   "bkmr version",
		Run: func([]string) error {
			fmt.Fprintln(out, "bkmr", version)
			return nil
		},
	})
}

// dispatch runs one invocation and returns the process exit code.
func dispatch(args []string) int {
	name := "picker"
	if len(args) > 0 {
		name = args[0]
		args = args[1:]
	}
	if name == "--help" || name == "-h" {
		name, args = "help", nil
	}

	c, ok := find(name)
	if !ok {
		fmt.Fprintf(out, "bkmr: unknown command %q\nRun 'bkmr help' for the list of commands.\n", name)
		return 2
	}
	if err := c.Run(args); err != nil {
		if errors.Is(err, errUsage) {
			fmt.Fprintf(out, "usage: %s\n", c.Usage)
			return 2
		}
		fmt.Fprintln(os.Stderr, "bkmr:", err)
		return 1
	}
	return 0
}

// errUsage makes a command print its usage line and exit 2.
var errUsage = errors.New("usage")

func main() { os.Exit(dispatch(os.Args[1:])) }
