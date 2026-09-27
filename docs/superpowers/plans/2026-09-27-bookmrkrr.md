# BookMrkr Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `bkmr`, a terminal-first bookmark manager whose vault is encrypted at rest and unlocked by a local password.

**Architecture:** A single Go binary. `internal/crypto` seals one JSON document into an authenticated envelope; `internal/store` loads and atomically rewrites that file; `internal/model` owns the bookmark type and its normalization rules; `cmd/bkmr` is a flat dispatch table of subcommands; `internal/tui` is one reusable fuzzy picker serving both bookmark retrieval and tab capture. All outbound network lives in `internal/fetch` and `internal/capture/browser`, enforced by a test.

**Tech Stack:** Go 1.26+, `golang.org/x/crypto` (argon2id, XChaCha20-Poly1305, term), `bubbletea` + `bubbles/textinput` + `lipgloss`, `sahilm/fuzzy`, `atotto/clipboard`, `zalando/go-keyring`, `BurntSushi/toml`.

**Spec:** `docs/superpowers/specs/2026-09-27-bookmrkrr-design.md`

## Global Constraints

- Module path: `github.com/FredrikSchold/bookmrkrr`. Binary: `bkmr`. License: MIT.
- `go.mod` declares `go 1.26`. Everything must build with `CGO_ENABLED=0`.
  > **Amended during execution (Task 2):** the plan originally said `go 1.24`. Current `golang.org/x/crypto` requires a higher directive, and pinning the crypto library back to an older release to preserve a version floor is the wrong trade for a tool whose whole point is encryption — `govulncheck` runs in CI and CVE fixes land in the newest release. Users who cannot run Go 1.26 use the release binaries.
- **Nine direct dependencies, no more**: `bubbletea`, `bubbles`, `lipgloss`, `sahilm/fuzzy`, `atotto/clipboard`, `zalando/go-keyring`, `golang.org/x/crypto`, `golang.org/x/term`, `BurntSushi/toml`. A tenth needs a justification in the PR.
  > **Correction to spec §12:** the spec lists eight and folds `term` into `golang.org/x/crypto`. `golang.org/x/term` is a separate module, and it is what reads a password without echoing it, so the real floor is nine. Nothing else changes.
- `net/http` may be imported **only** by `internal/fetch` and `internal/capture/browser`. Task 13 enforces this in CI.
- All cryptographic operations live in `internal/crypto`. No crypto primitives elsewhere.
- Vault format version is `1`. The golden fixture from Task 2 must keep opening forever.
- A failed title fetch must never lose a bookmark.
- No telemetry, analytics, or crash reporting.
- File permissions: vault, backup, temp and lock files are `0o600`; the data directory is `0o700`.
- Every command writes user-facing output through a package-level `var out io.Writer = os.Stdout` so tests can capture it.

## Review Focus

These are input classes the spec implies but does not discuss. Each has a test attached to the task that owns the code.

1. **Truncated or empty vault file** (0 bytes, or 30 bytes) — must report the ordinary wrong-password-or-corrupt error, never panic on a slice bound. *Task 2.*
2. **A bare host pasted from the clipboard** (`example.com`, no scheme) — the overwhelmingly common paste. Accepted by prefixing `https://`; anything still unparseable is rejected with a clear message. *Task 3.*
3. **A hostile or broken page title** — 40 KiB of whitespace, embedded newlines, HTML entities, no closing tag. Titles are entity-unescaped, whitespace-collapsed, and truncated to 300 runes. *Task 9.*
4. **A terminal with zero usable height** — a picker rendering into a 0-row window must not panic or divide by zero. *Task 10.*
5. **A concurrent `bkmr add` while the picker is open** — the picker must not write back a stale collection and silently delete the new bookmark. Mutations reload under the lock before saving. *Task 10.*

## Deviations from the spec

Five places where this plan does not do what the spec says. Each is deliberate; raise them in the PR.

1. **Nine direct dependencies, not eight.** Spec §12 folds `term` into `golang.org/x/crypto`; it is its own module. See Global Constraints.
2. **No `Store` interface in `internal/store`.** Spec §3 describes `Load/Add/Update/Delete`, but `model.Collection` already owns `Add` and `Delete`, so the store exposes `Load`, `Save` and `Mutate` on a concrete type, and the consumer-side interface is declared in `internal/tui` where a fake is actually needed. A SQLite implementation remains a drop-in. *Task 4.*
3. **No `teatest`.** Spec §11 names it; the picker's `Update` is a pure function of `(Model, tea.Msg)`, so it is unit-tested directly. That avoids a tenth dependency and runs faster. *Task 10.*
4. **Picker keybindings differ.** Spec §8 assigns bare letters (`o` copy, `e` edit, `d` delete), which cannot work because every printable key goes into the filter box. Copy and delete are `ctrl+y` and `ctrl+d`; in-picker `$EDITOR` editing is dropped because `bkmr edit <id>` covers it. *Task 10.*
5. **`bkmr export` with no argument writes to stdout.** The spec left the no-argument case undefined. *Task 12.*

---

### Task 1: Module skeleton and platform paths

**Files:**
- Create: `go.mod`, `.gitignore`, `LICENSE`
- Create: `internal/config/paths.go`
- Test: `internal/config/paths_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `config.DataDir() (string, error)` — returns the data directory, creating it with `0o700` if absent. `config.ConfigPath() (string, error)` — full path to `config.toml`.

- [ ] **Step 1: Write the failing test**

```go
// internal/config/paths_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataDirHonorsEnvOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "nested", "vaultdir")
	t.Setenv("BKMR_DATA_DIR", want)

	got, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir() error = %v", err)
	}
	if got != want {
		t.Errorf("DataDir() = %q, want %q", got, want)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("DataDir() did not create the directory: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("DataDir() is not a directory")
	}
}

func TestDataDirRejectsOverrideThatIsAFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BKMR_DATA_DIR", f)

	if _, err := DataDir(); err == nil {
		t.Fatal("DataDir() error = nil, want an error when the path is a file")
	}
}

func TestDataDirDefaultIsNotEmpty(t *testing.T) {
	t.Setenv("BKMR_DATA_DIR", "")
	got, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir() error = %v", err)
	}
	if got == "" {
		t.Error("DataDir() = empty string, want a platform default")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/config/ -run TestDataDir -v`
Expected: FAIL — the package does not build, `undefined: DataDir`.

- [ ] **Step 3: Write the minimal implementation**

```go
// internal/config/paths.go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const appDir = "bookmrkrr"

// DataDir returns the directory holding the vault, creating it if needed.
// BKMR_DATA_DIR overrides the platform default; the test suite relies on it.
func DataDir() (string, error) {
	dir := os.Getenv("BKMR_DATA_DIR")
	if dir == "" {
		base, err := platformDataBase()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(base, appDir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create data directory %s: %w", dir, err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is a file, not a directory", dir)
	}
	return dir, nil
}

// ConfigPath returns the full path to config.toml.
func ConfigPath() (string, error) {
	if runtime.GOOS == "linux" {
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".config")
		}
		return filepath.Join(base, appDir, "config.toml"), nil
	}
	if runtime.GOOS == "windows" {
		if base := os.Getenv("APPDATA"); base != "" {
			return filepath.Join(base, appDir, "config.toml"), nil
		}
	}
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

func platformDataBase() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			return base, nil
		}
	case "linux":
		if base := os.Getenv("XDG_DATA_HOME"); base != "" {
			return base, nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	}
	return os.UserConfigDir()
}
```

Note on `os.Stat` after `MkdirAll`: when the override points at an existing file, `MkdirAll` fails first, so the explicit `IsDir` check is belt-and-braces for the case where the path is created between the two calls.

- [ ] **Step 4: Create `go.mod`, `.gitignore` and `LICENSE`**

```bash
go mod init github.com/FredrikSchold/bookmrkrr
go mod edit -go=1.24
```

`.gitignore`:

```
/bkmr
/bkmr.exe
/dist/
*.bkmr
*.bkmr.bak
*.bkmr.tmp
vault.lock
```

`LICENSE`: the standard MIT text, `Copyright (c) 2026 Fredrik Schöld`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS — three tests.

- [ ] **Step 6: Commit**

```bash
git add go.mod .gitignore LICENSE internal/config/
git commit -m "feat(config): platform data and config paths with BKMR_DATA_DIR override"
```

---

### Task 2: Encrypted vault envelope

**Files:**
- Create: `internal/crypto/vault.go`
- Test: `internal/crypto/vault_test.go`
- Test fixture: `internal/crypto/testdata/golden_v1.bkmr`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `crypto.Params{Time uint32, MemoryKiB uint32, Threads uint8}` and `crypto.Default`
  - `crypto.KeyLen = 32`, `crypto.SaltLen = 16`, `crypto.Version = 1`
  - `crypto.NewSalt() ([]byte, error)`
  - `crypto.DeriveKey(password, salt []byte, p Params) []byte`
  - `crypto.Seal(key, plaintext []byte, p Params, salt []byte) ([]byte, error)`
  - `crypto.Open(key, blob []byte) ([]byte, error)`
  - `crypto.SaltOf(blob []byte) ([]byte, Params, error)`
  - `crypto.ErrBadVault` — the single error for wrong password and corrupt file alike

- [ ] **Step 1: Write the failing tests**

```go
// internal/crypto/vault_test.go
package crypto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fastParams keeps unit tests quick. The golden test deliberately uses Default.
var fastParams = Params{Time: 1, MemoryKiB: 8 * 1024, Threads: 1}

func TestSealOpenRoundTrip(t *testing.T) {
	salt, err := NewSalt()
	if err != nil {
		t.Fatal(err)
	}
	key := DeriveKey([]byte("correct horse battery staple"), salt, fastParams)
	plain := []byte(`{"version":1,"bookmarks":[]}`)

	blob, err := Seal(key, plain, fastParams, salt)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if bytes.Contains(blob, []byte("bookmarks")) {
		t.Fatal("Seal() left plaintext visible in the output")
	}

	got, err := Open(key, blob)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("Open() = %q, want %q", got, plain)
	}
}

func TestOpenWithWrongPasswordFails(t *testing.T) {
	salt, _ := NewSalt()
	blob, err := Seal(DeriveKey([]byte("right"), salt, fastParams), []byte("secret"), fastParams, salt)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Open(DeriveKey([]byte("wrong"), salt, fastParams), blob)
	if !errors.Is(err, ErrBadVault) {
		t.Errorf("Open() error = %v, want ErrBadVault", err)
	}
}

func TestOpenDetectsFlippedCiphertextByte(t *testing.T) {
	salt, _ := NewSalt()
	key := DeriveKey([]byte("pw"), salt, fastParams)
	blob, _ := Seal(key, []byte("secret payload"), fastParams, salt)

	blob[len(blob)-1] ^= 0x01

	if _, err := Open(key, blob); !errors.Is(err, ErrBadVault) {
		t.Errorf("Open() error = %v, want ErrBadVault", err)
	}
}

func TestOpenRejectsDowngradedKDFParams(t *testing.T) {
	salt, _ := NewSalt()
	key := DeriveKey([]byte("pw"), salt, fastParams)
	blob, _ := Seal(key, []byte("secret"), fastParams, salt)

	// Rewrite the memory cost in the header. It is authenticated as
	// associated data, so the vault must refuse to open.
	binary.BigEndian.PutUint32(blob[10:14], 8)

	if _, err := Open(key, blob); !errors.Is(err, ErrBadVault) {
		t.Errorf("Open() error = %v, want ErrBadVault", err)
	}
}

// Review Focus 1: a truncated or empty vault must not panic.
func TestOpenHandlesShortAndEmptyBlobs(t *testing.T) {
	key := make([]byte, KeyLen)
	for _, blob := range [][]byte{nil, {}, make([]byte, 30), make([]byte, headerLen)} {
		if _, err := Open(key, blob); !errors.Is(err, ErrBadVault) {
			t.Errorf("Open(%d bytes) error = %v, want ErrBadVault", len(blob), err)
		}
		if _, _, err := SaltOf(blob); !errors.Is(err, ErrBadVault) {
			t.Errorf("SaltOf(%d bytes) error = %v, want ErrBadVault", len(blob), err)
		}
	}
}

func TestSaltOfReturnsHeaderValues(t *testing.T) {
	salt, _ := NewSalt()
	key := DeriveKey([]byte("pw"), salt, fastParams)
	blob, _ := Seal(key, []byte("x"), fastParams, salt)

	gotSalt, gotParams, err := SaltOf(blob)
	if err != nil {
		t.Fatalf("SaltOf() error = %v", err)
	}
	if !bytes.Equal(gotSalt, salt) {
		t.Errorf("SaltOf() salt = %x, want %x", gotSalt, salt)
	}
	if gotParams != fastParams {
		t.Errorf("SaltOf() params = %+v, want %+v", gotParams, fastParams)
	}
}

const goldenPassword = "correct horse battery staple"
const goldenPlaintext = `{"version":1,"bookmarks":[{"id":"aaaaaaaa","url":"https://example.com/","added":"2026-09-27T00:00:00Z"}]}`

// TestGoldenVaultStillOpens pins format version 1 forever. Regenerate the
// fixture only when intentionally introducing a NEW version, never to make
// this test pass.
func TestGoldenVaultStillOpens(t *testing.T) {
	path := filepath.Join("testdata", "golden_v1.bkmr")

	if os.Getenv("BKMR_WRITE_GOLDEN") == "1" {
		salt, _ := NewSalt()
		blob, err := Seal(DeriveKey([]byte(goldenPassword), salt, Default), []byte(goldenPlaintext), Default, salt)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, blob, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Log("golden fixture written")
	}

	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden fixture: %v (regenerate with BKMR_WRITE_GOLDEN=1)", err)
	}
	salt, params, err := SaltOf(blob)
	if err != nil {
		t.Fatalf("SaltOf(golden) error = %v", err)
	}
	got, err := Open(DeriveKey([]byte(goldenPassword), salt, params), blob)
	if err != nil {
		t.Fatalf("Open(golden) error = %v", err)
	}
	if string(got) != goldenPlaintext {
		t.Errorf("Open(golden) = %s, want %s", got, goldenPlaintext)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/crypto/ -v`
Expected: FAIL — `undefined: NewSalt`, `undefined: Seal`, and so on.

- [ ] **Step 3: Add the crypto dependency**

```bash
go get golang.org/x/crypto@latest
```

- [ ] **Step 4: Write the implementation**

```go
// Package crypto is the only package in BookMrkr that performs cryptographic
// operations. The vault is one authenticated envelope: a cleartext header
// carrying the KDF parameters, salt and nonce, followed by XChaCha20-Poly1305
// ciphertext. The header is authenticated as associated data, so the KDF
// parameters cannot be weakened without invalidating the file.
package crypto

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	magic = "BMRK1"
	// Version is the on-disk format version.
	Version = 1
	// SaltLen is the Argon2id salt length in bytes.
	SaltLen = 16
	// KeyLen is the derived key length in bytes.
	KeyLen = 32

	headerLen = 5 + 1 + 4 + 4 + 1 + SaltLen + chacha20poly1305.NonceSizeX // 55
)

// ErrBadVault covers a wrong password and a corrupt file alike. Callers must
// not distinguish the two: doing so would hand an attacker an oracle.
var ErrBadVault = errors.New("could not decrypt vault - wrong password or corrupt file")

// Params holds the Argon2id cost parameters, stored in the file header.
type Params struct {
	Time      uint32
	MemoryKiB uint32
	Threads   uint8
}

// Default costs roughly 100ms on a laptop. Paid only on unlock, never on
// routine commands, because the derived key is cached in the OS keychain.
var Default = Params{Time: 3, MemoryKiB: 64 * 1024, Threads: 4}

// NewSalt returns a fresh random salt.
func NewSalt() ([]byte, error) {
	s := make([]byte, SaltLen)
	if _, err := rand.Read(s); err != nil {
		return nil, err
	}
	return s, nil
}

// DeriveKey stretches a password into a KeyLen key with Argon2id.
func DeriveKey(password, salt []byte, p Params) []byte {
	return argon2.IDKey(password, salt, p.Time, p.MemoryKiB, p.Threads, KeyLen)
}

func header(p Params, salt, nonce []byte) []byte {
	h := make([]byte, 0, headerLen)
	h = append(h, magic...)
	h = append(h, Version)
	h = binary.BigEndian.AppendUint32(h, p.Time)
	h = binary.BigEndian.AppendUint32(h, p.MemoryKiB)
	h = append(h, p.Threads)
	h = append(h, salt...)
	h = append(h, nonce...)
	return h
}

// Seal encrypts plaintext into a complete vault blob.
func Seal(key, plaintext []byte, p Params, salt []byte) ([]byte, error) {
	if len(key) != KeyLen {
		return nil, fmt.Errorf("key must be %d bytes, got %d", KeyLen, len(key))
	}
	if len(salt) != SaltLen {
		return nil, fmt.Errorf("salt must be %d bytes, got %d", SaltLen, len(salt))
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	h := header(p, salt, nonce)
	return append(h, aead.Seal(nil, nonce, plaintext, h)...), nil
}

// SaltOf reads the salt and KDF parameters from a vault blob so a caller can
// derive the key from a password without first decrypting anything.
func SaltOf(blob []byte) ([]byte, Params, error) {
	if len(blob) < headerLen || string(blob[:len(magic)]) != magic || blob[5] != Version {
		return nil, Params{}, ErrBadVault
	}
	return blob[15:31], Params{
		Time:      binary.BigEndian.Uint32(blob[6:10]),
		MemoryKiB: binary.BigEndian.Uint32(blob[10:14]),
		Threads:   blob[14],
	}, nil
}

// Open authenticates and decrypts a vault blob.
func Open(key, blob []byte) ([]byte, error) {
	if _, _, err := SaltOf(blob); err != nil {
		return nil, err
	}
	if len(key) != KeyLen {
		return nil, ErrBadVault
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, ErrBadVault
	}
	out, err := aead.Open(nil, blob[31:headerLen], blob[headerLen:], blob[:headerLen])
	if err != nil {
		return nil, ErrBadVault
	}
	return out, nil
}
```

- [ ] **Step 5: Generate the golden fixture, then run the full suite**

```bash
BKMR_WRITE_GOLDEN=1 go test ./internal/crypto/ -run TestGoldenVault -v
go test ./internal/crypto/ -v
```

On PowerShell: `$env:BKMR_WRITE_GOLDEN=1; go test ./internal/crypto/ -run TestGoldenVault -v; $env:BKMR_WRITE_GOLDEN=$null`

Expected: PASS — seven tests, and `internal/crypto/testdata/golden_v1.bkmr` exists.

- [ ] **Step 6: Commit**

```bash
git add internal/crypto/ go.mod go.sum
git commit -m "feat(crypto): authenticated vault envelope with argon2id and xchacha20-poly1305"
```

---

### Task 3: Bookmark model, normalization and dedupe

**Files:**
- Create: `internal/model/bookmark.go`
- Test: `internal/model/bookmark_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `model.Bookmark` with fields `ID, URL, Title string`, `Tags []string`, `Notes string`, `Added time.Time`, `Visited *time.Time`, `Opens int`
  - `model.Collection{Version int, Bookmarks []Bookmark}` and `model.Version = 1`
  - `model.NewID() string` — 8 lowercase base32 characters
  - `model.NormalizeURL(raw string) (string, error)` — for comparison; prefixes `https://` on a bare host
  - `model.NormalizeTags(in []string) []string` — lowercased, hyphenated, deduped, sorted
  - `(*Collection).Add(b Bookmark) (Bookmark, bool, error)` — returns the stored bookmark and whether it merged into an existing one
  - `(*Collection).Find(id string) (*Bookmark, bool)`
  - `(*Collection).Delete(id string) bool`
  - `(*Collection).TagCounts() map[string]int`

- [ ] **Step 1: Write the failing tests**

```go
// internal/model/bookmark_test.go
package model

import (
	"testing"
	"time"
)

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"lowercases scheme and host", "HTTPS://Example.COM/Path", "https://example.com/Path"},
		{"drops a bare trailing slash", "https://example.com/", "https://example.com"},
		{"keeps a meaningful trailing slash", "https://example.com/docs/", "https://example.com/docs/"},
		{"strips utm parameters", "https://example.com/a?utm_source=x&id=7", "https://example.com/a?id=7"},
		{"strips fbclid and gclid", "https://example.com/a?fbclid=1&gclid=2", "https://example.com/a"},
		{"keeps the fragment", "https://example.com/app#/route", "https://example.com/app#/route"},
		// Review Focus 2: the most common clipboard paste.
		{"prefixes https on a bare host", "example.com", "https://example.com"},
		{"prefixes https on a bare host with a path", "example.com/docs", "https://example.com/docs"},
		{"trims surrounding whitespace", "  https://example.com  ", "https://example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeURL(tt.in)
			if err != nil {
				t.Fatalf("NormalizeURL(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("NormalizeURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeURLRejectsNonURLs(t *testing.T) {
	for _, in := range []string{"", "   ", "hello world", "ftp://example.com", "file:///etc/passwd", "not a url at all"} {
		if got, err := NormalizeURL(in); err == nil {
			t.Errorf("NormalizeURL(%q) = %q, want an error", in, got)
		}
	}
}

func TestNormalizeTags(t *testing.T) {
	got := NormalizeTags([]string{" Rust ", "rust", "Go Lang", "", "  ", "Web/Dev"})
	want := []string{"go-lang", "rust", "web/dev"}
	if len(got) != len(want) {
		t.Fatalf("NormalizeTags() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("NormalizeTags() = %v, want %v", got, want)
		}
	}
}

func TestNewIDIsEightLowercaseChars(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := NewID()
		if len(id) != 8 {
			t.Fatalf("NewID() = %q, want 8 characters", id)
		}
		if id != lower(id) {
			t.Fatalf("NewID() = %q, want lowercase", id)
		}
		if seen[id] {
			t.Fatalf("NewID() returned a duplicate: %q", id)
		}
		seen[id] = true
	}
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

func TestAddAssignsIDAndTimestamp(t *testing.T) {
	c := &Collection{Version: Version}

	got, merged, err := c.Add(Bookmark{URL: "https://example.com", Tags: []string{"Rust"}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if merged {
		t.Error("Add() merged = true, want false for a new bookmark")
	}
	if len(got.ID) != 8 {
		t.Errorf("Add() ID = %q, want 8 characters", got.ID)
	}
	if got.Added.IsZero() {
		t.Error("Add() left Added zero")
	}
	if len(got.Tags) != 1 || got.Tags[0] != "rust" {
		t.Errorf("Add() Tags = %v, want [rust]", got.Tags)
	}
	if len(c.Bookmarks) != 1 {
		t.Errorf("len(Bookmarks) = %d, want 1", len(c.Bookmarks))
	}
}

func TestAddMergesTagsIntoAnExistingURL(t *testing.T) {
	c := &Collection{Version: Version}
	first, _, _ := c.Add(Bookmark{URL: "https://example.com/a", Tags: []string{"rust"}, Title: "A"})

	got, merged, err := c.Add(Bookmark{URL: "HTTPS://Example.com/a?utm_source=x", Tags: []string{"async"}, Notes: "later"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if !merged {
		t.Fatal("Add() merged = false, want true for a duplicate URL")
	}
	if got.ID != first.ID {
		t.Errorf("Add() ID = %q, want the existing %q", got.ID, first.ID)
	}
	if len(c.Bookmarks) != 1 {
		t.Fatalf("len(Bookmarks) = %d, want 1", len(c.Bookmarks))
	}
	if len(got.Tags) != 2 || got.Tags[0] != "async" || got.Tags[1] != "rust" {
		t.Errorf("Add() Tags = %v, want [async rust]", got.Tags)
	}
	if got.Notes != "later" {
		t.Errorf("Add() Notes = %q, want %q", got.Notes, "later")
	}
	if got.Title != "A" {
		t.Errorf("Add() Title = %q, want the existing %q", got.Title, "A")
	}
}

func TestFindAndDelete(t *testing.T) {
	c := &Collection{Version: Version}
	b, _, _ := c.Add(Bookmark{URL: "https://example.com"})

	if _, ok := c.Find(b.ID); !ok {
		t.Fatal("Find() ok = false, want true")
	}
	if _, ok := c.Find("nosuchid"); ok {
		t.Error("Find(unknown) ok = true, want false")
	}
	if !c.Delete(b.ID) {
		t.Error("Delete() = false, want true")
	}
	if len(c.Bookmarks) != 0 {
		t.Errorf("len(Bookmarks) = %d, want 0", len(c.Bookmarks))
	}
	if c.Delete(b.ID) {
		t.Error("Delete() on an already-deleted id = true, want false")
	}
}

func TestTagCounts(t *testing.T) {
	c := &Collection{Version: Version}
	c.Add(Bookmark{URL: "https://a.example", Tags: []string{"rust", "web"}})
	c.Add(Bookmark{URL: "https://b.example", Tags: []string{"rust"}})

	got := c.TagCounts()
	if got["rust"] != 2 {
		t.Errorf("TagCounts()[rust] = %d, want 2", got["rust"])
	}
	if got["web"] != 1 {
		t.Errorf("TagCounts()[web] = %d, want 1", got["web"])
	}
}

func TestAddPreservesExplicitTimestamps(t *testing.T) {
	c := &Collection{Version: Version}
	when := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

	got, _, err := c.Add(Bookmark{URL: "https://example.com", Added: when})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Added.Equal(when) {
		t.Errorf("Add() Added = %v, want %v", got.Added, when)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/model/ -v`
Expected: FAIL — `undefined: Collection`, `undefined: NormalizeURL`.

- [ ] **Step 3: Write the implementation**

```go
// Package model holds the bookmark type and the rules for normalizing and
// deduplicating bookmarks. It performs no I/O.
package model

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Version is the plaintext document version stored inside the vault.
const Version = 1

// Bookmark is one saved link.
type Bookmark struct {
	ID      string     `json:"id"`
	URL     string     `json:"url"`
	Title   string     `json:"title,omitempty"`
	Tags    []string   `json:"tags,omitempty"`
	Notes   string     `json:"notes,omitempty"`
	Added   time.Time  `json:"added"`
	Visited *time.Time `json:"visited,omitempty"`
	Opens   int        `json:"opens,omitempty"`
}

// Collection is the whole vault plaintext.
type Collection struct {
	Version   int        `json:"version"`
	Bookmarks []Bookmark `json:"bookmarks"`
}

var idEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewID returns a random 8-character lowercase identifier.
func NewID() string {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		panic("bkmr: system randomness unavailable: " + err.Error())
	}
	return strings.ToLower(idEncoding.EncodeToString(b))
}

var trackingParams = map[string]bool{"fbclid": true, "gclid": true, "mc_eid": true}

// NormalizeURL returns a canonical form used only for duplicate detection.
// The original URL is always what gets stored. A bare host gains an https
// scheme, because that is the overwhelmingly common clipboard paste.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty URL")
	}
	if strings.ContainsAny(raw, " \t\n") {
		return "", fmt.Errorf("not a URL: %q", raw)
	}
	if !strings.Contains(raw, "://") {
		if !strings.Contains(raw, ".") {
			return "", fmt.Errorf("not a URL: %q", raw)
		}
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("not a URL: %q", raw)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("only http and https are supported, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("not a URL: %q", raw)
	}
	u.Host = strings.ToLower(u.Host)

	q := u.Query()
	for k := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "utm_") || trackingParams[lk] {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	if u.Path == "/" {
		u.Path = ""
	}
	return u.String(), nil
}

// NormalizeTags lowercases, hyphenates internal whitespace, removes blanks
// and duplicates, and sorts the result.
func NormalizeTags(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.Join(strings.Fields(strings.ToLower(t)), "-")
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Add stores a bookmark, or merges it into an existing entry with the same
// normalized URL. The second return value reports whether a merge happened.
func (c *Collection) Add(b Bookmark) (Bookmark, bool, error) {
	key, err := NormalizeURL(b.URL)
	if err != nil {
		return Bookmark{}, false, err
	}
	b.Tags = NormalizeTags(b.Tags)

	for i := range c.Bookmarks {
		existing, err := NormalizeURL(c.Bookmarks[i].URL)
		if err != nil || existing != key {
			continue
		}
		c.Bookmarks[i].Tags = NormalizeTags(append(c.Bookmarks[i].Tags, b.Tags...))
		if c.Bookmarks[i].Title == "" {
			c.Bookmarks[i].Title = b.Title
		}
		switch {
		case b.Notes == "":
		case c.Bookmarks[i].Notes == "":
			c.Bookmarks[i].Notes = b.Notes
		default:
			c.Bookmarks[i].Notes += "\n" + b.Notes
		}
		return c.Bookmarks[i], true, nil
	}

	if b.ID == "" {
		b.ID = NewID()
	}
	if b.Added.IsZero() {
		b.Added = time.Now().UTC()
	}
	c.Bookmarks = append(c.Bookmarks, b)
	return b, false, nil
}

// Find returns a pointer to the bookmark with the given id.
func (c *Collection) Find(id string) (*Bookmark, bool) {
	for i := range c.Bookmarks {
		if c.Bookmarks[i].ID == id {
			return &c.Bookmarks[i], true
		}
	}
	return nil, false
}

// Delete removes a bookmark by id, reporting whether it existed.
func (c *Collection) Delete(id string) bool {
	for i := range c.Bookmarks {
		if c.Bookmarks[i].ID == id {
			c.Bookmarks = append(c.Bookmarks[:i], c.Bookmarks[i+1:]...)
			return true
		}
	}
	return false
}

// TagCounts returns how many bookmarks carry each tag.
func (c *Collection) TagCounts() map[string]int {
	counts := map[string]int{}
	for _, b := range c.Bookmarks {
		for _, t := range b.Tags {
			counts[t]++
		}
	}
	return counts
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/model/ -v`
Expected: PASS — nine tests.

- [ ] **Step 5: Commit**

```bash
git add internal/model/
git commit -m "feat(model): bookmark type, URL and tag normalization, dedupe on add"
```

---

### Task 4: Vault store with atomic writes and locking

**Files:**
- Create: `internal/store/store.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: `crypto.Seal`, `crypto.Open`, `crypto.SaltOf`, `crypto.NewSalt`, `crypto.Default`, `crypto.KeyLen`; `model.Collection`, `model.Version`.
- Produces:
  - `store.Vault` with `store.New(dir string, key []byte) *Vault`
  - `(*Vault).Path() string`, `(*Vault).Load() (*model.Collection, error)`, `(*Vault).Save(c *model.Collection) error`
  - `(*Vault).Mutate(fn func(*model.Collection) error) error` — reload under the lock, apply, save. This is how every writer avoids clobbering a concurrent change.
  - `store.Exists(dir string) bool`
  - `store.Create(dir string, key []byte, salt []byte) error`
  - `store.ErrBusy`

Note on the spec: spec §3 describes a `Store` interface with `Load/Add/Update/Delete`. `model.Collection` already owns `Add`/`Delete`, so the store needs only `Load`, `Save` and `Mutate`, and the consumer-side interface is declared in `internal/tui` where the fake is needed. Same outcome — a SQLite implementation remains a drop-in — with one fewer abstraction.

- [ ] **Step 1: Write the failing tests**

```go
// internal/store/store_test.go
package store

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/FredrikSchold/bookmrkrr/internal/crypto"
	"github.com/FredrikSchold/bookmrkrr/internal/model"
)

func newTestVault(t *testing.T) (*Vault, string) {
	t.Helper()
	dir := t.TempDir()
	salt, err := crypto.NewSalt()
	if err != nil {
		t.Fatal(err)
	}
	key := crypto.DeriveKey([]byte("pw"), salt, crypto.Params{Time: 1, MemoryKiB: 8 * 1024, Threads: 1})
	if err := Create(dir, key, salt); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return New(dir, key), dir
}

func TestCreateThenLoadGivesAnEmptyCollection(t *testing.T) {
	v, dir := newTestVault(t)

	if !Exists(dir) {
		t.Fatal("Exists() = false after Create()")
	}
	c, err := v.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.Version != model.Version {
		t.Errorf("Version = %d, want %d", c.Version, model.Version)
	}
	if len(c.Bookmarks) != 0 {
		t.Errorf("len(Bookmarks) = %d, want 0", len(c.Bookmarks))
	}
}

func TestCreateRefusesToOverwriteAnExistingVault(t *testing.T) {
	_, dir := newTestVault(t)
	salt, _ := crypto.NewSalt()
	key := make([]byte, crypto.KeyLen)

	if err := Create(dir, key, salt); err == nil {
		t.Fatal("Create() error = nil, want an error on an existing vault")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	v, _ := newTestVault(t)
	c, _ := v.Load()
	c.Add(model.Bookmark{URL: "https://example.com", Tags: []string{"rust"}})

	if err := v.Save(c); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := v.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got.Bookmarks) != 1 || got.Bookmarks[0].URL != "https://example.com" {
		t.Fatalf("Load() = %+v, want one bookmark for example.com", got.Bookmarks)
	}
}

func TestSaveLeavesTheOldVaultAsBackup(t *testing.T) {
	v, dir := newTestVault(t)
	c, _ := v.Load()
	c.Add(model.Bookmark{URL: "https://first.example"})
	if err := v.Save(c); err != nil {
		t.Fatal(err)
	}
	afterFirst, err := os.ReadFile(v.Path())
	if err != nil {
		t.Fatal(err)
	}

	c.Add(model.Bookmark{URL: "https://second.example"})
	if err := v.Save(c); err != nil {
		t.Fatal(err)
	}

	backup, err := os.ReadFile(filepath.Join(dir, "vault.bkmr.bak"))
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != string(afterFirst) {
		t.Error("backup does not match the previous vault contents")
	}
	if _, err := os.Stat(v.Path() + ".tmp"); !os.IsNotExist(err) {
		t.Error("Save() left a .tmp file behind")
	}
}

func TestLoadWithTheWrongKeyFails(t *testing.T) {
	v, dir := newTestVault(t)
	wrong := New(dir, make([]byte, crypto.KeyLen))

	if _, err := wrong.Load(); !errors.Is(err, crypto.ErrBadVault) {
		t.Errorf("Load() error = %v, want ErrBadVault", err)
	}
	_ = v
}

func TestLoadOnATruncatedVaultFails(t *testing.T) {
	v, _ := newTestVault(t)
	if err := os.WriteFile(v.Path(), []byte("BMRK1"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := v.Load(); !errors.Is(err, crypto.ErrBadVault) {
		t.Errorf("Load() error = %v, want ErrBadVault", err)
	}
}

func TestSaveReportsBusyWhenTheLockIsHeld(t *testing.T) {
	v, dir := newTestVault(t)
	lock, err := os.OpenFile(filepath.Join(dir, "vault.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(lock.Name())
	lock.Close()

	c := &model.Collection{Version: model.Version}
	if err := v.Save(c); !errors.Is(err, ErrBusy) {
		t.Errorf("Save() error = %v, want ErrBusy", err)
	}
}

// Review Focus 5: two writers must not lose each other's bookmark.
func TestMutateSerializesConcurrentWriters(t *testing.T) {
	v, _ := newTestVault(t)

	var wg sync.WaitGroup
	urls := []string{"https://a.example", "https://b.example", "https://c.example", "https://d.example"}
	for _, u := range urls {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			for attempt := 0; attempt < 50; attempt++ {
				err := v.Mutate(func(c *model.Collection) error {
					_, _, err := c.Add(model.Bookmark{URL: u})
					return err
				})
				if err == nil {
					return
				}
				if !errors.Is(err, ErrBusy) {
					t.Errorf("Mutate() error = %v", err)
					return
				}
			}
			t.Errorf("Mutate(%s) never acquired the lock", u)
		}(u)
	}
	wg.Wait()

	c, err := v.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Bookmarks) != len(urls) {
		t.Errorf("len(Bookmarks) = %d, want %d - a concurrent write was lost", len(c.Bookmarks), len(urls))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/ -v`
Expected: FAIL — `undefined: Create`, `undefined: New`.

- [ ] **Step 3: Write the implementation**

```go
// Package store reads and writes the encrypted vault file. It knows nothing
// about bookmarks beyond the fact that they serialize to JSON.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/FredrikSchold/bookmrkrr/internal/crypto"
	"github.com/FredrikSchold/bookmrkrr/internal/model"
)

// FileName is the vault's name inside the data directory.
const FileName = "vault.bkmr"

// ErrBusy means another bkmr process holds the write lock.
var ErrBusy = errors.New("vault is busy - another bkmr is writing to it")

// Vault is an encrypted bookmark collection on disk.
type Vault struct {
	dir string
	key []byte
}

// New returns a Vault handle. It performs no I/O.
func New(dir string, key []byte) *Vault { return &Vault{dir: dir, key: key} }

// Path is the vault file's full path.
func (v *Vault) Path() string { return filepath.Join(v.dir, FileName) }

// Exists reports whether a vault file is present in dir.
func Exists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, FileName))
	return err == nil
}

// Create writes a new empty vault. It refuses to overwrite an existing one.
func Create(dir string, key, salt []byte) error {
	path := filepath.Join(dir, FileName)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("a vault already exists at %s", path)
	}
	plain, err := json.Marshal(&model.Collection{Version: model.Version, Bookmarks: []model.Bookmark{}})
	if err != nil {
		return err
	}
	blob, err := crypto.Seal(key, plain, crypto.Default, salt)
	if err != nil {
		return err
	}
	return os.WriteFile(path, blob, 0o600)
}

// Load decrypts and parses the vault.
func (v *Vault) Load() (*model.Collection, error) {
	blob, err := os.ReadFile(v.Path())
	if err != nil {
		return nil, err
	}
	plain, err := crypto.Open(v.key, blob)
	if err != nil {
		return nil, err
	}
	var c model.Collection
	if err := json.Unmarshal(plain, &c); err != nil {
		return nil, crypto.ErrBadVault
	}
	return &c, nil
}

// Save encrypts and atomically replaces the vault, keeping one backup. It
// reuses the salt and KDF parameters already in the file so a cached key
// stays valid.
func (v *Vault) Save(c *model.Collection) error {
	release, err := v.lock()
	if err != nil {
		return err
	}
	defer release()
	return v.write(c)
}

// Mutate reloads the vault under the write lock, applies fn, and saves the
// result. Every writer must use this rather than Load-then-Save, or a
// concurrent add made in between would be silently discarded.
func (v *Vault) Mutate(fn func(*model.Collection) error) error {
	release, err := v.lock()
	if err != nil {
		return err
	}
	defer release()

	c, err := v.Load()
	if err != nil {
		return err
	}
	if err := fn(c); err != nil {
		return err
	}
	return v.write(c)
}

func (v *Vault) write(c *model.Collection) error {
	blob, err := os.ReadFile(v.Path())
	if err != nil {
		return err
	}
	salt, params, err := crypto.SaltOf(blob)
	if err != nil {
		return err
	}
	plain, err := json.Marshal(c)
	if err != nil {
		return err
	}
	sealed, err := crypto.Seal(v.key, plain, params, salt)
	if err != nil {
		return err
	}

	tmp := v.Path() + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(sealed); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(v.Path(), v.Path()+".bak"); err != nil && !os.IsNotExist(err) {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, v.Path())
}

// lock takes an advisory lock via exclusive file creation.
//
// ponytail: a crash leaves vault.lock behind and the user must delete it.
// Upgrade to a PID-stamped lock with staleness detection only if that
// actually bites someone.
func (v *Vault) lock() (func(), error) {
	path := filepath.Join(v.dir, "vault.lock")
	deadline := time.Now().Add(time.Second)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			f.Close()
			return func() { os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, ErrBusy
		}
		time.Sleep(20 * time.Millisecond)
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/store/ -v -race`
Expected: PASS — eight tests, no race warnings.

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat(store): encrypted vault with atomic writes, backup and advisory lock"
```

---

### Task 5: Keychain key cache and password prompt

**Files:**
- Create: `internal/keyring/key.go`
- Test: `internal/keyring/key_test.go`

**Interfaces:**
- Consumes: `crypto.KeyLen`.
- Produces:
  - `keyring.Put(key []byte) error`, `keyring.Get() ([]byte, error)`, `keyring.Delete() error`
  - `keyring.ErrNoKey` — no key is cached
  - `keyring.PromptPassword(prompt string) ([]byte, error)` — reads from the terminal without echo

- [ ] **Step 1: Write the failing tests**

```go
// internal/keyring/key_test.go
package keyring

import (
	"bytes"
	"errors"
	"testing"

	kr "github.com/zalando/go-keyring"

	"github.com/FredrikSchold/bookmrkrr/internal/crypto"
)

func TestPutGetDeleteRoundTrip(t *testing.T) {
	kr.MockInit()
	key := bytes.Repeat([]byte{7}, crypto.KeyLen)

	if err := Put(key); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	got, err := Get()
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !bytes.Equal(got, key) {
		t.Errorf("Get() = %x, want %x", got, key)
	}
	if err := Delete(); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := Get(); !errors.Is(err, ErrNoKey) {
		t.Errorf("Get() after Delete() error = %v, want ErrNoKey", err)
	}
}

func TestGetWithNothingStoredReportsErrNoKey(t *testing.T) {
	kr.MockInit()

	if _, err := Get(); !errors.Is(err, ErrNoKey) {
		t.Errorf("Get() error = %v, want ErrNoKey", err)
	}
}

func TestDeleteWithNothingStoredIsNotAnError(t *testing.T) {
	kr.MockInit()

	if err := Delete(); err != nil {
		t.Errorf("Delete() error = %v, want nil when no key is cached", err)
	}
}

func TestPutRejectsAWrongLengthKey(t *testing.T) {
	kr.MockInit()

	if err := Put([]byte("short")); err == nil {
		t.Error("Put() error = nil, want an error for a wrong-length key")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/keyring/ -v`
Expected: FAIL — the package does not exist yet.

- [ ] **Step 3: Add the dependency**

```bash
go get github.com/zalando/go-keyring@latest
```

- [ ] **Step 4: Write the implementation**

```go
// Package keyring caches the derived vault key in the operating system's
// credential store so routine commands never prompt for a password.
//
// The DERIVED KEY is stored, never the passphrase: a compromised keychain
// then yields something usable only against this vault, not a secret the
// user may have reused elsewhere.
package keyring

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	kr "github.com/zalando/go-keyring"
	"golang.org/x/term"

	"github.com/FredrikSchold/bookmrkrr/internal/crypto"
)

const (
	service = "bookmrkrr"
	user    = "vault-key"
)

// ErrNoKey means no key is cached, so the caller should prompt.
var ErrNoKey = errors.New("no cached key - run 'bkmr unlock'")

// Put caches the derived key.
func Put(key []byte) error {
	if len(key) != crypto.KeyLen {
		return fmt.Errorf("key must be %d bytes, got %d", crypto.KeyLen, len(key))
	}
	return kr.Set(service, user, base64.StdEncoding.EncodeToString(key))
}

// Get returns the cached key, or ErrNoKey. Any platform failure - no Secret
// Service on a headless Linux box, for instance - also reports ErrNoKey, so
// the caller falls back to prompting instead of crashing.
func Get() ([]byte, error) {
	s, err := kr.Get(service, user)
	if err != nil {
		return nil, ErrNoKey
	}
	key, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(key) != crypto.KeyLen {
		return nil, ErrNoKey
	}
	return key, nil
}

// Delete drops the cached key. Deleting nothing is not an error.
func Delete() error {
	if err := kr.Delete(service, user); err != nil && !errors.Is(err, kr.ErrNotFound) {
		return err
	}
	return nil
}

// PromptPassword reads a password from the terminal without echoing it.
func PromptPassword(prompt string) ([]byte, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, errors.New("cannot prompt for a password: stdin is not a terminal")
	}
	fmt.Fprint(os.Stderr, prompt)
	pw, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, err
	}
	if len(pw) == 0 {
		return nil, errors.New("empty password")
	}
	return pw, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/keyring/ -v`
Expected: PASS — four tests.

- [ ] **Step 6: Commit**

```bash
git add internal/keyring/ go.mod go.sum
git commit -m "feat(keyring): cache the derived key in the OS credential store"
```

---

### Task 6: Command dispatch, help and version

**Files:**
- Create: `cmd/bkmr/main.go`, `cmd/bkmr/help.go`
- Test: `cmd/bkmr/help_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `type command struct { Name, Summary, Usage string; Run func(args []string) error }`
  - `var commands []command` — every later task appends its command here
  - `var out io.Writer = os.Stdout` — all user-facing output goes through this
  - `func dispatch(args []string) int` — returns the process exit code
  - `func find(name string) (command, bool)`

- [ ] **Step 1: Write the failing tests**

```go
// cmd/bkmr/help_test.go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func capture(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	old := out
	out = &buf
	defer func() { out = old }()
	fn()
	return buf.String()
}

func TestHelpListsEveryCommand(t *testing.T) {
	got := capture(t, func() {
		if code := dispatch([]string{"help"}); code != 0 {
			t.Errorf("dispatch(help) = %d, want 0", code)
		}
	})

	for _, c := range commands {
		if !strings.Contains(got, c.Name) {
			t.Errorf("help output is missing command %q", c.Name)
		}
		if c.Summary == "" {
			t.Errorf("command %q has no summary", c.Name)
		}
	}
	if !strings.Contains(got, "bkmr") {
		t.Error("help output does not mention the binary name")
	}
}

func TestHelpForOneCommandPrintsItsUsage(t *testing.T) {
	got := capture(t, func() { dispatch([]string{"help", "help"}) })

	h, ok := find("help")
	if !ok {
		t.Fatal("the help command is not registered")
	}
	if !strings.Contains(got, h.Usage) {
		t.Errorf("help help = %q, want it to contain the usage line %q", got, h.Usage)
	}
}

func TestHelpFlagsAreAliases(t *testing.T) {
	long := capture(t, func() { dispatch([]string{"--help"}) })
	short := capture(t, func() { dispatch([]string{"-h"}) })

	if long == "" || long != short {
		t.Errorf("--help and -h must print the same help text; got %q and %q", long, short)
	}
}

func TestUnknownCommandExitsTwo(t *testing.T) {
	got := capture(t, func() {
		if code := dispatch([]string{"nosuchcommand"}); code != 2 {
			t.Errorf("dispatch(unknown) = %d, want 2", code)
		}
	})

	if !strings.Contains(got, "nosuchcommand") {
		t.Errorf("output = %q, want it to name the unknown command", got)
	}
	if !strings.Contains(got, "help") {
		t.Errorf("output = %q, want it to point at 'bkmr help'", got)
	}
}

func TestVersionPrintsSomething(t *testing.T) {
	got := capture(t, func() {
		if code := dispatch([]string{"version"}); code != 0 {
			t.Errorf("dispatch(version) = %d, want 0", code)
		}
	})

	if !strings.Contains(got, version) {
		t.Errorf("version output = %q, want it to contain %q", got, version)
	}
}

func TestEveryCommandHasAUniqueName(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range commands {
		if seen[c.Name] {
			t.Errorf("duplicate command name %q", c.Name)
		}
		seen[c.Name] = true
		if c.Run == nil {
			t.Errorf("command %q has a nil Run", c.Name)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/bkmr/ -v`
Expected: FAIL — `undefined: dispatch`, `undefined: commands`.

- [ ] **Step 3: Write the implementation**

```go
// Command bkmr is a terminal-first bookmark manager with a locally encrypted
// vault.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

const version = "0.1.0"

// out is where all user-facing output goes, so tests can capture it.
var out io.Writer = os.Stdout

type command struct {
	Name    string
	Summary string
	Usage   string
	Run     func(args []string) error
}

// commands is the whole command surface. Each command file appends to it in
// its own init function.
var commands []command

func register(c command) { commands = append(commands, c) }

func find(name string) (command, bool) {
	for _, c := range commands {
		if c.Name == name {
			return c, true
		}
	}
	return command{}, false
}

func init() {
	register(command{
		Name:    "version",
		Summary: "print the bkmr version",
		Usage:   "bkmr version",
		Run: func([]string) error {
			fmt.Fprintln(out, "bkmr", version)
			return nil
		},
	})
}

// dispatch runs one invocation and returns the process exit code.
func dispatch(args []string) int {
	name := "picker"
	if len(args) > 0 {
		name = args[0]
		args = args[1:]
	}
	if name == "--help" || name == "-h" {
		name, args = "help", nil
	}

	c, ok := find(name)
	if !ok {
		fmt.Fprintf(out, "bkmr: unknown command %q\nRun 'bkmr help' for the list of commands.\n", name)
		return 2
	}
	if err := c.Run(args); err != nil {
		if errors.Is(err, errUsage) {
			fmt.Fprintf(out, "usage: %s\n", c.Usage)
			return 2
		}
		fmt.Fprintln(os.Stderr, "bkmr:", err)
		return 1
	}
	return 0
}

// errUsage makes a command print its usage line and exit 2.
var errUsage = errors.New("usage")

func main() { os.Exit(dispatch(os.Args[1:])) }
```

```go
// cmd/bkmr/help.go
package main

import (
	"fmt"
	"text/tabwriter"
)

func init() {
	register(command{
		Name:    "help",
		Summary: "list commands, or show one command's usage",
		Usage:   "bkmr help [command]",
		Run:     runHelp,
	})
}

func runHelp(args []string) error {
	if len(args) > 0 {
		c, ok := find(args[0])
		if !ok {
			return fmt.Errorf("unknown command %q", args[0])
		}
		fmt.Fprintf(out, "%s\n\nusage: %s\n", c.Summary, c.Usage)
		return nil
	}

	fmt.Fprint(out, "bkmr - terminal-first bookmarks in a locally encrypted vault\n\nusage: bkmr <command> [arguments]\n\ncommands:\n")
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, c := range commands {
		fmt.Fprintf(w, "  %s\t%s\n", c.Name, c.Summary)
	}
	w.Flush()
	fmt.Fprint(out, "\nRun 'bkmr help <command>' for one command's usage.\n")
	return nil
}
```

Note: the zero-argument name is `picker`, registered in Task 10. Until then, bare `bkmr` exits 2 with the unknown-command message, which is correct for a half-built binary.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./cmd/bkmr/ -v`
Expected: PASS — six tests.

- [ ] **Step 5: Commit**

```bash
git add cmd/bkmr/
git commit -m "feat(cli): command dispatch table, help and version"
```

---

### Task 7: init, unlock and lock

**Files:**
- Create: `cmd/bkmr/session.go`, `cmd/bkmr/init.go`, `cmd/bkmr/lock.go`
- Test: `cmd/bkmr/session_test.go`

**Interfaces:**
- Consumes: `config.DataDir`, `crypto.*`, `keyring.*`, `store.*`, `model.*`.
- Produces:
  - `var readPassword = keyring.PromptPassword` — a seam tests replace
  - `func openVault() (*store.Vault, error)` — resolves the data directory, gets a key from the keychain or a prompt, and returns a handle; errors with an instruction to run `bkmr init` when no vault exists
  - `func keyFor(dir string) ([]byte, error)`
  - commands `init`, `unlock`, `lock`

- [ ] **Step 1: Write the failing tests**

```go
// cmd/bkmr/session_test.go
package main

import (
	"errors"
	"strings"
	"testing"

	kr "github.com/zalando/go-keyring"

	"github.com/FredrikSchold/bookmrkrr/internal/crypto"
	"github.com/FredrikSchold/bookmrkrr/internal/keyring"
	"github.com/FredrikSchold/bookmrkrr/internal/model"
)

// newVaultForTest points BKMR_DATA_DIR at a temp dir, mocks the keychain,
// and creates a vault with the given password.
func newVaultForTest(t *testing.T, password string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("BKMR_DATA_DIR", dir)
	kr.MockInit()

	old := readPassword
	readPassword = func(string) ([]byte, error) { return []byte(password), nil }
	t.Cleanup(func() { readPassword = old })

	if err := runInit(nil); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
	return dir
}

func TestInitCreatesAVaultAndCachesTheKey(t *testing.T) {
	newVaultForTest(t, "hunter2")

	if _, err := keyring.Get(); err != nil {
		t.Errorf("keyring.Get() error = %v, want the key cached after init", err)
	}
	v, err := openVault()
	if err != nil {
		t.Fatalf("openVault() error = %v", err)
	}
	c, err := v.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(c.Bookmarks) != 0 {
		t.Errorf("a fresh vault holds %d bookmarks, want 0", len(c.Bookmarks))
	}
}

func TestInitRefusesToRunTwice(t *testing.T) {
	newVaultForTest(t, "hunter2")

	if err := runInit(nil); err == nil {
		t.Error("runInit() error = nil on a second run, want an error")
	}
}

func TestOpenVaultWithoutAVaultTellsTheUserToInit(t *testing.T) {
	t.Setenv("BKMR_DATA_DIR", t.TempDir())
	kr.MockInit()

	_, err := openVault()
	if err == nil {
		t.Fatal("openVault() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "bkmr init") {
		t.Errorf("openVault() error = %q, want it to mention 'bkmr init'", err)
	}
}

func TestLockDropsTheCachedKeyAndUnlockRestoresIt(t *testing.T) {
	newVaultForTest(t, "hunter2")

	if err := runLock(nil); err != nil {
		t.Fatalf("runLock() error = %v", err)
	}
	if _, err := keyring.Get(); !errors.Is(err, keyring.ErrNoKey) {
		t.Errorf("keyring.Get() after lock error = %v, want ErrNoKey", err)
	}

	if err := runUnlock(nil); err != nil {
		t.Fatalf("runUnlock() error = %v", err)
	}
	if _, err := keyring.Get(); err != nil {
		t.Errorf("keyring.Get() after unlock error = %v, want the key cached", err)
	}
}

func TestOpenVaultFallsBackToAPromptWhenNoKeyIsCached(t *testing.T) {
	newVaultForTest(t, "hunter2")
	if err := runLock(nil); err != nil {
		t.Fatal(err)
	}

	v, err := openVault()
	if err != nil {
		t.Fatalf("openVault() error = %v", err)
	}
	if _, err := v.Load(); err != nil {
		t.Errorf("Load() after a prompted unlock error = %v", err)
	}
	// Prompting must not silently cache the key; only unlock does that.
	if _, err := keyring.Get(); !errors.Is(err, keyring.ErrNoKey) {
		t.Error("openVault() cached the key; only 'bkmr unlock' should do that")
	}
}

func TestOpenVaultWithTheWrongPasswordFailsToLoad(t *testing.T) {
	newVaultForTest(t, "hunter2")
	if err := runLock(nil); err != nil {
		t.Fatal(err)
	}
	readPassword = func(string) ([]byte, error) { return []byte("wrong"), nil }

	v, err := openVault()
	if err != nil {
		t.Fatalf("openVault() error = %v", err)
	}
	if _, err := v.Load(); !errors.Is(err, crypto.ErrBadVault) {
		t.Errorf("Load() error = %v, want ErrBadVault", err)
	}
}

func TestUnlockWithTheWrongPasswordDoesNotCacheAKey(t *testing.T) {
	newVaultForTest(t, "hunter2")
	if err := runLock(nil); err != nil {
		t.Fatal(err)
	}
	readPassword = func(string) ([]byte, error) { return []byte("wrong"), nil }

	if err := runUnlock(nil); err == nil {
		t.Fatal("runUnlock() error = nil with a wrong password, want an error")
	}
	if _, err := keyring.Get(); !errors.Is(err, keyring.ErrNoKey) {
		t.Error("runUnlock() cached a key derived from a wrong password")
	}
}

func TestInitSeedsTheCollectionVersion(t *testing.T) {
	newVaultForTest(t, "hunter2")
	v, _ := openVault()
	c, _ := v.Load()

	if c.Version != model.Version {
		t.Errorf("Version = %d, want %d", c.Version, model.Version)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/bkmr/ -run 'TestInit|TestOpenVault|TestLock|TestUnlock' -v`
Expected: FAIL — `undefined: runInit`, `undefined: openVault`.

- [ ] **Step 3: Write the session helper**

```go
// cmd/bkmr/session.go
package main

import (
	"fmt"
	"os"

	"github.com/FredrikSchold/bookmrkrr/internal/config"
	"github.com/FredrikSchold/bookmrkrr/internal/crypto"
	"github.com/FredrikSchold/bookmrkrr/internal/keyring"
	"github.com/FredrikSchold/bookmrkrr/internal/store"
)

// readPassword is a seam so tests can supply a password without a terminal.
var readPassword = keyring.PromptPassword

// openVault resolves the data directory and returns a vault handle whose key
// comes from the keychain, or from a password prompt when nothing is cached.
func openVault() (*store.Vault, error) {
	dir, err := config.DataDir()
	if err != nil {
		return nil, err
	}
	if !store.Exists(dir) {
		return nil, fmt.Errorf("no vault found in %s - run 'bkmr init' to create one", dir)
	}
	key, err := keyFor(dir)
	if err != nil {
		return nil, err
	}
	return store.New(dir, key), nil
}

// keyFor returns the cached key, or derives one from a prompted password. It
// does not cache what it derives; only 'bkmr unlock' does that.
func keyFor(dir string) ([]byte, error) {
	if key, err := keyring.Get(); err == nil {
		return key, nil
	}
	return deriveFromPrompt(dir)
}

func deriveFromPrompt(dir string) ([]byte, error) {
	blob, err := os.ReadFile(store.New(dir, nil).Path())
	if err != nil {
		return nil, err
	}
	salt, params, err := crypto.SaltOf(blob)
	if err != nil {
		return nil, err
	}
	pw, err := readPassword("Vault password: ")
	if err != nil {
		return nil, err
	}
	return crypto.DeriveKey(pw, salt, params), nil
}
```

- [ ] **Step 4: Write init, unlock and lock**

```go
// cmd/bkmr/init.go
package main

import (
	"bytes"
	"fmt"

	"github.com/FredrikSchold/bookmrkrr/internal/config"
	"github.com/FredrikSchold/bookmrkrr/internal/crypto"
	"github.com/FredrikSchold/bookmrkrr/internal/keyring"
	"github.com/FredrikSchold/bookmrkrr/internal/store"
)

func init() {
	register(command{
		Name:    "init",
		Summary: "create the vault and set its password",
		Usage:   "bkmr init",
		Run:     runInit,
	})
}

func runInit([]string) error {
	dir, err := config.DataDir()
	if err != nil {
		return err
	}
	if store.Exists(dir) {
		return fmt.Errorf("a vault already exists in %s", dir)
	}

	pw, err := readPassword("New vault password: ")
	if err != nil {
		return err
	}
	again, err := readPassword("Confirm password: ")
	if err != nil {
		return err
	}
	if !bytes.Equal(pw, again) {
		return fmt.Errorf("the passwords do not match")
	}

	salt, err := crypto.NewSalt()
	if err != nil {
		return err
	}
	key := crypto.DeriveKey(pw, salt, crypto.Default)
	if err := store.Create(dir, key, salt); err != nil {
		return err
	}
	if err := keyring.Put(key); err != nil {
		fmt.Fprintf(out, "Vault created in %s.\nCould not cache the key (%v); you will be prompted each time.\n", dir, err)
		return nil
	}
	fmt.Fprintf(out, "Vault created in %s and unlocked.\nAdd your first bookmark with 'bkmr add <url>'.\n", dir)
	return nil
}
```

```go
// cmd/bkmr/lock.go
package main

import (
	"fmt"

	"github.com/FredrikSchold/bookmrkrr/internal/config"
	"github.com/FredrikSchold/bookmrkrr/internal/keyring"
	"github.com/FredrikSchold/bookmrkrr/internal/store"
)

func init() {
	register(command{
		Name:    "unlock",
		Summary: "derive the key from your password and cache it",
		Usage:   "bkmr unlock",
		Run:     runUnlock,
	})
	register(command{
		Name:    "lock",
		Summary: "drop the cached key",
		Usage:   "bkmr lock",
		Run:     runLock,
	})
}

func runUnlock([]string) error {
	dir, err := config.DataDir()
	if err != nil {
		return err
	}
	if !store.Exists(dir) {
		return fmt.Errorf("no vault found in %s - run 'bkmr init' to create one", dir)
	}
	key, err := deriveFromPrompt(dir)
	if err != nil {
		return err
	}
	// Verify before caching, so a typo never gets stored.
	if _, err := store.New(dir, key).Load(); err != nil {
		return err
	}
	if err := keyring.Put(key); err != nil {
		return err
	}
	fmt.Fprintln(out, "Vault unlocked.")
	return nil
}

func runLock([]string) error {
	if err := keyring.Delete(); err != nil {
		return err
	}
	fmt.Fprintln(out, "Vault locked.")
	return nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./cmd/bkmr/ -v`
Expected: PASS — the six Task 6 tests plus eight new ones.

- [ ] **Step 6: Commit**

```bash
git add cmd/bkmr/
git commit -m "feat(cli): init, unlock and lock with a keychain-cached key"
```

---

### Task 8: add and ls

**Files:**
- Create: `cmd/bkmr/add.go`, `cmd/bkmr/ls.go`
- Test: `cmd/bkmr/add_test.go`, `cmd/bkmr/ls_test.go`

**Interfaces:**
- Consumes: `openVault`, `model.*`, `store.(*Vault).Mutate`.
- Produces:
  - `var readClipboard = clipboard.ReadAll` — a seam tests replace
  - `func runAdd(args []string) error`, `func runLs(args []string) error`
  - `func formatRow(b model.Bookmark) string` — one bookmark as a single stdout line
  - commands `add`, `ls`

Title fetching is deliberately **not** wired up here; Task 9 adds it. That keeps this task's tests free of network concerns.

- [ ] **Step 1: Write the failing tests**

```go
// cmd/bkmr/add_test.go
package main

import (
	"errors"
	"strings"
	"testing"
)

func TestAddStoresAURLWithTags(t *testing.T) {
	newVaultForTest(t, "pw")

	got := capture(t, func() {
		if err := runAdd([]string{"-t", "rust", "-t", "Async", "https://example.com/a"}); err != nil {
			t.Fatalf("runAdd() error = %v", err)
		}
	})
	if !strings.Contains(got, "https://example.com/a") {
		t.Errorf("runAdd() output = %q, want it to echo the URL", got)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 1 {
		t.Fatalf("len(Bookmarks) = %d, want 1", len(c.Bookmarks))
	}
	tags := strings.Join(c.Bookmarks[0].Tags, ",")
	if tags != "async,rust" {
		t.Errorf("Tags = %q, want %q", tags, "async,rust")
	}
}

func TestAddWithNoArgumentReadsTheClipboard(t *testing.T) {
	newVaultForTest(t, "pw")
	old := readClipboard
	readClipboard = func() (string, error) { return "https://clipboard.example/page", nil }
	defer func() { readClipboard = old }()

	if err := runAdd(nil); err != nil {
		t.Fatalf("runAdd() error = %v", err)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 1 || c.Bookmarks[0].URL != "https://clipboard.example/page" {
		t.Fatalf("Bookmarks = %+v, want the clipboard URL", c.Bookmarks)
	}
}

func TestAddRejectsAClipboardThatIsNotAURL(t *testing.T) {
	newVaultForTest(t, "pw")
	old := readClipboard
	readClipboard = func() (string, error) { return "just some copied prose", nil }
	defer func() { readClipboard = old }()

	err := runAdd(nil)
	if err == nil {
		t.Fatal("runAdd() error = nil, want an error for non-URL clipboard contents")
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 0 {
		t.Errorf("len(Bookmarks) = %d, want 0 - nothing should have been saved", len(c.Bookmarks))
	}
}

func TestAddRejectsAnEmptyClipboard(t *testing.T) {
	newVaultForTest(t, "pw")
	old := readClipboard
	readClipboard = func() (string, error) { return "   ", nil }
	defer func() { readClipboard = old }()

	if err := runAdd(nil); err == nil {
		t.Error("runAdd() error = nil, want an error for an empty clipboard")
	}
}

func TestAddMergesADuplicateAndSaysSo(t *testing.T) {
	newVaultForTest(t, "pw")
	if err := runAdd([]string{"-t", "rust", "https://example.com/a"}); err != nil {
		t.Fatal(err)
	}

	got := capture(t, func() {
		if err := runAdd([]string{"-t", "async", "https://example.com/a?utm_source=news"}); err != nil {
			t.Fatalf("runAdd() error = %v", err)
		}
	})
	if !strings.Contains(strings.ToLower(got), "already") {
		t.Errorf("runAdd() output = %q, want it to say the bookmark already existed", got)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 1 {
		t.Fatalf("len(Bookmarks) = %d, want 1", len(c.Bookmarks))
	}
	if strings.Join(c.Bookmarks[0].Tags, ",") != "async,rust" {
		t.Errorf("Tags = %v, want [async rust]", c.Bookmarks[0].Tags)
	}
}

func TestAddAcceptsTitleAndNoteFlags(t *testing.T) {
	newVaultForTest(t, "pw")

	if err := runAdd([]string{"--title", "The Page", "--note", "read later", "https://example.com"}); err != nil {
		t.Fatal(err)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if c.Bookmarks[0].Title != "The Page" {
		t.Errorf("Title = %q, want %q", c.Bookmarks[0].Title, "The Page")
	}
	if c.Bookmarks[0].Notes != "read later" {
		t.Errorf("Notes = %q, want %q", c.Bookmarks[0].Notes, "read later")
	}
}

func TestAddWithoutAVaultReportsTheInitHint(t *testing.T) {
	t.Setenv("BKMR_DATA_DIR", t.TempDir())

	err := runAdd([]string{"https://example.com"})
	if err == nil || !strings.Contains(err.Error(), "bkmr init") {
		t.Errorf("runAdd() error = %v, want it to mention 'bkmr init'", err)
	}
}

func TestAddRejectsTwoURLs(t *testing.T) {
	newVaultForTest(t, "pw")

	if err := runAdd([]string{"https://a.example", "https://b.example"}); !errors.Is(err, errUsage) {
		t.Errorf("runAdd() error = %v, want errUsage", err)
	}
}
```

```go
// cmd/bkmr/ls_test.go
package main

import (
	"strings"
	"testing"
)

func TestLsPrintsOneLinePerBookmark(t *testing.T) {
	newVaultForTest(t, "pw")
	runAdd([]string{"-t", "rust", "--title", "Rust Book", "https://doc.rust-lang.org/book/"})
	runAdd([]string{"-t", "go", "--title", "Go Docs", "https://go.dev/doc/"})

	got := capture(t, func() {
		if err := runLs(nil); err != nil {
			t.Fatalf("runLs() error = %v", err)
		}
	})

	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("runLs() printed %d lines, want 2:\n%s", len(lines), got)
	}
	if !strings.Contains(got, "Rust Book") || !strings.Contains(got, "https://go.dev/doc/") {
		t.Errorf("runLs() = %q, want both bookmarks", got)
	}
}

func TestLsFiltersByTag(t *testing.T) {
	newVaultForTest(t, "pw")
	runAdd([]string{"-t", "rust", "https://a.example"})
	runAdd([]string{"-t", "go", "https://b.example"})

	got := capture(t, func() {
		if err := runLs([]string{"--tag", "rust"}); err != nil {
			t.Fatalf("runLs() error = %v", err)
		}
	})

	if !strings.Contains(got, "a.example") {
		t.Errorf("runLs(--tag rust) = %q, want the rust bookmark", got)
	}
	if strings.Contains(got, "b.example") {
		t.Errorf("runLs(--tag rust) = %q, want the go bookmark excluded", got)
	}
}

func TestLsOnAnEmptyVaultSaysSo(t *testing.T) {
	newVaultForTest(t, "pw")

	got := capture(t, func() {
		if err := runLs(nil); err != nil {
			t.Fatalf("runLs() error = %v", err)
		}
	})

	if strings.TrimSpace(got) == "" {
		t.Error("runLs() on an empty vault printed nothing; want a friendly message")
	}
}

func TestLsShowsTheURLWhenThereIsNoTitle(t *testing.T) {
	newVaultForTest(t, "pw")
	runAdd([]string{"https://untitled.example/page"})

	got := capture(t, func() { runLs(nil) })

	if !strings.Contains(got, "https://untitled.example/page") {
		t.Errorf("runLs() = %q, want the URL used as the label", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/bkmr/ -run 'TestAdd|TestLs' -v`
Expected: FAIL — `undefined: runAdd`, `undefined: readClipboard`.

- [ ] **Step 3: Add the clipboard dependency**

```bash
go get github.com/atotto/clipboard@latest
```

- [ ] **Step 4: Write add**

```go
// cmd/bkmr/add.go
package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/atotto/clipboard"

	"github.com/FredrikSchold/bookmrkrr/internal/model"
)

// readClipboard is a seam so tests do not touch the real clipboard.
var readClipboard = clipboard.ReadAll

type tagList []string

func (t *tagList) String() string     { return strings.Join(*t, ",") }
func (t *tagList) Set(v string) error { *t = append(*t, v); return nil }

func init() {
	register(command{
		Name:    "add",
		Summary: "save a bookmark; with no URL, read the clipboard",
		Usage:   "bkmr add [url] [-t tag]... [--title text] [--note text] [--no-fetch]",
		Run:     runAdd,
	})
}

func runAdd(args []string) error {
	var tags tagList
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Var(&tags, "t", "tag (repeatable)")
	fs.Var(&tags, "tag", "tag (repeatable)")
	title := fs.String("title", "", "title")
	note := fs.String("note", "", "note")
	noFetch := fs.Bool("no-fetch", false, "do not fetch the page title")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	if fs.NArg() > 1 {
		return errUsage
	}

	raw := ""
	fromClipboard := false
	if fs.NArg() == 1 {
		raw = fs.Arg(0)
	} else {
		var err error
		raw, err = readClipboard()
		if err != nil {
			return fmt.Errorf("read clipboard: %w", err)
		}
		fromClipboard = true
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("nothing to add: %s", source(fromClipboard))
	}
	if _, err := model.NormalizeURL(raw); err != nil {
		return fmt.Errorf("%s is not a URL: %q", source(fromClipboard), raw)
	}

	v, err := openVault()
	if err != nil {
		return err
	}

	b := model.Bookmark{URL: raw, Title: *title, Notes: *note, Tags: tags}
	if !*noFetch {
		b.Title = resolveTitle(b.Title, raw)
	}

	var stored model.Bookmark
	var merged bool
	if err := v.Mutate(func(c *model.Collection) error {
		var err error
		stored, merged, err = c.Add(b)
		return err
	}); err != nil {
		return err
	}

	if merged {
		fmt.Fprintf(out, "already saved as %s; tags are now %s\n", stored.ID, tagsOrNone(stored.Tags))
		return nil
	}
	fmt.Fprintf(out, "saved %s  %s\n", stored.ID, label(stored))
	return nil
}

func source(fromClipboard bool) string {
	if fromClipboard {
		return "the clipboard"
	}
	return "that argument"
}

func tagsOrNone(tags []string) string {
	if len(tags) == 0 {
		return "(none)"
	}
	return strings.Join(tags, ", ")
}

func label(b model.Bookmark) string {
	if b.Title != "" {
		return b.Title
	}
	return b.URL
}

// resolveTitle is replaced with a real implementation in Task 9. Until then a
// supplied title is kept and nothing is fetched.
func resolveTitle(given, _ string) string { return given }
```

- [ ] **Step 5: Write ls**

```go
// cmd/bkmr/ls.go
package main

import (
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/FredrikSchold/bookmrkrr/internal/model"
)

func init() {
	register(command{
		Name:    "ls",
		Summary: "print bookmarks to stdout, newest first",
		Usage:   "bkmr ls [--tag name]",
		Run:     runLs,
	})
}

func runLs(args []string) error {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	tag := fs.String("tag", "", "only bookmarks carrying this tag")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}

	v, err := openVault()
	if err != nil {
		return err
	}
	c, err := v.Load()
	if err != nil {
		return err
	}

	rows := filterByTag(c.Bookmarks, *tag)
	if len(rows) == 0 {
		if *tag != "" {
			fmt.Fprintf(out, "no bookmarks tagged %q\n", *tag)
		} else {
			fmt.Fprintln(out, "no bookmarks yet - add one with 'bkmr add <url>'")
		}
		return nil
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Added.After(rows[j].Added) })
	for _, b := range rows {
		fmt.Fprintln(out, formatRow(b))
	}
	return nil
}

func filterByTag(all []model.Bookmark, tag string) []model.Bookmark {
	if tag == "" {
		return append([]model.Bookmark(nil), all...)
	}
	want := model.NormalizeTags([]string{tag})
	if len(want) == 0 {
		return nil
	}
	var rows []model.Bookmark
	for _, b := range all {
		for _, t := range b.Tags {
			if t == want[0] {
				rows = append(rows, b)
				break
			}
		}
	}
	return rows
}

func formatRow(b model.Bookmark) string {
	row := fmt.Sprintf("%s  %s  %s", b.ID, label(b), b.URL)
	if len(b.Tags) > 0 {
		row += "  [" + strings.Join(b.Tags, " ") + "]"
	}
	return row
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./cmd/bkmr/ -v`
Expected: PASS — twelve new tests alongside the earlier ones.

- [ ] **Step 7: Commit**

```bash
git add cmd/bkmr/ go.mod go.sum
git commit -m "feat(cli): add from argument or clipboard, and ls with tag filtering"
```

---

### Task 9: Title fetching and the network kill switch

**Files:**
- Create: `internal/fetch/title.go`, `internal/config/config.go`
- Modify: `cmd/bkmr/add.go` — replace the `resolveTitle` stub
- Test: `internal/fetch/title_test.go`, `internal/config/config_test.go`, `cmd/bkmr/fetch_test.go`

**Interfaces:**
- Consumes: `config.ConfigPath`.
- Produces:
  - `fetch.Title(ctx context.Context, rawURL string) (string, error)`
  - `fetch.MaxTitleRunes = 300`
  - `config.Config{Network string}` with `config.Load() Config` — never fails; a missing or broken file yields the default
  - `(Config).NetworkEnabled() bool`

- [ ] **Step 1: Write the failing tests**

```go
// internal/fetch/title_test.go
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
```

```go
// internal/config/config_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsToNetworkOn(t *testing.T) {
	t.Setenv("BKMR_DATA_DIR", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())

	cfg := Load()
	if !cfg.NetworkEnabled() {
		t.Error("NetworkEnabled() = false, want true by default")
	}
}

func TestLoadReadsNetworkOff(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BKMR_DATA_DIR", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)

	path, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("network = \"off\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if Load().NetworkEnabled() {
		t.Error("NetworkEnabled() = true, want false when the config says off")
	}
}

func TestLoadIgnoresAMalformedConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BKMR_DATA_DIR", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)

	path, _ := ConfigPath()
	os.MkdirAll(filepath.Dir(path), 0o700)
	if err := os.WriteFile(path, []byte("this is not = = toml ["), 0o600); err != nil {
		t.Fatal(err)
	}

	if !Load().NetworkEnabled() {
		t.Error("a malformed config must fall back to the default, not disable the network silently")
	}
}
```

```go
// cmd/bkmr/fetch_test.go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAddFetchesTheTitleWhenNoneIsGiven(t *testing.T) {
	newVaultForTest(t, "pw")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<title>Fetched Title</title>"))
	}))
	defer srv.Close()

	if err := runAdd([]string{srv.URL}); err != nil {
		t.Fatalf("runAdd() error = %v", err)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if c.Bookmarks[0].Title != "Fetched Title" {
		t.Errorf("Title = %q, want %q", c.Bookmarks[0].Title, "Fetched Title")
	}
}

func TestAddWithNoFetchSkipsTheNetwork(t *testing.T) {
	newVaultForTest(t, "pw")
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<title>Should Not Be Fetched</title>"))
	}))
	defer srv.Close()

	if err := runAdd([]string{"--no-fetch", srv.URL}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("--no-fetch still made a request")
	}

	v, _ := openVault()
	c, _ := v.Load()
	if c.Bookmarks[0].Title != "" {
		t.Errorf("Title = %q, want empty with --no-fetch", c.Bookmarks[0].Title)
	}
}

// The spec's hardest rule: network failure must never lose a bookmark.
func TestAddSavesTheBookmarkWhenTheFetchFails(t *testing.T) {
	newVaultForTest(t, "pw")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	if err := runAdd([]string{srv.URL}); err != nil {
		t.Fatalf("runAdd() error = %v, want the bookmark saved despite the fetch failing", err)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 1 {
		t.Fatalf("len(Bookmarks) = %d, want 1", len(c.Bookmarks))
	}
	if c.Bookmarks[0].Title != "" {
		t.Errorf("Title = %q, want empty after a failed fetch", c.Bookmarks[0].Title)
	}
}

func TestAddDoesNotFetchWhenAnExplicitTitleIsGiven(t *testing.T) {
	newVaultForTest(t, "pw")
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	if err := runAdd([]string{"--title", "Mine", srv.URL}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("an explicit --title still triggered a fetch")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/fetch/ ./internal/config/ ./cmd/bkmr/ -run 'TestTitle|TestLoad|TestAddFetch|TestAddWithNoFetch|TestAddSaves|TestAddDoesNot' -v`
Expected: FAIL — `undefined: Title`, `undefined: Load`.

- [ ] **Step 3: Add the TOML dependency**

```bash
go get github.com/BurntSushi/toml@latest
```

- [ ] **Step 4: Write the config loader**

```go
// internal/config/config.go
package config

import (
	"os"

	"github.com/BurntSushi/toml"
)

// Config is the user's config.toml. Every field has a working default, so a
// missing or malformed file is never fatal.
type Config struct {
	// Network is "on" (the default) or "off". Off disables title fetching
	// entirely, which leaves the binary making no outbound connections.
	Network string `toml:"network"`
}

// Default is the configuration used when no file is present.
var Default = Config{Network: "on"}

// Load reads config.toml. Any problem - missing file, bad TOML, unreadable
// path - yields Default rather than an error, because a config problem must
// not stop someone saving a bookmark.
func Load() Config {
	path, err := ConfigPath()
	if err != nil {
		return Default
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Default
	}
	cfg := Default
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Default
	}
	if cfg.Network == "" {
		cfg.Network = Default.Network
	}
	return cfg
}

// NetworkEnabled reports whether outbound requests are permitted.
func (c Config) NetworkEnabled() bool { return c.Network != "off" }
```

- [ ] **Step 5: Write the fetcher**

```go
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
	"strings"
)

const (
	// MaxTitleRunes caps a title so a hostile page cannot bloat the vault.
	MaxTitleRunes = 300
	maxBodyBytes  = 64 << 10
	maxRedirects  = 3
	userAgent     = "bkmr/0.1 (+https://github.com/FredrikSchold/bookmrkrr)"
)

// Title fetches rawURL and returns its page title, or an empty string when the
// page has no usable title. Callers must treat an error as cosmetic: a
// bookmark is never lost because a title could not be read.
func Title(ctx context.Context, rawURL string) (string, error) {
	client := &http.Client{
		// No cookie jar: nil Jar means no cookies are ever sent or stored.
		Jar: nil,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			if req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("refusing to follow a redirect off %s to %s", via[0].URL.Host, req.URL.Host)
			}
			return nil
		},
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
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "text/html") {
		return "", nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return titleFrom(string(body)), nil
}

// titleFrom extracts and cleans the contents of the first <title> element.
//
// ponytail: a string scan, not an HTML parser. Upgrade to x/net/html only if a
// real page defeats it - that would also mean a ninth dependency.
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

	title := strings.Join(strings.Fields(html.UnescapeString(body[start:start+end])), " ")
	if r := []rune(title); len(r) > MaxTitleRunes {
		title = strings.TrimSpace(string(r[:MaxTitleRunes]))
	}
	return title
}
```

- [ ] **Step 6: Wire it into add**

Replace the `resolveTitle` stub at the bottom of `cmd/bkmr/add.go` with:

```go
// resolveTitle returns the given title, or fetches one when the title is
// empty and the network is enabled. A fetch failure is reported on stderr and
// otherwise ignored: losing a bookmark because a site was down is never
// acceptable.
func resolveTitle(given, rawURL string) string {
	if given != "" {
		return given
	}
	if !config.Load().NetworkEnabled() {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	title, err := fetch.Title(ctx, rawURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bkmr: could not read the page title (%v); saving without one\n", err)
		return ""
	}
	return title
}
```

and extend that file's imports with `context`, `os`, `time`, `github.com/FredrikSchold/bookmrkrr/internal/config` and `github.com/FredrikSchold/bookmrkrr/internal/fetch`.

- [ ] **Step 7: Run the full suite**

Run: `go test ./... -v`
Expected: PASS — everything, including the four new `cmd/bkmr` fetch tests.

- [ ] **Step 8: Commit**

```bash
git add internal/fetch/ internal/config/ cmd/bkmr/ go.mod go.sum
git commit -m "feat(fetch): title fetching with a narrow network policy and a config kill switch"
```


---

### Task 10: The fuzzy picker, bare `bkmr`, and `open`

**Files:**
- Create: `internal/tui/picker.go`, `internal/tui/browser.go`
- Create: `cmd/bkmr/picker.go`, `cmd/bkmr/open.go`
- Test: `internal/tui/picker_test.go`, `cmd/bkmr/open_test.go`

**Interfaces:**
- Consumes: `model.*`, `store.(*Vault).Mutate`.
- Produces:
  - `tui.Item{ID, Label, Detail, Filter string, Tags []string}` — `Filter` is the haystack fuzzy matching runs against; `ID` identifies the chosen row; `Tags` feeds tag mode (Step 4b)
  - `(Model) TagMode() bool` and `(Model) ActiveTag() string` — tag mode state (Step 4b)
  - `tui.Action` with constants `tui.ActionNone`, `tui.ActionOpen`, `tui.ActionCopy`, `tui.ActionDelete`
  - `tui.Model` with `tui.New(items []Item, prompt string) Model`
  - `(Model) Visible() []Item` — the current filtered, ranked rows
  - `(Model) Chosen() (Item, Action)` — the row and action the user settled on
  - `(Model) Init/Update/View` — the bubbletea contract
  - `tui.Run(m Model) (Item, Action, error)` — runs the program and returns the outcome
  - `tui.OpenInBrowser(url string) error`
  - `var tui.Open = OpenInBrowser` — a seam tests replace

- [ ] **Step 1: Write the failing tests**

```go
// internal/tui/picker_test.go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func sample() []Item {
	return []Item{
		{ID: "aaa", Label: "Rust Book", Detail: "https://doc.rust-lang.org/book/", Filter: "rust book doc.rust-lang.org rust"},
		{ID: "bbb", Label: "Go Docs", Detail: "https://go.dev/doc/", Filter: "go docs go.dev golang"},
		{ID: "ccc", Label: "Ratatui", Detail: "https://ratatui.rs", Filter: "ratatui ratatui.rs rust tui"},
	}
}

func typeRunes(m Model, s string) Model {
	for _, r := range s {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}
	return m
}

func sizeIt(m Model, w, h int) Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(Model)
}

func TestAllItemsVisibleBeforeTyping(t *testing.T) {
	m := sizeIt(New(sample(), "search"), 80, 24)

	if len(m.Visible()) != 3 {
		t.Errorf("Visible() = %d items, want 3", len(m.Visible()))
	}
}

func TestTypingFiltersFuzzily(t *testing.T) {
	m := typeRunes(sizeIt(New(sample(), "search"), 80, 24), "rst")

	vis := m.Visible()
	if len(vis) == 0 {
		t.Fatal("Visible() = 0 items, want the fuzzy matches for 'rst'")
	}
	for _, it := range vis {
		if !strings.Contains(it.Filter, "rust") && !strings.Contains(it.Filter, "ratatui") {
			t.Errorf("Visible() included %q, which does not fuzzy-match 'rst'", it.Label)
		}
	}
}

func TestFilteringToNothingLeavesNoSelection(t *testing.T) {
	m := typeRunes(sizeIt(New(sample(), "search"), 80, 24), "zzzzzz")

	if len(m.Visible()) != 0 {
		t.Fatalf("Visible() = %d items, want 0", len(m.Visible()))
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if _, action := m.Chosen(); action != ActionNone {
		t.Errorf("Enter on an empty list chose an action %v, want ActionNone", action)
	}
}

func TestEnterChoosesTheHighlightedItemToOpen(t *testing.T) {
	m := sizeIt(New(sample(), "search"), 80, 24)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	item, action := m.Chosen()
	if action != ActionOpen {
		t.Errorf("action = %v, want ActionOpen", action)
	}
	if item.ID != "bbb" {
		t.Errorf("chosen ID = %q, want %q after one Down", item.ID, "bbb")
	}
	if !m.Done() {
		t.Error("Done() = false after Enter, want true")
	}
}

func TestCtrlCAndEscQuitWithoutChoosing(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyEsc, tea.KeyCtrlC} {
		m := sizeIt(New(sample(), "search"), 80, 24)
		next, _ := m.Update(tea.KeyMsg{Type: key})
		m = next.(Model)

		if _, action := m.Chosen(); action != ActionNone {
			t.Errorf("key %v chose an action, want ActionNone", key)
		}
		if !m.Done() {
			t.Errorf("Done() = false after %v, want true", key)
		}
	}
}

func TestCopyAndDeleteActions(t *testing.T) {
	m := sizeIt(New(sample(), "search"), 80, 24)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	if _, action := next.(Model).Chosen(); action != ActionCopy {
		t.Errorf("ctrl+y action = %v, want ActionCopy", action)
	}

	m = sizeIt(New(sample(), "search"), 80, 24)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if _, action := next.(Model).Chosen(); action != ActionDelete {
		t.Errorf("ctrl+d action = %v, want ActionDelete", action)
	}
}

func TestSelectionStaysInBounds(t *testing.T) {
	m := sizeIt(New(sample(), "search"), 80, 24)
	for i := 0; i < 10; i++ {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(Model)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if item, _ := next.(Model).Chosen(); item.ID != "ccc" {
		t.Errorf("chosen ID = %q, want the last item after many Downs", item.ID)
	}

	m = sizeIt(New(sample(), "search"), 80, 24)
	for i := 0; i < 10; i++ {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = next.(Model)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if item, _ := next.(Model).Chosen(); item.ID != "aaa" {
		t.Errorf("chosen ID = %q, want the first item after many Ups", item.ID)
	}
}

// Review Focus 4: a zero-height or absurdly small window must not panic.
func TestViewSurvivesADegenerateWindow(t *testing.T) {
	for _, size := range [][2]int{{0, 0}, {1, 1}, {80, 2}, {3, 0}} {
		m := sizeIt(New(sample(), "search"), size[0], size[1])
		if got := m.View(); got == "" && size[0] > 3 {
			t.Errorf("View() at %dx%d = empty", size[0], size[1])
		}
	}
}

func TestViewOnAnEmptyCollectionExplainsItself(t *testing.T) {
	m := sizeIt(New(nil, "search"), 80, 24)

	got := m.View()
	if !strings.Contains(strings.ToLower(got), "no bookmarks") {
		t.Errorf("View() with no items = %q, want it to say there are no bookmarks", got)
	}
}

func TestBackspaceRestoresFilteredItems(t *testing.T) {
	m := typeRunes(sizeIt(New(sample(), "search"), 80, 24), "go")
	if len(m.Visible()) == 3 {
		t.Fatal("typing 'go' did not narrow the list")
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(Model)

	if len(m.Visible()) != 3 {
		t.Errorf("Visible() = %d after clearing the query, want 3", len(m.Visible()))
	}
}
```

```go
// cmd/bkmr/open_test.go
package main

import (
	"strings"
	"testing"

	"github.com/FredrikSchold/bookmrkrr/internal/tui"
)

func TestOpenFindsTheBestMatchAndOpensIt(t *testing.T) {
	newVaultForTest(t, "pw")
	runAdd([]string{"--no-fetch", "--title", "Rust Book", "https://doc.rust-lang.org/book/"})
	runAdd([]string{"--no-fetch", "--title", "Go Docs", "https://go.dev/doc/"})

	var opened string
	old := tui.Open
	tui.Open = func(url string) error { opened = url; return nil }
	defer func() { tui.Open = old }()

	if err := runOpen([]string{"rust"}); err != nil {
		t.Fatalf("runOpen() error = %v", err)
	}
	if opened != "https://doc.rust-lang.org/book/" {
		t.Errorf("opened %q, want the Rust Book URL", opened)
	}
}

func TestOpenAcceptsAnID(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://byid.example"}) })

	v, _ := openVault()
	c, _ := v.Load()
	id := c.Bookmarks[0].ID

	var opened string
	old := tui.Open
	tui.Open = func(url string) error { opened = url; return nil }
	defer func() { tui.Open = old }()

	if err := runOpen([]string{id}); err != nil {
		t.Fatalf("runOpen() error = %v", err)
	}
	if opened != "https://byid.example" {
		t.Errorf("opened %q, want https://byid.example", opened)
	}
}

func TestOpenRecordsTheVisit(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://visited.example"}) })

	old := tui.Open
	tui.Open = func(string) error { return nil }
	defer func() { tui.Open = old }()

	if err := runOpen([]string{"visited"}); err != nil {
		t.Fatal(err)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if c.Bookmarks[0].Opens != 1 {
		t.Errorf("Opens = %d, want 1", c.Bookmarks[0].Opens)
	}
	if c.Bookmarks[0].Visited == nil {
		t.Error("Visited = nil, want a timestamp after opening")
	}
}

func TestOpenWithNoMatchFails(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://example.com"}) })

	err := runOpen([]string{"nothingmatchesthis"})
	if err == nil || !strings.Contains(err.Error(), "no bookmark") {
		t.Errorf("runOpen() error = %v, want a no-match error", err)
	}
}

func TestOpenWithNoArgumentIsAUsageError(t *testing.T) {
	newVaultForTest(t, "pw")

	if err := runOpen(nil); err == nil {
		t.Error("runOpen(nil) error = nil, want a usage error")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ ./cmd/bkmr/ -run 'Test.*Visible|TestTyping|TestEnter|TestCtrlC|TestCopy|TestSelection|TestView|TestBackspace|TestFiltering|TestOpen' -v`
Expected: FAIL — the `internal/tui` package does not exist.

- [ ] **Step 3: Add the TUI dependencies**

```bash
go get github.com/charmbracelet/bubbletea@latest
go get github.com/charmbracelet/bubbles@latest
go get github.com/charmbracelet/lipgloss@latest
go get github.com/sahilm/fuzzy@latest
```

- [ ] **Step 4: Write the picker**

```go
// Package tui holds one reusable fuzzy picker. Both bookmark retrieval and
// browser-tab capture use it, so there is exactly one list widget to maintain.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

// Action is what the user asked to do with the row they chose.
type Action int

const (
	ActionNone Action = iota
	ActionOpen
	ActionCopy
	ActionDelete
)

// Item is one row. Filter is the haystack fuzzy matching runs against.
type Item struct {
	ID     string
	Label  string
	Detail string
	Filter string
}

var (
	styleSelected = lipgloss.NewStyle().Bold(true).Reverse(true)
	styleDetail   = lipgloss.NewStyle().Faint(true)
	styleHelp     = lipgloss.NewStyle().Faint(true)
)

// Model is the picker's bubbletea model.
type Model struct {
	prompt  string
	all     []Item
	visible []Item
	cursor  int
	input   textinput.Model
	width   int
	height  int
	chosen  Item
	action  Action
	done    bool
}

// New builds a picker over items.
func New(items []Item, prompt string) Model {
	in := textinput.New()
	in.Prompt = prompt + " "
	in.Focus()

	m := Model{prompt: prompt, all: items, input: in, width: 80, height: 24}
	m.refilter()
	return m
}

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd { return textinput.Blink }

// Visible returns the current filtered, ranked rows.
func (m Model) Visible() []Item { return m.visible }

// Done reports whether the picker has finished.
func (m Model) Done() bool { return m.done }

// Chosen returns the selected row and the action requested. The action is
// ActionNone when the user quit without choosing.
func (m Model) Chosen() (Item, Action) { return m.chosen, m.action }

func (m *Model) refilter() {
	q := strings.TrimSpace(m.input.Value())
	if q == "" {
		m.visible = m.all
	} else {
		hay := make([]string, len(m.all))
		for i, it := range m.all {
			hay[i] = it.Filter
		}
		matches := fuzzy.Find(q, hay)
		m.visible = make([]Item, 0, len(matches))
		for _, mt := range matches {
			m.visible = append(m.visible, m.all[mt.Index])
		}
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m Model) finish(a Action) (tea.Model, tea.Cmd) {
	if a != ActionNone && m.cursor < len(m.visible) {
		m.chosen = m.visible[m.cursor]
		m.action = a
	}
	m.done = true
	return m, tea.Quit
}

// Update satisfies tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			return m.finish(ActionNone)
		case tea.KeyEnter:
			return m.finish(ActionOpen)
		case tea.KeyCtrlY:
			return m.finish(ActionCopy)
		case tea.KeyCtrlD:
			return m.finish(ActionDelete)
		case tea.KeyUp, tea.KeyCtrlP:
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case tea.KeyDown, tea.KeyCtrlN:
			if m.cursor < len(m.visible)-1 {
				m.cursor++
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.refilter()
	return m, cmd
}

// View satisfies tea.Model. It must tolerate a degenerate window size.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(m.input.View())
	b.WriteString("\n")

	if len(m.all) == 0 {
		b.WriteString("no bookmarks yet - add one with 'bkmr add <url>'\n")
		return b.String()
	}

	// Reserve the query line, the help line, and one blank.
	rows := m.height - 3
	if rows < 1 {
		rows = 1
	}
	if rows > len(m.visible) {
		rows = len(m.visible)
	}

	start := 0
	if m.cursor >= rows {
		start = m.cursor - rows + 1
	}
	for i := start; i < start+rows && i < len(m.visible); i++ {
		it := m.visible[i]
		line := fmt.Sprintf("%s  %s", it.Label, styleDetail.Render(it.Detail))
		if i == m.cursor {
			line = styleSelected.Render(" " + it.Label + " ") + " " + styleDetail.Render(it.Detail)
		}
		b.WriteString(truncate(line, m.width) + "\n")
	}
	if len(m.visible) == 0 {
		b.WriteString("no matches\n")
	}
	b.WriteString(styleHelp.Render("enter open · ctrl+y copy · ctrl+d delete · esc quit"))
	return b.String()
}

func truncate(s string, width int) string {
	if width <= 0 {
		return s
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	r := []rune(s)
	if len(r) > width {
		r = r[:width]
	}
	return string(r)
}

// Run displays the picker and returns what the user chose.
func Run(m Model) (Item, Action, error) {
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return Item{}, ActionNone, err
	}
	fm, ok := final.(Model)
	if !ok {
		return Item{}, ActionNone, fmt.Errorf("unexpected final model %T", final)
	}
	item, action := fm.Chosen()
	return item, action, nil
}
```

- [ ] **Step 4b: Add tag mode (spec §8)**

Tags are one of only two retrieval paths in the spec, so the picker needs an
explicit tag view, not just tags folded into the fuzzy haystack.

First the tests:

```go
// internal/tui/tagmode_test.go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func tagged() []Item {
	return []Item{
		{ID: "aaa", Label: "Rust Book", Detail: "https://doc.rust-lang.org/book/", Filter: "rust book", Tags: []string{"rust", "reading"}},
		{ID: "bbb", Label: "Go Docs", Detail: "https://go.dev/doc/", Filter: "go docs", Tags: []string{"go"}},
		{ID: "ccc", Label: "Ratatui", Detail: "https://ratatui.rs", Filter: "ratatui", Tags: []string{"rust"}},
		{ID: "ddd", Label: "Untagged", Detail: "https://plain.example", Filter: "untagged"},
	}
}

func press(m Model, k tea.KeyType) Model {
	next, _ := m.Update(tea.KeyMsg{Type: k})
	return next.(Model)
}

func TestTabEntersTagModeListingTagsByCount(t *testing.T) {
	m := press(sizeIt(New(tagged(), "search"), 80, 24), tea.KeyTab)

	if !m.TagMode() {
		t.Fatal("TagMode() = false after Tab, want true")
	}
	vis := m.Visible()
	if len(vis) != 3 {
		t.Fatalf("Visible() = %d tag rows, want 3 (rust, go, reading):\n%+v", len(vis), vis)
	}
	if vis[0].Label != "rust" {
		t.Errorf("Visible()[0] = %q, want rust first - it has the highest count", vis[0].Label)
	}
	if !strings.Contains(vis[0].Detail, "2") {
		t.Errorf("Visible()[0].Detail = %q, want it to show a count of 2", vis[0].Detail)
	}
}

func TestChoosingATagFiltersTheBookmarkList(t *testing.T) {
	m := press(sizeIt(New(tagged(), "search"), 80, 24), tea.KeyTab)
	m = press(m, tea.KeyEnter) // pick "rust"

	if m.TagMode() {
		t.Error("TagMode() = true after choosing a tag, want false")
	}
	if m.ActiveTag() != "rust" {
		t.Errorf("ActiveTag() = %q, want %q", m.ActiveTag(), "rust")
	}
	if m.Done() {
		t.Error("Done() = true after choosing a tag; choosing a tag must not end the picker")
	}

	vis := m.Visible()
	if len(vis) != 2 {
		t.Fatalf("Visible() = %d items, want the 2 rust bookmarks:\n%+v", len(vis), vis)
	}
	for _, it := range vis {
		if it.ID == "bbb" || it.ID == "ddd" {
			t.Errorf("Visible() included %q, which is not tagged rust", it.Label)
		}
	}
}

func TestTagFilterAndTypedQueryCombine(t *testing.T) {
	m := press(sizeIt(New(tagged(), "search"), 80, 24), tea.KeyTab)
	m = press(m, tea.KeyEnter) // rust
	m = typeRunes(m, "rata")

	vis := m.Visible()
	if len(vis) != 1 || vis[0].ID != "ccc" {
		t.Errorf("Visible() = %+v, want only Ratatui", vis)
	}
}

func TestTabAgainClearsAnActiveTagFilter(t *testing.T) {
	m := press(sizeIt(New(tagged(), "search"), 80, 24), tea.KeyTab)
	m = press(m, tea.KeyEnter) // rust
	m = press(m, tea.KeyTab)   // clear it

	if m.ActiveTag() != "" {
		t.Errorf("ActiveTag() = %q, want it cleared", m.ActiveTag())
	}
	if m.TagMode() {
		t.Error("TagMode() = true, want false - Tab on an active filter clears it rather than re-entering tag mode")
	}
	if len(m.Visible()) != 4 {
		t.Errorf("Visible() = %d items, want all 4 back", len(m.Visible()))
	}
}

func TestEscLeavesTagModeWithoutQuitting(t *testing.T) {
	m := press(sizeIt(New(tagged(), "search"), 80, 24), tea.KeyTab)
	m = press(m, tea.KeyEsc)

	if m.TagMode() {
		t.Error("TagMode() = true after Esc, want false")
	}
	if m.Done() {
		t.Error("Done() = true after Esc in tag mode; Esc must only leave tag mode")
	}
	if m.ActiveTag() != "" {
		t.Errorf("ActiveTag() = %q, want none applied", m.ActiveTag())
	}
}

func TestEnterInTagModeOnAnEmptyTagListDoesNothing(t *testing.T) {
	m := press(sizeIt(New([]Item{{ID: "x", Label: "No tags", Filter: "no tags"}}, "search"), 80, 24), tea.KeyTab)

	if len(m.Visible()) != 0 {
		t.Fatalf("Visible() = %d tag rows, want 0", len(m.Visible()))
	}
	m = press(m, tea.KeyEnter)
	if m.ActiveTag() != "" {
		t.Errorf("ActiveTag() = %q, want none", m.ActiveTag())
	}
	if m.Done() {
		t.Error("Done() = true, want false")
	}
}
```

Then the implementation changes to `internal/tui/picker.go`:

Add `Tags` to `Item`:

```go
// Item is one row. Filter is the haystack fuzzy matching runs against.
type Item struct {
	ID     string
	Label  string
	Detail string
	Filter string
	Tags   []string
}
```

Add three fields to `Model`, after `done bool`:

```go
	tagMode   bool
	activeTag string
	tagRows   []Item
```

Add the accessors and the tag-row builder:

```go
// TagMode reports whether the picker is showing tags rather than bookmarks.
func (m Model) TagMode() bool { return m.tagMode }

// ActiveTag is the tag currently narrowing the list, or "".
func (m Model) ActiveTag() string { return m.activeTag }

// buildTagRows lists every tag with its count, most-used first and
// alphabetical within a count.
func (m *Model) buildTagRows() {
	counts := map[string]int{}
	for _, it := range m.all {
		for _, t := range it.Tags {
			counts[t]++
		}
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})

	m.tagRows = make([]Item, 0, len(names))
	for _, name := range names {
		m.tagRows = append(m.tagRows, Item{
			ID:     name,
			Label:  name,
			Detail: fmt.Sprintf("%d bookmarks", counts[name]),
			Filter: name,
		})
	}
}
```

Add `"sort"` to the file's imports, and call `m.buildTagRows()` inside `New`, immediately before `m.refilter()`.

Replace `refilter` so it honors both the tag mode and the active tag:

```go
func (m *Model) refilter() {
	pool := m.all
	if m.tagMode {
		pool = m.tagRows
	} else if m.activeTag != "" {
		pool = pool[:0:0]
		for _, it := range m.all {
			for _, t := range it.Tags {
				if t == m.activeTag {
					pool = append(pool, it)
					break
				}
			}
		}
	}

	q := strings.TrimSpace(m.input.Value())
	if q == "" {
		m.visible = pool
	} else {
		hay := make([]string, len(pool))
		for i, it := range pool {
			hay[i] = it.Filter
		}
		matches := fuzzy.Find(q, hay)
		m.visible = make([]Item, 0, len(matches))
		for _, mt := range matches {
			m.visible = append(m.visible, pool[mt.Index])
		}
	}

	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}
```

Note the `pool[:0:0]` slice: it shares no backing array with `m.all`, so filtering never overwrites the caller's items.

Then handle the keys. In `Update`'s `tea.KeyMsg` switch, replace the `tea.KeyEsc, tea.KeyCtrlC` and `tea.KeyEnter` cases and add a `tea.KeyTab` case:

```go
		case tea.KeyCtrlC:
			return m.finish(ActionNone)
		case tea.KeyEsc:
			// In tag mode Esc only leaves tag mode; otherwise it quits.
			if m.tagMode {
				m.tagMode = false
				m.input.SetValue("")
				m.cursor = 0
				m.refilter()
				return m, nil
			}
			return m.finish(ActionNone)
		case tea.KeyTab:
			if m.activeTag != "" {
				m.activeTag = ""
			} else {
				m.tagMode = !m.tagMode
			}
			m.input.SetValue("")
			m.cursor = 0
			m.refilter()
			return m, nil
		case tea.KeyEnter:
			if m.tagMode {
				if m.cursor < len(m.visible) {
					m.activeTag = m.visible[m.cursor].ID
					m.tagMode = false
					m.input.SetValue("")
					m.cursor = 0
					m.refilter()
				}
				return m, nil
			}
			return m.finish(ActionOpen)
```

Finally, show the state in `View`. Replace the help line and add a mode banner directly after the input line:

```go
	if m.tagMode {
		b.WriteString(styleHelp.Render("tag mode - enter to filter by a tag, tab or esc to go back") + "\n")
	} else if m.activeTag != "" {
		b.WriteString(styleHelp.Render("filtering by tag: "+m.activeTag+" (tab to clear)") + "\n")
	}
```

and change the final help string to:

```go
	b.WriteString(styleHelp.Render("enter open · tab tags · ctrl+y copy · ctrl+d delete · esc quit"))
```

Because the banner consumes a line, change the row budget from `m.height - 3` to `m.height - 4` so a full-height window still fits.

Populate the new field in `cmd/bkmr/picker.go`'s `itemsFor`, adding one line to the `tui.Item` literal:

```go
			Tags:   b.Tags,
```

`cmd/bkmr/tab.go` needs no change: browser tabs have no tags, so tag mode is simply empty there, and `TestEnterInTagModeOnAnEmptyTagListDoesNothing` pins that behavior as harmless.

Run: `go test ./internal/tui/ -v`
Expected: PASS — the ten Step 1 tests plus six tag-mode tests.

A deliberate deviation from spec §8 to note in the PR: the spec assigns bare letters to actions (`o` copy, `e` edit, `d` delete). Bare letters cannot work, because every printable key goes into the filter box as you type. Copy and delete moved to `ctrl+y` and `ctrl+d`. In-picker `$EDITOR` editing is dropped entirely: `bkmr edit <id>` already covers it, and the picker's job is finding things.

- [ ] **Step 5: Write the browser opener**

```go
// internal/tui/browser.go
package tui

import (
	"fmt"
	"os/exec"
	"runtime"
)

// Open launches a URL in the user's default browser. It is a variable so
// tests can replace it.
var Open = OpenInBrowser

// OpenInBrowser hands a URL to the platform's default handler.
func OpenInBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open %s: %w", url, err)
	}
	// Reap the child so it does not linger as a zombie.
	go func() { _ = cmd.Wait() }()
	return nil
}
```

- [ ] **Step 6: Write the picker and open commands**

```go
// cmd/bkmr/picker.go
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"golang.org/x/term"

	"github.com/FredrikSchold/bookmrkrr/internal/model"
	"github.com/FredrikSchold/bookmrkrr/internal/store"
	"github.com/FredrikSchold/bookmrkrr/internal/tui"
)

func init() {
	register(command{
		Name:    "picker",
		Summary: "open the interactive picker (this is what bare 'bkmr' runs)",
		Usage:   "bkmr",
		Run:     runPicker,
	})
}

func runPicker([]string) error {
	v, err := openVault()
	if err != nil {
		return err
	}
	c, err := v.Load()
	if err != nil {
		return err
	}

	// Not a terminal: behave like ls so the command stays pipe-safe.
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return runLs(nil)
	}

	item, action, err := tui.Run(tui.New(itemsFor(c.Bookmarks), "search"))
	if err != nil {
		return err
	}
	switch action {
	case tui.ActionNone:
		return nil
	case tui.ActionOpen:
		return openByID(v, item.ID)
	case tui.ActionCopy:
		b, ok := c.Find(item.ID)
		if !ok {
			return fmt.Errorf("bookmark %s vanished", item.ID)
		}
		if err := clipboard.WriteAll(b.URL); err != nil {
			return err
		}
		fmt.Fprintln(out, "copied", b.URL)
		return nil
	case tui.ActionDelete:
		return v.Mutate(func(c *model.Collection) error {
			if !c.Delete(item.ID) {
				return fmt.Errorf("bookmark %s no longer exists", item.ID)
			}
			fmt.Fprintln(out, "deleted", item.ID)
			return nil
		})
	}
	return nil
}

// itemsFor ranks bookmarks by recency of use, then by when they were added,
// so the picker's unfiltered order is already useful.
func itemsFor(all []model.Bookmark) []tui.Item {
	rows := append([]model.Bookmark(nil), all...)
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Opens != rows[j].Opens {
			return rows[i].Opens > rows[j].Opens
		}
		return rows[i].Added.After(rows[j].Added)
	})

	items := make([]tui.Item, 0, len(rows))
	for _, b := range rows {
		items = append(items, tui.Item{
			ID:     b.ID,
			Label:  label(b),
			Detail: b.URL,
			Filter: strings.ToLower(strings.Join([]string{b.Title, b.URL, strings.Join(b.Tags, " ")}, " ")),
		})
	}
	return items
}

// openByID opens a bookmark and records the visit. The record is written
// through Mutate, so a concurrent add is never clobbered.
func openByID(v *store.Vault, id string) error {
	var url string
	if err := v.Mutate(func(c *model.Collection) error {
		b, ok := c.Find(id)
		if !ok {
			return fmt.Errorf("bookmark %s no longer exists", id)
		}
		url = b.URL
		now := time.Now().UTC()
		b.Visited = &now
		b.Opens++
		return nil
	}); err != nil {
		return err
	}
	return tui.Open(url)
}
```

```go
// cmd/bkmr/open.go
package main

import (
	"fmt"
	"strings"

	"github.com/sahilm/fuzzy"
)

func init() {
	register(command{
		Name:    "open",
		Summary: "open the best match for a query, or a bookmark id",
		Usage:   "bkmr open <id|query>",
		Run:     runOpen,
	})
}

func runOpen(args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	query := strings.Join(args, " ")

	v, err := openVault()
	if err != nil {
		return err
	}
	c, err := v.Load()
	if err != nil {
		return err
	}

	if b, ok := c.Find(query); ok {
		return openByID(v, b.ID)
	}

	items := itemsFor(c.Bookmarks)
	hay := make([]string, len(items))
	for i, it := range items {
		hay[i] = it.Filter
	}
	matches := fuzzy.Find(strings.ToLower(query), hay)
	if len(matches) == 0 {
		return fmt.Errorf("no bookmark matches %q", query)
	}
	return openByID(v, items[matches[0].Index].ID)
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./... -v`
Expected: PASS — ten new `internal/tui` tests and five new `cmd/bkmr` tests.

- [ ] **Step 8: Verify bare `bkmr` now resolves**

```bash
go build -o bkmr ./cmd/bkmr
./bkmr help
```
Expected: the help listing includes `picker`, `open`, `add`, `ls`, `init`, `unlock`, `lock`, `help`, `version`.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/ cmd/bkmr/ go.mod go.sum
git commit -m "feat(tui): fuzzy picker with open, copy and delete actions"
```

---

### Task 11: Browser tab capture

**Files:**
- Create: `internal/capture/browser/tabs.go`, `cmd/bkmr/tab.go`
- Test: `internal/capture/browser/tabs_test.go`, `cmd/bkmr/tab_test.go`

**Interfaces:**
- Consumes: `tui.*`, `model.*`, `openVault`.
- Produces:
  - `browser.Tab{Title, URL string}`
  - `browser.Tabs(ctx context.Context) ([]Tab, error)`
  - `browser.ErrNoBrowser`
  - `browser.Hint() string` — the platform-specific command that enables the debug port
  - `var browser.Endpoints = []string{...}` — probed in order; tests replace it

- [ ] **Step 1: Write the failing tests**

```go
// internal/capture/browser/tabs_test.go
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
```

```go
// cmd/bkmr/tab_test.go
package main

import (
	"strings"
	"testing"
)

func TestTabSavesTheChosenTabWithoutFetching(t *testing.T) {
	newVaultForTest(t, "pw")
	old := chooseTab
	chooseTab = func(items []tabChoice) (tabChoice, bool, error) {
		if len(items) != 2 {
			t.Fatalf("chooseTab got %d tabs, want 2", len(items))
		}
		return items[1], true, nil
	}
	defer func() { chooseTab = old }()
	stubTabs(t, "https://one.example", "One", "https://two.example", "Two")

	got := capture(t, func() {
		if err := runTab([]string{"-t", "reading"}); err != nil {
			t.Fatalf("runTab() error = %v", err)
		}
	})
	if !strings.Contains(got, "two.example") {
		t.Errorf("runTab() output = %q, want it to name the saved tab", got)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 1 {
		t.Fatalf("len(Bookmarks) = %d, want 1", len(c.Bookmarks))
	}
	if c.Bookmarks[0].Title != "Two" {
		t.Errorf("Title = %q, want %q taken from the browser", c.Bookmarks[0].Title, "Two")
	}
	if strings.Join(c.Bookmarks[0].Tags, ",") != "reading" {
		t.Errorf("Tags = %v, want [reading]", c.Bookmarks[0].Tags)
	}
}

func TestTabCancelledSavesNothing(t *testing.T) {
	newVaultForTest(t, "pw")
	old := chooseTab
	chooseTab = func([]tabChoice) (tabChoice, bool, error) { return tabChoice{}, false, nil }
	defer func() { chooseTab = old }()
	stubTabs(t, "https://one.example", "One")

	if err := runTab(nil); err != nil {
		t.Fatalf("runTab() error = %v", err)
	}

	v, _ := openVault()
	c, _ := v.Load()
	if len(c.Bookmarks) != 0 {
		t.Errorf("len(Bookmarks) = %d, want 0 after cancelling", len(c.Bookmarks))
	}
}

func TestTabWithNoBrowserPrintsTheHint(t *testing.T) {
	newVaultForTest(t, "pw")
	stubNoBrowser(t)

	err := runTab(nil)
	if err == nil {
		t.Fatal("runTab() error = nil, want an error when no browser answers")
	}
	if !strings.Contains(err.Error(), "--remote-debugging-port=9222") {
		t.Errorf("runTab() error = %q, want it to include the debug port hint", err)
	}
}

func TestTabWithNoOpenPagesSaysSo(t *testing.T) {
	newVaultForTest(t, "pw")
	stubTabs(t)

	got := capture(t, func() {
		if err := runTab(nil); err != nil {
			t.Fatalf("runTab() error = %v", err)
		}
	})
	if !strings.Contains(strings.ToLower(got), "no open tabs") {
		t.Errorf("runTab() output = %q, want it to report no open tabs", got)
	}
}
```

```go
// cmd/bkmr/tab_stub_test.go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FredrikSchold/bookmrkrr/internal/capture/browser"
)

// stubTabs serves a DevTools /json response listing the given url/title pairs.
func stubTabs(t *testing.T, urlTitlePairs ...string) {
	t.Helper()
	type entry struct {
		Type  string `json:"type"`
		Title string `json:"title"`
		URL   string `json:"url"`
	}
	var entries []entry
	for i := 0; i+1 < len(urlTitlePairs); i += 2 {
		entries = append(entries, entry{Type: "page", URL: urlTitlePairs[i], Title: urlTitlePairs[i+1]})
	}
	body, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	t.Cleanup(srv.Close)

	old := browser.Endpoints
	browser.Endpoints = []string{srv.URL}
	t.Cleanup(func() { browser.Endpoints = old })
}

// stubNoBrowser points the probe at a port nothing listens on.
func stubNoBrowser(t *testing.T) {
	t.Helper()
	old := browser.Endpoints
	browser.Endpoints = []string{"http://127.0.0.1:1"}
	t.Cleanup(func() { browser.Endpoints = old })
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/capture/... ./cmd/bkmr/ -run 'TestTabs|TestHint|TestTab' -v`
Expected: FAIL — the `browser` package does not exist.

- [ ] **Step 3: Write the tab discovery**

```go
// Package browser lists the pages currently open in a Chromium-based browser
// by querying its DevTools endpoint on loopback. Nothing leaves the machine,
// and no browser extension is required - but the browser must have been
// started with --remote-debugging-port.
//
// This package and internal/fetch are the only two permitted to import
// net/http; a CI test enforces that.
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// Endpoints are probed in order. 9222 is Chrome's documented default; 9223
// and 9224 are common when Edge or Brave run alongside it.
var Endpoints = []string{
	"http://127.0.0.1:9222",
	"http://127.0.0.1:9223",
	"http://127.0.0.1:9224",
}

// ErrNoBrowser means no DevTools endpoint answered.
var ErrNoBrowser = errors.New("no browser is exposing its DevTools endpoint on loopback")

// Tab is one open page.
type Tab struct {
	Title string
	URL   string
}

type devtoolsTarget struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

var internalSchemes = []string{"chrome://", "chrome-extension://", "devtools://", "about:", "edge://", "brave://", "chrome-untrusted://", "view-source:"}

// Tabs returns the real pages open in the first browser that answers.
func Tabs(ctx context.Context) ([]Tab, error) {
	client := &http.Client{Timeout: 2 * time.Second}

	for _, base := range Endpoints {
		targets, err := probe(ctx, client, base)
		if err != nil {
			continue
		}
		tabs := make([]Tab, 0, len(targets))
		for _, t := range targets {
			if t.Type != "page" || isInternal(t.URL) {
				continue
			}
			title := t.Title
			if strings.TrimSpace(title) == "" {
				title = t.URL
			}
			tabs = append(tabs, Tab{Title: title, URL: t.URL})
		}
		return tabs, nil
	}
	return nil, fmt.Errorf("%w\n%s", ErrNoBrowser, Hint())
}

func probe(ctx context.Context, client *http.Client, base string) ([]devtoolsTarget, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", base, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var targets []devtoolsTarget
	if err := json.Unmarshal(body, &targets); err != nil {
		return nil, err
	}
	return targets, nil
}

func isInternal(url string) bool {
	low := strings.ToLower(url)
	for _, s := range internalSchemes {
		if strings.HasPrefix(low, s) {
			return true
		}
	}
	return low == "" || low == "about:blank"
}

// Hint returns the platform-specific command that enables the debug port, so
// the error message tells the user exactly what to do.
func Hint() string {
	switch runtime.GOOS {
	case "windows":
		return `Start your browser with the debug port enabled, for example:
  "C:\Program Files\Google\Chrome\Application\chrome.exe" --remote-debugging-port=9222`
	case "darwin":
		return `Start your browser with the debug port enabled, for example:
  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --remote-debugging-port=9222`
	default:
		return `Start your browser with the debug port enabled, for example:
  google-chrome --remote-debugging-port=9222`
	}
}
```

- [ ] **Step 4: Write the tab command**

```go
// cmd/bkmr/tab.go
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/FredrikSchold/bookmrkrr/internal/capture/browser"
	"github.com/FredrikSchold/bookmrkrr/internal/model"
	"github.com/FredrikSchold/bookmrkrr/internal/tui"
)

type tabChoice struct {
	Title string
	URL   string
}

// chooseTab is a seam so tests do not need a terminal.
var chooseTab = func(choices []tabChoice) (tabChoice, bool, error) {
	items := make([]tui.Item, len(choices))
	for i, c := range choices {
		items[i] = tui.Item{
			ID:     fmt.Sprint(i),
			Label:  c.Title,
			Detail: c.URL,
			Filter: strings.ToLower(c.Title + " " + c.URL),
		}
	}
	item, action, err := tui.Run(tui.New(items, "which tab?"))
	if err != nil || action != tui.ActionOpen {
		return tabChoice{}, false, err
	}
	for i, c := range choices {
		if fmt.Sprint(i) == item.ID {
			return c, true, nil
		}
	}
	return tabChoice{}, false, nil
}

func init() {
	register(command{
		Name:    "tab",
		Summary: "pick one of your open browser tabs and save it",
		Usage:   "bkmr tab [-t tag]... [--note text]",
		Run:     runTab,
	})
}

func runTab(args []string) error {
	var tags tagList
	fs := flag.NewFlagSet("tab", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Var(&tags, "t", "tag (repeatable)")
	fs.Var(&tags, "tag", "tag (repeatable)")
	note := fs.String("note", "", "note")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}

	v, err := openVault()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tabs, err := browser.Tabs(ctx)
	if err != nil {
		return err
	}
	if len(tabs) == 0 {
		fmt.Fprintln(out, "no open tabs to save")
		return nil
	}

	choices := make([]tabChoice, len(tabs))
	for i, t := range tabs {
		choices[i] = tabChoice{Title: t.Title, URL: t.URL}
	}
	picked, ok, err := chooseTab(choices)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	// The title comes from the browser, so this path never touches the network.
	var stored model.Bookmark
	var merged bool
	if err := v.Mutate(func(c *model.Collection) error {
		var err error
		stored, merged, err = c.Add(model.Bookmark{URL: picked.URL, Title: picked.Title, Notes: *note, Tags: tags})
		return err
	}); err != nil {
		return err
	}

	if merged {
		fmt.Fprintf(out, "already saved as %s (%s); tags are now %s\n", stored.ID, stored.URL, tagsOrNone(stored.Tags))
		return nil
	}
	fmt.Fprintf(out, "saved %s  %s  %s\n", stored.ID, stored.Title, stored.URL)
	return nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./... -v`
Expected: PASS — five new `browser` tests and four new `cmd/bkmr` tests.

- [ ] **Step 6: Commit**

```bash
git add internal/capture/ cmd/bkmr/
git commit -m "feat(capture): save an open browser tab via the DevTools endpoint"
```

---

### Task 12: tags, edit, rm, export and import

**Files:**
- Create: `cmd/bkmr/tags.go`, `cmd/bkmr/edit.go`, `cmd/bkmr/rm.go`, `cmd/bkmr/transfer.go`
- Test: `cmd/bkmr/tags_test.go`, `cmd/bkmr/edit_test.go`, `cmd/bkmr/transfer_test.go`

**Interfaces:**
- Consumes: `openVault`, `model.*`, `store.(*Vault).Mutate`.
- Produces: commands `tags`, `edit`, `rm`, `export`, `import`; `var confirm = askYesNo` — a seam tests replace.

- [ ] **Step 1: Write the failing tests**

```go
// cmd/bkmr/tags_test.go
package main

import (
	"strings"
	"testing"
)

func TestTagsListsCountsSorted(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() {
		runAdd([]string{"--no-fetch", "-t", "rust", "-t", "web", "https://a.example"})
		runAdd([]string{"--no-fetch", "-t", "rust", "https://b.example"})
	})

	got := capture(t, func() {
		if err := runTags(nil); err != nil {
			t.Fatalf("runTags() error = %v", err)
		}
	})

	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("runTags() printed %d lines, want 2:\n%s", len(lines), got)
	}
	if !strings.Contains(lines[0], "rust") || !strings.Contains(lines[0], "2") {
		t.Errorf("first line = %q, want rust with a count of 2", lines[0])
	}
	if !strings.Contains(lines[1], "web") {
		t.Errorf("second line = %q, want web", lines[1])
	}
}

func TestTagsOnAnUntaggedVaultSaysSo(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://a.example"}) })

	got := capture(t, func() { runTags(nil) })
	if !strings.Contains(strings.ToLower(got), "no tags") {
		t.Errorf("runTags() = %q, want it to report no tags", got)
	}
}
```

```go
// cmd/bkmr/edit_test.go
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

	if err := runEdit([]string{"--title", "New Title", "--note", "new note", "-t", "fresh", id}); err != nil {
		t.Fatalf("runEdit() error = %v", err)
	}

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

	if err := runEdit([]string{"--note", "only the note", id}); err != nil {
		t.Fatal(err)
	}

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

func TestRmDeletesAfterConfirmation(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://a.example"}) })
	id := idOfFirst(t)

	old := confirm
	confirm = func(string) (bool, error) { return true, nil }
	defer func() { confirm = old }()

	if err := runRm([]string{id}); err != nil {
		t.Fatalf("runRm() error = %v", err)
	}

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

	if err := runRm([]string{id}); err != nil {
		t.Fatal(err)
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

	if err := runRm([]string{"--force", id}); err != nil {
		t.Fatal(err)
	}
}
```

```go
// cmd/bkmr/transfer_test.go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportWritesPlaintextJSONAndWarns(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "--title", "A", "https://a.example"}) })
	path := filepath.Join(t.TempDir(), "out.json")

	if err := runExport([]string{path}); err != nil {
		t.Fatalf("runExport() error = %v", err)
	}

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
}

func TestExportToStdoutWhenNoFileIsGiven(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "https://stdout.example"}) })

	got := capture(t, func() {
		if err := runExport(nil); err != nil {
			t.Fatalf("runExport() error = %v", err)
		}
	})
	if !strings.Contains(got, "https://stdout.example") {
		t.Errorf("runExport() stdout = %q, want the bookmark", got)
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

func TestImportDeduplicatesAgainstExistingBookmarks(t *testing.T) {
	newVaultForTest(t, "pw")
	capture(t, func() { runAdd([]string{"--no-fetch", "-t", "mine", "https://shared.example"}) })

	path := filepath.Join(t.TempDir(), "in.json")
	body := `{"version":1,"bookmarks":[{"id":"zzzzzzzz","url":"https://shared.example","tags":["theirs"],"added":"2026-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := runImport([]string{path}); err != nil {
		t.Fatal(err)
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

	if err := runImport([]string{path}); err != nil {
		t.Fatalf("runImport() error = %v", err)
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

func TestImportOfAMissingFileFails(t *testing.T) {
	newVaultForTest(t, "pw")

	if err := runImport([]string{filepath.Join(t.TempDir(), "nope.json")}); err == nil {
		t.Error("runImport() error = nil for a missing file, want an error")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/bkmr/ -run 'TestTags|TestEdit|TestRm|TestExport|TestImport' -v`
Expected: FAIL — `undefined: runTags`, `undefined: runEdit`, and so on.

- [ ] **Step 3: Write tags**

```go
// cmd/bkmr/tags.go
package main

import (
	"fmt"
	"sort"
	"text/tabwriter"
)

func init() {
	register(command{
		Name:    "tags",
		Summary: "list your tags with a count of bookmarks each",
		Usage:   "bkmr tags",
		Run:     runTags,
	})
}

func runTags([]string) error {
	v, err := openVault()
	if err != nil {
		return err
	}
	c, err := v.Load()
	if err != nil {
		return err
	}

	counts := c.TagCounts()
	if len(counts) == 0 {
		fmt.Fprintln(out, "no tags yet - add one with 'bkmr add <url> -t name'")
		return nil
	}

	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	// Most-used first, alphabetical within a count.
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, name := range names {
		fmt.Fprintf(w, "%s\t%d\n", name, counts[name])
	}
	return w.Flush()
}
```

- [ ] **Step 4: Write edit and rm**

```go
// cmd/bkmr/edit.go
package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/FredrikSchold/bookmrkrr/internal/model"
)

func init() {
	register(command{
		Name:    "edit",
		Summary: "change a bookmark's title, note or tags",
		Usage:   "bkmr edit <id> [--title text] [--note text] [-t tag]...",
		Run:     runEdit,
	})
}

func runEdit(args []string) error {
	var tags tagList
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Var(&tags, "t", "replacement tag (repeatable)")
	fs.Var(&tags, "tag", "replacement tag (repeatable)")
	title := fs.String("title", "", "new title")
	note := fs.String("note", "", "new note")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	if fs.NArg() != 1 {
		return errUsage
	}
	id := fs.Arg(0)

	// Distinguish "flag absent" from "flag set to empty".
	setTitle, setNote := false, false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "title":
			setTitle = true
		case "note":
			setNote = true
		}
	})

	v, err := openVault()
	if err != nil {
		return err
	}
	return v.Mutate(func(c *model.Collection) error {
		b, ok := c.Find(id)
		if !ok {
			return fmt.Errorf("no bookmark with id %q", id)
		}
		if setTitle {
			b.Title = *title
		}
		if setNote {
			b.Notes = *note
		}
		if len(tags) > 0 {
			b.Tags = model.NormalizeTags(tags)
		}
		fmt.Fprintf(out, "updated %s  %s\n", b.ID, label(*b))
		return nil
	})
}
```

```go
// cmd/bkmr/rm.go
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/FredrikSchold/bookmrkrr/internal/model"
)

// confirm asks a yes/no question. A seam so tests do not need stdin.
var confirm = askYesNo

func askYesNo(question string) (bool, error) {
	fmt.Fprintf(os.Stderr, "%s [y/N] ", question)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func init() {
	register(command{
		Name:    "rm",
		Summary: "delete a bookmark",
		Usage:   "bkmr rm <id> [--force]",
		Run:     runRm,
	})
}

func runRm(args []string) error {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	force := fs.Bool("force", false, "delete without confirming")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	if fs.NArg() != 1 {
		return errUsage
	}
	id := fs.Arg(0)

	v, err := openVault()
	if err != nil {
		return err
	}
	c, err := v.Load()
	if err != nil {
		return err
	}
	b, ok := c.Find(id)
	if !ok {
		return fmt.Errorf("no bookmark with id %q", id)
	}

	if !*force {
		ok, err := confirm(fmt.Sprintf("Delete %s (%s)?", b.ID, b.URL))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(out, "kept", b.ID)
			return nil
		}
	}

	return v.Mutate(func(c *model.Collection) error {
		if !c.Delete(id) {
			return fmt.Errorf("bookmark %s no longer exists", id)
		}
		fmt.Fprintln(out, "deleted", id)
		return nil
	})
}
```

- [ ] **Step 5: Write export and import**

```go
// cmd/bkmr/transfer.go
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

func runExport(args []string) error {
	if len(args) > 1 {
		return errUsage
	}
	v, err := openVault()
	if err != nil {
		return err
	}
	c, err := v.Load()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "bkmr: this output is UNENCRYPTED plaintext")
		_, err := out.Write(data)
		return err
	}
	if err := os.WriteFile(args[0], data, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(out, "wrote %d bookmarks to %s\n", len(c.Bookmarks), args[0])
	fmt.Fprintln(os.Stderr, "bkmr: that file is UNENCRYPTED plaintext - delete it when you are done")
	return nil
}

func runImport(args []string) error {
	if len(args) != 1 {
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

	v, err := openVault()
	if err != nil {
		return err
	}
	added, merged := 0, 0
	if err := v.Mutate(func(c *model.Collection) error {
		for _, b := range incoming {
			// Let Add mint a fresh id so an imported id can never collide.
			b.ID = ""
			_, wasMerged, err := c.Add(b)
			if err != nil {
				continue // skip unparseable URLs rather than abort the import
			}
			if wasMerged {
				merged++
			} else {
				added++
			}
		}
		return nil
	}); err != nil {
		return err
	}
	fmt.Fprintf(out, "imported %d new bookmarks, merged %d existing\n", added, merged)
	return nil
}

// hrefPattern matches one anchor in a Netscape bookmark file.
var hrefPattern = regexp.MustCompile(`(?is)<a\s+[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)

func parseImport(data []byte) ([]model.Bookmark, error) {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "{") {
		var c model.Collection
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("parse JSON: %w", err)
		}
		return c.Bookmarks, nil
	}

	var out []model.Bookmark
	for _, m := range hrefPattern.FindAllStringSubmatch(trimmed, -1) {
		href := html.UnescapeString(m[1])
		if _, err := model.NormalizeURL(href); err != nil {
			continue // skip bookmarklets, place: URLs, and other non-http entries
		}
		title := strings.Join(strings.Fields(html.UnescapeString(stripTags(m[2]))), " ")
		out = append(out, model.Bookmark{URL: href, Title: title})
	}
	if out == nil {
		return nil, fmt.Errorf("no bookmarks found - expected JSON or a browser HTML export")
	}
	return out, nil
}

var tagPattern = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string { return tagPattern.ReplaceAllString(s, "") }
```

- [ ] **Step 6: Run the full suite**

Run: `go test ./... -v`
Expected: PASS — fourteen new `cmd/bkmr` tests.

- [ ] **Step 7: Commit**

```bash
git add cmd/bkmr/
git commit -m "feat(cli): tags, edit, rm, and plaintext export and import"
```

---

### Task 13: Boundary test, CI, release and documentation

**Files:**
- Create: `internal/boundary/boundary_test.go`
- Create: `.github/workflows/ci.yml`, `.goreleaser.yaml`
- Create: `README.md`, `SECURITY.md`, `CONTRIBUTING.md`

**Interfaces:**
- Consumes: the whole module.
- Produces: no Go API — this task produces guarantees and documentation.

- [ ] **Step 1: Write the failing boundary test**

```go
// Package boundary holds architectural tests: rules about the shape of the
// codebase that CI must enforce, not behavior of any one package.
package boundary

import (
	"strings"
	"testing"
)

// netAllowed lists the only packages permitted to import net/http. This is
// what makes "bkmr does not phone home" a checkable property rather than a
// claim in the README.
var netAllowed = map[string]bool{
	"github.com/FredrikSchold/bookmrkrr/internal/fetch":           true,
	"github.com/FredrikSchold/bookmrkrr/internal/capture/browser": true,
}

func TestOnlyApprovedPackagesImportNetHTTP(t *testing.T) {
	for pkg, imports := range nonTestImports(t) {
		for _, imp := range imports {
			if imp != "net/http" {
				continue
			}
			if !netAllowed[pkg] {
				t.Errorf("%s imports net/http, which is only allowed in internal/fetch and internal/capture/browser.\n"+
					"If this package genuinely needs the network, that is a design decision - discuss it before adding the import.", pkg)
			}
		}
	}
}

// cryptoAllowed lists the only packages permitted to import cryptographic
// primitives. All crypto lives in internal/crypto (spec section 3).
var cryptoAllowed = map[string]bool{
	"github.com/FredrikSchold/bookmrkrr/internal/crypto": true,
}

// cryptoPrefixes are the import paths that mean "this package is doing
// cryptography". golang.org/x/term is a terminal helper and does not match.
var cryptoPrefixes = []string{"golang.org/x/crypto/", "crypto/aes", "crypto/cipher", "crypto/sha", "crypto/hmac"}

func TestOnlyTheCryptoPackageImportsCryptoPrimitives(t *testing.T) {
	for pkg, imports := range nonTestImports(t) {
		if cryptoAllowed[pkg] {
			continue
		}
		for _, imp := range imports {
			for _, prefix := range cryptoPrefixes {
				if strings.HasPrefix(imp, prefix) {
					t.Errorf("%s imports %s; all cryptography belongs in internal/crypto", pkg, imp)
				}
			}
		}
	}
}

func TestDirectDependencyBudget(t *testing.T) {
	const budget = 9

	requires := requireBlock(t)
	if len(requires) > budget {
		t.Errorf("go.mod requires %d direct modules, budget is %d:\n%s\n"+
			"A new direct dependency needs a justification in the PR - a small dependency tree is itself a privacy feature.",
			len(requires), budget, strings.Join(requires, "\n"))
	}
}
```

```go
// internal/boundary/imports_test.go
package boundary

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// nonTestImports maps each package in this module to its non-test imports.
func nonTestImports(t *testing.T) map[string][]string {
	t.Helper()
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = ".."
	cmd.Dir = moduleRoot(t)
	data, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -json ./...: %v", err)
	}

	result := map[string][]string{}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	for dec.More() {
		var p struct {
			ImportPath string
			Imports    []string
		}
		if err := dec.Decode(&p); err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		result[p.ImportPath] = p.Imports
	}
	if len(result) == 0 {
		t.Fatal("go list returned no packages")
	}
	return result
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	outBytes, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	gomod := strings.TrimSpace(string(outBytes))
	if gomod == "" || gomod == os.DevNull {
		t.Fatal("not inside a Go module")
	}
	return strings.TrimSuffix(gomod, "go.mod")
}

// requireBlock returns the module paths in go.mod's direct require block.
func requireBlock(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(moduleRoot(t) + "go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}

	var direct []string
	inBlock := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "require ("):
			inBlock = true
		case inBlock && trimmed == ")":
			inBlock = false
		case inBlock && trimmed != "" && !strings.HasPrefix(trimmed, "//"):
			if strings.Contains(trimmed, "// indirect") {
				continue
			}
			direct = append(direct, strings.Fields(trimmed)[0])
		case strings.HasPrefix(trimmed, "require ") && !strings.Contains(trimmed, "// indirect"):
			direct = append(direct, strings.Fields(trimmed)[1])
		}
	}
	return direct
}
```

- [ ] **Step 2: Run the boundary tests**

Run: `go test ./internal/boundary/ -v`
Expected: PASS. If `TestOnlyApprovedPackagesImportNetHTTP` fails, a package has grown a network import it should not have — fix the code, never the allowlist. If the dependency budget test fails, `go mod tidy` may have promoted an indirect dependency; check `go.mod`.

- [ ] **Step 3: Write the CI workflow**

```yaml
# .github/workflows/ci.yml
name: ci

on:
  push:
    branches: [main]
  pull_request:

permissions:
  contents: read

jobs:
  test:
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    env:
      CGO_ENABLED: "0"
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.26"
          check-latest: true
      - run: go build ./...
      - run: go vet ./...
      - run: go test ./... -race -count=1

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.26"
          check-latest: true
      - run: go install honnef.co/go/tools/cmd/staticcheck@latest
      - run: staticcheck ./...
      - run: go install golang.org/x/vuln/cmd/govulncheck@latest
      - run: govulncheck ./...

  cross-compile:
    runs-on: ubuntu-latest
    env:
      CGO_ENABLED: "0"
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.26"
          check-latest: true
      - name: build every release target
        run: |
          for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
            GOOS=${target%/*} GOARCH=${target#*/} go build -o /dev/null ./cmd/bkmr
          done
```

Note: `staticcheck` and `govulncheck` are installed as tools, not added to `go.mod`, so they do not count against the dependency budget.

- [ ] **Step 4: Write the release config**

```yaml
# .goreleaser.yaml
version: 2

project_name: bkmr

before:
  hooks:
    - go mod tidy

builds:
  - id: bkmr
    main: ./cmd/bkmr
    binary: bkmr
    env:
      - CGO_ENABLED=0
    ldflags:
      - -s -w
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]

archives:
  - formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]
    files:
      - README.md
      - LICENSE
      - SECURITY.md

checksum:
  name_template: checksums.txt

changelog:
  use: github
  sort: asc
```

- [ ] **Step 5: Write the README**

`README.md` must contain, in this order:

1. **One-line description** and the install block:

```bash
go install github.com/FredrikSchold/bookmrkrr/cmd/bkmr@latest
```
plus a note that release binaries for Linux, macOS and Windows are on the releases page.

2. **A 60-second demo**, exactly these commands with their real output:

```
$ bkmr init
New vault password:
Confirm password:
Vault created in ~/.local/share/bookmrkrr and unlocked.

$ bkmr add https://doc.rust-lang.org/book/ -t rust -t reading
saved k3m9qp2x  The Rust Programming Language

$ bkmr tab          # pick one of your open browser tabs
$ bkmr              # fuzzy-search everything, enter to open
$ bkmr tags
rust     4
reading  2
```

3. **A `## Privacy` section** stating, without softening:
   - The vault is encrypted with XChaCha20-Poly1305, key derived by Argon2id from your password. Nothing is readable without it.
   - **`bkmr` contacts the sites you save.** When you add a bare URL it makes one plain `GET` to read the page's `<title>`: no cookies, no referer, a 3-second timeout, same-host redirects only, and at most 64 KiB read. Disable it per command with `--no-fetch`, or permanently with `network = "off"` in `config.toml`. `bkmr tab` never fetches, because the browser already has the title.
   - No accounts, no telemetry, no analytics, no crash reporting, no server.
   - The entire network surface is two files: `internal/fetch/title.go` and `internal/capture/browser/tabs.go`, and a CI test fails the build if `net/http` appears anywhere else.

4. **A `## Threat model` section**, in plain words: it protects a vault file that someone else can read — stolen laptop without full-disk encryption, a leaked backup, an over-shared synced folder, another account on a shared machine. It does **not** protect against malware running as you, which can read the cached key from the keychain or log your keystrokes. File size and timestamps still leak roughly how many bookmarks you have and when you last touched them.

5. **A `## Where your data lives` section** with the three platform paths and the `BKMR_DATA_DIR` override.

6. **A `## File format` section** reproducing the byte layout table from spec §5, so the data outlives the tool.

7. **A `## Syncing` section**: the vault is a single encrypted file, so git or Syncthing works — but a conflict is unmergeable, and you must pick a side. `bkmr export` is the escape hatch.

8. **A `## Browser tabs` section** explaining the `--remote-debugging-port=9222` requirement up front, with the command for each platform.

- [ ] **Step 6: Write SECURITY.md and CONTRIBUTING.md**

`SECURITY.md`: how to report a vulnerability (open a GitHub security advisory rather than a public issue), what is in scope (the vault format, the KDF parameters, the key caching, the network policy) and what is not (malware running as the user — see the threat model), and a realistic response window.

`CONTRIBUTING.md`: `go test ./...` must pass on all three platforms; TDD is expected; the eight-dependency budget needs a justification to exceed; changes to `internal/crypto` or the vault format need a version bump, a migration and a new golden fixture; the boundary tests are not to be edited to make a change pass.

- [ ] **Step 7: Verify everything end to end**

```bash
go build ./...
go vet ./...
go test ./... -race -count=1
go build -o bkmr ./cmd/bkmr
./bkmr help
```
Expected: all green, and `bkmr help` lists every command from the spec §6 table.

- [ ] **Step 8: Commit**

```bash
git add internal/boundary/ .github/ .goreleaser.yaml README.md SECURITY.md CONTRIBUTING.md
git commit -m "chore: enforce import boundaries, add CI, release config and docs"
```

