---
status: accepted
date: 2026-10-05
deciders: maintainer
---

# Refusals and rehearsals stay outside the trail, and the trail says so

## Context and problem statement

[ADR-0047](0047-make-a-decision-record-defensible.md) left one item open under "Selection (T3)"
(0047:209). An operator can run many rehearsals and record one deciding run. A count of rehearsals
and refusals on the trail, without their inputs, "would make that visible for activity through a
cooperating runtime"; it is "operational visibility, not protection against an operator evaluating
elsewhere or suppressing the count, and it needs its own ADR". Issue #224 asks for that record:
should a trail carry a count, or a record, of refused and rehearsed evaluations, so that a reader
can tell "no decision was made" from "a decision was refused"?

What the runtime does at `main` (`3a5f031`, v0.27.0). Its line numbers are at that commit; Runner's,
Desk's and the gateway's are at the commits named under More information.

- **Only a completed evaluation leaves a line.** The kinds are `evaluation`, `graph-composite` and
  `discontinuity` (`internal/audit/audit.go:179-190`). None is a refusal or a failure. A refused
  evaluation "has no disposition at all under §8.4" and leaves no record (`audit.go:35-40`), and a
  refusal before the trail is opened writes nothing at all (`audit.go:63-65`). No member of a record
  speaks about any other run (`audit.go:289-311`).
- **Refusals happen before the trail is reached.** On the CLI, an unreadable or drifted reviewed-set
  lock and a `requireReviewed` refusal return before the evaluator (`internal/cli/app.go:309-324`);
  `requireComparableFacts` refuses inside the engine, after admission and before evaluation,
  rehearsal or not (`app.go:337-341`); a §8.4 evaluation failure returns before anything is written
  (`app.go:342-344`). The MCP tool does the same (`internal/mcp/tools.go:1120-1137`, `:1153-1155`).
  A graph run holds its node records and appends them with the composite in one write, so a node
  refused late leaves nothing for the nodes before it (`internal/graph/evaluate.go:373-378`,
  `:451-462`). An argument refusal precedes the configuration, so it never knows which trail it
  would belong to (`tools.go:1061-1066`).
- **Rehearsals and test runs write nothing.** A rehearsal is the caller's explicit declaration; it
  consults no reviewed set and appends no record (`app.go:302-306`, `:360-361`;
  `tools.go:1116-1119`, `:1171-1172`; ADR-0028, ADR-0041). `packs test`, `experimental graph test`,
  `experimental evaluate-corpus`, `experimental compare` and the MCP test tools write nothing, and
  `experimental_test_cases` reads no project files at all (`tools.go:45`).
- **A failed append withholds the disposition** and refuses the run with `JPS-AUDIT-WRITE`
  (`app.go:345-368`; `tools.go:1156-1179`). A write that fails after it started can leave the
  record's bytes in the file (guide `docs/building-with-packs.md:881-883`; ADR-0018 under "What that
  does *not* promise"), so the trail is not a ledger of attempts in either direction: a line can
  exist for a run whose caller was told it failed.
- **The guide says what is not recorded** (`docs/building-with-packs.md:767-778`), and the chain
  section says the chain "says nothing about decisions that were never written to the trail"
  (`:906-907`). `audit verify` says "nothing here says which decisions were never written to it"
  only when no checkpoint is held. With one, that sentence is replaced by one about lines removed
  from the end (`internal/audit/verify.go:84-85`, `:766-772`), so a checkpointed report is silent on
  the point.

So a chained, signed, checkpointed trail establishes nothing about how many evaluations were refused
at the gate. The 2026-10-01 product review named this ("refusals are never recorded").

What the callers already do:

- **Runner** gives each attempt its own audit directory, never reused, so each attempt's trail is
  one record (ADR-0047's Runner section, 0047:185-189; Runner `docs/MAPPING-V2.md:556-558`). It
  records a run whose Runtime invocation failed or was refused in its own store, with state `failed`
  and a problem (Runner `internal/runner/store.go:491-503`). Its installation chain gives a failed
  run no entry (`MAPPING-V2.md:607-608`), so that record is operational, not chained. A release's
  preview evaluation is a rehearsal (Runner `internal/runner/runtime.go:245`).
- **Desk** declares every evaluation it makes a rehearsal, the assistant's included (Desk
  `web/src/routes/GatesHelp.tsx:55`; `web/src/packs/test-workspace/TestsWorkspace.tsx:499`). The
  Tests workspace stores each run in its own test store, a failed one with its error
  (`TestsWorkspace.tsx:533-538`). Desk's jobs plan: "Runtime audit records describe completed
  decisions, not scheduling attempts or failed acquisitions. The runner needs its own operational
  ledger and links to decision records." (`docs/design/operational-jobs-plan.md:37`)
- **The MCP server's stock configuration keeps no trail.** The image runs in `/project` and reads a
  `jpack.json` only if one is mounted there or named by `JPACK_CONFIG`
  (`build/oci/Dockerfile:16-18`); the registry entry says a project that does not use the convention
  "still validates and evaluates" (`build/mcp-registry/server.json.tmpl:20`). Without a
  configuration that declares `audit`, there is no writer at all
  (`internal/project/project.go:1070-1073`; `audit.go:800-803`).

## Decision drivers

- **The trail holds decisions and only decisions.** "A trail whose lines were sometimes results and
  sometimes failures would not be a trail of what the project decided" (ADR-0018, "Errors are not
  records"); "both a missing decision and a phantom one corrupt it" (ADR-0028's first driver).
- **A refusal is not a decision.** A §8.4 refusal has no disposition. The project's own refusals,
  `requireReviewed` and `requireComparableFacts`, carry no §8.4 class and no disposition either
  (`tools.go:141`; guide `:817-818`).
- **Say whom a capability holds up against** (ADR-0047's table, 0047:34-38) and what it does and
  does not establish.
- **Nothing new is paid by a project that does not ask for it** (0047:60). Today no refusal at the
  gate or in the preflight takes the trail's lock or writes to the audit directory.
- **Count where an attempt's meaning is known.** The runtime sees one invocation at a time. Runner
  knows runs, attempts, triggers and idempotency keys; Desk knows its test runs.
- **Closed formats move their versions** (`VERSIONING.md:79-91`). The checkpoint is a closed input
  at `checkpointVersion` `"1"`.

## Considered options

- **A. Nothing changes, and the trail says so.** The guide and ADR-0047 each state, in one sentence,
  that refusals and rehearsals are deliberately outside the trail; that a chained, signed,
  checkpointed trail therefore establishes nothing about how many evaluations were refused or
  rehearsed; and that a caller that needs that count keeps it itself, as Runner does for Jobs.
- **B. A refusal counter beside the trail, not in it.** The runtime keeps a count of refusals (and
  perhaps rehearsals) since the last record, by code and without inputs, in a file beside the trail.
  The count is written in the same append as the first record after the refusals, and covered by the
  chain through a member of that record or of the checkpoint line. `audit verify` reports it as
  coverage, with test vectors.
- **C. Refusal records in the trail.** A new kind, written for each refused evaluation, chained,
  signed and checkpointed like any line.

## Decision outcome (proposed)

Proposed: **A now. B is revisited when a reader outside Runner needs the count.** C is rejected.

1. **The trail is a record of decisions.** That is the property ADR-0018 and ADR-0028 protect, and
   every reader of evaluation records selects lines by kind on that understanding
   (`audit.go:186-189`). A refusal is a run that did not decide. C would change what a line means; B
   would put a statement about other runs onto a decision's record.
2. **A count binds nothing against the operator either.** T3 is the operator at decision time, and
   the operator runs the runtime that would keep the count. Around it:
   - an evaluation in a project with no `audit` member, or in no project at all;
   - `experimental_test_cases`, which reads no project files and so has no trail to count on;
   - `packs test`, `experimental evaluate-corpus` and `experimental compare`, which write nothing;
   - `"chain": false`, which leaves the count uncovered, or removing the count's file before the
     next record;
   - another build, or another implementation.

   Even held by someone else, a checkpoint over B's member would establish only "this is the count
   this runtime wrote when this record was appended". That is what ADR-0047 §T3 already called it:
   operational visibility, not protection.
3. **A refusal at the gate leaves no disposition under §8.4.** There is nothing in it to bind, to
   replay or to cite: no outcome, and, for the project's own refusals, no §8.4 class.
4. **Runner already records attempts, and is the caller that knows what an attempt is.** B gives
   Runner nothing. Each attempt's directory is new, so a count written in it is never followed by a
   record that would cover it.
5. **The MCP path's stock configuration keeps no trail at all.** A counter beside a trail counts
   nothing where there is no trail. The case issue #224 names as open, the CLI and MCP paths "where
   nothing else records them", is mostly a case where nothing is recorded, by design or by
   configuration. A counter would not change that.
6. **B's two routes to coverage collapse into one.**
   - **The checkpoint route.** A new checkpoint member needs `checkpointVersion` `"2"`, which every
     reader of version 1 refuses (`internal/audit/checkpoint.go:38-51`). It would also break the
     rule that a checkpoint is a function of its record's exact bytes
     (`internal/result/audit.go:7-13`; guide `:967-975`). Two checkpoints for one record would then
     differ, which the guide calls proof of rewriting.
   - **The record route.** A member on the next record is all that is left. It puts the count under
     that record's digest, and so under any gateway receipt's `decision.recordDigest` for it.

### What this establishes, and what it does not

**Establishes:** nothing beyond what ADR-0047 and `audit verify` already establish about the lines
that are there. Those lines are what this runtime appends, read by the package's reader rule
(`audit.go:44-62`):

- completed, non-rehearsal evaluations;
- graph runs' composites;
- repairs' discontinuities.

**Does not establish:** how many evaluations were refused, rehearsed, or run on a test surface, in
this project or anywhere else. "Refused" covers:

- the §8.4 preflight;
- an unreadable or drifted reviewed-set lock, or `requireReviewed`;
- `requireComparableFacts`;
- a record that could not be appended.

In one sentence: **a chained, signed and checkpointed trail establishes nothing about how many
evaluations were refused or rehearsed, in this project or anywhere else, and its silence is not
evidence that none were.**

### The two sentences

**The guide.** A paragraph of its own in "Checking a trail, and handing over a checkpoint", after
the `--expect` bullet (`docs/building-with-packs.md:948`):

> A trail records decisions, not attempts: a rehearsal, a test run and an evaluation refused before
> it had a disposition write no line, so a chained, signed and checkpointed trail establishes
> nothing about how many evaluations were refused or rehearsed, and a caller that needs that count
> keeps it itself, as Runner keeps each Job's failed runs in its own store (ADR-0048).

**ADR-0047.** At the end of the "Selection (T3)" bullet (0047:209):

> Settled by [ADR-0048](0048-refusals-and-rehearsals-outside-the-trail.md): refusals and rehearsals
> stay outside the trail, so a chained, signed, checkpointed trail establishes nothing about how
> many evaluations were refused or rehearsed, and a caller that needs that count keeps it itself, as
> Runner does for Jobs.

`docs/adr/README.md` says an accepted record is never edited. The house's mark for a discharged
deferral is an annotation on the earlier record's index row, as on 0025's and 0035's. Commit
`3cede80` (#202) edited accepted ADR-0044 in place to correct an overstatement. So the second
sentence is proposed alongside an index-row annotation for 0047, "Selection (T3) settled by 0048";
question 4 asks how it lands.

Both sentences, and the annotation, are applied in the pull request that accepts this record. They
are documentation only. Question 4's answer, below, settles how the second lands: both.

### What the rejected options would have touched

**B, a counter beside the trail:**

- **Record kinds** (`audit.go:179-190`): none.
- **Record members** (`audit.go:289-311`): one additive member on the first record after the
  refusals, counts by code and no inputs. `recordVersion` stays `"1"` (`VERSIONING.md:75-78`).
- **The checkpoint line** (`checkpoint.go:38-51`): none by the record route. By the checkpoint
  route, `checkpointVersion` `"2"`, which every holder's file and every reader of version 1 refuses,
  Runner's `verify-run --expect` included (Runner `MAPPING-V2.md:574-581`).
- **The writing path:** every refusal after the configuration loads takes the trail's lock and
  writes the counter's file. That reverses "a refused run is never opened for" (ADR-0018;
  `audit.go:63-65`). A failed counter write needs a rule of its own. A lost file resets the count
  silently, and refusals after the last record are covered by nothing.
- **`audit verify`** (`internal/result/audit.go:26-66`): a coverage member for the counts and the
  sequence they reach, a finding for a malformed count, and a fixed sentence saying the count is the
  operator's runtime's word. Test vectors beside the guide's signature vectors.
- **Configuration:** a new `configVersion` `"7"` member to opt in (`VERSIONING.md:79-87`).
- **Surface statements:** the tool and argument text saying a rehearsal "writes nothing"
  (`tools.go:141`, `:160`) would no longer be true if rehearsals were counted.
- **Other repositories:** none required. The gateway's policy passes an extra member over, and
  Runner gains nothing.
- **A second implementation:** the record format is this runtime's convention (0047 decision 6,
  0047:223). Even so, a verifier of this runtime's trails would need the member's rule and vectors.

**C, refusal records in the trail:**

- **Record kinds:** a fourth kind. It is additive, as `discontinuity` was (`VERSIONING.md:88-90`).
- **Record members:** a shape of its own, as the discontinuity's is
  (`internal/audit/repair.go:45-55`), with no disposition, which an evaluation record always carries
  (`audit.go:310`). It either carries the refused inputs, recording facts the project asked to
  refuse, or carries none and replays nothing.
- **The checkpoint line:** no change, but every refusal becomes a checkpointable line.
- **The writing path:** as for B, and each refusal also pays the lock, a sync, a signature and,
  where configured, a stamp. A refusal whose own append fails needs a rule.
- **`audit verify`:** the new kind recognised, its shape checked, and findings for a malformed one.
- **Configuration:** as for B.
- **Surface statements:** as for B, and the guide's "It does not record refusals" (`:771-775`).
- **Other repositories:** every reader that counts lines as decisions must select by kind. The
  gateway already refuses any kind but `evaluation` as a decision (gateway
  `go/decision_policy.go:264-270`).
- **A second implementation:** a reader must pass over the kind. A writer must decide which refusals
  are written: argument refusals precede the configuration and cannot be (`tools.go:1061-1066`).

### Consequences

- Good, because nothing changes in the record, the checkpoint, `audit verify` or the configuration.
  Runner, the gateway, Desk and any second implementation have nothing to follow.
- Good, because the trail's silence is stated where a reader of the trail looks: the guide's section
  on checking a trail, and the record that left the question open.
- Bad, because on the CLI and MCP paths nothing counts refusals. An operator who hands
  `experimental_evaluate` to an agent and wants to see it probing the gate gets nothing from the
  runtime.
- Bad, because the selection risk (T3) stays exactly where ADR-0047 left it. This record names it
  and does not reduce it.
- Revisit B when a reader outside Runner needs the count. For example:
  - a deployment hands `experimental_evaluate` to an agent the operator does not control, in a
    project that keeps a trail, and someone asks how often it was refused;
  - a counterparty requires attempts to be disclosed;
  - the specification takes up a portable decision-record format (0047 decision 6).

## Questions for the maintainer

1. **Is a count wanted on the CLI or MCP paths now?** Recommendation: no. No reader outside Runner
   has asked for one. The CLI's caller is the operator, who would be counting its own refusals. The
   MCP path's stock configuration keeps no trail for a count to sit beside. B is revisited on the
   condition above.
2. **If a counter is ever kept, should a rehearsal under `requireReviewed` be distinguishable from a
   refusal?** Recommendation: yes. Count each refusal by its code (`JPS-LOCK-*`,
   `JPS-FACTS-COMPARABLE-REQUIRED`, each §8.4 class) and count rehearsals separately, never as one
   number:
   - A rehearsal is exempt from `requireReviewed` (`app.go:320-324`), and the refusal's own message
     tells the caller to "declare the run a rehearsal" (`internal/lock/lock.go:766`, `:773`). So a
     refusal followed by a rehearsal of the same draft is what the refusal asks for, and the
     author's loop ADR-0044 designed ("the author's loop moves to `--rehearsal`").
   - One number would count that loop as two attempts at the gate.
3. **Does the Desk Tests workspace need anything?** Recommendation: no.
   - Every evaluation Desk makes is a rehearsal (`GatesHelp.tsx:55`; `TestsWorkspace.tsx:499`).
   - The Tests workspace already keeps each run, failed ones with their error, in its own store
     (`TestsWorkspace.tsx:533-538`).
   - Desk's jobs plan keeps test storage apart from operational records
     (`operational-jobs-plan.md:36-37`).
   - In a project that keeps a trail, a rehearsal counter would count every Desk test click as
     exploration against the project, which is noise and not evidence.
4. **How does the ADR-0047 sentence land, given that accepted records are not edited?**
   Recommendation: do both. Annotate 0047's index row ("Selection (T3) settled by 0048"), which is
   the house's mark for a discharged deferral. Also add the one sentence to the bullet, on the #202
   precedent, because a reader of 0047 lands on "it needs its own ADR" and should find where it
   went. The sentence changes no determination of 0047.
5. **Should `audit verify` say it too?** Recommendation: yes, in a separate small pull request
   (`documented-claim`): one fixed `doesNotEstablish` sentence, present in every report, saying the
   trail is silent about refused and rehearsed evaluations. Today the closest sentence appears only
   without a held checkpoint (`verify.go:84`, `:770-771`). Yet the checkpointed report is the one a
   counterparty reads.

## The maintainer's answers (2026-10-05)

These were recorded from the maintainer's "accept the recommendation" on 2026-10-05. Each answer is
the recommendation above, as written. A later record may overrule any of them.

1. **A count on the CLI or MCP paths now:** no, the recommendation as written. B is revisited on the
   condition under Consequences.
2. **A rehearsal under `requireReviewed` distinguished from a refusal in any counter:** yes, the
   recommendation as written. A counter, if one is ever kept, counts each refusal by its code and
   counts rehearsals separately, never as one number.
3. **The Desk Tests workspace:** needs nothing, the recommendation as written.
4. **How the ADR-0047 sentence lands:** both, the recommendation as written. 0047's index row is
   annotated "Selection (T3) settled by 0048", and the one sentence is added at the end of its
   "Selection (T3)" bullet, on the #202 precedent for correcting an accepted record in place. The
   sentence changes no determination of 0047.
5. **`audit verify`:** yes, the recommendation as written: one fixed `doesNotEstablish` sentence,
   present in every report, saying the trail is silent about refused and rehearsed evaluations. It
   is made in a separate pull request (`documented-claim`), not in this record's.

## More information

- Runtime: the audit trail and "Errors are not records", ADR-0018; the reviewed-set lock, ADR-0019
  and ADR-0044; rehearsals, ADR-0028 and ADR-0041; the type refusal, ADR-0046; the defensible record
  and "Selection (T3)", ADR-0047; the version rules, `VERSIONING.md`; the guide's "Keeping a record
  of what you decided" and "Checking a trail, and handing over a checkpoint".
- Runner (at `db067e5`): `docs/MAPPING-V2.md`, "The installation's chain of runs";
  `internal/runner/store.go` and `internal/runner/runtime.go`.
- Desk (at `c15769d`): `docs/design/operational-jobs-plan.md`; `web/src/routes/GatesHelp.tsx`;
  `web/src/packs/test-workspace/TestsWorkspace.tsx`.
- Gateway (at `f4d626b`): `go/decision_policy.go`, the record kinds a decision policy reads.
- Issue #224.
