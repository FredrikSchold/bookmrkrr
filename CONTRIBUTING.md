# Contributing

Patches welcome. The rules below are short, and most of them exist because the
project makes specific promises in its [README](README.md) that only hold if the
code keeps holding them.

## Before you open a pull request

```bash
gofmt -l .          # must print nothing
go build ./...
go vet ./...
go test ./... -count=1
```

`go test ./...` must pass on **Linux, macOS and Windows**. CI runs all three,
plus `go test ./... -race` on Linux, plus `staticcheck` and `govulncheck`. If
you only have one platform, say so in the PR and let CI find the rest — but
never assume the other two, especially around file paths, renaming an open file,
and file permissions. Windows does not implement `0o600`, so any test asserting
permission bits must skip there.

`CGO_ENABLED=0` must keep building. There is no C code and cross-compilation
stays a single command.

## Test first

Write the failing test, watch it fail, then implement. This is not a
preference in this repo: several of the tests here exist because a change that
looked obviously right was not, and the only thing that caught it was a test
written before the fix. A PR whose tests were written afterwards tends to test
what the code does rather than what it should do, and it is visible.

Tests must not reach the network. `cmd/bkmr` replaces its fetcher with one that
cannot make a request; `internal/fetch`'s own tests point the real one at an
`httptest` server they started. Keep it that way — a privacy tool whose test
suite phones out is exactly the wrong look.

Nothing in the suite calls `t.Parallel()`, and several packages depend on that:
package-level function values are swapped in place by tests (`renameFile` in
`internal/store`, the style renderers in `internal/tui`, `browser.Endpoints`,
and the `fetchTitle`, `readPassword`, `readClipboard`, `confirm`, `chooseTab`
and `tui.Open` seams in `cmd/bkmr`). Those swaps are safe only while tests in a
package run one at a time. If you want parallel tests, the seams have to become
parameters first — that is a real refactor, not a line in a test file.

## The boundary tests are not negotiable

`internal/boundary` holds five rules:

1. Networking packages — `net`, `net/http` and its relatives, `crypto/tls`,
   anything under `golang.org/x/net/` — are imported only by `internal/fetch`
   and `internal/capture/browser`. (`net/url` is string parsing and is fine
   anywhere; there is a test that keeps it that way.)
2. Cryptographic primitives are imported only by `internal/crypto`.
3. `go.mod` has exactly nine direct dependencies.
4. No `.go` file contains a literal control character.
5. Rule 1 is not passing vacuously — the two allowlisted packages must actually
   be seen importing `net/http`. This is the one you are most likely to trip
   while refactoring `internal/fetch`, and its failure means nothing is wrong
   with your code: rule 1 can only prove something when it has something to look
   at, and this test is what notices when it stopped having that. Move the
   network code, update `netAllowed`, done. Do not delete it — without it, moving
   `internal/fetch` and forgetting the allowlist would leave rule 1 green and
   guarding nothing.

**Do not edit these tests to make a change pass.** They are the enforcement
behind claims the README makes to users about what this tool does and does not
do, and a PR that loosens one to let code through will be closed. If your change
genuinely needs a rule widened, that is a design discussion first: open an issue
saying what you need and why, and change the rule in its own commit, with the
reasoning, once that is settled.

A tenth direct dependency needs a justification in the PR — the small
dependency tree is a privacy feature, not tidiness, because every direct module
is code running in the same address space as the vault key. Reach for the
standard library first. The rule for the control-character test is the same
trap it was written for: write `"\x1b[2J"`, never the byte itself.

## Changing the crypto or the vault format

Any change under `internal/crypto`, or to the on-disk layout, needs all of:

- **a bumped format version** in the header,
- **a migration** that opens a version-1 vault and writes the new one,
- **a new golden fixture** for the new version, checked in, and
- **the existing golden fixture still opening.** `internal/crypto/testdata/golden_v1.bkmr`
  pins format version 1 forever and is byte-exact; it is deliberately exempt
  from `.gitignore`. A change that stops it opening is data loss for everybody
  who already has a vault.

No hand-rolled constructions, and no new cryptographic dependency.

## Style

- `gofmt`. No other formatter, no configuration.
- Comments explain *why*, not *what*. The existing ones are long where the
  reasoning was hard-won; match that where it applies and stay quiet where it
  does not.
- A failed title fetch must never lose a bookmark. Network is decoration, never
  a gatekeeper — the same goes for any future optional enrichment.
- No telemetry, no analytics, no crash reporting. Ever, under any name.

## Reporting a security issue

Not here — see [SECURITY.md](SECURITY.md). Do not open a public issue for a
vulnerability.
