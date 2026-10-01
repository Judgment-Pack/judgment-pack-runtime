---
status: proposed
date: 2026-10-01
deciders: maintainer
---

# Make a decision record defensible to someone who does not trust its operator

## Context and problem statement

The audit trail (ADR-0018) appends one JSON line per decision to `evaluations.jsonl`. A line holds the pack's digest, the inputs, the evaluator's version and executable digest (ADR-0043), whether the reviewed set was applied (ADR-0044), the citations the caller supplied (ADR-0033), and the disposition. That is enough to *reproduce* a decision: give the same pack and inputs to the same evaluator and the disposition comes back byte for byte.

It is not enough to *defend* one to a third party who does not trust whoever ran it. Nothing in a line is signed, chained, sequenced or witnessed, and its time is the operator's clock. An audit of the released tools on 2026-10-01 found:

- an edited, backdated or deleted line passed `packs verify`, `packs validate` and the gateway's `verify`;
- Runner's `verify-run` passed a backdated run, a rewritten tool digest, and a run spliced under another's id with other inputs and another outcome;
- the gateway's decision policy (its ADR-0011) accepts any well-shaped file in its records directory, so a record written by hand satisfies even its strictest policy (gateway #195).

One thing already exists. A gateway action receipt (receipt version 3) commits, under the gateway's signature and in its chain, to `decision.recordDigest`, the SHA-256 of the decision record the write relied on. For a decision that was acted on through the gateway, a witness already holds the record's bytes as they were at the moment of the write. For every other decision there is none, and the record itself proves nothing about who wrote it.

This ADR proposes how a record becomes defensible: against whom, by what means, in what order, and what each step does and does not establish. It is a design for the maintainer to accept, amend or reject before any code.

## Who the record must hold up against

| | Adversary | Example | In scope |
| --- | --- | --- | --- |
| T1 | A writer who cannot read the operator's keys | an agent limited to MCP tools; a storage layer; a co-tenant process | yes: Stage 2 |
| T2 | The operator, after the fact | rewriting, backdating, deleting or splicing history to defend a past decision | yes: Stages 1 and 3 |
| T3 | The operator at decision time | supplying false facts; running many evaluations and recording only the one it likes | no, except where noted |

T3 is out of scope by nature: a record of a decision cannot make its inputs true. The facts are the subject of input acquisition (gateway receipts; Runner's mapping v2), not of this ADR. Selection among evaluations is discussed under "What stays open".

## Decision drivers

- Detect, after the fact, an edited, deleted, inserted, reordered or backdated record.
- Keep what the operator can produce alone apart from what a party the operator does not control attests. Say which is which on every record and in every check.
- Nothing new is paid by a project that does not ask for it. No step needs a network or a key unless it is configured.
- Reuse the gateway's signed, chained receipts where a witness is needed, rather than invent a second receipt format.
- Each layer states what it establishes and what it does not, as ADR-0043 does for the executable digest.
- Records written before this change stay valid and readable. Each step is additive to `recordVersion` `"1"`, as `tool` was.

## Considered options

- **A. Chain the trail.** Each record carries `sequence`, its position in the trail from 1, and `previous`, the SHA-256 of the canonical bytes of the record before it (null for the first). A new `jpack audit verify` walks the trail and reports the first break.
  - Detects edits, deletions, insertions and reordering, given the latest record (the *head*) from somewhere the operator did not rewrite.
  - On its own, the operator can rewrite the whole chain consistently: it defends against T2 only once the head is held elsewhere (C).
- **B. Sign each record with a runtime key.** An Ed25519 key, configured by path outside the project and never inside it. The signature covers the record's canonical form, `previous` included, and the public key is published to whoever checks.
  - Proves a record came from a holder of the key, which defends against T1.
  - Does nothing against the operator, who holds the key.
- **C. An independent witness of the head.** A party the operator does not control attests that a given head existed by a given time. The candidates:
  - **C1. The gateway as witness.** A new gateway endpoint takes a record's digest, or a head, and returns a signed receipt in the gateway's chain, of a new kind beside `acquisition` and `action`. Witnessing is a gateway's job, and its receipts are already verifiable offline. It is independent only when the gateway is operated by another party: Desk's local gateway generates its key in the operator's own store, so it witnesses nothing against the operator.
  - **C2. An RFC 3161 time-stamping authority.** A standard, widely run, needs only a network request and the authority's certificate to check. It attests existence by a time, not completeness between stamps.
  - **C3. A public transparency log** (for example Sigstore's Rekor). Strong and public, but it publishes digests of every decision to a third party, which some deployments must not do.
  - **C4. Hand the head to the counterparty**, the person the decision was about, or an auditor, at decision time. Simple, and the strongest for that one party, but it depends on delivery that nothing here performs.
- **D. Do nothing; state the limit.** The documents already say the record is unsigned (gateway #195; the issues filed on 2026-10-01). This leaves "defend" unsupported.

## Decision outcome (proposed)

Proposed: **A as the base, B opt-in, and C1 as the native witness with C2 as the fallback**, in three stages. Each stage is useful alone, and each later stage depends on the one before.

### Stage 1: a chained trail (runtime and Runner)

- Every record a project's trail appends carries `sequence` and `previous`. The writer takes an exclusive lock on the trail file, reads its last record, and appends under the lock. Today the writer opens the file in append mode and takes no lock, so two concurrent `jpack` processes would fork the chain.
- `jpack audit verify [--trail path] [--head digest]`: a new command. It walks the trail, checks every `previous` and `sequence`, and reports the first break by line. Given `--head`, it also checks that the trail ends there. Its exit code fails on a break, unlike the gateway's `verify`, whose exit-0-on-failure is a recorded trap.
- Runner's `verify-run` checks that the retained audit record's `previous` names the record before it in the run's attempt trail. Its report says whether the chain was checked.
- On by default where a project already keeps a trail? This is a question for the maintainer (below). Chaining costs one read of the last line per write, and makes a record's bytes depend on the record before it.

What it establishes: given a head the operator did not rewrite, the trail up to that head is the one that was written. What it does not: anything, against the operator, without that head held elsewhere.

### Stage 2: signed records (runtime, gateway, Runner)

- A project may configure `audit.signingKey`, a path to an Ed25519 seed outside the project, readable by the runtime's user only. Every record then carries `signature`, over the record's canonical form without that member, in the gateway's convention for receipts. `jpack audit verify --public-key` checks every signature.
- The gateway's decision policy gains `requireSignedRecord` with a list of trusted runtime public keys. A record without a valid signature from one of them is refused at a new step, `policy-signature`. This closes gateway #195 for T1.
- Runner's `verify-run` checks the signature when the release names a runtime key.

What it establishes: a record came from a holder of the key, so an agent that cannot read the key cannot forge one. What it does not: anything against the operator. In Desk, the agent and the runtime usually run as the same OS user, so the key binds an agent only if the agent cannot read the file (MCP-only tools, a separate user, a sandbox).

### Stage 3: an independent witness (gateway, runtime, Desk)

- **C1.** The gateway gains a witness endpoint and a receipt kind for it. The runtime, when configured with a witness gateway, submits each new head (per record, or per run for a graph run) and stores the witness receipt beside the trail. `jpack audit verify --witness` checks that every witnessed head is on the chain and every receipt verifies under the witness's public key.
- **C2.** As the fallback where no independent gateway exists: an RFC 3161 timestamp over the head, at a configured interval or per record, stored beside the trail.
- **Desk** says which of these an installation has. Desk's own local gateway cannot be the witness, since its key is the operator's. Desk can be pointed at a witness, or at a time-stamping authority.

What it establishes: the trail up to each witnessed head existed by the witness's time, and has not been changed since. Deleting or rewriting a record before the last witnessed head is detectable by anyone holding the witness's public key. What it does not: completeness after the last witnessed head; anything about decisions never written to the trail; the truth of the facts.

## Consequences

- Good, because the three ways a record was found indefensible on 2026-10-01 each meet a layer that detects them: edits, deletions and splices (A, given a held head), hand-written records (B), and backdating (C).
- Good, because the gateway's existing action receipts become the first witness without new machinery. An acted-on decision's `decision.recordDigest` is already signed and chained, and with A it pins the whole trail up to that record, not just one line.
- Good, because each layer is opt-in and additive. A project that keeps no trail pays nothing, and old records stay valid.
- Bad, because a chained trail turns a lost or corrupted line into a permanent break. Repair has to be an explicit, recorded event (a "break" record that names what was lost), never a silent rewrite.
- Bad, because concurrent writers now contend for a lock, and a crash between taking the lock and appending must leave the trail readable.
- Bad, because a signing key is a secret the runtime must read, and its custody (path, permissions, rotation, revocation) becomes part of operating a project.
- Bad, because a witness needs a party to operate it, and C2 needs a network request per head or per interval.
- Revisit when: a deployment needs records hidden from the witness (C1 and C2 see only digests, but a digest of a small facts document can be guessed); a decision must be defended without the trail; or the spec takes up record provenance (RFC 0003's line).

## What stays open

- **Selection (T3).** An operator can run many rehearsals and record one deciding run. Rehearsals append nothing by design (ADR-0028), and that stays. One option is to have the trail count rehearsals and refusals, without their inputs, so that a run of fifty rehearsals before one recorded decision is at least visible. This needs its own ADR: it changes what a rehearsal is.
- **The facts.** A defended record still holds the facts the caller supplied. Defending the facts is input acquisition's job: gateway receipts, and Runner's mapping v2, which verifies them. A record citing receipts the gateway can verify (ADR-0033) is the bridge, and Stage 1 makes those citations part of the chained bytes.
- **The evaluator.** The executable digest stays the program's account of itself (ADR-0043). Only re-execution, as `verify-run --runtime` does, checks it.

## Questions for the maintainer

1. Accept the three-stage plan, or change the order, or stop after Stage 1 or Stage 2?
2. Should Stage 1 be on by default in a project that keeps a trail, or opt-in? On by default is the defensible choice and costs a lock per write; opt-in keeps today's behaviour byte for byte.
3. Is C1 or C2 the first witness? C1 reuses the gateway but needs a gateway the operator does not run; C2 works anywhere with a network.
4. Should the signature, the witness receipts and the chain members wait for the specification to define a portable decision-record format, or stay this runtime's convention until a second implementation needs them?

On acceptance, each stage becomes issues in the repositories it touches: runtime (writer, `audit verify`, keys), gateway (`requireSignedRecord`, the witness endpoint and its receipt kind), Runner (`verify-run`), and Desk (key custody and witness settings).

## More information

- The audit trail: ADR-0018. Citations on a record: ADR-0033. The executable digest: ADR-0043. The reviewed set in the record: ADR-0044. Rehearsals append nothing: ADR-0028.
- The gateway: its SPEC.md §1.2a (receipt version 3, `decision.recordDigest`), §5a.2 (verify's exit code), and ADR-0011 (holding a write to its decision); gateway issue #195.
- Runner: `docs/MAPPING-V2.md` (offline verification) and runner issue #24 (asserted and derived inputs).
