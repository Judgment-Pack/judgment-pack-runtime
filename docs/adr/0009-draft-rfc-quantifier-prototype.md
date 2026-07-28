---
status: proposed # proposed | accepted | deprecated | superseded by NNNN
date: 2026-07-27
deciders: Brian Jin
---

# Prototype the specification's RFC 0008 quantifiers behind an opt-in flag on the experimental evaluator

## Context and problem statement

The specification published RFC 0008 (Draft), bounded collection quantifiers, and names this
runtime's experimental evaluator as the natural prototype bed. The RFC cannot advance without
implementation experience: its Implementation section asks for two independent implementations, and
its own Specification section leaves the limit-accounting model undefined and calls producing one an
acceptance precondition. Nothing can be learned about a grammar nobody has run. But the operators
belong to no published JPS version — a pack using one is structurally non-conforming under
`0.1.0-draft`, whose `$defs/condition` is a closed `oneOf` — so admitting them anywhere near the
default path would mean this runtime evaluating packs the same runtime's validator rejects.

## Decision drivers

- Generate RFC 0008's implementation evidence and, in particular, a concrete candidate for the
  accounting model it leaves open, including whether the model is even usable.
- Never contaminate the conformance surface: `spec validate` behavior, claims, and exit classes stay
  byte-identical, and no pack becomes valid that was not valid before.
- Make the prototype impossible to reach by accident and impossible to mistake for a standard, in
  both output formats and in the artifact a machine reads.
- Hold everything the draft grammar does not add to full document conformance anyway, so the opt-in
  buys three operators and not a validation hole.

## Considered options

- **A. An opt-in flag on the existing experimental evaluator**, with an in-band prototype marker.
- **B. A separate experimental binary, branch, or repository.**
- **C. Wait for RFC 0008 to be accepted, or for its accounting model to be written.**
- **D. Admit the operators unconditionally in the experimental evaluator.**

## Decision outcome

Chosen option: **A**. C is circular in the same way ADR-0007's option C was, and doubly so here: the
accounting model is a precondition for acceptance and can only be argued from an implementation. D
would make a non-conformant pack evaluable on the surface's default path, which the whole ADR-0007
guardrail structure exists to prevent. B fragments distribution for no isolation gain — the
guardrail is the flag and the marker, not a second artifact.

Settled constraints:

- **Surface:** CLI `judgment-pack experimental evaluate --rfc0008-quantifiers` only. The MCP tool
  does not expose the flag; a prototype grammar is not something an agent should reach through a
  tool description.
- **Semantics:** RFC 0008's Specification section as written — the current condition root and its
  restoration per level, `uniform`'s `at` rooted in each member, the empty-array values (`exists`
  false, `every` true) as pinned choices, `uniform`'s five ordered clauses with clause 3 before
  clause 4, and short-circuiting on the dominant value only, never on `unknown`. One case is this
  runtime's own rather than the RFC's, and is not claimed as conformance: the five clauses do not
  say what `uniform` produces for two *resolved* `at` values whose §7.4 equality is indeterminate —
  clause 3 pins known inequality and clause 4 pins an `at` that fails to resolve, and neither covers
  it. This runtime produces `unknown`, as clause 4 does for a missing `at`, after clause 3 so a
  known counterexample still dominates. An amendment folding indeterminate equality into clause 4 in
  that order is proposed to the RFC; until it is adopted the behavior is a documented extension of
  the five clauses, pinned by tests named as such.
- **Grammar:** the aggregate-depth bound of two, structural rather than syntactic, enforced by a
  depth-indexed check whose declarative twin is committed as a testdata schema artifact. That check
  owns aggregate shape and depth and nothing else; the Core validity of a `where` is delegated to
  the untouched `0.1.0-draft` validator run over the pack's Core projection.
- **Limits:** a candidate accounting model, invented here because RFC 0008 defines none and stated
  in full in the code — a work unit, a preflight charge complete before any element of a condition
  tree is evaluated and invariant under any permutation of the elements, ragged nesting charged as
  Σᵢ|Bᵢ|, Boolean branches a short-circuiting evaluator never reaches charged anyway, deep equality
  charged by value size, `uniform` charged per member, siblings additive. A unit is byte-sensitive
  wherever the processing it stands for is: a pointer costs its path's bytes to compile — charged
  before the scan runs, and once per distinct authored pointer, because the compiled form is then
  cached — plus its token bytes per resolution, and a scalar costs its token length, so a long path
  or a long decimal operand cannot buy unbounded work for one flat unit. The charge is *not*
  duplication-invariant: a duplicated element is one more element and is charged like one. Only the
  condition's value is duplication-invariant, and only while both inputs fit the limits, which is
  what RFC 0008's result-invariance wording says. Exhaustion is an explicit evaluation error
  (`JPS-RESOURCE-EVALUATION-WORK-LIMIT`), never a disposition. The budget is also this runtime's §10
  collection-size limit, which RFC 0008 raises to a MUST: the bound is derived, not a second knob.
- **Labeling:** every successful evaluation payload carries a `draftPrototype` member naming the
  operators used and denying validity under the published `specVersion`, and both output formats say
  the same thing — including for a pack that used no draft operator, which the marker reports as
  unchanged rather than invalid. A refusal is not such a payload: a grammar, projection, or
  work-limit failure is reported through the ordinary operational-error envelope, which carries no
  `draftPrototype` member and names the draft codes instead.
- **What does not change:** `spec validate`, the conformance corpus, the exit classes, the MCP
  surface, and the evaluator without the flag. No conformance claim of any kind is made or implied;
  Core §3.4 forbids one under `0.1.0-draft` whatever is implemented.

This is a decision under [0007](0007-experimental-evaluator.md)'s umbrella, not an amendment to it:
the surface, the labeling discipline, and the no-claim posture are ADR-0007's, and this record adds
one opt-in inside them.

### Consequences

- Good, because RFC 0008 gains its first implementation, a concrete accounting model to argue with,
  and executable conformance rows for the corpus its Conformance section describes.
- Good, because the equivalence check the RFC asks for — re-encoding the three census facts a bare
  quantifier reaches (`A6:/reservation/anySegmentCancelledByAirline`,
  `R3:/modification/allNewItemsAvailable`, and `R5:/request/allNewItemsAvailable`) and confirming the
  dispositions match their prepared-boolean twins — runs as a committed test rather than as a claim,
  with a distinct pair of pack fixtures per fact.
- Bad, because a runtime that refuses a pack in one command and evaluates it in another is a
  confusing thing to explain; mitigated by the flag being explicit, the marker being unavoidable,
  and the refusal without the flag being unchanged.
- Bad, because the accounting model is invented rather than specified, so another implementation
  will disagree with it. That disagreement is the evidence, not a defect.
- Revisit when RFC 0008 is accepted, rejected, or superseded, or when a `specVersion` publishing the
  operators exists — at which point this stops being a prototype and becomes ordinary validation.

## More information

Semantics source: the specification's
[RFC 0008 (Draft)](https://github.com/Judgment-Pack/judgment-pack-spec/blob/main/rfcs/0008-bounded-collection-quantifiers.md),
its dependency [RFC 0006 (Draft)](https://github.com/Judgment-Pack/judgment-pack-spec/blob/main/rfcs/0006-evaluator-conformance.md),
and JPS Core §§7–8 and §10. Implementation: `internal/evaluation/rfc0008.go` (the grammar gate and
the Core projection), `internal/evaluation/quantifier.go` (the operators and the accounting model),
and their testdata fixtures. Follows [0007](0007-experimental-evaluator.md).
