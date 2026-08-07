---
status: accepted
date: 2026-08-07
deciders: maintainer
---

# Derive candidate test-row inputs from a pack's own literals, and never their expectations

## Context and problem statement

[ADR-0023](0023-boundary-probes-for-ordered-comparisons.md) made one defect class visible: a rule
described as "5000 or more" that compares `greater-than "5000"` is two individually valid members
disagreeing at exactly one input, and a suite with rows at 4999 and 5001 is green while pinning
neither reading. The coverage report now names the gap. It does not help anyone close it, and the
gap it names is one of a family: the same pack's bands between literals go unprobed, its `in`
operands go untried, and the report says nothing about any of those because it derives no demand for
them.

Closing a named gap by hand is transcription work — read the pointer out of the report, write a
facts document that places a decimal at it — and transcription is exactly what a deterministic
runtime should do instead of asking a person to. But the *other* half of a row is not transcription
at all. An expectation says what the pack **should** decide, and the only source for that is the
policy text. A generator that supplied both halves would be [ADR-0014](0014-matrix-coverage-report.md)'s
circular oracle with better coverage numbers, and that record already conceded that nothing
mechanical closes it.

## Decision drivers

- ADR-0014's circular-oracle concession, inherited whole: a suite whose expectations came from the
  implementation tests nothing, and no mechanism here may make that easier.
- ADR-0023's demand discipline: a derivation that *demands* a row must not demand one no author
  could justify. Whether that constrains a derivation that *offers* one is the question this record
  has to answer rather than assume.
- The reference-only rule of [ADR-0011](0011-first-evaluator-conformance-claim.md): nothing here may
  state or imply that a matrix with more rows is a better one.
- Volume is the enemy of review. An unreviewable pile of candidates is a design failure and not a
  coverage win, because the failure mode being guarded against is a human approving what they did
  not read.
- The demotion discipline of [ADR-0012](0012-jpack-project-convention.md): a dimension the generator
  cannot address is reported, never silently omitted.

## Considered options

- **A. `jpack packs suggest`, emitting candidate *inputs* with the expectation absent, CLI-only**
  (chosen).
- **B. Status quo:** the coverage report names the gap and nobody is helped to close it.
- **C. Emit rows with a sentinel expectation** the loader refuses to score — `"TODO"`, or an empty
  disposition.
- **D. Gate `packs test` on `origin: "generated"`,** refusing to run a row that still declares it.
- **E. Emit rows with the expectation filled in** by running the evaluator over each candidate.
- **F. Fix the step at the census's 0.01/0.001,** the distances authored corpora actually use.
- **G. Cross the pointers:** every combination of every pointer's lattice values.
- **H. Widen `comparisonSites` to collect membership sites too,** rather than adding a second leaf
  handler over the same walk.
- **I. Ship it as an MCP tool beside `experimental_test_packs`** in the same change.

## Decision outcome

Chosen option: **A**. Every other option is refused for one load-bearing reason.

**E is refused because it is the circular oracle, stated plainly.** Running the pack to obtain the
expectation makes the expectation a restatement of the implementation, and a suite of such rows
passes forever while saying nothing. This generator therefore **never runs the evaluator**: it reads
a pack document and emits facts, and no member of its output is a product of an evaluation. That is
a real property and a bounded one, and this record claims exactly that much — it declines to
*shorten* the loop, and does not close it (see the consequences).

**C is refused, and the reasoning is the hinge of the whole design.** The brief offered "absent or a
sentinel the loader refuses to score", and absence wins on two grounds. First, absence is enforced by
machinery that already exists, and a verbatim paste meets **two** refusals of it, in this order:

1. A candidate carries a `rationale`, which is a member of no row. `internal/project/matrix.go`
   decodes strictly, so the *whole matrix* is refused — "the matrix has a member this runtime does
   not know" — before any row is examined. This is the anti-scoring layer for the generator's own
   prose: the sentence explaining a candidate cannot ride into anything that gets scored, and it
   cannot be mistaken for a row member the loader tolerated.
2. With the `rationale` removed, the row declares neither expectation, and the loader refuses it by
   name: "must declare exactly one of `expectedDisposition` and `expectedErrorClass`". That is the
   message naming the missing work.

Neither refusal is code this change adds or can relax; a sentinel would need new refusal code, one
`if` away from being softened by a future edit. Second, and more importantly, **a sentinel is a slot
inviting a fill, and the fill is one token.** Absence is not a slot: the reviewer must *author* the
expectation from the policy text. What the reviewer writes for an outcome disposition is `kind`,
`outcomeId`, `reasons`, and `handoff` — a categorically different act from overwriting `"TODO"` —
and what the loader *enforces* is the weaker, unrelaxable "exactly one of the two". The distinction
is deliberate: the four members are the shape of the authoring act, not a check anything performs at
load.

**D is refused, and refusing it is not a concession.** A gate on the provenance marker is defeated by
deleting one member — a *worse* affordance than the sentinel this record already refuses — and it
makes the marker self-erasing, destroying the one signal that actually measures rubber-stamping.
More basically, the gate would be aimed at the wrong member: once a human writes the expectation the
row is human-authored in the only member that decides anything, and gating on how its *facts* were
supplied would refuse a row that is exactly as good as one typed by hand. So `origin` is carried,
plumbed, and **counted**, never gated.

**F is refused because the census's evidence, read correctly, is inverted.** The corpus study
(`DIVERSITY.md`) shows authored values are a two-point mass: 181 of 784 records exactly on a
threshold, 229 more within 0.01, and only **4 records in the whole 784** in the gap `(0.01, 1.0]`.
The authored neighbours of the integer thresholds 40 and 70 were 39.99 ×58, 39.999 ×28, 69.99 ×51,
69.999 ×48, 70.01 ×33, 70.001 ×8, while `69` appears exactly once. Authors already hug thresholds
two and three orders *finer* than the threshold's own precision. A generator that reproduced the hug
would mechanize what already happens; the unit at the authored precision is what authorship misses.
The hug is available behind `--include-hugs` and is off by default for that reason.

**G is refused on review grounds before performance grounds.** One factor at a time keeps each
candidate's point legible — "this row moves `/expense/amountUsd` to 5000, the literal" is
reviewable — where a cross product produces mostly-nonsense rows in a pile nobody reads. Row count
is then linear in the pointers rather than exponential in them, which is the secondary benefit.

**H is refused because widening the shared walk would widen a demand.** `comparisonSites` feeds
ADR-0023's coverage probes, which are demands a report says no row satisfies; the generator's
membership sites feed offers. The recursion is instead split into an enumeration half
(`packConditions`) and a leaf half (`walkConditions`), and the generator supplies a second leaf. A
golden test pins the ordered walk's collected sites byte for byte over every pack document this
repository ships that states an ordered comparison — the evaluator's and the graph package's
testdata packs and the five bundled JPS artifacts across both draft versions, two of which exist
precisely to state a comparison the decimal rule refuses — plus the synthetic shapes the boundary
tests exercise. The golden's lines were taken from the pre-split recursion, built from
`git show HEAD:internal/project/coverage.go` in a scratch tree, so they are the old implementation's
output rather than the new one's restated: the split is demonstrably a refactor and not a widening.

**I is refused for now, with the condition recorded.** [ADR-0021](0021-run-the-declared-matrix-over-mcp.md)
set the precedent of a CLI surface lived with before its MCP counterpart, and ADR-0023 spent a whole
determination on the 16 MiB response bound a candidate set would push against. Decisively: **an MCP
tool returning candidate rows into an agent's context is the rubber-stamping vector.** The agent
would receive the inputs and an expectation-shaped hole in the same turn, with the evaluator one
tool call away. A file a human must open is friction, and here friction is the feature.

Settled determinations:

- **The command is `jpack packs suggest`**, an eighth subcommand beside `list`, `validate`, `test`,
  `lock`, `verify`, `lint`, and `schema`. It emits candidate **inputs**, never rows.
- **The emitted document is not a matrix, by construction.** Its root members are
  `candidatesVersion: "1"` and `candidates`, not `matrixVersion`/`cases`. A `jpack.json` `matrix`
  path aimed at a raw candidate file fails at `LoadMatrix` twice over — strict decoding rejects the
  unknown root members, and the zero-rows check rejects it again. Neither refusal is new; the
  document was shaped to trip refusals that already existed.
- **Each candidate carries `id`, `origin`, `facts`, optionally `evidenceAvailability`, and a
  `rationale`, and carries NEITHER `expectedDisposition` NOR `expectedErrorClass`** — absence, not a
  sentinel, for the reasons under option C. The `rationale` is prose about the *pack*: a sentence
  saying what the candidate places and which of the pack's own declarations imply it, closed by the
  one sentence every candidate ends with — no expectation is stated, write one from the policy text
  or delete this candidate. It never describes what an evaluation would produce, because nothing here
  evaluates anything. `rationale` is also what makes a verbatim paste fail first, under option C.
- **`origin: "generated"` is provenance, reported and never gated.** `packs test` gains a per-pack
  `origins` array counting the rows by the origin each declares, rendered in both formats. It moves
  no status, no summary, and no exit code. Only rows that declare an origin are counted, so a suite
  where nothing declares one carries no member rather than a zero: the absence of the marker is not a
  claim that every row was hand written, and a count of `""` would read like one.
- **The lattice, per fact pointer, is at-literal, one step either side, interior midpoints, and
  outer edges.** For a pointer compared against *n* distinct literals that is at most `4n+1` values
  — `6n+1` under `--include-hugs`, which adds the hug pair to each literal — deduplicated by
  `evaluation.DecimalKey` — the evaluator's own decimal identity, so `70` and `70.0` derive one
  lattice — and ordered along the number line.
  - **At the literal**, in the spelling the pack authored: ADR-0023's own probe point, the single
    input where a strict and a non-strict encoding disagree. Where one value was authored under two
    spellings the group carries the first-declared one, exactly as ADR-0023's probe does.
  - **One step either side, at `10^-d` where `d` is the count of digits after the decimal point of
    the FINEST spelling the boundary's sites authored.** `"5000"` steps by 1; `"70.0"` by 0.1; a
    pointer compared against `"5000"` in one rule and `"5000.0"` in another steps by 0.1, because
    that is one boundary and a tenth is a precision this policy wrote. Reading `d` off the
    first-declared spelling instead would make **reordering two rules change the lattice** — the
    same policy, two different candidate sets, with nothing in the pack to explain the difference —
    and the derivation must be a function of what the pack says rather than of the order it says it
    in. `d` is clamped at 6, the clamp is likewise taken over every spelling, and a clamped
    candidate's rationale says the value is not the pack's own next distinguishable one rather than
    letting it pass as the authored precision.
  - **Interior midpoints of adjacent literals.** This invents no granularity: 2 divides 10, so the
    midpoint of two terminating decimals is itself a terminating decimal and therefore itself a §2.2
    value. That is precisely why midpoints are principled where "adjacent" is not.
  - **Outer edges** at `v₁ − 1` and `vₙ + 1`, the regions beyond every band the literals partition.
  - **`--include-hugs` carries the same `10^-6` floor**, and the floor is the design's rather than an
    accident of it: nothing here steps finer than the unit step does. So the flag's "two decimal
    places finer" is exact only below five authored digits; at five the pair lands one place finer
    instead of two, and at six or more there is no finer pair to offer and none is emitted. Both
    narrowings are **reported as declined dimensions** (`clamped-hug`, `unavailable-hug`) and the
    clamped pair's rationale says which distance it carries, because a pair quietly delivered one
    place finer — or quietly not delivered — reads as the pair the flag named.
- **This is not a reversal of ADR-0023's refused option E, and the hinge is demand versus offer.**
  That record reads, in full (the quotation is delimited with `_` so the source's own emphasis
  survives inside it): _"**E is refused because the decimal domain is dense.** §2.2 decimals
  have no successor: 5000.1, 5000.01, and so on downward forever. "Adjacent" is a granularity the
  runtime would have to invent, and a probe built on it would demand a row at a value nobody can
  justify — the failure ADR-0014 refuses when it declines to demand unconstructible rows. Nothing is
  lost: a row at exactly the literal is *precisely* the discriminator between a strict and a non-strict
  encoding (at 5000, `greater-than` is false and `greater-than-or-equal` is true; everywhere else the
  two agree), so a far-side row adds no information about this class at all."_ Every clause of that
  is about a **probe** — a derived demand a report marks missing, which moves a line in a report a
  reader must answer. A generated value is an **offer**: it moves no probe, no status, no summary,
  and no exit code, and an offer nobody can justify costs one delete. The density objection is
  answered the same way: the step rule picks a granularity the *pack* stated rather than one the
  runtime invented, and where even that is unjustifiable the candidate is deleted at no cost. The
  refusal of E stands unchanged for the coverage report, which derives no far-side probe and still
  will not.
- **Composition is one factor or axis at a time.** A value or membership candidate varies exactly
  one pointer and holds every other member of the base at what the base said. An evidence candidate
  varies no pointer at all — it moves the availability axis and holds the facts at the base — and the
  one absence candidate a run with no `--base` emits states no facts, so it withholds every consulted
  pointer at once, because with no base there is nothing to hold the others at and a per-pointer
  absence would be the same empty document repeated. A run's size is the sum over pointers and axes,
  never their product. The base assignment is, in order of preference: `--base <rowId>`, an
  already-reviewed row of that pack's matrix, so the candidate reads as "this reviewed row, with one
  pointer moved"; otherwise a facts document carrying **only** the varied pointer, which invents
  nothing and will often evaluate to unknown — which is fine, because the generator claims nothing
  about what a candidate produces. A plausible-looking full record is **never** synthesized: that is
  the generator inventing a policy world, and it is what would make a generated row look
  authoritative. Where a base states a scalar the varied pointer needs to descend through, the
  placement is refused and reported rather than overwriting it, because overwriting would change the
  base beyond the one varied pointer. An **explicit JSON null is such a scalar**: `{"expense": null}`
  is a base that states the answer is null, so growing an object under it would edit a stated value
  rather than vary a pointer — a member the base never mentioned is the different case, and there the
  containers the path needs are created. A pointer naming an **array position this runtime's own RFC
  6901 resolution does not address** is refused on the other side of the same rule: the placement
  admits exactly the tokens the resolution admits (`evaluation.ArrayIndex`, exported for that one
  reason), because `strconv.Atoi` reads `00` and `+0` as zero and the evaluator reads neither, so a
  candidate placed there would sit where no condition ever looks.
- **The non-numeric dimensions are scoped to what the pack itself states.** Each stated member of an
  `in` operand, and the operand of an `equals` or `not-equals`, gets a candidate — inventing nothing.
  The negative witness is an **absence**: the pointer simply not present, which is a stated
  possibility that drives the unknown path. A synthesized non-member value is refused outright rather
  than offered behind a flag: it would be the one place this generator invents a value, and there is
  no dimension it reaches that absence does not. Where an array lies on the pointer's path the
  withholding is **declined and reported** under its own name, because an array member cannot be
  withheld without renumbering every element past it — which changes more of the base than the one
  answer withheld, exactly as overwriting a scalar would. A pointer the base never states is a
  different thing and is silent: there is no answer to withhold, so nothing was declined. Evidence
  availability is a separate axis — three tri-states per declared requirement, facts held at the
  base — and is never crossed with the numeric lattice, because crossing two one-factor axes is the
  cross product option G refuses.
- **Draft RFC 0008 quantifiers are out of scope and reported as skipped.** The walk descends only
  through `all`, `any`, and `not`, exactly as ADR-0023's does, so a comparison inside a `where` or an
  `at` derives no candidate — and an element-relative pointer has no place in a flat facts document
  to derive one at. A pack stating one is reported with the dimension named, on ADR-0022's
  skipped-not-passed precedent, never silently derived around. The detection is keyed to the
  quantifier operator names **and to the positions a condition can occupy**, over the same
  enumeration: a condition-shaped object carried as an operand *value* is data, and reporting a
  quantifier for it would announce a skipped dimension where nothing was skipped — and, in
  `packs lint`, would skip the fact half of a pack that states no quantifier, leaving a genuinely
  unproduced pointer unreported behind it.
- **`evaluation.DecimalString` is a new export, because `DecimalKey` cannot render.** `DecimalKey`
  returns `big.Rat`'s canonical string — `"81/2"` for 40.5 — which is an *identity* key and a
  spelling §7.4 declines to compare. Emitting a derived value needs a §2.2 writer:
  `-?(0|[1-9][0-9]*)(\.[0-9]+)?`, exact, at the fewest digits that are exact, and **returning false
  rather than rounding** for any value whose expansion does not terminate. A rounded value is a
  different number, and a generator that quietly substituted one would offer an input the pack's
  literals do not imply. `evaluation.DecimalValue` and `evaluation.PointerTokens` are exported beside
  it for the same one-statement reason the four ADR-0023 exports carry: without them this package
  would hold a second decimal admission (`big.Rat`'s own `SetString` reads `"1/3"` and `"1e5"`) and a
  second implementation of the `~1`/`~0` escapes, and a pointer the two escaped differently would
  place a fact where no condition looks for it.
- **`siteStage` and `exercisedBy` are deliberately not reused.** Eligibility is a property of an
  *expectation*, and a candidate has none. Tagging a candidate with a stage would be the generator
  implying which §8 stage a row reaches, a reachability claim neither this record nor ADR-0023
  permits. The stage may appear in *rationale prose* — "this comparison sits in an exception's
  `when`" — because that is a fact about the pack.
- **Write guards, and one new refusal.** `--write -` goes to stdout; `--write <file>` goes through
  the same exclusive `writeNewFile` open every generated artifact uses, and a remote path is refused.
  The new one: **a destination this configuration declares as a pack, a matrix, a graph, or a `rows`
  document is refused by name.** The exclusive open cannot make that refusal — it rejects an existing
  file with an I/O error and would happily *create* a declared matrix that does not exist yet, which
  is the case a reviewer would never notice. The check resolves the destination rather than comparing
  its text, so a declared document reached by a spelling that leaves the project directory and comes
  back is refused as the same file — and it resolves **both** ends through their symlinks, the
  destination by resolving its deepest *existing* ancestor and rejoining the tail that is not there
  yet, so a symlink aliasing the configuration's own directory is not a second spelling that walks
  past the check. Lexical comparison alone cannot refuse that one, and neither can the inode check
  behind it: a destination that does not exist has no inode to compare, and "the declared matrix that
  is not there yet" is precisely the file this refusal exists for. Both spellings are asked and
  either refuses, because a collision refused wrongly costs a rename and one missed writes
  machine-supplied inputs over reviewed law.
- **The report and the emitted document are two artifacts.** `--format` renders the report *about*
  the run — counts, the pack identities read, every skipped dimension — and `--write` emits the
  candidate document. `--write -` and `--format json` are refused together, because one stream cannot
  carry both; when the document goes to stdout the **report goes to stderr**, so a piped stdout is
  exactly the document's bytes and the skipped dimensions are still stated. Emitting the document and
  swallowing the report would be silence over a gap, which is what ADR-0012's demotion discipline
  refuses. The **pack's** provenance lives in the report and not in the document — which document was
  read, at which identity and version — because the document exists to be edited into a matrix, so a
  member naming the pack would either be pasted into a row that has no place for it or be silently
  dropped. The candidate's own `origin` is the exception and travels *in* the document, because
  `origin` is a member a row carries: it survives the edit, which is precisely what makes the count
  below possible. A pack **digest** in the document was considered for
  PR #94's replay-tuple discipline and refused: it would add a staleness failure mode that nothing
  enforces to a file whose whole purpose is to be edited and thrown away.
- **The run has no verdict and the exit code is neutral.** A pack that derives nothing is reported
  `skipped`, never `failed`; an unreadable declared pack is reported with the read failure named and
  `packs validate` pointed at, because duplicating that verdict here would give a generator a gate's
  exit code. Past `--max` (500 by default) the run **refuses rather than truncates**, naming the
  flag: a truncated candidate set looks exactly like a complete one, and a reviewer cannot tell that
  the dimensions past the cut were never offered. The cap is **charged as candidates are composed**,
  not read off the finished document: a cap checked at the end bounds what is returned and nothing
  about the work done to reach it, which would make "lower `--max`" advice that changes no cost. So
  the run stops deriving the moment the count a caller asked for is reached, and refuses there —
  naming the cap rather than a total, because the total is exactly what a run stopped at the cap did
  not go on to find out. A **non-positive `--max` is refused**, not read as
  the default: a run asked for at most zero candidates was asked for nothing, and answering it with
  five hundred is the silent substitution this family refuses everywhere else. Only an explicit unset
  sentinel means "state no bound of my own", and no command line produces it, because `--max` is
  registered carrying the default.
- **A 16 MiB output budget bounds the emitted document, beside the count.** `--max` bounds a
  *count*, and a run's size is that count times its base row — and a base row is bounded only by
  `MaxMatrixBytes`, so a three-megabyte reviewed row under the default count is gigabytes of output
  and gigabytes of memory to compose it in. It bounds the **written** form — what the emitter puts
  in the file, indentation and all — and not a compact measure of the same values, because
  indentation is not a constant factor: it multiplies with nesting depth, so a base row a hundred
  containers deep wrapping very many one-character tokens is legal on every §2.1 carrier bound,
  under a megabyte composed, and hundreds of megabytes written. A budget charged compact would admit
  exactly that document. The budget sits on `MaxMatrixBytes`' own footing and is **charged as each
  candidate is composed** rather than measured once the document is whole: at most one candidate's
  encoding is held while it is measured — the encoder materializes that one in order to indent it,
  and the counter it is written through keeps nothing — so the document's own bytes are never
  accumulated and the refusal fires before a document that size exists. What the refusal names is a
  **bound and not a measurement**, and it says so: each candidate is charged its written encoding
  plus a fixed 128-byte envelope for the framing around it, deliberately more than that framing
  costs, because a byte budget that under-charges is the only kind that fails. Crossing it refuses
  the run whole — naming that bound, the budget, and what each remedy actually does: a narrower
  `--base` row is what lets a run of this shape finish, and a lower `--max` stops the derivation
  earlier at the cost of a refusal naming the cap instead. It writes nothing and truncates nothing,
  for exactly the reason `--max` does not truncate.
- **A compared literal or operand longer than 128 bytes derives no candidate, and is reported.** §2.1
  bounds an authored string only at a megabyte, and ADR-0023 already determined that a report
  repeating such a string is a size defect; a *candidate* would repeat it in an id, in a rationale,
  and in a facts document. A value no reviewer can read is a candidate no reviewer can review, which
  is this surface's whole purpose. The bound is applied over **every spelling** a boundary's sites
  authored, on `groupStepPrecision`'s reasoning: one value spelled `"1"` in one rule and with a
  hundred and forty trailing zeroes in another is one boundary, so reading the bound off the
  first-declared spelling would make reordering those two rules decide whether the pointer derives a
  lattice at all. The oversize spelling is reported either way; the readable one still derives, and
  the candidates render in it.
- **Three further refusals, each reported under its own name.** They are recorded here because a
  refusal a record does not name is a behavior nobody agreed to:
  - **The root pointer.** A condition may compare the whole facts document at `""`, and the reason
    for refusing is *replacement*, not addressability: a facts document CAN be a scalar the empty
    pointer selects, and a row can carry one. Placing there would replace the whole document with
    that one value, leaving no base assignment for the candidate to hold the pack's other pointers
    at, so the one-factor-at-a-time rule could not be stated of it. A path that is neither empty nor
    rooted at `/` is refused beside it, because RFC 6901 resolves it against nothing.
  - **A consulted pointer longer than 128 bytes**, on the literal bound's own reasoning: the pointer
    is repeated in the candidate's id and in its rationale.
  - **A declared evidence requirement whose id is empty or longer than 128 bytes.** Neither can be
    named in an `evidenceAvailability` a reviewer could check, so the axis is reported rather than
    silently left out.
- **The output is deterministic.** No clock is read, and no range over a map decides anything a run
  writes: the maps in the derivation are looked up in or copied through, the one place a map's keys
  become output sorts them first (the `origins` count), order is otherwise first-occurrence walk
  order across pointers and numeric order within one, and object members are rendered in the
  encoder's own sorted order. Two runs over an unchanged pack write identical bytes,
  so a re-run in the middle of a review produces no diff to explain.
- **This record amends nothing in ADR-0014, and generated rows get no special credit.** ADR-0014's
  determinations govern *witnesses*, and a candidate has no expectation to witness with — the
  coverage derivation already skips rows without one, so generated output is invisible to coverage
  under ADR-0014 unmodified. ADR-0023's prohibitive half, "never by what it *produced*", stands
  verbatim: this generator runs no evaluator, so nothing it emits is an evaluator's product. A
  candidate that becomes a row is **just a row**; crediting generated rows differently in the
  coverage report would build the very metric that rewards the rubber-stamp.
- **The `test_pack` prompt gains no clause here.** ADR-0023 set the precedent, citing ADR-0016's:
  the prompt's numbered list is method ([ADR-0008](0008-mcp-prompts-authoring-method.md)) and is
  amended there. The follow-up adds one step after `packs suggest` — fill each candidate's
  expectation from the policy text under the existing arbiter rule, and **delete the ones the policy
  does not decide**.

### The failure mode this creates, and what is and is not done about it

A generator whose expectations get rubber-stamped is ADR-0014's circular oracle with better coverage
numbers. That record already conceded nothing mechanical closes it. This section states precisely
what this design does, so no reader takes a safeguard for a solution.

**What resists it.** The generator never runs the evaluator, so its output contains no evaluator
product — a real but bounded property that declines to *shorten* the loop rather than closing it.
The expectation is absent rather than sentinel, so it must be authored — four members for an
outcome disposition — and there is nothing to overwrite. One factor at a time, with no invented base, keeps each candidate's point legible; a
reviewable candidate is one somebody can actually refuse. And `origin` survives into the result
payload and into a rendered count, so a suite where 40 of 45 rows declare a generated origin is
visible to a reviewer and to CI.

**Why the human half is worth reserving at all.** The clean-room study `MIRROR-AGREEMENT.md` in the
evaluator-experiments repository is the supporting evidence: a second author given only the policy
text reproduced six readings the text determines and diverged on six it does not. That independent
second reading is exactly the role a human expectation plays against a machine-supplied input — it
is where the text's determinacy is tested, and it is the half a generator cannot stand in for.

**What does not.** A determined agent can run each candidate through the experimental evaluator and
paste what it produced. Nothing here stops that. More basically: **this makes coverage cheaper to
reach, and cheaper coverage under unchanged review discipline is worse-justified coverage.** That is
the cost this record accepts, not a risk it mitigates.

**The falsifier, registered here.** The honest claim is that the origin count **measures**
rubber-stamping rather than preventing it — which is exactly why gating on `origin` would be
self-defeating. A follow-up study can measure the *acceptance rate* of generated candidates
(expectation written versus candidate deleted) and the edit distance between a generated facts
document and the facts that reached the suite. **An acceptance rate near 100% falsifies this record's
premise**, because it would mean nobody is exercising the judgement the design reserved for them.
The corpus study's own lesson — count distinct probes, not runs — is the method that study should
use.

### Consequences

- Good, because the gap ADR-0023 names becomes a gap somebody is helped to close, in the one half a
  machine can supply honestly. Writing a facts document at a stated value is transcription; deciding
  which side of a threshold that value falls on is not, and only the second is left to a person.
- Good, because the two refusals that make a candidate not-a-row are refusals that already existed.
  No new enforcement code was written for the load-bearing property, so no future edit can relax it
  without visibly changing what a matrix is.
- Good, because the lattice covers a class the coverage report cannot demand. The band between two
  literals draws no probe under ADR-0023 and is where the corpus study found most records sitting;
  an offer can reach it where a demand cannot.
- Bad, because it makes coverage cheaper to reach, and cheaper coverage under unchanged review
  discipline is worse-justified coverage. Stated above rather than mitigated, and the falsifier that
  would show it happening is registered above too.
- Bad, because the generator derives only edges the pack's literals *state* and the bands they
  partition. It cannot derive an edge that lives only in policy prose and appears in no literal —
  the corpus study's sharpest finding is exactly that case, an unstated 39 boundary that drew no
  probe anywhere. This is the same class of limit ADR-0023 recorded for itself: a threshold moved
  from 70 to 71 derives values around 71 and says nothing about whether 71 was the right number.
- Bad, because a candidate whose facts carry only the varied pointer will often evaluate to unknown
  or to no match. That is honest rather than useless — the generator claims nothing about what a
  candidate produces, and the rationale says so — but it does mean the default output is less
  immediately usable than `--base` output, and `--base` needs a matrix that already has a reviewed
  row in it.
- Bad, because a fourth artifact shape now exists in this repository — pack, matrix, lock, and now
  candidates. The mitigation is that it is deliberately *not* interoperable with the third: it is
  refused everywhere a matrix is expected, which is the property that made it worth a new shape
  rather than a variant of an existing one.
- Neutral: `origin` was already a member of the case carrier and already echoed into results, and it
  already loaded silently in a project matrix while meaning nothing. This record gives it a meaning
  and a count without changing what loads.
- Revisit when the MCP tool is asked for (option I's condition: the CLI has been lived with, and the
  prompt clause has landed in ADR-0008 so the deletion step is method rather than hope); when the
  acceptance-rate study reports; or if the type probe ADR-0023 named as a follow-up is taken up, at
  which point a candidate placing a JSON number at a compared pointer becomes derivable and this
  record's refusal to invent values has to be re-argued for it.

## More information

`internal/project/candidates.go` carries the derivation — the lattice, the membership walk's leaf
handler, the one-factor composition, the placement rules, and the emitted document type — beside
ADR-0023's coverage derivation in the same package. `internal/project/coverage.go` carries the split
walk (`packConditions` and `walkConditions`) that both leaf handlers share, and
`internal/project/candidates_test.go` carries the golden that holds the split to a refactor.
`internal/evaluation/condition.go` exports `DecimalString`, `DecimalValue`, and `PointerTokens`, each
with the reason it is exported. `internal/cli/packs.go` carries the command and its write guards, and
`internal/project/project.go` the `DeclaresOutputPath` check behind the declared-document refusal.
[ADR-0023](0023-boundary-probes-for-ordered-comparisons.md) is the record whose option E this one
distinguishes rather than reverses; [ADR-0014](0014-matrix-coverage-report.md) is the record whose
circular-oracle concession this one inherits and does not repair;
[ADR-0021](0021-run-the-declared-matrix-over-mcp.md) is the CLI-before-MCP precedent;
[ADR-0022](0022-producer-lint.md) is the skipped-not-passed precedent. The cross-vendor adversarial
review this decision requires attaches to the pull request, not to this file (`docs/adr/README.md`,
"Review of material decisions").
