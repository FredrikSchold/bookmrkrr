# BookMrkr — Design Spec

- **Date:** 2026-09-27
- **Status:** Approved design, pending implementation plan
- **Project name:** BookMrkr
- **Repository:** `bookmrkrr`
- **Binary / command:** `bkmr`
- **Language:** Go
- **License:** MIT

## 1. Intent

BookMrkr is a terminal-first bookmark manager that keeps its data encrypted on
the user's own machine.

The motivation is workflow, not paranoia. The author lives in the shell, and
leaving it to save or find a link breaks flow. Privacy is therefore a design
constraint rather than the product pitch: no accounts, no telemetry, no
third-party server, and a vault that is unreadable without the CLI and a
password.

**Success means** saving the page currently open in the browser takes one
command and no mouse, and finding a link saved months ago takes a few
keystrokes.

### Goals

1. Capture a URL from the shell with minimum ceremony.
2. Retrieve a bookmark through an interactive fuzzy picker, organized by tags.
3. Keep the vault encrypted at rest, unlocked by a local password.
4. Preserve flow: routine commands must not prompt for a password.
5. Be auditable. A reader should be able to confirm the privacy claims in
   minutes.

### Non-goals for v1

- Page archiving or full-text search of page content. The tool never stores
  page bodies.
- Built-in sync to any remote. Sync is the user's business (git, Syncthing).
- Web UI, browser extension, multi-user support, or a server component.
- Tag hierarchies as a feature. A `/` inside a tag name is convention only.

## 2. Threat model

**Protects against:** an attacker who can read files but cannot run code as the
user — a stolen laptop without full-disk encryption, a leaked backup, an
over-shared or cloud-synced folder, another account on a shared machine,
someone browsing the home directory.

**Does not protect against:** malware or any process running as the user. Such
a process can read the cached key out of the OS keychain, read process memory,
or log keystrokes. No local-vault design defeats this, and the README says so
in plain words rather than implying otherwise.

**Also out of scope:** operating-system metadata. File size and modification
times still leak roughly how many bookmarks exist and when they were last
touched.

## 3. Architecture

```
cmd/bkmr/main.go           argument dispatch and help text, nothing else
internal/crypto/           KDF, AEAD seal/open, file header    <- all crypto
internal/keyring/          OS keychain get/set/delete, password prompt fallback
internal/store/            Store interface, encrypted-JSON impl, atomic write, lock
internal/model/            Bookmark, URL and tag normalization, dedupe
internal/capture/          url argument, clipboard
internal/capture/browser/  open-tab discovery over the DevTools endpoint
internal/fetch/            title fetching            <- all outbound network
internal/tui/              bubbletea picker: list, fuzzy filter, tag mode, actions
internal/config/           config file, defaults, platform paths
```

Two packages are security-relevant, and both are deliberately small enough to
read in one sitting: `internal/crypto` holds every cryptographic operation, and
`internal/fetch` holds every outbound network call.

`Store` is an interface — `Load`, `Add`, `Update`, `Delete` — so no package
above it knows the vault is encrypted or that the plaintext is JSON.

### Enforced boundary

A CI test walks the import graph and **fails the build if `net/http` is
imported by any package other than `internal/fetch` and
`internal/capture/browser`**. This converts "it does not phone home" from a
README claim into a property the test suite enforces on every commit.

## 4. Data model

```go
type Bookmark struct {
    ID      string     `json:"id"`      // 8 chars, base32 of 5 random bytes
    URL     string     `json:"url"`
    Title   string     `json:"title,omitempty"`
    Tags    []string   `json:"tags,omitempty"`
    Notes   string     `json:"notes,omitempty"`
    Added   time.Time  `json:"added"`
    Visited *time.Time `json:"visited,omitempty"` // last opened; ranks results
    Opens   int        `json:"opens,omitempty"`   // open count; ranking tiebreak
}
```

The decrypted plaintext is a single JSON document:

```json
{"version": 1, "bookmarks": [ ... ]}
```

The `version` field is what makes a future format change survivable.

### Normalization

**URLs** are normalized before comparison: lowercase scheme and host, strip a
trailing slash when the path is otherwise empty, drop `utm_*`, `fbclid`,
`gclid` and `mc_eid` query parameters. The original URL is stored exactly as
given; the normalized form is used only for duplicate detection.

**Tags** are lowercased and trimmed, internal whitespace becomes a hyphen, and
duplicates within an entry are removed.

**Duplicates:** adding a URL whose normalized form already exists merges the new
tags and notes into the existing entry and reports that it did so, rather than
creating a second entry.

## 5. On-disk format

Vault file, default name `vault.bkmr`:

```
offset  size  field
0       5     magic "BMRK1"
5       1     format version (1)
6       4     argon2id time cost   (uint32, big endian)
10      4     argon2id memory KiB  (uint32, big endian)
14      1     argon2id parallelism (uint8)
15      16    salt
31      24    XChaCha20-Poly1305 nonce
55      ..    ciphertext + 16-byte authentication tag
```

Bytes 0–54 are passed to the AEAD as **associated data**, so an attacker cannot
weaken the KDF parameters and still produce a file that opens. Authentication
also means a corrupted or hand-edited vault fails loudly instead of silently
yielding garbage.

**KDF:** Argon2id, 64 MiB memory, time cost 3, parallelism 4 — roughly 100 ms
on a laptop. This cost is paid only by `bkmr unlock`, never by routine
commands, because the keychain caches the derived 32-byte key.

**Storing the derived key rather than the passphrase is deliberate.** A
compromised keychain yields a key usable only against this vault, never a
passphrase the user may have reused elsewhere.

### Write protocol

1. Acquire an advisory lock (`vault.lock`) so concurrent `bkmr add` invocations
   cannot clobber one another.
2. Serialize, encrypt, and write to `vault.bkmr.tmp` in the same directory.
3. `fsync` the temp file.
4. **Copy** the current vault to `vault.bkmr.bak`, leaving the vault in place.
5. Rename the temp file over `vault.bkmr`, replacing it.
6. Release the lock.

**There is no ordering in which `vault.bkmr` is absent.** Interrupted during
steps 1–4, the vault is untouched and openable; interrupted during step 5, the
result is either the old or the new vault, both openable. An interrupted step 4
can leave a partial `.bak`, which is an acceptable trade because the vault
itself is intact.

> **Amended during execution (Task 4):** step 4 originally *renamed* the vault
> to `.bak` before renaming the temp file into place. That left a window with
> no vault file at all, and Windows makes it reachable — `os.Rename` onto an
> open path fails with a sharing violation. A user landing in that window is
> told "no vault found — run `bkmr init`", and `init` then creates a fresh
> empty vault because `Exists()` is false, stranding their bookmarks in `.bak`
> with no indication anything survived. That is data loss wearing a friendly
> error message. Copying rather than rotating removes the window entirely, at
> the cost of one extra read-and-write of a small file per save.

### Paths

| Platform | Data | Config |
|---|---|---|
| Windows | `%LOCALAPPDATA%\bookmrkrr\` | `%APPDATA%\bookmrkrr\config.toml` |
| Linux | `$XDG_DATA_HOME/bookmrkrr/` | `$XDG_CONFIG_HOME/bookmrkrr/config.toml` |
| macOS | `~/Library/Application Support/bookmrkrr/` | same directory |

`BKMR_DATA_DIR` overrides the data directory; the test suite relies on this.

## 6. Commands

```
bkmr                     open the picker (default, zero-argument path)
bkmr help [command]      list all commands, or detail one
bkmr init                create the vault and set the password
bkmr add [url]           add a bookmark; no argument reads the clipboard
                         -t/--tag (repeatable), --title, --note, --no-fetch
bkmr tab                 list open browser tabs, select one to save
bkmr tags                list tags with counts
bkmr open <id|query>     open the best match in the default browser
bkmr edit <id>           --tag, --title, --note
bkmr rm <id>             delete, with confirmation
bkmr ls [--tag x]        print matching bookmarks to stdout
bkmr unlock              derive the key from the password and cache it
bkmr lock                drop the cached key
bkmr export [file]       write plaintext JSON to file, or stdout if omitted;
                         warns on stderr that the output is unencrypted
bkmr import <file>       read plaintext JSON or a browser bookmarks HTML export
bkmr version
```

`bkmr help` is a first-class command, not only a flag: it lists every command
with a one-line summary, and `bkmr help add` prints that command's flags.
`bkmr --help` and `bkmr -h` are aliases for it.

Two commands were added during design rather than requested, and both earn
their place. **`bkmr ls`** is about twenty lines, makes the tool scriptable,
and is how integration tests assert vault state without driving a TUI.
**`bkmr export`** is the escape hatch that a proprietary encrypted format owes
its users: no lock-in, and a recovery path if the format is ever distrusted.

## 7. Capture

Three paths, in ascending order of convenience:

1. **Explicit argument** — `bkmr add https://example.com -t rust`.
2. **Clipboard** — `bkmr add` with no argument reads the clipboard and refuses,
   with a clear message, if the contents are not a URL.
3. **Open-tab picker** — `bkmr tab` queries the browser's DevTools endpoint at
   `127.0.0.1:9222/json`, also trying 9223 and 9224 for Edge and Brave, and
   presents the real open tabs with their titles for selection. `chrome://`,
   `devtools://`, `about:` and extension pages are filtered out.

The tab picker **requires the browser to have been started with
`--remote-debugging-port=9222`**. This is the honest limitation of the
approach: it needs no extension and nothing leaves the machine, but it does
need that flag. When no endpoint answers, the error prints the exact command
for the user's platform rather than a generic failure. The README states the
requirement up front.

Multi-select in the tab picker is deferred; single selection is the v1 scope.

## 8. Retrieval

Bare `bkmr` opens a `bubbletea` picker built on `bubbles/list` and `lipgloss`.

- Typing filters live, fuzzy-matching over title, URL and tags via
  `sahilm/fuzzy`.
- Equal fuzzy scores break toward recently and frequently opened bookmarks,
  using `Visited` and `Opens`.
- `Tab` switches to tag mode: choose a tag to narrow the set, then keep typing.
- `Enter` opens in the default browser and records the visit. `o` copies the
  URL. `e` opens `$EDITOR` for tags and notes. `d` deletes after a confirm.
  `Esc` or `q` quits.
- If stdout is not a TTY, bare `bkmr` prints `bkmr ls` output instead of
  failing, which keeps the tool pipe-safe.

## 9. Network policy

Title fetching is **on by default** for URLs supplied by argument or clipboard,
and is the tool's only outbound traffic. This is a deliberate trade of a little
privacy for convenience, and the design keeps that trade narrow and legible:

- A plain `GET`, no cookies, no `Referer`, a generic user agent.
- 3-second timeout; redirects followed only within the same host, at most 3.
- The response body is capped at 64 KiB and parsed only for `<title>`.
- Non-HTML content types are skipped without reading the body.
- Disabled per invocation with `--no-fetch`, or globally with `network = "off"`
  in `config.toml`.
- Never used by `bkmr tab`, which already has the title from the browser.

The README states plainly, in its own section: *this tool contacts the sites you
save, unless you turn it off*. All of it lives in `internal/fetch`, so the
complete network surface is one reviewable file.

## 10. Error handling

| Situation | Behavior |
|---|---|
| Title fetch fails or times out | **Save the bookmark anyway** with an empty title, and note it on stderr. Network is decoration, never a gatekeeper. |
| Wrong password or corrupt vault | One message covering both, exit 1. No oracle distinguishing the two cases. |
| No vault found | Exit 1 with an instruction to run `bkmr init`. |
| Keychain unavailable (headless Linux, no Secret Service) | Fall back to prompting for the password, with a one-line explanation. Never a crash. |
| Clipboard empty or not a URL | Clear message, nothing saved. |
| No DevTools endpoint answers | Print the platform-specific command to enable the debug port. |
| Concurrent write | Advisory lock; the second invocation waits briefly, then reports that the vault is busy. |

## 11. Testing

Development follows TDD throughout.

- **crypto** — seal/open round trip; wrong password fails; a single flipped
  ciphertext byte fails; a header with downgraded KDF parameters fails; and a
  **golden v1 vault committed to the repository**, so every future version
  proves it can still open today's files.
- **store** — add, update, delete; an interrupted write leaves the previous
  vault intact; the lock serializes concurrent writers.
- **model** — table-driven URL and tag normalization, and dedupe-on-add.
- **capture** — tab discovery against an `httptest` server serving a real
  `/json` payload; clipboard behind an interface with a fake.
- **fetch** — `httptest` server covering title parsing, timeout, oversized
  body, non-HTML content type, and the redirect policy.
- **tui** — `teatest` for filter, select and open against a fake store. View
  code stays thin so most behavior is testable as plain units.
- **boundary** — the import-graph test from section 3.
- **end to end** — drive the compiled binary against a temporary
  `BKMR_DATA_DIR` through init, add, ls, open, rm.

## 12. Dependencies

Nine direct dependencies, because a small dependency tree is itself a privacy
feature:

`bubbletea`, `bubbles`, `lipgloss`, `sahilm/fuzzy`, `atotto/clipboard` (pure
Go, shells out to platform tools), `zalando/go-keyring`, `golang.org/x/crypto`
(argon2, chacha20poly1305), `golang.org/x/term`, and `BurntSushi/toml`.

Everything else is the standard library. No CGO, so cross-compilation stays a
single command.

## 13. Packaging and documentation

- GoReleaser builds Windows, macOS and Linux for amd64 and arm64, with
  checksums. `go install` works. A Homebrew tap and Scoop manifest follow later.
- CI runs build and test on all three operating systems, plus `go vet`,
  `staticcheck`, `govulncheck`, and the import-boundary test.
- `README.md` leads with a 60-second demo, then a **Privacy** section stating
  exactly what touches the network and exactly what is encrypted, the threat
  model from section 2 in plain words, and the section 5 file format spec so the
  data outlives the tool.
- `SECURITY.md`, because the project touches cryptography.
- `CONTRIBUTING.md` and the MIT `LICENSE`.

## 14. Build order

1. `crypto` + `store` + `init`, `add`, `ls` — fully headless and testable.
2. TUI picker + `open`.
3. Tab capture, clipboard, title fetch.
4. `tags`, `edit`, `rm`, `help`, `export`, `import`.
5. Packaging, CI, documentation.

## 15. Decision log

| Decision | Rationale |
|---|---|
| Go | Single binary, no runtime for users, good TUI ecosystem. |
| Encrypted JSON blob, not SQLite | SQLite provides no encryption at all; encrypted SQLite means SQLCipher and CGO. Encryption is orthogonal to storage format, and the dataset fits in RAM, so a sealed JSON document is strictly simpler. |
| Whole-file rewrite on change | At tens of thousands of bookmarks the cost is negligible, and it removes all partial-write and migration complexity. |
| Keychain stores the derived key | Preserves flow without prompting, and never exposes a possibly-reused passphrase. |
| Title fetch on by default | The author's explicit choice, accepted with the mitigations in section 9 and honest documentation. |
| No full-text search | Removes any need to fetch or store page content, which keeps the privacy claim narrow and true. |
| Sync deferred | The `Store` interface and a portable format keep the option open. An encrypted blob syncs through git or Syncthing, but a conflict is unmergeable — the user must pick a side. |
| Binary named `bkmr` | The author's choice. Note: an unrelated Rust bookmark manager also publishes as `bkmr`, so search-result collision is expected. |
