// Package browser lists the pages currently open in a Chromium-based browser
// by querying its DevTools endpoint on loopback. Nothing leaves the machine,
// and no browser extension is required - but the browser must have been
// started with --remote-debugging-port.
//
// The one request this package makes is a GET of a fixed path, /json, which
// enumerates the open targets and nothing else. The same endpoint does have
// mutating routes - /json/new opens a tab, /json/activate focuses one,
// /json/close closes one - and the websocket URL each target advertises is
// where real CDP commands like Page.navigate and Runtime.evaluate would live.
// None of them is reachable from here: the path is a constant, no part of it
// comes from a caller, and no websocket is ever opened. Listing is the whole
// capability.
//
// This package and internal/fetch are the only two permitted to import
// net/http; a CI test enforces that. It returns Tab values and nothing more,
// so it imports neither internal/model, internal/store nor internal/tui.
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// Endpoints are probed in order. 9222 is Chrome's documented default; 9223
// and 9224 are common when Edge or Brave run alongside it.
var Endpoints = []string{
	"http://127.0.0.1:9222",
	"http://127.0.0.1:9223",
	"http://127.0.0.1:9224",
}

// ErrNoBrowser means no DevTools endpoint answered.
var ErrNoBrowser = errors.New("no browser is exposing its DevTools endpoint on loopback")

// Tab is one open page.
type Tab struct {
	Title string
	URL   string
}

type devtoolsTarget struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// internalSchemes are the URLs a bookmark manager must never offer to save:
// either the browser's own pages, or a URL that cannot be revisited later.
//
// The first eight are the browser-internal ones. The rest were added because
// each would otherwise put junk in front of the user: blob: and data: URLs are
// meaningless the moment the tab that holds them closes, chrome-search:// is
// Chrome's own new-tab page, and Vivaldi and Opera are Chromium browsers whose
// internal pages use their own scheme rather than chrome://, exactly as Edge's
// and Brave's do.
var internalSchemes = []string{
	"chrome://", "chrome-extension://", "devtools://", "about:",
	"edge://", "brave://", "chrome-untrusted://", "view-source:",
	"vivaldi://", "opera://", "chrome-search://", "chrome-native://",
	"blob:", "data:",
}

// Tabs returns the real pages open in the first browser that answers.
func Tabs(ctx context.Context) ([]Tab, error) {
	client := &http.Client{Timeout: 2 * time.Second}

	for _, base := range Endpoints {
		targets, err := probe(ctx, client, base)
		if err != nil {
			continue
		}
		tabs := make([]Tab, 0, len(targets))
		for _, t := range targets {
			if t.Type != "page" || isInternal(t.URL) {
				continue
			}
			tabs = append(tabs, Tab{Title: titleOf(t), URL: t.URL})
		}
		return tabs, nil
	}
	return nil, fmt.Errorf("%w\n%s", ErrNoBrowser, Hint())
}

func probe(ctx context.Context, client *http.Client, base string) ([]devtoolsTarget, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", base, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var targets []devtoolsTarget
	if err := json.Unmarshal(body, &targets); err != nil {
		return nil, err
	}
	return targets, nil
}

// titleOf is the label for one tab.
//
// The whitespace is collapsed because document.title keeps whatever the markup
// had - a <title> written across two indented lines really does arrive with a
// newline and a tab in it, and only the tab strip collapses it for display.
// Left alone, one of those would break the picker's one-row-per-tab layout and
// split the saved line the command prints in two. internal/fetch collapses page
// titles for the same reason; a title from a browser tab comes from the same
// place, which is a page, not the user.
//
// An empty title falls back to the URL rather than dropping the tab. A tab that
// is still loading, or showing a PDF or a download, has no title at all, and
// hiding a tab the user can see in their own tab strip would be the worse
// surprise - as would a blank row in the picker.
func titleOf(t devtoolsTarget) string {
	if title := strings.Join(strings.Fields(t.Title), " "); title != "" {
		return title
	}
	return t.URL
}

func isInternal(url string) bool {
	low := strings.ToLower(url)
	for _, s := range internalSchemes {
		if strings.HasPrefix(low, s) {
			return true
		}
	}
	return low == ""
}

// Hint returns the platform-specific command that enables the debug port, so
// the error message tells the user exactly what to do.
func Hint() string {
	switch runtime.GOOS {
	case "windows":
		return `Start your browser with the debug port enabled, for example:
  "C:\Program Files\Google\Chrome\Application\chrome.exe" --remote-debugging-port=9222`
	case "darwin":
		return `Start your browser with the debug port enabled, for example:
  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --remote-debugging-port=9222`
	default:
		return `Start your browser with the debug port enabled, for example:
  google-chrome --remote-debugging-port=9222`
	}
}
