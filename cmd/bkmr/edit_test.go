package main

import (
	"strings"
	"testing"
)

func idOfFirst(t *testing.T) string {
	t.Helper()
	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) == 0 {
		t.Fatal("no bookmarks in the vault")
	}
	return c.Bookmarks[0].ID
}

func TestEditReplacesTitleNoteAndTags(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "-t", "old", "https://a.example"}) })
	id := idOfFirst(t)

	capture(t, func() {
		if err := runEdit([]string{"--title", "New Title", "--note", "new note", "-t", "fresh", id}); err != nil {
			t.Fatalf("runEdit() error = %v", err)
		}
	})

	v, _ := openVault()
	c, _ := v.Load()
	b := c.Bookmarks[0]
	if b.Title != "New Title" {
		t.Errorf("Title = %q, want %q", b.Title, "New Title")
	}
	if b.Notes != "new note" {
		t.Errorf("Notes = %q, want %q", b.Notes, "new note")
	}
	if strings.Join(b.Tags, ",") != "fresh" {
		t.Errorf("Tags = %v, want [fresh] - tags given to edit replace rather than merge", b.Tags)
	}
}

func TestEditLeavesUnspecifiedFieldsAlone(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "--title", "Keep", "-t", "keep", "https://a.example"}) })
	id := idOfFirst(t)

	capture(t, func() {
		if err := runEdit([]string{"--note", "only the note", id}); err != nil {
			t.Fatal(err)
		}
	})

	v, _ := openVault()
	c, _ := v.Load()
	if c.Bookmarks[0].Title != "Keep" {
		t.Errorf("Title = %q, want it untouched", c.Bookmarks[0].Title)
	}
	if strings.Join(c.Bookmarks[0].Tags, ",") != "keep" {
		t.Errorf("Tags = %v, want them untouched", c.Bookmarks[0].Tags)
	}
}

func TestEditUnknownIDFails(t *testing.T) {
	newVaultForTest(t, "pw")

	if err := runEdit([]string{"--title", "x", "nosuchid"}); err == nil {
		t.Error("runEdit() error = nil for an unknown id, want an error")
	}
}

// Titles and notes reach a terminal - through ls, through the picker, through
// the line edit itself prints - and Collection.Add is not on this path: edit
// writes through the *Bookmark that Find hands out. An ESC in either field is a
// terminal-injection vector, so both have to come out the far side clean.
func TestEditStripsControlCharactersFromTitleAndNote(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://a.example"}) })
	id := idOfFirst(t)

	got := capture(t, func() {
		if err := runEdit([]string{"--title", "Red \x1b[31mThing", "--note", "note \x1b]0;pwned\x07here", id}); err != nil {
			t.Fatalf("runEdit() error = %v", err)
		}
	})
	if strings.ContainsRune(got, 0x1b) {
		t.Errorf("runEdit() printed %q, want no escape characters", got)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if want := "Red [31mThing"; c.Bookmarks[0].Title != want {
		t.Errorf("Title = %q, want %q", c.Bookmarks[0].Title, want)
	}
	if want := "note ]0;pwnedhere"; c.Bookmarks[0].Notes != want {
		t.Errorf("Notes = %q, want %q", c.Bookmarks[0].Notes, want)
	}
}

// Nothing to change is a mistake worth naming rather than a silent rewrite of
// the vault: the fields are all optional, so 'bkmr edit <id>' with none of them
// would otherwise re-encrypt the whole file to no effect.
func TestEditWithNothingToChangeIsAUsageError(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "--title", "Keep", "https://a.example"}) })
	id := idOfFirst(t)

	var err error
	stdout, stderr := bothStreams(t, func() { err = runEdit([]string{id}) })
	if err != errUsage {
		t.Errorf("runEdit() error = %v, want the bare errUsage sentinel", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if stderr == "" {
		t.Error("stderr = \"\", want a line saying what was wrong")
	}
}

func TestRmDeletesAfterConfirmation(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://a.example"}) })
	id := idOfFirst(t)

	old := confirm
	confirm = func(string) (bool, error) { return true, nil }
	defer func() { confirm = old }()

	capture(t, func() {
		if err := runRm([]string{id}); err != nil {
			t.Fatalf("runRm() error = %v", err)
		}
	})

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 0 {
		t.Errorf("len(Bookmarks) = %d, want 0", len(c.Bookmarks))
	}
}

func TestRmKeepsTheBookmarkWhenDeclined(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://a.example"}) })
	id := idOfFirst(t)

	old := confirm
	confirm = func(string) (bool, error) { return false, nil }
	defer func() { confirm = old }()

	got := capture(t, func() {
		if err := runRm([]string{id}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(got, "kept") {
		t.Errorf("runRm() = %q, want it to say the bookmark was kept", got)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 1 {
		t.Errorf("len(Bookmarks) = %d, want 1 - declining must keep the bookmark", len(c.Bookmarks))
	}
}

func TestRmWithForceSkipsTheConfirmation(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://a.example"}) })
	id := idOfFirst(t)

	old := confirm
	confirm = func(string) (bool, error) { t.Fatal("--force must not ask"); return false, nil }
	defer func() { confirm = old }()

	capture(t, func() {
		if err := runRm([]string{"--force", id}); err != nil {
			t.Fatal(err)
		}
	})
}

// An id that does not resolve must be refused before the confirmation, so the
// user is never asked about a bookmark that was never going to be deleted, and
// the vault is not rewritten either.
func TestRmUnknownIDFailsWithoutAskingOrWriting(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://a.example"}) })

	old := confirm
	confirm = func(string) (bool, error) {
		t.Fatal("an unknown id must not reach the confirmation")
		return false, nil
	}
	defer func() { confirm = old }()

	if err := runRm([]string{"nosuchid"}); err == nil {
		t.Error("runRm() error = nil for an unknown id, want an error")
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 1 {
		t.Errorf("len(Bookmarks) = %d, want the vault untouched", len(c.Bookmarks))
	}
}

// An id is required, and both commands reach their arity check with zero
// positionals as easily as with two - 'bkmr edit --title x' is a plausible typo.
func TestEditWithNoIDIsAUsageError(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "--title", "Keep", "https://a.example"}) })

	var err error
	stdout, stderr := bothStreams(t, func() { err = runEdit([]string{"--title", "x"}) })
	if err != errUsage {
		t.Errorf("runEdit() error = %v, want the bare errUsage sentinel", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if !strings.Contains(stderr, "one bookmark id") {
		t.Errorf("stderr = %q, want it to say an id is required", stderr)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if c.Bookmarks[0].Title != "Keep" {
		t.Errorf("Title = %q, want it untouched", c.Bookmarks[0].Title)
	}
}

func TestRmWithNoIDIsAUsageError(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://a.example"}) })

	old := confirm
	confirm = func(string) (bool, error) {
		t.Fatal("a usage error must not reach the confirmation")
		return false, nil
	}
	defer func() { confirm = old }()

	var err error
	stdout, stderr := bothStreams(t, func() { err = runRm([]string{"--force"}) })
	if err != errUsage {
		t.Errorf("runRm() error = %v, want the bare errUsage sentinel", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if !strings.Contains(stderr, "one bookmark id") {
		t.Errorf("stderr = %q, want it to say an id is required", stderr)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 1 {
		t.Errorf("len(Bookmarks) = %d, want the vault untouched", len(c.Bookmarks))
	}
}
