---
status: accepted
date: 2026-08-09
deciders: maintainer
---

# Let a matrix row assert the handoff target, because no disposition can carry it

## Context and problem statement

Study 013 in the evaluator-experiments repository ran a frozen preregistration whose holdout set
that study's cross-vendor reviewer wrote adversarially. Cell h02 is a pack mutation that corrupts
**only** `escalation.target.name` — one authored string, nothing else. Every §8.3 disposition the
mutated pack produces is byte-identical to the correct pack's: `kind`, `outcomeId`, `reasons`,
`handoff.state`, and `handoff.triggeredBy` are all unchanged, because §8.3 keeps the configured
target **out** of the disposition object on purpose, and this runtime reports it as the separate
envelope member `handoffTarget` for exactly that reason
([ADR-0010](0010-evaluator-aligned-to-core-0.2.0-draft.md), `result.HandoffTarget`).

A `jpack packs test` instance matrix therefore cannot see the mutation. Every row still passes,
because every row compares a disposition and the disposition is right. The corruption was caught
only downstream, by an external harness, when the corrupted target reached a tool argument — which
is to say it was caught by an integration nobody had asked to be a regression test, and only after
the wrong destination had already been named.

The gap stated once: **a matrix that does not project the handoff target cannot regression-test the
handoff target.** The member is reported, it is authored in the pack, it decides where a request
goes, and it is the one part of an evaluation's payload no expectation could reach.

## Decision drivers

- The disposition comparison is byte-exact and stays that way. Whatever is added must not weaken
  what §8.3 requires two implementations to agree on, and must not move anything into or out of the
  disposition object.
- Every matrix in existence keeps passing. A regression surface that fails suites it never asked
  anything of is a worse surface than the gap it closes.
- The demotion discipline of [ADR-0012](0012-jpack-project-convention.md): silence over a gap is
  what this repository refuses. A member nothing can assert about is that silence.
- The reference-only rule of [ADR-0011](0011-first-evaluator-conformance-claim.md): nothing here may
  state or imply that a matrix asserting the target is a correct one, or that this runtime delivered
  anything to it.
- One statement of what a well-formed expectation is, however many readers it has — the rule
  [ADR-0023](0023-boundary-probes-for-ordered-comparisons.md) applied to a comparison and
  [ADR-0024](0024-suggest-candidate-row-inputs.md) inherited for a decimal writer.

## Considered options

- **A. An optional per-row `expectedHandoffTarget`, asserted only where a row states it, gating
  exactly as `expectedDisposition` does** (chosen).
- **B. Status quo:** document the gap and leave the target unassertable.
- **C. Fold the target into `expectedDisposition`,** comparing it as part of the disposition's
  canonical bytes.
- **D. Always assert it:** every row's produced target compared against the pack's declared one.
- **E. Report it as coverage,** deriving a probe demanding that some row exercise the target.
- **F. Echo the produced target on every row result,** asserted or not, so a diff of two runs shows
  the mutation.
- **G. Extend the assertion to the graph surface's `rows` document** in the same change.

## Decision outcome

Chosen option: **A**. Every other option is refused for one load-bearing reason, except **G**, which
is deferred for one — and the difference between a refusal and a deferral is stated rather than
blurred, because a deferral leaves the gap open on that surface.

**B is refused because the gap is not a documentation defect, and saying so is not a formality.**
The status quo is a real option here — every other record in this family refused a gate and settled
for a report — so it has to be weighed rather than assumed away. It loses on what the h02 story
actually shows. A documented gap is closed by a person remembering it at the moment they change a
pack, and the mutation this exists for is the one nobody looks at: the disposition is right, the
suite is green, and the only signal is a wrong destination reaching an integration downstream. This
runtime already refuses that trade elsewhere — ADR-0012's demotion discipline is that silence over a
gap is the failure mode, and ADR-0023 declined to leave an ordered-comparison boundary to prompt
text on exactly this reasoning. What tips it past those precedents is that the repair is a **row
member a person writes**, not a derivation that has to be argued for: the whole cost of closing this
is four tokens in the rows that already exist, and refusing to spend them would leave a member the
runtime reports, the pack authors, and nothing can hold to anything.

**C is refused because §8.3's disposition composition is normative and the target is deliberately
outside it.** That section enumerates the disposition's members and this runtime's `Disposition`
type carries "these members and no others"; the target is reported beside it precisely so that "a
disposition can never disagree with the pack it came from". Comparing the target *as part of* the
disposition's canonical bytes would mean either canonicalizing a document §8.3 does not define — the
byte comparison two implementations are held to would stop being the one the specification names —
or teaching the comparator to canonicalize one shape and compare another, which is the same thing
with the disagreement hidden. The target's separateness is not an oversight to be repaired; it is
the property that makes the disposition portable. So the assertion is a **second** comparison,
reported as its own pair of members and stated in its own row member, and the disposition comparison
is byte for byte what it was.

**D is refused because it breaks every existing matrix.** A row that says nothing about the target
would begin failing the moment the pack's target changed for a reason the row was never about, and
an always-assert rule has no absent state to fall back to: a suite written before this record
existed would have to be edited row by row before it could pass again. Worse, the assertion would
then have no author — the runtime would be comparing the pack against itself, which is
[ADR-0014](0014-matrix-coverage-report.md)'s circular oracle wearing a gate's exit code. What makes
this assertion worth anything is that a person wrote the destination down.

**E is refused because coverage informs and never gates, and this must gate.** ADR-0014 settled that
line and [ADR-0016](0016-graph-rows-coverage-report.md) restated it; ADR-0023 kept it even where a
gate was the strongest candidate yet. Nothing here reopens it, and this record is careful not to
appear to: **`expectedHandoffTarget` is not coverage.** A derived probe is a demand the runtime
makes of a matrix from the pack's own declarations, and it is informational because the runtime is
in no position to insist. An expectation is a **statement the row's author made**, and a statement
its author made that turns out to be false is a mismatch — the same status, the same summary, the
same exit code an `expectedDisposition` mismatch produces. ADR-0014 and ADR-0023's never-gate stance
governs derived probes and is untouched by a member a human writes. The two live side by side in the
same report and mean different things, which is why the wording of every line this change adds
avoids the word "coverage" entirely.

**F is refused because an echo is not a test.** Echoing the produced target on every row would put
the mutation in the payload, where a diff of two runs could find it — but "a diff of two runs" is
not a CI gate, it is a person remembering to look, and the h02 story is precisely a case where
nobody looked at the right place. It would also change the bytes of every row result for every
existing suite, which is the cost of D without D's (rejected) benefit. The produced target is
reported **only** for a row that asked about it, so a suite that asserts nothing reports what it
reported before.

**G is deferred rather than refused, and the reason is a real open choice rather than a missing
mechanism.** An earlier draft of this record said the graph surface had no target to assert, and
that was wrong: `internal/graph/evaluate.go` already binds the composite's headline `HandoffTarget`
to the declared result node's, so a headline-level assertion is *available* today and would need no
new plumbing. What is unsettled is which assertion is the right one. A graph row can hold the
headline — one assertion per row, cheap, and blind to every upstream node — or it can hold each node
beside `expectedNodes`, which is where the graph surface already puts a claim about a decision that
fed the headline and where ADR-0016 put its own per-node coverage. Choosing under the pressure of
this record would settle a graph question inside a pack-matrix decision, and ADR-0023 set the
precedent for not doing that.

The cost of deferring is stated rather than implied: **a graph matrix stays blind to a target-only
mutation**, which is the exact defect this record exists to close, on a surface where it is
arguably worse — a composite's escalation is the one a caller is furthest from. The mitigation is
not a mitigation: where a graph's nodes are packs a project maintains, the assertion belongs in each
pack's own matrix, which does have it, and `docs/building-with-packs.md` says so at the point a
builder would otherwise assume the graph covers it. A test pins the deferral in the direction that
can rot — a graph row stating `expectedHandoffTarget` is refused as the unknown member it is — so
one surface cannot quietly acquire half of this.

Settled determinations:

- **The member is `expectedHandoffTarget` on the matrix row, and it has three states.** Absent
  asserts nothing. The JSON literal `null` asserts that the evaluation reports **no** target at
  all. An object states `kind` and `name`, both required and neither empty, compared by exact string
  equality against the evaluation's top-level `handoffTarget`. Absent and `null` are two different
  statements and the carrier keeps them apart: the member is `json.RawMessage`, which is nil when
  the member is missing and the four bytes `null` when it is present and null. It carries
  `omitempty` for the same reason: without it, marshaling a row that asserted nothing writes
  `expectedHandoffTarget: null`, which reloads as the assertion that the evaluation reports no
  target, so a round trip through the carrier type would invent an expectation nobody wrote.
- **It moves `matrixVersion` to `"2"`, and an earlier draft of this record was wrong not to.** A
  matrix is a **closed input**, and VERSIONING.md states the rule for those in the sentence this
  change would otherwise have contradicted: their schemas are closed, so an older reader *rejects* a
  document carrying a member it does not know rather than ignoring it, and adding one therefore
  moves the version whatever the addition is — as `graphs` moved `configVersion` to `"2"` and
  `audit` moved it to `"3"`. Reading the additive-output rule onto an input is the exact conflation
  that document names as a live source of error. So: `"1"` and an omitted version are the shape
  written before this record and stay target-free; `"2"` admits the assertion; and a `"1"` matrix
  that states one is refused **by name**, naming the version it would take, because
  "`expectedHandoffTarget` is not a member" is a false sentence to print at an author whose only
  mistake was not moving a version string.
- **The declared version is settled before anything version-specific decodes it.** The check runs
  off the carrier-decoded document, ahead of the typed decode, so a matrix declaring a version this
  runtime has never heard of is told exactly that rather than told that one of its members is
  unknown — true, uninformative, and pointing at the wrong repair. The ordering is a determination
  and not an implementation detail: it is what makes a future version's document produce an
  actionable refusal from an older binary instead of a puzzling one.
- **A member spelled in another case is refused rather than read as the member it folds onto.**
  `encoding/json` matches member names case-insensitively **even under `DisallowUnknownFields`**, so
  `{"Facts":…}` and `{"ExpectedHandoffTarget":…}` decode into the exact members, and a document
  carrying both spellings has one silently overwrite the other in map order — an assertion an author
  wrote replaced by one they did not. A closed shape that admits an alias is not closed. The member
  names are therefore checked against the carrier-decoded document, where every authored name still
  exists verbatim, before anything is decoded into a Go type. This fixes the whole row shape rather
  than this record's member alone, because the defect was never specific to it; the strict decode
  stays, as the type check it also is.
- **The expectation is decoded from the token stream, not into a struct.** The same case-folding
  applies inside a target object, and two more defects come with it: `encoding/json` accepts a
  duplicated member silently, and it stops at the end of the first value, so trailing JSON rides
  along unread and `null {"kind":"queue","name":"Ops"}` reads as an assertion of no target. None of
  the three is expressible as a decoder option, so `DecodeHandoffTarget` reads tokens, matches
  member names exactly, refuses a repeat, and requires EOF.
- **Absent is byte-identical to today for an otherwise-valid matrix, in the payload and in the exit
  code.** A row that states nothing carries neither new result member, produces no new line on the
  human surface, and is judged by exactly the comparison it was judged by before. That is the
  property that makes this additive rather than a migration, and it is asserted in a test rather
  than assumed. The qualifier is load-bearing and is stated here rather than only in the
  consequences: two documents that this change *does* newly refuse are a matrix whose member names
  differ from the exact spellings by case, and any document carrying an unpaired surrogate escape.
  Both were being misread before, which is why they are refused now — but "byte-identical" without
  the qualifier is false of them, and a determination that has to be corrected in a later paragraph
  is a determination that was written too broadly.
- **It is an assertion, so it gates.** A mismatch moves the row to `mismatch`, the pack entry to
  `mismatch`, the run to `mismatch`, and the exit code to 1 — the same path an expected-disposition
  mismatch takes, through the same counters. Nothing new decides a status; the existing row status
  does.
- **It rides only beside `expectedDisposition`, refused at load.** A refused evaluation produces no
  disposition and therefore no target beside one, so a target assertion on a row expecting an §8.4
  error class is unsatisfiable rather than merely unmet. `LoadMatrix` refuses it by name, on the
  precedent `expectedNodes` set on the graph surface ("a refused run produces no node dispositions
  to compare") — and refusing it at load rather than reporting it as a mismatch is the difference
  between naming a defect in the row and reporting a result about the pack.
- **A row may assert it whatever the row's disposition kind is.** An `unresolved` row, a
  `not-applicable` row, and an `outcome` row can each state one, because §8.1 makes
  `not-applicable` an escalation trigger a pack may name and an outcome request no handoff at all.
  The two directions are symmetric and both are mismatches: an evaluation with no target against a
  row expecting one, and an evaluation with a target against a row expecting `null`.
- **The expectation's shape is decoded once, by `evaluation.DecodeHandoffTarget`, read by two
  callers.** `LoadMatrix` calls it so a malformed assertion is a carrier defect `packs validate`
  reports before `packs test` runs anything; the row comparator calls it so a row reaching the
  comparator by any other route is held to the same shape. One gate, however many readers — the rule
  `DecodeDisposition` already carries, and the reason it is exported.
- **The `kind` vocabulary is deliberately not restated in Go.** The pack schema enumerates
  `human-role`, `queue`, and `system`, and it has already held every evaluated pack to that list; a
  second copy here would be a vocabulary this package does not own, drifting silently the first time
  the specification adds a fourth. A row naming a kind outside the enumeration is reported as the
  loud mismatch it is, with both renderings beside it. What **is** enforced is the shape no target
  can lack: both members present, neither empty, no unknown member. That is the same reason the
  reason-and-kind vocabularies of §8.3 *are* stated in `result` — those are the gate a disposition
  passes on its way to bytes, and this is not a gate anything passes on its way anywhere.
- **Both sides are rendered by one writer**, `result.HandoffTarget.Canonical`: the RFC 8785 form of
  the `{kind, name}` object, and the JSON literal `null` where there is no target. A row's
  expectation and an evaluation's report therefore cannot differ because two encoders disagreed,
  which is the same discipline the disposition comparison already follows. The canonicalization is
  the existing `internal/jcs` encoder over an object of two strings — inside the value space that
  package implements, and stated here because that package's doc comment names the disposition as
  its reason for existing. That writer also **refuses a target with an empty `kind` or `name`**,
  exactly as `Disposition.Canonical` refuses a disposition §8.3 does not admit: §8.1 states both
  members, the engine builds no such value, and an exported type can be handed one — which would
  put a target no pack can declare on both sides of a comparison, where it compares equal to itself
  and passes.
- **A reported target is bounded, and a bounded rendering is a display value and never an equality
  key.** This is the correction two rounds of adversarial review forced, and the arithmetic is worth
  recording rather than the conclusion alone: a pack may configure a target whose `name` §2.1 admits
  at a megabyte, a matrix may declare `MaxMatrixCases` — ten thousand — rows, and every asserting
  row retains two renderings. Uncapped, ten thousand rows asserting `null` against such a pack build
  a report in the gigabytes out of inputs every carrier limit admits, and the MCP surface's 16 MiB
  check runs only once the whole thing exists. An earlier draft of this record claimed the added
  bytes were "two short renderings"; that claim was false, and it is withdrawn here rather than
  softened. So each rendering is capped at `result.HandoffTargetBudget` bytes on ADR-0023's own
  shape — the value whole within the budget, and beyond it a prefix, an ellipsis, and the first
  sixteen hex digits of the SHA-256 of the full canonical bytes.

  A second draft then made the opposite mistake, and it is recorded because the mistake is
  instructive: it decided the comparison **on the capped rendering**, reasoning that the digest tail
  kept two capped targets distinct. It does, with overwhelming probability — and overwhelming
  probability is not the standard a verdict is held to. Sixty-four bits of digest deciding whether a
  suite passes is a probabilistic answer to a question that has an exact one, and this record exists
  precisely because a comparison that could not see a difference let a corrupted target through. So
  the two are separated: **the rendering is what the report shows, and the decoded values are what
  the comparison reads** — presence against presence, then each member in full. Nothing is lost by
  the split, and nothing is spent either: a row's expectation lives in the matrix, which
  `MaxMatrixBytes` bounds whole, so the total compared across a run is bounded by the matrix rather
  than by the pack, and a length mismatch settles a comparison before a byte of either is read.
- **The pack's own target is rendered once per admission, and no row renders or compares one.** The
  budgets above bound what a report *retains*; they say nothing about what producing it *costs*, and
  the second round of review found the gap between those two. §8.1 gives a pack one escalation
  target and every row of one matrix evaluates one pack, so ten thousand rows asserting `null`
  against a megabyte-long target canonicalize and hash the same authored string ten thousand times —
  ten gigabytes of work, while the retained bytes stay at a comfortable 2.6 MB and the aggregate
  budget never fires.

  The first repair for that was a single-entry memo keyed on the target's **content**, and the third
  round of review showed it fixed the arithmetic only for a matrix that uses one capability set.
  `Options.SupportedExtensions` selects a distinct *admission*; each admission decodes the pack
  separately, so two admissions hold target strings that are equal and separately allocated; and the
  memo's hit test then compared a full megabyte on every row after the capability set changed. One
  row with no extensions followed by 9,999 rows declaring one harmless extension is the same ten
  gigabytes, under every budget, and a test using one capability set cannot see it. **Content
  equality is not identity**, and a cache that has to prove a hit by reading both values has not
  removed the work — it has renamed it. The claim that a miss was "never slower" is withdrawn with
  it: a miss paid the rendering *and* the lookup, the lock, and the counter.

  So the rendering is a property of the **admission**, which is the immutable thing a row already
  selects by: it is computed once, where the pack is decoded, and stored on that admission. A row
  reads a field — no comparison, no hashing, no lock on the row path at all. The total is therefore
  at most `maxAdmissions` renderings of one authored string, however many rows run; past that bound
  admissions stop being cached and each row pays the pre-existing cost of re-validating and
  re-decoding the whole pack, which one rendering is a rounding error beside. A test asserts that
  the count does not move when the row count grows by an order of magnitude, and that it equals the
  number of capability sets the run used. It counts renderings rather than timing them, because a
  cache whose only evidence is a stopwatch is a cache nothing holds to its purpose — and it says
  plainly that the absence of a per-row *comparison* is structural rather than counted, since a test
  cannot assert the absence of work it has no hook for.
- **A second budget bounds the accumulation, charged as each row's result is composed.** The
  per-rendering cap bounds one row; what a report retains is a product of that cap, the row cap, and
  the number of packs a project declares. `MaxHandoffTargetReportBytes` bounds the total across the
  whole run — one counter, because a report is one document however many packs contributed to it —
  and it is charged **before** each rendering is retained rather than measured once the report is
  whole, on ADR-0024's reasoning: a budget checked at the end bounds what is returned and nothing
  about the memory spent reaching it. Crossing it **refuses the run** and writes nothing: a report
  cut short looks exactly like a complete one. It is a `Failure` and not a mismatch, because a
  mismatch is a statement about a pack and a row and "this report does not fit" is a statement about
  neither. It has no configuration surface, deliberately — a limit a project could raise is a limit
  an oversized report can ask to be allowed — and its number is derived from the other two rather
  than chosen as a preference.
- **An unpaired surrogate escape is refused at the carrier, for both sides of this comparison and
  for every other document.** Go's decoder replaces `"\ud800"` with U+FFFD without complaint, so a
  pack authoring a lone surrogate in its target name canonicalizes to the same bytes as an
  expectation carrying a literal replacement character: two different documents compare equal, and a
  byte comparison §8.3 requires to be exact quietly stops being one. RFC 8785 §3.2.2.2 makes such a
  value invalid rather than replaceable, so `carrier.Decode` terminates on it. The fix is at the
  carrier rather than at this comparison because the carrier is where both paths meet and because
  the defect was never specific to a handoff target — it reaches every pack, matrix, facts,
  evidence, configuration, and graph document this runtime reads. Nothing else about Unicode
  changes: an escape and the literal it names remain one string, and NFC and NFD remain two, because
  normalizing either toward the other is not something §8.3 asks for.
- **The report carries `expectedHandoffTarget` and `actualHandoffTarget`, and the detail does not
  repeat them.** The two renderings are their own members of the row result and are printed as one
  line on the human surface, beside the disposition pair and the error-class pair. The mismatch
  detail names the **shape** of the difference — the row expects no target and one was reported, the
  row expects a target and none was, or both name one and the two differ — because a target's `name`
  is an authored string §2.1 bounds only at a megabyte, and a detail that inlined it would put the
  same unbounded bytes in one payload a third time. That is ADR-0023's rendering-budget reasoning
  applied to the reason it exists rather than to its exact mechanism: the disposition comparison
  already reports its two byte sequences as members and says only "the canonical disposition bytes
  differ", and this follows it.
- **The bundled evaluation corpus cannot state it, and nothing was done to make it able to.** The
  carrier is shared — `evaluation.MatrixCase` is the corpus row and the project row, so a project's
  rows are judged by the corpus's own comparator rather than by a second implementation of it — but
  the corpus manifest's schema closes its case object with `additionalProperties: false`, so no
  bundled row can carry this member and the corpus run is unchanged in every byte — pinned at the
  row level and at the whole envelope a consumer receives. What the two carriers share is the fields
  the comparator reads and not the shape of a document: that schema separately *requires* `pack`,
  `origin`, `supportedExtensions`, `focus`, and `specSection`, which a project row need not declare,
  so lifting a row into a corpus was never the no-op an earlier draft of this record described. That is the shape
  of the thing rather than a restriction imposed on it: an escalation target is a fact about the
  pack that declares it, and the corpus's packs are fixtures of the specification, not a project's
  policy.
- **The reported pair appears together, and its third value is `unavailable`.** A row that asserts a
  target and whose evaluation is then refused produced nothing to compare. Reporting `null` there
  would state that an evaluation reported no target, which is a §8.3 statement no evaluation made;
  reporting the expected side alone would break the rule that the pair appears whenever the row
  asked. So both members are set to `unavailable` the moment a row is seen to declare an assertion,
  and each is replaced when there is something true to put there. The human surface spells it out —
  `unavailable (evaluation refused)` — because a reader meeting the bare word beside a target on the
  other side needs to know which of the two facts it is.
- **The output is additive and `outputVersion` stays `"2"`.** Two members appear on a row result
  only when that row asked for them, which is weaker than the additive member ADR-0023 already
  precedented and squarely inside VERSIONING.md's backward-compatible-addition rule. This is the
  **output** side and it is deliberately decided separately from the matrix's own version above:
  output is read by consumers that ignore what they do not know, and a matrix is a closed input read
  by a runtime that must not. The MCP response bound ADR-0023 spent a determination on is held by
  the two budgets rather than by a claim about how short the renderings are.
- **This asserts the target as evaluated, and nothing beyond it.** It compares the destination the
  pack **configures** against the destination a row's author wrote down. It does not verify that any
  handoff was delivered, that the named role or queue exists, that anyone acted on it, or that the
  target is the right one — a requested handoff is a request, and §8.3 is explicit that it is not
  evidence one occurred. A green row means the pack still names the destination the author recorded;
  it means nothing else, and no rendered line says otherwise.
- **The graph surface is untouched, and the deferral is pinned in the direction that can rot.**
  `internal/graph`'s `rows` carrier gains no member and its result rows gain none, for the reason
  option G is **deferred** for — deferred, not refused, and this determination says so because a
  record that refuses in one paragraph and defers in another has decided nothing. A test asserts
  that a graph row stating `expectedHandoffTarget` is refused as the unknown member it is, so one
  surface cannot quietly acquire half of this while the choice between a headline assertion and a
  per-node one is still open.
- **The `test_pack` prompt gains no clause here.** ADR-0023 set the precedent and ADR-0024 followed
  it, both citing ADR-0016's: the prompt's numbered list is method
  ([ADR-0008](0008-mcp-prompts-authoring-method.md)) and is amended there. A follow-up adds the
  step — where a pack declares an escalation target, assert it on the rows that reach it.

### Consequences

- Good, because the defect class Study 013's h02 cell registers becomes a row that fails in CI
  instead of a wrong destination discovered downstream. The test that pins it is written the way the
  study found it: the same matrix over the correct and the corrupted pack, green both times until a
  row asserts the target.
- Good, because the assertion is cheap and legible. A row gains four tokens, and what it gains them
  for is the one member of the evaluation payload that names the configured target downstream
  integrations may use. It is not always a person: §8.1's kinds are `human-role`, `queue`, and
  `system`, and this record claims nothing about what any of them does with a request.
- Good, because it costs an existing suite nothing — with the scope stated, because the unqualified
  version of this sentence is false. A matrix that was **otherwise valid** — exact member spellings,
  no unpaired surrogate escape — is judged by the comparison it was written for, byte for byte, and
  the two new result members do not appear. A matrix that was none of those things was being
  misread, and is now refused; that is the next consequence, not an exception hidden inside this
  one. The corpus payload is pinned at both levels — the rows and the whole envelope a consumer
  receives — to digests taken from the commit this branched from, so the claim is checked against
  bytes rather than against empty Go fields.
- Good, unexpectedly, because closing this required fixing two defects older than it: a "closed"
  matrix shape that accepted case-folded aliases for every one of its members, and a carrier that
  silently repaired invalid Unicode into U+FFFD. Both were found by the adversarial review of this
  change, both are refusals now, and neither is specific to a handoff target.
- Bad, because a matrix asserting a target needs `matrixVersion: "2"`, which is a two-step edit for
  an author who wanted one. The alternative was leaving a closed input's version standing while its
  shape changed, which is the conflation VERSIONING.md names; the refusal names the version, so the
  second step is told to the author rather than discovered.
- Bad, because refusing case-folded member names may refuse a matrix that loaded yesterday. Such a
  matrix was being *misread* yesterday — a member the author spelled `Facts` was silently accepted
  as `facts`, and a document carrying both had one overwrite the other — so the refusal is the bug
  fix and the previous acceptance was the defect. It is called out here because it is a behavior
  change nobody asked for.
- Bad, because a very long target is reported truncated, which is a rendering an author did not
  write, and the tail it ends in is a digest rather than the value. What is lost is readability of a
  value that was never readable at that length. What is **not** lost is correctness, and the reason
  is that the rendering decides nothing: an earlier draft of this record said the digest tail kept
  two capped targets distinct and concluded that "nothing is decided wrongly", which was a
  probabilistic guarantee dressed as an exact one. The rendering is display; the verdict is taken
  from the decoded values.
- Bad, because it asserts what the pack **configures** and not what happens next. Nothing here
  observes a delivery, and a project whose escalation target is correct and whose routing is broken
  gets a green row. The limit is stated on the surface as well as here.
- Bad, because it is opt-in, and an opt-in regression test protects only the suites that opted in.
  The h02 gap is closed for a matrix that asserts the target and open for one that does not. Making
  it mandatory is option D and costs more than it buys; deriving a demand for it is option E and
  crosses a line two records have held. What is left is a member, documented where matrix rows are
  documented, and a follow-up in the authoring method.
- Bad, because the runtime now compares two things per row where it compared one, and a reader of a
  mismatching row has one more place to look. The mitigation is that the second comparison is
  reported in the shape of the first — an expected/actual pair on its own line — and that it appears
  at all only for rows that asked.
- Neutral: `handoffTarget` was already reported on every evaluation payload and already rendered on
  the human evaluation surface. This record gives a matrix a way to hold it to something; it changes
  nothing about how it is produced, and the resolver is untouched.
- Revisit when the graph surface asks for the same assertion (option G's condition: a per-node or
  per-headline carrier is designed, rather than borrowed); if a future specification version moves
  the target into the disposition, at which point option C stops being a refusal and becomes the
  only correct answer; or if `escalation.message` is asked for next, which is the same shape of
  question about a different member and deliberately not answered here.

## More information

`internal/evaluation/corpus.go` carries the carrier member, `DecodeHandoffTarget` — the token-stream
decoder and its three refusals — and the second comparison in `RunCaseAdmitted`, beside the
disposition comparison that both the bundled corpus and a project matrix run through.
`internal/result/result.go` carries `result.HandoffTarget.Canonical` and `Rendered` — the one writer
of both *reported* sides, and the rendering budget — the `unavailable` constant, and the two row
members; `sameHandoffTarget` in `internal/evaluation/corpus.go` is the comparison, which reads the
decoded values and never those renderings. `internal/evaluation/engine.go` carries
`declaredHandoffTarget` and the per-admission fields that render one pack's configured target once
where the pack is decoded, and `reportedHandoffTarget`, which is the row path and does no work.
`internal/project/matrix.go` carries the version preflight, the exact-member check, and the
load-time refusals: the companionship rule and the shape check through that one decoder.
`internal/project/project.go` carries `MaxHandoffTargetReportBytes` and the injectable counter, and
`internal/project/test.go` charges it as each row is composed. `internal/carrier/decode.go` carries
the unpaired-surrogate refusal, which is where both sides of this comparison — and every other
document — stop being silently repaired.
[ADR-0010](0010-evaluator-aligned-to-core-0.2.0-draft.md) is where the target came to be reported
outside the disposition; [ADR-0014](0014-matrix-coverage-report.md) and
[ADR-0023](0023-boundary-probes-for-ordered-comparisons.md) are the never-gate line this record
stays on the other side of, and neither is amended;
[ADR-0024](0024-suggest-candidate-row-inputs.md) is the record whose generator emits no expectation,
and it emits none of these either. The cross-vendor adversarial review this decision requires
attaches to the pull request, not to this file (`docs/adr/README.md`, "Review of material
decisions").
