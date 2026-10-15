---
status: accepted
date: 2026-10-15
deciders: maintainer
---

# Share exact artifact version options through CLI and MCP

## Context

Spec #180 and Desk #404 separate artifact identity/version from document format,
mutable draft revisions and digest identity. Packs already have an exact
three-component grammar. Dialects' future artifact identity remains a shared
contract decision. Clients need deterministic arithmetic without a different
numeric parser in every editor or language-model prompt.

## Decision

Add `artifact version --base <x.y.z> [--candidate <x.y.z>]` and the read-only MCP
tool `get_artifact_version_options`. Return all three bump choices, optionally
with the candidate's numeric relationship to the base. The report scope is
`version-arithmetic`; `compatibilityChecked` is always false. No operation selects
a bump, writes a document, reads a project, resolves a format or reserves an
identity. This does not add a new dialect format or change a released schema.

Use the Core pack grammar without suffixes or leading zeroes, exact arithmetic
for components, and numeric comparison by component. Bound each input to 256
ASCII bytes. Refuse a base if any proposed next version would exceed that bound;
every returned choice fits the version grammar and input byte bound. A subsequent
bump may be refused if its own choices would exceed the bound. This is an
operation resource bound, not a change to Core's schema grammar. Reports for
invalid inputs contain no successful base, choices or comparison. CLI and MCP
have the same structured report apart from command name. Invalid values exit 1;
CLI invocation errors retain the existing invocation status. The public report
uses the existing output envelope/version.

The client chooses a bump under its publication policy. Runner owns durable job
usage and frozen releases. Desk's authoritative save/publish boundary enforces
application policy, optimistic concurrency, approvals and exact checked bytes.
A read-only arithmetic result is neither evidence of unused identity nor a grant
to write. Plan may inspect choices without transitioning to execution.

Spec owns portable identity, version grammar, canonicalization and immutability.
The shared contract proposal must land before a writer emits a changed Dialect
format. First-job promotion or post-use bump rules are application policy;
Runtime must not infer them from local state or fabricate compatibility findings.

## Evidence and review

Tests cover exact carry and ordering beyond floating-point precision, malformed
and oversized inputs, whole-report refusal, missing/null/duplicate/case-variant
arguments, CLI/MCP parity, closed discovery schemas and absence of project access.
The new tool is additive and read-only; no dependency was added.

Material-decision impact: public-surface, documented-claim. Recorded review by a
model from a different drafting vendor and maintainer dispositions are required
before merge under this directory's README. This local implementation is not a
released contract and has not been merged or installed.
