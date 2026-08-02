---
status: accepted
date: 2026-08-02
deciders: maintainer
---

# Record evaluations into the project's own tree, and only when the project asks

## Context and problem statement

A project that runs decisions through this runtime has no record of what it decided. Every payload
goes to a stream and is gone: a CI step pipes it into a gate, an agent reads the disposition and
moves on, and an MCP client keeps a transcript at best. Reconstructing "what did this pack decide,
on what, in what version" after the fact requires that the caller kept the payload, and callers do
not. The runtime is the only party that ever holds the pack bytes, the facts as evaluated, and the
canonical §8.3 disposition in the same instant, and today it holds them for the length of one
function call.

The reason it does nothing with them is a settled one. ADR-0006 decided the authoring lifecycle
lives in the client because "the runtime is a stateless `bytes -> result` validator; a store is
state", and ADR-0012 stated flatly that on the MCP surface "nothing writes". Those determinations
are what make this runtime safe to point at a directory of real policy documents, and any record
that contradicts them wholesale buys an audit trail at the price of the posture the trail is
supposed to be trustworthy under.

## Decision drivers

- The record must be the *project's* to ask for, not an operator's to remember and not a default.
- A project that does not ask must be written to by nothing at all — no directory, no empty file.
- The write must be bounded exactly as every read is: the handle of ADR-0012, not a pathname.
- Errors are not dispositions (§8.4). A trail of decisions must not contain non-decisions.
- A test run is not a decision: `packs test` and the graph matrix run the same evaluator.
- One statement per rule: the version gate lives in the schema's own bytes, as ADR-0017 established.
- Whatever is written must be reconstructible into the evaluation that produced it, or it is a log
  rather than a record.

## Considered options

- **A. An `audit` member under a new configVersion `"3"`, written by the deciding surfaces only**
  (chosen).
- **B. Status quo** — the caller keeps the payload. Rejected: it is exactly what callers do not do,
  and the one party that could record cheaply is the one told not to.
- **C. A CLI flag, `--audit-log <path>`** — rejected: an operator-named path makes recording a
  property of an invocation rather than of a project, so the run that forgot the flag is the run
  with no record, and the MCP surface has no flags at all by design (ADR-0012). It also reopens
  nothing about containment because it stays outside the project, which is precisely the problem:
  the write would be unrooted.
- **D. Records to the MCP server's existing stderr diagnostic sink** — rejected: a diagnostic stream
  is not a trail. It interleaves with everything else the client writes there, most clients discard
  it, its posture is sanitized and value-free, and it leaves the CLI with no answer at all.
- **E. `audit` as an optional member under configVersion `"2"`** — rejected on measured behavior,
  exactly as ADR-0017 rejected the same move for `graphs`: the closed schema turns an unknown member
  into `JPS-PROJECT-CONFIG-SCHEMA` at exit `invalid`, and that one refusal takes down every `packs`
  command and all three MCP project tools, indistinguishable from a genuinely broken configuration.
  A version bump is refused as `JPS-PROJECT-CONFIG-VERSION` at exit `unsupported`, naming what it
  accepts. Opting in must cost the actionable refusal.

## Decision outcome

Chosen option: **A**, because the only defensible answer to "does this runtime write?" is one the
project itself gives, in the file it already uses to say what it owns, and because the write can
then be bounded by the handle that file's directory is already held open on.

**The shape.** `configVersion` becomes `"3"`; this runtime reads `"1"`, `"2"`, and `"3"`. A root
`audit` member — a closed object whose one required member is `dir`, a path relative to the
configuration's own directory — is gated to `"3"` by an `if/then` in the schema itself, beside the
one ADR-0017 wrote for `graphs`, so the rule is stated once in the bytes `packs schema` prints and a
configuration asking for a trail under an earlier version is told the exact value to change. A
`"3"` may still declare graphs: a later shape adds a member, it does not drop one. The embedded
schema's `$id` becomes `urn:judgmentpack:runtime:jpack-config:3`. Declaring the member is the whole
of the opt-in; there is no flag, no environment variable, and no default directory.

**What records, and what deliberately does not.** Three surfaces write, each in its own code and
only after the evaluator has returned a result: `experimental evaluate`, `experimental graph
evaluate` (one record per node, plus one for the composite headline), and the MCP
`experimental_evaluate` tool. Three surfaces reach the same evaluator and write nothing: `packs
test`, `experimental graph test`, and `experimental evaluate-corpus`. A matrix row is a check on a
pack, not a decision anyone took, and a CI run that emitted one record per row would bury the ones
that matter. The graph surface makes that distinction structurally rather than by inspecting a
command string: the trail is an explicit field on the graph options that the evaluate verb sets and
the test verb does not, over the same project and the same configuration.

**The evaluator is untouched.** No option, no field, no hook, no constructor. The engine stays a
pure function of its arguments, and every record is composed by the surface that called it, from
what that surface already held. This is also what keeps `internal/cli`'s constructor-site count —
the mechanical guard on CONFORMANCE.md's enumeration of the six surfaces that reach the evaluator —
unchanged.

**The record.** One JSON text per line in `<dir>/evaluations.jsonl`, appended and never rewritten:
`recordVersion` `"1"`, `run` (this invocation's id), `at` (RFC 3339, UTC, nanoseconds), `kind`,
`surface`, the `tool` that produced it and the bundled `artifact` it evaluated against,
`evaluatorSpecVersion`, the pack's `id`, `version`, `specVersion` and the SHA-256 digest of its
exact bytes, the graph's identity, `formatVersion` and document digest where there is one, the
`facts` and `evidence` documents as they reached the engine with the `evidenceSupplied` flag that
keeps §8.2's omitted document distinct from an empty one, `draftPrototype` exactly when the payload
carries it, and the disposition as the RFC 8785 canonical bytes the run itself produced — embedded,
never re-serialized through the pretty printer that re-indents inside that member. Provenance is
recorded because a record that named a contract version and nothing that ran it would not
reconstruct an evaluation; the graph digest because a graph's `id` and `version` are as mutable as a
pack's, and the pack digest already answers that question for packs; `draftPrototype` because a
disposition produced under operators no published JPS version defines is not an ordinary
disposition, and a record that dropped the label would assert it was. On the graph surface the
recorded facts are the *assembled* document, after upstream outcomes were injected: what the node
was actually evaluated against, which on that surface is not what the caller supplied.

**Inputs are recorded as JSON values, not as source bytes.** The documents reach the trail through
the line encoder, which compacts them, so `{ "x": 1 }` is written `{"x":1}` and a line is the source
compacted rather than the source itself or a normalization of it. Nothing about replay is lost —
evaluation is a function of the value — and the alternative, a second copy of every input as raw
bytes or a digest of them, is weight for a question the pack and graph digests already answer about
the documents that decide what an evaluation means. The package documentation says which of the two
a reader is holding.

**The record contains values, and that is not a reversal of the value-free posture.** Diagnostics
are sanitized because they go to an operator who did not ask for the values and may not be entitled
to them. A record goes to a directory the project named for exactly this purpose, in the project's
own tree, at the project's own request. They are different artifacts with different readers, and
only the first is sanitized. The timestamp is the other deliberate difference: nothing on the
payload path reads a clock, and nothing on the payload path reads a record.

**Errors are not records, and a refused run is never opened for.** Only the evaluator's success
return leaves a record. A refused evaluation has no disposition at all under §8.4, and a trail whose
lines were sometimes results and sometimes failures would not be a trail of what the project
decided. A composition is one run for this purpose: its node records are composed as each node
completes but *held*, and appended in a single write once the run has a composite, so a graph
refused at its third node never opens the trail at all and leaves nothing for the first two.
Records are stamped when composed rather than when written, at nanosecond resolution, so a held
record's time is when its node ran. The ordering key within a run is the file itself — records are
appended in composition order — because no wall clock guarantees distinct stamps for adjacent
records (Windows ticks at 100ns granularity).

**What that does *not* promise, stated plainly.** A flat append cannot be atomic against arbitrary
I/O failure. A refusal reached before or at the open writes nothing, which covers every refusal this
implementation produces by choice — an escaping path, a symlinked trail, a directory that is not
one, a disposition that will not canonicalize. But a write that fails partway through has already
put bytes in the file, and no appender can take them back: a trail can end in a complete record
whose run was then reported failed, or in a partial line. So the guarantee is stated as a *reader's*
rule instead of a writer's. Every record carries a `run` id, one per invocation; a graph run's
`graph-composite` line is its commit marker, and node lines whose run id has no composite belong to
a run that did not finish. A trailing line that is not a complete JSON text is a write that did not
complete, and it can only be the last line. That rule is written down in `internal/audit`'s package
documentation, where a reader of a trail will look for it, and it is what makes an orphaned run
identifiable rather than indistinguishable from a later run's nodes.

**A failed write refuses the run.** The append is fail-closed: exit `4` with the provisional code
`JPS-AUDIT-WRITE` and a value-free sentence, through the same operational path every other
input/output failure takes, and the disposition is not emitted. A project that asked to be told what
its packs decided is not served by an answer it has no record of. The evaluation itself is never
influenced — it has already happened, in full, before anything is written.

**The configuration is consulted on every evaluation the deciding surfaces run**, not only on the
ones naming a pack by decision id, because whether a run is recorded is the configuration's to say
and a pack named by path or passed as text is still evaluated *in the project the command was run
in*. That is the honest statement of what the trail records and it is worth being exact about: the
trail belongs to the configuration `--config`/`JPACK_CONFIG`/the working directory resolves, not to
whatever project the evaluated file happens to live in, so a pack read from anywhere on disk is
recorded as a decision that project ran. Three cases, and the middle one is a deliberate change of
behavior: a configuration that resolves and loads and declares `audit` records every evaluation on
that surface; a configuration that *is there* and fails to load refuses the run, where a path-named
or inline pack would previously have been evaluated without the configuration ever being read; a
configuration that is not there at all changes nothing, records nothing, and leaves a project that
does not use the convention exactly as it was. "There" is mere presence, not a readable regular
file: a symlinked or otherwise unloadable configuration must reach the loader and be refused by it,
because reading it as "this project does not use the convention" is the fail-open this
consultation exists to prevent.

**Argument refusals still precede the configuration.** A call missing a required argument, or naming
an input the command does not take, never became an evaluation — so on both surfaces every
argument-shape check runs before the filesystem is touched, the CLI's `--facts`, dual-stdin,
evidence-stdin and URL refusals included, and before a decision id is resolved. Otherwise a broken
configuration answers in place of the mistake the caller actually made. `packs validate` gains the one check the configuration can now ask for that no pack entry
owns: `audit-dir-inside-root`, decided against the same handle every read is, reported beside the
pack checks and moving the report to invalid. Only an escape fails it; a contained directory that
is not there yet is contained, because the first record is what creates it.

**The write primitive.** `internal/fssecure` gains three operations and no more: appending one
record to a file beneath the retained `os.Root` handle, creating the directory for it beneath the
same handle, and answering the containment question a *directory* poses. They refuse everything the
reader refuses — the lexical escapes, a path leaving the root through a symlinked component, a final
component that is a symlink, anything that is not a regular file — and they resolve nothing to a
pathname for something else to open.

Two refusals are the write's own. Which flags the open carries is decided by what is already at the
path: an existing regular file is opened *without* `O_CREATE`, and an absent one with
`O_CREATE|O_EXCL`, retried a bounded number of times if the two conditions swap under it. That is
what keeps a refusal from planting a file — `os.Root` follows a link that stays inside the root, so
a plain `O_CREATE` open would create the target of a symlink swapped in behind the check, again on
every run, and `O_NOFOLLOW` in the flag word does not help because `os.Root` resolves the path
itself. And a file with more than one link is refused where the platform reports the count — a
hardlinked alias is invisible to every path-based check, and appending a record into whatever else
names that inode should not be the one case that does not fail closed. It is not a containment
question: making the alias needs write access inside the project, the same trust domain as editing
the packs, and SECURITY.md says so rather than claiming more than the rule delivers.

The directory question is separate from the file one, and the difference decides a validate-time
check. `Contains` deliberately leaves a final component unresolved, because for a file a final
symlink is refused at the open whatever it points at. For `audit.dir` that component becomes an
*intermediate* one of everything written beneath it, so `audit -> ../outside` is an escape — and a
check that left it unresolved would report the project valid and let every evaluation fail. The
directory form resolves an existing final component through the handle, treats a directory that is
not there yet as contained, and is what `audit-dir-inside-root` asks.

`O_APPEND` is what makes each write one operation at the end of the file; the trail file's mode is
set to owner-only on every append rather than only at creation, on unix — a Go file mode on Windows
sets the read-only attribute and does not touch the DACL, so confidentiality there is the containing
directory's ACL and the documents say so; a directory that was already there keeps the mode the
project gave it; and the record's bytes are synced before the surface is told it was written — which
says nothing about the directory entry of a file the same call created, because nothing here fsyncs
a directory.

**What this supersedes, exactly.** ADR-0006's determinations that "the runtime never stores,
overwrites, or deletes user content" and that "there is no session document and no store", as
unconditional statements: they hold for everything except the one trail a project declares in its
own configuration, and they hold entirely for the authoring lifecycle that record is about — no
validation, schema, example, or prompt operation writes anything. ADR-0012's determination that
on the MCP surface "nothing writes": keyless and offline stand untouched, and read-only is now
read-only plus one appended record when the project asked for one. ADR-0012's corollary that no
surface may stop at the lexical half of containment and none may hand back a pathname is *not*
superseded — it is the rule this write is built to obey, which is why the primitive lives in
`internal/fssecure` rather than at a call site. And ADR-0017's two version determinations: the
accepted `configVersion` value list, which was `"1"` and `"2"` and is now also `"3"`, and the
embedded schema's `$id`, which named `…:2` as the newest shape the bytes describe and now names
`…:3`. Its reasoning is what this record relies on rather than something this record revises —
that the gate belongs in the schema's own bytes, and that opting in must cost the actionable
`unsupported` refusal. No superseded record is edited; each supersession is annotated on that
record's index row, as `docs/adr/README.md` requires.

### Consequences

- Good, because a project can answer "what did we decide, on what, under which pack version" from
  its own tree, with the pack digest that no payload carries and the canonical disposition bytes
  another implementation can compare against.
- Good, because a project that declares no `audit` member is written to by nothing: the posture the
  earlier records established is the default and stays the default.
- Bad, because this runtime now writes at all, and every document that said it never does had to be
  narrowed rather than left to be discovered as false by a reader.
- Bad, because an evaluation in a project whose configuration is broken now fails where a
  path-named pack used to succeed. That is the price of fail-closed recording, and it is a
  configuration defect either way.
- Bad, because the records accumulate without bound. No rotation, no retention, no compaction: the
  file is the project's, in the project's tree, and the project's own tooling owns its lifetime.
  Adding rotation would be adding a policy nobody stated.
- The conformance claim is untouched: the evaluator gains nothing, no new surface reaches it, and
  CONFORMANCE.md is unedited — this record restates no part of what that file says.
- Bad, because owner-only permissions on the trail are a unix guarantee only. On Windows a Go file
  mode sets the read-only attribute and does not restrict the DACL, so confidentiality there is the
  containing directory's ACL and this record says so rather than claiming otherwise. Establishing a
  DACL through the opened handle is a Windows-specific syscall this runtime does not make today.
- Revisit when a project needs more than one file, needs records shipped off the box, or needs the
  record shape to carry something an evaluation payload does not — each of which is a different
  decision from this one, and the first of them reopens the file layout rather than the posture.
- Revisit when a trail must survive arbitrary I/O failure with a stronger guarantee than a reader's
  rule: that is a transactional format — per-run artifacts committed by rename, or a length-and-
  checksum framing a reader verifies — and it is a different storage decision from this one, not an
  adjustment to it. The run id is deliberately the shape that decision would build on.

## More information

ADR-0006 (the store belongs in the client), ADR-0012 (the project convention, the handle, and the
no-pathname corollary), ADR-0017 (the version-gate pattern this copies, and the two determinations
it superseded), ADR-0005 (one `JPS-` prefix, exit class carries the category). `internal/audit`
composes and appends the records; `internal/fssecure` holds the one write primitive bounded by a
project's handle — `--write` on `spec schema`, `spec examples`, and `packs schema` is a separate,
unrooted copy at an operator-named pathname and is untouched here;
`internal/project/jpack.schema.json` is the one statement of the version gate.
