---
status: accepted
date: 2026-10-18
deciders: maintainer
---

# Prototype Policy document and retained-reference checks offline

## Context and problem statement

RFC 0023 proposes a Policy companion, but remains Draft and nonnormative. Clients
need consistent document identity and reference checks without treating a prototype
as stable law or making Runtime responsible for source access, approval or publication.
The existing offline CLI/MCP boundary can supply these checks from exact caller bytes.

## Decision drivers

- Pin exact prototype `0.0.2`, schemas and synthetic fixtures with source provenance.
- Share one bounded, strict implementation between CLI and MCP.
- Keep document validity, retained-byte checks, canonical identity and authority distinct.
- Avoid locators, implicit project reads, persistence and evaluator side effects.

## Considered options

- Add stable Policy formats or infer a default format.
- Leave clients to implement separate document and retained-material validators.
- Add an explicit experimental offline prototype surface with supplied bytes.

## Decision outcome

Chosen option: an explicit experimental offline surface. `internal/policy` implements
`contract`, `init`, `schema`, `canonicalize`, `digest`, `validate` and `check`, adapted
by `experimental policy` on CLI and seven `experimental_*policy*` MCP tools. Init
and schema require an exact kind and format selector. Init additionally requires
artifact identity, and reports `starter-template` with missing retained materials.
No stable Policy format is selected or claimed.

Validate checks strict carrier/schema forms and local references. Check additionally
checks the exact supplied retained-material reference closure, raw byte digests and
lengths, section anchors, structural Core packs and specific consumer references.
It performs no full Core semantic validation or evaluation. Canonical identity and
raw byte identity are separate; a retained Core pack is held to the canonical number
domain only when a reference explicitly requests canonical identity.

The supplied material carrier is private Runtime transport:
`{materials:[{content:{bytesDigest,byteLength,mediaType},dataBase64}]}`. It is not
a portable release envelope and carries no paths or URLs. CLI opens only named
bounded regular files or one standard-input operand. MCP accepts exact JSON text
strings and reads no project or other file. Neither surface writes or fetches.
Reports and errors omit retained plaintext and base64 material.

The limits distinguish 256 KiB documents, 4 MiB source originals, 1 MiB narrative
and review-basis material, 16 MiB aggregate distinct retained bytes and 24 MiB material JSON
text. MCP's existing 16 MiB stdio line bound remains unchanged: JSON/base64 overhead
means some material bundles beneath the retained-byte limit cannot fit that transport.
The CLI can accept the larger named-file material JSON envelope. Schema reports
select the first 100 sorted unique diagnostics and set `diagnosticsTruncated: true`
when findings remain; material length literals are bounded lexically before
schema numeric conversion.

The RFC, schemas and synthetic examples are pinned from specification revision
`8a56a95d555c17640d2db4984363b779536b3c2a`, with per-file digests and license provenance.
RFC 0023 remains Draft; the prototype and Runtime implementation are nonnormative.
Checking a document establishes no publication, approval, governing-adoption
authority, source access, billing behavior or standard conformance.

### Consequences

- Good, because clients share exact offline checks with typed diagnostics and explicit scope.
- Bad, because prototype compatibility is not promised and clients must retain and supply bytes.
- Revisit when the specification publishes a normative Policy contract or a reviewed transport.

## Material decision and review

Material-decision impact categories: `public-surface`, `documented-claim`, `dependency`,
`security`. The public surface and capability statements are new; the pinned
prototype artifacts add a dependency boundary. Bounded material parsing and shared
MCP refusals concern the security posture. Duplicate unknown argument names are
refused with fixed messages; known duplicate and case findings name only trusted
canonical members, keeping rejected source prose and base64 out of the response.
The existing 16 MiB stdio request-line limit is unchanged.

The maintainer explicitly authorized a temporary waiver of the repository's
different-vendor review requirement for this current Policy work. The bounded
clean-room OpenAI/Codex review of candidate
`f6cad9d8b1516485c8e695c56dc2bdd9e4af693e` is same-vendor review under that waiver;
it is not different-vendor review or independent implementation evidence. The
introducing pull request records this current exception, the reviewed SHA, each
finding's maintainer disposition, and the implementation and verification of
accepted findings. The waiver is scoped to this work and changes no standing
review rule. This ADR's accepted status is the authorized merge-ready procedural
transition; it adopts no normative Policy format and makes no standard-conformance
claim.

## More information

- [Architecture](../architecture.md#experimental-offline-policy-prototype)
- [CLI commands](../../README.md#experimental-offline-policy-prototype)
- [MCP tools and transport limits](../mcp-clients.md#experimental-policy-prototype-tools)
