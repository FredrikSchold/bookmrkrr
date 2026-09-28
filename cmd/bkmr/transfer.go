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
	// 0o600: owner only. The vault itself is written with the same mode, and an
	// unencrypted copy of it has no business being more readable than the
	// original. On Windows this is not recorded by the filesystem, which is one
	// more reason the warning below is not optional.
	if err := os.WriteFile(args[0], data, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(out, "wrote %d bookmarks to %s\n", len(c.Bookmarks), args[0])
	// Last, so it is the line still on screen when the command finishes.
	fmt.Fprintln(errOut, plaintextWarning)
	fmt.Fprintf(errOut, "bkmr: delete %s when you are done with it\n", args[0])
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
		fmt.Fprintf(errOut, "bkmr: skipped %d entries whose URL was not http or https\n", skipped)
	}
	return nil
}

// hrefPattern matches one anchor in a Netscape bookmark file.
var hrefPattern = regexp.MustCompile(`(?is)<a\s+[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)

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
		href := html.UnescapeString(m[1])
		if _, err := model.NormalizeURL(href); err != nil {
			continue // skip bookmarklets, place: URLs, and other non-http entries
		}
		title := strings.Join(strings.Fields(html.UnescapeString(stripTags(m[2]))), " ")
		found = append(found, model.Bookmark{URL: href, Title: title})
	}
	if found == nil {
		return nil, fmt.Errorf("no bookmarks found - expected JSON or a browser HTML export")
	}
	return found, nil
}

var tagPattern = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string { return tagPattern.ReplaceAllString(s, "") }
