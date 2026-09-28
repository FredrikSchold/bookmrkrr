# bkmr

Terminal-first bookmark manager with a locally encrypted vault. One binary, no
server, no account.

```bash
go install github.com/FredrikSchold/bookmrkrr/cmd/bkmr@latest
```

Release binaries for Linux, macOS and Windows (amd64 and arm64) are attached to
each entry on the [releases page](https://github.com/FredrikSchold/bookmrkrr/releases),
with a `checksums.txt` alongside them.

> Note: an unrelated Rust bookmark manager also publishes a binary called
> `bkmr`. This is not it.

## 60 seconds

```
$ bkmr init
New vault password:
Confirm password:
Vault created in /home/you/.local/share/bookmrkrr and unlocked.
Add your first bookmark with 'bkmr add <url>'.

$ bkmr add https://go.dev/doc/effective_go -t go -t reading
saved a3xnt3ou  Effective Go - The Go Programming Language

$ bkmr add https://www.sqlite.org/whentouse.html -t sqlite
saved v3dx7jrj  Appropriate Uses For SQLite

$ bkmr tags
go       1
reading  1
sqlite   1

$ bkmr ls
v3dx7jrj  Appropriate Uses For SQLite  https://www.sqlite.org/whentouse.html  [sqlite]
a3xnt3ou  Effective Go - The Go Programming Language  https://go.dev/doc/effective_go  [go reading]

$ bkmr tab          # pick one of your open browser tabs and save it
$ bkmr              # fuzzy-search everything; enter opens, tab browses tags,
                    # ctrl+y copies the URL, ctrl+d deletes, esc quits
```

Those titles and ids are real output from a real run; the ids are random, so
yours will differ. The password prompts do not echo.

## Commands

```
bkmr                     open the picker (the zero-argument default; the
                         subcommand 'bkmr picker' runs the same thing)
bkmr help [command]      list all commands, or detail one (--help and -h too)
bkmr init                create the vault and set its password
bkmr add [url]           save a bookmark; no argument reads the clipboard
                         [-t tag]... [--title text] [--note text] [--no-fetch]
bkmr tab                 pick one of your open browser tabs and save it
                         [-t tag]... [--note text]
bkmr tags                list tags with a count each
bkmr open <id|query>     open the best match in your default browser
bkmr edit <id>           [--title text] [--note text] [-t tag]...
bkmr rm <id>             delete, after a confirmation [--force]
bkmr ls [--tag name]     print bookmarks to stdout, newest first
bkmr unlock              derive the key from your password and cache it
bkmr lock                drop the cached key
bkmr export [file]       write plaintext JSON to a file, or stdout if omitted
bkmr import <file>       read plaintext JSON, or a browser bookmarks HTML export
bkmr version
```

Flags may come before or after the positional argument: `bkmr add -t go URL`
and `bkmr add URL -t go` are the same command.

In the picker, typing fuzzy-matches over title, URL and tags as you go; `tab`
switches to tag mode so you can narrow to one tag and keep typing; `enter` opens
the highlighted bookmark in your default browser and records the visit; `ctrl+y`
copies its URL; `ctrl+d` deletes it — immediately, with no confirmation, unlike
`bkmr rm`; `esc` or `ctrl+c` quits. Equally good fuzzy matches are ordered by
how often and how recently you have opened them.

With stdout not a terminal, bare `bkmr` prints what `bkmr ls` prints instead of
trying to draw a picker, so `bkmr | grep rust` works.

## Privacy

**The vault is encrypted.** XChaCha20-Poly1305, with the key derived from your
password by Argon2id (64 MiB, time cost 3, parallelism 4). Without the password
the file is noise. The derived 32-byte key — never the password — is cached in
your OS keychain so routine commands do not prompt; `bkmr lock` drops it.

**`bkmr` contacts the sites you save.** This is the one piece of outbound
traffic in the whole tool, and it is on by default, so here it is in full. When
you `bkmr add` a URL from an argument or the clipboard, `bkmr` makes **one plain
`GET`** to read the page's `<title>`:

- No cookies. The HTTP client has no cookie jar at all, so a fetch cannot carry
  a session you have with the site and cannot leave one behind.
- No `Referer`, ever — including on a redirect hop, where Go's own client would
  add one, so it is explicitly deleted.
- A fixed, uninformative `User-Agent` (`bkmr/0.1` plus the project URL). It is
  not configurable, because a configurable one is a place to leak identity.
- **3-second timeout.**
- **At most 3 requests in a redirect chain** — the original plus two hops
  followed — **and only within the same site.** A leading `www.` counts as the
  same site, in both directions; every other subdomain does not, and neither
  does a different port. Each hop is compared against the URL you asked for, not
  the previous hop, so a chain cannot walk away one host at a time.
- **The response body is capped at 64 KiB** and scanned only for `<title>`.
- A declared non-HTML content type is skipped without reading the body at all.
- **The URL requested is the normalized one**, so tracking parameters
  (`utm_*`, `fbclid` and friends) are stripped *before* the request, not merely
  before storage. `bkmr` stores what you typed and asks for the cleaned version.
- `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` **are honoured**, deliberately:
  routing a CLI through a corporate or privacy proxy is what those variables are
  for, and ignoring them would be the surprising choice. It does mean your
  environment can redirect where the request goes.
- A failed or slow fetch never costs you the bookmark. It is saved with an
  empty title and a note on stderr.

Turn it off per command with `--no-fetch`, or permanently by putting this in
`config.toml`:

```toml
network = "off"
```

`network = false` and `network = "no"` mean the same thing. Passing `--title`
also skips the fetch, because there is then nothing to look up. `bkmr tab`
**never** fetches: the browser already told it the title.

**A misspelled or malformed `config.toml` prints a problem and leaves the
network on.** `netwrok = "off"` is valid TOML, so it would otherwise parse
cleanly and hand you a kill switch that silently does nothing. The safe-looking
direction would be to fail closed, and this deliberately does not: the default
is the permissive one, and what stops it being silent is that every `bkmr add`
reports the problem on stderr whether or not it was going to fetch anything. If
you turn the network off, run one `bkmr add` and check that nothing complains.

**No accounts, no telemetry, no analytics, no crash reporting, no server.** Not
"opt-out" — absent.

**The entire network surface is two files:** `internal/fetch/title.go` (title
fetching) and `internal/capture/browser/tabs.go` (the loopback DevTools query
that `bkmr tab` uses). A test in `internal/boundary` fails the build if any
package outside those two imports something that can open a connection in
non-test code — `net`, `net/http` and its relatives, `crypto/tls`, anything
under `golang.org/x/net/` — and the same package pins all cryptography to
`internal/crypto` and the direct dependency count to nine. Those are tests, not
prose: CI runs them on every push.

The one thing those tests cannot bound is a subprocess. `bkmr` shells out twice
on purpose — to launch your browser, and to read the clipboard — and a socket
opened by another program is not something an import graph can see. That gap is
closed by review, not by a test.

## Threat model

**It protects a vault file that someone else can read.** A stolen laptop
without full-disk encryption. A leaked backup. An over-shared or cloud-synced
folder. Another account on a shared machine. Someone browsing your home
directory. In all of those the attacker has your bytes and not your password,
and the bytes are useless.

**It does not protect against malware, or anything else running as you.** Such
a process can read the cached key straight out of the OS keychain, read `bkmr`'s
process memory, or log your keystrokes and take the password itself. No
local-vault design defeats that, and this one does not pretend to.

**Filesystem metadata still leaks.** The vault's size tells an observer roughly
how many bookmarks you have; its modification time tells them when you last
touched it. Encryption does not hide either.

## Where your data lives

| Platform | Vault | Config |
|---|---|---|
| Linux | `$XDG_DATA_HOME/bookmrkrr/` (default `~/.local/share/bookmrkrr/`) | `$XDG_CONFIG_HOME/bookmrkrr/config.toml` (default `~/.config/bookmrkrr/config.toml`) |
| macOS | `~/Library/Application Support/bookmrkrr/` | same directory |
| Windows | `%LOCALAPPDATA%\bookmrkrr\` | `%APPDATA%\bookmrkrr\config.toml` |

The directory holds `vault.bkmr`, a `vault.bkmr.bak` copy of the previous
contents, and a short-lived `vault.lock` while a write is in progress.

**`BKMR_DATA_DIR` overrides all of it — including `config.toml`.** Set it and
both the vault and the config move into that one directory, so a portable or
throwaway setup is a single environment variable rather than two. The test suite
relies on this.

## File format

The vault is a single file. Written out here so your data outlives the tool:

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

Bytes 0–54 are passed to the AEAD as **associated data**, so nobody can weaken
the recorded KDF parameters and still produce a file that opens. Because the
ciphertext is authenticated, a corrupted or hand-edited vault fails loudly
instead of quietly decrypting to garbage. The plaintext inside is the same JSON
document `bkmr export` writes, just without the indentation.

Reading it back needs nothing from this repository: derive a 32-byte key with
Argon2id over your password and the salt using the parameters in the header,
then open XChaCha20-Poly1305 with the nonce at offset 31 and bytes 0–54 as
additional data.

Writes go: take `vault.lock`, write the new vault to `vault.bkmr.tmp`, `fsync`
it, **copy** the current vault to `vault.bkmr.bak`, then rename the temp file
over `vault.bkmr`. There is no moment at which `vault.bkmr` does not exist —
interrupt it anywhere and what remains is either the old vault or the new one,
both openable. (An early design rotated the vault to `.bak` instead of copying
it; that left a window with no vault file, and on Windows a `bkmr init` landing
in that window would have created an empty one over the top. Copying costs one
extra small write per save and removes the window.)

## Syncing

The vault is one encrypted file, so git, Syncthing, Dropbox or a USB stick all
work, and none of them can read it. What none of them can do is merge it: two
machines that both added a bookmark produce a conflict with no resolution but to
pick a side. Sync it if you understand that; `bkmr export` on both sides and
`bkmr import` into one is the manual way to keep everything.

`bkmr export` is also the escape hatch generally — plain JSON, no lock-in. Two
things to know: it writes **everything, unencrypted**, and it **refuses to
overwrite an existing file** (it creates the file with `O_EXCL`, so the refusal
is the same syscall as the create). Pick a new path, or delete the old export
yourself.

## Browser tabs

`bkmr tab` lists the tabs you have open and saves the one you pick. It needs no
extension and nothing leaves your machine — it reads the browser's own DevTools
endpoint on loopback (`127.0.0.1:9222/json`, also trying 9223 and 9224 where
Edge or Brave commonly land).

**The browser must have been started with `--remote-debugging-port=9222`.** That
is the honest cost of needing no extension. A browser already running without
the flag will not answer, and you have to restart it:

```bash
# Linux
google-chrome --remote-debugging-port=9222

# macOS
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --remote-debugging-port=9222
```

```powershell
# Windows
& "C:\Program Files\Google\Chrome\Application\chrome.exe" --remote-debugging-port=9222
```

`bkmr tab` prints the command for your platform when nothing answers.
`chrome://`, `devtools://`, `about:` and extension pages are filtered out of the
list, and nothing here leaves your machine: the only request is to `127.0.0.1`,
and the title comes from the browser rather than from the site.

## Building

```bash
go build ./cmd/bkmr      # CGO_ENABLED=0 works; there is no C code
go test ./...
```

Nine direct dependencies, pinned in **both** directions by a test: it fails at
ten, and it also fails at eight. The ceiling is the point — every direct module
is code running in the same address space as your vault key — but the floor is
what keeps the ceiling honest, because `go get` has twice left a module here
marked `// indirect` that the build in fact imports directly, and a count that
silently reads too low would let a tenth dependency in unnoticed. So if you
legitimately remove a dependency, expect the test to fail and lower the constant
in the same commit. See [CONTRIBUTING.md](CONTRIBUTING.md) and
[SECURITY.md](SECURITY.md).

MIT licensed.
