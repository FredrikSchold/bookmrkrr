package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

// recordFetchURL replaces the fetcher with one that remembers the URL it was
// handed and returns nothing. It is how the tests below assert what add would
// request without a round trip - which matters for the normalized-URL cases,
// because NormalizeURL gives a schemeless input an https scheme and no local
// test server can serve https to a client that has no way to trust its
// certificate. That the fetcher cannot be handed a permissive TLS config is the
// point of it having no injectable transport.
func recordFetchURL(t *testing.T) *atomic.Pointer[string] {
	t.Helper()
	var got atomic.Pointer[string]
	fetchTitle = func(_ context.Context, u string) (string, error) {
		got.Store(&u)
		return "", nil
	}
	return &got
}

func fetched(t *testing.T, p *atomic.Pointer[string]) string {
	t.Helper()
	u := p.Load()
	if u == nil {
		t.Fatal("no fetch was attempted at all")
	}
	return *u
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
	var called atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<title>Should Not Be Fetched</title>"))
	}))
	defer srv.Close()

	capture(t, func() {
		if err := runAdd([]string{"--no-fetch", srv.URL}); err != nil {
			t.Fatal(err)
		}
	})
	if called.Load() {
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
	var called atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
	}))
	defer srv.Close()

	capture(t, func() {
		if err := runAdd([]string{"--title", "Mine", srv.URL}); err != nil {
			t.Fatal(err)
		}
	})
	if called.Load() {
		t.Error("an explicit --title still triggered a fetch")
	}
}

func TestAddDoesNotFetchWhenTheConfigTurnsTheNetworkOff(t *testing.T) {
	dir := newVaultForTest(t, "pw")
	useRealFetcher(t)
	var called atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
	}))
	defer srv.Close()

	writeConfigForTest(t, dir, "network = \"off\"\n")

	capture(t, func() {
		if err := runAdd([]string{srv.URL}); err != nil {
			t.Fatal(err)
		}
	})
	if called.Load() {
		t.Error("network = \"off\" still made a request")
	}
}

// Review finding: the tracking parameters the tool strips before storage were
// still being transmitted to the site, because the raw URL went to the fetcher
// and only the stored copy was normalized. Sending utm_source to the very site
// we refuse to remember it for is the wrong way round.
func TestAddDoesNotSendTrackingParametersToTheSite(t *testing.T) {
	newVaultForTest(t, "pw")
	useRealFetcher(t)
	var served atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served.Store(true)
		if q := r.URL.RawQuery; q != "q=1" {
			t.Errorf("the site received query %q, want %q - no tracking parameters", q, "q=1")
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<title>Clean</title>"))
	}))
	defer srv.Close()

	raw := srv.URL + "/?utm_source=news&fbclid=xyz&q=1"
	capture(t, func() {
		if err := runAdd([]string{raw}); err != nil {
			t.Fatal(err)
		}
	})
	if !served.Load() {
		t.Fatal("the site was never reached, so nothing was asserted")
	}

	// What is stored is still exactly what the user typed. That was Task 8's
	// decision and this must not have changed it.
	v, _ := openVault()
	c, _ := v.Load()
	if c.Bookmarks[0].URL != raw {
		t.Errorf("stored URL = %q, want the URL as typed, %q", c.Bookmarks[0].URL, raw)
	}
	if c.Bookmarks[0].Title != "Clean" {
		t.Errorf("Title = %q, want %q", c.Bookmarks[0].Title, "Clean")
	}
}

// The same finding's other half: "example.com" is a supported input, and
// handing it to the fetcher unnormalized meant an unsupported-protocol error
// and a confusing warning instead of a fetch.
func TestAddFetchesASchemelessURLWithASchemeAdded(t *testing.T) {
	newVaultForTest(t, "pw")
	got := recordFetchURL(t)

	stdout, stderr := bothStreams(t, func() {
		if err := runAdd([]string{"example.com/a?utm_source=news"}); err != nil {
			t.Fatal(err)
		}
	})

	if want := "https://example.com/a"; fetched(t, got) != want {
		t.Errorf("fetched %q, want %q", fetched(t, got), want)
	}
	if !strings.Contains(stdout, "saved") {
		t.Errorf("stdout = %q, want it to report the bookmark was saved", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing - a schemeless URL is a supported input, not a problem", stderr)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if c.Bookmarks[0].URL != "example.com/a?utm_source=news" {
		t.Errorf("stored URL = %q, want the URL as typed", c.Bookmarks[0].URL)
	}
}

// Load cannot fail, so a broken config.toml falls back to the default - network
// on. Doing that in silence would take someone's kill switch away without
// telling them.
func TestAddReportsABrokenConfigOnErrOut(t *testing.T) {
	dir := newVaultForTest(t, "pw")
	writeConfigForTest(t, dir, "network = = broken [")

	stderr := captureErr(t, func() {
		capture(t, func() {
			if err := runAdd([]string{"https://example.com/a"}); err != nil {
				t.Fatal(err)
			}
		})
	})

	if !strings.Contains(stderr, "config") {
		t.Errorf("stderr = %q, want it to name the config file as the problem", stderr)
	}

	// And the bookmark is still saved: a broken config is not a reason to refuse
	// to work.
	if got := bookmarksInVault(t); len(got) != 1 {
		t.Errorf("len(Bookmarks) = %d, want 1", len(got))
	}
}

// A misspelled key parses as valid TOML, so nothing else would notice that the
// user's kill switch is not being read.
func TestAddReportsAnUnrecognizedConfigKey(t *testing.T) {
	dir := newVaultForTest(t, "pw")
	writeConfigForTest(t, dir, "netwrok = \"off\"\n")

	stderr := captureErr(t, func() {
		capture(t, func() {
			if err := runAdd([]string{"https://example.com/a"}); err != nil {
				t.Fatal(err)
			}
		})
	})

	if !strings.Contains(stderr, "netwrok") {
		t.Errorf("stderr = %q, want it to name the unrecognized key", stderr)
	}
	if got := bookmarksInVault(t); len(got) != 1 {
		t.Errorf("len(Bookmarks) = %d, want 1 - a stray config key is not a reason to refuse to work", len(got))
	}
}

// A config problem is a fact about the user's configuration, not about this one
// add, so the flags that mean nothing would have been fetched must not silence
// it.
func TestAddReportsABrokenConfigEvenWhenItWouldNotFetch(t *testing.T) {
	for _, args := range [][]string{
		{"--no-fetch", "https://example.com/a"},
		{"--title", "Mine", "https://example.com/a"},
	} {
		t.Run(strings.Join(args[:len(args)-1], " "), func(t *testing.T) {
			dir := newVaultForTest(t, "pw")
			writeConfigForTest(t, dir, "network = = broken [")

			stderr := captureErr(t, func() {
				capture(t, func() {
					if err := runAdd(args); err != nil {
						t.Fatal(err)
					}
				})
			})

			if !strings.Contains(stderr, "config") {
				t.Errorf("stderr = %q, want the config problem reported even though nothing was fetched", stderr)
			}
			if got := bookmarksInVault(t); len(got) != 1 {
				t.Errorf("len(Bookmarks) = %d, want 1", len(got))
			}
		})
	}
}

// writeConfigForTest writes config.toml where BKMR_DATA_DIR puts it, which is
// the same directory newVaultForTest returns.
func writeConfigForTest(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
