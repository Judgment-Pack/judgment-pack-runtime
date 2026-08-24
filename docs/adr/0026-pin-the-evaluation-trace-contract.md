---
status: proposed
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

Between those two texts, this runtime's trace has exact, implemented properties:

- entries appear in walk order — the authored applicability first when one exists, then every
  exception in document order, then every rule in document order;
- once the rule stage is reached, every authored rule appears exactly once — evaluated, or
  `not-evaluated` with `skipped: true` when a forced outcome ended the walk at §8 step 6, or
  `not-evaluated` with `suppressed: true` when a true exception removed it;
- an unknown that resolution ignored stays visible, carrying the `onUnknown` that ignored it —
  the one minimum-content property §8 itself states;
- the step-2 evidence inspection contributes no trace stage, because it evaluates no condition;
  its findings surface as disposition reasons, and an invented entry would record an evaluation
  that never ran;
- a refused evaluation (§8.4) reports no trace at all — whatever prefix of a record the
  interrupted walk had built never escapes, the same standard §8.4 sets for a partial disposition;
- the whole record is a pure function of the admitted pack document, the facts document, the
  tri-state evidence presences, and the caller's effective options — the supported-extension set,
  the draft-quantifier opt-in, and the work budget among them — under one evaluator release: same
  inputs, same options, same release, the same ordered trace.

Fragments of this are already tested on main — an authored applicability is traced and an omitted
one is not, forced-outcome skips and suppressions stay visible, an ignored unknown remains in the
record, a refusal reports zero output. What no document states and no test pins is the
consolidated contract: the byte-level shape, the order under ids whose document order is not
lexical order, the envelope-level `[]` floor, and the combinations (a suppression target under a
forced outcome; a blocked walk's later exceptions). Each of those is an accident a refactor could
change while the gate stays green: nothing distinguishes contract from coincidence.

And the record's durability runs entirely through determinism. The opt-in audit trail
([ADR-0018](0018-opt-in-evaluation-audit-trail.md)) deliberately stores the canonical disposition
and the replay inputs, **not** the payload: a consumer that wants the trace behind a recorded
decision re-runs the recorded evaluator over the recorded inputs and reads the trace the replay
reproduces. That design is only sound if the trace is a deterministic function of what the record
retains — which is exactly the property nothing states. A client presenting an evaluation today
likewise has no text telling it which trace properties it may lean on and which are this
release's coincidence.

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

0. **Scope.** The contract binds the `trace` member of a successful standalone evaluation
   envelope and of each node evaluation inside a graph run, which carries "the same disposition,
   handoff target, and trace a standalone evaluation reports." It binds nothing else: the graph
   composite's top level has no trace, a refused evaluation has no payload, and corpus and
   matrix row results carry dispositions, not traces.
1. **Position.** `trace` is a member of the envelope beside the disposition, never inside it
   (§8.3). A successful payload always carries it, `[]` at minimum — never omitted, never
   `null`; its presence changes no disposition member.
2. **Determinism.** The trace is a pure function of the admitted pack document, the facts
   document, the evidence-availability tri-states, and the caller's effective options — the
   supported-extension set, the draft-quantifier opt-in, and the work budget among them — under
   one evaluator release. Same inputs, same options, same release produce the same ordered trace
   value, byte-identical under the envelope's compact serialization; presentation reformatting
   such as `--pretty` re-indents the same value and is outside this identity. The replay tuple
   that reproduces a disposition reproduces its trace with it — which is what lets the audit
   trail store inputs instead of payloads.
3. **Order.** Entries appear in walk order: the authored applicability first when the pack authors
   one (an omitted applicability is the literal `true` with no authored condition, and records
   nothing), then every exception in document order, then every rule in document order — document
   order, not id order. Reaching the exception stage records every authored exception; §8 step 5
   returns only after all of them were inspected, so a blocking exception never hides the ones
   after it.
4. **Rule completeness, and the precedence between its shapes.** Once the rule stage is reached,
   every authored rule appears exactly once: evaluated with its verdict; or
   `condition: "not-evaluated"` with `skipped: true` when a forced outcome produced without
   evaluating normal rules; or `condition: "not-evaluated"` with `suppressed: true` when a true
   exception's `suppress-rule` removed it from the evaluated walk. Skipping takes precedence:
   when a compatible forced outcome and a suppression coexist, §8 step 6 ends the walk and every
   rule — the suppression's target included — is `skipped`, because suppression filtering belongs
   to the walk that never ran. The `skipped` entry is this evaluator's answer to the Core's open
   question on trace minimums — a true rule a forced outcome skipped **is** surfaced, unevaluated
   and labeled with why. The answer is reported as this evaluator's, and claims nothing for the
   specification.
5. **Unknown visibility.** An unknown that resolution ignored stays in the record with the
   `onUnknown` that let resolution ignore it. `ignore` never erases an unknown from a trace (§8).
6. **Step-2 evidence inspection has no trace stage.** §8 step 2 inspects tri-state presence
   without evaluating a condition, so there is no evaluation to record; its findings surface as
   disposition reasons, whichever of the three states each requirement is in. A trace entry has
   exactly three stages: `applicability`, `exception`, `rule`. An authored `evidence-present`
   condition is different — it **is** evaluated, inside whatever applicability, exception, or
   rule condition contains it, and surfaces there as that stage's verdict.
7. **No leaked in-progress record.** A §8.4 evaluation error reports no trace: the engine returns
   the zero payload beside the failure, discarding whatever prefix the interrupted walk had
   built. A successful trace is complete **for the path §8 reached** — a false applicability is a
   one-entry record, and a walk blocked at step 5 records no rules. That is the "possibly
   partial" ADR-0008 already names, §8's own order at work; what cannot exist is a payload
   carrying a record the evaluator abandoned mid-stage.
8. **Status: informative, unchanged.** The trace remains outside every equality §8.3 defines,
   remains no witness for coverage or conformance (ADR-0014's refusal stands), and remains inside
   ADR-0007's experimental surface. A deliberate change to its shape is an `outputVersion`
   decision under VERSIONING.md's machine-output rules, recorded in the release's changelog entry
   — never drift.

The entry shapes, exhaustively — members serialize in the fixed order `stage`, `id`, `condition`,
`effect`, `outcome`, `suppressed`, `onUnknown`, `skipped`, and every member but `stage` and
`condition` is omitted when it has nothing to say:

| Entry | Emitted when | Members |
| --- | --- | --- |
| `applicability` | The pack authors an applicability condition | `stage`, `condition` (no `id`: the condition is unnamed) |
| `exception` | Every authored exception, once the stage is reached | `stage`, `id`, `condition`; plus `onUnknown` when unknown; plus `effect` when true; plus `outcome` when that effect is `force-outcome` |
| `rule`, evaluated | The step-7 walk evaluates it | `stage`, `id`, `condition`; plus `outcome` when true; plus `onUnknown` when unknown |
| `rule`, skipped | A forced outcome ended the walk | `stage`, `id`, `condition: "not-evaluated"`, `skipped: true` |
| `rule`, suppressed | A true suppression removed it from the walk | `stage`, `id`, `condition: "not-evaluated"`, `suppressed: true` |

No behavior changes. The deliverable is this statement and the tests that make it enforceable:
byte-golden trace serializations for the full walk under deliberately non-lexical document order,
the forced-outcome skip, the suppression-under-forcing overlap, the suppressed-beside-evaluated
record, and the blocked walk's complete exception stage; the envelope-level `"trace":[]` floor;
the three-state evidence inspection's absence; and the refused evaluation's zero payload asserted
together with §8.4's resource-exhaustion class in the evaluation phase, so a preflight refusal
cannot satisfy it and the discarded prefix is a real one.

### Consequences

- Good, because a reader of a payload — a client rendering an evaluation, a study replaying a
  tuple, a replay reproducing the trace behind an audit record's stored inputs — can now
  distinguish what the trace guarantees from what this release happens to do, from a stated text
  rather than from the source.
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
  unchanged, in both of its readings: a successful trace is partial relative to the authored
  stages whenever §8's own order ended the walk early (clause 7), and a handed payload is an
  untrusted argument besides. The contract binds what this evaluator emits, not what a client
  receives.
