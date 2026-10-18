---
status: accepted
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
`0852c5fa7726e51b36e574a5b110e7f5e012e03d`, with a complete per-file provenance
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

Candidate URI assertions use the RFC 3986 `URI` production: a scheme is required
and a fragment is permitted. Component grammar distinguishes authority, IP
literals, user information, paths, queries and fragments; percent-encoded host
characters and IPvFuture literals are valid without network interpretation.
Generic ports remain digit strings without a network port-range restriction.
Only candidate schemas/init and candidate-consuming Pack structural checks opt
into this assertion. The structural engine validates the same raw Pack bytes and
bundled schema with a per-call URI format override; default Core and prototype
URI behavior and their cached schemas remain unchanged.

The private Runtime base64 material transport remains separate from portable raw
vector inputs. Its 24 MiB material JSON ceiling and MCP's 16 MiB request-line
ceiling remain unchanged. Repeated registrations of identical large bytes can
fit the candidate's distinct-byte budget while exceeding an encoded transport
ceiling. The portable harness therefore uses the same shared post-parse checks
and raw registration function, acquires sources lazily in declaration order and
stops on the first failure. Passing all 124 vectors promises no larger wire
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
current Policy work. The recorded clean-room review of Runtime commit
`558df435bbc50fc872b14aceecae1d482b45898d`, performed by OpenAI/Codex
`gpt-6.1-sol` at `xhigh`, found two P2 issues and no P1 issue. The maintainer
accepted both findings: candidate URI assertions and private-carrier registration
phase ordering. The [review and dispositions on PR 296](https://github.com/Judgment-Pack/judgment-pack-runtime/pull/296#issuecomment-6254960095)
identify that reviewed commit and the bounded review's actual evidence.

Commits `6dd003e29cccf7808227b98157e55cebce4b231b` and
`2e158c5105046594749139c955839a041331a106` implement those accepted findings,
including candidate-consuming Pack URI checks over unchanged bytes and schema.
The coordinator reports that the reviewer closed both findings at `2e158c5`.
Commit `38b06043283d2ae294653ca8f25c8c5db415dd9e` additionally preserves the
vendored candidate's exact bytes under Windows checkout conversion, addressing
the third P2 finding. The [final Runtime correction review](https://github.com/Judgment-Pack/judgment-pack-runtime/pull/296#issuecomment-6254971185)
closes all three findings through that commit. Focused
verification covers the 124 portable vectors, 62 standalone and 62 consuming
Pack URI probes, ordered private-wire refusals, schema/init parity and unchanged
Core/prototype URI behavior. Checkout verification with `core.autocrlf=true`
checks every vendored source digest/length and the independent provenance pin.

The URI source correction is pinned to the exact candidate commit above and is
recorded in [Spec PR 217's clean-room review](https://github.com/Judgment-Pack/judgment-pack-spec/pull/217#issuecomment-6254970472).
That exact `0852c5f` source passed review without P1/P2 findings, 13 focused
tests, 124 portable vectors and 99 additional URI probes, and merged as
`a74a440cfc31d0e135cdc8035f3474b17f27f320` after repository CI passed. Runtime
retains the immutable reviewed source pin; its merge also requires repository CI. This
accepted status records the maintainer-authorized merge-ready procedural
transition; it does not adopt a normative Policy format or change the RFC's
Review status. All implementation, review and agreement evidence described here
is same-vendor work under the temporary waiver, never different-vendor review
or independently authored interoperability evidence. The waiver changes no
standing repository requirement.

## More information

- [Prototype decision](0059-experimental-offline-policy-tools.md)
- [Offline Policy architecture](../architecture.md#experimental-offline-policy-prototype)
- [Candidate source and provenance](../../internal/policy/candidate/provenance.json)
