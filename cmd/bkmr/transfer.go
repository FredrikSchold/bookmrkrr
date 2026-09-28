package main

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"regexp"
	"strings"

	"github.com/FredrikSchold/bookmrkrr/internal/model"
)

func init() {
	register(command{
		Name:    "export",
		Summary: "write the vault as plaintext JSON (unencrypted!)",
		Usage:   "bkmr export [file]",
		Run:     runExport,
	})
	register(command{
		Name:    "import",
		Summary: "read bookmarks from plaintext JSON or a browser HTML export",
		Usage:   "bkmr import <file>",
		Run:     runImport,
	})
}

// plaintextWarning is the one wording of the loudest thing this tool says. An
// export is every URL, title and note in the vault, in the clear, and the whole
// point of the vault is that those are not lying around in the clear.
const plaintextWarning = "bkmr: this is UNENCRYPTED plaintext - every bookmark, title and note in the clear"

func runExport(args []string) error {
	if len(args) > 1 {
		fmt.Fprintln(errOut, "bkmr: export takes at most one file")
		return errUsage
	}
	v, err := openVault()
	if err != nil {
		return err
	}
	c, err := v.Load()
	if err != nil {
		return explainVaultError(err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if len(args) == 0 {
		// On errOut, and before the JSON, so that it is read rather than lost
		// under a long dump - and so that it cannot end up inside the JSON when
		// stdout is a pipe or a shell redirect.
		fmt.Fprintln(errOut, plaintextWarning)
		_, err := out.Write(data)
		return err
	}
	if err := writeNewFile(args[0], data); err != nil {
		return err
	}
	fmt.Fprintf(out, "wrote %d bookmarks to %s\n", len(c.Bookmarks), args[0])
	// Last, so it is the line still on screen when the command finishes.
	fmt.Fprintln(errOut, plaintextWarning)
	fmt.Fprintf(errOut, "bkmr: delete %s when you are done with it\n", args[0])
	return nil
}

// writeNewFile writes data to a file it creates, at 0o600, and refuses to
// overwrite anything already there.
//
// os.WriteFile would have been one line, but it sets a mode only when it creates
// the file: exporting over an existing world-readable dump.json would have left
// it world-readable and filled it with every URL, title and note in the clear.
// O_EXCL makes the refusal the same syscall as the create, so there is no gap in
// which the file could appear - the same reasoning as store.Create.
//
// Refusing rather than overwriting is also the right default on its own terms.
// This is a plaintext copy of an encrypted vault; a typo that names an unrelated
// file should not destroy it, and a little friction belongs on the one operation
// this tool is loudest about.
func writeNewFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%s already exists - remove it or choose another path; bkmr will not overwrite a file with a plaintext export", path)
		}
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		// A half-written plaintext export is the worst of both outcomes: it is
		// not a usable export and it is still a file full of bookmarks.
		os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}

func runImport(args []string) error {
	if len(args) != 1 {
		fmt.Fprintln(errOut, "bkmr: import takes exactly one file")
		return errUsage
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}

	incoming, err := parseImport(data)
	if err != nil {
		return err
	}
	if len(incoming) == 0 {
		fmt.Fprintln(out, "nothing to import")
		return nil
	}
	for i := range incoming {
		// Mint fresh ids. An id in the file is a claim by whoever wrote the
		// file, and nothing validates it: two entries carrying the same one
		// would leave the vault with two bookmarks sharing an id, and Find and
		// Delete would both resolve to the first of them - so 'bkmr rm' could
		// never reach the second. An empty id is what makes model mint one.
		incoming[i].ID = ""
	}

	v, err := openVault()
	if err != nil {
		return err
	}
	var added, merged, skipped int
	// AddAll rather than Add in a loop: Add normalizes every stored URL on every
	// insert, which for a twenty-thousand-entry browser export is quadratic and
	// takes minutes.
	if err := v.Mutate(func(c *model.Collection) error {
		added, merged, skipped = c.AddAll(incoming)
		return nil
	}); err != nil {
		return explainVaultError(err)
	}
	fmt.Fprintf(out, "imported %d new bookmarks, merged %d existing\n", added, merged)
	if skipped > 0 {
		// Dropping part of what somebody asked to import without saying so is
		// not acceptable, and it is a diagnostic rather than output.
		fmt.Fprintf(errOut, "bkmr: skipped %d entries whose URL bkmr will not store\n", skipped)
	}
	return nil
}

// hrefPattern matches one anchor in a Netscape bookmark file. Either quoting
// style, because both are legal HTML and anything that writes a bookmark file
// through a templating layer may emit single quotes. Matching only double quotes
// meant such an anchor was never seen at all - not rejected, not counted as
// skipped, just absent, which is the one thing parseImport's own comment says
// must not happen.
//
// Two capture groups for the URL, one per quoting style, and exactly one of them
// is ever non-empty for a given match. The inner classes are * rather than +
// deliberately: href="" is an anchor with no URL, and letting it match hands it
// to NormalizeURL to reject and to AddAll to count, rather than dropping it the
// silent way this change exists to stop.
var hrefPattern = regexp.MustCompile(`(?is)<a\s+[^>]*href=(?:"([^"]*)"|'([^']*)')[^>]*>(.*?)</a>`)

// parseImport reads either the JSON this tool exports or the Netscape bookmark
// HTML that every browser exports, chosen by the first character. Titles in the
// second are page text somebody else's site chose, which is why nothing here
// trusts them: they are entity-decoded and collapsed to one line, and the
// control characters come out on the way into the vault.
func parseImport(data []byte) ([]model.Bookmark, error) {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "{") {
		var c model.Collection
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("parse JSON: %w", err)
		}
		return c.Bookmarks, nil
	}

	var found []model.Bookmark
	for _, m := range hrefPattern.FindAllStringSubmatch(trimmed, -1) {
		// Every anchor is returned, bookmarklets and place: URLs included.
		// Whether a URL is one bkmr will store is model.NormalizeURL's decision
		// and nobody else's, and AddAll is where it gets made and counted - so a
		// skipped entry is reported to the user instead of vanishing here.
		// m[1] is the double-quoted href and m[2] the single-quoted one; the
		// alternation guarantees the other is empty.
		href := html.UnescapeString(m[1] + m[2])
		title := strings.Join(strings.Fields(html.UnescapeString(stripTags(m[3]))), " ")
		found = append(found, model.Bookmark{URL: href, Title: title})
	}
	if found == nil {
		return nil, fmt.Errorf("no bookmarks found - expected JSON or a browser HTML export")
	}
	return found, nil
}

var tagPattern = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string { return tagPattern.ReplaceAllString(s, "") }
