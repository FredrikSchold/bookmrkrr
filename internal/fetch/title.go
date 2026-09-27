// Package fetch is the only package in BookMrkr that makes outbound network
// requests, and it makes exactly one kind: a plain GET to read a page's
// <title>. Everything here is deliberately narrow so the whole network
// surface of the tool can be reviewed in one file.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	// MaxTitleRunes caps a title so a hostile page cannot bloat the vault.
	MaxTitleRunes = 300
	// maxBodyBytes stops a page streaming megabytes at us. A <title> that is
	// not in the first 64 KiB is not worth the bandwidth or the memory.
	maxBodyBytes = 64 << 10
	// maxRedirects bounds a redirect loop. Three is generous for the one hop a
	// canonical URL normally needs.
	maxRedirects = 3
	// userAgent identifies the tool and nothing about the user: no version of
	// the OS, no locale, no browser string to blend into. It is a constant so
	// there is no per-user variation to fingerprint, and it is not
	// configurable, because a configurable one is a place to leak identity.
	userAgent = "bkmr/0.1 (+https://github.com/FredrikSchold/bookmrkrr)"
)

// Title fetches rawURL and returns its page title, or an empty string when the
// page has no usable title. Callers must treat an error as cosmetic: a
// bookmark is never lost because a title could not be read.
func Title(ctx context.Context, rawURL string) (string, error) {
	client := &http.Client{
		// No cookie jar: nil Jar means no cookies are ever sent or stored, so
		// a fetch cannot carry a session the user has with the site and cannot
		// leave one behind for the next fetch to carry.
		Jar:           nil,
		CheckRedirect: sameSite,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("%s returned %s", rawURL, resp.Status)
	}
	// A declared non-HTML body has no <title> worth trusting, and reading a PDF
	// or a video to look for one would be pure waste. Not an error: the page
	// was reachable, it simply has no title.
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "text/html") {
		return "", nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return titleFrom(string(body)), nil
}

// sameSite is the whole redirect policy: bounded, and never off the site the
// user asked for. Refusing a site change keeps one deliberate request from
// turning into a request to a third party the user never named - a tracker, an
// ad host, an internal address on their own network. It compares against via[0]
// rather than the previous hop so a chain cannot walk away one host at a time.
//
// "Site" tolerates exactly one difference: a leading www. on either side, so
// that the ordinary example.com -> www.example.com hop (and its reverse) is
// followed. Everything else is still refused, and should stay refused:
//
//   - any other subdomain - cdn.example.com, login.example.com - because a page
//     title should come from the page, and a redirect to a sibling host is the
//     shape a tracker or an SSO bounce takes;
//   - a port change, which is why the comparison keeps the port;
//   - a different registrable domain.
//
// Widening this to "same registrable domain" would mean a public-suffix list to
// tell example.co.uk from co.uk, and therefore another dependency. Not worth it
// for a page title.
//
// The scheme is deliberately not compared: an http:// to https:// upgrade on the
// same host is an improvement, not a redirection elsewhere.
func sameSite(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	if canonicalHost(req.URL) != canonicalHost(via[0].URL) {
		return fmt.Errorf("refusing to follow a redirect off %s to %s", via[0].URL.Host, req.URL.Host)
	}
	return nil
}

// canonicalHost is the identity a redirect has to preserve: the host with one
// leading www. removed, and the port kept.
func canonicalHost(u *url.URL) string {
	return strings.TrimPrefix(u.Hostname(), "www.") + ":" + u.Port()
}

// titleFrom extracts and cleans the contents of the first <title> element.
//
// ponytail: a string scan, not an HTML parser. Upgrade to x/net/html only if a
// real page defeats it - that would also mean another dependency.
func titleFrom(body string) string {
	low := strings.ToLower(body)
	open := strings.Index(low, "<title")
	if open < 0 {
		return ""
	}
	gt := strings.Index(low[open:], ">")
	if gt < 0 {
		return ""
	}
	start := open + gt + 1
	end := strings.Index(low[start:], "</title>")
	if end < 0 {
		return ""
	}

	// Fields collapses every run of whitespace, newlines and tabs included, so
	// a title cannot smuggle line breaks into the listing. The cap counts runes
	// rather than bytes so a multi-byte title is not cut mid-character.
	title := strings.Join(strings.Fields(html.UnescapeString(body[start:start+end])), " ")
	if r := []rune(title); len(r) > MaxTitleRunes {
		title = strings.TrimSpace(string(r[:MaxTitleRunes]))
	}
	return title
}
