---
status: proposed # proposed | accepted | deprecated | superseded by NNNN
date: 2026-07-28
deciders: Brian Jin
---

# Make the §3.4.1 evaluator-conformance claim against Core `0.2.0-draft`, and supply the §10 limits it requires first

## Context and problem statement

[ADR-0010](0010-evaluator-aligned-to-core-0.2.0-draft.md) aligned this runtime's evaluator to the
evaluator conformance class of Core `0.2.0-draft` and deliberately claimed nothing under it, recording
the claim question as a separate decision with its own evidence and its own review. That record also
recorded the one thing standing in the way as a matter of fact rather than of caution: "**Step 4 of
§3.4 is deferred, deliberately.** §3.4's fourth requirement — defining the §10 limits the class
requires — is met for the phase split and the admission limits and is *not* met for the
evaluation-phase limits on the Core path. That is one of the reasons this record claims nothing, and
closing it is work the claim ADR would have to do first."

This is that ADR. The decision it takes is whether to make the one permitted claim (§3.4.1) now, and
the precondition it has to discharge before taking it is the deferred §10 requirement. Not deciding is
not neutral: a class exists to be claimable, and an unclaimed class gives no one a reference point to
compare an implementation against — while a runtime that runs the corpus clean and says it claims
nothing is, as ADR-0010's own consequences note, the implementation most likely to be misread either
way.

## Decision drivers

- §3.4 admits exactly one path to the claim, and it is a conjunction: produce the §8.3 disposition
  under §§7–8 semantics, report every blocking condition as a §8.4 evaluation error, **define the
  limits §10 requires of this class**, and pass the corpus published for the exact `specVersion` named.
  Three of the four were already true; the fourth was not.
- §3.4.1 makes the claim's *form* the whole of what is permitted: one exact `specVersion`, the corpus
  version, the results obtained, and — in the claim's own words — that every row of that corpus version
  passed. Everything else is forbidden, including a partial or "except for" claim and a claim resting on
  agreement with another implementation in place of corpus results.
- Honesty about the evidence's weight, not only its result. The corpus is a twenty-row seed with a
  published gap list; both implementations that agree on it trace to one maintainer.
- The claim has to bind this runtime to obligations it will still be under later: §11 makes a claim
  non-inheritable, so a later `specVersion` requires a fresh corpus run and a restated claim, and §3.4
  forbids the claimant from adjudicating a failing row for itself.

## Considered options

- **A. Supply the §10 evaluation-phase limits, then make the §3.4.1 claim against `0.2.0-draft`.**
- **B. Keep the experimental-no-claim posture indefinitely**, on the reasoning that a research-preview
  specification is no place for a claim.
- **C. Claim later, after an external implementation exists** to compare against.
- **D. Claim now without closing the §10 gap**, on the strength of a clean corpus run.

## Decision outcome

Chosen option: **A**. The class's four requirements are now all met rather than three of them, and the
form of the claim is §3.4.1's and nothing else.

**B is rejected because the class exists precisely to be claimable.** A conformance class no
implementation claims is a class no one can evaluate the format by: a reader deciding whether JPS
describes evaluation precisely enough to be portable has nothing to inspect except prose. The
no-claim posture was load-bearing while the §10 requirement was unmet — it was the accurate
description of an implementation that did not meet §3.4 — and once it is met, continuing to say
"claims no conformance" is no longer caution but a false statement about this runtime, in the other
direction. Research-preview maturity is an argument for stating the claim's scope exactly, which
§3.4.1's own form and §3.5 already require, not for withholding it.

**C is rejected as circular.** An external implementation is judged against the complete normative
contract — §§7–10, with §1.1 making the specification control where the evaluation corpus is silent or
disagrees — and against the corpus published for the exact version it names. No implementation is the
thing another is measured against: this runtime's output can corroborate or contradict another's
diagnostically, and it is never the deciding reference. So waiting for an external claimant would
change nothing about what either party is judged against, while leaving both waiting for the other; the
clean-room Python lineage in the evaluator-experiments repository is not that external claimant either,
since it traces to the same maintainer's direction.

**D is rejected because it would be a false claim.** §3.4 names the §10 limits as a requirement of the
class, not as a recommendation; claiming with `resource-exhaustion` unreachable on the Core path would
have claimed compliance with a requirement this runtime knowingly did not satisfy.

Settled constraints:

- **The §10 limits, supplied here, are the discharged precondition.** Both are defined, documented, and
  enforced on the ordinary Core path before the claim is made:
  - **Evaluation-work limit**, `DefaultCoreWorkLimit` = 20,971,520 units per evaluation, configurable
    per evaluation exactly as the draft-RFC prototype's budget is (ADR-0009). The accounting model is
    the one that prototype already documented — a unit is one visited condition node, one step of a
    pointer resolution, or one byte of a path, member name, or scalar token a comparison reads — now
    charged on the Core path too, plus one unit per §8 iteration over an authored evidence requirement,
    exception, or rule, charged before step 1 because §8 step 2 walks requirements without evaluating
    anything and a suppressed rule is still visited and still traced. One model, both paths. Reaching
    it is `resource-exhaustion` in the `evaluation` phase, with no disposition and no partial state.

    The number is **derived in code, not written down**: `limits.go` computes it as twice
    `carrier.HardMaxBytes`, the carrier's per-document byte cap, so at the current 10 MiB cap it is
    20,971,520 units and there is no independent constant to drift from the cap it is derived from. That
    derivation is **the whole of what is stated about the ratio**: it is an arithmetic relation between
    two numbers, and it guarantees nothing about a whole evaluation.

    **Correction, recorded rather than quietly dropped.** Two earlier drafts of this record made two
    successive overstatements about that ratio. The first said the limit was "about twice the largest
    admissible input", that such an evaluation "is therefore never refused", and that "what it refuses is
    amplification": the arithmetic was wrong (twice 10 MiB is 20,971,520, not 20,000,000) and the last
    two overstated what a limit can promise. The second replaced them with a guarantee that **one full
    read of every admitted byte always fits**, inferred from "every unit is backed by at least one byte
    of an admitted input", plus the paired boundary sentence that **a single maximal cross-document
    comparison therefore sits exactly at the boundary**. Both of those are now **withdrawn** too, for
    the reviewer's reasons, which are sound: (1) an evaluation admits *three* documents, not two — pack,
    facts, and an optional evidence-availability document — each under the same cap, so the admitted
    bytes alone can exceed twice the cap; (2) "each unit is backed by a byte" is not a one-to-one bound,
    because the bytes of a pointer and of a selected value are charged again on every use, alongside the
    per-node and §8 per-iteration charges, so the number of units is not bounded by the inputs' length;
    and (3) the row that stood behind the guarantee proved only the identity `2C == C+C` — it exercised
    no admitted input and no actual charge, so the boundary sentence was unverified as well. The
    identity row is deleted with the guarantee (`limits_test.go` keeps the derivation relation);
    proving a statement of that shape would need near-cap conformant pack, facts, and evidence fixtures
    driven through the real accounting path, and this record does not assert one in the meantime.
    What is left is the derivation, the documented number, and behavior measured rather than inferred:
    amplification is what the limit refuses in practice — the same large selected value re-read once per
    candidate or once per condition — Core has no runtime fan-out, and every row of the bundled corpus
    charges under 1,000 units, which measures those rows and bounds nothing beyond them. Reaching the
    limit is `resource-exhaustion`, which §10 permits: the limit is documented, it is not portable, and
    an input above it is outside the portable claim.
  - **Collection-size limit**, 250,000 members — the carrier's parsed-node cap. This is a
    determination, recorded rather than reimplemented: the cap is a whole-document budget, so no
    admitted document contains a larger collection, and every collection a Core evaluation traverses
    comes from an admitted document, since Core constructs none and has no operator that iterates one.
    Because the bound is enforced while admitting an input, §10's own phase split makes reaching it
    `malformed-input` in the `preflight` phase, which is stricter than an evaluation-phase check of the
    same bound: §2.1 refuses the document whole instead of admitting it and stopping partway through.
    A second mechanism could only report what the preflight already refuses, so none was added. This
    extends ADR-0010's determination that the node cap is a document-size limit rather than
    contradicting it: it is both, and where it is enforced decides the class.
- **The admitted version scope equals the claim's version scope, and the gate is in code.** Only a pack
  declaring `specVersion` `0.2.0-draft` is evaluated. A pack declaring any other value — `0.1.0-draft`
  included — is refused as `pack-not-conformant` in the `preflight` phase
  (`JPS-EVALUATION-PACK-SPEC-VERSION`), with a message that cites §11's re-declaration rule and states
  the whole remedy: **one edit, the `specVersion` string**, and nothing else in the document, because §11
  says `0.2.0-draft` changes no part of the document format. There is **no legacy path**: no second
  evaluator, no unclaimed mode, one admitted version.

  This is required, not chosen. §11: "Because the value is exact (§4), an unedited `0.1.0-draft` pack is
  not structurally conforming to `0.2.0-draft` and must be re-declared before an implementation claiming
  this draft evaluates it." The moment this runtime claims this draft, evaluating an unedited
  `0.1.0-draft` pack applies the claimed contract to an input §11 says has not opted into it — and the
  claim asserts §§7–10 compliance for **every input the implementation admits**, so an admitted input the
  claim's version scope does not cover falsifies the claim rather than sitting harmlessly beside it.
  Naming the two versions separately in the payload does not repair that: it describes the mismatch
  accurately instead of refusing it.

  **This reverses ADR-0010's contrary determination, explicitly.** 0010 considered exactly this behavior
  as its option D — "Align, and refuse to evaluate any pack that does not declare `0.2.0-draft`" —
  called it "spec-faithful", and rejected it "because it would refuse every pack the runtime has today
  for no gain in honesty that naming the two versions separately does not already buy". That reasoning
  was sound only while nothing was claimed, which is the state 0010 recorded; once a claim exists the
  gain is exactly the honesty of the claim, and 0010's option D becomes the only behavior consistent with
  it. So this record **supersedes that determination of 0010's** — option D's rejection, and the
  admit-either-version rule that followed from it, and nothing else in 0010. ADR-0010 stays immutable and
  is not marked superseded: it recorded the right decision for the posture it recorded, and this record
  is the later decision that changes the posture.
- **The statement itself lives in [`CONFORMANCE.md`](../../CONFORMANCE.md) at the repository root, and
  this record reproduces no part of it.** §3.4.1 fixes the entire form such a statement may take, so
  every element of it — the class, the exact version scope, the corpus, the results, and the exclusions —
  is written there and nowhere else, this record included: a record that enumerated the elements would
  be a second, partial instance of a form that admits no parts. What this record decides is *that* the
  file is written and *where*: a root file rather than a README section, because it has to be findable
  and citable as one document with one scope — the README is a tour of a tool and would bury it between
  install instructions and exit codes, and an auditor looks for `CONFORMANCE.md`. The README links to it
  and keeps the limits and the behavior.
- **Every other surface is reference-only, and none of them restates the claim.** §3.4.1 fixes the whole
  form a claim may take, so a surface naming the class and the version while omitting the corpus version,
  the results, and the every-row statement would be making the partial claim §3.4.1 forbids — the defect
  is not that such a surface says too much but that it says part of a form that admits no parts. So the
  in-band member is a **locator**: `conformanceClaimReference`, whose value is the string
  `CONFORMANCE.md`, replacing the `conformanceClaim` member that carried `"none"` under ADR-0007 and
  ADR-0010. The CLI help, the human output header, the corpus label, the MCP tool descriptions, the
  `test_pack` prompt, the README, and the architecture notes each say where the claim is stated — "in
  full and only, in `CONFORMANCE.md`" — and no sentence outside that file says this runtime claims
  anything. The version scope a consumer needs in band stays in band as `evaluatorSpecVersion`, which is
  a fact about the contract applied and not a claim about conformance to it.
- **The machine-output protocol version is incremented with it.** Renaming an in-band member breaks a
  consumer that read the old name, and `VERSIONING.md` requires a change that breaks machine-output
  compatibility to increment that protocol version deliberately. `outputVersion` therefore goes from
  `"1"` to `"2"` on every payload this runtime writes, and the CHANGELOG entry for this change carries
  the migration: read `conformanceClaimReference` where `conformanceClaim` was read, expect the fixed
  value `CONFORMANCE.md`, and branch on `evaluatorSpecVersion` for the contract version. Bumping the
  protocol version for every payload rather than only the evaluation payloads is deliberate: one protocol
  version describes one output contract, and a consumer that must inspect which payload it holds to know
  whether the version applies has no protocol version at all.
- **The claim has one activation point: the merge of `CONFORMANCE.md` to `main`.** §3.4.1 attaches a
  claim to an implementation and an exact `specVersion` and requires no tag, no release, and no
  publication step, so the source that satisfies the class is the thing that claims — and a development
  build from this history emits the same reference to the same claim its released artifacts do, which is
  the behavior a release-conditional claim could not honestly produce. The earlier draft of this record
  paired a release-conditional effective date in `CONFORMANCE.md` with unconditional present-tense
  surfaces and an unconditional in-band value; that is two activation points, and it is withdrawn.
  Releases carry the claim thereafter because they are built from this history, not because a tag
  activates anything.
- **What the evidence had to be, with the results themselves left to `CONFORMANCE.md`.** The basis is
  the run of the corpus published for the exact version named, compared as §8.3 defines the comparison —
  the RFC 8785 canonical bytes of both sides, through the same canonicalizer — and the file records the
  result of that run. §3.4.1 forbids substituting agreement with another implementation for corpus
  results, so the clean-room Python evaluator in the evaluator-experiments repository — derived from the
  `0.2.0-draft` text alone under the barrier its `CLEAN-ROOM-PROTOCOL.md` states — and the committed
  driver's byte-agreement record (`harness/CLASS-AGREEMENT.md`, 2026-07-28) are recorded there as
  corroboration only, and are not what anything rests on. The alignment those runs test
  was adversarially reviewed twice over before it merged: two internal adversarial verifier passes
  inside the drafting workflow, which absorbed thirteen findings before review, and the recorded
  cross-vendor round on pull request #27 (OpenAI `gpt-5.6-sol`, reviewed SHA `27d6204`) which returned
  one blocker, one major, and three minor findings — every one accepted and implemented — and an
  unusually deep verified-sound list including an independent RFC 8785 cross-check and a byte-comparison
  of default behavior against `main`.
- **The limits of that evidence, quoted rather than summarized, and at the length the result gets.** The corpus is a *seed*
  corpus of twenty rows with a gap list its own README publishes — `conformance/evaluation/README.md`
  as published with `suiteVersion` `0.2.0-draft`, of which this runtime bundles the manifest, that
  manifest's schema, and the four pack fixtures rather than the prose — and the biggest gaps are quoted
  from it verbatim rather than summarized: "**No error rows.** `expectedErrorClass` is part of the carrier and no case
  uses it yet"; "**Three mandatory operators have no row** … There is no `not-equals`, `greater-than`,
  or `less-than-or-equal` row"; "**No `literal` and no `not` row**"; "**No composite-equality row.**
  Every `equals` operand in the fixtures is a Boolean or a string"; "**No fallback-selection row** …
  Step 10's positive branch … has no row"; "**Thin handoff coverage, stated exactly.** Of the twenty
  rows, exactly one … has `triggeredBy` as a proper subset of `reasons` … So the subset rule of §8.3
  rests on a single row". The corpus README is equally exact about the agreement: "both implementations
  trace to one maintainer's direction, so their agreement corroborates the semantics rather than
  independently confirming them", and the seven constructed rows "do not carry that agreement" at all —
  they were read off the text by that maintainer and replayed through one of the two implementations.
  So: two implementations, one direction. Corroboration, not independent confirmation.
- **What the claim binds this runtime to.** Every obligation §3.4.1 attaches: the claim is against one
  exact `specVersion` and is not inherited (§11), so a later version that republishes the class requires
  running that version's corpus and restating the claim before anything is claimed under it; a failing
  row blocks the claim and this runtime does not adjudicate whether the row or the implementation is
  wrong (§3.4), so the response is a fixed implementation or a withdrawn claim, never a self-issued
  exclusion, and only a project-issued erratum can excuse a row; and the claim asserts compliance for
  **every input it admits**, not for the inputs it ran, so a defect found on an input no row contains is
  a defect in the claim.
- **The `experimental` namespace stays, and stops meaning what it no longer means.** No command is
  renamed: `experimental evaluate`, `experimental evaluate-corpus`, and `experimental_evaluate` keep
  their names, and the namespace goes on meaning that the surface may change or be removed without a
  compatibility promise — a stability statement, which is orthogonal to conformance. What is removed is
  the blanket no-claim language on every surface it appeared on: the in-band member, the corpus label, the
  CLI help, the human output header, the MCP tool descriptions and prompts, the README, the draft-RFC
  prototype note, and the CHANGELOG now reference `CONFORMANCE.md` instead of denying a claim this
  runtime makes. This reverses ADR-0010's "The non-claim is a constraint, not a caveat" and nothing else
  in that record; the labeling discipline behind it is kept and inverted rather than dropped, since a
  surface that overstates the claim — including one that restates part of it — is the same defect in the
  other direction.
- **The draft-RFC prototype note becomes a contract fact about the class, not a statement about a
  claim.** The note used to deny conformance of any kind, which contradicted the same payload's own
  reference to the claim file. It does not replace that denial with a scope sentence about the claim
  either — saying what a claim excludes states part of the claim, and §3.4.1 admits no parts. What it
  states instead is a property of the class the specification publishes: packs using RFC 0008 operators
  are not inputs the JPS Core `0.2.0-draft` evaluator class defines, so such a result is evidence for
  nothing about any requirement of that class, and the note points at `CONFORMANCE.md` for anything
  further.

This record **supersedes two determinations of earlier records and nothing else in them.** From
[ADR-0010](0010-evaluator-aligned-to-core-0.2.0-draft.md): the rejection of its option D, and the
admit-either-version rule that followed from it, now reversed by the version gate above; 0010's
contract, its two-versions-in-band reporting, its output shape, and its error classes are all in force
unchanged. From [ADR-0007](0007-experimental-evaluator.md): its consequence that this evaluator "claims
no conformance", carried in its summary note and in the `"conformanceClaim": "none"` envelope it
specified — that consequence only. Everything else 0007 decided stands: the experimental surface, its
naming, its inputs and outputs, errors-are-not-dispositions, and its scope limits. **Neither file is
edited and neither is marked superseded or deprecated**, since each recorded the right decision for the
posture it recorded; each keeps the status it actually holds — 0007 `accepted`, 0010 still `proposed` —
and this is partial supersession, which `docs/adr/README.md` now defines as a determination-level
relation the index carries as an annotation rather than as a record-level status. The ADR index
annotates both rows accordingly: 0007's claim posture and 0010's claim-scope determination are each
noted as superseded by this record, so a reader arriving at either is not misled. ADR-0010's own `status` flip from
`proposed` to `accepted` on merge is its own follow-up docs change — the flow `docs/adr/README.md` step 3
describes, which this repository has always carried out in a dedicated commit (ADR-0007's and ADR-0008's
each did) — so this record neither performs nor depends on it, and 0011 lands `proposed` in the pull
request that makes the decision, like every record before it. That is also why the claim's activation
point is the merge and not this file's `status`: merging is the event that makes the source claim, and
the `status` flip records the decision in the commit that follows it.

### Consequences

- Good, because the class becomes claimable in fact and not only in principle: an implementer can see
  the class claimed by someone, with the exact `specVersion`, the corpus version, and the documented
  limits all named in one citable file. What an implementer is judged against is unchanged by that — the
  complete normative contract of §§7–10, with §1.1 making the specification control where the corpus is
  silent. This runtime's output is diagnostic corroboration when two implementations differ, never the
  deciding reference, and §3.4 forbids this claimant from adjudicating a divergence in any case.
- Good, because closing the §10 gap made the runtime better regardless of the claim.
  `resource-exhaustion` was previously unreachable outside a draft-RFC opt-in, which meant an ordinary
  Core evaluation had no bound on the work one small condition could buy over a large admitted value;
  it now does, with the same order-independent accounting the prototype uses.
- Bad, because **this runtime becomes the first claimant of a class it co-authored.** The conflict is
  named rather than managed: the same maintainer wrote RFC 0006, accepted it into Core `0.2.0-draft`,
  wrote the corpus rows, directed both implementations that agree on them, and now claims conformance
  against all of it. A claim scored against a corpus its claimant wrote is worth exactly as much as the
  corpus, and the corpus says of itself that it is a seed with a published gap list. What limits the
  damage is that every part of it is public and version-pinned — the rows are frozen at release, the
  claim names them, and an external implementation can disagree in public — and what does not limit it
  is any assurance from this record.
- Bad, because a claim is a standing obligation on a `0.x` specification. `0.2.0-draft` may be
  superseded by a breaking version; the claim will then be stale by construction until this runtime runs
  the new corpus, and a reader who sees `CONFORMANCE.md` without reading its version scope will
  over-read it. Mitigated by naming the exact version in the claim, by `evaluatorSpecVersion` on every
  payload, and by the version gate, which refuses to evaluate a pack outside that scope at all.
- Bad, because replacing the in-band `"none"` with a differently named member is a breaking change for
  any consumer that asserted on it, and it costs a protocol-version increment across every payload of
  every command (`outputVersion` `"1"` → `"2"`), including commands this decision does not otherwise
  touch. That is the price of one protocol version describing one output contract, and the experimental
  namespace is what makes the evaluation half of it changeable at all.
- Bad, because the version gate refuses inputs this runtime accepted yesterday: every `0.1.0-draft` pack
  in existence, including this repository's own fixtures, must be re-declared to be evaluated. The cost is
  real and it is one edit per pack (§11), document validation is unaffected, and the alternative was a
  claim contradicted by its own admitted inputs.
- Revisit when a later `specVersion` republishes the class or the corpus (a fresh run and a restated
  claim, not an inherited one), when a row of `suiteVersion` `0.2.0-draft` is found to fail, when the
  corpus's gap list closes enough to change what the evidence is worth, or when an independent
  implementation not tracing to this maintainer claims the class.

## More information

Contract source: JPS Core `0.2.0-draft` §§3.4, 3.4.1, 3.5, 8.2, 8.3, 8.4, 10, 11, the
`conformance/evaluation/` corpus, its `README.md` gap list, and its `errata.md` (no errata for this
`suiteVersion`). The claim: [`CONFORMANCE.md`](../../CONFORMANCE.md). Implementation of the limits:
`internal/evaluation/limits.go` (both limits and the one error, the work limit derived from
`carrier.HardMaxBytes`), `internal/evaluation/quantifier.go` (the accounting model, now charged on both
paths), `internal/evaluation/resolve.go` (§8's own iteration), with
`internal/evaluation/limits_test.go` covering reachability at the documented default, configurability,
the §8 charge, the admission-phase collection bound, and the corpus's headroom. The version gate:
`declaredSpecVersion` in `internal/evaluation/engine.go`. The in-band reference:
`result.EvaluationClaimReference` and `result.OutputVersion` in `internal/result/result.go`.
Supersedes two determinations of [0010](0010-evaluator-aligned-to-core-0.2.0-draft.md) and
[0007](0007-experimental-evaluator.md) as stated above, and otherwise extends 0010; the draft-RFC opt-in
and its own budget are [0009](0009-draft-rfc-quantifier-prototype.md)'s. Corroborating runs: the evaluator-experiments
repository's `harness/CLASS-AGREEMENT.md` and its clean-room Python lineage. The cross-vendor
adversarial review this decision requires attaches to the pull request, not to this file
(`docs/adr/README.md`, "Review of material decisions").
