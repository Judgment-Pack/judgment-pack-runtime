---
status: proposed
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
- Report the signing step's own error after the write. The writer would have to return it, and a
  refusal at that point comes after the record is written, so it cannot keep the record out of the
  trail.
- Read the key before the run evaluates, by `packs validate`'s rules: warn by default, and refuse
  when the project asks.

## Decision outcome

Chosen option: "read the key before the run evaluates, warn by default, refuse on request", because
it gives the caller `packs validate`'s reason at decision time without changing a decision for a
project that did not ask, and the refusal can come before anything is recorded.

### 1. The warning

`experimental evaluate`, `experimental graph evaluate` and the MCP `experimental_evaluate` tool take
their writer from `Project.DecisionWriter`. It loads the named key once, by the rules of the
`audit-signing-key` check, gives the writer the key whenever it loads, as `AuditWriter` does, and
returns the check's reason when the key will not sign: the key is refused, it is not the key in force
in the sidecar, or the sidecar cannot be read to tell. The check and the warning are built from one
value (`project.Unsigned`), so their text is the same bytes.

A run that records, and only one, reports the reason as one diagnostic after its record is written:
code `JPS-AUDIT-UNSIGNED`, provisional, layer `audit`, severity `warning`, empty `instancePath`,
the check's detail as its message. A graph run reports it once, for all its records. It is carried
in a new `diagnostics` member of the evaluation and graph evaluation payloads, omitted when empty;
the member is additive, so `outputVersion` stays `"2"`. Without `--format json` the CLI prints
`warning: <message>` on standard error. Over MCP it is in the structured content and the text. The
disposition, every other payload member, the record and the exit code are unchanged.

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

It binds as `requireReviewed` does (ADR-0044 §5): a caller that neither chooses the configuration a
run reads nor can edit it. Whoever can edit `jpack.json` can turn it off; in a project with a
reviewed-set lock, which pins the configuration's digest, that edit is an amendment, and by-id runs
are refused until the project locks again.

### 3. What this does not cover

Both read the key before the run evaluates. What changes after that read is not seen: a rotation
another process writes before the record, a signature that cannot be written, and a trail whose
file system offers no lock, where the writer appends without one and signs nothing, all still leave
a record unsigned with no warning, and `requireSigning` does not refuse them. `jpack audit verify
--require-signed-through` stays the reader's check that records were signed.

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
- Revisit when the writer returns the signing step's error, so a failure after the read can be
  reported too.

## More information

- Issue #354.
- `internal/project/signing.go` (`Unsigned`, `DecisionWriter`, `RequireSigning`).
- [docs/building-with-packs.md](../building-with-packs.md), the signing section.
