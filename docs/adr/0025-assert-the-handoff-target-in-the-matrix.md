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

Chosen option: **A**. Every other option is refused for one load-bearing reason.

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

**G is refused as out of scope, on ADR-0023's own precedent for the same surface.** A graph's `rows`
document already carries `expectedNodes`, and a per-node target assertion is a different derivation
with a different carrier — a composite's headline is not one pack's evaluation, and which node's
target a headline should be held to is a question this record has not answered. Named as a
follow-up, not smuggled in.

Settled determinations:

- **The member is `expectedHandoffTarget` on the matrix row, and it has three states.** Absent
  asserts nothing. The JSON literal `null` asserts that the evaluation reports **no** target at
  all. An object states `kind` and `name`, both required and neither empty, compared by exact string
  equality against the evaluation's top-level `handoffTarget`. Absent and `null` are two different
  statements and the carrier keeps them apart: the member is `json.RawMessage`, which is nil when
  the member is missing and the four bytes `null` when it is present and null.
- **Absent is byte-identical to today, in the payload and in the exit code.** A row that states
  nothing carries neither new result member, produces no new line on the human surface, and is
  judged by exactly the comparison it was judged by before. That is the property that makes this
  additive rather than a migration, and it is asserted in a test rather than assumed.
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
  its reason for existing.
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
  bundled row can carry this member and the corpus run is unchanged in every byte. That is the shape
  of the thing rather than a restriction imposed on it: an escalation target is a fact about the
  pack that declares it, and the corpus's packs are fixtures of the specification, not a project's
  policy.
- **The output is additive and `outputVersion` stays `"2"`.** Two members appear on a row result
  only when that row asked for them, which is weaker than the additive member ADR-0023 already
  precedented and squarely inside VERSIONING.md's backward-compatible-addition rule. The MCP
  response bound ADR-0023 spent a determination on is not moved: the added bytes are two short
  renderings per **asserting** row, and no string is repeated into a detail.
- **This asserts the target as evaluated, and nothing beyond it.** It compares the destination the
  pack **configures** against the destination a row's author wrote down. It does not verify that any
  handoff was delivered, that the named role or queue exists, that anyone acted on it, or that the
  target is the right one — a requested handoff is a request, and §8.3 is explicit that it is not
  evidence one occurred. A green row means the pack still names the destination the author recorded;
  it means nothing else, and no rendered line says otherwise.
- **The graph surface is untouched.** `internal/graph`'s `rows` carrier gains no member and its
  result rows gain none, for the reason option G is refused for.
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
  for is the one member of the evaluation payload that says where a human is being sent.
- Good, because it costs existing suites nothing. A matrix written before this record is judged by
  the comparison it was written for, byte for byte, and the two new result members do not appear.
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

`internal/evaluation/corpus.go` carries the carrier member, `DecodeHandoffTarget`, and the second
comparison in `RunCaseAdmitted`, beside the disposition comparison that both the bundled corpus and
a project matrix run through. `internal/result/result.go` carries
`result.HandoffTarget.Canonical` — the one writer of both sides — and the two row members.
`internal/project/matrix.go` carries the load-time refusals: the companionship rule and the shape
check through that one decoder. `internal/cli/render.go` prints the pair.
[ADR-0010](0010-evaluator-aligned-to-core-0.2.0-draft.md) is where the target came to be reported
outside the disposition; [ADR-0014](0014-matrix-coverage-report.md) and
[ADR-0023](0023-boundary-probes-for-ordered-comparisons.md) are the never-gate line this record
stays on the other side of, and neither is amended;
[ADR-0024](0024-suggest-candidate-row-inputs.md) is the record whose generator emits no expectation,
and it emits none of these either. The cross-vendor adversarial review this decision requires
attaches to the pull request, not to this file (`docs/adr/README.md`, "Review of material
decisions").
