---
status: accepted
date: 2026-10-15
deciders: maintainer
---

# Offline dialect document tools, outside the decision evaluator

## Context

The dialect companion has a portable document contract, while authoring clients
currently rely on Desk's TypeScript writer and Runner's preparation-facing
checks. The Runtime repository already provides document tooling in addition to
its evaluator. Runtime #275 records the maintainer's request for a consistent
CLI/MCP dialect surface; Desk #402 and Runner #86 track its consumers.

The contract used here is Spec commit
`9fb1142546e79ab4d03f744415afc098a81efe3e`, `spec/dialect.md` §§1–3 and §12.
This is a pinned draft companion, not an additional Core `specVersion` or a new
conformance claim. This pin records reference Runner version-3 support since
v0.12.0; this change's capability report describes only this implementation.

## Decision

Add a pure `internal/dialect` document boundary and four offline operations:
`contract`, `init`, `canonicalize`, and `digest`. CLI and MCP use the same report
and implementation. This is the first delivery of #275. Full structural and
semantic validation, schema access and pack compatibility remain follow-up work.
The capability report explicitly says those checks and preparation verification
are unavailable. No command is named `validate` until its declared validation
scope is implemented.

- `init` returns an empty version-2 or version-3 dialect with no sources or
  copies. It creates no file or persisted draft. ADR-0006's authoring lifecycle
  and host-owned persistence remain unchanged. Starters follow the reference
  Runner spelling for their version and the JSON report marks `starterTemplate`.
- Encoding admits a bounded JSON object with a version of 2 or 3 after checking
  strict JSON, the canonical domain and version-3 identity spelling (§2).
  Forbidden version-2-only members and named optional empty containers are refused,
  never silently omitted. It reports `encoded`, never `valid`.
  Unknown members and source kinds remain encoded as supplied values; they have
  not passed semantic validation or executable-capability checks.
- Canonical bytes use code-point member order, preserve array order and optional
  members, and admit only integer literals within ±(2^53−1). Negative zero becomes
  zero. Duplicate keys (decoded names), malformed Unicode, fractions/exponents,
  unsafe integers, over-limit input and unsupported encoding versions are refused.
  Disposition JCS and preparation canonicalization are separate contracts and
  remain unchanged. The existing strict carrier is reused with dialect bounds.
- CLI reads a named local regular file or stdin, bounded before parsing. Human
  `init` and `canonicalize` write exactly the document bytes, with no newline;
  `digest` writes the digest followed by a newline. `--pretty` affects the JSON
  report only, never the reported document string or its digest. MCP receives
  text, never paths, and reads no project. Every operation writes no store and
  starts no acquisition or evaluation.
- JSON reports retain `outputVersion: "2"`, add `contractVersion: "1"`, and name
  the exact Spec revision and draft maturity. `fullDocumentConformance` is false.
  Refused documents have diagnostics and no bytes or digest. CLI uses the existing
  exit classes (invalid 1, unsupported 2, invocation 3, I/O 4); MCP returns the same
  report with `isError` on a refusal. Invalid tool arguments use ordinary tool
  errors. Clients must discover operations, not infer them from a version number.

Spec owns meaning. Runtime owns generic document tooling. Runner retains local
binding, source capability checks, acquisition coordination, preparation and its
verification. Runtime's evaluator continues to consume prepared facts; its audit
binding remains opaque as ADR-0050 specifies. Desk's UI/AI authoring and Exchange
publication remain consumers of the document contract. No public Go API or
cross-language package dependency is added; the CLI/MCP boundary follows ADR-0002.

## Evidence and delivery limits

Twelve staged mapping/preparation pairs and their upstream manifest are pinned.
The ten version-2 vectors contain six distinct mapping documents; two version-3
vectors add bound and unbound-at-import cases. The unbound dialect and binding are
retained too. Evidence is stored under `internal/dialect/testdata`. Each mapping's computed canonical digest is
compared to its retained preparation's `mappingDigest`; source checksums are held
exactly. Additional tests cover Unicode code-point ordering, safe integer limits,
absence/null, unchanged optional members, JSON-RPC argument boundaries, input
limits and identical CLI/MCP reports. These are encoding tests, not preparation
verification or source truth checks.

Before merge this public-surface and documented-claim change requires the recorded
cross-vendor review in this directory's README, tied to the reviewed commit.

## Alternatives and consequences

Keeping the only authoring writer in Desk makes non-Desk clients duplicate it.
Moving preparation into the evaluator would couple decisions to installations
and acquisition; this change adds only offline document tools to the repository.
Shipping partial structural checks as `validate` would let clients overstate
what was established. Explicit capabilities allow the first slice to be used
without that ambiguity while the complete document checks are implemented.

The new namespace is additive. Existing commands, audit bytes, evaluator
semantics and saved documents do not change. Packaging this work calls for a
minor release under VERSIONING.md; this PR creates no release or tag.
