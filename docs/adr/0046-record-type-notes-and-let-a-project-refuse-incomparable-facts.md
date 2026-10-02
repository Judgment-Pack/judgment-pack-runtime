---
status: accepted
date: 2026-10-01
deciders: maintainer
---

# Keep the trace's type and unknown notes on the audit record, and let a project refuse a fact no comparison can match

## Context and problem statement

Core has no coercion between JSON types (§7.4). A fact of another JSON type than an `equals`, `not-equals`
or `in` operand, `null` included, makes `equals` and `in` false and `not-equals` true. It is never
unknown, so `onUnknown: escalate` does not reach it. The specification field guide's recommended
detector shape is `equals true`, `onUnknown: escalate`, and a `permitted` fallback. Evaluated
with jpack 0.24.0 (spec issue #112), that shape answers `permitted`, with no handoff, when the
detector's fact arrives as `"true"`, `1` or `null`. Only an absent fact gives `unresolved`.

[ADR-0040](0040-say-what-left-a-result-unknown.md) made the trace say so. A trace entry names each
equality comparison the walk evaluated across JSON types (`typeMismatches`) and each cause of an
unknown (`unknownCauses`). Neither changes a verdict. But those notes exist only in the payload's
trace (issue #199):

- **The audit record keeps no trace.** A decision recorded on `{"exceedsAmountPaid": "true"}` reads
  `approve`. It can say `reviewed: true`, and it carries nothing to say the input could never have
  matched. A replay of the inputs recovers the note, but only if someone thinks to replay.
- **Nothing refuses such an input.** A project cannot ask for it to be refused, even when it hands
  its tools to callers whose feeds it does not control.

The issue's third item, a sentence in the `author_pack` prompt, is a documentation change and is
made separately.

## Decision drivers

- A recorded decision should carry what the runtime already knew about its inputs, in the trace's
  own words and without a value.
- A record that has nothing new to say must be byte for byte what it was. Readers of the trail
  outside this repository read it by exact member names.
- Core's answer is Core's. A refusal can only be something a project asks for, never the default.
- A refusal should depend on the pack and the facts only. It must not depend on which branch the
  walk happened to reach first.
- One precedent for a project's requirement: [ADR-0044](0044-say-and-require-the-reviewed-set.md)'s
  `requireReviewed`. The new member takes the same configuration mechanics, refusal shape and exit
  class.

## Considered options

For the record:

- **A. Counts only**, such as `typeMismatchCount`. A count says something was wrong but not where, so
  a reader must replay to learn which pointer. The record exists to save that replay.
- **B. The trace's notes, gathered, as two top-level members.**
- **C. The whole trace on the record.** It ties the record's shape to the payload's trace contract
  (ADR-0027) and grows every record by every stage. The notes are the part of the trace that nobody
  thinks to recover by replay.
- **D. Nothing.** Replay recovers the notes, but only for someone who already suspects the inputs.

For the refusal:

- **E. Refuse by default.** That would make this runtime's evaluator answer differently from Core's.
- **F. A project member, checked statically over the whole pack.**
- **G. A project member, checked against the walk:** refuse when the trace records a type mismatch.
  Whether a wrong-typed flag is refused would then depend on the other facts. A detector that a true
  sibling short-circuits records nothing, so the same flag is refused in one run and not in the
  next. And the check could only run after evaluating.
- **H. An invocation flag**, such as `--require-comparable-facts`. It binds nobody: the caller who
  supplies the facts would choose. The point is a project setting the rule for callers it hands its
  tools to, as `requireReviewed` does.
- **I. Coerce instead of refusing.** That changes Core's semantics.

## Decision outcome

Chosen options: **B** for the record and **F** for the refusal. B says where the inputs could not
have matched, at no cost to a record that has nothing to say. F gives one answer per pack and facts
document, whatever the walk reaches, and leaves Core's answer intact wherever the project does not
ask for it.

### The record

1. **The members.** An evaluation record gains `unknownCauses` and `typeMismatches`, in that order,
   after `cites` and before `disposition`. Each gathers the notes of the evaluation's trace: every
   entry's causes and every entry's mismatches, each distinct note once, in the order the trace
   first names it. That is entry by entry in walk order, and within an entry in the order the walk
   met them. Two notes are the same when they encode to the same JSON, so the root pointer `""` and
   an unset pointer stay apart, and so do a `within` and its absence, as in the trace. Each note
   has the trace's own shape: a cause names a pointer or an evidence requirement and a cause, with
   the fact's type where the trace gives one; a mismatch names the pointer, any `within`, the
   operator, the fact's type and the operand's types. Neither names a value. The trace is a pure
   function of the inputs, and so are these members.
2. **Only what the walk compared.** These are the trace's notes, so they cover the comparisons the
   walk evaluated (ADR-0040 clause 2). A wrong-typed fact that the walk short-circuited is in no
   note. The refusal below is static and does catch it.
3. **Absent when empty.** Each member is omitted when it has nothing to say. A record whose
   evaluation crossed no type and left nothing unknown is byte for byte the line it was before
   (a test encodes the same values through the former shape and compares the bytes).
   `recordVersion` stays `"1"`: the members are additive, as `tool.digest` was
   ([ADR-0043](0043-record-the-executable-digest.md)).
4. **A graph run.** Each node's record carries its own node's notes, from the trace that node's
   evaluation produced. That matches 0.24.0, which put `unmetEvidence` on each node evaluation and
   not on the composite. The composite record carries neither member: it carries no node's inputs,
   and the composite has no trace of its own.
5. **`unmetEvidence` is not added.** The record already holds the evidence document as evaluated,
   and the pack declares which requirements are required, so a reader learns it from the record
   without evaluating anything.
6. **The readers outside this repository.** Both read the record by exact member names and accept
   the new members. This was checked by reading their code.
   - **Runner's `verify-run`** (`internal/runner/mapping_v2_export.go`, `verifyInputs` and
     `VerifyDisposition`) reads the retained record through `member()`. That decodes into
     `map[string]json.RawMessage` and reads `kind`, `pack.digest`, `inputs.facts`,
     `inputs.evidence`, `inputs.evidenceSupplied`, `cites` and `disposition`, ignoring the rest.
     The export bundle is decoded strictly, but the record sits in `Run.Audit`, a
     `json.RawMessage`, which strict decoding does not open. The bundle's own-encoding check
     compares that raw value canonically after a round trip that keeps every member. The bundle's
     depth limit of 64 is not approached: the new members are at most three levels below the
     record, shallower than `inputs.facts`. The run path (`internal/runner/runtime.go`) reads a
     fresh record into a struct with `json.Unmarshal`, which ignores unknown members.
   - **Gateway's `readRuntimeRecord`** (`go/decision_policy.go`) parses the record with its
     canonical parser and reads `recordVersion`, `kind` and `pack.digest`. `holdToPolicy` reads
     `cites`, `disposition`, `reviewed` and `inputs.facts`. It has no closed-shape check, and its
     SPEC.md §4 step 8 says the verifier reads a record "for these members and for nothing else".
     The parser refuses a name given twice, a string that is not UTF-8 or escapes a lone surrogate,
     and nesting past its bound. The new members are strings and arrays of strings, carry no
     number, repeat no name, are written by Go's encoder as UTF-8, and sit shallower than
     `inputs.facts`.

### The refusal

7. **The configuration member.** `jpack.json` may set `"requireComparableFacts": true` under a new
   configVersion `"5"`. A configuration is a closed input, so the member moves the version, as
   `requireReviewed` moved it to `"4"` (ADR-0044). The schema's `$id` becomes
   `urn:judgmentpack:runtime:jpack-config:5`. Its version gates now admit `graphs` from `"2"`,
   `audit` from `"3"`, `requireReviewed` from `"4"` and the new member under `"5"` alone. The member
   under `"4"` or earlier is refused, and the refusal names `"5"`. `SupportedConfigVersions` reads
   all five. Absent or false, nothing changes.
8. **What is refused.** With the member set, an evaluation is refused when a fact that some
   comparison in the pack reads is present and has a JSON type that comparison can never match:
   - for `equals`, `not-equals` and `in`: a type that no operand has, where `null` is a type;
   - for the ordered comparisons: anything but a §2.2 decimal string, as the evaluator requires. A
     JSON number is not one, and neither is a string outside that grammar.
9. **Static over the whole pack.** Every comparison is checked whether or not evaluation would reach
   it: the applicability, every exception's condition and every rule's, and every branch of each.
   That includes a branch an `any` short-circuits and every rule a forced outcome skips.
   - **An absent fact is not refused.** It is unknown, and the pack's `onUnknown` governs it.
   - **Under the draft RFC 0008 opt-in,** a comparison inside a quantifier's `where` reads each
     element, so each element present is checked. A finding names the collection, the innermost
     one, as the trace's `within` does. `uniform` states no operand, and `evidence-present` compares
     no fact, so neither is checked.
10. **When.** After the §8.2 preflight has admitted every input and before §8 begins. Every refusal
    §8.4 classes comes first: a pack that is not conformant, a malformed facts or evidence document,
    a byte limit, an unsupported required extension. On a deciding surface the lock checks and
    `requireReviewed` come before all of that. The check sits in the engine call only so that it
    reads the documents the walk would read. It is not part of the evaluator contract, and on an
    input it passes, the evaluation is exactly what it would have been without it.
11. **The refusal.** It has the code `JPS-FACTS-COMPARABLE-REQUIRED`, exit 1, no §8.4 class, no
    disposition and no record, the same exit class and "no record" behaviour as
    `JPS-LOCK-REVIEW-REQUIRED`. The message names each pointer, and the collection for an element,
    with the fact's type and the types the comparison can match. It never quotes a value. It names
    each distinct finding once, in walk order, at most ten of them, and counts the rest; a pointer
    longer than 200 characters is shown by its first 200 and an ellipsis, so the message stays
    bounded. It says how to proceed: send the fact in a type its comparison can match, or leave it
    out, which the pack reads as unknown.
12. **Where.** The CLI's `experimental evaluate`, the MCP `experimental_evaluate` tool, and
    `experimental graph evaluate` for each node, against that node's assembled facts with the
    upstream outcomes injected. A graph refusal names the node, and the run records nothing, as for
    any refused node. Decisions and declared rehearsals are both refused, because a rehearsal's
    confident wrong answer misleads too. The test surfaces are not refused, because their rows may
    probe such facts on purpose: `packs test`, `experimental graph test`,
    `experimental evaluate-corpus`, `experimental compare` and the MCP test tools. `packs suggest`
    evaluates nothing.
13. **Its work is bounded.** The walk is charged against a budget the size of the evaluation's own
    work limit. It is charged separately, so it changes no evaluation's limit. It is charged one
    unit per condition node visited, per pointer resolution the steps and bytes the evaluator's
    model charges, per `in` the length of its operand, and for an ordered comparison the bytes of
    the string the decimal grammar reads, before it reads them. On the Core path that total is the
    pack's own size, plus the length of each string an ordered comparison reads. Under the draft
    RFC 0008 opt-in an aggregate multiplies it by the elements present. A walk that reaches the
    budget refuses under the same code, with its own sentence, because the check cannot then say
    that every compared fact is matchable. That holds whatever the pack compares: under the draft
    opt-in, a pack that states no fact comparison can still reach the budget by its aggregates
    alone.
14. **Whom it binds.** The same callers as `requireReviewed` (ADR-0044 point 5). Whoever chooses the
    configuration chooses whether the requirement applies. Whoever can edit `jpack.json` can turn it
    off, and in a locked project that edit is drift the lock records.
15. **Conformance.** `CONFORMANCE.md` is unchanged. It already says the project and graph surfaces
    choose which inputs reach the evaluator, which is what this member and `requireReviewed` do.
    The corpus surface never sets it, and with it absent no surface behaves differently.

This **extends** [ADR-0044](0044-say-and-require-the-reviewed-set.md)'s configVersion value list and
schema `$id` by `"5"`, as ADR-0044 extended ADR-0018's. It takes up
[ADR-0040](0040-say-what-left-a-result-unknown.md)'s option D, a check of a facts document against a
pack, for projects that set the member. Both are annotated in the index without editing either body.

### Consequences

- Good, because a recorded decision says where its inputs could not have matched, in the trail
  itself.
- Good, because a project that hands its tools to callers can refuse the input that turns the field
  guide's detector shape into a confident `permitted`, and the same facts are refused whatever the
  walk would have reached.
- Bad, because a pack that compares one pointer with operands of different types in different
  comparisons, such as `equals true` in one rule and `equals "yes"` in another, can never satisfy the
  requirement for that pointer. The remedy is one `in` whose operand carries both types.
- Bad, because the record's notes and the refusal answer different questions. The notes cover what
  the walk compared, and the refusal covers every comparison the pack states. A record can be
  silent about a wrong-typed fact the walk short-circuited.
- Bad, because records grow by their notes. The notes are bounded by the comparisons the pack
  states, and by the elements present under the draft RFC 0008 opt-in.
- Neutral, because the evaluator already answers `unknown` for an ordered comparison of the wrong
  type, so `onUnknown` governs it. The requirement refuses it anyway, so that it means one thing:
  every fact a comparison reads is one that comparison can match.
- Revisit when the specification lets a pack declare its facts' types. The check would then read
  the declarations instead of inferring a type from each comparison.

## More information

Issue #199; spec issue #112 (the field guide's Shape 1). ADR-0040 (the trace's notes), ADR-0044
(`requireReviewed` and configVersion `"4"`), ADR-0018 and ADR-0043 (the audit record and additive
members), ADR-0027 (the trace contract), ADR-0028 and ADR-0041 (rehearsals), VERSIONING.md (closed
inputs move their version). JPS Core §7.4 (no coercion between JSON types), §8.2–§8.4.
