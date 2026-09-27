package main

import (
	"bytes"
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
	got := capture(t, func() {
		if code := dispatch([]string{"nosuchcommand"}); code != 2 {
			t.Errorf("dispatch(unknown) = %d, want 2", code)
		}
	})

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
