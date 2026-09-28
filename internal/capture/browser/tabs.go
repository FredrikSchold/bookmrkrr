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
			if t.Type != "page" || !savable(t.URL) {
				continue
			}
			// The whitespace is collapsed because document.title keeps whatever
			// the markup had - a <title> written across two indented lines really
			// does arrive with a newline and a tab in it, and only the tab strip
			// collapses it for display. An empty title is left empty rather than
			// filled in with the URL: cmd/bkmr shows the URL in the picker row
			// for a tab with no title, and stores no title at all, so ls does not
			// print the URL twice for that bookmark.
			tabs = append(tabs, Tab{Title: strings.Join(strings.Fields(t.Title), " "), URL: t.URL})
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

// savable reports whether a tab's URL is one a bookmark can actually be made
// from: http or https, and nothing else.
//
// A whitelist, deliberately, because the blacklist it replaces was not merely
// incomplete - it was the wrong shape. model.NormalizeURL accepts only http and
// https, so every scheme the list did not happen to name was offered to the
// user, chosen, and then refused by Add, after openVault had already asked for a
// password. file://, ftp:, chrome-error://, filesystem: and any custom protocol
// handler page were all dead options in the chooser.
//
// The prefix test also subsumes every scheme that list did name, including the
// three a scheme-prefix blacklist is worst at: view-source:https://... and
// blob:https://... both wrap an https URL, and data: carries its payload inline.
// None of the three starts with http:// or https://, so all of them are gone
// without being enumerated, along with chrome://, devtools://, about:, the other
// Chromium browsers' own schemes, and an empty URL.
//
// Whether file:// ought to be savable is a question about NormalizeURL, not
// about this filter.
func savable(rawURL string) bool {
	low := strings.ToLower(rawURL)
	return strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://")
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
