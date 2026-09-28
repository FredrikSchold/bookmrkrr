package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FredrikSchold/bookmrkrr/internal/capture/browser"
)

// No test in this package may open the real tab picker: chooseTab's default
// calls tui.Run, which starts a bubbletea program on the alt screen of whoever
// is running the suite. Replacing the seam for the whole package makes that a
// property of the suite rather than an accident of which branch each test
// happens to take - the same reasoning as stubOpen in open_test.go. The tests
// that need a choice install their own.
func init() {
	chooseTab = func([]tabChoice) (tabChoice, bool, error) {
		return tabChoice{}, false, errors.New("the tab picker was opened in a test")
	}
}

// stubTabs serves a DevTools /json response listing the given url/title pairs.
func stubTabs(t *testing.T, urlTitlePairs ...string) {
	t.Helper()
	type entry struct {
		Type  string `json:"type"`
		Title string `json:"title"`
		URL   string `json:"url"`
	}
	var entries []entry
	for i := 0; i+1 < len(urlTitlePairs); i += 2 {
		entries = append(entries, entry{Type: "page", URL: urlTitlePairs[i], Title: urlTitlePairs[i+1]})
	}
	body, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	t.Cleanup(srv.Close)

	old := browser.Endpoints
	browser.Endpoints = []string{srv.URL}
	t.Cleanup(func() { browser.Endpoints = old })
}

// stubNoBrowser points the probe at a port nothing listens on.
func stubNoBrowser(t *testing.T) {
	t.Helper()
	old := browser.Endpoints
	browser.Endpoints = []string{"http://127.0.0.1:1"}
	t.Cleanup(func() { browser.Endpoints = old })
}
