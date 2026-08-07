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

**D is refused because the sites have identical witness sets.** The witness test — some row's facts
place this pointer's value at this literal — is a pure function of the pointer and the literal.
Sites sharing them are always covered together and always missing together, so a per-site partition
would inflate the `n/m` denominator in proportion to how often an author repeated a threshold. The
study's correct pack compares `/vendor/riskScore` at `70` in three places with two operators; one
row at 70 settles all three, and this derivation reports one question rather than three.

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
- **Eligibility is per site, from §8's evaluation order.** The expectation is not only a gate: it
  also says whether that row's evaluation could have reached the comparison at all. §8 evaluates
  applicability (step 1), then required evidence (step 2), and only then rules and exceptions — so a
  comparison sited in `applicability` is exercised by *any* row whose expectation decodes (a
  not-applicable row exercised it by definition; that disposition is what the comparison produced),
  while one sited in a rule's or an exception's `when` is exercised only by a row whose disposition
  implies rules ran. Exactly two dispositions prove they did not: kind `not-applicable`, which §8.3
  pins to the reason set `{"not-applicable"}`, and an `unresolved` carrying no reason but
  `missing-required-evidence`. A row witnesses a boundary when its facts place the value there *and*
  it is eligible for at least one of the boundary's sites; since applicability-sited eligibility is
  implied by rule-sited eligibility, a boundary compared at both stages takes the applicability
  reading. Recorded honestly in one direction: an `unresolved` whose reasons include `unknown` is
  admitted at a rule site although §8 can also reach that reason at an unknown applicability, before
  any rule. Telling those apart is the fact-value reachability analysis this record refuses
  elsewhere, and the generous reading is the right one here — its cost is a probe covered by a row
  that does place the value at the boundary, where the strict reading's cost is demanding a row the
  author has already written, which is the failure mode option H is refused for.
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
  rules — weaker than the additive member ADR-0014 already precedented. A missing boundary moves no
  status, no summary, and no exit code. One human line changes: the count sentence read "*n/m*
  derived probes have a row expecting them", which is false of a family witnessed by facts, and now
  reads "*n/m* derived probes are witnessed by a row".
- **The wording stays inside ADR-0014's bounded-wording rule.** The missing sentence names the
  pointer, the value, and each site with its operator (capped, in the house style), then states what
  a row at that value would settle and that the policy text is the arbiter of which encoding the
  pack should carry. No text states or implies that a covered boundary is a correct one.
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
  type-probe clause narrows with the affirmative half, for the same reason. This record amends those
  two clauses' scope by name and supersedes nothing else in ADR-0014.
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
- Bad, because eligibility is read off §8's order rather than off a run, and the order alone cannot
  always answer it. An `unresolved` whose reasons include `unknown` is credited with having reached
  a rule's comparison even where the unknown in fact came from applicability, so a boundary can read
  covered by a row that placed the value there and stopped short of comparing it. The two
  dispositions §8 makes unambiguous — `not-applicable` and an `unresolved` carrying only
  `missing-required-evidence` — are excluded exactly; the rest are admitted, and this is the
  generous direction on purpose.
- Bad, because the circular-oracle escape is exactly as open as ADR-0014 left it: a facts document
  can be pasted from a run as easily as an expectation can. Coverage forces the row to exist; the
  arbiter rule governs what it may contain; nothing mechanical closes the remainder.
- Bad, because the probe name is not machine-splittable — an RFC 6901 pointer may itself contain a
  colon, so `boundary:<pointer>:<literal>` has an ambiguous delimiter. Accepted: probe strings are
  labels this runtime never parses, both members are restated in the detail sentence, and
  ADR-0016's `node:<nodeId>:<packProbe>` has the same property.
- Bad, because the shipped demo project's packs carry ordered comparisons and its matrices will
  start reporting gaps. That is ADR-0014's own precedent — the gaps become visible where they
  already exist — and closing them is a demo-repository follow-up, deliberately not folded in here.
- Bad, because the report grows lines on a surface whose argument is that it has few. Merged
  identity is the mitigation: a threshold repeated across a pack is one probe.
- The `test_pack` prompt gains no clause pointing at the derived boundary probes in this change.
  ADR-0016 set the precedent of not smuggling a method-layer change into a derivation record; the
  prompt's numbered list is method (ADR-0008) and is amended there.
- Revisit when a gate is asked for (this family is the natural first candidate, and the question
  belongs to ADR-0014's revisit clause); when the type probe is taken up as its own family; or if
  per-site identity is ever adopted, at which point the nearest `description` becomes well-defined
  and option F can be reconsidered.

## More information

`internal/project/coverage.go` carries the derivation — the structure-keyed walk, the grouping, the
witness pass, and the emitted probes — beside ADR-0014's disposition family and outside
`PackProbes`. `internal/evaluation/condition.go` exports `OrderedOperator`, `DecimalCompare`,
`DecimalKey`, and `ResolvePointer`, each with the reason it is exported. `internal/cli/render.go`
carries the reworded count line and `internal/result/result.go` the corrected coverage doc comments.
[ADR-0014](0014-matrix-coverage-report.md) is the record this one amends by scope and otherwise
inherits; [ADR-0016](0016-graph-rows-coverage-report.md) is the surface excluded from the family;
[ADR-0020](0020-report-consulted-fact-pointers.md) is the shape-keyed walk this one deliberately
inverts. The cross-vendor adversarial review this decision requires attaches to the pull request,
not to this file (`docs/adr/README.md`, "Review of material decisions").
