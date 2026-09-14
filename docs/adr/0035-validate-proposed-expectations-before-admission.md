---
status: proposed
date: 2026-09-14
deciders: maintainer
---

# Validate proposed exact expectations before authoring admission

## Context and problem statement

Desk PR #76 reached 15 of 16 live smoke cases, then stalled on an expectation that
asked for `kind: "unresolved"` with `reasons: []`. Core 0.2.0-draft §8.3 cannot
produce that combination. Changing the candidate cannot fix the expectation.
The authoring client needs to distinguish a malformed expectation from a valid
expectation that disagrees with the candidate, before starting its repair loop.
The runtime already owns the strict disposition decoder used by matrix comparison.

## Decision drivers

- Keep the specification and exact comparison unchanged; no implicit partial assertions.
- Reuse one contract gate, rather than copying §8.3 into an authoring client.
- Account for every proposal, including invalid ones, without evaluating a pack.
- Keep correction, human approval and source interpretation in the client.
- Bound untrusted input and expose no new filesystem or network authority.

## Considered options

- An additive, read-only MCP validation tool using the existing decoder.
- Duplicate the decoder in Desk, with an independently maintained contract.
- Generate a temporary project matrix and run it merely to check its assertions.
- Change the disposition format or treat omitted/empty fields as wildcards.

## Decision outcome

Add `experimental_validate_expectations`, independently discoverable in
`tools/list`. It accepts exactly `spec_version: "0.2.0-draft"` and an
`expectations` array of 1–256 JSON strings, each a complete expected disposition.
No pack, project, or facts are accepted. Unsupported versions or malformed call
arguments fail the whole tool call. Each well-formed string argument receives one
indexed finding, including strings containing malformed JSON or an invalid
§8.3 value. The aggregate is `valid` only when every finding is valid.

Each input is limited to 16 KiB UTF-8, depth 16, 1,024 JSON value nodes and 8 KiB
per string. Carrier failure or disposition failure produces
`JPS-EXPECTATION-INVALID`; resource limits produce `JPS-EXPECTATION-LIMIT`.
A limit finding means the tool did not admit the input, not that Core prohibits
its meaning. Valid findings carry the runtime's canonical disposition text,
including sorted, duplicate-free reason and trigger sets. No case is dropped.

The shared `evaluation.DecodeDisposition` now checks raw member presence before
typed decoding can erase it: mandatory `kind`, `reasons`, `handoff`, and
`handoff.state` cannot be missing or null; `outcomeId` must be present exactly
for outcome and cannot be null; `triggeredBy` must be present exactly for
requested handoff. Existing canonical validation enforces types, enumerations,
non-empty values and reason/trigger relationships. This closes decoder acceptance
holes; it does not change §8.3. Matrix and corpus readers using this decoder
inherit the stricter representation checks.

## Consequences

- **Compatibility and migration:** the new tool is additive and experimental.
  Clients detect it before using it; old runtimes cannot provide this admission
  check. Previously tolerated missing/null mandatory members or present-but-empty
  forbidden members now fail existing matrix/corpus decoding. Correct the stored
  expectation to an explicit §8.3 value. Empty arrays are not wildcards.
- **Automation:** clients can reject contract-invalid proposals before evaluating
  or repairing a candidate. They must retain failed proposals and avoid silently
  claiming a reduced suite passed. The tool supplies no corrected expectation.
- **Authority:** this checks representation and disposition-local constraints only.
  It does not establish that a pack can reach the expectation, that an outcome ID
  belongs to the pack, that handoff matches its configuration, or that a source
  or business policy supports it. Those disagreements remain for evaluation and
  human review. The evaluator and specification formats do not change.
- **Security and privacy:** no project, filesystem, credentials, network, source,
  audit record, or model is accessed. Inputs are handled in memory under explicit
  bounds. Existing stdio request limits remain. There is no automatic repair,
  execution, new dependency, or persistence surface.
- **Validation:** native wire tests cover valid/invalid indexed results, exact
  member spelling and presence, kind/reason/handoff contradictions, canonical
  sets, duplicate members, malformed Unicode/JSON and resource bounds. Desk's
  separate integration replay exercises blocked admission, approved correction
  and all rehearsals against this tool and evaluator.

Material impact is public-surface, documented-claim and conformance (stricter
expectation decoding). Cross-vendor review and maintainer dispositions are required
on the introducing PR before merge, under the repository's existing review regime.
