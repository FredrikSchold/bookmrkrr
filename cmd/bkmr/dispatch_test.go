package main

import (
	"errors"
	"strings"
	"testing"
)

// Two probe commands, registered only in the test binary, so the errUsage and
// generic-error branches of dispatch have real commands to exercise them. The
// live-slice tests in help_test.go absorb these registrations unchanged.
func init() {
	register(command{
		Name:    "probe-usage",
		Summary: "probe: returns errUsage",
		Usage:   "bkmr probe-usage <arg>",
		Run:     func([]string) error { return errUsage },
	})
	register(command{
		Name:    "probe-fail",
		Summary: "probe: returns a plain error",
		Usage:   "bkmr probe-fail",
		Run:     func([]string) error { return errors.New("the vault is locked") },
	})
}

// bothStreams runs fn and returns what it wrote to stdout and to stderr.
func bothStreams(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	stderr = captureErr(t, func() { stdout = capture(t, fn) })
	return stdout, stderr
}

func TestErrUsagePrintsTheUsageLineToStderr(t *testing.T) {
	stdout, stderr := bothStreams(t, func() {
		if code := dispatch([]string{"probe-usage"}); code != 2 {
			t.Errorf("dispatch(probe-usage) = %d, want 2", code)
		}
	})

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing; a usage error must not land in the pipe", stdout)
	}
	c, ok := find("probe-usage")
	if !ok {
		t.Fatal("the probe-usage command is not registered")
	}
	if !strings.Contains(stderr, "usage: "+c.Usage) {
		t.Errorf("stderr = %q, want it to contain the usage line %q", stderr, "usage: "+c.Usage)
	}
}

func TestAPlainErrorGoesToStderrAndExitsOne(t *testing.T) {
	stdout, stderr := bothStreams(t, func() {
		if code := dispatch([]string{"probe-fail"}); code != 1 {
			t.Errorf("dispatch(probe-fail) = %d, want 1", code)
		}
	})

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing; an error must not land in the pipe", stdout)
	}
	if !strings.Contains(stderr, "the vault is locked") {
		t.Errorf("stderr = %q, want it to contain the error message", stderr)
	}
	if !strings.HasPrefix(stderr, "bkmr:") {
		t.Errorf("stderr = %q, want it to be prefixed with the binary name", stderr)
	}
}

func TestLoneHelpFlagAfterACommandShowsThatCommandsUsage(t *testing.T) {
	c, ok := find("probe-usage")
	if !ok {
		t.Fatal("the probe-usage command is not registered")
	}

	for _, flag := range []string{"--help", "-h"} {
		// probe-usage's Run returns errUsage, so if the flag were passed
		// through instead of intercepted this would exit 2 and write to stderr.
		stdout, stderr := bothStreams(t, func() {
			if code := dispatch([]string{"probe-usage", flag}); code != 0 {
				t.Errorf("dispatch(probe-usage %s) = %d, want 0", flag, code)
			}
		})

		if stderr != "" {
			t.Errorf("probe-usage %s wrote %q to stderr, want nothing", flag, stderr)
		}
		if !strings.Contains(stdout, c.Usage) {
			t.Errorf("probe-usage %s = %q, want it to contain the usage line %q", flag, stdout, c.Usage)
		}
		if !strings.Contains(stdout, c.Summary) {
			t.Errorf("probe-usage %s = %q, want it to contain the summary %q", flag, stdout, c.Summary)
		}
	}
}

func TestHelpFlagAmongOtherArgumentsIsNotIntercepted(t *testing.T) {
	// More than a lone flag is a real invocation, so Run must fire and its
	// errUsage must surface rather than dispatch swallowing the flag.
	stdout, stderr := bothStreams(t, func() {
		if code := dispatch([]string{"probe-usage", "--help", "extra"}); code != 2 {
			t.Errorf("dispatch(probe-usage --help extra) = %d, want 2", code)
		}
	})

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if !strings.Contains(stderr, "usage: bkmr probe-usage") {
		t.Errorf("stderr = %q, want the command's own errUsage to have surfaced", stderr)
	}
}

func TestHelpFlagOnAnUnknownCommandStillFails(t *testing.T) {
	stdout, stderr := bothStreams(t, func() {
		if code := dispatch([]string{"nosuchcommand", "--help"}); code != 2 {
			t.Errorf("dispatch(nosuchcommand --help) = %d, want 2", code)
		}
	})

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if !strings.Contains(stderr, "nosuchcommand") {
		t.Errorf("stderr = %q, want it to name the unknown command", stderr)
	}
}
