---
status: proposed
date: 2026-11-12
deciders: maintainer
---

# Let a packs test run require a coverage level and witnessed boundaries

## Context and problem statement

`jpack packs test` prints each pack's derived coverage ([ADR-0014](0014-matrix-coverage-report.md))
and its boundary probes ([ADR-0023](0023-boundary-probes-for-ordered-comparisons.md)), and neither
changes the exit status: a passing matrix with unwitnessed probes exits 0. A project that wants its
gate to hold a coverage level, or to hold every threshold to a row at the literal, has no way to say
so (issue #388).

ADR-0014 refused a gate, "outright or behind a flag", because a derived probe is reachable by
declaration and not by proof: a gate could demand a row no facts can produce. It named the revisit
condition as "when real projects ask for a gate", and ADR-0023 recorded that a boundary probe is
almost always constructible, which makes it the strongest candidate for that clause. This record
takes up that clause for the behind-a-flag form only.

Material-decision impact: public-surface.

## Decision drivers

- The default stays as it is, byte for byte and exit code for exit code. ADR-0014's reason holds for
  every run that does not ask.
- The choice belongs to the caller who owns the gate, stated where the gate is run, as with
  `--require-matrix` ([ADR-0042](0042-let-packs-test-require-a-matrix.md)).
- A requirement that cannot be measured is not met. A pack for which nothing was measured must not
  pass a gate that asked for a measurement.
- The payload says which requirement failed, for which pack.

## Considered options

- **A. Two per-run opt-ins on `packs test`:** `--min-coverage N`, a whole percentage of each pack's
  derived probes, and `--require-boundaries`, every derived boundary probe witnessed.
- **B. One option, a level only.** A level over all probes cannot say "every boundary", the one
  family ADR-0023 found almost always constructible, without also demanding every disposition probe,
  some of which ADR-0014 showed can be unconstructible.
- **C. A `jpack.json` member.** Refused for the reasons ADR-0042 refused its option B: it is sticky
  for every caller, needs a `configVersion` and a schema change, and a local author's loop would
  inherit the gate's strictness.

## Decision outcome

Chosen option: **A**.

1. **The declarations.** `--min-coverage N` with N a whole number from 0 to 100; any other value is
   refused with `JPS-INVOCATION-MIN-COVERAGE`, exit 3, before the project is read.
   `--require-boundaries` is a boolean. Omitting either is the default.
2. **The level.** It is read over the same count the report prints, `covered/probes` across both
   probe families: a pack meets N when `covered × 100 ≥ N × probes`. A pack for which no coverage
   was derived (the evaluator admits it for no row's capability set, or its declarations derive no
   probe) does not meet any level, 0 included.
3. **The boundaries.** A pack meets `--require-boundaries` when every `boundary:` probe it derives
   is covered. A pack that compares nothing in order derives none and meets it. A pack with
   boundaries for which no coverage was derived does not meet it.
4. **Scope.** Both apply to each selected pack whose matrix loaded and ran. A pack with no matrix is
   untouched and stays `skipped`; `--require-matrix` is what fails one. A pack whose matrix did not
   load is already `mismatch`.
5. **What a failure changes.** The pack's entry is `mismatch`, its new member `unmetRequirements`
   lists `min-coverage` and `require-boundaries`, in that order, for each requirement it misses,
   and its `detail` says why with the counts. The run is then `mismatch` and the CLI exits 1. Rows,
   their statuses, the summary counts and the coverage report are unchanged.
6. **The label.** The payload carries `minCoverage: N` and `requireBoundaries: true` exactly when
   the caller asked, so a stored report says which rule it was judged under; the human output adds
   one `required:` line per option asked for. All are absent otherwise: additive output under
   VERSIONING.md's MINOR rule, and `outputVersion` stays `"2"`.
7. **Surface.** The CLI only. `experimental_test_packs` and `experimental_test_cases` are not
   changed; their callers read the same coverage report and can hold it to a level themselves.

This supersedes, for a run that asks for one of these requirements only, ADR-0014's determination
"Coverage never moves a status or an exit code" and ADR-0023's refusal of its option C. Both records
stand as written for every other run.

### Consequences

- Good, because a project can make its gate mean "every threshold has a row at its literal", or "at
  least this share of what the declarations derive is stated", with one flag.
- Bad, because a level near 100 can demand a row no facts can produce, the case ADR-0014 named: two
  rules whose conditions exclude each other on purpose still derive a `conflict` probe. The caller
  who chose the level owns that; `--require-boundaries` does not share the risk, because a row can
  place any decimal at a pointer.
- Bad, because the gate's strictness is a property of how the command is invoked, which a reviewer of
  the CI configuration has to read (as with ADR-0042).
- Revisit if MCP callers ask for the same requirements, or if projects ask for them to live in
  `jpack.json`.

## More information

Issue #388. Implementation: `TestOptions.MinCoverage` and `TestOptions.RequireBoundaries` and
`holdToRequirements` in `internal/project/test.go`; `PackTest.MinCoverage`,
`PackTest.RequireBoundaries` and `PackTestEntry.UnmetRequirements` in `internal/result/result.go`;
the flags in `internal/cli/packs.go`. ADR-0014 (coverage), ADR-0023 (boundary probes), ADR-0042 (the
per-run opt-in pattern).
