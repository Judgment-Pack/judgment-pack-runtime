---
status: accepted
date: 2026-08-05
deciders: maintainer
---

# Run the declared matrix over MCP, and keep the rehearsal outside the record

## Context and problem statement

The runtime serves the matrix authoring method over MCP and runs matrices only
on the CLI. The `test_pack` prompt walks a client's model through building the
instance matrix row by row and closes on the discipline — re-run the whole
matrix after any change — while no MCP tool runs one: `jpack packs test` is the
only runner, and for a client whose model reaches this runtime only over MCP,
the closing step of the loop happens somewhere the author is not (issue [#74]).
The substitute a client can build — replaying rows through
`experimental_evaluate` and comparing client-side — re-implements a byte
comparison this repository ships and drops the derived coverage report
entirely, re-creating ADR-0014's problem statement one layer up: what the
matrix failed to probe is silent.

## Decision drivers

- The method and the means belong on one surface (ADR-0003, ADR-0008): a
  prompt that teaches a discipline its own server cannot execute is a gap, not
  a design.
- One evaluator, one comparison: a row must be judged by the same code
  everywhere, or two surfaces can disagree about one matrix.
- Coverage must reach the agent (ADR-0014), or its absence is invisible
  exactly where the matrix is authored.
- Posture unchanged: no credential, no connection, no path over the wire, no
  write.
- A rehearsal is not a decision (ADR-0018, ADR-0019): a matrix row is a check
  on a pack, not a decision anyone took.
- The claim-scope enumeration is held mechanically, so a new surface reaching
  the evaluator is a documented change, not a drift.

## Considered options

- **A. One tool, `experimental_test_packs`**, optional `pack_id`, payload
  `result.PackTest` verbatim.
- **B. The same tool unprefixed** (`test_packs`), on `packs test`'s own
  precedent — the CLI twin is not in the `experimental` command group.
- **C. Two tools now**, adding `experimental_test_graphs` beside it.
- **D. No tool**: serve the matrix rows through `list_packs`/`get_pack` and
  let the client run them.
- **E. Do nothing**: document the CLI as the only runner.

## Decision outcome

Chosen option: "A", because serving `result.PackTest` unchanged is what makes
the wire and the shell one answer, and because ADR-0007's labeling constraint
names the tool name among the artifacts that carry the experimental marker —
MCP has no command group to carry it instead, and `experimental_evaluate` is
the sibling a client's model reads the parallel from. B trades that marker for
a consistency with the CLI that the CLI does not need (its marker lives in its
help text and payload). C doubles the reviewed surface for a surface that is
experimental atop an experimental one. D hands every client the comparison to
re-implement, which is the defect being fixed. E leaves the taught loop
unclosable where it is taught.

Determinations the record settles:

- **The name.** `test_pack` is refused outright: it is already the name of an
  MCP *prompt*, and a client that surfaces both would show one name for two
  things. `experimental_test_packs` follows the tool family's verb-object
  shape.
- **`pack_id` optional; present-but-empty refused.** Absent runs every
  declared pack, exactly as `--id` omitted. A key present with an empty string
  is refused rather than silently running the whole project: presence is the
  discriminator on the wire, and this deliberately diverges from
  `packs test --id ""`, where flag emptiness has no presence to observe.
- **The tool-result/tool-error boundary.** `mismatch` and `skipped` are
  successful calls carrying the payload — the caller asked what the rows did
  and is being told, exactly as `packs test` prints its report while exiting
  nonzero. Tool errors are what stopped the run from happening: a bad
  argument, an unknown decision id, a configuration that is there and will not
  load, and no configuration at all.
- **No configuration is a tool error, unlike `list_packs`.** An empty
  inventory answers "what can this project decide"; a skipped suite does not
  answer "run the suite", and a model reading `skipped` as green is the exact
  misreading `packs test` refuses with its exit code. Presence is tested with
  `project.Present`, so a configuration demonstrably there and unloadable
  refuses instead of answering as a project that does not use the convention.
- **Nothing is written and no reviewed set is consulted.** A matrix row is a
  rehearsal: no audit record is appended (ADR-0018's split, restated), and a
  declared pack that drifted from its lock still runs (ADR-0019 keeps the
  matrix outside the reviewed set; the deciding surfaces are where drift
  refuses).
- **`outputVersion` stays `"2"`** — the payload is `packs test`'s own, with
  only the command string naming the surface.

### Consequences

- Good, because the loop the `test_pack` prompt teaches closes where the
  method is served, and the derived coverage report reaches the agent that
  authors the rows.
- Good, because one comparison judges every row on every surface, pinned by a
  test that the wire and the shell report one run identically.
- Bad, because the claim-scope enumeration grows to seven surfaces and
  CONFORMANCE.md must say so — held mechanically, so the cost is a build
  failure rather than a stale sentence.
- Bad, because one call may run the evaluator over every declared pack's every
  row: more work per call than any other tool here, bounded by the project's
  own declared documents.
- Bad, because one operation now carries two differently-marked names across
  surfaces (`packs test`, `experimental_test_packs`); `get_pack_diagram`'s
  shipping and withdrawal (0.6.x) shows an unprefixed tool can still be
  removed, but the marker is kept where ADR-0007 put it.
- Revisit when a graph twin is wanted — `experimental graph test` has the same
  gap one level up, and the `author_graph` prompt already says "where a
  terminal is available"; the deferral is recorded here rather than accidental
  — or when the evaluator leaves the experimental surface and every
  `experimental_`-prefixed name is renamed together.

## More information

Issue [#74]. The transcription line's consumer
(Judgment-Pack/judgment-pack-evaluator-experiments#23) is an agent working
over MCP whose acceptance check is exactly "run the suite"; this tool is that
check's surface. ADR-0014 (coverage derivation), ADR-0018 (what the audit
trail records and why a rehearsal is outside it), ADR-0019 (why the matrix is
not part of the reviewed set).

[#74]: https://github.com/Judgment-Pack/judgment-pack-runtime/issues/74
