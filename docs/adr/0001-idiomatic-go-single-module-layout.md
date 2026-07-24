---
status: accepted
date: 2026-07-24
deciders: Brian Jin
---

# Idiomatic Go single-module layout; no `packages/` umbrella

## Context and problem statement

An early structure sketch drew a polyglot-monorepo layout: a `packages/` parent holding
`core/`, `conformance/`, `cli/`, `api/`, `mcp/`, `sdk/`, and a `pyproject.toml`-or-equivalent at the
root. The runtime is in fact a single Go module. The question was whether to reshape the repository
toward that sketch or to follow Go's own conventions.

## Decision drivers

- In Go a directory is a package and its path **is** the import path; there are no neutral grouping
  folders.
- Go already provides entrypoint/private/public grouping with compiler-enforced meaning.
- The runtime ships as a single static binary.
- A layout Go developers recognize at a glance lowers the cost of every future contribution.

## Considered options

- **A. Idiomatic Go.** `cmd/` for entrypoints, `internal/` for private implementation, top-level
  directories (e.g. `sdk/`) for the public surface.
- **B. `packages/` umbrella** wrapping the same packages.
- **C. Multi-module workspace** with `go.work` and per-module `go.mod`.

## Decision outcome

Chosen option: **A**, because the grouping a `packages/` folder would provide already exists here
with stronger semantics, and adding it only takes something away:

- `cmd/` = the `main` packages; `internal/` = private, with the compiler forbidding import from
  outside the module; top-level dirs = the public, importable surface.
- A `packages/` wrapper prepends a dead segment to every import path
  (`.../judgment-pack-runtime/packages/internal/validation`), duplicates the role `internal/`
  already plays, and would still need an `internal/` beneath it to get privacy.
- `packages/` is a JavaScript/TypeScript and Python monorepo idiom for a workspace of
  independently-publishable modules. This repository is one module whose layers are not
  independently published — most are `internal/` — and which compile into one binary. There is no
  workspace to group.

### Consequences

- Good, because the tree matches Go tooling and reader expectations, and the single-binary story is
  unaffected.
- Bad, because the repository no longer visually resembles the original cross-language sketch.
- Revisit when the repository genuinely becomes multi-module or multi-language — at which point the
  idiom is sibling modules plus `go.work` (option C), still **not** a `packages/` umbrella. Other
  languages are expected to live in their own repositories; see
  [0002-language-plurality-at-the-wire.md](0002-language-plurality-at-the-wire.md).

## More information

Current top-level layout is described in [architecture.md](../architecture.md).
