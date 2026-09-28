package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExportWritesPlaintextJSONAndWarns(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "--title", "A", "https://a.example"}) })
	path := filepath.Join(t.TempDir(), "out.json")

	_, stderr := bothStreams(t, func() {
		if err := runExport([]string{path}); err != nil {
			t.Fatalf("runExport() error = %v", err)
		}
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "https://a.example") {
		t.Errorf("export = %s, want the bookmark URL in plaintext", data)
	}
	if !strings.Contains(string(data), `"version"`) {
		t.Errorf("export = %s, want a version field", data)
	}
	// The name of this test promises the warning, so it asserts it. An
	// unencrypted copy of the whole vault is the one artifact this tool has to
	// be loud about, and the warning is a diagnostic, so it belongs on errOut
	// where it cannot be mistaken for part of the export.
	if !strings.Contains(stderr, "UNENCRYPTED") {
		t.Errorf("stderr = %q, want an unmissable plaintext warning", stderr)
	}
}

// os.WriteFile is handed 0o600, which on Windows is not a thing the filesystem
// records, so the assertion is POSIX-only. It is worth having anyway: this file
// is every URL, title and note the vault holds, in the clear.
func TestExportWritesAnOwnerOnlyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not carry POSIX permission bits")
	}
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://a.example"}) })
	path := filepath.Join(t.TempDir(), "out.json")

	bothStreams(t, func() {
		if err := runExport([]string{path}); err != nil {
			t.Fatalf("runExport() error = %v", err)
		}
	})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("mode = %v, want no group or world access", perm)
	}
}

func TestExportToStdoutWhenNoFileIsGiven(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://stdout.example"}) })

	got, stderr := bothStreams(t, func() {
		if err := runExport(nil); err != nil {
			t.Fatalf("runExport() error = %v", err)
		}
	})
	if !strings.Contains(got, "https://stdout.example") {
		t.Errorf("runExport() stdout = %q, want the bookmark", got)
	}
	// Piping the export somewhere does not make it any less plaintext, so the
	// warning is printed here too - and on errOut, so that it does not end up
	// inside the JSON the pipe is carrying.
	if !strings.Contains(stderr, "UNENCRYPTED") {
		t.Errorf("stderr = %q, want an unmissable plaintext warning", stderr)
	}
	if strings.Contains(got, "UNENCRYPTED") {
		t.Errorf("stdout = %q, want the warning kept out of the JSON", got)
	}
}

func TestImportReadsPlaintextJSON(t *testing.T) {
	newVaultForTest(t, "pw")
	path := filepath.Join(t.TempDir(), "in.json")
	body := `{"version":1,"bookmarks":[
	  {"id":"zzzzzzzz","url":"https://imported.example/one","title":"One","tags":["imported"],"added":"2026-01-01T00:00:00Z"},
	  {"id":"yyyyyyyy","url":"https://imported.example/two","added":"2026-01-02T00:00:00Z"}
	]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	got := capture(t, func() {
		if err := runImport([]string{path}); err != nil {
			t.Fatalf("runImport() error = %v", err)
		}
	})
	if !strings.Contains(got, "2") {
		t.Errorf("runImport() = %q, want it to report two imported bookmarks", got)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 2 {
		t.Fatalf("len(Bookmarks) = %d, want 2", len(c.Bookmarks))
	}
}

// An id in the file is a claim by whoever wrote the file. Two entries carrying
// the same one would leave the vault with two bookmarks sharing an id, and Find
// and Delete would both resolve to the first of them.
func TestImportMintsFreshIDs(t *testing.T) {
	newVaultForTest(t, "pw")
	path := filepath.Join(t.TempDir(), "in.json")
	body := `{"version":1,"bookmarks":[
	  {"id":"samesame","url":"https://imported.example/one","added":"2026-01-01T00:00:00Z"},
	  {"id":"samesame","url":"https://imported.example/two","added":"2026-01-02T00:00:00Z"}
	]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	capture(t, func() {
		if err := runImport([]string{path}); err != nil {
			t.Fatalf("runImport() error = %v", err)
		}
	})

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 2 {
		t.Fatalf("len(Bookmarks) = %d, want 2", len(c.Bookmarks))
	}
	if a, b := c.Bookmarks[0].ID, c.Bookmarks[1].ID; a == b {
		t.Errorf("both bookmarks have id %q, want two distinct fresh ids", a)
	}
	for _, b := range c.Bookmarks {
		if b.ID == "samesame" {
			t.Errorf("id = %q, want the id from the file discarded", b.ID)
		}
	}
}

func TestImportDeduplicatesAgainstExistingBookmarks(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "-t", "mine", "https://shared.example"}) })

	path := filepath.Join(t.TempDir(), "in.json")
	body := `{"version":1,"bookmarks":[{"id":"zzzzzzzz","url":"https://shared.example","tags":["theirs"],"added":"2026-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	got := capture(t, func() {
		if err := runImport([]string{path}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(got, "merged 1") {
		t.Errorf("runImport() = %q, want it to report one merge and no new bookmarks", got)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 1 {
		t.Fatalf("len(Bookmarks) = %d, want 1 after importing a duplicate", len(c.Bookmarks))
	}
	if strings.Join(c.Bookmarks[0].Tags, ",") != "mine,theirs" {
		t.Errorf("Tags = %v, want the tags merged", c.Bookmarks[0].Tags)
	}
}

func TestImportReadsBrowserBookmarksHTML(t *testing.T) {
	newVaultForTest(t, "pw")
	path := filepath.Join(t.TempDir(), "bookmarks.html")
	body := `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<DL><p>
  <DT><A HREF="https://html.example/one" ADD_DATE="1700000000">First &amp; Best</A>
  <DT><A HREF="https://html.example/two">Second</A>
  <DT><A HREF="javascript:void(0)">A Bookmarklet</A>
</DL>`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr := bothStreams(t, func() {
		if err := runImport([]string{path}); err != nil {
			t.Fatalf("runImport() error = %v", err)
		}
	})
	// Skipping is reported rather than silent: what bkmr will not store is
	// decided in one place, model.NormalizeURL, and counted in one place,
	// Collection.AddAll.
	if !strings.Contains(stderr, "skipped 1") {
		t.Errorf("stderr = %q, want the bookmarklet reported as skipped", stderr)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 2 {
		t.Fatalf("len(Bookmarks) = %d, want 2 - the bookmarklet must be skipped", len(c.Bookmarks))
	}
	var titles []string
	for _, b := range c.Bookmarks {
		titles = append(titles, b.Title)
	}
	joined := strings.Join(titles, "|")
	if !strings.Contains(joined, "First & Best") {
		t.Errorf("titles = %q, want the entity-decoded title", joined)
	}
}

// A browser export is a file somebody else wrote, and its titles are page text
// the page itself chose. This is the highest-risk way attacker-influenced text
// gets into the vault, and from there onto a terminal. The fixture is built with
// a double-quoted string on purpose: a raw string would store the four
// characters of "\x1b" instead of an escape, and the test would pass while
// proving nothing.
func TestImportStripsControlCharactersFromHTMLTitles(t *testing.T) {
	newVaultForTest(t, "pw")
	path := filepath.Join(t.TempDir(), "bookmarks.html")
	body := "<!DOCTYPE NETSCAPE-Bookmark-file-1>\n<DL><p>\n" +
		"  <DT><A HREF=\"https://esc.example/one\">Red \x1b[31mThing\x07</A>\n" +
		"</DL>"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	capture(t, func() {
		if err := runImport([]string{path}); err != nil {
			t.Fatalf("runImport() error = %v", err)
		}
	})

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 1 {
		t.Fatalf("len(Bookmarks) = %d, want 1", len(c.Bookmarks))
	}
	if want := "Red [31mThing"; c.Bookmarks[0].Title != want {
		t.Errorf("Title = %q, want %q", c.Bookmarks[0].Title, want)
	}
}

func TestImportOfAMissingFileFails(t *testing.T) {
	newVaultForTest(t, "pw")

	if err := runImport([]string{filepath.Join(t.TempDir(), "nope.json")}); err == nil {
		t.Error("runImport() error = nil for a missing file, want an error")
	}
}

// A file that is neither JSON nor a bookmark export is a mistake, not an empty
// import: saying so beats reporting that nothing happened, and the vault must be
// left exactly as it was.
func TestImportOfAnUnrecognizedFileFailsAndChangesNothing(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://a.example"}) })
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("just some prose about bookmarks\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := runImport([]string{path}); err == nil {
		t.Error("runImport() error = nil for an unrecognized file, want an error")
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 1 {
		t.Errorf("len(Bookmarks) = %d, want the vault untouched", len(c.Bookmarks))
	}
}

// A URL is attacker-authored on exactly this path, and a stored URL reaches a
// terminal raw - through ls, and through label() for every titleless bookmark,
// which an HTML import produces for any anchor with no text. NormalizeURL
// refuses a URL carrying a control character, so such an entry never enters the
// vault at all, and AddAll counts it so the user is told.
//
// Three anchors, one storable. The second is the interesting one: "&#27;" is
// pure ASCII in the file, and html.UnescapeString turns it into a real ESC, so a
// plain-looking bookmark export can manufacture a control character out of
// nothing. The third carries a literal C1 CSI, which is the spelling net/url
// used to let through.
func TestImportSkipsHTMLHrefsCarryingControlCharacters(t *testing.T) {
	newVaultForTest(t, "pw")
	path := filepath.Join(t.TempDir(), "bookmarks.html")
	// U+009B is built from its code point rather than written as an escape. This
	// project has already been bitten once by tooling that rewrote a \u escape in
	// committed source into the control byte it denotes; string(rune(...)) cannot
	// be misread that way, and it is unambiguous about what is intended.
	csi := string(rune(0x9b))
	body := "<!DOCTYPE NETSCAPE-Bookmark-file-1>\n<DL><p>\n" +
		"  <DT><A HREF=\"https://fine.example/one\">Fine</A>\n" +
		"  <DT><A HREF=\"https://evil.example/a&#27;[31mZ\"></A>\n" +
		"  <DT><A HREF=\"https://evil.example/b" + csi + "2JZ\"></A>\n" +
		"</DL>"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := bothStreams(t, func() {
		if err := runImport([]string{path}); err != nil {
			t.Fatalf("runImport() error = %v", err)
		}
	})
	if !strings.Contains(stdout, "imported 1 new") {
		t.Errorf("stdout = %q, want one bookmark imported", stdout)
	}
	if !strings.Contains(stderr, "skipped 2") {
		t.Errorf("stderr = %q, want two entries reported as skipped", stderr)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 1 {
		t.Fatalf("len(Bookmarks) = %d, want 1", len(c.Bookmarks))
	}
	if got := c.Bookmarks[0].URL; got != "https://fine.example/one" {
		t.Errorf("URL = %q, want only the clean one stored", got)
	}
}

// The JSON half of the same threat: an exported vault somebody else edited. Also
// the proof that refusing beats cleaning - no URL in the vault was quietly
// rewritten to point somewhere else, because none of these was stored at all.
func TestImportSkipsJSONURLsCarryingControlCharacters(t *testing.T) {
	newVaultForTest(t, "pw")
	path := filepath.Join(t.TempDir(), "in.json")
	// esc is one backslash and a u; the four hex digits are appended below, so
	// that this file never holds a complete JSON escape for a control character.
	// The point of the fixture is that the *file* carries the escape and the
	// decoded URL carries the control character - if the source were rewritten
	// into a raw byte, encoding/json would reject the fixture outright and the
	// test would be proving something else entirely.
	esc := `\u`
	body := `{"version":1,"bookmarks":[
	  {"id":"a","url":"https://fine.example/one","added":"2026-01-01T00:00:00Z"},
	  {"id":"b","url":"https://evil.example/a` + esc + `009b2JZ","added":"2026-01-02T00:00:00Z"},
	  {"id":"c","url":"https://evil.example/b` + esc + `001b[31mZ","added":"2026-01-03T00:00:00Z"}
	]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := bothStreams(t, func() {
		if err := runImport([]string{path}); err != nil {
			t.Fatalf("runImport() error = %v", err)
		}
	})
	if !strings.Contains(stdout, "imported 1 new") {
		t.Errorf("stdout = %q, want one bookmark imported", stdout)
	}
	if !strings.Contains(stderr, "skipped 2") {
		t.Errorf("stderr = %q, want two entries reported as skipped", stderr)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 1 || c.Bookmarks[0].URL != "https://fine.example/one" {
		t.Fatalf("Bookmarks = %+v, want only the clean one", c.Bookmarks)
	}
	for _, b := range c.Bookmarks {
		for _, r := range b.URL {
			if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
				t.Errorf("URL = %q still contains U+%04X", b.URL, r)
			}
		}
	}
}

// os.WriteFile sets a mode only when it creates the file, so exporting over an
// existing dump.json left a world-readable file world-readable and filled it
// with every URL, title and note in the clear. O_EXCL refuses instead: for a
// plaintext copy of an encrypted vault, not overwriting is the right default,
// and it also saves an unrelated file from a typo.
func TestExportRefusesToOverwriteAnExistingFile(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://secret.example"}) })

	path := filepath.Join(t.TempDir(), "dump.json")
	const existing = "a file that was already here\n"
	if err := os.WriteFile(path, []byte(existing), 0o666); err != nil {
		t.Fatal(err)
	}

	var err error
	stdout, _ := bothStreams(t, func() { err = runExport([]string{path}) })
	if err == nil {
		t.Fatal("runExport() error = nil, want a refusal to overwrite")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error = %v, want it to name the path", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != existing {
		t.Errorf("file = %q, want it untouched", data)
	}
	if strings.Contains(string(data), "secret.example") {
		t.Error("the refused export wrote the vault into the existing file")
	}
}

// Single-quoted href attributes were dropped without a word, which contradicts
// the one principle this file states about importing: whether a URL is one bkmr
// stores is NormalizeURL's decision, and an entry it rejects is counted and
// reported rather than vanishing. An anchor the pattern never matched was not
// rejected - it was never seen, so it was not even in the skipped count.
//
// Single quotes are legal HTML and real exporters emit them; anything that
// serializes a bookmark file through a templating layer may. Four anchors here,
// two of each quoting style, and all four have to arrive.
func TestImportAcceptsSingleQuotedHrefs(t *testing.T) {
	newVaultForTest(t, "pw")
	path := filepath.Join(t.TempDir(), "bookmarks.html")
	body := "<!DOCTYPE NETSCAPE-Bookmark-file-1>\n<DL><p>\n" +
		"  <DT><A HREF=\"https://double.example/one\" ADD_DATE=\"1\">Double One</A>\n" +
		"  <DT><A HREF='https://single.example/two' ADD_DATE='2'>Single Two</A>\n" +
		"  <DT><A ADD_DATE='3' HREF='https://single.example/three'>Single Three</A>\n" +
		"  <DT><A HREF=\"https://double.example/four\">Double Four</A>\n" +
		"</DL>"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := bothStreams(t, func() {
		if err := runImport([]string{path}); err != nil {
			t.Fatalf("runImport() error = %v", err)
		}
	})
	if !strings.Contains(stdout, "imported 4 new") {
		t.Errorf("stdout = %q, want all four anchors imported", stdout)
	}
	if strings.Contains(stderr, "skipped") {
		t.Errorf("stderr = %q, want nothing skipped", stderr)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 4 {
		t.Fatalf("len(Bookmarks) = %d, want 4 - a single-quoted href must not be silently lost", len(c.Bookmarks))
	}
	titles := map[string]string{}
	for _, b := range c.Bookmarks {
		titles[b.URL] = b.Title
	}
	for url, want := range map[string]string{
		"https://double.example/one":   "Double One",
		"https://single.example/two":   "Single Two",
		"https://single.example/three": "Single Three",
		"https://double.example/four":  "Double Four",
	} {
		if got, ok := titles[url]; !ok {
			t.Errorf("%s is missing from the vault", url)
		} else if got != want {
			t.Errorf("%s title = %q, want %q", url, got, want)
		}
	}
}
