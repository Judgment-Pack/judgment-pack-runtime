---
status: accepted
date: 2026-09-30
deciders: maintainer
---

# Say in the payload whether the reviewed set was applied, and let a project require it

## Context and problem statement

[ADR-0019](0019-reviewed-set-lock.md) pinned a project's reviewed set in a lock. A pack named by
decision id is declared law and is held to it; a pack named by path, or passed as text over MCP, is a
draft, "never refused for being unlocked", and the audit record says `"reviewed": false`. The payload
says nothing. Measured with jpack 0.23.1 after `packs lock` and one edit to a declared pack
(issue #180): by id the run is refused; by path it is evaluated with exit 0 and only the audit file
says it was a draft; declared a rehearsal it is evaluated and says so. A caller that receives a
disposition has to read the project's trail to learn what the runtime already knew, and a project
with no audit directory has nowhere to learn it at all.

The issue also asks for a way to make the deciding surfaces refuse a draft outright. `SECURITY.md`
states that the lock "is evidence, not a control", because whatever can edit a pack can run
`packs lock` again. That remains true of a caller who can edit the project. It is not true of a
caller who cannot: an agent limited to the MCP tools can pass any pack as text, and nothing in a
locked project stops it deciding on one.

## Decision drivers

- A caller should learn from the payload what the record says, in the same words.
- One meaning of "reviewed" across the record and the payload, and across the surfaces.
- The author's loop stays open: drafting and trying a pack must never need a lock or a declaration
  beyond the one it already has, `--rehearsal`.
- A refusal is honest about whom it binds.

## Considered options

- **A. Payload member only.** The payload carries `reviewed`, and nothing is ever refused for being
  a draft.
- **B. Payload member, and an opt-in configuration member that refuses drafts.**
- **C. Compare a draft's bytes with the lock**, and call a path-named pack whose bytes match a locked
  pack reviewed. It changes the meaning of the record's `reviewed`, which ADR-0019 settled as "named
  as declared law and matched", and a pack passed as text has no decision id to compare against
  anything but every locked digest.

## Decision outcome

Chosen option: **B**.

1. **The payload member.** The evaluation payload of `experimental evaluate` and
   `experimental_evaluate`, and the composite of `experimental graph evaluate`, carry `reviewed`
   exactly as the audit record does: present when the project declares a lock and the run was not a
   declared rehearsal; `true` when every document the run applied was named as declared law and
   matched the lock; `false` when any was a draft. When it is `true`, `reviewedSet` names the lock's
   revision in the record's shape (`lockDigest`, `lockVersion`, `configDigest`), now one type shared
   by both. A project with no lock, and a rehearsal, which consults none, carry neither. The human
   output adds one line saying which. Both members are additive output under VERSIONING.md's MINOR
   rule, and `outputVersion` stays `"2"`.
2. **The configuration member.** `jpack.json` may set `"requireReviewed": true` under a new
   configVersion `"4"`; a configuration is a closed input, so the member moves the version, as
   `audit` moved it to `"3"` (ADR-0018). Absent or false, nothing changes.
3. **What it refuses.** With it set, the deciding surfaces refuse, after the existing lock checks and
   before any evaluation, every run that does not apply the reviewed set: a run that applies a draft
   (a pack named by path or passed as text, a graph document the configuration does not declare),
   and every run while the project has no lock. The refusal is `JPS-LOCK-REVIEW-REQUIRED`, exit 1,
   with no disposition and no record, and it names both ways forward: name the pack by its decision
   id (or declare the reviewed set), or declare the run a rehearsal.
4. **What it does not refuse.** A declared rehearsal is not a decision: it consults no reviewed set
   and records nothing, so it is not refused (ADR-0028, ADR-0041). The test surfaces are untouched.
5. **Whom it binds.** A caller who neither chooses which configuration a run reads nor can edit
   that configuration or its lock, such as an agent limited to the tools of an MCP server someone
   else launched. Whoever chooses the configuration (the `--config` argument, `JPACK_CONFIG`, the
   working directory, the server's launch) chooses whether the requirement applies: naming another
   configuration applies that one's rules, and naming a file that is not there means no project and
   no requirement, as it always has. So a caller who controls the CLI's arguments is not bound.
   Whoever can edit the configuration or the lock can turn it off; because the lock pins the
   configuration's digest, turning it off is drift that refuses every by-id run until the project
   locks again, so the edit is recorded rather than silent. `SECURITY.md` says so.
6. **Declared law whose bytes never arrived.** A pack named by decision id that is over the byte
   limit is not consulted against the lock, because there are no bytes to check; the evaluator
   refuses it at that limit. It is not a draft, and the requirement does not call it one: with a
   lock present it passes to that refusal, and with none it is refused for having no lock.

This is a **partial supersession**, annotated in the index without editing either body:
[ADR-0019](0019-reviewed-set-lock.md)'s determination that a draft is never refused for being
unlocked is narrowed for projects that set `requireReviewed`; and
[ADR-0018](0018-opt-in-evaluation-audit-trail.md)'s configVersion value list and schema `$id` are
extended by `"4"`, as ADR-0018 extended ADR-0017's.

### Consequences

- Good, because a caller learns from the payload which law decided, without reading the trail and in
  a project with no trail.
- Good, because a project that hands its tools to an agent can keep the agent to reviewed law, and
  the author's loop moves to `--rehearsal`, which already exists and already records nothing.
- Bad, because a project that sets the member and has no lock refuses every deciding run until it
  runs `packs lock`; the refusal says so.
- Bad, because the lock now has a mode in which it is a control, and a reader can over-read that. It
  binds only callers who neither choose the configuration nor can edit it or the lock, and the
  documents say so wherever they describe it.
- Revisit when the decision desk holds its own checkout (ADR-0019's first revisit condition): the
  member then protects a tree the deciding party does not write.

## More information

Issue #180. ADR-0018 (the audit trail and configVersion `"3"`), ADR-0019 (the lock), ADR-0028 and
ADR-0041 (rehearsals), VERSIONING.md (closed inputs move their version).
