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

// out receives user-facing program output; errOut receives diagnostics.
// Both are variables so tests can capture them.
var (
	out    io.Writer = os.Stdout
	errOut io.Writer = os.Stderr
)

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
		fmt.Fprintf(errOut, "bkmr: unknown command %q\nRun 'bkmr help' for the list of commands.\n", name)
		return 2
	}

	// A lone --help or -h after a command name asks for that command's usage,
	// handled here so no command has to implement it. name is registered, so
	// runHelp cannot fail. A longer argument list is a real invocation and is
	// passed through untouched.
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_ = runHelp([]string{name})
		return 0
	}

	if err := c.Run(args); err != nil {
		if errors.Is(err, errUsage) {
			fmt.Fprintf(errOut, "usage: %s\n", c.Usage)
			return 2
		}
		fmt.Fprintln(errOut, "bkmr:", err)
		return 1
	}
	return 0
}

// errUsage makes a command print its usage line and exit 2.
//
// Wrapping it is pointless: dispatch prints only the usage line, so any
// wrapped text is discarded. A command that wants to explain why the usage was
// wrong prints its own line to errOut and then returns errUsage bare. This is
// deliberate — errUsage.Error() is the single word "usage", so a wrapped error
// would render as "add: url is required: usage", worse than either half alone.
var errUsage = errors.New("usage")

func main() { os.Exit(dispatch(os.Args[1:])) }
