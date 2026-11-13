---
status: proposed
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
- **B. A seventh named check** reporting the advice as `passed` or `failed`. A check that fails moves
  the entry to `invalid`, which would change the exit code of every project whose packs draw advice.
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
4. **Bounds.** Each pack's advice is bounded by ADR-0066's limits; a run's advice is therefore
   bounded by those limits times the number of selected packs, as its checks are.

### Consequences

- Good, because the gate a project runs shows the same advice the author's own validation shows.
- Bad, because a project with many overlapping packs gets a longer report; `--id` selects one pack.
- Revisit if the semantic layer reports advice of another kind: it would appear here unchanged, and
  this record would need to say whether that is wanted.

## More information

Issue #389. Implementation: `validatePack` in `internal/project/validate.go`,
`PackValidationEntry.Advisories` in `internal/result/result.go`, the human rendering in
`internal/cli/render.go`. ADR-0066 (the advice), ADR-0012 (the project convention).
