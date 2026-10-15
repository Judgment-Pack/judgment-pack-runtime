---
status: accepted
date: 2026-10-15
deciders: maintainer
---

# Serve dialect authoring resources and check declared pack coverage

## Context

Runtime #275 asks for schema/reference access and checks of a dialect against a
pack, in addition to ADR-0052's encoding and ADR-0053's document validation. The
pinned companion specifies document forms in prose; it supplies no normative
JSON schema. Runtime already derives a pack's consulted pointers and evidence
requirements for its inventory (ADR-0020), and Runner compares those declarations
with mapping targets when reviewing a release. An editor should not reimplement
that inventory or treat a digest as proof of compatible prepared data.

## Decision

Add three offline CLI operations and equivalent read-only MCP tools:

- `dialect schema [--dialect-version 2]` / `get_dialect_schema`: a Runtime-derived
  JSON Schema 2020-12 editing aid for version 2, labelled `runtime-derived` and
  `version-2-structure-aid`. Strict JSON, integer literal spelling, byte/depth
  limits, cross-references, target ownership, aggregate counts and final-clause
  rules still require `dialect validate`. Schema acceptance is not validation.
- `dialect reference` / `get_dialect_reference`: the exact embedded pinned
  companion text, labelled `pinned-specification`. It includes proposed forms;
  availability of the reference does not advertise their implementation.
- `dialect check <dialect-or-> --pack <pack-or->` / `dialect_check`: validate two
  supplied snapshots, then check version-2 mapping targets against the declared
  pack inventory. MCP takes `document` and `pack` as strings, never paths. CLI
  permits stdin for one operand only. No project, profile, acquisition or store
  is consulted. Neither operation invokes the evaluator or saves anything.

Resource reports contain exact text, media type, byte length and SHA-256, separate
from a dialect's canonical bytes and digest. These resources are embedded and
never fetched at runtime. The reference retains the pinned upstream checksum;
Git preserves the derived schema's bytes across platforms too.

The coverage checker reuses Runtime's identity/inventory derivation through a
pure snapshot interface. Pack validation uses the existing semantic validator;
unsupported versions/extensions, invalid documents and uncheckable collection
scope yield no coverage result. The inventory remains the intentional
*over-approximation* of ADR-0020, including condition-shaped data. A non-pointer
in that inventory is refused rather than quietly ignored.

For each consulted fact pointer, report:

- `mapped`: a target at that pointer or an ancestor declares the subtree;
- `partial`: a descendant is mapped, or the pack reads the root facts object;
- `acknowledged`: an `unmapped` pointer covers the otherwise unmapped read;
- `missing`: neither a mapping nor an acknowledgement covers it.

Evidence ids are exact matches. Undeclared mapped evidence or evidence omissions
are errors. Extra fact targets, partial aggregates, explicit omissions, redundant
omissions and unused fact omissions remain visible notes. The whole-token prefix
primitive is shared with existing producer lint. Lint's established boolean
behavior is preserved; the dialect report distinguishes descendant coverage,
as Runner's release review does, instead of presenting it as full coverage.

Overall `complete` and `review-required` exit 0: the latter is a completed static
check with visible review notes, not full mapping completeness. `incomplete`
(missing producers or invalid evidence declarations) and `not-applicable` (no
reads, requirements or notes) exit 1. `not-applicable` does not supply a green gate
over zero checks. Parsing/validation/capability refusals say `refused`, with no
coverage, dialect bytes or digest. A completed incomplete check retains the
valid dialect identity and its findings. MCP mirrors exit failure with `isError`.
A valid dialect's bytes are never changed, and the checked pack is identified by
its id, version, specVersion and SHA-256 of the exact supplied bytes.

This is **declared inventory coverage**. Actual value types and availability,
profile/request compatibility, installed source capabilities, preparation
verification and decision correctness are not checked. The report says so in-band;
no inferred numeric coercions, policy decisions or expected outcomes are added.
The pack is bounded to the existing 10 MiB carrier-file limit, the dialect to
131072 bytes. A conservative work estimate refuses more than 1,000,000 pointer
and evidence comparisons before any coverage is emitted. Coverage is charged
incrementally against a 2 MiB encoded-size budget, before each component is
retained. String escaping is counted without allocating the encoded report;
framing is conservatively charged. Exceeding the budget refuses the whole
report, keeping construction and transport replies bounded. Capability discovery
states the supported schema/check versions, scope and limits. Version 3 remains
unsupported by these checks, independently of its encoding support.

## Evidence and consequences

Tests cover the pinned resources and mappings, derived-schema admission and its
intentional semantic limits, parent/child/escaped pointer cases, evidence and
omission ownership, zero checks, exact-byte identities, invalid and unsupported
inputs, work refusal, CLI/MCP report parity, strict arguments and no project
lookup or file mutation. Existing producer-lint tests protect its behavior.

This additive public surface is the next unreleased slice of #275, stacked on
#278. It does not change Spec, prepare data, migrate Desk or publish a release.
The schema is an implementation aid, not a new spec authority. Before merge this
material public-surface, documented-claim, conformance and dependency decision
requires the recorded cross-vendor review under this directory's README. A
clean-room Codex review is supplementary evidence when Codex assisted drafting.
