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

	capture(t, func() {
		if err := runImport([]string{path}); err != nil {
			t.Fatalf("runImport() error = %v", err)
		}
	})

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
