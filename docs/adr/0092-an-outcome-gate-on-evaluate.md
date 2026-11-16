---
status: accepted
date: 2026-11-16
deciders: maintainer
---

# Gate a run or a batch on an allowed set of outcomes, with an exit code a CI step can act on

## Context and problem statement

A CI step that runs a pack over the cases it guards wants one answer: did every case end in an
outcome we accept? `experimental evaluate` exits 0 whenever it produced a disposition, whatever the
disposition is, and a batch (ADR-0085) exits 0 when every item produced one. So a step had to parse
every result line and compare outcome ids itself, and each script did it differently: some passed
an unresolved case, some failed on a not-applicable one, and some printed the facts that failed.

## Decision drivers

- One exit code a CI step can act on without reading the results.
- Telling "the cases were judged and one is a finding" apart from "the check could not judge".
- A mistyped outcome id must not silently pass, or fail, every run.
- The verdict must not leak fact values into CI logs or artefacts.
- No change to any run that does not ask for the gate: its exit codes and output stay as they are.
- The summary sits under a stability promise, so its shape carries a version from the start.

## Considered options

- Leave it to the caller: document how to read the result lines.
- A separate `gate` command that reads a batch's output.
- A flag on `evaluate`, single and batch, that changes only the exit code and adds a verdict.

## Decision outcome

Chosen option: a flag on `evaluate`, because the run already holds every disposition, and a
separate command would trust result lines a caller could have edited between the two steps.

`--require-outcome a,b` names the outcomes a run may end in.

**Exit codes.**

- **0:** every item produced a disposition, and every disposition is an outcome in the list.
- **1:** every item produced a disposition, and at least one is an outcome outside the list or
  `not-applicable`. This is a finding.
- **2:** the gate could not judge. That covers:
  - an item unresolved, or refused by the engine;
  - an input that could not be read;
  - a record or a result that could not be written;
  - a batch stopped at `--max-items`;
  - the gate's own arguments refused;
  - zero decisions, unless `--allow-empty` is given with `--batch`.

  The code the run would have exited with is kept in the summary as `runExitCode`.

**Two kinds of "refused".** The runtime refusing to evaluate (an §8.4 error, with no disposition)
exits 2. An outcome a pack declares with the id `refused`, or `needs-approval`, is an ordinary
outcome, judged against the list like any other.

**Not-applicable exits 1, not 2.** A not-applicable disposition is a decision the pack made: the
case is outside its scope. It is not an error, so it is a finding unless allowed. The id space is
not used for that: an id in the list must be an outcome the pack declares, and a pack may declare
an outcome named `not-applicable`. A later way to allow the not-applicable kind would be its own
flag, which adds without breaking.

**Unparseable command lines.** A command line that cannot be parsed exits 3, as for every command,
because the gate was never read. Under the gate, every exit other than 0 and 1 means the check did
not judge. A CI step treats 2 and 3 alike, and only 1 as a finding.

**Declared ids only.** Each id must be an outcome the pack declares. This is checked once the pack
is read, before any facts are read and before anything is evaluated or recorded. An empty or
repeated id is refused. A pack whose outcomes cannot be read is left to the engine, which refuses
it, so the gate exits 2. Had it been admitted, every disposition would still be judged against the
list, so the check fails closed.

**Output.** Standard output is the bytes the same run writes without the gate. The verdict goes to
standard error and names each item that did not pass by line, the caller's id and the outcome id.
`--gate-summary FILE` writes the same verdict as one JSON document:

- `gateSummaryVersion`, which is `"1"`;
- `verdict` and `exitCode`, plus the run's own exit code as `runExitCode`;
- `allowedOutcomes`, and `packId` and `packVersion`;
- `inputs`: each digest names what it covers (`pack` by path or by `--pack-id`, `facts`,
  `evidence`, or `batch`). The batch digest is of the bytes the batch read, never of a second read
  of the path, and is given only when the batch was read to its end (`readToEnd`);
- `items` and `decisions`;
- `failing`, each entry with its line, id, kind, outcome id, or the exit code of an item that
  produced no disposition.

No fact or evidence value appears in either form.

Members are added to the summary within a major version, never removed or changed in meaning.

### Consequences

- Good, because a CI step needs only the exit code, and the summary is an artefact it can keep.
- Good, because a mistyped id is refused before anything is evaluated or recorded.
- Bad, because exits 1 and 2 mean something else under the gate than they do for the same command
  without it (invalid and unsupported). The flag is opt-in, and the help says so.
- Revisit when a caller needs to allow the not-applicable kind, or to gate a graph evaluation.

## More information

- ADR-0085 (the batch, its exit codes and its result lines).
- `internal/cli/gate.go` and `internal/cli/gate_test.go`.
