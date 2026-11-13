---
status: proposed
date: 2026-11-13
deciders: maintainer
---

# Measure a matrix's test strength with variants of its pack

## Context and problem statement

`jpack packs test` says whether each row of a pack's matrix matched its expectation, and the coverage
report ([ADR-0014](0014-matrix-coverage-report.md), [ADR-0023](0023-boundary-probes-for-ordered-comparisons.md))
says which probe classes the declarations derive and which some row states. Neither says whether
the rows would *notice* the pack being slightly wrong. A matrix can pass and witness every probe
while no row tells `greater-than-or-equal "5000"` from `greater-than-or-equal "5001"`, because no
row places the amount at 5000 (issue #392).

The question is a mutation-testing one: derive small changes of the pack, run the matrix against
each, and report the changes no row notices.

Material-decision impact: public-surface.

## Decision drivers

- Nothing changes for a run that does not ask: the same bytes and the same exit code.
- The conformance claim and its eleven-surface enumeration in CONFORMANCE.md stay as they are.
- A variant is a pack: it is judged by the same admission and the same row comparison as the pack
  itself, never by a looser path.
- Output is deterministic and the ids are readable and stable.
- Work has a stated ceiling, and a run cut short is never reported as a complete one.
- Nothing is written beside the pack.

## Considered options

- **A. An option on `packs test`:** `--strength`, with `--fail-on-survivor` and `--max-variants`.
- **B. A new subcommand, `jpack packs test-strength`.** The issue's suggested shape. Its command
  would construct the evaluator in a new file, which is a twelfth surface reaching it:
  `internal/cli/conformance_enumeration_test.go` would fail until CONFORMANCE.md's claim scope
  named it, and changing the claim is out of scope here.
- **C. B, reusing the `packs test` constructor.** It keeps the count at eleven by hiding a surface
  from the check whose job is to find them, which defeats it.

## Decision outcome

Chosen option: **A**. It is the narrowest form: the evaluator is reached through `packs test`,
already in the enumeration, and every byte and exit code of a run without `--strength` is unchanged.

1. **The kinds.** Each variant is one change to the authored pack document:
   - `bound-moved`: an ordered comparison's decimal literal moved one unit down and one unit up, at
     the finest precision the pack authors that value in at that pointer (the step `packs suggest`
     takes, [ADR-0024](0024-suggest-candidate-row-inputs.md), at most 10^-6);
   - `operator-reversed`: an ordered operator to its strict or non-strict twin, which differs only
     at the literal, and to its complement; `equals` and `not-equals` to each other; `in` derives
     none, because its complement is not an operator the grammar has;
   - `condition-dropped`: one member removed from an `all` or an `any`;
   - `outcome-swapped`: a rule's outcome, a `force-outcome` exception's outcome and the
     `fallbackOutcome`, each to every other declared outcome;
   - `on-unknown-changed`: a rule's or exception's `onUnknown` to the other value;
   - `value-set-entry-removed`: on Core `0.5.0` only, one entry removed from a value set some
     condition references.
   Conditions are reached through applicability, each rule's and exception's `when`, and only the
   `all`, `any` and `not` doors the coverage walk opens.
2. **Equivalence.** A change the evaluator provably cannot see is not derived: removing a member of
   an `all` or `any` that another member of the same array repeats byte for byte (in sorted-member
   JSON), and removing a value-set entry whose value another entry of the same set repeats. No other
   equivalence is detected, and the documentation says so.
3. **Validity.** Each variant is built in memory as a path copy of the decoded document, encoded,
   and admitted by the same `AdmitPack` and capability-set predicate coverage uses. A change that
   leaves no admitted pack under the same `specVersion` (dropping the only member of an `all`,
   removing the only entry of a set) is `not-derivable`, with the refusal's code and first
   diagnostic, and is never a survivor.
4. **Noticed and survived.** The rows run in matrix order through the same `judgeRow` the pack's own
   rows run through; the first row whose result differs from its expectation notices the variant
   and is named in `noticedBy`. A variant every row still passes on survived. A survivor is a gap in
   the matrix or an equivalent variant; the command cannot tell which, and the result proves nothing
   about the pack.
5. **When variants run.** Only for a pack whose matrix ran, has at least one row, every row matched,
   and which the evaluator admits for some row's capability set. Otherwise the pack's `strength` is
   `skipped` with a detail; a mismatching row would make every variant read as noticed.
6. **Ids.** `<declaration>/<pointer>:<kind>[:<new value>]`, from where the change is and what it is,
   never from a position in the variant list: `rules/expense-threshold/when:bound-moved:5001`,
   `rules/r/when/conditions/1:condition-dropped`, `fallbackOutcome:outcome-swapped:review`,
   `valueSets/tiers/entries:value-set-entry-removed:"gold"`. Authored text in an id goes through the
   boundary probe's 128-byte rendering. Variants are sorted by id; two variants sharing an id is an
   internal failure.
7. **Ceiling.** `--max-variants N` (default 1000, at least 1) bounds the changes one pack may derive,
   admitted or not. A pack that derives more refuses the whole run with
   `JPS-RESOURCE-STRENGTH-VARIANTS`, exit 3, naming the ceiling, before any variant runs; nothing is
   truncated. A value below 1 is refused with `JPS-INVOCATION-MAX-VARIANTS`, exit 3. The worst case
   is one admission per change and N × rows row evaluations per pack, one variant at a time, with no
   parallelism. The change count is linear in conditions, value-set entries and declarations except
   for outcome swaps, which are declarations × outcomes, so the ceiling is required.
8. **The gate.** By default the run informs and moves no status. `--fail-on-survivor` makes a pack
   whose matrix ran fail when a variant survives or when none could run (a requirement that cannot
   be measured is not met, as in [ADR-0080](0080-let-packs-test-require-coverage.md)), with
   `fail-on-survivor` in `unmetRequirements` after the coverage requirements, and the CLI exits 1. A
   pack with no matrix is untouched; `--require-matrix` carries over unchanged and is what fails one.
   `--fail-on-survivor` and `--max-variants` without `--strength` are refused with
   `JPS-INVOCATION-STRENGTH-OPTION`, exit 3.
9. **Output.** A `strength` member per pack entry (`status`, `counts`, every variant with `id`,
   `kind`, `declaration`, `change` in the pack's own words, `status`, `noticedBy`, and a `hint` for a
   survivor) and one for the run (`label`, `maxVariants`, `failOnSurvivor`, summed `summary`),
   present only under `--strength`: additive, and `outputVersion` stays `"2"`. The text output prints
   the counts and each survivor with its hint (for a moved bound, the value at which the two bounds
   disagree and, when that is the literal, its `boundary:` probe). No row fact beyond what `packs
   test` already prints appears.
10. **Scope.** Core `0.2.0-draft`, `0.3.0` base and `0.5.0` base, each under its own contract. The
   CLI only: no MCP tool, no change to the pack format, the matrix format or the claim.

### Consequences

- Good, because a passing matrix can be asked what it would miss, in the pack's own terms, with the
  row that would catch each survivor described.
- Good, because a variant is judged by exactly the code that judges the pack, so "noticed" means
  `packs test` would have reported that row as a mismatch.
- Bad, because a survivor can be an equivalent variant (moving a bound on a fact that only ever
  holds whole thousands), which only the policy owner can rule out; the label says so on every run.
- Bad, because a large pack under a large matrix is slow: the worst case is stated, and the
  ceiling refuses rather than running a part.
- Revisit if MCP callers ask for the same run, or if a second constructor site for it is wanted, which
  would change the claim's enumeration.

## More information

Issue #392. Implementation: `internal/project/strength.go` (derivation, the run, `holdToSurvivors`),
`judgeRow` and the hook in `internal/project/test.go`, the result types in
`internal/result/result.go`, the flags in `internal/cli/packs.go` and the text in
`internal/cli/render_strength.go`. ADR-0014 and ADR-0023 (coverage and boundaries), ADR-0024 (the
step), ADR-0042 and ADR-0080 (per-run opt-ins and gates).
