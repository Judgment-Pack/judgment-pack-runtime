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
its meaning, so a client branches on the code and not the status. Valid findings
carry the runtime's canonical disposition text, including sorted, duplicate-free
reason and trigger sets: duplicates and order are normalized rather than refused,
because §8.3 makes both sets values whose equality is set equality, so the
canonical text and not the submitted text is what this runtime compared. No case
is dropped. The report carries the versioned envelope every payload this runtime
writes carries, and `experimental: true`.

The shared `evaluation.DecodeDisposition` now checks raw member presence before
typed decoding can erase it: mandatory `kind`, `reasons`, `handoff`, and
`handoff.state` cannot be missing or null; `outcomeId` must be present exactly
for outcome and cannot be null; `triggeredBy` must be present exactly for
requested handoff. A presence rule is read only against a value its section
admits, so a misspelled or wrong-typed `kind` or `handoff.state` is reported
where the defect is rather than as a presence defect in the member it governs.
One §8.3 rule about the disposition itself joins the canonical validation: a
retained `exception-escalation` reason is a direct request (§8, §8.1), so it
requires a `requested` handoff whose `triggeredBy` names it, whatever the pack's
escalation object says. Existing canonical validation enforces types,
enumerations, non-empty values and the remaining reason/trigger relationships.
The gate also refuses text carrying more than one JSON value, so it holds for a
caller whose bytes did not come through a carrier. This closes decoder acceptance
holes; it does not change §8.3. Matrix, graph, coverage and corpus readers using
this decoder inherit the stricter representation checks.

## Consequences

- **Compatibility and migration:** the new tool is additive and experimental.
  Clients detect it before using it; old runtimes cannot provide this admission
  check. Four shapes that decoded before are refused now: `reasons` missing or
  null on an outcome, `outcomeId` as `""` or `null` on a non-outcome kind,
  `triggeredBy` as `[]` or `null` beside `"state": "none"`, and a retained
  `exception-escalation` reason without the requested handoff naming it. Missing
  or null `kind`, `handoff` and `handoff.state`, and a missing `outcomeId` on an
  outcome, were already refused. Every reader of a stored expectation inherits
  this — `packs test` and `experimental_test_packs`, the graph matrix's headline
  and `expectedNodes` rows, the coverage and profile derivations that witness
  probes from those rows, and `evaluate-corpus`. Such a row is reported as a
  mismatch naming the rule, and `packs validate` does not flag it, because a
  matrix loads without decoding its expectations. Correct the stored expectation
  to an explicit §8.3 value. Empty arrays are not wildcards.
- **Automation:** clients can reject contract-invalid proposals before evaluating
  or repairing a candidate. They must retain failed proposals and avoid silently
  claiming a reduced suite passed. The tool supplies no corrected expectation.
- **Authority:** this checks representation and disposition-local constraints only.
  A valid finding is necessary and not sufficient, in a stronger sense than "this
  pack may disagree": some legal §8.3 dispositions are reachable by no pack at
  all, because §8's step order rules them out rather than §8.3's grammar — an
  unresolved result retaining `not-applicable`, or `no-match` beside any other
  reason — and an admitted `outcomeId` may be a string no conforming pack could
  declare (§5's local-id grammar). Those are not checked here. The one
  pack-independent rule this gate does state is the direct request a retained
  `exception-escalation` reason makes, which §8.3 states about the disposition
  itself. Beyond that it does not establish that a pack can reach the
  expectation, that an outcome ID belongs to the pack, that handoff matches its
  configuration, or that a source or business policy supports it. Those
  disagreements remain for evaluation and human review. A separately coded
  reachability finding is the way to close the rest, and it belongs to the tool
  rather than to the decoder, which stays the §8.3 grammar gate its other readers
  need. The evaluator and specification formats do not change.
- **Security and privacy:** no project, filesystem, credentials, network, source,
  audit record, or model is accessed. Inputs are handled in memory under explicit
  bounds. Existing stdio request limits remain. There is no automatic repair,
  execution, new dependency, or persistence surface.
- **Validation:** native wire tests cover valid/invalid indexed results, exact
  member spelling and presence, kind/reason/handoff contradictions, canonical
  sets, duplicate members, malformed Unicode/JSON and resource bounds. Each
  fixture states the rule it is named for and the finding must name that rule,
  so a fixture that would also be refused for some other reason cannot stand in
  for it; each valid fixture states its canonical text byte for byte; each
  documented bound is stated as a literal on both sides. Unit tests hold the
  shared decoder itself, which the matrix, graph and coverage readers call
  directly. Desk's separate integration replay exercises blocked admission,
  approved correction and all rehearsals against this tool and evaluator.

Material impact is public-surface, documented-claim and conformance (stricter
expectation decoding). Cross-vendor review and maintainer dispositions are required
on the introducing PR before merge, under the repository's existing review regime.
