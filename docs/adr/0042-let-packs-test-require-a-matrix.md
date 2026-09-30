---
status: accepted
date: 2026-09-30
deciders: maintainer
---

# Let a packs test run require every selected pack to have a matrix

## Context and problem statement

`jpack packs test` reports a declared pack with no matrix as `skipped`, never `passed`, and a run in
which no row ran at all as `skipped` with exit 1. A run in which some pack's rows ran and passed,
beside a pack with no matrix, exits 0. A CI step that gates on `packs test` therefore stays green
while a pack the deciding surfaces will evaluate has no test at all, and nothing in the command or
in `jpack.json` changes that (issue #179).

[ADR-0014](0014-matrix-coverage-report.md) keeps coverage informative and never gating, because a
coverage probe is derived from a pack's content and a gate on it "would demand a row no facts can
produce". A pack with no matrix is not a derived demand. It is a declared pack that nothing checks.

## Decision drivers

- The default stays as it is. A project may declare a pack before its rows exist, and a run that
  newly failed for that would break every such project's gate on upgrade.
- The choice belongs to the caller who owns the gate, stated where the gate is run.
- ADR-0014's refusal stands: nothing derived from a pack's content gates.

## Considered options

- **A. A per-run opt-in:** `--require-matrix` on `packs test`, and `require_matrix` on
  `experimental_test_packs`.
- **B. A `jpack.json` member** that makes every run require matrices. It is sticky for everyone
  running the project, needs a new `configVersion` and schema change, and a local author's loop would
  inherit the CI gate's strictness.
- **C. Change the default.** This breaks existing gates.

## Decision outcome

Chosen option: **A**.

1. **The declaration.** CLI `--require-matrix` on `packs test`. MCP `require_matrix` on
   `experimental_test_packs`: a JSON boolean, with null and every other type refused, as ADR-0028's
   `rehearsal` is. Omitting the key, or passing `false`, is the default.
2. **What it changes.** A selected pack whose entry declares no matrix is reported `mismatch`, with
   a detail saying it declares no matrix and this run requires one, instead of `skipped`. The run's
   status is then `mismatch`, and the CLI exits 1. Nothing else changes: packs with matrices run and
   are judged exactly as before, and the coverage report still moves no status.
3. **The label.** The payload carries `requireMatrix: true` exactly when the caller asked, so a
   stored report says which rule it was judged under. It is absent otherwise: additive output under
   VERSIONING.md's MINOR rule, and `outputVersion` stays `"2"`.
4. **Scope.** The selection is the scope. `--id` selects one pack, and only that pack must have a
   matrix.

### Consequences

- Good, because a project can make its CI gate mean "every declared pack is tested" with one flag,
  while an author's local loop stays as forgiving as it was.
- Bad, because the gate's strictness is now a property of how the command is invoked, which a
  reviewer of the CI configuration has to read.
- Revisit if projects ask for the rule to live in `jpack.json` (option B), for example so that every
  caller of `experimental_test_packs` inherits it.

## More information

Issue #179. ADR-0014 (coverage never gates), ADR-0021 (the project matrix), ADR-0028 (the strict
boolean argument shape).
