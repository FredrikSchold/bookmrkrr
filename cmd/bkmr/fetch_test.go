package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FredrikSchold/bookmrkrr/internal/fetch"
)

// useRealFetcher undoes the network stub newVaultForTest installs. Only the
// tests below do this, and only because each one starts its own httptest
// server and wants the real fetcher pointed at it. newVaultForTest's own
// t.Cleanup puts fetch.Title back afterwards, so nothing leaks into the next
// test either way.
func useRealFetcher(t *testing.T) {
	t.Helper()
	fetchTitle = fetch.Title
}

func TestAddFetchesTheTitleWhenNoneIsGiven(t *testing.T) {
	newVaultForTest(t, "pw")
	useRealFetcher(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<title>Fetched Title</title>"))
	}))
	defer srv.Close()

	capture(t, func() {
		if err := runAdd([]string{srv.URL}); err != nil {
			t.Fatalf("runAdd() error = %v", err)
		}
	})

	v, _ := openVault()
	c, _ := v.Load()
	if c.Bookmarks[0].Title != "Fetched Title" {
		t.Errorf("Title = %q, want %q", c.Bookmarks[0].Title, "Fetched Title")
	}
}

func TestAddWithNoFetchSkipsTheNetwork(t *testing.T) {
	newVaultForTest(t, "pw")
	useRealFetcher(t)
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<title>Should Not Be Fetched</title>"))
	}))
	defer srv.Close()

	capture(t, func() {
		if err := runAdd([]string{"--no-fetch", srv.URL}); err != nil {
			t.Fatal(err)
		}
	})
	if called {
		t.Error("--no-fetch still made a request")
	}

	v, _ := openVault()
	c, _ := v.Load()
	if c.Bookmarks[0].Title != "" {
		t.Errorf("Title = %q, want empty with --no-fetch", c.Bookmarks[0].Title)
	}
}

// The spec's hardest rule: network failure must never lose a bookmark.
func TestAddSavesTheBookmarkWhenTheFetchFails(t *testing.T) {
	newVaultForTest(t, "pw")
	useRealFetcher(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	stdout, stderr := bothStreams(t, func() {
		if err := runAdd([]string{srv.URL}); err != nil {
			t.Fatalf("runAdd() error = %v, want the bookmark saved despite the fetch failing", err)
		}
	})

	if !strings.Contains(stdout, "saved") {
		t.Errorf("stdout = %q, want it to report the bookmark was saved", stdout)
	}
	// The failed fetch is a diagnostic, so it belongs on errOut and nowhere
	// else: a silent failure leaves the user with an untitled bookmark and no
	// way to tell whether bkmr tried at all.
	if !strings.Contains(stderr, "title") {
		t.Errorf("stderr = %q, want a note that the title could not be read", stderr)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 1 {
		t.Fatalf("len(Bookmarks) = %d, want 1", len(c.Bookmarks))
	}
	if c.Bookmarks[0].Title != "" {
		t.Errorf("Title = %q, want empty after a failed fetch", c.Bookmarks[0].Title)
	}
}

func TestAddDoesNotFetchWhenAnExplicitTitleIsGiven(t *testing.T) {
	newVaultForTest(t, "pw")
	useRealFetcher(t)
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	capture(t, func() {
		if err := runAdd([]string{"--title", "Mine", srv.URL}); err != nil {
			t.Fatal(err)
		}
	})
	if called {
		t.Error("an explicit --title still triggered a fetch")
	}
}

func TestAddDoesNotFetchWhenTheConfigTurnsTheNetworkOff(t *testing.T) {
	dir := newVaultForTest(t, "pw")
	useRealFetcher(t)
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	// BKMR_DATA_DIR, which newVaultForTest set, also relocates config.toml.
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("network = \"off\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	capture(t, func() {
		if err := runAdd([]string{srv.URL}); err != nil {
			t.Fatal(err)
		}
	})
	if called {
		t.Error("network = \"off\" still made a request")
	}
}
