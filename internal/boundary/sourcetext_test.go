package boundary

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// sourceTextExtensions are the files this project's own tooling writes, and so
// the files that can arrive with a decoded escape sequence in them.
//
// Not only .go. The rationale below is that a tool writing source decoded a
// \u009b-style escape into the real byte - and that same tooling wrote
// README.md, CONTRIBUTING.md, SECURITY.md and the CI workflow. A control
// character in a markdown file is worse than in a comment, not better: anyone
// who cats the README gets it, and GitHub renders the file without complaint.
// YAML is here because a raw control byte in a workflow is both invisible in
// review and capable of changing what a shell step runs.
var sourceTextExtensions = map[string]bool{
	".go":   true,
	".md":   true,
	".yml":  true,
	".yaml": true,
}

// TestNoLiteralControlCharactersInSourceText fails if any of the module's own
// source or documentation files contains a control character as a literal rune.
//
// This is not a style rule. Twice during this project's construction a tool
// writing Go source decoded a \u009b-style escape sequence into the real
// control character it names and committed the result - once inside a comment,
// where the compiler, gofmt, go vet and staticcheck were all perfectly happy
// and the only symptom was a terminal that started interpreting the file as
// escape sequences. A byte like that in a string literal changes what the
// program does; in a comment it changes nothing and so nothing catches it. This
// test catches both.
//
// Control characters that a program or a document legitimately needs are written
// as escapes - "\x1b[2J", '\u009b' - which are ASCII source text and pass here.
// The rule is about the literal byte, never about the concept.
func TestNoLiteralControlCharactersInSourceText(t *testing.T) {
	root := moduleRoot(t)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Nothing under .git is source, and packed objects are full of
			// arbitrary bytes. vendor/ is somebody else's code: if anyone ever
			// runs go mod vendor, one upstream file with an embedded control
			// byte would turn this project's own guard red, and the fix would
			// not be ours to make.
			//
			// .superpowers is the third skip and the only judgement call among
			// them. It holds the session records this project was built from, and
			// several of those reports quote the exact byte this guard is about -
			// they are transcripts of finding it, so they contain it. They are not
			// source, nothing ships them, and editing a past report to satisfy a
			// present test would be rewriting the record rather than fixing
			// anything. Everything the project actually publishes - README.md,
			// CONTRIBUTING.md, SECURITY.md, docs/, the workflows - is checked.
			if d.Name() == ".git" || d.Name() == "vendor" || d.Name() == ".superpowers" {
				return fs.SkipDir
			}
			return nil
		}
		if !sourceTextExtensions[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		if !utf8.Valid(data) {
			t.Errorf("%s is not valid UTF-8; this project's source and documentation must be", rel)
			return nil
		}
		line := 1
		for _, r := range string(data) {
			if r == '\n' {
				line++
				continue
			}
			// Tab is legal indentation and gofmt writes it; it is also legal in
			// markdown and YAML. Carriage return is left alone here because line
			// endings are .gitattributes' job and a CRLF checkout must not fail a
			// test about escape sequences.
			if r == '\t' || r == '\r' {
				continue
			}
			if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
				t.Errorf("%s:%d contains the literal control character U+%04X. "+
					"Write it as an escape sequence (\"\\u%04X\") instead - a raw control byte in source is "+
					"invisible in review, and in a comment nothing else in the toolchain will ever complain about it.",
					rel, line, r, r)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}
