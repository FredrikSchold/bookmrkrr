# Security policy

`bkmr` encrypts a file that is meant to survive being read by someone else, so
a bug in the crypto, the key handling or the network policy is a real problem
and not a nitpick. Please report one.

## Reporting a vulnerability

**Open a private security advisory**, not a public issue:
<https://github.com/FredrikSchold/bookmrkrr/security/advisories/new>

Include what you need to make it reproducible — the command, the platform, the
vault state, and what you expected instead. A proof of concept is welcome but
not required; a clear description of the flaw is enough to start.

Please do not attach a real vault or a real password. If a specific file is the
only way to show the bug, say so in the advisory and we will work out how to
share it.

## What to expect

This is a small project maintained in spare time, so here is the honest window
rather than a corporate one:

- **Acknowledgement within 7 days.** If you have heard nothing in two weeks,
  assume the notification was missed and open a public issue saying only "I have
  filed a security advisory, please look" — no details.
- **An assessment within 30 days**, saying whether it is in scope, how severe it
  looks, and roughly when a fix will land.
- A fix ships as a normal tagged release with the advisory published alongside
  it. You will be credited unless you ask not to be.

There is no bounty.

## In scope

- **The vault format** — the header, the associated-data construction, the
  authentication, the version field, anything that lets a modified file open or
  a valid file fail to.
- **The KDF parameters** — Argon2id cost, salt generation, the bounds checks on
  the parameters read back out of a file's header.
- **Key handling** — the derived key in the OS keychain, `bkmr lock`, what is
  held in memory and for how long, anything that could put the password itself
  rather than the derived key into storage or a log.
- **The network policy** — anything that makes `bkmr` reach a host the user did
  not name, send a cookie, a `Referer` or an identifying header, ignore
  `--no-fetch` or `network = "off"`, follow a redirect off-site, or read
  unbounded amounts of a response.
- **The write protocol** — any sequence of interruptions or concurrent `bkmr`
  processes that loses bookmarks or leaves no openable vault.
- **Injection through untrusted input** — a hostile page title, a hostile
  browser tab title, or an imported file that escapes into the terminal, the
  editor, or the URL the tool opens.

## Not in scope

- **Malware or any other code running as the user.** See the threat model in
  the [README](README.md#threat-model): such a process can read the cached key
  out of the keychain, read process memory, or log keystrokes. That is not a bug
  in `bkmr` and no local-vault design defeats it.
- **Filesystem metadata.** The vault's size and modification time leak roughly
  how many bookmarks exist and when they were last touched. Documented, accepted.
- **A weak password.** Argon2id raises the cost of guessing; it does not make
  `hunter2` safe.
- **Title fetching existing at all.** It is on by default, documented in full,
  and switched off with `--no-fetch` or `network = "off"`. A report that it makes
  a request is a duplicate of the README; a report that it makes a request it
  documents itself as not making is in scope.
- **A vulnerability in a dependency with no path from `bkmr` to it.** CI runs
  `govulncheck`, which reports reachable ones. If you find a reachable path that
  `govulncheck` misses, that is in scope and worth telling us about.
