---
status: accepted
date: 2026-09-30
deciders: maintainer
---

# Say what left a result unknown: causes and cross-type comparisons in the trace, unmet evidence beside it

## Context and problem statement

When a step cannot be decided, the payload says `unknown` and nothing says why. Two issues measured
the gap with jpack 0.23.1 on the specification's own examples.

- **#178.** A run stopped by a required requirement marked `absent`, and one stopped by a requirement
  omitted from the evidence document, return the same trace. Neither the payload nor the trace names
  the requirement. And a rule over `/refund/amount` with `onUnknown: ignore` reads a JSON number
  exactly as it reads an absent fact: the entry says `unknown`, the fallback answers, the exit code is
  0, and nothing says the value was there but in a form an ordered comparison does not take.
- **#186.** Equality is type-preserving (§7.4): `"true"` never equals `true`. With
  `/expense/activeInvestigation` sent as `"true"` or `1`, the specification's minimal expense example
  approves an expense under investigation, and the exception's trace entry reads `condition: false`,
  exactly as it does when the fact is `false`.

Every one of these results follows the specification, and none changes here. The gap is in
diagnosis. The person a handoff reaches is not told which fact or document to fetch, and an
integrator whose upstream encodes a value in the wrong JSON type sees confident answers with nothing
to say why.

[ADR-0027](0027-pin-the-evaluation-trace-contract.md) pins the trace's shape. Its clause 6 gives the
step-2 evidence inspection no trace stage, because that step evaluates no condition and an invented
entry would record an evaluation that never ran. Its clause 8 makes a deliberate change to the shape
a decision under VERSIONING.md's machine-output rules. This record is that decision.

## Decision drivers

- A cause is reported only where the walk established it. A member that named a fact whose absence
  changed nothing would be a new way for the record to mislead.
- ADR-0027 stands wherever it can. Clause 6's reason for giving step 2 no trace stage is still true.
- §8.3's boundary is unchanged. Nothing here enters the disposition, changes a verdict, or is compared
  by any equality the specification defines.
- Determinism. What is added is a pure function of the same inputs as the rest of the trace, so the
  audit trail's replay reproduces it (ADR-0018).
- A payload that carried none of this before carries none of it now, byte for byte, where nothing
  was unknown, crossed types, or went wanting at step 2.

## Considered options

- **A. Two optional members on a trace entry, and one member beside the trace.**
  - `unknownCauses` on an entry whose condition is unknown names the leaves the unknown came from.
  - `typeMismatches` on any entry names the equality comparisons it evaluated across JSON types.
  - `unmetEvidence` beside the trace names the required requirements that stopped the walk at step 2.
- **B. As A, but step 2 gets an `evidence` stage in the trace.** This is #178's own wording. It
  reverses ADR-0027 clause 6 and records an evaluation that did not happen.
- **C. One `diagnosis` member beside the trace for all of it.** Trace entries stay untouched, and every
  cause is keyed back to a stage by id. An applicability entry has no id, and a reader would join two
  lists to read one entry.
- **D. A separate check of a facts document against a pack**, which #186 offers as an alternative. It
  answers "could this document cause trouble" rather than "what happened in this run", and it can be
  added later beside A.

## Decision outcome

Chosen option: **A**. It puts each cause on the entry it explains, which is where a reader of that
entry looks, and it leaves ADR-0027 clause 6 true by reporting step 2 beside the trace rather than as
a stage of it. B spends clause 6 for the sake of a place, and C spends the entry's self-containment.

### The contract

1. **`unknownCauses`** is present only on an entry whose condition is `unknown`, and lists the leaves
   the unknown came from in the order the walk met them, each once.
   - Under strong three-valued logic an `all` that some child made false, or an `any` that some child
     made true, is decided whatever its unknown children were. Such an unknown caused nothing and is
     not listed. An `all` or `any` that is itself unknown lists every unknown child it saw, and `not`
     passes its child's causes through.
   - Each cause names a fact pointer (`path`) or an evidence requirement (`evidenceRequirement`), and
     says why (`cause`):
     - `absent`: the pointer selects nothing;
     - `not-comparable`: an ordered comparison selected a value that is not a §2.2 decimal string, and
       `factType` names the JSON type it has;
     - `unknown`: an `evidence-present` condition read a requirement whose presence is unknown,
       stated or omitted;
     - `not-an-array`: under the draft RFC 0008 opt-in, a quantifier's collection pointer selected
       something else, whose type `factType` names;
     - `unsupported`: a condition shape this evaluator does not decide, which a conformant pack cannot
       state, is reported rather than hidden.
   - Under the draft RFC 0008 opt-in, a leaf inside a quantifier's `where`, or a `uniform`'s `at`, is
     resolved against an element, and its cause carries `within`, the collection pointer. The
     innermost collection names it.
2. **`typeMismatches`** is present on any entry whose condition evaluated an `equals`, `not-equals` or
   `in` comparison whose fact value has a JSON type no operand value has. It is recorded whatever the
   verdict, because such a comparison could not have been equal and the verdict cannot say so.
   - Each mismatch carries `path` (and `within`, as above), `operator`, `factType`, and
     `operandTypes`: the operand's types in first-appearance order, one for `equals` and `not-equals`,
     the distinct member types for `in`.
   - An `in` with an empty operand states no type and records nothing.
   - Only comparisons the walk evaluated are recorded; a short-circuited leaf is not.
3. **`unmetEvidence`** is a member of the evaluation payload beside `trace`, and of each node
   evaluation of a graph run. It is present only when §8 step 2 found a required requirement `absent`
   or `unknown`, and lists every such requirement with its state, in the order the pack declares them.
   Both states stop the walk, so both are named, whichever of the two reasons the disposition
   carries. ADR-0027 clause 6 stands: step 2 has no trace stage.
4. **Entry shape.** The two entry members serialize after `skipped`, in the order `unknownCauses`,
   `typeMismatches`, and each is omitted when it has nothing to say. An entry without them is byte for
   byte what ADR-0027 pinned. Its table gains two rows' worth of optional members and no new entry
   kind.
5. **Status.** Everything here is informative, as the trace is. It changes no verdict, no disposition
   member, no reason and no handoff, and it is outside every equality §8.3 defines. It is no witness
   for coverage or conformance (ADR-0014 stands).
6. **Versioning.** Three optional members are added and none is removed or changed, so this is
   additive output under VERSIONING.md's MINOR rule and `outputVersion` stays `"2"`, recorded in the
   changelog as clause 8 of ADR-0027 requires. The human output adds one line for unmet evidence and
   one bracket per cause and per mismatch on a trace line. The `explain_disposition` prompt names the
   three members: a narrator names the recorded causes and nothing beyond them, and says so when an
   unknown entry records none.

### Consequences

- Good, because a handoff now carries what its recipient needs to fetch. An integrator can see a fact
  sent in the wrong JSON type, whether the answer was `unknown`, `false` or `true`.
- Good, because ADR-0027's reasoning about step 2 survives intact, and every golden that holds no
  unknown entry is unchanged.
- Bad, because the contract grows. A refactor of condition evaluation must now keep the causes exact
  as well as the verdicts, and the tests hold it to that.
- Bad, because `typeMismatches` names comparisons whose verdict they did not change, such as a
  cross-type leaf in an `any` a sibling made true. The member says what was compared, not what
  decided.
- Neutral, because the specification's open question on trace minimums stays open. This evaluator's
  answer is evidence for it, not a claim about it.
- Revisit when the specification defines a diagnostic vocabulary of its own, or a facts check
  (option D) is wanted.

## More information

Issues #178 and #186. ADR-0027 (the trace contract), ADR-0018 (the audit trail replays inputs, not
payloads), ADR-0008 (`explain_disposition`), ADR-0039 (the `outcome-value` stage under the draft RFC
0016 opt-in, which this leaves as it is).
