---
status: accepted
date: 2026-09-30
deciders: maintainer
---

# Compare what two versions of a pack decide for the same inputs

## Context and problem statement

Someone who revises a rule wants to see which inputs now get a different answer before writing any
new expectation. Today only `packs test` compares anything, and it compares each row with the
expectation the row states. Measured with jpack 0.23.1 on the specification's
`deal-evidence-readiness` demo, after moving a discount limit from `"20"` to `"15"` (issue #187):
`packs test` finds the 3 of 40 rows whose expectations the move broke. `packs suggest` derives 38
candidate inputs from the revised pack, among them discounts of 14, 15 and 16, the inputs nearest
the moved line. They carry no expectation by design (ADR-0024), so the matrix loader refuses them,
and the only way to see what the two versions do with them is one rehearsal per input per version,
compared by hand.

[ADR-0034](0034-profile-a-matrix-against-its-history.md) names the same question for history: how
many past cases sit on each side of a line is what "a policy owner asks before moving the line".

A difference between two documents is not an expectation. It says the two versions disagree on an
input and nothing about which is right, so it is not the circular oracle ADR-0024 refuses.

## Decision drivers

- A difference is information, never a verdict. Nothing about it gates, and nothing is recorded.
- The comparison must be the specification's own equality (§8.3), computed once, in the runtime,
  rather than re-implemented by every client.
- The inputs a person already has should work as they are: a matrix and a `packs suggest` candidates
  document.
- A run of this command is a rehearsal (ADR-0028) by construction.

## Considered options

- **A. A flag on `packs test`**, such as `--against old.json`. `packs test` judges rows against
  their expectations; a difference is not a failure, candidates are not rows, and its report and its
  exit code would then mean two different things.
- **B. A command of its own**, `jpack experimental compare`.
- **C. Clients.** Desk can already rehearse a supplied pack and matrix through
  `experimental_test_cases` (ADR-0036), but only for rows that state an expectation, and each client
  would re-implement §8.3's equality.

## Decision outcome

Chosen option: **B**.

1. **The command.** `jpack experimental compare <old-pack> <new-pack> --inputs <file>`, under
   `experimental` because it runs the experimental evaluator (ADR-0007). The two packs are documents
   by path, typically the committed version and the working copy. The inputs are either a matrix,
   admitted under the matrix's own rules but whose expectations play no part in the comparison,
   or a candidates document from `packs suggest`
   (`candidatesVersion` `"1"`), which this command is the first to read. Each is held to its own
   closed shape; a document that is neither is refused.
2. **The run.** Every input is evaluated under both packs, as `packs test` evaluates a row that
   reaches evaluation (a row `packs test` stops on its own expectation still reaches compare): its facts,
   its evidence availability, and its own supported extensions, joined by any the caller names. No
   project is opened. No audit record is written and no reviewed set is consulted, and the payload
   carries `"rehearsal": true`. The draft-RFC opt-ins are not offered.
3. **What differs.** An input differs when the two §8.3 canonical dispositions are not byte for byte
   equal, when the handoff targets differ, or when one side is refused and the other is not, or both
   are refused differently. Two refusals of the same class, phase and code are the same. Each
   difference lists both sides and names what changed: `kind`, `outcomeId`, `reasons`, `handoff`,
   `handoffTarget`, or `refusal`. The names describe; the canonical bytes decide.
4. **The report.** It names both packs (path, id, version, and the digest of the exact bytes, which
   is absent for a pack over the byte limit, whose bytes were never whole in hand), the kind of
   inputs, the counts of inputs, same and different, and every difference in input order. Inputs
   that are the same are counted, not listed. A difference repeats two dispositions and two handoff
   targets whose strings a pack may make large, so the report is charged as it is built and the run
   is refused past 16 MiB, the matrix's own limit, rather than truncated.
5. **Exit status.** 0 whenever the comparison ran, however many inputs differ. Producing a
   disposition is success, and so is producing a comparison. Unreadable or malformed arguments and
   inputs are refused with the runtime's usual classes.
6. **No MCP tool yet.** One is added on these terms when a client asks for it.

### Consequences

- Good, because the inputs nearest a moved line can be seen changing before anyone writes an
  expectation for them, and the comparison is the one §8.3 defines.
- Good, because the command cannot write a record or consult a lock: it opens no project.
- Bad, because a second document reader for candidates now exists, and the candidates shape is
  therefore an input as well as an output; its version moves if a member is added.
- Neutral, because a comparison says nothing about which version is right. The report and its label
  say so.

## More information

Issue #187. ADR-0024 (candidates are not rows), ADR-0028 (the rehearsal), ADR-0034 (the history
profile), ADR-0036 (rehearsing supplied snapshots), JPS §8.3 (the canonical disposition).
