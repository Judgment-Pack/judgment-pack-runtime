---
status: proposed
date: 2026-08-02
deciders: maintainer
---

# Pin the reviewed set in a sibling lock, and refuse to decide under law that left it

## Context and problem statement

Observed live, during this project's own work: an agent in a stale sandbox met a `configVersion`
refusal, edited the declaration in `jpack.json` down until the refusal stopped, ran an evaluation,
narrated the result as an approval, and reverted the edit. Nothing in the transcript was a lie and
nothing in the runtime malfunctioned. What happened is that a decision was produced under law the
agent had just amended, and the amendment left no trace — the file was the same afterward, the
disposition was reported as ordinary, and no artifact anywhere recorded which bytes had been
applied. #69 narrowed that particular doorway by telling a reader which side to change. The doorway
is not the problem.

The problem is structural, and it does not go away by adding rules to the tree. A pack is law a
project reviewed; a `jpack.json` is the index that says which law counts. Both are files, in the
same working tree, in the same write domain as the agent asking for the decision. A rule that lives
in that tree is a rule its addressee can rewrite, so "the runtime must refuse to evaluate an edited
pack" is unenforceable in the general case: whatever the runtime checks, an editor with write access
can make the check pass. Any design that claims otherwise is claiming a wall it does not have.

So the question is not "how do we stop this" but "what can a runtime honestly do about it, knowing
nothing about the client". Two things. It can make every amendment an **explicit, verifiable act** —
one that leaves a diff a reviewer sees rather than a silence — and it can make every decision record
say **which law it was judged under**, so a disposition and the bytes that produced it are one
artifact rather than two unrelated ones. Neither prevents an editor from amending. Both convert a
silent edit into a recorded one, which is what an audit is for and what the incident lacked.

## Decision drivers

- The runtime and the law share a write domain; a design that pretends otherwise is dishonest.
- An amendment is legitimate. Authors change policy on purpose, and the mechanism must not punish
  them or it will be routed around within a week.
- The author's loop must stay free: writing a pack and trying it is drafting, not deciding.
- A project that never adopts this must be affected in no way at all — the audit trail's rule
  (ADR-0018), for the same reason.
- Whatever is checked at decision time must be recorded, or the record's strongest claim is
  unverifiable.
- One statement per rule, and no new place for a version to be declared.

## Considered options

- **A. A generated sibling lock, verified by the deciding surfaces, recorded in the audit trail**
  (chosen).
- **B. Discovery instead of declaration** — the runtime notices a document changed since the last
  run and warns. Rejected: it needs state of its own to compare against, which is the store
  ADR-0006 put in the client; and "changed since last time" is not "changed since it was reviewed",
  which is the only question worth answering. A first run would have nothing to compare against and
  would have to trust whatever it found.
- **C. Split the registry out of `jpack.json`** — keep a separate immutable index of reviewed packs.
  Rejected: it is the same file with a different name, in the same write domain, and it splits one
  project's inventory across two files that will drift (ADR-0017 rejected a second index file for
  exactly this reason).
- **D. Declare the lock inside the configuration** — a `lock` member naming the file. Rejected as
  circular: the lock pins the configuration, so a configuration that named its lock could rename it,
  and a reader that followed the rename would verify against whatever the edit chose. Convention is
  what makes the pointer unforgeable from inside the thing it points at.
- **E. Sign the lock in the runtime** — a signature over the reviewed set, verified before
  deciding. Rejected here and not on the merits of signing: this runtime is keyless and offline by
  ADR-0004's settled determination, and a signature it could verify is a signature it holds a key
  for. Signing belongs where the key lives — the desk, the product layer, the CI identity — which
  is the specification's own "around the pack" principle (RFC 0003) rather than the pack's.
- **F. Status quo** — rely on version control. Rejected: version control is where the amendment is
  reviewed, and it is the right place for that, but it is not consulted at decision time and cannot
  be. Between commits, the working tree is what evaluates.

## Decision outcome

Chosen option: **A**, because it is the largest honest step: it makes amendment explicit without
claiming to prevent it, and it costs a project that does not want it exactly nothing.

**The artifact.** `jpack.lock.json`, a sibling of `jpack.json` found by convention — the
configuration's own name with `.lock` before the extension, so `jpack.staging.json` locks to
`jpack.staging.lock.json`. A configuration filename that does not end in `.json` has no lock name at
all and the lock verbs refuse it, because trimming a suffix that is not there would map both
`jpack.json` and a file named `jpack` onto one `jpack.lock.json`, and two projects in one directory
sharing one reviewed set is two projects each denying the other the convention. It carries
`lockVersion` `"1"`, the digest of the configuration's exact bytes, and one entry per declared pack
and graph: the declared path, and the digest of that document's exact bytes, read through the
project's own rooted handle. It is generated, sorted, two-space indented, and byte-deterministic —
two runs over one tree write identical bytes, because it is an artifact a human reviews in a diff
and a generated file that churns is a file nobody reads.

**What the lock does not cover, and why.** A `jpack.json` declares four document paths; the lock
pins two. A pack's `matrix` and a graph's `rows` are deliberately outside the reviewed set, because
the only surfaces that read them are `packs test` and `graph test` — rehearsal, which neither
records nor decides. The lock covers the law a deciding surface applies, and a matrix is not law: it
is the harness that checks law. A project that changes a matrix changes nothing any decision is
judged under, and making a test-fixture edit an amendment would put the lock in the middle of the
author's loop, which is the one place this record is careful to keep it out of.

**Its presence is the whole of the opt-in.** There is no configuration member, no `configVersion`
bump, and no change to `jpack.schema.json`. A project with no lock file reaches nothing: no
verification, no refusal, no member in an audit record, no behavior change on any surface. That is
also why the lock is not declared inside the configuration — a member pointing at the file that pins
the member would be circular, and a convention cannot be renamed by the thing it constrains.

**Two verbs.** `packs lock` (re)generates the lock from the current configuration and documents, and
running it *is* the amendment: it is how a project says, in a reviewable file, that the law changed
on purpose. It reviews nothing and approves nothing — the pull request is still the approval, as it
is for the documents themselves. `packs verify` compares the tree against the lock and names every
difference: `config-drift`, `document-drift`, `document-missing`, `lock-entry-missing` (declared but
not locked), `locked-but-undeclared`, and `path-mismatch`. A document wrong in more than one way at
once produces one finding per way — a complete report is what the command promises. It reports what changed and never whether the change was
right, because a runtime cannot tell an amendment from tampering and should not pretend to.

**The deciding surfaces verify; the rehearsal surfaces do not.** `experimental evaluate`,
`experimental graph evaluate`, and the MCP `experimental_evaluate` tool consult the lock before
evaluating: the configuration's digest, plus the declared documents that one evaluation actually
applies — the named pack, or every node's pack and the graph document. A mismatch is fail-closed:
`JPS-LOCK-VERIFY`, exit `invalid`, no disposition, and a message naming both honest ways forward
(declare the amendment, or restore the reviewed bytes).

**The check is on the bytes that will be applied, never on a second read of the path they came
from.** A surface hands the lock the documents it already holds, and the comparison is against those
bytes; the configuration is bound the same way, from the digest taken when it was decoded. Reading
the file again to digest it would make the verified bytes and the evaluated bytes two different
reads of one pathname, and a writer between them yields a disposition from one document carrying a
review established over another — with the lock untouched, no `packs lock` run, and no diff. That is
the same gap `internal/project`'s reader exists to close by never handing back a pathname, and the
lock check must not reopen it. The graph evaluator reads each node's pack inside its own loop, so it
takes a caller-supplied check applied at the one point those bytes are in hand, before that node
evaluates; the graph package learns nothing about locks, and the test verb passes no check at all.
`packs verify` keeps the re-reading form, because re-reading is exactly the question that command
asks.

**One read of the reviewed set per run.** A run that consults the lock reads it once and retains the
decoded document and the digest of its exact bytes; every check that run makes is against that one
revision. This matters most on the graph surface, where the configuration and the graph document are
checked before the run starts and each node's pack as its bytes come in hand: two reads could see
two revisions, and a run that passed one check against each would record a review no single reviewed
set ever declared.

**An id the configuration does not declare is not the lock's finding.** A graph node naming an
unknown decision id is the graph's own reference check, which names the id and lists the configured
ones; the lock passes it over rather than refusing with a sentence that would state the opposite of
what is wrong and steer at a command that cannot fix it.

**A run that applies no declared document does not reach the lock at all** — not even to read it. An
unreadable lock, or one from a newer toolchain, must not stop someone drafting, because there is no
declared law in play for it to say anything about.

**The generator never writes over declared law.** `packs lock` refuses when the derived lock path is
a path the configuration declares — a pack, a matrix, a graph, or rows — because writing there would
destroy a reviewed document with a generated one and then record the digest of what it destroyed.
It also refuses to emit a lock past the byte limit its own readers apply, since a generator that
produced a document `packs verify` and every deciding surface must refuse would leave a project
whose only steer regenerates the same unreadable file — and it makes that refusal from the
configuration alone, before a single document is read, so an impossible lock costs no work. The
collision check asks about *files* rather than spellings: cleaned relative forms, file identity
through the project's own handle when both names exist, and a case-folded comparison for a name that
is not there yet, because on a filesystem that folds case the alternative is destroying a reviewed
document. The audit directory is refused the same way, at its own name or at any component above it:
a generated file there leaves the directory uncreatable and every later evaluation refuses *after*
evaluating.

**What the lock is held to when it is read.** It is generated, so anything malformed in it was
written by hand or by something that is not this runtime — and reading it loosely would let a
hand-edited entry verify. `lockVersion`, the configuration digest, and every entry's path and digest
are checked on load, digests against the one form this runtime writes. Each entry's recorded path is
compared with the path the configuration declares, and a mismatch is its own finding
(`path-mismatch`): the digest may be right while the file a reviewer reads says the bytes belong to
another document, and the lock's whole purpose is to be read.

**Work is bounded by the documents there are, not by the ids that name them.** A configuration may
point many declared ids at one document; each distinct file is read and digested once per run. An
aggregate byte budget over all declared documents is deliberately deferred: the per-document limits
already apply, the encoded-size preflight bounds the pathological configuration, and a new global
budget is a number this record would have to justify rather than inherit. `packs test`, `experimental graph test`, and
`experimental evaluate-corpus` consult nothing, ever — not even a lock this runtime cannot parse.
That is the same split ADR-0018 drew for audit records and it rests on the same principle, stated
here once for both: **the author's loop is free and decisions are classified.** A matrix row is a
check on a pack; a disposition someone acts on is a decision. Making rehearsal obey the lock would
make every edit-test cycle a lock cycle, and a mechanism that taxes drafting is a mechanism authors
disable.

**A document nobody declared is a draft, not a violation.** A pack named by path, or passed inline
over the wire, is never refused for being unlocked: writing a pack and trying it is the whole of
authoring. Nothing is verified for a run that reaches no declared document at all, so an unrelated
unlocked configuration edit does not block a scratch evaluation. What a draft costs is the claim: it
records itself as one.

**The record says which law it was judged under.** The audit record (ADR-0018) gains one optional
member, `reviewed`, present exactly when the project carries a lock: `true` when every document the
evaluation applied was declared and matched, `false` when any of them was a draft, and *absent* for
a project with no lock — because "does not use the convention" and "ran on unreviewed law" are
different facts and a record that collapsed them would be worse than one that said nothing.
The record names *which* reviewed set: `reviewedSet` carries the digest of the exact lock bytes the
checks used, the `lockVersion` those bytes declared, and the configuration digest compared. The lock is replaced in place, so without it the Boolean is a claim nothing outside the run
can re-derive — an auditor holding a record and a lock file could not tell whether that lock is the
one the decision was judged under, which contradicts this record's own driver that whatever is
checked at decision time is recorded. It is present exactly when `reviewed` is true.
`recordVersion` stays `"1"`: an added member is backward-compatible and a removed or renamed one is
the break, which is this repository's settled practice for every versioned JSON artifact it writes —
stated for `outputVersion` in VERSIONING.md, applied to `configVersion` by ADR-0012 and ADR-0017,
and generalized in VERSIONING.md by this change so the rule is stated where it is relied on. There is no "declared but drifted" value and there cannot be —
a deciding surface refuses such a run before the evaluator is reached, so nothing composes a record
for it. That interlock is what makes `reviewed: true` a strong claim rather than a label.

**The write.** `internal/fssecure` gains one primitive, a rooted whole-file replace, under exactly
the refusals the audit append is held to: the same flag choice by what is already at the path so a
symlink swapped in behind the check loses the race, the same post-open same-file and link-count
checks, and no pathname handed to anything. It is not atomic — `os.Root.Rename` arrived after this
module's Go floor — which is acceptable for a file whose one producer can be run again and whose
reader refuses a document it cannot decode.

### Consequences

- Good, because an amendment now leaves an artifact: `jpack.lock.json` changes, in a diff, in the
  same pull request as the document it pins, and a reviewer who sees policy change sees the lock
  change beside it.
- Good, because a decision record now says which law produced it, and the deciding surfaces make
  `reviewed: true` mean something rather than assert something.
- Good, because a project that ignores all of it is unaffected, and adopting it is one command.
- **Bad, and this is the honest limit: it is not a wall.** Anything that can edit a pack can run
  `packs lock` again, and this runtime cannot tell that from an author amending policy on purpose —
  they are the same act. The incident that motivated this record would, under this design, have left
  a modified lock file and a `reviewed` bit; it would not have been prevented. What stops an editor
  is not a check inside its write domain but a decision desk that holds its own reviewed checkout and
  never evaluates the caller's tree. That is product-side work this record names and does not build.
- Bad, because a project that adopts the lock takes on a step: a legitimate policy change now needs
  `packs lock` before the next decision, and forgetting it produces a refusal at exactly the wrong
  moment. The steer in the refusal message is the mitigation, and it is a mitigation rather than a
  fix.
- Bad, because a newly created lock is made durable by syncing its directory on unix and not on
  Windows, where a directory handle does not support it cleanly: there the guarantee is the file's
  contents and not its entry, so a crash in that window can leave a project whose lock is absent and
  therefore unverified. An atomic temp-file-and-rename would settle both, and `os.Root` gained no
  rename on this module's Go floor; it is recorded here as the work that closes it.
- Bad, because a project whose configuration filename does not end in `.json` cannot use the
  convention at all: the lock verbs refuse it and the deciding surfaces find no lock, which is the
  same answer a project without one gets. Injectivity is worth more than that case.
- Bad, because the graph verbs take a path rather than a decision id, so "is this the declared
  graph" is answered by comparing resolved pathnames. Its failure mode is the safe one — an
  unmatched path is treated as a draft, recording the run unreviewed rather than claiming a review
  it did not verify — but it is a comparison this runtime otherwise avoids making.
- The conformance claim is untouched: the evaluator gains nothing, no new surface reaches it, and
  CONFORMANCE.md is unedited.
- Revisit when the decision desk exists: once the deciding party holds its own checkout, the lock
  becomes what the desk verifies *against the tree it was given* rather than what a runtime checks
  in a tree it shares, and the honest limit above stops being one.
- Revisit when a reviewed set must be attested rather than declared — a signature, an attestation, a
  transparency log. Each of those needs a key or a service, which is the keyless posture (ADR-0004)
  reopened deliberately at the layer that holds one, not here.

## More information

ADR-0012 (the project convention, the handle, and the no-pathname corollary), ADR-0018 (the audit
trail this extends, and the deciding/rehearsal split it shares), ADR-0004 (keyless and offline),
ADR-0006 (no store in the runtime), ADR-0017 (why a second index file was rejected). The specific
incident is recorded in the commit message of #69, which narrowed the doorway this record is about
the room behind. `internal/lock` holds the document, the generation, and the verification;
`internal/project` holds the sibling-name convention; `internal/fssecure` holds the write.
