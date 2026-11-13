---
status: proposed
date: 2026-11-13
deciders: maintainer
---

# Evaluate a batch of fact documents in one run

## Context and problem statement

`jpack experimental evaluate` decides one facts document per process. Measured on one machine
against a chained, signed trail, one process per decision ran about 16 decisions a second, about
60 ms each; a resident MCP server ran about 60 recorded decisions a second and about 189 rehearsals
a second, about 5 ms each; and four servers writing one trail ran about 96 a second in all, because
the trail's lock serializes the writers (#357). A caller with many decisions to take against one
pack pays, per decision, for a process, the bundled schemas, the project's configuration, the pack's
admission, the reviewed-set consult, the signing key, and four syncs under the trail's lock.

The issue asks for a batch form of evaluate (many fact documents, one process, one lock acquisition
per batch or a stated equivalent), and for a look at why a rehearsal in a resident server still
costs about 5 ms.

Material-decision impact: public-surface.

## Decision drivers

- Each result is exactly what a single run gives for that item, byte for byte, with its exit code.
- The trail is exactly what single runs would leave: one record per decision, of the kind and shape
  a single run writes, so `audit verify` and every reader take it unchanged. No new record kind.
- No result is reported for a decision whose record is not durable.
- An interrupted batch leaves a trail that `audit verify` reports valid and that holds a prefix of
  the batch.
- Inputs are read as a stream, and every limit has a stable code.
- The conformance claim and its eleven-surface enumeration in CONFORMANCE.md stay as they are.
- A single run's output, exit codes and records do not change.

## Considered options

- **A. An option on `experimental evaluate`: `--batch <file-or->`.** The evaluator is reached
  through the surface already named, at its own constructor site in `internal/cli/app.go`.
- **B. A new subcommand, `experimental evaluate-batch`.** A clearer name, but it constructs the
  evaluator in a new place, a twelfth surface reaching it:
  `internal/cli/conformance_enumeration_test.go` would fail until CONFORMANCE.md's claim scope
  named it, and changing the claim is out of scope here.
- **C. B, reusing the `experimental evaluate` constructor.** It keeps the count at eleven by hiding
  a surface, which is what the enumeration exists to prevent.

## Decision outcome

Chosen option: **A**. Every check a single run makes once is made once for the batch, at the same
place in the same order, and the engine then evaluates each item against the pack admitted once.

1. **Invocation.** `jpack experimental evaluate <pack-or-> | --pack-id X --batch <file-or-> --format
   json [--max-items N]`, with `--rehearsal`, `--supported-extension`, `--rfc0008-quantifiers`,
   `--rfc0016-outcome-values` and `--config` as for a single run. Refused before the filesystem is
   consulted, exit 3: `--batch` with `--facts`, `--evidence`, `--cites` or `--binding`, under a
   format other than `json`, or with `--pretty` (`JPS-INVOCATION-BATCH`); `--max-items` below 1 or
   without `--batch` (`JPS-INVOCATION-MAX-ITEMS`); the pack and the batch both on standard input
   (`JPS-INVOCATION-STDIN`); a URL or remote path (`JPS-INVOCATION-INPUT`). Citations and a binding
   are one decision's commitments, and a batch records many, so they are refused rather than copied
   onto every record. A batch input that cannot be opened is `JPS-INPUT-READ`, exit 4, as a facts
   document that cannot be read is.
2. **Once per batch.** The project is loaded, the pack resolved and read, its contract selected,
   the writer and the unsigned reason taken, the reviewed set consulted, `requireReviewed` and
   `requireSigning` applied, and the evaluator constructed, once, exactly as for a single run. The
   pack is then admitted once. A pack the engine refuses is refused once, with the envelope and exit
   code a single run gives it, before any item is read, and nothing is recorded. The reviewed set is
   the one consulted at the start, as a single run consults it once; every record names the
   revision it was judged under.
3. **Items.** The batch is JSON Lines. Each line that ends in a newline, and a last line that does
   not, is one item: one JSON object of exactly `facts` (required), `id` (optional, a string of
   well-formed UTF-8, 1 to 256 bytes) and `evidence` (optional; present means supplied, as
   `--evidence` does), each once and spelled exactly. The `facts` and `evidence` values go to the
   engine as their exact bytes, so their every refusal, the byte limit
   (`JPS-RESOURCE-INPUT-BYTE-LIMIT`) included, is the one a single run gives for the same bytes in a
   file. A line that is not of that shape, a blank one included, is refused on its own as
   `JPS-INVOCATION-BATCH-ITEM`, exit 3, in a sentence that repeats nothing from the line. The JSON
   grammar locates the values, so a value that is not JSON at all (`NaN`) makes the line a shape
   refusal rather than the carrier refusal a file would get.
4. **Results.** One line per item, in input order:
   `{"line": N, "id": "…", "exitCode": E, "result": R}`, where `R` is exactly what `experimental
   evaluate --format json` writes for that item run alone (without its newline) and `E` is the code
   that run exits with; `id` is echoed when the line was an item that carried one. A refused item
   does not stop the others. There is no fail-fast option.
5. **Exit code.** 0 when every item produced a disposition (an empty batch too); 1 when the input
   was read to its end and some item was refused; otherwise the code of the last line, the one that
   stopped the batch: more items than `--max-items` (`JPS-RESOURCE-BATCH-ITEMS`, exit 4), an input
   that could not be read (`JPS-INPUT-READ`, exit 4), a chunk whose records could not be written
   (`JPS-AUDIT-WRITE`, exit 4, on each of its evaluations), or output that could not be written
   (exit 4).
6. **Chunks and the lock.** Records are composed as each item is evaluated and appended a chunk at
   a time with the existing `audit.Writer.AppendAll`: one hold of the trail's exclusive lock, one
   write of the chunk's chained lines, the trail file synced and then its directory, then every
   line signed and the signature sidecar written and synced, all under that hold. A chunk closes at
   256 items, at 32 MiB of input lines, or at the end of the input, whichever comes first. Other
   writers take the lock between chunks, so a chunk's records are consecutive in the trail and other
   records can fall between chunks. A rehearsal, or a project keeping no trail, writes each result
   as soon as it exists.
7. **Durability.** No result of a chunk is written before `AppendAll` for that chunk has returned,
   which is after the syncs above. An append that fails reports every evaluation of the chunk with
   the refusal a single run reports (`JPS-AUDIT-WRITE`, exit 4), keeps the refusals of the chunk's
   refused items, and stops the batch; the trail is then what `AppendAll` left, as for a single run.
   The `JPS-AUDIT-UNSIGNED` warning a single run adds is added to each evaluation of a chunk,
   decided from that chunk's append.
8. **Crash.** A batch ended between chunks leaves a trail that `audit verify` reports valid, holding
   exactly the chunks already reported: a prefix of the batch, in order. One ended during a chunk's
   write can leave an incomplete last line, as a single run can, which `audit repair` handles; the
   chunk's results were not reported.
9. **Records.** Each record is the one a single run writes for the item, of kind `evaluation` and
   surface `experimental evaluate`, differing only in what a run, its moment and the chain assign:
   `run`, `at`, `trail`, `sequence`, `previous`. A batch is one invocation, so its records share one
   run id, as a graph run's do; none of them is a graph record, so the graph reader rule does not
   apply to them. The pack digest is taken once (`audit.EvaluationRecordFor`).
10. **Limits.** `--max-items` defaults to 100000; the line one past it is refused and stops the
    batch, after the items before it are recorded and reported. A line is bounded at twice the
    documents' byte limit plus 64 KiB, 20 MiB and 64 KiB in all, so a facts and an evidence document
    each at the limit fit; a longer line is read to its end without being held and refused for its
    own line (`JPS-RESOURCE-BATCH-LINE-BYTE-LIMIT`, exit 4). The input is read a line at a time and
    what a chunk holds is bounded by the chunk.
11. **MCP and the SDK.** No MCP batch tool: a tool constructing the evaluator would be a twelfth
    surface and a change to the claim's enumeration, which is more than a thin wrapper. No SDK
    method: `sdk.Load` admits a pack once and `Pack.Evaluate` writes nothing, so a batch there is a
    loop.

### Measurements

Measured on one machine, the issue's pack and facts, a chained
trail signed by a key in force, each item a decision recorded and signed:

| run | decisions a second | each |
|---|---|---|
| one process per decision, before | 13 to 15 | 68 to 77 ms |
| one process per decision, after | 13 | 80 ms |
| one batch of 5000, after | 1730 | 0.58 ms |
| one process per rehearsal, before | 23 | 43 ms |
| one batch of 5000 rehearsals, after | 2224 | 0.45 ms |

The trail the batches wrote was reported valid by `audit verify --public-key`, every record signed.
`BenchmarkEvaluateDecisions` in `internal/cli` measures the same comparison in one process on an
unsigned chained trail: 33.6 ms a decision for one run per decision, 0.52 ms in one batch.

### Consequences

- Good, because many decisions against one pack cost one process, one admission, one consult of
  the project's law and one lock hold per 256 records, and the results and the trail are the ones
  single runs would give.
- Good, because a crash or a failed append never leaves a reported result without a durable record.
- Bad, because the results of a chunk wait for the chunk: a caller streaming items sees results in
  groups of up to 256 when the batch records.
- Bad, because a batch holds the trail's lock for one chunk's write and syncs at a time, so other
  writers wait for up to a chunk's worth of writing rather than one record's.
- Revisit if MCP callers ask for a batch, which would change the claim's enumeration, or if a
  caller needs citations or a binding per item.

## More information

Issue #357. Implementation: `internal/cli/evaluate_batch.go` (the reader, the item shape, chunks,
results), the flags and the branch in `internal/cli/app.go`, `evaluationFailureEnvelope` in
`internal/cli/render.go`, and `audit.EvaluationRecordFor` in `internal/audit/audit.go`. ADR-0018
(the trail), ADR-0028 (rehearsals), ADR-0044 (the reviewed set on the payload), ADR-0047 and
ADR-0071 (signing), ADR-0084 (an option instead of a new surface).
