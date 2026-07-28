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

**C is rejected as circular.** An external implementation compares itself against the corpus and,
where the corpus is silent, against a reference claimant. Waiting for an external claimant before
claiming leaves both parties waiting for the other; the clean-room Python lineage in the
evaluator-experiments repository is not that external claimant either, since it traces to the same
maintainer's direction.

**D is rejected because it would be a false claim.** §3.4 names the §10 limits as a requirement of the
class, not as a recommendation; claiming with `resource-exhaustion` unreachable on the Core path would
have claimed compliance with a requirement this runtime knowingly did not satisfy.

Settled constraints:

- **The §10 limits, supplied here, are the discharged precondition.** Both are defined, documented, and
  enforced on the ordinary Core path before the claim is made:
  - **Evaluation-work limit**, `DefaultCoreWorkLimit` = 20,000,000 units per evaluation, configurable
    per evaluation exactly as the draft-RFC prototype's budget is (ADR-0009). The accounting model is
    the one that prototype already documented — a unit is one visited condition node, one step of a
    pointer resolution, or one byte of a path, member name, or scalar token a comparison reads — now
    charged on the Core path too, plus one unit per §8 iteration over an authored evidence requirement,
    exception, or rule, charged before step 1 because §8 step 2 walks requirements without evaluating
    anything and a suppressed rule is still visited and still traced. One model, both paths. Reaching
    it is `resource-exhaustion` in the `evaluation` phase, with no disposition and no partial state.
    The number is derived from the admission limits rather than picked: a unit is backed by at least one
    byte of an admitted input and the carrier admits at most 10 MiB per input, so this limit is about
    twice the largest admissible input — one full read of the pack plus one of the facts document, which
    is therefore never refused — and what it refuses is amplification: the
    same large selected value re-read once per candidate or once per condition. Core has no runtime
    fan-out, so a generous default is safe: every row of the bundled corpus charges under 1,000 units.
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
- **The claim, in §3.4.1's form and nowhere else:** [`CONFORMANCE.md`](../../CONFORMANCE.md), at the
  repository root, naming the class, the exact `specVersion` `0.2.0-draft`, the corpus `suiteVersion`
  `0.2.0-draft`, the results obtained, and in its own words that every row of that corpus version
  passed. A root file rather than a README section, because a claim has to be findable and citable as
  one document with one scope: the README is a tour of a tool and would bury it between install
  instructions and exit codes, and an auditor looks for `CONFORMANCE.md`. The README links to it and
  keeps the limits and the behavior.
- **The evidence, complete:** the bundled twenty-row corpus of `suiteVersion` `0.2.0-draft` passes
  20/20, compared as §8.3 defines the comparison — the RFC 8785 canonical bytes of both sides, through
  the same canonicalizer. Corroboration, which §3.4.1 forbids substituting for corpus results and which
  is therefore recorded as corroboration: the clean-room Python evaluator in the evaluator-experiments
  repository, derived from the `0.2.0-draft` text alone under the barrier its `CLEAN-ROOM-PROTOCOL.md`
  states, reproduces all twenty rows, and the committed driver records **20/20 byte-agreement between
  the two implementations** (`harness/CLASS-AGREEMENT.md`, 2026-07-28). The alignment those runs test
  was adversarially reviewed twice over before it merged: two internal adversarial verifier passes
  inside the drafting workflow, which absorbed thirteen findings before review, and the recorded
  cross-vendor round on pull request #27 (OpenAI `gpt-5.6-sol`, reviewed SHA `27d6204`) which returned
  one blocker, one major, and three minor findings — every one accepted and implemented — and an
  unusually deep verified-sound list including an independent RFC 8785 cross-check and a byte-comparison
  of default behavior against `main`.
- **The limits of that evidence, stated at the same volume as the result.** The corpus is a *seed*
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
  compatibility promise — a stability statement, which is orthogonal to conformance. What is replaced is
  the blanket no-claim language on every surface it appeared on: the in-band `conformanceClaim` member,
  which was `"none"`, now names the claim and its exact version; the corpus label, the CLI help, the
  human output header, the MCP tool descriptions and prompts, the README, and the CHANGELOG point at
  `CONFORMANCE.md` and its scope instead of denying a claim this runtime now makes. This reverses
  ADR-0010's "The non-claim is a constraint, not a caveat" and nothing else in that record; the labeling
  discipline behind it is kept and inverted rather than dropped, since a surface that overstates the
  claim is the same defect in the other direction.

This record **extends** ADR-0010 and does not supersede it: 0010's contract, its two-versions-in-band
rule, its output shape, and its error classes are all in force unchanged, and the only constraint of
0010's that this record reverses is the no-claim posture whose own precondition 0010 named. Nothing
here marks 0010 superseded or deprecated. Its `status` flip from `proposed` to `accepted` on merge is
its own follow-up docs change — the flow `docs/adr/README.md` step 3 describes, which this repository
has always carried out in a dedicated commit (ADR-0007's and ADR-0008's each did) — so this record
neither performs nor depends on it, and 0011 lands `proposed` in the pull request that makes the
decision, like every record before it.

### Consequences

- Good, because the class becomes claimable in fact and not only in principle: an implementer now has a
  reference claimant to compare against, with the exact `specVersion`, the corpus version, and the
  documented limits all named in one citable file.
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
  over-read it. Mitigated by naming the exact version in the claim, in `evaluatorSpecVersion` on every
  payload, and in the in-band `conformanceClaim` value itself.
- Bad, because replacing the in-band `"none"` is a breaking change for any consumer that asserted on
  it. That is what the experimental namespace is for, and the value now says something true instead of
  something false.
- Revisit when a later `specVersion` republishes the class or the corpus (a fresh run and a restated
  claim, not an inherited one), when a row of `suiteVersion` `0.2.0-draft` is found to fail, when the
  corpus's gap list closes enough to change what the evidence is worth, or when an independent
  implementation not tracing to this maintainer claims the class.

## More information

Contract source: JPS Core `0.2.0-draft` §§3.4, 3.4.1, 3.5, 8.2, 8.3, 8.4, 10, 11, the
`conformance/evaluation/` corpus, its `README.md` gap list, and its `errata.md` (no errata for this
`suiteVersion`). The claim: [`CONFORMANCE.md`](../../CONFORMANCE.md). Implementation of the limits:
`internal/evaluation/limits.go` (both limits and the one error), `internal/evaluation/quantifier.go`
(the accounting model, now charged on both paths), `internal/evaluation/resolve.go` (§8's own
iteration), with `internal/evaluation/limits_test.go` covering reachability at the documented default,
configurability, the §8 charge, the admission-phase collection bound, and the corpus's headroom.
Extends [0010](0010-evaluator-aligned-to-core-0.2.0-draft.md); the draft-RFC opt-in and its own budget
are [0009](0009-draft-rfc-quantifier-prototype.md)'s. Corroborating runs: the evaluator-experiments
repository's `harness/CLASS-AGREEMENT.md` and its clean-room Python lineage. The cross-vendor
adversarial review this decision requires attaches to the pull request, not to this file
(`docs/adr/README.md`, "Review of material decisions").
