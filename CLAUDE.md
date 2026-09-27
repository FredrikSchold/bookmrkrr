# BookMrkr — working agreement

Terminal-first, locally encrypted bookmark manager. Go. Binary: `bkmr`. MIT.

Design spec: `docs/superpowers/specs/2026-09-27-bookmrkrr-design.md`. Read it
before proposing changes; it is the source of truth for architecture, the vault
format, and scope.

## Required skills

**Invoke `ponytail` on every coding task in this repo** — writing, adding,
refactoring, fixing, reviewing, or designing code, and whenever choosing a
library or dependency. The laziest solution that actually works wins. Question
whether the code needs to exist, reach for the standard library before a
dependency, one line before fifty. This repo has a deliberate eight-dependency
budget (spec section 12); adding a ninth needs a justification in the PR.

**Invoke `caveman` for communication.** Default intensity. Keep replies short
and dense; full technical accuracy, minimal prose.

Both are skills — call the `Skill` tool, do not merely imitate the style.

## Non-negotiables

- **No new outbound network calls outside `internal/fetch` and
  `internal/capture/browser`.** A CI import-graph test enforces this. If a
  change needs network elsewhere, that is a design discussion, not an edit.
- **All cryptography stays in `internal/crypto`.** No crypto primitives
  elsewhere, no hand-rolled constructions, no new crypto dependency.
- **The vault format is versioned and the golden test vault must keep opening.**
  Never change the on-disk layout without bumping the version and adding a
  migration plus a new golden fixture.
- **A failed title fetch must never lose a bookmark.**
- No telemetry, no analytics, no crash reporting, ever.

## Development workflow

- TDD. Test first, watch it fail, then implement.
- No CGO. Cross-compilation must stay a single command.
- Run `go vet` and `staticcheck` before claiming work is done, and report real
  output rather than asserting success.
