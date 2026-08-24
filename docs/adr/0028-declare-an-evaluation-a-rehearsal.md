---
status: proposed
date: 2026-08-24
deciders: maintainer
---

# Declare an evaluation a rehearsal, so exploration never writes a decision record

## Context and problem statement

A client exploring counterfactuals — edit the facts, re-evaluate, compare dispositions — calls
`experimental evaluate` repeatedly. In a project that declares an audit directory
([ADR-0018](0018-opt-in-evaluation-audit-trail.md)), every completed call appends one record: a
what-if session of N runs leaves N records saying the project decided N times, when it decided
zero times. The trail exists to make decisions accountable, and phantom decisions are the exact
corruption it exists to prevent (issue #124).

The vocabulary for this already exists, one surface over. `experimental_test_packs` states it in
its own tool description: "a matrix row is a rehearsal, not a decision, so no audit record is
appended (ADR-0018) and no reviewed set is consulted (ADR-0019)"
([ADR-0021](0021-run-the-declared-matrix-over-mcp.md) put the rehearsal outside the record). A
single exploratory evaluation has no equivalent: the tool takes no argument to say "this is a
rehearsal," so the only ways to explore safely are to avoid the tool or to un-declare the audit
directory — the second of which turns off recording for the decisions too, which is the wrong
lever pulled all the way.

Two questions have to be settled together: what a declared rehearsal skips, and whether the
payload it produces says what it is.

## Decision drivers

- ADR-0018's record integrity: the trail must contain decisions and only decisions; both a
  missing decision and a phantom one corrupt it.
- One vocabulary: "rehearsal" already means something exact in this runtime — writes nothing,
  consults no reviewed set (ADR-0021). A second, narrower meaning of the same word would make
  the tool descriptions contradict each other.
- Explicit over inferred, the same rule the evidence document follows at §8.2: absence of a
  declaration is the ordinary recorded call, and nothing — not the client's identity, not the
  call's shape — infers rehearsal on the caller's behalf. A consequential evaluation must not be
  able to quietly opt itself out.
- A payload outlives its call. Payloads are copied, stored, and handed to other tools as
  untrusted arguments (ADR-0008); a rehearsal payload indistinguishable from a decision's is a
  forgery kit assembled by accident.
- The engine stays pure (ADR-0007): evaluation is `bytes -> result`, and rehearsal changes what
  is *recorded about* an evaluation, never what is evaluated.

## Considered options

- **A. An explicit opt-in that adopts the matrix row's full standing** — `--rehearsal` on the
  CLI, boolean `rehearsal` on the MCP tool: the evaluation runs identically, no audit record is
  appended, no reviewed set is consulted, and the payload carries `"rehearsal": true` in band.
- **B. Skip the record only** — rehearsal still consults the reviewed set.
- **C. Status quo** — explore via matrix rows, or un-declare the audit directory.
- **D. Infer rehearsal** from the surface or client identity.

## Decision outcome

Chosen option: **A**.

Option D is rejected on the explicit-over-inferred driver alone: an inference that turns off
recording is an inference that can be gamed, and the caller who wants the ordinary call must
never wonder what was recorded. Option C makes the honest path the awkward one — authoring a
matrix row to ask one exploratory question, or turning off the trail for real decisions — and
awkward honesty loses to convenient corruption. Option B splits the word: "rehearsal" would
consult the lock on one surface and not the other, and the lock's own purpose
([ADR-0019](0019-reviewed-set-lock.md): refuse to *decide* under law that left the reviewed set)
does not reach a run that is declared, in band, to decide nothing. The capability B withholds
already exists regardless — a matrix row runs unreviewed law today, by design.

Settled constraints:

1. **The declaration.** CLI `--rehearsal`; MCP boolean `rehearsal` on `experimental_evaluate`,
   held to its type exactly as the document arguments are (null and every non-boolean rejected;
   omitting the key is the only form absence takes — a decoding accident must never
   manufacture the declaration).
2. **What it skips.** No audit record is appended, and no reviewed set is consulted — the
   matrix row's standing (ADR-0021), extended to one declared exploratory run. Nothing else
   changes: the same engine, the same options, the same disposition and trace the recorded call
   would produce (ADR-0027's contract applies unchanged).
3. **The label.** The payload carries `"rehearsal": true` exactly when the caller declared it,
   beside `experimental`; the human rendering says the same in one line. Absent otherwise —
   an additive member under VERSIONING.md's MINOR rule, so a consumer that never read it reads
   the payload it read before. A stored rehearsal thereby says in band that it was never a
   decision, the same in-band honesty `draftPrototype` established for the draft grammar.
4. **The default.** Record-when-declared stands. Rehearsal is the caller's explicit statement of
   intent, made call by call; there is no mode, no environment variable, and no configuration
   member that makes it sticky.
5. **Scope.** `experimental_evaluate` and its CLI command, exactly. Matrix and graph-matrix runs
   already write nothing; the graph *evaluation* surface, if it ever wants a rehearsal, takes
   this ADR's shape as precedent rather than being amended into it here.

### Consequences

- Good, because a what-if loop of any length leaves the audit trail exactly as it found it, and
  the trail keeps meaning what ADR-0018 says it means.
- Good, because every rehearsal payload confesses: a consumer holding one cannot mistake it for
  a decision without ignoring a member that is present precisely to be read.
- Good, because "rehearsal" now means one thing everywhere a tool description uses the word.
- Bad, because a caller can evaluate declared law without consulting the lock by declaring a
  rehearsal. That capability is not new — a matrix row has always had it — and the label is the
  mitigation: the run that skipped the lock is marked as the run that decided nothing.
- Neutral, because the engine, the disposition, the trace, and every conformance surface are
  byte-identical either way; the change lives entirely in what is recorded and reported about a
  run.
