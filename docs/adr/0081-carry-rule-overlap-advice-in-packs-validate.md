---
status: accepted
date: 2026-11-12
deciders: maintainer
---

# Carry the rule-overlap advice of spec validate in packs validate

## Context and problem statement

`jpack spec validate` reports the non-normative `JPS-RULES-OVERLAP` advice
([ADR-0066](0066-bounded-rule-overlap-advice.md)) beside a valid verdict for a Core `0.3.0` or
`0.5.0` pack in which two rules can both match with different outcomes. `jpack packs validate`, the
command a project gate runs, validates each pack through the same semantic layer and drops that
advice: its document check reports `passed` and nothing else (issue #389).

Material-decision impact: public-surface.

## Decision drivers

- One statement of the advice: `packs validate` carries what `spec validate` computes for the same
  bytes, and computes nothing of its own.
- The existing members of the `packs validate` payload, its statuses and its exit codes do not move
  for a run that does not ask: advice is not a check.
- The advice stays bounded as ADR-0066 bounds it.

## Considered options

- **A. An `advisories` member on each pack entry**, holding the diagnostics `spec validate` reports
  beside a valid verdict, with `advisoriesTruncated` beside it.
- **B. A seventh named check** reporting the advice as `passed` or `failed`, always. A check that
  fails moves the entry to `invalid`, which would change the exit code of every project whose packs
  draw advice.
- **C. Advice in the document check's detail text.** A sentence cannot carry the advice's witness
  facts and suggested rewrite, which are what make it actionable.

## Decision outcome

Chosen option: **A**.

1. **What is carried.** When the document check passed, the entry's `advisories` member holds the
   diagnostics the semantic layer reported beside that valid verdict, exactly as `spec validate`'s
   `diagnostics` holds them for the same bytes, and `advisoriesTruncated` is true when they stopped
   at the validation output limit. Both are absent when there is none, so a pack that draws no
   advice reads as it did.
2. **What it changes.** Nothing else: the six checks, the entry and run statuses, the summary and the
   exit code are the same with or without advice. The members are appended after `checks`;
   additive output under VERSIONING.md's MINOR rule, and `outputVersion` stays `"2"`.
3. **Human output.** Each piece of advice is printed under its pack as an `advice:` line, in the
   form `spec validate` prints it.
4. **Failing on advice, by request.** `--fail-on-advisory` is option B behind a per-run opt-in, the
   pattern of [ADR-0042](0042-let-packs-test-require-a-matrix.md). With it, every selected pack's
   checks end with a seventh check, `advisories`: `failed` when the pack draws any advice (the
   entry is then `invalid` and the run exits 1), `passed` when it draws none, and `skipped` when no
   advice was computed: the document check did not pass, or the pack declares a Core version before
   `0.3.0`, for which ADR-0066 defines no advice. **Any advice fails, of either certainty.** This
   differs from `spec test-conformance --strict` (ADR-0066), which fails an `exact` overlap and
   keeps `not-excluded` a warning. Issue #389 asks for any advisory to fail; a gate that chose the
   option is told about every overlap the analysis could not rule out. The payload
   carries `failOnAdvisory: true` exactly then. Without the option there is no seventh check and no
   member, so the default is as item 2 states. The option is on the CLI only, as ADR-0080's are.
5. **Bounds.** Each pack's advice is bounded by ADR-0066's limits; a run's advice is therefore
   bounded by those limits times the number of selected packs, as its checks are.

### Consequences

- Good, because the gate a project runs shows the same advice the author's own validation shows.
- Bad, because a project with many overlapping packs gets a longer report; `--id` selects one pack.
- Bad, because advice is non-normative and can name an overlap the author intends; a gate run with
  `--fail-on-advisory` then fails until the rules are rewritten. The caller who chose the option owns
  that.
- Bad, because a `not-excluded` advisory fails the run too, though it says only that the analysis
  could not rule an overlap out within its budget, not that one exists, and it carries no rewrite.
  For rules meant to be disjoint, the author rewrites the conditions so the exclusion is exact (for
  example, one shared comparison with complementary bounds), or gates that pack in a separate
  `--id` run without the option.
- Revisit if the semantic layer reports advice of another kind: it would appear here unchanged, and
  this record would need to say whether that is wanted.

## More information

Issue #389. Implementation: `validatePack` in `internal/project/validate.go`,
`PackValidationEntry.Advisories` in `internal/result/result.go`, the human rendering in
`internal/cli/render.go`; the option is `ValidateOptions.FailOnAdvisory` and `holdToAdvice` in
`internal/project/validate.go`. ADR-0066 (the advice), ADR-0012 (the project convention).
