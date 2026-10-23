---
status: proposed
date: 2026-10-23
deciders:
---

# Report bounded rule-overlap advice without changing document conformance

## Context and problem statement

Core 0.3.0 §3.3.1 recommends a non-normative check for rules naming different
outcomes whose conditions can both be true. Authors need the pair, an exact
region or witness when decidable, and a replacement condition they can paste.
The optional manifest assertions are separate from normative conformance.

## Decision drivers

- Keep validation and evaluation admission, precedence and dispositions unchanged.
- Preserve three-valued conditions and the distinction between decimal ordering
  and type-preserving JSON equality, including lexical string equality.
- Bound Boolean expansion, analysis work and diagnostic output explicitly.
- Consume immutable advisory expectations without changing embedded artifacts.

## Considered options

- Treat overlap as semantic nonconformance immediately.
- Inspect only adjacent numeric thresholds.
- Add a bounded shared advisory pass and a separate strict authoring gate.

## Decision outcome

Choose the shared advisory pass, after successful semantic and capability checks
for explicit Core 0.3.0. Earlier contracts retain their behavior. The pass compares
pairs with different outcomes; same-outcome pairs are silent. No facts document
is read and no evaluation code changes. Carrier and structural-only validation
remain partial checks and do not run this advice.

Conditions are reduced to clauses for their true or false set: `all` intersects
true sets and unites false sets, `any` does the inverse, and `not` swaps them.
A fact clause partitions its path's possible values into authored equality
values, the remaining nondecimal JSON values, and decimal points/open intervals.
Each decimal point also has a spelling outside the authored equality strings.
Ordered comparisons accept only decimal strings; other types and missing facts
remain unknown, including under negation. Equality recursively preserves JSON
type and string spelling and compares JSON number tokens by mathematical value
without expanding arbitrary exponents. Boolean true clauses never turn a missing
fact into false. Literal conditions include the full/empty sets.

On one path this finite partition decides whether the true sets intersect.
Ascending numeric bands carry the exact lower endpoint; simple finite comparisons
carry shared values. Other exact forms carry one witness facts input rather than
an incomplete interval for a union or complement. The evaluator independently
checks the hand-authored exact witnesses. Across paths, independent per-path
constraints are a relaxation, including ancestor pointers: an unsatisfiable
relaxation proves disjointness, including relating negation; a satisfiable one
is only `not-excluded`. Evidence conditions are opaque Boolean constraints and
never establish exact overlap.

The pass permits at most 256 clauses per condition and one million work units
per pack, charging nodes, copied terms, candidates, generated decimal spelling bytes and
serialized operand bytes. Generated replacements and facts use the same configured
carrier limits as validation. Before marshaling a replacement, a charged shallow
replacement view checks the complete rewritten pack's depth, value-node count,
string/number limits and exact serialized byte count, then the actual carrier
reader verifies its encoded bytes. The original loaded tree is never modified.
A replacement that cannot pass this preflight is omitted and `rewriteUnavailable`
gives an explicit bounded-unavailability reason; it is never advertised as pasteable.

Witness generation charges pointer bytes before scanning or tokenizing and
checks pointer-derived depth and node counts before allocating token storage.
Its leaf and complete generated shape receive the same node/depth/string/byte
preflight. It then encodes the bounded pointer structure directly, without nested
map construction, and verifies the actual facts bytes with the carrier reader.
An inadmissible witness is omitted. Exact certainty remains only if another
supported exact region is available; otherwise the result becomes `not-excluded`.
Boundary regressions cover a valid pack with 124 nested `not` conditions, a
129-token fact path, the maximum admitted pointer string, custom limits, and
allocation-free rejection of excessive pointer depth before tokenization.

A failed budget or unsupported exact form becomes `not-excluded`, with no exact
region or witness. The existing 100-diagnostic cap applies; a 1 MiB serialized
advisory budget retains whole diagnostics only and sets `diagnosticsTruncated`
when it cannot retain another. Suite diagnostics additionally share a 4 MiB
serialized budget and the existing 1,000-diagnostic cap. Matching and strict
gating occur before suite display truncation. A truncated validation result
cannot satisfy an optional advisory assertion. These are implementation bounds,
not new Core requirements; a budget-limited result does not prove disjointness.

Each `JPS-RULES-OVERLAP` diagnostic has `certainty`, both rule identities,
`effect`, and either `suggestedRewrite: {ruleId, when}` or `rewriteUnavailable`,
beside the existing diagnostic fields. Its pointer is `/rules/<first-index>/when`. An ascending pair rewrites
the lower numeric threshold using the existing two-comparison form. Other pairs
rewrite the first rule as its original condition conjoined with `not` of the
second condition. A missing or incomparable higher-rule fact leaves that rewrite
unknown; `onUnknown: escalate` therefore does not silently select the lower band.
No publication interface or new operator is introduced.

Plain CLI/MCP validation reports warnings and remains `valid`. The conformance
runner keeps normative `actualStatus` separate: strict mode promotes exact advice
to `error` and fails the suite row even when its optional expectation correctly
predicts the advice. `not-excluded` stays a warning and does not fail strict mode.
Normative expected-diagnostic and single-defect comparisons exclude advice.
Optional assertions are checked explicitly with a per-row `advisoryStatus` and
separate `advisories` totals, including empty expectations and advisory-only
suites. The aggregate suite summary counts all checked manifest rows; it is not
a count of normative requirements.

Frozen advisory expectations use `path: /rules/<index>`. A local comparison adapter
maps that rule-object pointer to the Runtime's `instancePath: /rules/<index>/when`
and selects `severity.validate` or `severity.strict`. All other expected members
must match, including regions and rewrites; extra Runtime envelope fields are
allowed. Normative pointer comparison remains literal. Manifest reading also
checks that the two advisory rule ids differ, beyond schema `uniqueItems`.

### Consequences

- Authors get the same advice in CLI and MCP, with pasteable exclusive conditions
  when a complete replacement fits the applicable limits, and an explicit reason
  when no bounded replacement is available.
- Strict authoring can fail while the conforming document remains valid; this
  deliberately does not promote the recommendation into semantic conformance.
- Budget limits and cross-path uncertainty prevent complete overlap proofs for
  every admitted pack; output reports uncertainty or truncation explicitly.
- Core 0.3.0's unchanged 151 normative rows pass in both modes. Its five optional
  assertions match in both modes; the exact-overlap row makes strict mode fail.
- Revisit bounds or the severity gate when the specification changes this
  recommendation or evidence shows a useful case exceeds these limits.

## More information

- [Core 0.3.0 implementation posture](../../CONFORMANCE.md)
- [Runtime issue 304](https://github.com/Judgment-Pack/judgment-pack-runtime/issues/304)
- Supersedes ADR-0063's determination to skip optional rule-overlap assertions;
  its artifact pin, normative contracts and evaluation behavior remain in force.
