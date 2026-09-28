package browser

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const devtoolsJSON = `[
  {"type":"page","title":"Real Page","url":"https://example.com/a"},
  {"type":"page","title":"Settings","url":"chrome://settings/"},
  {"type":"page","title":"An Extension","url":"chrome-extension://abcdef/popup.html"},
  {"type":"background_page","title":"Background","url":"https://example.com/bg"},
  {"type":"page","title":"DevTools","url":"devtools://devtools/bundled/x.html"},
  {"type":"page","title":"About","url":"about:blank"},
  {"type":"page","title":"Second Real Page","url":"https://go.dev/doc/"}
]`

func serveTabs(t *testing.T, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	old := Endpoints
	Endpoints = []string{srv.URL}
	t.Cleanup(func() { Endpoints = old })
}

func TestTabsReturnsOnlyRealPages(t *testing.T) {
	serveTabs(t, devtoolsJSON)

	tabs, err := Tabs(context.Background())
	if err != nil {
		t.Fatalf("Tabs() error = %v", err)
	}
	if len(tabs) != 2 {
		t.Fatalf("Tabs() = %d tabs, want 2:\n%+v", len(tabs), tabs)
	}
	if tabs[0].Title != "Real Page" || tabs[0].URL != "https://example.com/a" {
		t.Errorf("Tabs()[0] = %+v, want the example.com page", tabs[0])
	}
	if tabs[1].URL != "https://go.dev/doc/" {
		t.Errorf("Tabs()[1] = %+v, want the go.dev page", tabs[1])
	}
}

func TestTabsWithNoEndpointReportsErrNoBrowser(t *testing.T) {
	old := Endpoints
	// A port nothing is listening on.
	Endpoints = []string{"http://127.0.0.1:1"}
	defer func() { Endpoints = old }()

	_, err := Tabs(context.Background())
	if !errors.Is(err, ErrNoBrowser) {
		t.Errorf("Tabs() error = %v, want ErrNoBrowser", err)
	}
}

func TestTabsWithNoOpenPagesReportsAnEmptySlice(t *testing.T) {
	serveTabs(t, `[{"type":"page","title":"Settings","url":"chrome://settings/"}]`)

	tabs, err := Tabs(context.Background())
	if err != nil {
		t.Fatalf("Tabs() error = %v", err)
	}
	if len(tabs) != 0 {
		t.Errorf("Tabs() = %d tabs, want 0", len(tabs))
	}
}

func TestTabsIgnoresAMalformedResponseAndTriesTheNextEndpoint(t *testing.T) {
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("this is not json"))
	}))
	defer broken.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"type":"page","title":"Good","url":"https://good.example"}]`))
	}))
	defer good.Close()

	old := Endpoints
	Endpoints = []string{broken.URL, good.URL}
	defer func() { Endpoints = old }()

	tabs, err := Tabs(context.Background())
	if err != nil {
		t.Fatalf("Tabs() error = %v", err)
	}
	if len(tabs) != 1 || tabs[0].Title != "Good" {
		t.Errorf("Tabs() = %+v, want the tab from the working endpoint", tabs)
	}
}

func TestHintNamesTheDebugFlag(t *testing.T) {
	if !strings.Contains(Hint(), "--remote-debugging-port=9222") {
		t.Errorf("Hint() = %q, want it to name the debug port flag", Hint())
	}
}

// The filter is a whitelist: http and https, and nothing else. A blacklist of
// internal schemes left the user a chooser full of dead options, because
// model.NormalizeURL accepts only http and https - so a file:// or ftp: tab was
// listed, picked, and then refused by Add, after the vault had already asked for
// a password. Every one of these must be gone, and the two that matter most are
// the ones a prefix list would miss: a blob: or data: URL cannot be revisited
// once its tab closes, and view-source: wraps a URL that does start with https.
func TestTabsKeepsOnlyHTTPAndHTTPS(t *testing.T) {
	serveTabs(t, `[
	  {"type":"page","title":"Settings","url":"chrome://settings/"},
	  {"type":"page","title":"Ext","url":"chrome-extension://abcdef/popup.html"},
	  {"type":"page","title":"Edge","url":"edge://settings/"},
	  {"type":"page","title":"Brave","url":"brave://rewards/"},
	  {"type":"page","title":"Vivaldi","url":"vivaldi://settings/"},
	  {"type":"page","title":"Opera","url":"opera://about/"},
	  {"type":"page","title":"NTP","url":"chrome-search://local-ntp/local-ntp.html"},
	  {"type":"page","title":"Native","url":"chrome-native://newtab/"},
	  {"type":"page","title":"Untrusted","url":"chrome-untrusted://print/"},
	  {"type":"page","title":"DevTools","url":"devtools://devtools/bundled/x.html"},
	  {"type":"page","title":"About","url":"about:blank"},
	  {"type":"page","title":"Source","url":"view-source:https://example.com/"},
	  {"type":"page","title":"Blob","url":"blob:https://example.com/9f2"},
	  {"type":"page","title":"Data","url":"data:text/html,<p>hi"},
	  {"type":"page","title":"Local file","url":"file:///C:/notes.txt"},
	  {"type":"page","title":"FTP","url":"ftp://ftp.example.com/pub/"},
	  {"type":"page","title":"Failed","url":"chrome-error://chromewebdata/"},
	  {"type":"page","title":"Filesystem","url":"filesystem:https://example.com/temporary/x"},
	  {"type":"page","title":"Custom handler","url":"zoommtg://zoom.us/join?confno=1"},
	  {"type":"page","title":"Nothing","url":""},
	  {"type":"page","title":"Keep plain","url":"http://plain.example/"},
	  {"type":"page","title":"Keep secure","url":"https://keep.example/"}
	]`)

	tabs, err := Tabs(context.Background())
	if err != nil {
		t.Fatalf("Tabs() error = %v", err)
	}
	if len(tabs) != 2 {
		t.Fatalf("Tabs() = %d tabs, want only the http and https ones: %+v", len(tabs), tabs)
	}
	if tabs[0].URL != "http://plain.example/" || tabs[1].URL != "https://keep.example/" {
		t.Errorf("Tabs() = %+v, want the two http(s) pages", tabs)
	}
}

// An uppercase scheme is still the same scheme. Chrome reports lowercase, so
// this is about the prefix test not being accidentally case-sensitive.
func TestTabsKeepsAnUppercaseScheme(t *testing.T) {
	serveTabs(t, `[{"type":"page","title":"Shouty","url":"HTTPS://Example.com/A"}]`)

	tabs, err := Tabs(context.Background())
	if err != nil {
		t.Fatalf("Tabs() error = %v", err)
	}
	if len(tabs) != 1 {
		t.Fatalf("Tabs() = %d tabs, want 1", len(tabs))
	}
}

// A tab that is still loading, or showing a PDF or a download, has no title.
// Dropping it would hide a tab the user can see in their own tab strip, so it is
// kept with an empty title - and left empty, rather than filled in with the URL:
// cmd/bkmr shows the URL in the picker row and stores no title at all, so ls does
// not end up printing the URL twice for that bookmark.
func TestATabWithNoTitleIsKeptWithAnEmptyTitle(t *testing.T) {
	serveTabs(t, `[{"type":"page","title":"   ","url":"https://untitled.example/paper.pdf"}]`)

	tabs, err := Tabs(context.Background())
	if err != nil {
		t.Fatalf("Tabs() error = %v", err)
	}
	if len(tabs) != 1 {
		t.Fatalf("Tabs() = %d tabs, want the untitled tab kept", len(tabs))
	}
	if tabs[0].Title != "" {
		t.Errorf("Title = %q, want it left empty", tabs[0].Title)
	}
	if tabs[0].URL != "https://untitled.example/paper.pdf" {
		t.Errorf("URL = %q, want it unchanged", tabs[0].URL)
	}
}

// document.title keeps whatever whitespace the markup had, newlines included,
// and the browser only collapses it for display. Left alone, one such title
// would break the picker's one-row-per-tab layout and split the command's own
// "saved ..." line in two. internal/fetch collapses titles for exactly this
// reason; a title from a browser tab comes from the same place.
func TestATitleSpanningSeveralLinesIsCollapsed(t *testing.T) {
	serveTabs(t, `[{"type":"page","title":"Two\n\tLines  Here","url":"https://wrapped.example/"}]`)

	tabs, err := Tabs(context.Background())
	if err != nil {
		t.Fatalf("Tabs() error = %v", err)
	}
	if len(tabs) != 1 {
		t.Fatalf("Tabs() = %d tabs, want 1", len(tabs))
	}
	if tabs[0].Title != "Two Lines Here" {
		t.Errorf("Title = %q, want the whitespace collapsed to single spaces", tabs[0].Title)
	}
}

// Something other than a browser really can be listening on 9222 - it was, on
// the machine this was written on, answering /json with a 404. Without the
// status check that endpoint would win the probe and 'bkmr tab' would report
// "no open tabs" on a machine with a browser full of them, instead of falling
// through and printing the hint. The 404 body here is valid JSON on purpose:
// that is what makes this a test of the status code rather than of parsing.
func TestTabsSkipsAnEndpointThatIsNotADevToolsServer(t *testing.T) {
	stranger := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`[]`))
	}))
	defer stranger.Close()
	real := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"type":"page","title":"Real","url":"https://real.example/"}]`))
	}))
	defer real.Close()

	old := Endpoints
	Endpoints = []string{stranger.URL, real.URL}
	defer func() { Endpoints = old }()

	tabs, err := Tabs(context.Background())
	if err != nil {
		t.Fatalf("Tabs() error = %v", err)
	}
	if len(tabs) != 1 || tabs[0].Title != "Real" {
		t.Errorf("Tabs() = %+v, want the tab from the endpoint that is actually a browser", tabs)
	}
}
