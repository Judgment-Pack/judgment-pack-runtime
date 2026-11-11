---
status: accepted
date: 2026-11-10
deciders: maintainer
---

# Say at decision time why a record is unsigned, and let a project require signing

## Context and problem statement

[ADR-0047](0047-make-a-decision-record-defensible.md) §2b lets a project name a signing key, and
the guide makes an unsigned record pending, never failed: a key that is refused, a key that is not
the key in force, and a signature that cannot be written all leave the record unsigned and the
decision recorded. `packs validate` says why, as the `audit-signing-key` check. The deciding
surfaces say nothing (#354). `Project.AuditWriter` drops the load error, and the writer drops the
signing step's error, so a caller that decides and never runs `packs validate`, such as an agent
limited to MCP tools, learns of it only when someone verifies the trail. A project that wants every
decision signed has no way to say so.

Material-decision impact: public-surface, documented-claim, security.

## Decision drivers

- The caller learns, when it decides, what `packs validate` would say, in the same words.
- A project that does not ask sees no other change: the disposition, the record's bytes and the exit
  code are what they were.
- A project that wants signed records can refuse an unsigned one, opt-in, as `requireReviewed`
  ([ADR-0044](0044-say-and-require-the-reviewed-set.md)) and `requireComparableFacts`
  ([ADR-0046](0046-record-type-notes-and-let-a-project-refuse-incomparable-facts.md)) do.
- A declared rehearsal records nothing ([ADR-0028](0028-declare-an-evaluation-a-rehearsal.md),
  [ADR-0041](0041-declare-a-graph-evaluation-a-rehearsal.md)), so there is nothing to say of its
  record and nothing to refuse.

## Considered options

- Leave it to `packs validate`.
- Fail every decision whose key will not sign. That breaks every project that relies on pending,
  never failed: one where no key signs (Windows), and one that inherits `JPACK_SIGNING_KEY` without
  having asked for signing.
- Report only the signing step's own error after the write. A refusal at that point comes after the
  record is written, so it cannot keep the record out of the trail, and a caller learns nothing until
  then.
- Read the key before the run evaluates, by `packs validate`'s rules, and check again at the write,
  under the trail's lock: warn by default, and refuse when the project asks.

## Decision outcome

Chosen option: "read the key before the run evaluates and check again at the write, warn by
default, refuse on request", because it gives the caller `packs validate`'s reason at decision time
without changing a decision for a project that did not ask, the refusal can come before anything is
evaluated or recorded, and what changes between the read and the write is said or refused too.

### 1. The warning

`experimental evaluate`, `experimental graph evaluate` and the MCP `experimental_evaluate` tool take
their writer from `Project.DecisionWriter`. It loads the named key once, by the rules of the
`audit-signing-key` check, gives the writer the key whenever it loads, as `AuditWriter` does, and
returns the check's reason when the key will not sign: the key is refused, it is not the key in force
in the sidecar, or the sidecar cannot be read to tell. The check and the warning are built from one
value (`project.Unsigned`), so their text is the same bytes.

A run that records, and only one, reports the reason as one diagnostic after its record is written:
code `JPS-AUDIT-UNSIGNED`, provisional, layer `audit`, severity `warning`, empty `instancePath`,
the check's detail as its message, or what the write found (below). A graph run reports it once,
for all its records. It is carried in a new `diagnostics` member of the evaluation and graph
evaluation payloads, omitted when empty; the member is additive, so `outputVersion` stays `"2"`.
Without `--format json` the CLI prints `warning: <message>` on standard error. Over MCP it is in the
structured content and the text. The disposition, every other payload member, the record and the
exit code are unchanged.

The warning is said of what was written (`Project.UnsignedAfter`). The writer keeps why its append
left its records unsigned although it holds a key (`audit.Writer.SigningFailure`): its key was no
longer the key in force when it wrote, as when another process wrote a rotation after the run read
the key; the signature could not be written to the sidecar; or no lock can be taken on the trail, so
the writer appended without one, unchained, and signed nothing. Each is reported though nothing was
found before the run evaluated, with these messages, <keyId> being the key's id:

- "Key <keyId> is not the key in force in the signature sidecar, so it signs nothing and records
  are written unsigned: rotate to it from the key in force with jpack audit key rotate, or name the
  key in force." (the reason given before evaluating, when that already found it);
- "The signature sidecar could not be written, so key <keyId> signed nothing and the records
  written are unsigned.";
- "No lock can be taken on the audit trail here, so records are written unchained and unsigned."

A record the write signed reports nothing, and a writer that holds no key reports the reason given
before evaluating, unchanged.

A reason that names the key's path names at most its first 512 bytes, cut on a character boundary
and followed by "…". `packs validate`'s detail is the same value, so it is cut the same way, and a
path of 512 bytes or fewer is named whole, as before.

Nothing is reported for a rehearsal, a project that names no key, a project with no audit member, or
a trail that is not chained, which no key signs (`packs validate` reports the check skipped).

### 2. `requireSigning`

configVersion `"8"` adds `audit.requireSigning`, a boolean. The schema refuses it under an earlier
version and beside `"chain": false`. When true, the deciding surfaces call `Project.RequireSigning`
in a run that is not a declared rehearsal, after the reviewed-set checks and before anything is
evaluated. It refuses (`JPS-AUDIT-SIGNING-REQUIRED`, exit 1, the operational envelope, no
disposition, nothing recorded for any node or the composite):

- every run while no key is named, by `signingKey` or `JPACK_SIGNING_KEY`;
- every run for which §1 gives a reason, with that reason.

The message follows the reviewed-set refusals: "This evaluation was refused because <config>
requires every decision's record to be signed, and <what is wrong>. <the fix>, or declare the run a
rehearsal." A declared rehearsal is not refused.

At the write, `DecisionWriter` gives such a project a writer that requires signed records
(`audit.Writer.RequireSigned`). Under the trail's lock, before anything is appended, it refuses the
append when no lock can be taken on the trail, when its key is not the key in force in the sidecar as
it stands under that lock, or when the sidecar cannot be read to tell. The run then fails through the
existing audit-failure path (`JPS-AUDIT-WRITE`, exit 4, no disposition), with nothing appended for
any node or the composite, since a graph run appends its records in one write. An append whose
signature then cannot be written to the sidecar has already written its records, and they stay; the
run fails the same way, and its message says the record was written unsigned. The messages:

- "Audit record could not be written: the project requires every record to be signed, and the
  signing key is not the key in force in the signature sidecar, so the record would be unsigned."
- "Audit record could not be written: the project requires every record to be signed, and no lock
  can be taken on the audit trail here, so no record would be chained or signed."
- "Audit record could not be written: the project requires every record to be signed, and the
  signature sidecar could not be read to see whether the signing key is the key in force."
- "Audit record was written unsigned: the project requires every record to be signed, and the
  record's signature could not be written to the signature sidecar."

It binds as `requireReviewed` does (ADR-0044 §5): a caller that neither chooses the configuration a
run reads nor can edit it. Whoever can edit `jpack.json` can turn it off; in a project with a
reviewed-set lock, which pins the configuration's digest, that edit is an amendment, and by-id runs
are refused until the project locks again.

### 3. What this does not cover

- Under `requireSigning`, a record whose signature cannot be written stays in the trail, unsigned:
  the trail is append-only, and the record was written before its signature could be. The run fails
  and says so, but the record is not withdrawn, and a caller that retries records the decision
  again.
- On a file system where no lock can be taken, a refused run in a project whose trail did not yet
  exist leaves the trail's file created and empty, because the file is opened before the lock is
  tried.
- Without `requireSigning`, each of these is a warning, and the records are written unsigned, as
  before.
- A run is told only of its own records. `jpack audit verify --require-signed-through` stays the
  reader's check that records were signed.

This narrows the pending, never failed rule of the documentation on ADR-0047 §2b for a project that
sets `requireSigning`, and extends ADR-0047's configVersion value list and schema `$id` with `"8"`.

### Consequences

- Good, because a caller that only decides learns that its record is unsigned, and why, from the
  run that wrote it.
- Good, because a project that needs signed records can make an unsigned decision a refusal without
  any change for a project that does not.
- Bad, because the warning's reason names the key's path when the path is what is refused, as
  `packs validate` does, so the path now reaches an MCP client. The expected-key reasons name neither
  the path nor a key.
- Bad, because under `requireSigning` whoever can write the sidecar can make every decision refused,
  as a forged rotation stops the named key signing. Without it, the same write leaves records
  unsigned.
- Bad, because under `requireSigning` a sidecar that cannot be written fails a run whose record is
  in the trail.
- Revisit if a record whose signature cannot be written under `requireSigning` should be kept out
  of the trail rather than reported.

## More information

- Issue #354.
- `internal/project/signing.go` (`Unsigned`, `DecisionWriter`, `UnsignedAfter`, `RequireSigning`).
- `internal/audit/audit.go` (`Writer.RequireSigned`, `Writer.SigningFailure`).
- [docs/building-with-packs.md](../building-with-packs.md), the signing section.
