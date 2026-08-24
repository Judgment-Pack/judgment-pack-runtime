---
status: accepted
date: 2026-08-24
deciders: maintainer
---

# Let a graph row assert the handoff target

## Context and problem statement

ADR-0025 gave pack matrix rows `expectedHandoffTarget` and closed a defect class: an edit
reaching only `escalation.target.name` leaves every disposition byte identical, so a green suite
said nothing about where an escalation would go. The same record deliberately deferred the graph
surface, named the open question — a per-headline carrier is blind upstream, a per-node carrier
had no designed shape — and pinned the deferral with a test that refuses the member on graph
rows, reopening "when a per-node or per-headline carrier is designed, rather than borrowed"
(issue #128). A wire client of the graph surface hit exactly that wall.

The prerequisite ADR-0025's version-gating rationale rests on — "a matrix is a closed input" —
became true for graph rows when the spelling holds landed: member names are held exact before
the decoder runs. What the surface still lacked was the version machinery (a supported-versions
list, a member-introduction map) and the carriers.

## Decision drivers

- ADR-0025's semantics are the precedent, not a suggestion: decoded-value comparison
  (`SameHandoffTarget`, now exported so two surfaces cannot disagree), capped renderings as
  display values, null as an assertion, "unavailable" as the honest degraded state, and the
  member gated by a matrix version because a closed input refuses strangers.
- The echo rule: the assertion holds what this run reported, off the one evaluation — the
  configured target exactly when the disposition requested a handoff, null otherwise, no
  delivery observed and no second read of any pack.
- ADR-0025's own criticism of a headline-only design: a graph matrix that asserts only the
  composite's target "stays blind to a target-only mutation" upstream, on a surface where that
  is arguably worse.
- The pack side's costliest machinery exists for problems this surface does not have: its
  render-once-per-pack pass and digest-binding guard protect a rendering minted from pack bytes
  in one place and consumed in another; here both sides are rendered at the comparison, from
  values the run itself reported.

## Considered options

- **A. Headline only** — `expectedHandoffTarget` against the composite's reported target.
- **B. Per-node only** — a map beside `expectedNodes`.
- **C. Both** — the row member for the composite, the map for named nodes.

## Decision outcome

Chosen option: **C**. Option A is the assertion the issue asks for and the exact mirror of a
pack row, but ADR-0025 already said why it cannot be the whole answer: upstream nodes escalate
too, and their targets are as editable. Option B covers everything but breaks the mirror — the
composite is the row's own subject, and requiring a row to name the result node in
`expectedNodes` to assert the graph's one headline target is a coupling nobody asked for. Both
carriers, one semantics.

Settled constraints:

1. **The members, version-gated.** `expectedHandoffTarget` (the composite's reported target) and
   `expectedNodeHandoffTargets` (a map, node id → assertion, each named node also named in
   `expectedNodes`: a target is asserted beside the node's disposition, as a pack row asserts it
   beside its own) require `graphMatrixVersion "2"`. The machinery is the pack matrix's, built
   for this surface: a supported-versions list, a default of "1" for silence, and a
   member-introduction map, so rows read as version 1 are refused with the version the member
   would take — never with a false "unknown member" sentence. The load-time rot-pin ADR-0025
   placed on this surface is inverted into exactly that refusal.
2. **What is asserted.** The target the run reported beside each disposition: the configured
   escalation target exactly when that disposition requested a handoff, null otherwise. Each
   assertion value is null or a `{kind, name}` object through the one decoder the pack matrix
   uses; both target assertions ride only beside a disposition expectation and are refused
   beside an expected error, which is ADR-0025's own rule and keeps the third state almost
   unreachable.
3. **Comparison and report.** Decided on decoded values by the one exported comparator; reported
   as `expectedHandoffTarget`/`actualHandoffTarget` pairs — on the row for the composite, on the
   named node's comparison for the map — rendered under the pack matrix's budget by the one
   writer, set before the comparisons run so a row that mismatches earlier still says which
   destination each side named — exactly when a well-formed assertion rode a run this walk
   performed: a row-defect mismatch (an undecodable expectation, a node the graph does not
   declare) reports the defect in the detail and no pair. The single reachable "unavailable": a
   run refused where the row expected a composite — a §8.4-classed refusal sets the actual error
   class beside it, and a graph-layer refusal that carries no class is told by the detail naming
   the refusal.
4. **Cost is the existing accounting.** Renderings ride inside each row's marshaled report, so
   the report budget charges them; the pack side's dedicated 4 MiB target-report counter is
   deliberately not ported, because it exists there to meter a report nothing else meters, and
   duplicating it here would be second bookkeeping over the same bytes. The render-once pass and
   the digest-binding guard are likewise not ported: with both renderings minted at the
   comparison from reported values, there is no cross-boundary handle for either to protect.
   Per-assertion rendering is bounded by work this surface already does: unlike the pack walk,
   which loads each pack once per run and so had to render once to stay that way, the graph walk
   reads and re-admits every node's pack per row by design, so one canonicalization of one
   member of those same bytes per asserted row is a constant factor on the run's existing cost,
   never a new class of it.
5. **Unasserted rows are untouched; the version gate is stricter, declared.** A row asserting no
   target carries neither member, and a document that omits `graphMatrixVersion` or declares a
   supported version loads to the byte what it did. Three refusals moved, declared in the
   changelog rather than smuggled: a wrong-typed `graphMatrixVersion` keeps its shape
   classification but is caught at the gate with a better sentence; a null or empty
   `graphMatrixVersion`, which the old decoder silently read as the default, is refused —
   neither is silence, and reading them as silence is the looseness this loader has been
   shedding; and the unsupported-version message now names every accepted version. Coverage is
   untouched: a target expectation witnesses no probe, because the probe grammar is dispositions
   (ADR-0016/ADR-0023).

### Consequences

- Good, because the defect class is closed on the last surface that had it, at both ends: the
  composite's target and any named node's, each catchable by a row while every disposition byte
  stays identical.
- Good, because one comparator, one decoder, one rendering writer, and one triple vocabulary
  serve both surfaces — a consumer that learned ADR-0025 has nothing new to learn.
- Bad, because a graph row author must move to `graphMatrixVersion "2"` to use it — the closed
  input doing its job, with the refusal naming the version it would take.
- Neutral, because human renderings gain nothing new: a target mismatch surfaces through the
  row's detail on every human surface, and the JSON pairs carry the renderings for machines.
- Neutral, because `outputVersion` stays: additive members under VERSIONING.md's MINOR rule.
