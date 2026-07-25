---
status: accepted
date: 2026-07-25
deciders: Brian Jin
---

# Use a single `JPS-` prefix for diagnostic codes

## Context and problem statement

Diagnostic and error codes carried two prefixes: `JPS-` for document-conformance
diagnostics (carrier, structural, semantic, capability) and for the runtime's own
operational failures (input, resource, artifact, suite), and `JPR-` for the CLI's
argument/invocation errors alone. The `JPR-` prefix was defined nowhere, and the
split was inconsistent -- operational failures used `JPS-` while invocation
failures used `JPR-`, though both are runtime rather than specification
conditions. Every external reviewer read the lone `JPR-` code as a typo.

## Decision drivers

- One namespace is easier to consume and to document than an undocumented split.
- The README already names `JPS` as the sole prefix of the provisional diagnostic codes.
- The process exit class already conveys the error category; the prefix need not.
- Codes are marked `provisional`, so renaming them is permitted during `0.x`.

## Considered options

- **A. Unify under `JPS-`.** Rename `JPR-INVOCATION-*` to `JPS-INVOCATION-*`.
- **B. Make the two-prefix scheme consistent.** `JPR-` for every runtime, operational, and invocation code and `JPS-` for document-conformance only -- renaming dozens of `JPS-RESOURCE/INPUT/ARTIFACT/SUITE-*` codes and rewriting the README.
- **C. Keep `JPR-` and merely document it.**

## Decision outcome

Chosen option: **A**. All diagnostic codes use the single `JPS-` prefix; the
prefix carries no category semantics -- the process exit class does. This matches
the only documented statement (the README), touches only `internal/cli/app.go`
(no test, fixture, or the sha256-locked conformance bundle references a `JPR-`
code), and abandons the unenforceable "prefix tracks class" idea rather than
entrenching it.

### Consequences

- Good, because every code now shares one prefix and the README reads true as written.
- Bad, because codes are machine-visible; the rename is recorded in the CHANGELOG and is sanctioned only because codes are provisional pre-1.0.
- Revisit only if a deliberate, documented multi-namespace taxonomy is ever designed; do not reintroduce an undocumented prefix.

## More information

Exit classes and `codeStability: "provisional"` live in
[internal/result/result.go](../../internal/result/result.go).
