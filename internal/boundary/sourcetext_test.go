package boundary

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestNoLiteralControlCharactersInGoSource fails if any .go file in the module
// contains a control character as a literal rune.
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
// Control characters that a Go program legitimately needs are written as
// escapes - "\x1b[2J", '\u009b' - which are ASCII source text and pass here.
// The rule is about the literal byte, never about the concept.
func TestNoLiteralControlCharactersInGoSource(t *testing.T) {
	root := moduleRoot(t)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Nothing under .git is source, and packed objects are full of
			// arbitrary bytes.
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
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
			t.Errorf("%s is not valid UTF-8; Go source must be", rel)
			return nil
		}
		line := 1
		for _, r := range string(data) {
			if r == '\n' {
				line++
				continue
			}
			// Tab is legal indentation and gofmt writes it. Carriage return is
			// left alone here because line endings are .gitattributes' job and a
			// CRLF checkout must not fail a test about escape sequences.
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
