package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTitleReadsTheTitleElement(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html><head><TITLE>Hello &amp; Goodbye</TITLE></head><body>x</body></html>"))
	}))
	defer srv.Close()

	got, err := Title(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Title() error = %v", err)
	}
	if got != "Hello & Goodbye" {
		t.Errorf("Title() = %q, want %q", got, "Hello & Goodbye")
	}
}

func TestTitleSendsNoCookiesAndAGenericUserAgent(t *testing.T) {
	var gotUA string
	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotCookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<title>t</title>"))
	}))
	defer srv.Close()

	if _, err := Title(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(gotUA, "bkmr/") {
		t.Errorf("User-Agent = %q, want it to start with bkmr/", gotUA)
	}
	if gotCookie != "" {
		t.Errorf("Cookie = %q, want no cookie header", gotCookie)
	}
}

// Review Focus 3: a hostile or broken title.
func TestTitleCollapsesWhitespaceAndTruncates(t *testing.T) {
	long := strings.Repeat("ab ", 500)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<title>\n\t  " + long + "  \n</title>"))
	}))
	defer srv.Close()

	got, err := Title(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Title() error = %v", err)
	}
	if strings.Contains(got, "\n") || strings.Contains(got, "\t") {
		t.Errorf("Title() = %q, want newlines and tabs collapsed", got)
	}
	if n := len([]rune(got)); n > MaxTitleRunes {
		t.Errorf("Title() is %d runes, want at most %d", n, MaxTitleRunes)
	}
	if strings.Contains(got, "  ") {
		t.Errorf("Title() = %q, want runs of whitespace collapsed", got)
	}
}

func TestTitleWithNoClosingTagReturnsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><head><title>never closed"))
	}))
	defer srv.Close()

	got, err := Title(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Title() error = %v", err)
	}
	if got != "" {
		t.Errorf("Title() = %q, want an empty string", got)
	}
}

func TestTitleSkipsNonHTMLContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-1.4 <title>not really</title>"))
	}))
	defer srv.Close()

	got, err := Title(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Title() error = %v", err)
	}
	if got != "" {
		t.Errorf("Title() = %q, want an empty string for non-HTML", got)
	}
}

func TestTitleStopsReadingAfterTheByteCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><head>"))
		w.Write([]byte(strings.Repeat("<!-- padding -->", 8000))) // well past 64 KiB
		w.Write([]byte("<title>too late</title>"))
	}))
	defer srv.Close()

	got, err := Title(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Title() error = %v", err)
	}
	if got != "" {
		t.Errorf("Title() = %q, want an empty string once past the byte cap", got)
	}
}

func TestTitleRefusesACrossHostRedirect(t *testing.T) {
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<title>other host</title>"))
	}))
	defer elsewhere.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusFound)
	}))
	defer srv.Close()

	if _, err := Title(context.Background(), srv.URL); err == nil {
		t.Error("Title() error = nil, want a refusal to follow a cross-host redirect")
	}
}

func TestTitleFollowsASameHostRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/end", http.StatusFound)
	})
	mux.HandleFunc("/end", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<title>arrived</title>"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got, err := Title(context.Background(), srv.URL+"/start")
	if err != nil {
		t.Fatalf("Title() error = %v", err)
	}
	if got != "arrived" {
		t.Errorf("Title() = %q, want %q", got, "arrived")
	}
}

// The two httptest servers above can only ever differ by port, so the policy's
// host rules are asserted here against the predicate itself. www.example.com and
// example.com cannot both be stood up locally, and a title fetch that gives up
// on an apex-to-www hop would look broken on a large share of the web.
func TestSameSiteAllowsAWwwHopAndNothingElse(t *testing.T) {
	cases := []struct {
		name    string
		from    string
		to      string
		wantErr bool
	}{
		{"apex to www", "https://example.com/a", "https://www.example.com/a", false},
		{"www to apex", "https://www.example.com/a", "https://example.com/a", false},
		{"an http to https upgrade on the same host", "http://example.com/a", "https://example.com/a", false},
		{"a path change on the same host", "https://example.com/a", "https://example.com/b", false},
		{"another subdomain", "https://example.com/a", "https://cdn.example.com/a", true},
		{"a subdomain of www", "https://www.example.com/a", "https://cdn.example.com/a", true},
		{"a different registrable domain", "https://example.com/a", "https://example.test/a", true},
		{"a different port on the same host", "http://127.0.0.1:1/a", "http://127.0.0.1:2/a", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			via, err := http.NewRequest(http.MethodGet, tc.from, nil)
			if err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequest(http.MethodGet, tc.to, nil)
			if err != nil {
				t.Fatal(err)
			}
			err = sameSite(req, []*http.Request{via})
			if tc.wantErr && err == nil {
				t.Errorf("sameSite(%s -> %s) = nil, want a refusal", tc.from, tc.to)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("sameSite(%s -> %s) = %v, want it followed", tc.from, tc.to, err)
			}
		})
	}
}

func TestTitleRespectsAContextDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<title>slow</title>"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if _, err := Title(ctx, srv.URL); err == nil {
		t.Error("Title() error = nil, want a timeout")
	}
}

func TestTitleOnAServerErrorFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := Title(context.Background(), srv.URL); err == nil {
		t.Error("Title() error = nil, want an error on HTTP 500")
	}
}
