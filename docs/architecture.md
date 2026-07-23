# Architecture

## Repository boundary

`judgment-pack-runtime` owns the executable, command UX, result rendering, resource limits, packaging, and
future public extension contracts. It is one nonnormative consumer of JPS.

`judgment-pack-spec` separately owns normative prose and schemas plus nonnormative public examples,
conformance metadata, and release records. The CLI consumes reviewed immutable snapshots and does
not write to or release the specification repository.

Private commercial repositories may depend on the executable and versioned output contract. The
current Go packages are internal implementation details; a versioned in-process composition or
plugin API remains future work. The public CLI never depends on private source, packages, services,
or credentials.

## Runtime flow

```text
bounded file/stdin bytes
        │
        ▼
strict UTF-8 JSON carrier parser
        │
        ▼
exact specVersion registry ──► embedded schema and metadata
        │
        ▼
Draft 2020-12 structural validator
        │
        ▼
JPS semantic references and declarations
        │
        ▼
required-extension capability check
        │
        ▼
one result model ──► human or versioned JSON renderer
```

Each layer stops when the preceding layer fails. No layer evaluates a condition, resolves a
decision outcome, fetches a locator, loads an extension, or authorizes an action.

## Packages

- `internal/carrier` performs bounded strict JSON decoding and JSON Pointer tracking.
- `internal/artifacts` embeds exact files and verifies their lock before use.
- `internal/validation` compiles offline schemas and performs semantic checks.
- `internal/conformance` validates suite metadata and safely runs pinned cases.
- `internal/fssecure` opens selected local files defensively and enforces bounded regular-file reads.
- `internal/result` defines machine output version 1 and exit classes.
- `internal/cli` owns commands, streams, and human/JSON rendering.
- `tools/sync-spec-artifacts` is an explicit maintainer-only snapshot importer.

## Version independence

The CLI version, JPS `specVersion`, machine-output version, and any future plugin API version are
independent. During JPS `0.x`, registry dispatch requires the entire `specVersion`; prefix matching
and nearby-version substitution are forbidden.

Development builds report version `0.0.0-dev`. GoReleaser injects the exact tag version into
official binaries through a Go linker flag; source builds without that release metadata remain
development builds.
