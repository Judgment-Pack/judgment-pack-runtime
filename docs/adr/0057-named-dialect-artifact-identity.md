---
status: accepted
date: 2026-10-16
deciders: maintainer
---

# Share offline tools for named Dialect artifacts

## Context and decision

Spec RFC 0021, merged in Spec #187, separates a reusable Dialect's format from
its opaque identity and artifact version. Extend the existing encoder, validator,
authoring schemas, initializer and CLI/MCP reports with exact named selectors
`2.0.0` and `3.0.0`. Reuse the shared artifact-version grammar. Preserve integer
formats and all historical fixture identities. Pin reference and staged fixture
bytes to merged Spec commit b7cc8906c23d92c63f4e42c4706cc1e21b9d324f.

Both named forms require `formatVersion`, `id`, `version` and `case`, reject legacy
root members, and apply strict omission spelling. The named header participates
in canonical identity. Artifact IDs are not normalized; malformed UTF-8 and
forbidden whitespace/control bytes fail. Initialization checks supplied argument
presence, including empty/null arguments, rather than silently ignoring them.
Authoring schemas use the same strict boundaries and family-specific source
scope as validation; they remain nonnormative editing aids.

This extends ADRs 0052–0056 without moving publication policy or storage into
Runtime. Runtime does not publish, bump, bind or execute a Dialect, and a check
cannot authorize those actions. Runner retains preparation and binding ownership;
Desk retains recipe conversions, publication review and persistence. No network
fetch, new dependency, record migration or broader conformance claim is added.

## Verification and review

Pinned named fixtures, compiled authoring schemas, malformed identifiers,
omission constraints, CLI/MCP parity and all existing tests are required.

Material-decision impact: public-surface, documented-claim, conformance. The PR
records clean-room Codex review under the maintainer's explicit same-vendor
exception. It does not claim to satisfy the different-vendor requirement.
