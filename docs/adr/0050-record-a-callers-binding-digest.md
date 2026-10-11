---
status: proposed
date: 2026-10-11
deciders: maintainer
---

# Record a caller's binding digest on the evaluation record, as given and never interpreted

## Context and problem statement

A companion can prepare an evaluation's inputs before this runtime evaluates them. Runner, for a
mapping-v2 run, verifies each acquisition, and each table of a calculator that computed, as fresh
against one instant (`preparation.verifiedAt`), beside the mapping's digest and the parameters each
source was bound to. The record this runtime writes and signs (ADR-0047 §2b) carries the facts, the
evidence and the citations the caller passed (ADR-0033), and nothing of that preparation. An export
whose `verifiedAt` was moved by one second therefore still verified with signing and witnessing
required: the signed record and the checkpoint were identical (Judgment-Pack/judgment-pack-runner#55).

Runner cannot commit the preparation itself. It signs nothing of its own, the record has no member a
caller may fill with its own material, and `cites` is held to a gateway citation's shape and must
carry nothing else.

## Decision drivers

- The runtime interprets no caller material, verifies no claim a caller makes and reads no store
  (ADR-0006, ADR-0012, ADR-0033). Recording a commitment must not change that.
- A record written without the new input stays byte for byte what it is.
- The commitment is covered by the protections every member already has: the chain and the record
  signature. Nothing new is signed and no key is added.
- A rehearsal records nothing (ADR-0028).
- One statement per rule: a commitment not of its shape is a bad invocation, not a record carrying
  something no reader can read.
- The member is additive: under VERSIONING.md an added member of a record this runtime produces is
  backward-compatible and does not move `recordVersion`, which stays `"1"`, as it did for `reviewed`
  (ADR-0019), `cites` (ADR-0033) and the chain (ADR-0047).

## Considered options

- **A. One optional `binding` member, `{kind, digest}`, supplied by the caller on an operational
  evaluation, held to its shape and otherwise recorded as given** (chosen).
- **B. Record the preparation itself.** Rejected: it is the caller's material, of the caller's
  shape and size, and the runtime would carry content it neither evaluates nor reads. A digest
  commits to it as well, and the caller already holds what it digests.
- **C. Carry the commitment in `cites`.** Rejected: `cites` is the gateway's citation grammar, so
  that the gateway's verifier resolves a record's citations with no second grammar (ADR-0033). A
  member of another shape in it would break that.
- **D. Have the companion sign its own material.** Not this runtime's to decide, and it would add a
  second key for an operator to hold, for a commitment the record signature can already carry.
- **E. A free-form caller member.** Rejected: a member whose shape no reader knows cannot be held to
  anything, and a scheme name and a digest are all a commitment needs.

## Decision outcome

Chosen option: **A**, because the record then carries, under the signature it already has, the one
fact a companion needs committed, and says nothing about it.

**The shape.** `binding` is one JSON object of exactly two members, none given twice: `kind`, 1 to
128 characters of `a-z`, `0-9`, `.`, `_`, `/` and `-`, naming the caller's scheme (Runner's is
`judgment-pack-runner/preparation/1`); and `digest`, `sha256:` and 64 lowercase hexadecimal
characters. Member names are matched exactly, a string is its value (a JSON escape is read as the
character it spells, and the grammar admits only ASCII), and a document is at most 1024 bytes.
`audit.ParseBinding` is that rule, on every surface and in `audit verify`.

**Where it enters.** `jpack experimental evaluate --binding <file>` (a local file path; `-` and a URL
or remote path are refused, as for `--cites`) and the MCP `experimental_evaluate` tool's `binding`
argument (the object itself, not text). The document is held at the point the citations are: after
the arguments and before the project is consulted or an input read. A document not of the shape is
refused as a bad invocation, `JPS-INVOCATION-BINDING`, exit 3 on the CLI and a tool error on MCP,
with a fixed sentence that says what is wrong and repeats no value of the document. A rehearsal
with a binding is refused too, before the document is read: a rehearsal leaves no record, and a
binding accepted and then not recorded would let the caller believe it was committed. `experimental
graph evaluate` takes none; no caller has asked for one there.

**Where it lands.** On the evaluation's record, beside `cites`: the member `binding`, written as
every member is, compact, `kind` then `digest`, whatever order and spacing the caller used. It is
assigned by the writer for the invocation (`Writer.Bind`), as the reviewed set is. It is omitted
when none was given, so a record written without one is byte for byte what it was, which a test
holds against a trail the v0.30.0 writer wrote. It is a member of the chained line, so the chain
and the record signature cover it as they cover the rest of the line: a changed byte breaks the
next record's link and the record's signature, as for any member. As for every record, a project
that keeps no trail records nothing, binding included.

**What the runtime does not do with it.** It never reads meaning into the digest, never verifies
it and never recomputes it. The evaluation and the disposition do not depend on it. Whether the
digest is the digest of anything is the caller's verifier's finding, made from material this
runtime never sees. Runner's side, still to be built (Judgment-Pack/judgment-pack-runner#55), has
`verify-run` recompute it from the export's preparation and refuse an export whose digest differs.

**What `audit verify` checks.** Only the shape. A line that is one JSON object and carries a
`binding` not of the shape is the finding `binding-malformed` at that line, chained or not, with a
fixed detail naming no value; the verification fails (exit 1), and `audit checkpoint` and `audit
stamp` refuse the trail as they refuse any trail with a finding. A well-formed binding is not
mentioned. `binding-malformed` is not a check of the chain: like a finding about the sidecar it
moves no coverage, so the signed, checkpointed, stamped and countersigned coverage are what they
would be without it. A malformed binding says the line is not one this runtime's surfaces would
have written; it does not say the line's bytes are other than those signed and chained.

**What a reader may conclude.** That the record whose signature verifies carried this binding when
it was signed: the material whose digest it is, once the caller's verifier recomputes it, is the
material the caller handed the runtime for that record. Not, against the operator, who holds the
key: the operator can recompute the material, write a record with the new binding and sign it,
and that record verifies like the first; only a checkpoint held independently from before the
change shows the difference (ADR-0047 §2a). And not when the material was prepared: the binding
carries no time.

### Consequences

- Good, because a companion commits its own material under the record signature without holding a
  key of its own.
- Good, because the runtime's posture is unchanged: no key, no store, no interpretation.
- Bad, because a caller can bind any digest, and the record says so faithfully; the trail is the
  caller's claim, and the caller's verifier is where the claim is tested.
- Bad, because the shape is fixed: a caller that needs to commit two things digests them together.
- Revisit when a caller needs a binding on a graph evaluation, or a commitment of another shape.

## More information

- Judgment-Pack/judgment-pack-runner#55 (the gap and Runner's side) and #57, this runtime's #263
  (the request).
- ADR-0018 (the trail), ADR-0028 (a rehearsal records nothing), ADR-0033 (`cites`, the precedent
  for a caller's assertion recorded as given), ADR-0047 (the chain and the record signature),
  VERSIONING.md (why `recordVersion` stays `"1"`).
- `internal/audit` (`Binding`, `ParseBinding`, `Writer.Bind`, `FindingBindingMalformed`), the
  `--binding` flag in `internal/cli` and the `binding` argument in `internal/mcp`.
