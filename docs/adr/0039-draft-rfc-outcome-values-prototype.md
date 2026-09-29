---
status: accepted
date: 2026-09-28
deciders: maintainer
---

# Prototype the specification's RFC 0016 outcome values behind an opt-in flag on the experimental evaluator

## Context and problem statement

The specification published RFC 0016 (Draft), outcome values: an outcome may declare named values,
each a constant or a copy of one fact, and the disposition of that outcome carries them. The RFC
says of itself that "no evaluator implements any part of this", and it leaves open whether the
declaration is a specification-defined extension or a member of Core.

This runtime cannot evaluate such a pack today, whatever its evaluator does. The RFC's text carries
the declaration under the name `org.judgmentpack.outcome-values`, and the bundled `0.2.0-draft`
schema refuses every name beginning `org.judgmentpack.` in both places the RFC uses it: as a member
of an `extensions` object and as an item of `metadata.requiredExtensions`. The pack fails validation
before a rule is read.

## Decision drivers

- Give RFC 0016 its first implementation, and find out whether its rules can be built as written.
- Leave the conformance surface as it is. `spec validate`, the evaluation corpus, the claim in
  `CONFORMANCE.md` and the evaluator without the flag answer every input as they did.
- Leave the disposition of every evaluation made without the opt-in byte for byte as it was, and keep
  every expected disposition held to §8.3 as it is published.
- Make the prototype impossible to reach by accident and impossible to mistake for a standard.
- Hold everything the draft does not add to full document conformance.

## Considered options

- **A. An opt-in flag on the experimental evaluator**, admitted through a Core projection and
  labeled in band, as [0009](0009-draft-rfc-quantifier-prototype.md) did for RFC 0008.
- **B. Treat the name as an extension the caller says it supports.**
- **C. Change the bundled schema to admit the name.**
- **D. Build the Core form**, an optional `values` member of an outcome.
- **E. Wait for the RFC to be accepted.**

## Decision outcome

Chosen option: **A**.

B cannot work: the schema refuses the reserved name before the supported set is consulted. C would
change an artifact this runtime pins to the specification's published bytes by digest, and would
make `spec validate` accept a pack no published version accepts. D has the same semantics and would
be admitted the same way; the extension form is chosen because it is the one the RFC's text and
examples use, so they run as written. E is circular: the RFC asks for implementation experience.

This is a decision under [0007](0007-experimental-evaluator.md)'s umbrella, as 0009 is. The surface
and the labeling are 0007's. This record adds one opt-in inside them.

Settled constraints:

- **Surface.** `jpack experimental evaluate --rfc0016-outcome-values`, and nothing else. The MCP tool
  does not take the opt-in. Neither do the matrix, graph and corpus surfaces.
- **One draft to an evaluation.** The flag and `--rfc0008-quantifiers` are mutually exclusive, on the
  command line and in the engine. The marker of a payload names one RFC, and what each proposal
  yields is learned on its own. How the two drafts compose is a question neither RFC asks.
- **Admission.** Three steps, in the order 0009 fixed. The pack's real bytes pass the carrier layer.
  A gate checks what the draft adds: the form of each value declaration, and the rule that ties a
  declaration on an outcome to the name's entry in `metadata.requiredExtensions`. The pack's Core
  projection then goes through the untouched validator. The projection is the pack with the name
  removed from each outcome's `extensions` and from `metadata.requiredExtensions`. It is never
  evaluated.
- **The name elsewhere.** The RFC admits the name on an outcome and nowhere else. The gate lists no
  other place. The projection removes the name from outcomes only, so a pack that carries it on any
  other object still carries it in the projection, and the validator refuses the reserved name
  there. A test reads the bundled schema for every object that has an `extensions` member and fails
  if one is not tried.
- **Semantics.** The RFC's Resolution section as written, for every input this runtime admits. The
  one input it does not admit and the RFC does is the first finding below. Resolution runs once,
  for the one outcome §8 produced, whether by a forced outcome, by true rules or by
  `fallbackOutcome`. A value that does not resolve withholds the outcome: the result is
  `unresolved` with the one reason `unknown`, the fallback is not tried, and handoff follows §8.1.
  A value is copied as found.
- **The disposition.** It gains the member `value`, present exactly when an outcome that declares
  values is produced. The canonical encoder admits a Boolean for it, which it refused before.
  Canonicalization refuses a `value` the RFC does not admit: one beside a kind that is not `outcome`,
  an empty one, a name that is not a value name, a member that is not a string or a Boolean.
- **Expected dispositions.** Every reader of an expected disposition refuses `value`: matrix rows,
  graph rows, corpus rows and proposed expectations. None of those surfaces evaluates under the
  opt-in, so an expectation that carried the member could not be met.
- **Limits.** The step is charged to the §10 evaluation-work limit in force. Each charge is made
  before the work it pays for: one unit for each outcome of the pack and the bytes of its id,
  before the produced one is looked for; the size of the declaration, its value names and constants included, before its
  names are ordered; each pointer, before it is scanned and resolved; and the size of each value a
  pointer selected, before it is checked and carried. What a charge cannot come before is its own
  measuring, which walks the declaration or the value once and reads the length of each id. An outcome that declares no value is
  charged the search for it and nothing else. The work stops at the first charge the limit does
  not hold, and reaching the limit is `resource-exhaustion`. The draft adds no limit of its own:
  a declaration is bounded by the carrier layer when the pack is admitted, and what it costs to
  resolve is bounded by the work limit. One measurement shows the two apart: in a pack of one
  rule whose values have short names and select one string of the largest size the carrier
  admits through one short pointer, nineteen values are carried under the default limit, and
  twenty pass the first bound and reach the second. Longer names or costlier rules leave room
  for fewer.
- **The trace.** One entry for each value the produced outcome declares, in the order of the value
  names, after the entries that produced the outcome. The stage is `outcome-value`. The entry names
  the value and the outcome and says `resolved` or `unresolved`. It never carries the value. The
  RFC permits an implementation to name the value that did not resolve, outside the disposition, and
  this is where. Every value is resolved and recorded, including one that follows a value that did
  not resolve.
- **Labeling.** Every successful payload carries `draftPrototype` with `rfc` of `"0016"`. Its
  `operators` member is the empty array. A new member, `outcomes`, lists the outcomes that declare
  values. It is absent where no outcome declares one, and absent under RFC 0008, whose marker is
  unchanged. A pack that declares no value is reported as a plain pack, which it is. A refusal is
  the ordinary error envelope and carries no marker.
- **The record.** An evaluation recorded under [0018](0018-opt-in-evaluation-audit-trail.md) carries
  the disposition whole, `value` included, and the marker. The record already holds the facts
  document whole, so a value drawn from a fact adds nothing to what the trail holds.
- **What does not change.** What `spec validate` accepts, the conformance and evaluation corpora,
  the exit classes, the MCP surface, the project and graph surfaces, and what the evaluator without
  the flag admits and produces. One message changes without the flag: an expected disposition
  that carries `value` was refused as a member this runtime does not know, and is now refused
  under a sentence that names the draft. A pack that carries a value declaration is not an input
  the evaluator class defines, and `CONFORMANCE.md` says so where it lists what it does not cover.
  A pack that carries none is the input it was, with the flag or without it.

### What building it found

These are findings for the RFC. None is decided here.

1. **One of the RFC's evaluation rows cannot be reached in this runtime.** The RFC expects a fact
   string that holds an unpaired surrogate not to resolve, which is an `unresolved` disposition. This
   runtime's carrier refuses any input that holds an unpaired surrogate escape, so the facts
   document is a `malformed-input` error before a value is selected. A pack whose constant holds one
   is refused too, which agrees with the RFC by another route. Core §2.1 does not settle which
   carrier is right.
2. **The `unsupported-required-extension` row needs a schema that admits the name.** Under the
   published schema a pack that requires the extension is `pack-not-conformant` for a consumer that
   does not support it. The RFC's Compatibility section says as much.
3. **Two faults are hidden by removing the name and have to be checked by the gate.** One is the
   name listed as required and carried on no object of the pack, which is §9's fault. The other
   is the name listed twice, which is the schema's. The projection no longer holds the entry the
   validator would have read for either. A third case is not hidden: the name listed as required
   and carried on another object and on no outcome. §9 is met there and the fault is the RFC's
   own rule of place. The validator would refuse that pack for the reserved name, which the
   projection leaves where it is. The gate reports it first, in the same sentence as the first
   fault.
4. **The rule "on an outcome and nowhere else" needs no list of places.** It follows from removing
   the name from outcomes only.
5. **A consumer can tell a missing quantity from an unknown condition only outside the disposition.**
   Here that is the trace. This bears on the RFC's third unresolved question.

### Consequences

- Good, because RFC 0016 gains an implementation, the findings above, and the rows its
  Conformance section lists as executable tests. Four of those rows are run as tests of a stated
  difference and not of the answer the RFC gives: the fact that holds an unpaired surrogate, and
  the three for a consumer that does not support the extension.
- Good, because without the flag no input is admitted, refused or answered differently, and the
  tests hold that in place.
- Bad, because the disposition type now has a member Core does not define. It is nil everywhere but
  under the flag, and canonicalization and the readers of expectations hold it there.
- Bad, because the canonical encoder admits a Boolean anywhere in a value it is handed. The
  disposition's own checks are what keep one inside `value`.
- Bad, because a disposition printed under the flag can hold a copy of a fact. The marker's note says
  so, and the human surface writes a string with its controls removed. The JSON surface writes
  the canonical form, in which a direction control is written as itself.
- Bad, because two flags that cannot be combined is one more thing to explain.
- Revisit when RFC 0016 is accepted, rejected or superseded; when the specification decides between
  the extension and the Core form; when a published schema admits the name; or when a pack needs
  both drafts at once.

## More information

Semantics source: the specification's
[RFC 0016 (Draft)](https://github.com/Judgment-Pack/judgment-pack-spec/blob/main/rfcs/0016-outcome-values.md)
and JPS Core §2.2, §7.4, §§8–8.4, §9 and §10. Implementation: `internal/evaluation/rfc0016.go` (the
gate, the projection, the resolution step and the marker), `internal/evaluation/rfc0008.go` (the
admission steps both drafts share), `internal/result/result.go` (the member and its checks) and
`internal/jcs/jcs.go`. Fixtures: `internal/evaluation/testdata/rfc0016/`. Follows
[0007](0007-experimental-evaluator.md) and [0009](0009-draft-rfc-quantifier-prototype.md).
