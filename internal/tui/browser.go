package tui

import (
	"fmt"
	"os/exec"
	"runtime"
)

// Open launches a URL in the user's default browser. It is a variable so
// tests can replace it.
//
// That seam is not a convenience: a test that reached OpenInBrowser would
// launch a real browser window on whoever's machine ran the suite. Every test
// that exercises an open path replaces this.
var Open = OpenInBrowser

// OpenInBrowser hands a URL to the platform's default handler.
func OpenInBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open %s: %w", url, err)
	}
	// Reap the child so it does not linger as a zombie.
	go func() { _ = cmd.Wait() }()
	return nil
}
