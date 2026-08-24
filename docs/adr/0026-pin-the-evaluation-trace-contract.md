---
status: accepted
date: 2026-08-23
deciders: maintainer
---

# Pin the evaluation trace: deterministic, complete, ordered, and still informative

## Context and problem statement

Every evaluation payload carries `trace` beside its disposition. [ADR-0007](0007-experimental-evaluator.md)
settled the envelope as "RFC 0006's disposition plus a minimal trace"; Core §8.3 places any trace
**outside** the disposition object, forbids its presence from changing any disposition member, and
the Core's open-questions list leaves "the minimum a trace must surface, including whether it must
surface a true rule that a forced outcome skipped" unresolved.

Between those two texts, this runtime's trace has exact, implemented properties that no document
states and no test pins:

- entries appear in walk order — the authored applicability first when one exists, then every
  exception in document order, then every rule in document order;
- once the rule stage is reached, every authored rule appears exactly once — evaluated, or
  `not-evaluated` with `skipped: true` when a forced outcome ended the walk at §8 step 6, or
  `not-evaluated` with `suppressed: true` when a true exception removed it;
- an unknown that resolution ignored stays visible, carrying the `onUnknown` that ignored it —
  §8's own floor, the one trace property the specification does state;
- evidence inspection (§8 step 2) is never traced, because it evaluates no condition; its findings
  surface as disposition reasons, and an invented entry would record an evaluation that never ran;
- a refused evaluation (§8.4) reports no trace at all — whatever prefix of a record the
  interrupted walk had built never escapes, the same standard §8.4 sets for a partial disposition;
- the whole record is a pure function of the admitted pack document, the facts document, the
  tri-state evidence presences, the supported-extension set, and this evaluator release: same
  inputs, same release, byte-identical trace.

Unstated, each of these is an accident a refactor could change while the gate stays green: nothing
distinguishes contract from coincidence. And the record has become durable. Opt-in audit records
([ADR-0018](0018-opt-in-evaluation-audit-trail.md)) embed evaluation payloads; the replay-pinning
discipline records the evaluator release and binary digest beside the pack digest precisely so a
payload can be reproduced later. A durable record whose semantics were never stated ages into a
record no reader can safely interpret — and a client presenting an evaluation today has no text
telling it which trace properties it may lean on and which are this release's coincidence.

The temptation this ADR declines is equally exact: promoting the trace into a second witness.
[ADR-0014](0014-matrix-coverage-report.md) refused to derive coverage from the trace because §8.3
keeps it informative, and [ADR-0011](0011-first-evaluator-conformance-claim.md)'s reference-only
rule forbids anything here from stating or implying a conformance claim. Pinning what the trace
**is** must not change what the trace is **for**.

## Decision drivers

- The demotion discipline of [ADR-0012](0012-jpack-project-convention.md): silence over a gap is
  the failure mode this repository refuses, and here the gap is an entire payload member's
  semantics.
- ADR-0014's refusal must stand: the trace stays the wrong witness for coverage and for
  conformance, stated shape or not.
- The reference-only rule of ADR-0011: the Core's open trace question is the specification's to
  close; this evaluator may report its own answer but claim nothing for the spec.
- The mutation discipline: a contract only tests defend needs tests that discriminate — each
  pinned clause must fail when the behavior behind it changes, which was verified by breaking each
  guarded behavior and watching its test fail.
- [ADR-0008](0008-mcp-prompts-authoring-method.md)'s `explain_disposition` treats its arguments as
  untrusted and the trace as informative and possibly partial; a stated contract must not weaken
  that client-side stance, because a payload reaching a client remains an unauthenticated argument.

## Considered options

- **A. State the contract in this ADR and pin it with byte-golden tests; change no behavior.**
- **B. Version the trace as its own sub-schema** — a `traceVersion` member and dedicated
  compatibility machinery.
- **C. Leave the trace unstated.**

## Decision outcome

Chosen option: **A**. Option C is the demotion discipline's named failure mode. Option B builds
contract machinery for a member whose envelope is experimental: ADR-0007's surface "may change or
be removed without compatibility promise," and a versioned sub-schema inside it would promise more
than its carrier can honor. `outputVersion` and VERSIONING.md's machine-output rules already
govern deliberate shape changes; what was missing is the statement of what the shape **is**.

Settled constraints — the contract, stated once:

1. **Position.** `trace` is a member of the experimental evaluation envelope beside the
   disposition, never inside it (§8.3). An evaluated payload always carries it, `[]` at minimum;
   its presence changes no disposition member.
2. **Determinism.** The trace is a pure function of the admitted pack document, the facts
   document, the evidence-availability tri-states, the supported-extension set, and this evaluator
   release. Same inputs under the same release produce a byte-identical serialized trace. The
   replay tuple that reproduces a disposition reproduces its trace with it.
3. **Order.** Entries appear in walk order: the authored applicability first when the pack authors
   one (an omitted applicability is the literal `true` with no authored condition, and records
   nothing), then every exception in document order, then every rule in document order. Reaching
   the exception stage records every authored exception; §8 step 5 returns only after all of them
   were inspected.
4. **Rule completeness.** Once the rule stage is reached, every authored rule appears exactly
   once: evaluated with its verdict; or `condition: "not-evaluated"` with `skipped: true` when a
   forced outcome produced without evaluating normal rules; or `condition: "not-evaluated"` with
   `suppressed: true` when a true exception's `suppress-rule` removed it. The `skipped` entry is
   this evaluator's answer to the Core's open question on trace minimums — a true rule a forced
   outcome skipped **is** surfaced, unevaluated and labeled with why. The answer is reported as
   this evaluator's, and claims nothing for the specification.
5. **Unknown visibility.** An unknown that resolution ignored stays in the record with the
   `onUnknown` that let resolution ignore it. `ignore` never erases an unknown from a trace (§8).
6. **Evidence is never traced.** §8 step 2 inspects presence without evaluating a condition, so
   there is no evaluation to record; its findings surface as disposition reasons. A trace entry
   has exactly three stages: `applicability`, `exception`, `rule`.
7. **No partial trace.** A §8.4 evaluation error reports no trace: the engine returns the zero
   payload beside the failure. A reader holding a trace holds a complete one — there is no third,
   partial state to misread as complete.
8. **Status: informative, unchanged.** The trace remains outside every equality §8.3 defines,
   remains no witness for coverage or conformance (ADR-0014's refusal stands), and remains inside
   ADR-0007's experimental surface. A deliberate change to its shape is an `outputVersion`
   decision under VERSIONING.md's machine-output rules, recorded in the changelog — never drift.

No behavior changes. The deliverable is this statement and the tests that make it enforceable:
byte-golden trace serializations for the full walk, the forced-outcome skip, and the
suppressed-beside-evaluated record; the evidence-inspection absence; and the refused evaluation's
zero payload, with the refusal test built around a non-empty interrupted prefix so a leaked
partial record is distinguishable from an empty one.

### Consequences

- Good, because a reader of a recorded payload — an audit record consumer, a client rendering an
  evaluation, a study replaying a tuple — can now distinguish what the trace guarantees from what
  this release happens to do, from a stated text rather than from the source.
- Good, because the byte-goldens make refactors honest: an accidental reordering, a renamed
  member, or a member emitted empty fails the gate instead of shipping as silent drift.
- Good, because the §8.3 boundary is now load-bearing in both directions: the contract states the
  trace's shape *and* restates what the trace is not, so pinning it cannot be read as promotion.
- Bad, because the contract constrains refactoring: a resolver change that alters walk order or
  entry shape must now update the ADR, the changelog, and the goldens together. That cost is the
  point.
- Neutral, because the Core's open question stays open — the specification can still close it
  either way, and this evaluator's reported answer is evidence for that decision, not pressure on
  it.
- Neutral, because `explain_disposition`'s caution ("informative, possibly partial") stands
  unchanged: the method narrates whatever payload it is handed, and a handed payload is an
  untrusted argument that may be truncated by any intermediary. The contract binds what this
  evaluator emits, not what a client receives.
