---
status: proposed
date: 2026-10-19
deciders: maintainer
---

# Adopt immutable official Policy draft resources in the shared offline reader

## Context and problem statement

The independent Spec release `policy-v1.0.0-draft.1` publishes the bounded Policy
draft at exact document format `1.0.0`. Runtime v0.36.0 already reads the identical
proposed grammar, but pins historical candidate schemas and reports proposed
maturity. New checks need an explicit immutable official source without changing
retained artifact identities, prototype defaults or host authority.

## Decision drivers

- Pin the actual published archive, HTTPS schemas, prose and complete 124-row corpus.
- Reuse the existing bounded reader and its original-byte, typed Pack and URI checks.
- Keep prototype reports byte compatible and historical candidate evidence identifiable.
- Make observable report/schema metadata changes explicit to strict consumers.
- Leave migration, approvals, Jobs, providers and current authority to their owners.

## Considered options

- Leave explicit 1.0.0 operations pinned to the unadopted candidate.
- Add a second engine or host authority/profile selection layer for identical grammar.
- Repin explicit 1.0.0 operations to the official draft and retain historical resources.

## Decision outcome

Chosen option: repin existing explicit 1.0.0 operations to official draft resources.
The complete 387-file published archive is retained under `internal/policy/official`
with original source paths. Its provenance manifest records release/tag
`policy-v1.0.0-draft.1`, source commit
`0799b37b74e35c4a86bfbf70f7bee80bdb7bbe94`, archive SHA-256 and per-file hashes and
lengths. An independent manifest digest and complete inventory verification guard
schema compilation and the Runtime release check. The original Spec reader and
dependencies remain unchanged and runnable against that archive. No mutable source
is fetched. The old candidate and prototype vendor trees and provenance stay frozen.

This partially supersedes ADR-0060's determination of the source/maturity for new
explicit 1.0.0 operations. Its dispatch, byte-preservation, URI, transport and
authority boundaries remain in force. Default contract discovery, prototype
reports and `0.0.2` operations retain their exact prior behavior. The all-formats
registry still has version `1` and contains prototype and official draft entries;
there is no separate current candidate selector for the same grammar.

Explicit 1.0.0 reports keep `outputVersion: "2"`, `contractVersion: "1"`,
`profile: "jps-policy-companion"`, `profileVersion`/`formatVersion: "1.0.0"` and the
existing `candidateCapabilities` and `candidateResult` names. Source revision and
URL now identify the immutable official `spec/policy.md`; maturity becomes `draft`.
`candidateCapabilities` is the exact released capability artifact. Validate's scope
is `closed-draft-document-and-local-references`; check's scope is
`draft-supplied-exact-bytes-anchors-and-scoped-core-references`.

Only full retained-material checks emit the complete portable result:
`{"status":"valid","formatVersion":"1.0.0","maturity":"draft"}` or the existing
finite refusal object. Narrow encoding/local validation and environmental failures
emit no full-check result. `experimental` remains true because these are the
existing experimental host tools; `fullDocumentConformance`, approval, execution,
live-authority and network-acquisition capabilities stay false. Full Core semantics
and evaluation are outside these checks. No stable or independently authored
interoperability claim is made. The Runtime conformance claim remains solely in
`CONFORMANCE.md`.

The official raw schemas retain their exact HTTPS `$id` values and bytes. Schema
operations return the existing self-contained editing form under a new derived ID:
`urn:judgmentpack:runtime:experimental-policy:policy-v1.0.0-draft.1:{kind}:authoring-schema:1`.
Its `resource.sourceId` names the immutable official schema. The changed copy is
never labeled with the upstream HTTPS identity or the historical candidate editing
URN. Prototype derived schema identities and report fields stay unchanged.

The 0.x minor release documents this intentional change to explicit 1.0.0 metadata.
Strict consumers must update their immutable Runtime/Spec pins and exact report
expectations. Report member names and finite results retain the existing protocol
shape, so the global output version does not change. Historical candidate reports
keep their original pin and proposed maturity; they are never relabeled. Promotion
changes no Policy/adoption, Pack, source, narrative or review-basis bytes. Prototype
migration requires separate explicit artifact creation, immutable identities and
dependent snapshot review under the released migration guide. No approval, Job,
permission or host lifecycle migrates.

### Consequences

- New explicit draft checks share the complete official corpus and immutable provenance.
- Strict explicit 1.0.0 consumers must update metadata pins before accepting new reports.
- The same shared engine preserves raw Core numbers, URI semantics and private wire limits.
- Revisit when a new exact format or host contract changes the supported boundary.

## Material decision and review

Material-decision impact categories: `public-surface`, `documented-claim`,
`conformance`, `security`, `dependency`. Official metadata and editing resource
identity change the public surface; immutable artifact integrity adds a release
gate. The published draft grammar and finite result ordering are reused, including
the existing input bounds and original-byte Core checks.

The maintainer's standing temporary different-vendor-review waiver applies to this
bounded Policy adoption. The parent coordinator will record the exact reviewed
commit, independent bounded review and any dispositions before merge. A second
Codex agent is same-vendor review and supplies no independent authorship evidence;
the waiver changes no standing repository rule. This proposed record does not
assert completed review, merge or a Runtime release.

## More information

- [Original candidate decision](0060-explicit-policy-candidate-format.md)
- [Official source provenance](../../internal/policy/official/provenance.json)
- [Promotion identity map](../../internal/policy/official/conformance/policy/1.0.0/PROMOTION.md)
- [Explicit prototype migration](../../internal/policy/official/conformance/policy/1.0.0/MIGRATION.md)
- [CLI documentation](../../README.md#official-policy-draft-selection)
- [MCP documentation](../mcp-clients.md#explicit-official-draft-selection)
