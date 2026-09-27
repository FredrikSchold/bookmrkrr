package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

func capture(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	old := out
	out = &buf
	defer func() { out = old }()
	fn()
	return buf.String()
}

// captureErr is capture's counterpart for diagnostics.
func captureErr(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	old := errOut
	errOut = &buf
	defer func() { errOut = old }()
	fn()
	return buf.String()
}

// listedCommandNames returns the command names from the listing section of
// help output, in the order they were printed.
func listedCommandNames(t *testing.T, help string) []string {
	t.Helper()
	_, rest, ok := strings.Cut(help, "commands:\n")
	if !ok {
		t.Fatalf("help output has no commands section: %q", help)
	}
	body, _, ok := strings.Cut(rest, "\nRun '")
	if !ok {
		t.Fatalf("help output has no trailing hint: %q", help)
	}
	var names []string
	for _, line := range strings.Split(body, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 {
			names = append(names, fields[0])
		}
	}
	return names
}

func TestHelpListsEveryCommand(t *testing.T) {
	got := capture(t, func() {
		if code := dispatch([]string{"help"}); code != 0 {
			t.Errorf("dispatch(help) = %d, want 0", code)
		}
	})

	for _, c := range commands {
		if !strings.Contains(got, c.Name) {
			t.Errorf("help output is missing command %q", c.Name)
		}
		if c.Summary == "" {
			t.Errorf("command %q has no summary", c.Name)
		}
	}
	if !strings.Contains(got, "bkmr") {
		t.Error("help output does not mention the binary name")
	}

	listed := listedCommandNames(t, got)
	if len(listed) != len(commands) {
		t.Errorf("help listed %d commands %v, want %d", len(listed), listed, len(commands))
	}
	if !slices.IsSorted(listed) {
		t.Errorf("help listing is not sorted by name: %v", listed)
	}
}

func TestHelpForOneCommandPrintsItsUsage(t *testing.T) {
	got := capture(t, func() { dispatch([]string{"help", "help"}) })

	h, ok := find("help")
	if !ok {
		t.Fatal("the help command is not registered")
	}
	if !strings.Contains(got, h.Usage) {
		t.Errorf("help help = %q, want it to contain the usage line %q", got, h.Usage)
	}
}

func TestHelpFlagsAreAliases(t *testing.T) {
	long := capture(t, func() { dispatch([]string{"--help"}) })
	short := capture(t, func() { dispatch([]string{"-h"}) })

	if long == "" || long != short {
		t.Errorf("--help and -h must print the same help text; got %q and %q", long, short)
	}
}

func TestUnknownCommandExitsTwo(t *testing.T) {
	var stdout string
	got := captureErr(t, func() {
		stdout = capture(t, func() {
			if code := dispatch([]string{"nosuchcommand"}); code != 2 {
				t.Errorf("dispatch(unknown) = %d, want 2", code)
			}
		})
	})

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing; an error must not land in the pipe", stdout)
	}
	if !strings.Contains(got, "nosuchcommand") {
		t.Errorf("output = %q, want it to name the unknown command", got)
	}
	if !strings.Contains(got, "help") {
		t.Errorf("output = %q, want it to point at 'bkmr help'", got)
	}
}

func TestVersionPrintsSomething(t *testing.T) {
	got := capture(t, func() {
		if code := dispatch([]string{"version"}); code != 0 {
			t.Errorf("dispatch(version) = %d, want 0", code)
		}
	})

	if !strings.Contains(got, version) {
		t.Errorf("version output = %q, want it to contain %q", got, version)
	}
}

func TestEveryCommandHasAUniqueName(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range commands {
		if seen[c.Name] {
			t.Errorf("duplicate command name %q", c.Name)
		}
		seen[c.Name] = true
		if c.Run == nil {
			t.Errorf("command %q has a nil Run", c.Name)
		}
	}
}
