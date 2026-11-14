# Revalidating before moving a Runtime pin

A project pins one Runtime version and moves the pin deliberately. Before it moves, the project can
check that the candidate version gives its own matrix rows the results the pinned version gave,
without both versions installed side by side: the pinned binary writes a baseline, and the candidate
binary compares its own results with that file
([ADR-0091](adr/0091-revalidate-matrix-results-before-moving-a-runtime-pin.md)).

`jpack experimental revalidate` is on the EXPERIMENTAL surface. It runs the same evaluator
`packs test` runs, and the conformance claim for that evaluator is stated, in full and only, in
[CONFORMANCE.md](../CONFORMANCE.md). A baseline is what one runtime gave for the project's own rows;
comparing two of them says whether two runtimes agree on those rows, and nothing about rows the
project did not write.

## The procedure

Run both steps from the same checkout of the project, with the pinned binary first.

From a pin of v0.46.0 or later, the pinned binary takes the baseline:

```bash
jpack experimental revalidate --write baseline.json
```

and the candidate binary checks against it:

```bash
jpack experimental revalidate --baseline baseline.json
```

A pin older than this command has no `--write`, so its baseline is the report `packs test` already
writes. Under the pinned binary:

```bash
jpack packs test --format json > baseline.json
```

`packs test` writes a complete report at exit 0 and at exit 1, when a row or a declared requirement
was not met. Either is a usable baseline when every declared pack and matrix loaded; a report in
which a pack or its matrix failed to load holds no rows for that pack, and `--baseline` refuses it
whole (`JPS-BASELINE-INVALID`, exit 3). Any other exit wrote no report. One `packs test`
run refuses a project whose packs declare more than one Core version
(`JPS-MATRIX-MIXED-CONTRACT`); take one report per pack with `--id` instead, and give the
candidate every report:

```bash
jpack packs test --id intake --format json > intake.json
jpack packs test --id expense --format json > expense.json
jpack experimental revalidate --baseline intake.json --baseline expense.json
```

A `packs test` report records no digest, so the check compares its rows and states, for each pack,
that the pack and matrix bytes were not compared (`bytes: "not-recorded"`, and a
`BYTES NOT COMPARED` line). In that case only running both steps from one checkout keeps the bytes
the same. The run's `status` is then `same` when every row is, though no byte was compared, so a
JSON consumer reads `summary.bytesNotRecorded` beside `status`: a count above zero says how many
packs' bytes were not compared.

## What is compared

Every declared pack with a matrix is run under the candidate, each pack on its own and by the code
`packs test` runs, and matched with the baseline by its id in `jpack.json`; rows are matched by
their id.

- **A row** is the same when the RFC 8785 canonical JSON of the object
  `{id, status, disposition, errorClass, errorPhase, handoffTarget}` is the same bytes on both
  sides. `disposition` is the row's actual canonical §8.3 disposition text, which holds the outcome,
  the reasons, sorted as the canonical form sorts them, and the handoff; `errorClass` and
  `errorPhase` are the §8.4 error a refused row produced, so a refusal is compared by its class and
  phase and not by its `JPS-*` code, which a row does not record; `handoffTarget` is the
  rendering a row that asserts `expectedHandoffTarget` reports;
  `status` is `passed` or `mismatch`. A member that is empty is absent. The expectation, the detail
  text and the coverage report are not compared: they are what the project wrote, or prose.
- **A pack** is compared by its `packId`, `packVersion` and the `specVersion` its declaration
  selected, as exact strings, and, from a baseline document, by the `sha256:` digests of the exact
  pack and matrix bytes its rows ran over. The pack digest is the form `jpack.lock.json` records.

Each difference is a finding: `row-differs` (naming the members), `row-only-in-baseline`,
`row-only-in-candidate`, `pack-bytes-differ`, `matrix-bytes-differ`, `pack-identity-differs`,
`spec-version-differs`, `pack-only-in-baseline`, `pack-only-in-candidate` and `pack-not-run`.
A finding carries the canonical text of each side. Rows are still compared when the bytes or the
`specVersion` differ.

No record is written and no reviewed set is consulted. Decision records and audit trails are not
compared by this command.

## Exit codes

| Exit | `--baseline` | `--write` |
| ---- | ------------ | --------- |
| 0 | No finding. | The baseline was written. |
| 1 | At least one finding. | A pack or its matrix does not load (`JPS-BASELINE-PACK-NOT-RUN`), no declared pack has a matrix (`JPS-BASELINE-EMPTY`), or the destination is a file the configuration declares (`JPS-BASELINE-PATH`). Nothing was written. |
| 2 | A baseline declares a `baselineVersion`, or a report an `outputVersion`, this runtime does not read (`JPS-BASELINE-VERSION`). | |
| 3 | A baseline is malformed, has a duplicate or unknown member, holds a pack another baseline holds, or the baselines exceed 64 MiB together (`JPS-BASELINE-INVALID`); or the invocation is invalid. | The invocation is invalid. |
| 4 | A baseline cannot be read (`JPS-BASELINE-READ`). | The destination exists or cannot be written (`JPS-BASELINE-WRITE`), or the baseline would exceed 64 MiB (`JPS-RESOURCE-BASELINE-BYTE-LIMIT`). |
| 5 | | The baseline could not be encoded (`JPS-BASELINE-ENCODE`). |

Every baseline is read and admitted before the project loads, so a refused baseline costs no row.
A configuration that does not load, and a refusal `packs test` makes for a whole run, such as an
unsupported profile (2) or a report budget crossed (4), keep the code `packs test` gives them in
both modes; nothing is compared or written.

## In CI

A job that runs before a pin change checks the candidate against the release the project pins.
With both release archives fetched and checked as
[Install a tagged release](../README.md#install-a-tagged-release) describes, and
their binaries at `$PINNED` and `$CANDIDATE`:

```bash
set -eu
status=0
"$PINNED" packs test --format json > baseline.json || status=$?
test "$status" -le 1
"$CANDIDATE" experimental revalidate --baseline baseline.json
```

When the pinned release has `experimental revalidate`, take the baseline with
`"$PINNED" experimental revalidate --write baseline.json` instead, so the bytes are compared too. The
last command's exit is the job's: 0 lets the pin move, 1 lists what differs.
