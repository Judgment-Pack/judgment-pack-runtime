---
status: accepted
date: 2026-08-07
deciders: maintainer
---

# Derive a boundary probe for every ordered comparison, witnessed by a row's facts

## Context and problem statement

A contributed case: a rule described as "5000 or more spend requires review" compares
`greater-than "5000"`. The description and the operator are each individually valid, they are
checked by nothing, and they disagree at exactly one input — an amount of exactly 5000. A matrix
with rows at 4999 and 5001 passes green and pins neither reading. The same defect is family index 0
of the transcription study's injected-defect set, and the study's own correct pack shows the shape
in the wild: one threshold compared at three sites with two operators.

The coverage report of [ADR-0014](0014-matrix-coverage-report.md) cannot see this. It derives
probes from declared outcomes and reachable §8 reasons, and by construction it never reads a
condition — a line [ADR-0016](0016-graph-rows-coverage-report.md) restates as "no condition
analysis, no fact-value reachability". The only ordered-comparison probe anywhere in this runtime
is a sentence in the `test_pack` prompt, and it probes a different defect (a JSON number where a
decimal string belongs). Advisory prompt text is exactly the quality control ADR-0014's own premise
says a deterministic runtime should not rely on.

## Decision drivers

- ADR-0014's driver, inherited whole: coverage is the one layer of matrix quality that is currently
  advisory and could be mechanical.
- The reference-only rule of [ADR-0011](0011-first-evaluator-conformance-claim.md): nothing here may
  state or imply that a covered matrix is a correct one.
- The demotion discipline of [ADR-0012](0012-jpack-project-convention.md): silence over a gap is the
  failure mode this repository refuses.
- Derived by declaration is not proven, so a derivation that demands a row must not demand one no
  author could justify.
- One derivation shared with the graph surface, so no two surfaces can disagree about what a pack's
  declarations reach.

## Considered options

- **A. One probe per distinct fact pointer and decimal value, at the literal, expectation-gated and
  facts-located, reported informationally** (chosen).
- **B. Status quo:** the boundary stays invisible and the prompt's type probe is the only
  ordered-comparison guidance.
- **C. Gate:** a missing boundary probe flips the run to `mismatch`.
- **D. One probe per comparison site,** carrying the site's operator in its identity.
- **E. A pair probe:** the literal and an adjacent value on the far side of it.
- **F. Include the nearest `description` text in the probe's detail.**
- **G. Extend the family to the graph surface,** one boundary per node.
- **H. Reuse ADR-0020's shape-keyed pointer collector** to find the comparisons.

## Decision outcome

Chosen option: **A**. Every other option is refused for one load-bearing reason.

**C is refused on ADR-0014's inherited grounds.** Coverage informs and never gates, and nothing
about this family reopens that. Recorded honestly, because it cuts the other way here: a boundary
probe is almost always constructible — a row author writes the facts document, and placing a decimal
at a pointer needs no cooperation from the pack's other conditions — which makes this the strongest
candidate yet for ADR-0014's "revisit when real projects ask for a gate" clause. That clause is
where the question belongs, not this record.

**D is refused because the sites ask one question of the facts.** The witness test's fact half —
some row's facts place this pointer's value at this literal — is a pure function of the pointer and
the literal, so a per-site partition would inflate the `n/m` denominator in proportion to how often
an author repeated a threshold. The study's correct pack compares `/vendor/riskScore` at `70` in
three places with two operators, and this derivation asks one question rather than three.

The sites do **not** ask one question of the expectation, and this record says so rather than
assuming otherwise. An earlier draft justified D's refusal by claiming that sites sharing a pointer
and a value have identical witness sets and are therefore always covered together and always missing
together. That claim is false: a row expecting `not-applicable` is an eligible witness for an
applicability-sited comparison and is not one for a rule-sited comparison against the same value.
Merged identity with a union of eligibilities would then let a rule-sited boundary read covered
because some row settled the applicability copy — masking exactly the defect this family exists to
show. So identity merges and eligibility does not: the probe is one, and it is covered only when
every §8 stage its sites sit at has an eligible row at the literal (below).

**E is refused because the decimal domain is dense.** §2.2 decimals have no successor: 5000.1,
5000.01, and so on downward forever. "Adjacent" is a granularity the runtime would have to invent,
and a probe built on it would demand a row at a value nobody can justify — the failure ADR-0014
refuses when it declines to demand unconstructible rows. Nothing is lost: a row at exactly the
literal is *precisely* the discriminator between a strict and a non-strict encoding (at 5000,
`greater-than` is false and `greater-than-or-equal` is true; everywhere else the two agree), so a
far-side row adds no information about this class at all.

**F is refused as a consequence of the identity rule, not from squeamishness.** Identity merges
sites, but a `description` belongs to a site: the merged `/vendor/riskScore:70` probe covers three
rules with three different descriptions, and there is no single "the description" to print. The
detail carries the structural half instead — each owning rule or exception id with the operator it
compares by — which is enough to open the pack at exactly the right member. Printing prose beside
logic would also edge a computed line toward appearing to have judged the prose, which this runtime
cannot do.

**G is refused because a graph node's compared fact may be edge-injected.** Asking whether some row
places a value at a node's boundary is then the fact-value reachability question ADR-0016
explicitly refuses, and inheriting the family without a witness source would mark every node's
boundaries permanently missing.

**H is refused because the safe direction reverses between a warning and a demand.** ADR-0020's
collector over-approximates on purpose: an over-reported *pointer* produces a visible, arguable
line. An over-reported *probe* is a demand for a row, and one derived from a condition-shaped object
carried as data — inside a `value` literal, inside an `extensions` slot — would demand a row for a
boundary the evaluator never evaluates. For a warning over-report is safe; for a demand under-report
is.

Settled determinations:

- **Identity is one probe per distinct fact pointer and decimal value**, named
  `boundary:<pointer>:<literal>`, in walk order of first occurrence. Grouping is by pointer text
  verbatim plus decimal *value*, so `70` and `70.0` at one pointer are one boundary; the probe
  carries the first-authored spelling. The operator is excluded from identity — it does not change
  the witness — and carried in the detail, because it is the half that can disagree with the prose.
- **The witness is expectation-gated and facts-located.** A row witnesses a boundary only when its
  `expectedDisposition` passes the comparator's own strict decoder (the existing
  `project.DecodeWitness` gate) *and* its facts, decoded under the carrier rules the matrix loader
  used, place the pointer's value at the literal. A row expecting a §8.4 error class is refused
  before §7 compares anything; a row whose expectation cannot decode mismatches forever; a row whose
  facts do not decode states no input. None of the three pins what the pack does at the boundary.
- **Eligibility is per stage, from §8's evaluation order, and there are three stages.** The
  expectation is not only a gate: it also says whether that row's evaluation could have reached the
  comparison at all. §8 runs applicability (step 1), records required evidence *without returning*
  (step 2), evaluates **every** exception condition (step 3), combines their effects (step 4), may
  halt before normal rules (step 5), and evaluates the surviving rules last (steps 6-7). A pack's
  conditions are read at three of those points, each behind a different halt:

  - **`applicability`** is exercised by *any* row whose expectation decodes. A not-applicable row
    exercised it by definition — that disposition is what the comparison produced.
  - **An exception's `when`** is exercised by every row except one expecting kind
    `not-applicable`, which §8.3 pins to the reason set `{"not-applicable"}`. Step 1 is the only
    halt in front of step 3: step 2 *records* `missing-required-evidence` or `unknown` and returns
    nothing, because §8 places the halt those reasons cause at step 5, "after all exception effects
    have been inspected". A missing-evidence row therefore did evaluate every exception.
  - **A normal rule's `when`** additionally excludes every expectation that proves that step-5 halt:
    a retained `missing-required-evidence`, which only step 2 records and which §8 retains beside
    whatever else was discovered, so its presence proves the halt however many other reasons
    accompany it; and a retained `exception-escalation`, which only a true `escalate` exception
    records at step 4 and which is one of step 5's named halting states.

  A boundary is covered when **every stage its sites sit at** has a row that both places the value
  at the literal and is eligible for that stage — not when one stage does. Eligibility for a later
  stage implies eligibility for an earlier one, so one rule-eligible row usually answers for the
  whole boundary; what the per-stage rule refuses is the reverse, an applicability-only row
  answering for a rule-sited comparison. The missing sentence then names the sites still unwitnessed
  rather than all of them.

  Recorded honestly in two directions: reason `unknown` is admitted at a rule site although §8 also
  reaches it at an unknown applicability, at unknown required evidence, and at an escalating
  exception; and reason `conflict` is admitted there although §8 records it at step 5 for
  incompatible forced outcomes as well as at step 8 for rules naming different outcomes. Telling
  either apart is the fact-value reachability analysis this record refuses elsewhere, and the
  generous reading is the right one for both — its cost is a probe covered by a row that does place
  the value at the boundary, where the strict reading's cost is demanding a row the author has
  already written, which is the failure mode option H is refused for.
- **Equality is the evaluator's own comparison, exported rather than restated.**
  `evaluation.DecimalCompare` wraps the ordering §7.4 applies, so `"5000.0"` witnesses the literal
  `"5000"` and a JSON number `5000` witnesses nothing — the value §7.4 cannot compare never
  exercises the boundary. `evaluation.DecimalKey` renders that same grammar's canonical value, so
  the grouping folds sites in one pass by the identity the comparator would decide instead of
  comparing every pair. `evaluation.ResolvePointer` locates the fact by the same resolution a
  condition uses, and `evaluation.OrderedOperator` answers the operator set once, read by the
  evaluator's own dispatch and by this derivation. The one-statement discipline ADR-0014 applied to
  the reason vocabulary, applied to a comparison, a resolution, and an operator set. The operator
  set is exported as a predicate and not as its map: an exported map variable has no write barrier,
  so a package that only meant to read the set could assign into it and rewrite §7.4's dispatch.
- **The walk is structure-keyed.** It visits `applicability`, each `rules[].when`, and each
  `exceptions[].when`, and descends only through `all.conditions`, `any.conditions`, and
  `not.condition`. It never descends into a condition's `value`, into `extensions`, into a
  quantifier's `where` or `at`, or into any member it does not know. Nothing is lost by the
  narrowness: coverage is derived only after the evaluator admits the pack, admission runs full
  document and semantic conformance, and `packs test` never opts into the draft RFC 0008 grammar —
  so every pack that reaches here recurses through exactly those three doors and every
  ordered-comparison operand is a well-formed decimal string.
- **The scope is the whole addressable class, not a first phase.** §7.4 has no date, time, duration,
  or arithmetic comparison — a prepared date supplied as a decimal string is already covered — and
  `equals`, `not-equals`, and `in` are point predicates with no boundary. There is no second cut to
  schedule.
- **The family is derived outside the shared entry point.** `project.PackProbes` — ADR-0016's one
  derivation of a pack's declared reach — is untouched and derives no boundary probe; the boundary
  derivation is called only from the matrix path, which is the only place a row's facts exist. The
  exclusion is structural, not a flag.
- **The output is additive and the exit code is untouched.** Boundary probes are new values in the
  existing `coverage` array, so `outputVersion` stays `"2"` under `VERSIONING.md`'s machine-output
  rules — weaker than the additive member ADR-0014 already precedented. Additive in the payload is
  not automatically additive in *behavior*, though, and this record separates the two: a report that
  crosses the MCP surface's response bound turns a previously successful `experimental_test_packs`
  call into a refusal, which is a compatibility break the shape rules would not have caught. That is
  why the rendering budget below is a determination here and not an implementation detail. A missing
  boundary moves no
  status, no summary, and no exit code. One human line changes: the count sentence read "*n/m*
  derived probes have a row expecting them", which is false of a family witnessed by facts, and now
  reads "*n/m* derived probes are witnessed by a row".
- **The wording stays inside ADR-0014's bounded-wording rule, and this is what it is.** The missing
  sentence names the pointer, the value, and the sites still unwitnessed — one entry per distinct
  owning declaration *and* operator, in walk order, so a rule comparing the same pointer and value
  twice (once under a `not`) is named once — then states what a row at that value would settle and
  that the policy text is the arbiter of which encoding the pack should carry. The site list is
  capped at six entries with the rest counted, in the house style. The covered sentence names the
  witnessing row, or the witnessing rows when the stages were settled by different ones, under the
  same cap. No text states or implies that a covered boundary is a correct one.
- **Every authored string this family renders is capped, with a digest tail.** A fact pointer, a
  decimal literal, and a declaration id are authored strings §2.1's carrier bounds only at a
  megabyte each, and this family repeats a pointer and a literal in a probe name and again in a
  sentence, once per distinct pair. Rendered whole, a pack well inside every carrier limit — four
  maximum-length pointer/literal pairs, about 8 MiB, one passing row — produced a 16.8 MB report
  where the previous shape produced 1.3 KB, past the MCP surface's 16 MiB response bound: a call
  that used to succeed became a refusal. An additive JSON member that removes an existing call is a
  compatibility break, whatever the payload's shape rules say, so each rendered string is bounded to
  128 bytes: within it, the string is rendered exactly as authored, and beyond it the rendering is
  the prefix, an ellipsis, and the first 16 hex digits of the SHA-256 of the authored bytes.
  Identity stays unambiguous where the text stops being readable — two pointers agreeing for their
  whole first megabyte still name two probes — and the same pack renders the same names on every
  run. What the cap deliberately does not bound is the *number* of probes: that stays proportional
  to the pack's distinct compared pairs, exactly as ADR-0014's count stays proportional to a pack's
  declared outcomes and reasons. The response bound remains the backstop for a report that is large
  because the pack is, and it names the CLI command that streams the same report.
- **ADR-0014's expectation-only clause is amended by scope, and its prohibition is kept verbatim.**
  ADR-0014's determination reads "**a probe is witnessed by what a row expects, never by what it
  produced**", and the same record separately declines the ordered-comparison type probe because it
  "lives in a row's facts, not its expectation". The determination is two halves and they fare
  differently here. The **affirmative half** — witnessed by what a row *expects* — narrows to the
  disposition family: it was the correct application of the load-bearing invariant to the probe
  classes ADR-0014 derived, which are properties of a **disposition**, and only an expectation
  states a disposition. A boundary is a property of an **input**, and only the facts state an input,
  so for that family the affirmative half no longer holds as a universal. The **prohibitive half** —
  never by what it *produced* — stands verbatim for both families, and it is the load-bearing one:
  the invariant is **document, never run**, argued where ADR-0014 refuses trace-based coverage and
  where it admits the circular oracle, and a row's facts are an authored input in the row document,
  as static and portable as its expectation and not a product of the evaluator. The separate
  type-probe clause narrows with the affirmative half, for the same reason.
- **Three further ADR-0014 statements are universals this record generalizes, and they are listed
  rather than left implied.** Each was written when the disposition family was the only family, and
  each is stated of "the probes" without qualification:
  - "**The probes are derived per pack from its declared outcomes and the reachable §8 reasons.**"
    A boundary probe is derived from a *condition site* — a pointer and a decimal literal an ordered
    comparison names — so the derivation's sources are now the declared outcomes, the reachable
    reasons, **and** the ordered comparisons the pack's own conditions state.
  - "**A missing probe is a fact about what the rows expect.**" For a boundary it is a fact about
    what the rows' authored **facts** state. ADR-0014's point survives the widening whole — a
    missing probe is a fact about the row documents and never a failed row — and only its "expect"
    narrows.
  - "**A probe's detail states 'no row expects X'.**" A boundary's missing detail states "No row's
    facts place …", which is the same bounded-wording rule applied to an input rather than to a
    disposition; the rule that no detail may state or imply that covered means correct is untouched.

  Those three are generalized by scope, the affirmative-witness and type-probe clauses are narrowed
  by scope, and this record supersedes nothing else in ADR-0014.
- **The type probe is deliberately still not derived.** The widened witness makes it derivable — a
  row placing a JSON number at a compared pointer is a readable fact — and it is left out anyway,
  for two reasons. The schema pins an ordered-comparison operand to a decimal string, so the mistake
  the type probe catches lives in fact *production*, upstream of the pack and outside what a pack
  document can be held to. And widening a witness should not silently drag a second family in behind
  it. It is a named follow-up, not a refusal.

### Consequences

- Good, because the contributed case and the transcription study's family-index-0 defect become a
  named line in a report every client reads, instead of guidance in prompt text that reaches only
  clients surfacing prompts.
- Good, because the demand is cheap to satisfy honestly: the row a boundary probe asks for is one
  facts document at a stated value, and writing it forces the author to decide, against the policy
  text, which side of the threshold the value falls on.
- Bad, because it catches one defect class and not its neighbours, and this record says which. A
  threshold moved from 70 to 71 — family index 1 of the same study — derives a probe at 71 that a
  row at 71 covers, saying nothing about whether 71 was the right number. A comparison reading the
  wrong pointer derives a probe at the wrong pointer. Both are questions about what the policy says,
  and no derivation over the pack's own declarations can ask them.
- Bad, because eligibility is read off §8's order rather than off a run, and the order alone
  cannot always answer it. An `unresolved` whose reasons include `unknown` is credited with having
  reached a rule's comparison even where the unknown in fact came from applicability, from required
  evidence, or from an escalating exception; a `conflict` is credited the same way although step 5
  records it too. What §8 makes unambiguous is excluded exactly — kind `not-applicable` before an
  exception or a rule, and a retained `missing-required-evidence` or `exception-escalation` before a
  normal rule, each of which has exactly one writer in the resolver and a halt behind it — and the
  rest are admitted, which is the generous direction on purpose.
- Bad, because per-stage coverage can demand two rows where one reads like enough: a threshold
  compared both in `applicability` and in a rule, whose only row at the literal expects
  `not-applicable`, stays missing until a row that reaches the rule places the value there too. That
  is the demand being right rather than convenient — the rule copy of the comparison is exactly what
  a not-applicable row never evaluated — and the sentence names the site still unprobed so the
  second row is obvious to write.
- Bad, because the circular-oracle escape is exactly as open as ADR-0014 left it: a facts document
  can be pasted from a run as easily as an expectation can. Coverage forces the row to exist; the
  arbiter rule governs what it may contain; nothing mechanical closes the remainder.
- Neutral, and correcting an earlier draft of this record: `boundary:<pointer>:<literal>` **is**
  splittable, at its **final** colon. A pointer may contain as many colons as it likes, including a
  trailing one, but the literal cannot contain any — every literal that reaches a probe has already
  passed §2.2's decimal grammar, `-?(0|[1-9][0-9]*)(\.[0-9]+)?`, and a capped rendering's tail is an
  ellipsis and hex digits. Splitting at the last colon therefore recovers the two halves uniquely,
  including for a root pointer and for `/a~1b/~0c:d`. What it recovers is the *rendered* pointer and
  literal, which are the authored ones except where the 128-byte budget capped them. None of that
  makes the name an interface: probe strings stay labels this runtime never parses, and both members
  are restated in the detail sentence. ADR-0016's `node:<nodeId>:<packProbe>` is a different
  composition — its second half is itself a probe name that may contain colons — and nothing here
  changes what that record says about it.
- Bad, because the shipped demo project's packs carry ordered comparisons and its matrices will
  start reporting gaps. That is ADR-0014's own precedent — the gaps become visible where they
  already exist — and closing them is a demo-repository follow-up, deliberately not folded in here.
- Bad, because the report grows lines on a surface whose argument is that it has few. Merged
  identity is the mitigation for the count — a threshold repeated across a pack is one probe — and
  the rendering budget is the mitigation for the size of each line. Neither bounds a report whose
  pack declares thousands of distinct thresholds; that report is large because the pack is, and the
  MCP surface refuses it with the command that streams it rather than truncating it.
- The `test_pack` prompt gains no clause pointing at the derived boundary probes in this change.
  ADR-0016 set the precedent of not smuggling a method-layer change into a derivation record; the
  prompt's numbered list is method (ADR-0008) and is amended there.
- Revisit when a gate is asked for (this family is the natural first candidate, and the question
  belongs to ADR-0014's revisit clause); when the type probe is taken up as its own family; or if
  per-site identity is ever adopted, at which point the nearest `description` becomes well-defined
  and option F can be reconsidered.

## More information

`internal/project/coverage.go` carries the derivation — the structure-keyed walk, the grouping, the
per-stage witness pass with its eligibility predicate, the rendering budget, and the emitted probes
— beside ADR-0014's disposition family and outside `PackProbes`. The eligibility predicate's doc
comment derives each stage's rule from §8 and from `internal/evaluation/resolve.go`, which is this
runtime's implementation of that order and the second authority every claim here is checked against.
`internal/evaluation/condition.go` exports `OrderedOperator`, `DecimalCompare`,
`DecimalKey`, and `ResolvePointer`, each with the reason it is exported. `internal/cli/render.go`
carries the reworded count line and `internal/result/result.go` the corrected coverage doc comments.
[ADR-0014](0014-matrix-coverage-report.md) is the record this one amends by scope and otherwise
inherits; [ADR-0016](0016-graph-rows-coverage-report.md) is the surface excluded from the family;
[ADR-0020](0020-report-consulted-fact-pointers.md) is the shape-keyed walk this one deliberately
inverts. The cross-vendor adversarial review this decision requires attaches to the pull request,
not to this file (`docs/adr/README.md`, "Review of material decisions").
