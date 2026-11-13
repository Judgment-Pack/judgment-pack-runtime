---
status: proposed
date: 2026-11-13
deciders: maintainer
---

# Analyse a Core 0.5.0 pack as its evaluation form

## Context and problem statement

`jpack experimental analyze-pack` and the MCP `experimental_analyze_pack` tool run the Draft RFC
0027 finite-domain analyzer ([ADR-0068](0068-finite-domain-pack-design-advice.md)). Since
[ADR-0077](0077-admit-core-0-5-0-and-retire-the-staged-selector.md) they refuse a pack declaring
`specVersion` `0.5.0` with `pack-unsupported`, although validation and evaluation read that
version; the same pack declared `0.3.0` is analysed (issue #401). ADR-0077 refused it because the
proposal is written against Core `0.3.0`.

Material-decision impact: public-surface.

## Decision drivers

- The analysis is a rehearsal of evaluation. Whatever evaluation reads a pack as, the analysis
  reads it as, through the same code; there is no second lowering to drift from the evaluator's.
- The report keeps Draft RFC 0027's format 1. Its schema is closed (`additionalProperties: false`,
  `formatVersion` 1), so a new member is a change to the proposal, not to this runtime.
- A Core `0.3.0` report does not change by a byte.
- A Core version nobody has read against the analyzer is still refused.

## What the specification says

- Core §8.5: a `0.5.0` pack `P` is evaluated as its evaluation form `EF(P)` — each value-set
  reference replaced by its set's values as an inline `value` array, `valueSets` removed, and the
  annotations removed from the root, the outcomes and the rules. `EF(P)` "is itself a semantically
  conforming pack of the same specVersion and profile"; "Annotations are inert"; and
  "Substitution. A reference behaves exactly as its inline array."
- Core §11, "Migrating down": "`EF(P)` with the selector changed is evaluation-equivalent within
  §10's limit rule, but loses the annotations and the entry descriptions." `EF(P)` is therefore
  written in Core `0.3.0` constructs only, and §4.1 sets no maximum on an inline `in` operand, so
  the expanded arrays are not over a `0.3.0` limit.
- Draft RFC 0027: "Every report names the exact supported Core version used to validate and
  evaluate the pack … `targetSpecVersion` must equal the pack's complete `specVersion` and be
  supported by the analyzer. No compatibility is inferred from version prefixes." "Initial
  examples analyze Core `0.3.0`" — an example, not a limit. `packDigest` binds the exact bytes.
  "An unsupported Core version, pack profile, required capability or inadmissible pack is likewise
  a `pack-unsupported` refusal before analysis."
- Core §3.3.1 adds the value-set advisories (`JPS-VALUE-SET-DUPLICATE-VALUE`,
  `JPS-VALUE-SET-UNREFERENCED`) to Core text "when RFC 0027 is dispositioned"; they are not in
  format 1's six families.

## Considered options

- **A. Analyse `EF(P)`, taken from the evaluator's own admission**, and name the pack's own
  selector in `targetSpecVersion`.
- **B. Lower the pack in the analyzer** with `evalform`, beside the evaluator's lowering. Two
  lowerings of one pack can drift, and the analysis would no longer be a rehearsal of what is
  evaluated.
- **C. Keep the refusal, with a specific code for what `0.5.0` adds.** Nothing a valid base `0.5.0`
  pack holds is outside what the analyzer reads once the pack is in its evaluation form, so there is
  nothing within the version to refuse.
- **D. Add a member that states the form analysed.** Format 1's closed schema refuses it.

## Decision outcome

Chosen option: **A**.

1. **Selection.** The analyzer selects the evaluator contract from the pack's exact `specVersion`
   and reads `0.3.0` and `0.5.0`, base profile, by name. Any other version, the `extended` profile,
   a required extension outside the supported set and every pack evaluation refuses are
   `pack-unsupported`, before analysis, as before.
2. **One form.** The analyzer reads the rules, exceptions, evidence requirements, applicability
   and fallback from `AdmittedPack.EvaluationForm`, the root the evaluator's admission made once
   and every evaluation of the analysis walks. For `0.3.0` that root is the decoded document, as
   before. The analyzer no longer decodes the pack's bytes itself.
3. **What value sets and annotations mean to the analysis.** A value-set reference means its inline
   array: a finding about a condition that names a set is the finding its inline twin gets. An
   annotation, a set's description and an entry's description mean nothing: changing, adding or
   removing one changes the pack's bytes and so `packDigest`, and nothing else in the report. A set
   no condition references takes no part. The format 1 families are unchanged; the value-set
   advisories stay with the proposal's disposition.
4. **Which form the report describes.** `targetSpecVersion` is the pack's selector, `0.5.0`, as
   Draft RFC 0027 requires, and under Core `0.5.0` evaluation is of `EF(P)` (§8.5), so the
   selector names the form. `packDigest` is over the exact bytes of `P`. Every source pointer the
   analyzer emits (`/rules/i`, `/exceptions/i`, `/evidenceRequirements/i`, `/applicability`,
   `/fallbackOutcome`, `/rules/i/when` and the root) names the same entity in `P` and in `EF(P)`,
   which removes members but moves no entity; no pointer names a value set.
5. **Refusals.** The code stays `pack-unsupported`, as the proposal fixes. For a `0.5.0` pack the
   message also names the Core §8.4 class, evaluation's code and, for a pack that is not
   conformant, the code of Core's first diagnostic, for example
   `pack-not-conformant (JPS-EVALUATION-PACK-NOT-CONFORMANT), first diagnostic
   JPS-SEMANTIC-UNRESOLVED-VALUE-SET`. All three are this runtime's fixed identifiers, never pack
   text. A `0.3.0` pack's refusal keeps the message it had, so its bytes do not change.

6. **Graphs and the policy check keep their refusal.** Neither change is as small or as plainly
   allowed as the analysis. Draft RFC 0002 names no Core version for a node pack and no
   specification issue proposes reading a `0.5.0` one, so reading one is this runtime's own
   decision across graph validate, explain, test and evaluate, the mixed-contract rule and the
   graph reports. Policy companion 1.0.0 §4 consumes a Core `0.2.0-draft` pack, and whether a
   value set is a coverage target is undecided; the next companion format is Spec issue #244. The
   code (`JPS-SURFACE-SPEC-VERSION-UNSUPPORTED`) and the exit stay. The message gains one sentence
   naming what reads such a pack meanwhile: for graphs, `experimental evaluate` and `packs test`,
   which read a `0.5.0` pack on its own as its evaluation form; for the policy check,
   `experimental policy validate`, which checks the adoption without reading its consumer, and
   `spec validate`, which checks the pack. The sentence is the graph's and the policy check's own;
   the shared message helper, and so the dialect check's message, is unchanged. CONFORMANCE.md
   states, dated, what each waits for.

This supersedes ADR-0077's refusal of a `0.5.0` pack for pack-design analysis only. The graph
commands' and the policy check's refusals stand, with the added sentence.

### Consequences

- Good, because a pack moved to `0.5.0` keeps its pack-design advice, and a set or an annotation
  cannot make the advice disagree with evaluation.
- Good, because the analyzer has one reading of a pack fewer: no separate decode of the bytes.
- Bad, because nothing in a format 1 report but `targetSpecVersion` says that references were
  expanded; a reader has to know §8.5.
- Bad, because a refused `0.5.0` pack's message is longer than a refused `0.3.0` pack's.
- Revisit when Draft RFC 0027 is dispositioned (Spec issue #252): a format with a member naming
  the analysed form, or the value-set advisories, would change points 3 and 4. Revisit also when
  a later Core version is admitted by evaluation: it is refused here until it is read against the
  analyzer.

## More information

Issue #401. Implementation: `Analyze` and `refusePack` in `internal/packadvice/analyze.go`;
`AdmittedPack.EvaluationForm` in `internal/evaluation/document.go`; `core050Refusal` in
`internal/graph/contract.go` and `core050ConsumerMeanwhile` in `internal/policy/check.go`. Tests:
`internal/packadvice/core050_test.go` over the hand-written fixtures in
`internal/packadvice/testdata/core050`, `TestEvaluationForm*` in `internal/evaluation`, the CLI
golden `internal/cli/testdata/analyze_pack_core030.golden.json` and the MCP parity test. The
inventory guard in `internal/cli/conformance_enumeration_test.go` holds the gate and the single
read of the evaluation form. ADR-0068 (the analyzer), ADR-0077 (Core `0.5.0`), ADR-0082 (the
dialect check's reading of `0.5.0`).
