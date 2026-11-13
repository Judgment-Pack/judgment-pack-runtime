---
status: accepted
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
- An interrupted batch leaves a trail holding a prefix of the batch's records, every reported one
  among them, and a caller can tell which of its items those records are.
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
   not, is one item: one JSON object of exactly `facts` (required), `id` (optional) and `evidence`
   (optional; present means supplied, as `--evidence` does), each once. A member name is compared
   as written, escapes undecoded, so `"\u0066acts"` is not `facts`. The `id` is a string of
   well-formed UTF-8, 1 to 256 bytes once decoded, whose every escape names a Unicode character:
   an escaped half of a surrogate pair without its other half is refused, not echoed as U+FFFD.
   The `facts` and `evidence` values are located, not decoded: a string runs to its closing quote,
   an object or an array to the bracket that closes the last one open, of either kind, and
   anything else to the next whitespace or structural character. The bytes so located go to the
   engine as they are, so their every refusal, the byte limit (`JPS-RESOURCE-INPUT-BYTE-LIMIT`)
   and the depth limit (`JPS-RESOURCE-DEPTH-LIMIT`, at any depth, deeper than a JSON decoder's own
   limit included) among them, and the refusal of a value that is not JSON (`NaN`, a string with a
   raw control character, `[}`), is the one a single run gives for the same bytes in a file. A line
   that is not of that shape, one whose object cannot be delimited or a blank one included, is
   refused on its own as `JPS-INVOCATION-BATCH-ITEM`, exit 3, in a sentence that repeats nothing
   from the line.
4. **Results.** One line per item, in input order:
   `{"line": N, "id": "…", "run": "…", "exitCode": E, "result": R}`, where `R` is exactly what
   `experimental evaluate --format json` writes for that item run alone (without its newline) and
   `E` is the code that run exits with; `id` is echoed when the line was an item that carried one;
   `run` is on every line of a batch that records, the stop line included, and is the run id
   every record of the batch carries (§9); a batch that records nothing, a rehearsal or a project
   keeping no trail, has no `run`. Before it appends its first record, a batch that records also
   writes `note: the records of this batch carry run <id>` to standard error, so the id is known
   even when the batch ends before it writes a line. A refused item does not stop the others.
   There is no fail-fast option.
5. **Exit code.** 0 when every item produced a disposition (an empty batch too); 1 when the input
   was read to its end and some item was refused; 4 when the batch stopped, the stop being the last
   thing it reports: more items than `--max-items` (a last line `JPS-RESOURCE-BATCH-ITEMS`, exit 4,
   after the items before it), an input that could not be read (a last line `JPS-INPUT-READ`, exit
   4), a chunk whose append failed (`JPS-AUDIT-WRITE`, exit 4, on each of its evaluations, reported
   as not confirmed, §7; the last chunk reported), or output that could not be written.
6. **Chunks and the lock.** Records are composed as each item is evaluated and appended a chunk at
   a time with the existing `audit.Writer.AppendAll`: one hold of the trail's exclusive lock, one
   write of the chunk's chained lines, the trail file synced and then its directory, then every
   line signed and the signature sidecar written and synced, all under that hold. A chunk closes at
   256 items, at 32 MiB of input lines, or at the end of the input, whichever comes first. Other
   writers take the lock between chunks, so a chunk's records are consecutive in the trail and other
   records can fall between chunks. A rehearsal, or a project keeping no trail, writes each result
   as soon as it exists.
7. **Durability.** No result of a chunk is written before `AppendAll` for that chunk has returned,
   which is after the syncs above, so every evaluation the output reports, exit 0, has a durable
   record. The output is a lower bound, not the trail's whole account of the batch. An append can
   fail after writing some or all of the chunk's lines: a write that fails partway, a sync that
   fails, a signature sidecar that cannot be written. So an append that fails reports every
   evaluation of the chunk with the code a single run reports (`JPS-AUDIT-WRITE`, exit 4) and a
   message that opens by saying the item's record is not confirmed written, that the trail may hold
   all of the chunk's records, the first of them, or none, under the batch's run id, and then says
   what the append reported. It keeps the refusals of the chunk's refused items and stops the
   batch. The trail is then what `AppendAll` left, as for a single run, but a single run's exposure
   is one record and a batch's is a chunk. The `JPS-AUDIT-UNSIGNED` warning a single run adds is
   added to each evaluation of a chunk, decided from that chunk's append.
8. **Crash.** A batch ended (killed, or its machine stopped) leaves a trail holding the chunks it
   reported and possibly the next chunk too: appended and synced but not yet reported, or partly
   written and ending in an incomplete line, which `audit repair` handles. So after exit 4 or an
   interruption the trail may hold up to one chunk of whole records the output did not report as
   recorded, unsigned when that chunk's signatures were not written. Those records are found by
   the batch's run id, which every line and the standard-error note name (§4). Before items are run
   again, a caller:
   - repairs the trail if `audit verify` reports an incomplete last line;
   - lists the trail's records carrying the run id. In sequence order they are the records of the
     batch's items that produced an evaluation, in input order. A record carries no line number or
     id, but its `inputs` member carries the item's facts and evidence documents, as JSON values;
   - matches the records beyond those of the items reported with exit 0 to the items after them in
     input order. After a failed append those are the items reported as not confirmed, first to
     last. After an interruption they are the unreported items, each record paired with the next
     unreported item whose facts and evidence it carries;
   - runs again only the items no record matches.
   A batch ended between chunks, while it waits for input, leaves exactly the chunks reported.
9. **Records.** Each record is the one a single run writes for the item, of kind `evaluation` and
   surface `experimental evaluate`, differing only in what a run, its moment and the chain assign:
   `run`, `at`, `trail`, `sequence`, `previous`. A batch is one invocation, so its records share one
   run id, as a graph run's do, and the output names it (§4); none of them is a graph record, so
   the graph reader rule does not apply to them. The pack digest is taken once
   (`audit.EvaluationRecordFor`). The signing key is read once for the batch, as for a single run,
   by the writer made at the start; whether it is the key in force is checked under the lock at
   each chunk's append. The schema's text for `signingPublicKey` says so.
10. **Limits.** `--max-items` defaults to 100000; the line one past it is refused and stops the
    batch, after the items before it are recorded and reported. A line is bounded at twice the
    documents' byte limit plus 64 KiB, 20 MiB and 64 KiB in all, so a facts and an evidence document
    each at the limit fit. The newline that ends a line is not counted, and a last line without one
    has the same bound. A longer line is read to its end without being held and refused for its
    own line (`JPS-RESOURCE-BATCH-LINE-BYTE-LIMIT`, exit 4). The input is read a line at a time and
    what a chunk holds is bounded by the chunk.
11. **MCP and the SDK.** No MCP batch tool: a tool constructing the evaluator would be a twelfth
    surface and a change to the claim's enumeration, which is more than a thin wrapper. No SDK
    method: `sdk.Load` admits a pack once and `Pack.Evaluate` writes nothing, so a batch there is a
    loop.

12. **The rehearsal in a resident server.** A CPU profile of the MCP server's
    `experimental_evaluate` rehearsal, called in-process, put its time in: compiling the
    configuration schema on every project load (`internal/project/project.go`, `Load`), about 47%;
    admitting the pack again on every call (`internal/mcp/tools.go`, `EvaluateWith`), about 25%;
    verifying the embedded bundle, a SHA-256 of every bundled file, on every evaluation
    (`internal/evaluation/engine.go`, `artifacts.Load`), about 11%; reading the signing key for a
    run that records nothing (`internal/mcp/tools.go`, `DecisionWriter`), about 5%; and the
    garbage collector, which the rest drives, about 20%. The first and the third are fixed here:
    the schema is compiled once per process and each bundle verified once per process. Both are
    embedded in the binary and cannot change while it runs, so the outcome of the first
    compilation or verification, a refusal included, is the outcome of every later one, and each
    `artifacts.Load` still hands out a Set of the caller's own. The second and the fourth are in
    the MCP surface and are left as follow-ups: a server holding admissions across calls needs a
    stated identity and eviction rule for the packs it keeps, and a rehearsal needs no writer.
    A recorded decision in the server is dominated by the trail's four syncs, about 10 ms of it;
    that is the durability rule, and a batch is how it is shared.

### Measurements

Measured on one machine, the issue's pack and facts, a chained trail signed by a key in force,
each item a decision recorded and signed, by the CLI (one process per run):

| run | before | with the batch | with the batch and the fixes of 12 |
|---|---|---|---|
| one process per decision | 13 to 15 a second, 68 to 77 ms | 13 a second, 80 ms | 14 a second, 72 ms |
| one batch of 5000 decisions | | 1730 a second, 0.58 ms | 5390 a second, 0.19 ms |
| one process per rehearsal | 20 to 23 a second, 43 to 50 ms | | 24 a second, 42 ms |
| one batch of 20000 rehearsals | | 2224 a second, 0.45 ms (5000) | 25800 a second, 0.04 ms |

The trail the batches wrote was reported valid by `audit verify --public-key`, every record signed.
In one process, the MCP server's `experimental_evaluate` call:

| call | before | after the fixes of 12 |
|---|---|---|
| rehearsal | 3.7 ms | 1.2 ms |
| recorded decision | 14.7 ms | 12.1 ms |
| project load | 1.9 ms | 0.045 ms |
| evaluation of an admitted pack | 0.39 ms | 0.016 ms |

`BenchmarkEvaluateDecisions` in `internal/cli` measures the CLI in one process on an unsigned
chained trail: one run per decision 33.6 ms before the fixes of 12 and 23.8 ms after; one batch
0.52 ms a decision before them and 0.11 ms after.

### Consequences

- Good, because many decisions against one pack cost one process, one admission, one consult of
  the project's law and one lock hold per 256 records, and the results and the trail are the ones
  single runs would give.
- Good, because a crash or a failed append never leaves a reported result without a durable record.
- Bad, because the converse does not hold: after a failed append or an interruption the trail can
  hold up to one chunk of records the output did not report as recorded, which a caller finds by
  the run id and matches by order and inputs (§8) before running items again. Truncating a failed
  append back to the trail's size under the lock would narrow this for a failed write; it would not
  for a kill, and it is not done here.
- Bad, because the results of a chunk wait for the chunk: a caller streaming items sees results in
  groups of up to 256 when the batch records.
- Bad, because a batch holds the trail's lock for one chunk's write and syncs at a time, so other
  writers wait for up to a chunk's worth of writing rather than one record's.
- Good, because every surface that loads a project or evaluates stops recompiling and re-hashing
  what is embedded in it.
- Revisit if MCP callers ask for a batch, which would change the claim's enumeration, or if a
  caller needs citations or a binding per item; and when the MCP surface takes up the follow-ups
  of 12.

## More information

Issue #357. Implementation: `internal/cli/evaluate_batch.go` (the reader, the item shape, chunks,
results), the flags and the branch in `internal/cli/app.go`, `evaluationFailureEnvelope` in
`internal/cli/render.go`, and `audit.EvaluationRecordFor` in `internal/audit/audit.go`; the fixes of
12 are `configSchema` in `internal/project/project.go` and `verifiedSets` in
`internal/artifacts/artifacts.go`. ADR-0018
(the trail), ADR-0028 (rehearsals), ADR-0044 (the reviewed set on the payload), ADR-0047 and
ADR-0071 (signing), ADR-0084 (an option instead of a new surface).
