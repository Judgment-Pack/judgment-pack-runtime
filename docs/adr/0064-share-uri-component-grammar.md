---
status: proposed
date: 2026-10-23
deciders: maintainer
---

# Share RFC 3986 URI grammar while preserving prototype Policy compatibility

## Context and problem statement

Ordinary Core `format: "uri"` combined a broad character allowlist with Go's
`net/url.Parse`. It accepted forbidden raw brackets in paths, queries and
fragments, but rejected valid percent-encoded reg-names and IPvFuture literals.
ADR-0060 introduced corrected candidate URI checking while deliberately leaving
the default predicate alone. Issue #297 opens that default compatibility change.

## Decision drivers

- Enforce the entire ASCII RFC 3986 Appendix A `URI` component grammar.
- Share one corrected parser across ordinary Core and official/candidate Policy.
- Preserve historical prototype Policy admission through an explicit local scope.
- Preserve exact source bytes, identifier spelling, finite diagnostics and pins.
- Keep generic syntax separate from network access, trust and authority.

## Considered options

- Retain the default defect and the permanent candidate parser fork.
- Change all callers, including the historical prototype Policy checker.
- Share the corrected parser by default, with an explicit prototype override.

## Decision outcome

Choose the third option. Extract the reviewed candidate component parser to
`internal/uriformat`; all ordinary Core schema compilation and validation use it.
This covers Core `0.1.0-draft`, `0.2.0-draft` and `0.3.0` document validation,
CLI/MCP validation and Core admission to the supported `0.2.0-draft` and `0.3.0`
evaluators. The official Policy `1.0.0` reader and its retained Core consumers use
the same assertion. There is no second corrected Policy RFC parser.

The historical predicate lives in `internal/policy/prototype_uri.go` under the
explicit name `prototypeURIFormat`. Only prototype `0.0.2` schema compilation and
its retained Core consumer validation override the default. That code preserves
compatibility with historical admission, including its known URI disagreements;
it is not claimed to implement RFC 3986 accurately. Separate compilation keeps
one request's choice out of cached ordinary Core schemas. No embedded prototype,
candidate, official or Core source bytes or provenance change.

This decision partially supersedes ADR-0060's determination that only candidate
schemas and candidate consumers use component grammar and ordinary Core keeps
the former predicate. Its prototype compatibility determination remains in force.
This is a deliberate Runtime assertion correction across exact Core versions,
without changing a schema resource or implying a new specification version.

### Acceptance and migration

The following changes apply to ordinary Core through every entry point:

| Identifier | Former default | Corrected default |
| --- | --- | --- |
| `urn:test:[x]` | accepted | refused |
| `https://example.invalid/[x]` | accepted | refused |
| `https://%65xample.invalid/a` | refused | accepted |
| `foo://[v1.a]/` | refused | accepted |

Queries and fragments also refuse raw brackets. Valid percent escapes retain
case and remain encoded. Both `v` and `V` introduce IPvFuture; valid IPv6,
userinfo, query/fragment punctuation, empty and arbitrarily large digit-only
ports remain accepted. Numeric reg-names are not restricted to valid IPv4 or DNS
syntax. In contrast, a dotted IPv4 tail within an IPv6 literal must meet the
IPv4 grammar, including no leading zeroes. Scoped IPv6, malformed escapes,
relative references, raw non-ASCII/control characters (including final LF), and
characters forbidden in each component remain refused.

The parser examines the whole string and never decodes, normalizes, fetches or
rewrites an identifier. `format: "uri"` checks generic syntax; it grants no
scheme-specific validity, reachability, publication, trust or authority. No URI
length bound is added to Core; existing carrier/schema/companion limits retain
their own scopes. Structural failures still use `JPS-STRUCTURE-FORMAT-URI` at the
original instance path with the existing finite message and exit class.

During the `0.x` research period this acceptance change requires a documented
minor release. Automation must update its Runtime pin deliberately and revalidate
original source bytes before relying on the corrected acceptance set. Existing
stored IDs, audit records, canonical identities and jobs are never rewritten or
silently migrated. A caller needing a corrected artifact must author and review
its new immutable identity and dependent references explicitly. Prototype Policy
compatibility is scoped by its existing format selector, not by a general legacy
Core switch.

### Verification and review

Hand-authored positive and negative fixtures exercise all component classes and
the four issue reproductions through all supported Core document versions.
The Go parser and the repaired Python reader retained in the official Policy
archive check those same expectations. The comparison uses the reader's explicit
final-LF, uppercase-IPvFuture and embedded-IPv4 repairs, not the dependency's known
bugs. Supplemental omission/mutation probes check that meaningful assertion
removal is detected; integration tests check finite refusals and exact IDs/bytes.
These are implementation evidence, not independent interoperability or a complete
Core `0.3.0` evaluator-conformance claim. That claim remains deferred in
[CONFORMANCE.md](../../CONFORMANCE.md).

Material-decision impact: `public-surface`, `documented-claim`, `conformance`.
Required platform CI and a recorded material review/disposition on the introducing
PR remain gates. This proposed ADR records the candidate compatibility choice;
it does not assert those gates have completed.

## More information

- [RFC 3986 Appendix A](https://www.rfc-editor.org/rfc/rfc3986#appendix-A)
- [Runtime issue #297](https://github.com/Judgment-Pack/judgment-pack-runtime/issues/297)
- [Spec correction PR #217](https://github.com/Judgment-Pack/judgment-pack-spec/pull/217)
- [Candidate compatibility decision](0060-explicit-policy-candidate-format.md)
