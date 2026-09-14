---
status: proposed
date: 2026-09-14
deciders: maintainer
---

# Let a decision record cite the receipts it relied on, as given and never verified

## Context and problem statement

A project that asked for a trail (ADR-0018) holds, per decision, the pack's digest, the facts
as evaluated and the disposition. What it does not hold is where the facts came from. The
gateway's receipts prove which bytes a named source returned, and from version 3 of its receipt
which system, which query and which snapshot were read; the record says what was judged and
decided; nothing joins the two but a person with both files open. When a decision is challenged,
"what information did we have" is answered from the record and "where did it come from" from
receipts that the record never names.

The gateway already defines the other half of this join. Its action receipt carries `cites`, an
array of `{sessionId, callIndex, signature}` naming the acquisition receipts a requester says a
decision relied on, and `decision.recordDigest`, the SHA-256 of the decision record — where a
candidate record is any regular file under the decision-record directory, and for a `.jsonl`
file each line on its own. That verifier hashes candidates and interprets none of them. A record
line of this runtime's trail is therefore already citable by digest; what it cannot do is cite.

## Decision drivers

- The runtime holds no key, opens no connection and reads no store (ADR-0006, ADR-0012). It must
  not become a verifier of receipts to record a reference to them.
- A record that carries a caller's assertion must say it is one: recorded as given, whether or
  not the cited receipts exist or verify.
- The shape must be the gateway's own, so the same verifier that resolves an action receipt's
  citations can resolve a record's without a second grammar.
- One statement per rule: a citation that is not of the stated shape is a bad invocation, not a
  record with junk in a member no reader could resolve.
- A rehearsal (ADR-0028) records nothing, citations included.
- The member is additive: recordVersion stays `"1"`, as it did for `reviewed` (ADR-0019), under
  VERSIONING.md's rule that an added member is backward-compatible.

## Considered options

- **A. An optional `cites` member on every record of a run, supplied by the caller on the three
  deciding surfaces, held to the gateway's shape and otherwise recorded as given** (chosen).
- **B. Resolve or verify the citations before recording.** Rejected: it makes the runtime a
  reader of the gateway's store and a holder of its public key, which ADR-0006 and ADR-0012
  ruled out, and it would make a record's existence depend on a store the runtime does not own.
- **C. Record the citations under `inputs`.** Rejected: `inputs` is what the engine evaluated,
  and the engine never sees a citation; a citation is lineage the caller asserts about the
  inputs, and the gateway puts the same assertion at the top level of an action receipt.
- **D. Free-form references (`evidenceRefs`, URLs, digests of the caller's choosing).** Rejected:
  the verifier that would resolve them would need a grammar per caller, which is the spreadsheet
  the join exists to remove.

## Decision outcome

Chosen option: **A**, because the record then names, in the gateway's own grammar, what the
caller says entered the decision — and says nothing more than that.

**The shape.** `cites` is an array of objects, each with exactly three members: `sessionId`, a
flat token (`[A-Za-z0-9._-]{1,128}`, never `.` or `..`); `callIndex`, an integer from 0 to
2^53−1; `signature`, exactly 128 lowercase hexadecimal characters. These are the structural
constraints the gateway's SPEC.md (§§1.1, 1.2a, 3a) puts on an action receipt's citation, so
nothing is recorded that no conforming receipt could carry. A string is its value — a JSON
escape is read as the character it spells, and what the grammar admits is ASCII, so no
malformed Unicode passes as anything. The runtime holds a supplied document to that shape and
to nothing else: it does not check that a session exists, that a receipt verifies, or that the
cited receipts have anything to do with the facts. A document that is not of the shape is refused as a bad invocation before the
evaluator is reached, on every surface, exactly as a misspelled argument is.

**Where it enters.** `experimental evaluate --cites <file>` (a local file path; `-` is refused,
since the pack and the facts may already be standard input, and a URL or a remote path is
refused as every input is), `experimental graph evaluate --cites <file>`, and the MCP
`experimental_evaluate` tool's `cites` argument (the array itself, not text; a member given
twice is refused, not read last-wins). On every surface the document is held before the
project is consulted or an input read. The test verbs and
the matrix tools take no citations: they record nothing (ADR-0018), and a citation on a
rehearsal would be a claim about a decision nobody took.

**Where it lands.** On every record the run leaves: the one record of an evaluation, and on the
graph surface each node's record and the composite, since the citations belong to the run's
inputs and every record of a run shares its `run` id. The member is omitted when no citations
were supplied, so today's records are byte for byte what they were. Nothing about the
disposition, the trace or the payload changes: a citation is recorded, never evaluated.

**What a reader may conclude.** That the caller asserted these receipts. Whether they exist and
verify is the gateway verifier's finding, made against its own store with the decision-record
directory beside it; whether they were the right receipts is nobody's finding here. The record
carries the assertion so that the finding can be made at all.

### Consequences

- Good, because a decision is traceable to its receipts by a verifier that reads both ledgers,
  with no grammar of this runtime's own.
- Good, because the runtime's posture is unchanged: no key, no store, no network, no verification.
- Bad, because a caller can cite receipts that do not exist or do not verify, and the record
  will say so faithfully rather than refuse; the trail is the caller's claim, and the verifier
  is where the claim is tested.
- Bad, because the shape is the gateway's, so a change to the gateway's citation grammar is a
  change here; the member is versioned by the record, which is where such a change would show.
- Revisit when the gateway's verifier gains a citation pass over decision records (the other
  half of the join), or when a second receipt format needs citing.

## More information

- ADR-0018 (the trail), ADR-0019 (`reviewed`, the precedent for an additive member under
  recordVersion `"1"`), ADR-0028 (a rehearsal records nothing).
- The gateway's SPEC.md §1.2a (`cites` on an action receipt) and §4 steps 5 and 6 (how a
  citation resolves and how a decision record is found by digest, one `.jsonl` line at a time).
- `internal/audit` (`Citation`, `ParseCites`, the record member), the three deciding surfaces.
