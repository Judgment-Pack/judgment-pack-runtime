---
status: proposed
date: 2026-11-13
deciders: maintainer
---

# Revalidate a project's matrix results against a baseline file before moving a Runtime pin

## Context and problem statement

A project pins one Runtime version, and the README told it to move the pin deliberately and
"revalidate original bytes" without saying how. Nothing compared what two Runtime versions give for
the same project, and one process cannot run two binaries. Whatever the comparison reads has to be
written by the pinned binary first, and the pinned binary of the first pin move after this change
predates the command (issue #403).

Material-decision impact: public-surface.

## Decision drivers

- "Same" has one meaning: byte equality over a canonical form this ADR states, never a judgement.
- The rows are judged by the code `packs test` runs; there is no second path to drift from it.
- The check works from a pin that predates it, using only what that release already writes.
- A baseline is a closed input, admitted whole before any row runs.
- No existing bytes or behaviour change: not the `packs test` report, not evaluation, not records.
- Nothing is downloaded, and no process runs a second binary.

## What released binaries already write

`jpack packs test --format json` (`outputVersion` `"2"`, with the same members in v0.45.0 as
in this change) writes, for each row, `status` and the actual results `actual` (the canonical §8.3
disposition, `""` when there is none), `actualErrorClass`, `actualErrorPhase` and
`actualHandoffTarget`, with each pack's `packId` and `packVersion` and the run's
`evaluatorSpecVersion`. It writes no digest of the pack or matrix bytes. One run refuses packs of
more than one Core version (`JPS-MATRIX-MIXED-CONTRACT`), so a project that mixes them needs one
report per pack, taken with `--id`. It writes a complete report at exit 0 and at exit 1.

## Considered options

- **A. Read only the `packs test` report.** Works from any pin that writes `outputVersion` `"2"`,
  but no form of the check could ever compare bytes, and a project of mixed Core versions needs one
  report per pack.
- **B. Only a new baseline format, written by a new command.** Records the digests, but the pin
  move from v0.45.0 cannot use it: v0.45.0 has no such command.
- **C. Both: a new baseline format, and the `packs test` report read as a baseline of rows only**,
  whose bytes are reported as not recorded.
- **D. Add digests to the `packs test` report.** Changes that report's bytes, and does nothing for
  pins already released.
- **E. Take the pack digests from `jpack.lock.json`.** The lock records no matrix digest, a project
  need not have a lock, and the lock says what was reviewed, not what was run.

## Decision outcome

Chosen option: **C**, because it checks the next pin move with what v0.45.0 already writes and
records bytes from v0.46.0 on, and changes nothing that exists.

1. **The command.** `jpack experimental revalidate` takes exactly one of `--write FILE` (or `-`
   for stdout) and `--baseline FILE`, which may be repeated. It sits on the EXPERIMENTAL surface.
2. **What is run.** Every declared pack that has a matrix, in the configuration's sorted id order,
   each pack on its own: its own contract selection and the row loop `packs test` runs
   (`testLoadedPack`). A pack's bytes and its matrix's bytes are read once; those bytes are
   digested and those bytes are run. A project of mixed Core versions is run whole. Packs without
   a matrix are listed and not compared.
3. **The baseline document**, `baselineVersion` `"1"`:
   `{baselineVersion, tool: {name, version}, packs: [{id, packId, packVersion, specVersion,
   packDigest, matrixDigest, rows: [{id, status, disposition?, errorClass?, errorPhase?,
   handoffTarget?}]}]}`. Digests are `sha256:` and 64 lowercase hex digits, the form the lock
   records. The writer encodes members in that order with two-space indentation, no HTML escaping
   and a final newline, reads the document back with its own reader, and refuses to write what it
   cannot read. It writes only a new file, never a path the configuration declares, and refuses
   the whole write when a declared pack or its matrix does not load or nothing has a matrix: a
   baseline that left a pack out would later compare as if nobody had run it. A row that does not
   match its own expectation is recorded as given and counted.
4. **What "same" means.** A row is the same exactly when the RFC 8785 canonical JSON of
   `{id, status, disposition, errorClass, errorPhase, handoffTarget}`, each a string and each
   optional member absent when empty, is the same bytes on both sides. `status` is in it because
   it is the candidate's own comparison of the row with its expectation, so a change in how an
   expectation is matched is a finding too. The expectation, `detail`, `cites`, `origin`,
   `specSection` and the coverage report are not compared: they are the project's text, or prose.
   A pack's `packId`, `packVersion` and `specVersion` are compared as exact strings, and its
   digests when the baseline holds them. Rows are matched by id, packs by their id in `jpack.json`.
5. **Findings.** `pack-only-in-baseline`, `pack-only-in-candidate`, `pack-not-run`,
   `pack-bytes-differ`, `matrix-bytes-differ`, `pack-identity-differs`, `spec-version-differs`,
   `row-differs` (naming the members, with both canonical texts), `row-only-in-baseline` and
   `row-only-in-candidate`, ordered by pack id, pack findings first, then the baseline's rows in
   its order, then the candidate's other rows in its order. Rows are compared even when bytes or
   `specVersion` differ. The status is `same` exactly when there is no finding: exit 0, otherwise 1.
6. **The report form.** `--baseline` also reads a `packs test` report of `outputVersion` `"2"` and
   `command` `"packs test"`. Its rows are compared through the members named above; its
   `specVersion` is the root `evaluatorSpecVersion`; its bytes are `not-recorded`, never `same`,
   and the human report says `BYTES NOT COMPARED`. An entry with no `matrixPath` is skipped; an
   entry with a `matrixPath` and no rows is refused, since it is a pack whose matrix did not run.
   Several baselines are merged by pack id; a pack two of them hold is refused. The form is
   decided by the root member: `baselineVersion` or `outputVersion`, never both or neither.
7. **A closed input.** The version is settled first, so a later `baselineVersion` or
   `outputVersion` is `JPS-BASELINE-VERSION` (exit 2) whatever else it holds. Then invalid JSON, a
   duplicate member name, a member the form does not carry, a wrong type, a present but empty
   optional member, a malformed digest, a duplicate pack or row id, more than 10,000 rows in a
   pack, more than 64 MiB of baselines together, 4,000,000 nodes, a 10 MiB string or depth 128 is
   `JPS-BASELINE-INVALID` (exit 3), before the project loads.
8. **The conformance surface.** The command runs the evaluator, so it is the twelfth surface the
   CONFORMANCE.md enumeration names, as `experimental compare` became one (issue #197). Its
   baseline side is what the runtime that took it gave; only the candidate side is this
   evaluator's.

Decision records and audit trails are not revalidated here: a record keeps digests where a trail
keeps documents, so re-running them needs its own design (a follow-up issue). Graphs and policy
documents are out of scope, and there is no `--id` selection in format `"1"`.

### Consequences

- Good, because the pin move from v0.45.0 can be checked with what v0.45.0 already writes, and
  from v0.46.0 on the check also compares the exact pack and matrix bytes.
- Good, because a difference is shown as both canonical texts, so a reader sees what changed
  without re-running either side.
- Good, because no existing output, record or evaluation changes by a byte.
- Bad, because a report baseline cannot say the bytes were the same; only running both sides from
  one checkout keeps them so, and the report says so.
- Bad, because the check is as broad as the project's matrices: a fact combination no row holds
  is not compared.
- Bad, because `baselineVersion` `"1"` is a new closed input to keep, beside the report form.
- Revisit when `packs test` reports gain digests, when a later `outputVersion` is written, when
  records or audit trails are revalidated, or when graphs need the same check.

## More information

Issue #403; the procedure is [Revalidating before moving a Runtime pin](../revalidating-a-runtime-pin.md).
Implementation: `RunMatrices` in `internal/project/revalidate.go`; `Encode`, `Decode` and
`Compare` in `internal/baseline`; the command in `internal/cli/revalidate.go`; the report types in
`internal/result/revalidate.go`. Tests: `internal/baseline/baseline_test.go` (the comparison
table, the closed-input refusals, a hand-written v0.45.0 report fixture in
`internal/baseline/testdata`, and a reflection guard that the report members are the `packs test`
payload's) and `internal/cli/revalidate_test.go`. ADR-0007 and ADR-0011 (the experimental
evaluator and its claim), ADR-0025 (the asserted handoff target).
