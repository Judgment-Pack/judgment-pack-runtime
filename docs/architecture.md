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

Each layer stops when the preceding layer fails. No validation layer evaluates a condition,
resolves a decision outcome, fetches a locator, loads an extension, or authorizes an action;
evaluation exists only behind the explicitly experimental surface of
[ADR-0007](adr/0007-experimental-evaluator.md).

That surface has its own flow, which the evaluator class of JPS Core `0.2.0-draft` fixes
([ADR-0010](adr/0010-evaluator-aligned-to-core-0.2.0-draft.md)):

```text
§8.2 input preflight, in this order and complete before resolution begins
   pack ──► facts document ──► evidence-availability document ──► required-extension set
        │
        ▼
§8 resolution over the admitted inputs
        │
        ├──► §8.3 portable disposition ──► RFC 8785 canonical bytes (internal/jcs)
        │
        └──► §8.4 evaluation error ──► one class, no disposition at all
```

The preflight order is also §8.4's error precedence — `pack-not-conformant`, `malformed-input`,
`unsupported-required-extension`, `resource-exhaustion` — so the first failure encountered is the one
reported, and an input error is never overtaken by a result. Every surface reports the byte limit
inside that preflight rather than at the read, so an oversized input is classed and ordered like any
other preflight condition. Only a pack declaring `specVersion` `0.2.0-draft` is evaluated — §11 makes the
declared value exact, so any other version is refused as `pack-not-conformant` in the preflight — and a
payload still names both the pack's `specVersion` and the contract's `evaluatorSpecVersion`. The
evaluation phase also carries the two limits §10 requires of the class — the evaluation-work limit and
the collection-size limit of `internal/evaluation/limits.go` — and reaching the work limit is
`resource-exhaustion` on the ordinary Core path, not only under the draft-RFC opt-in. The conformance
claim for this evaluator is stated, in full and only, in [`CONFORMANCE.md`](../CONFORMANCE.md) (ADR-0011);
this document states no part of it and every payload carries a reference to that file rather than a
claim. `experimental evaluate-corpus` runs the bundled evaluation corpus and reports row results: the
evidence §3.4.1 requires of a claim of this class, non-exhaustive by that section's own terms, and not a
claim — see [`CONFORMANCE.md`](../CONFORMANCE.md).

## Packages

- `internal/carrier` performs bounded strict JSON decoding and JSON Pointer tracking.
- `internal/artifacts` embeds exact files and verifies their lock before use.
- `internal/validation` compiles offline schemas and performs semantic checks.
- `internal/conformance` validates suite metadata and safely runs pinned cases.
- `internal/fssecure` opens selected local files defensively and enforces bounded regular-file reads, and roots a project's own reads at one directory so a configured path cannot leave it. It also holds the two writes bounded by a project's handle — appending a record beneath that same held directory (ADR-0018), and replacing a generated file whole (ADR-0019) — under exactly the refusals a read is held to, and it resolves nothing to a pathname for a caller to open. The runtime's other write, `--write` on `spec schema`, `spec examples`, and `packs schema`, is a create-exclusive copy at a pathname the operator named and is in `internal/cli`, outside this containment rule because it is outside any project.
- `internal/result` defines machine output version 2 and exit classes.
- `internal/describe` composes the machine descriptions the CLI and MCP server share, so neither drifts.
- `internal/evaluation` implements the EXPERIMENTAL JPS §§7–8 evaluator (ADR-0007), aligned to the §§8.2–8.4 evaluator class of Core `0.2.0-draft` (ADR-0010) and admitting only packs that declare that version, plus the bundled evaluation-corpus runner; and behind a further CLI opt-in the draft-RFC prototype of the specification's RFC 0008 collection quantifiers (ADR-0009), whose packs no published JPS version accepts and which the claim in `CONFORMANCE.md` does not cover.
- `internal/jcs` canonicalizes the one value JPS §8.3 compares byte for byte, the portable disposition, over the strings, arrays, and objects a disposition is made of and nothing else.
- `internal/project` implements the `jpack.json` project convention (ADR-0012): a NON-NORMATIVE convention of this runtime, not part of the specification, that names a project's packs by decision id, checks the files it points at, and runs each pack's instance matrix through the same row machinery the bundled evaluation corpus uses. It resolves nothing at decision time, reads no source a hint names, and holds no pack's identity — the pack document does, and everything here is a validated reference to it.
- `internal/graph` implements the EXPERIMENTAL graph surface (ADR-0015): a NON-NORMATIVE composition prototype for the specification's RFC 0002 (Draft) that evaluates a DAG of configured packs in deterministic topological order, feeding each node's disposition forward as declared facts and evidence availability. Every node runs through `internal/evaluation` unchanged; the composite envelope is this runtime's own, and every payload says so. `graph test` reports derived coverage beside its rows (ADR-0016) — per-node probes through the same exported derivation `packs test` uses, plus up to one probe per reachable edge branch (resolved and unresolved) — and coverage informs and never gates. Under `configVersion "2"` a project may declare its graphs and rows in `jpack.json` (ADR-0017), and `graph test`/`graph validate` with no argument walk them exactly as the path-named forms run one.
- `internal/audit` composes and appends the opt-in evaluation records of ADR-0018: one JSON line per completed evaluation, carrying the pack's identity and the digest of its exact bytes, the facts and evidence documents as they reached the engine, and the disposition as the canonical bytes the run produced rather than a re-serialization of them. It is reached only from a configuration that declared an audit directory, only after the evaluator has returned a result, and only from the three deciding surfaces — the test verbs run the same evaluator and record nothing. It evaluates nothing, changes no payload, and cannot influence a disposition; it does read a clock, which nothing on the payload path does.
- `internal/lock` implements the reviewed-set lock (ADR-0019): a generated, deterministic sibling of `jpack.json` pinning the digest of the configuration's exact bytes and of every declared pack and graph — not a declared matrix or rows document, which no deciding surface applies. Its presence is the whole of the opt-in — no configVersion moves and the configuration schema does not name it, because a configuration that named its own lock could rename it. `packs lock` writes it, `packs verify` names every difference from it, and the three deciding surfaces hold the law one evaluation applies to it before evaluating; the rehearsal verbs consult it never. It decides nothing about whether a change was right: an amendment and a tampering are the same act to a runtime, and it is not a wall against an editor that re-locks.
- `internal/cli` owns commands, streams, and human/JSON rendering.
- `internal/mcp` adapts the offline core onto Model Context Protocol tools over stdio.
- `tools/sync-spec-artifacts` is an explicit maintainer-only snapshot importer.

## Version independence

The CLI version, JPS `specVersion`, machine-output version, and any future plugin API version are
independent. During JPS `0.x`, registry dispatch requires the entire `specVersion`; prefix matching
and nearby-version substitution are forbidden.

Development builds report version `0.0.0-dev`. GoReleaser injects the exact tag version into
official binaries through a Go linker flag; source builds without that release metadata remain
development builds.

## Decisions

This document describes the runtime as it is. The cross-cutting decisions behind it -- why the layout
is idiomatic Go rather than a `packages/` monorepo, why language plurality lives at the wire, why MCP
is the integration surface -- are recorded as architecture decision records in
[`docs/adr/`](adr/README.md). Implementation decisions are ADRs; normative, cross-implementation
proposals belong to the specification's RFC process instead.

The create / read / update / delete authoring loop this runtime supports -- and why it lives in the
client rather than in a runtime store -- is walked end to end in
[authoring-lifecycle.md](authoring-lifecycle.md).
