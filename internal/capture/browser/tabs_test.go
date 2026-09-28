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

// The brief's own filter list stops at the schemes Chrome, Edge and Brave use.
// A tab whose URL cannot be revisited - a blob: or data: URL is meaningless the
// moment the tab closes - and the other Chromium browsers' own pages are junk in
// front of the user for the same reason chrome:// is.
func TestTabsFiltersTheOtherUnsavableSchemes(t *testing.T) {
	serveTabs(t, `[
	  {"type":"page","title":"Blob","url":"blob:https://example.com/9f2"},
	  {"type":"page","title":"Data","url":"data:text/html,<p>hi"},
	  {"type":"page","title":"Vivaldi","url":"vivaldi://settings/"},
	  {"type":"page","title":"Opera","url":"opera://about/"},
	  {"type":"page","title":"New Tab","url":"chrome-search://local-ntp/local-ntp.html"},
	  {"type":"page","title":"Keep","url":"https://keep.example/"}
	]`)

	tabs, err := Tabs(context.Background())
	if err != nil {
		t.Fatalf("Tabs() error = %v", err)
	}
	if len(tabs) != 1 || tabs[0].URL != "https://keep.example/" {
		t.Errorf("Tabs() = %+v, want only the https page", tabs)
	}
}

// A page still loading, a PDF, or a download has no title. Dropping the tab
// would hide something the user can see in their own tab strip, and an empty
// Label would render as a blank row in the picker, so the URL stands in.
func TestATabWithNoTitleFallsBackToItsURL(t *testing.T) {
	serveTabs(t, `[{"type":"page","title":"   ","url":"https://untitled.example/paper.pdf"}]`)

	tabs, err := Tabs(context.Background())
	if err != nil {
		t.Fatalf("Tabs() error = %v", err)
	}
	if len(tabs) != 1 {
		t.Fatalf("Tabs() = %d tabs, want 1", len(tabs))
	}
	if tabs[0].Title != "https://untitled.example/paper.pdf" {
		t.Errorf("Title = %q, want the URL to stand in for the missing title", tabs[0].Title)
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
