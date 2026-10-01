---
status: proposed
date: 2026-10-01
deciders: maintainer
---

# Make a decision record defensible to someone who does not trust its operator

## Context and problem statement

The audit trail (ADR-0018) appends one JSON line per decision to `evaluations.jsonl`. A line holds:

- the pack's digest;
- the inputs;
- the evaluator's version, and its executable digest when it can be read (ADR-0043);
- whether the reviewed set was applied (ADR-0019; the payload says the same since ADR-0044);
- the citations the caller supplied, held to a shape and verified by nothing (ADR-0033);
- the disposition.

A graph run writes a node record per node and a composite record. That is enough to *reproduce* a decision: the same pack and inputs, given to the same evaluator, return the disposition byte for byte.

It is not enough to *defend* one to a third party who does not trust whoever ran it. Nothing in a line is signed, chained, sequenced or witnessed, and its `at` is the operator's clock. An audit of the released tools on 2026-10-01 found the following:

- **An edited, backdated or deleted line still passed.** `packs verify` and `packs validate` exited 0. `gateway verify` reported `{"ok":true,"findings":[]}`, where the line was not one a retained action receipt names.
- **Runner's `verify-run --runtime` reported `verified-disposition`, exit 0,** for a backdated run, a rewritten tool digest, and a run spliced under another's id with other inputs and another outcome. Those runs' inputs were asserted, not derived from receipts (runner #24).
- **The gateway's decision policy accepts a hand-written record.** It accepts any well-shaped file in its records directory, so a record written by hand satisfies its strictest policy (gateway #195).

One thing already exists. A gateway action receipt (receipt version 3) carries `decision.recordDigest`, the SHA-256 of the exact bytes of the decision record the write relied on, under the gateway's signature and in its chain. The gateway does not keep the record (its SPEC: decision records "are the runtime's own files"). So for a decision acted on through the gateway, a signed commitment to the record's bytes, as observed before the write, exists, but only as long as someone keeps those bytes.

This ADR proposes how a record becomes defensible: against whom, by what means, in what order, and what each part does and does not establish. It is a design for the maintainer to accept, amend or reject before any code. A cross-vendor review of its first draft found five HIGH and nine MEDIUM gaps; this version answers them, and the review record is on the pull request.

## Who the record must hold up against, and what is trusted

| | Adversary | Example | In scope |
| --- | --- | --- | --- |
| T1 | A writer who cannot use the operator's signing key | an agent limited to MCP tools; a storage layer; a co-tenant process | yes: signatures |
| T2 | The operator, after the fact | rewriting, truncating, backdating or splicing history, or presenting an older complete copy | yes: chain and external checkpoints |
| T3 | The operator at decision time | supplying false facts; evaluating many times and recording one | no, except where noted |

**What a verifier must hold independently of the operator.** None of these can come from the evidence being checked:

- the trail identities it expects;
- the protection each must have (chained, signed, checkpointed);
- the public keys it trusts, obtained out of band;
- at least one checkpoint it obtained itself, or from a witness it trusts.

**What is assumed, not defended.**

- A witness or time-stamping authority does not collude with the operator.
- Its key is not compromised.
- Its clock is as good as its policy says.

**Key compromise.** A stolen signing key lets its holder sign new records, including ones that claim old times. External checkpoints made before the compromise still hold the history they cover; nothing does after them. Rotation and revocation are part of Stage 2.

## Decision drivers

- Detect, after the fact, an edited, deleted, inserted or reordered record, and an operator presenting an older or truncated copy, given a checkpoint the operator did not supply.
- Never canonicalize or rewrite a record. A record keeps facts as their caller spelled them, numbers included, and existing records and the action receipts naming their bytes must stay valid.
- Keep what the operator can produce alone apart from what a party it does not control attests. Say which is which in every check.
- Nothing new is paid by a project that does not ask for it. No step needs a network or a key unless configured.
- Each capability states what it establishes and what it does not.

## Considered options

- **A. Chain the trail over exact bytes.** New records carry `trail`, `sequence` and `previous`, where `previous` is the SHA-256 of the exact bytes of the line before, without its newline. No canonical form is needed.
- **A′. A detached commitment log beside the trail**, chaining digests of the exact record lines. It leaves record lines untouched, but an action receipt's `decision.recordDigest` then pins one record and not its history, since the record carries no `previous`. It loses the existing receipts as anchors.
- **B. Signatures.** Either inside each record, which needs a canonical form that existing records cannot meet (the gateway's canonical JSON admits integers only; the runtime's JCS covers dispositions only), or detached in a sidecar over the exact bytes.
- **C. Checkpoints held outside the operator.** A checkpoint is a trail identity, a sequence, and the digest of the exact bytes of the record at that sequence. The candidates:
  - **C1.** A gateway run by another party issues and serves checkpoints. It needs a new receipt version or extension: version 3 admits only `acquisition` and `action`.
  - **C2.** An RFC 3161 time-stamping authority stamps a checkpoint's digest.
  - **C3.** A public transparency log.
  - **C4.** The checkpoint is handed to the counterparty or an auditor when the decision is made.
- **D. Do nothing; state the limit.**

## Decision outcome (proposed)

Proposed: **A, detached signatures (B), and external checkpoints (C).** C4 and C2 come first; C1 follows when a gateway run by another party exists.

These are capabilities with real dependencies, not a ladder:
- the chain is the base for any statement about history;
- checkpoints need the chain;
- signatures need neither, and answer T1 alone.

### 1. A chained trail over exact bytes (runtime)

**The members.** A chaining writer adds three members to each new record. Each is additive to `recordVersion` `"1"`, as `tool` was; Runner's and the gateway's readers already tolerate extra members.
- `trail`: an identity of 128 random bits in hex, fixed when chaining starts.
- `sequence`: the line's position in the trail file, from 1.
- `previous`: the SHA-256 of the exact bytes of the preceding line, without its newline.

**Existing records.** Nothing is rewritten. When chaining starts on a trail that already has lines, the first chained record carries `sequence` = the number of existing lines + 1, and `previous` = the SHA-256 of the exact bytes of the whole existing prefix. That one commitment covers every legacy line as a block. A new trail's first record has `previous` = the SHA-256 of the empty string. A verifier reports a trail as:
- legacy prefix (committed as a block);
- chained;
- signed, where signatures exist;
- checkpointed up to the last checkpoint it holds.

**The writer.** It takes an exclusive, cooperative lock on the trail for the whole step:
- read the last line;
- refuse to go on if that line is incomplete (no trailing newline): the trail needs repair, below;
- assign every sequence of a batch, whether one record or a graph run's node records and composite;
- write;
- sync the file, and the directory when the file was created;
- release.

A graph batch keeps ADR-0018's commit-marker semantics: the composite is the batch's last line, and a batch without its composite is incomplete. The lock serializes cooperating writers and protects against no one who ignores it. Platforms without the lock fall back to refusing to chain.

**Repair.** A torn or corrupt line is never repaired in place. `jpack audit repair` starts a new segment:
- a `discontinuity` record (chained to the last intact line, and signed if signing is configured) names the digest of the bytes after that line, and why;
- the damaged bytes are kept beside the trail.

`audit verify` then reports each intact segment and the discontinuity. It never reports the whole history as intact. A checkpoint made before the break still verifies the segment it covers.

**The verifier.** `jpack audit verify --trail <file>` checks every `previous`, `sequence` and `trail` from the first chained record on.
- With `--expect <checkpoint>`, held independently, it also checks that the trail identity matches, that the trail reaches that sequence, and that the record there has the checkpoint's digest. A trail that is shorter, has another identity, or has another record there fails.
- Without `--expect` it reports "integrity of one supplied chain", and says it cannot tell whether this is the project's complete trail.
- `jpack audit checkpoint` prints the current checkpoint for handing to someone else.
- Its exit code fails on any failure, unlike `gateway verify`, whose verdict is in its JSON (gateway SPEC §5a.2).

**The existing action receipts.** A receipt's `decision.recordDigest` is a signed commitment to a chained record's exact bytes. Because that record's `previous` commits to the line before it, the receipt pins the trail prefix ending at that record. This holds only when:
- the exact bytes are kept;
- the verifier checks receipt to record, then record back along the chain;
- the receipt's signing key is trusted independently.

It covers nothing after that record: a graph node's receipt does not cover the later nodes or the composite. A gateway the operator runs (Desk's local gateway generates its key in the operator's own store) is no witness against T2.

What this establishes: given an independently held checkpoint, the trail up to it is the one that existed when the checkpoint was made. What it does not establish:
- that the trail is complete after the last checkpoint;
- that a record's `at` is true;
- anything about decisions never written to the trail.

### 2a. External checkpoints (runtime, Desk)

**C4 first.** Desk, or the runtime on request, hands the current checkpoint to whoever should hold it: the counterparty, an auditor, or a store the operator does not control. The verifier uses it with `--expect`. This needs no new service. It is as strong as the holder's independence and retention.

**C2 as the configured option.** The runtime asks an RFC 3161 time-stamping authority to stamp a checkpoint's digest, at a configured interval or per record, and keeps the token beside the trail. Verifying it requires:
- the authority's certificate chain, under a trusted root and the authority's policy;
- the status evidence for the certificate as of the stamp time.

A token establishes that the checkpoint existed by the stamp's time. It does not establish when the decision was made. `at` stays the operator's word; the verifier reports the lag between a record's `at` and the first time a checkpoint covering it was stamped.

**Failure is the maintainer's choice** (question 4):
- **Fail closed:** a decision is refused when its checkpoint cannot be stamped.
- **Pending:** the decision is recorded and appended first, then stamped. A record not yet covered is reported as "unwitnessed". `audit verify --require-checkpoint-through <sequence>` fails while any record up to that point is uncovered.

Retries are idempotent by the checkpoint's digest. A lost reply is recovered by asking again with the same digest.

### 2b. Detached signatures (runtime, gateway, Runner)

**The key.** A project may configure a signing key: an Ed25519 seed outside the project, readable by the runtime's user only.

**The sidecar.** For each chained record, the runtime appends a line to a sidecar file. The line carries the trail, the sequence, the record's exact-bytes digest, and a signature over a runtime-specific domain prefix (`judgment-pack-runtime/record-signature/1:`) followed by the canonical form of `{trail, sequence, record}`. That canonical form is well defined: the object holds only strings and small integers. The record line itself is untouched.

**Keys over time.** Rotation is a signed `key-rotation` line naming the next key, made with the old key. Revocation is out of band, and the verifier's trust configuration names the revoked keys and when.

**The gateway's policy.** It gains `requireSignedRecord` with the trusted runtime keys. The policy object is closed today, so this needs a new engine configuration version. A record without a valid sidecar signature from one of the keys is refused at a new step. This closes gateway #195 for T1.

**Runner.** Runner's `verify-run` checks the signature when the release names a runtime key.

What this establishes: a holder of the key produced the record. What it does not establish:
- **anything against the operator,** who holds the key;
- **anything once the key is stolen.**

In Desk the agent and the runtime usually run as the same OS user, so a key binds an agent only if the agent cannot read the file: MCP-only tools, a separate user, a sandbox.

### 3. A gateway witness (gateway; later)

A gateway that another party operates issues signed checkpoints over a trail's head and serves the latest one it holds for a trail identity, so a verifier can fetch the latest without trusting the operator. This needs:
- a new gateway receipt version, or a negotiated extension, for checkpoints;
- a retrieval endpoint;
- a design of its own.

It is the strongest answer to rollback, and the only one that needs a third party to run a service.

### Runner (open: question 5)

Runner gives each attempt its own audit directory, never reused, and keeps one record per run (`internal/runner/runtime.go`). Each attempt's trail would be a one-record chain, which proves nothing about Runner's history. The proposal is a chain of Runner's own, over its retained runs: an installation-level log in which each entry binds a run id to its audit record's exact-bytes digest, chained the same way. `verify-run` would export the run's entry and a checkpoint. This is Runner's design to settle; the runtime's chain does not reach it.

## Consequences

- Good, because each way a record was found indefensible on 2026-10-01 meets a capability that detects it:
  - edits, deletions, splices and a presented older copy, by the chain given an external checkpoint;
  - hand-written records, by signatures;
  - backdating beyond the gap between `at` and the stamp, by C2.
- Good, because no record is ever rewritten or canonicalized. Existing records, and the action receipts naming their bytes, stay valid. Facts keep their spelling.
- Good, because the existing action receipts become anchors for the prefix before each acted-on record, once records are chained.
- Bad, because chaining makes a lost or torn line a permanent, visible break. Repair starts a new segment and never restores the old one.
- Bad, because writers contend for a lock, and a trail on a platform or file system without one cannot be chained.
- Bad, because the defence is only as good as the checkpoints someone outside the operator actually keeps.
- Bad, because a signing key is a secret, and its custody (path, permissions, rotation, revocation) becomes part of operating a project.
- Bad, because the configuration (a new configVersion for chaining, signing and checkpoints), the gateway's policy (a new engine version) and the witness (a new receipt version) are separate compatibility steps. None of them changes the record's version.
- Revisit when: a deployment must hide checkpoints' contents from the stamping authority; a decision must be defended without the trail; or the specification takes up record provenance.

## What stays open

- **Selection (T3).** An operator can run many rehearsals and record one deciding run. Rehearsals append nothing by design (ADR-0028). A count of rehearsals and refusals on the trail, without their inputs, would make that visible for activity through a cooperating runtime. It is operational visibility, not protection against an operator evaluating elsewhere or suppressing the count, and it needs its own ADR.
- **The facts.** A defended record still holds the facts its caller supplied. Runner's mapping v2 verifies how acquired facts are bound to signed receipts and derived from them, not that the sources are true. Asserted inputs remain assertions (runner #24).
- **The evaluator.** The executable digest stays the program's account of itself (ADR-0043). `verify-run --runtime` shows that the release's pinned executable, given the verified inputs, reproduces the retained disposition. It does not show which bytes produced the historical decision. Comparing the record's `tool.digest` with the release's would be a separate consistency check.
- **Privacy.** A checkpoint's digest covers a whole record, including a random `run` id, so it is not trivially guessable. But a digest gives no general confidentiality. C2 discloses digests to the authority; C3 publishes them. If hiding them is required, a secret salt kept with the verification evidence must be decided before deployment.

## Questions for the maintainer

1. Accept the design: an exact-bytes chain added to new records, detached signatures, and checkpoints held outside the operator?
2. Should chaining be on by default in a project that keeps a trail, or opt-in? On by default is the defensible choice, at the cost of a lock and a read per write; opt-in keeps today's behaviour byte for byte.
3. Which checkpoint comes first: C4 (hand the checkpoint to a holder), C2 (time-stamping authority), or both?
4. When a checkpoint cannot be made: fail closed, or record and report the decision as pending?
5. Should Runner keep an installation-level chain of its runs, as proposed?
6. Should these stay this runtime's convention until the specification defines a portable decision-record format, or go to the specification now?

On acceptance, each capability becomes issues in the repositories it touches:
- runtime: writer, lock, `audit verify` and `audit checkpoint`, repair, signing, stamping;
- gateway: `requireSignedRecord`, and later the witness;
- Runner: its chain and `verify-run`;
- Desk: key custody, checkpoint hand-over, stamping settings.

## More information

- Runtime: the audit trail, ADR-0018; the reviewed set on the record, ADR-0019 and ADR-0044; rehearsals append nothing, ADR-0028 and ADR-0041; citations, ADR-0033; the executable digest, ADR-0043; the record version rule, `VERSIONING.md`.
- Gateway: `SPEC.md` §1.1 (canonical JSON, integers only), §1.2a (receipt version 3, its kinds, `decision.recordDigest`), §4 (what a verifier hashes), §5a.2 (verify's exit code); its ADR-0011 (holding a write to its decision); gateway issue #195.
- Runner: `docs/MAPPING-V2.md` (offline verification and what it does not establish); runner issue #24.
- RFC 3161 (time-stamp protocol).
