---
status: proposed
date: 2026-10-18
deciders: maintainer
---

# Add an explicit Policy contract candidate without changing prototype clients

## Context and problem statement

The RFC 0023 Policy contract candidate introduces `formatVersion: "1.0.0"` and
`workspace-managed` ownership intent. It remains proposed at draft maturity,
outside adopted specification and conformance artifacts. Existing Runtime
consumers strictly decode prototype reports; even an additive default field
would break their discovery and authoring flows.

## Decision drivers

- Preserve prototype 0.0.2 source bytes, provenance, reports and default discovery.
- Share exact-byte, anchor, material and typed Pack checks across both formats.
- Follow the candidate's finite result codes and ordered refusal phases.
- Keep current authority, source acquisition, signatures and evaluation outside Runtime.

## Considered options

- Replace the prototype grammar or silently migrate candidate bytes to it.
- Duplicate the Policy checking engine for the candidate.
- Add explicit dispatch and optional discovery around the shared offline engine.

## Decision outcome

Chosen option: explicit format dispatch around the shared engine. Candidate
artifacts are separately vendored from immutable Spec commit
`4406cec9714ab9bc8e8971237cc79d58682cc904`, with a complete per-file provenance
manifest and independent manifest digest test. Prototype files and ADR-0059
remain unchanged. This record adopts no normative Policy format.

The no-argument CLI/MCP contract remains exactly the prototype contract.
`contract --all-formats` and MCP `{allFormats:true}` explicitly request a separate
`registryVersion: "1"` report. An optional exact format selector requests a
single contract; the two discovery selectors are mutually exclusive. Init/schema
continue requiring explicit kind and format. Supplied documents dispatch by
their selector; an optional operation selector permits candidate classification
of malformed or missing document selectors without changing legacy refusals.
No document is silently migrated or tried against an older grammar.

Candidate reports identify their own provenance and `proposed-draft` maturity,
carry the exact candidate capability artifact, and add `formatVersion`.
Candidate full checks alone add `candidateResult`: exactly the portable valid or
refusal object. Canonicalize/digest check encoding and identity; validate checks
document forms and local references. Neither narrower operation claims a full
retained-material check. Runtime retains invalid exit 1 and unsupported exit 2;
the nested candidate result supplies the portable code. Environment failures
produce no candidate result.

Full checks measure/parse/select/canonicalize the root before registration count
and ordered material registration, then check its kind/schema and semantics.
Retained Policy references require the adopting candidate's exact format.
Resolution checks original raw bytes before parsing or optional canonical
identity, and Core structural validation receives the original Pack bytes.
Source and narrative bytes, registrations under distinct media types, consuming
role bounds and typed Pack references remain shared checks.

The private Runtime base64 material transport remains separate from portable raw
vector inputs. Its 24 MiB material JSON ceiling and MCP's 16 MiB request-line
ceiling remain unchanged. Repeated registrations of identical large bytes can
fit the candidate's distinct-byte budget while exceeding an encoded transport
ceiling. The portable harness therefore uses the same shared post-parse checks
and raw registration function, acquires sources lazily in declaration order and
stops on the first failure. Passing all 104 vectors promises no larger wire
envelope, authority, conformance or independent implementation agreement.

### Consequences

- Existing strict prototype clients retain their default contract and reports.
- Candidate users opt into a proposed contract with explicit provenance and refusal scope.
- Shared checks must preserve format-specific phase ordering as well as byte identity.
- Revisit when RFC review adopts or replaces this candidate and the owning host contracts settle publication and current admission.

## Material decision and review

Material-decision impact categories: `public-surface`, `documented-claim`,
`security`, `dependency`. The candidate selector, explicit discovery and typed
results extend a public surface; vendored candidate artifacts add a dependency
boundary; ordered bounded input handling concerns the security posture.

The maintainer authorized a temporary different-vendor review waiver for this
current Policy work. OpenAI/Codex implementation and any OpenAI/Codex adversarial
review are same-vendor work under that waiver, never different-vendor review or
independently authored interoperability evidence. Review records must identify
the reviewed commit and maintainer dispositions. The waiver changes no standing
repository requirement. This record remains proposed pending that procedural
disposition.

## More information

- [Prototype decision](0059-experimental-offline-policy-tools.md)
- [Offline Policy architecture](../architecture.md#experimental-offline-policy-prototype)
- [Candidate source and provenance](../../internal/policy/candidate/provenance.json)
