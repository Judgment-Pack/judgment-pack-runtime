---
status: proposed # proposed | accepted | deprecated | superseded by NNNN
date: 2026-07-28
deciders: Brian Jin
---

# Retarget the experimental evaluator at the evaluator conformance class of Core `0.2.0-draft`, and claim nothing under it

## Context and problem statement

The specification released `0.2.0-draft`, which accepts RFC 0006: Core now defines an **evaluator
conformance class** (§3.4), the exact form of the only permitted claim (§3.4.1), an input preflight
with a fixed order (§8.2), a portable disposition with an RFC 8785 byte-agreement requirement (§8.3),
four evaluation-error classes with a fixed precedence (§8.4), and evaluation-phase limits the class
must define (§10). This runtime's experimental evaluator was built against an unaccepted draft of that
RFC under [ADR-0007](0007-experimental-evaluator.md), whose context is explicitly scoped to
`0.1.0-draft` §3.4's blanket prohibition, and which settled an output shape §8.3 now forbids (the
disposition echoed the pack's escalation target) and an error surface §8.4 now classes.

Retargeting is not a small edit and it is not covered by ADR-0007. It bundles a second specification
version, changes a public payload in a breaking way, adds a CLI verb, and changes conformance-relevant
behavior — all three of the categories `docs/adr/README.md` calls material — and ADR-0007 is accepted
and therefore immutable. The decision that has to be recorded is *which* contract this surface is held
to, and, separately and explicitly, that being held to it is **not** a claim under it.

## Decision drivers

- Hold the evaluator to one exact contract, named in the artifact rather than inferred: §3.4.1 scopes
  a claim to one exact `specVersion`, and §11 makes a claim non-inheritable across versions.
- Keep the no-claim posture load-bearing rather than decorative. §3.4.1 makes exactly one claim form
  definable and forbids every other; an aligned implementation that has run the corpus is precisely
  the implementation most likely to be misread as claiming.
- Do not regress the only input the runtime has ever had. `0.1.0-draft` packs are what exists.
- Keep the corpus honest: running it produces results, and §3.4 makes a divergence as likely to be a
  defect in a row as in this implementation.

## Considered options

- **A. Align to `0.2.0-draft` §§8.2–8.4, claim nothing, and say so in every artifact.**
- **B. Align and claim evaluator conformance**, on the strength of a clean corpus run.
- **C. Stay on the ADR-0007 shape** and bundle `0.2.0-draft` for document validation only.
- **D. Align, and refuse to evaluate any pack that does not declare `0.2.0-draft`.**

## Decision outcome

Chosen option: **A**. B is a decision this record deliberately does not take: whether this runtime
ever makes the one permitted claim is its own question, with its own evidence and its own review, and
a **later ADR decides it**. Bundling the corpus and passing it is the evidence such a claim would
require and is not the claim, so taking the claim decision here would smuggle it in as a side effect of
an implementation change. C leaves the runtime implementing an evaluator contract that no released
specification version contains, which is the position ADR-0007 accepted only because no released
version contained one at all. D is spec-faithful — §11 is explicit that an unedited `0.1.0-draft` pack
is not structurally conforming to `0.2.0-draft` — and is rejected because it would refuse every pack
the runtime has today for no gain in honesty that naming the two versions separately does not already
buy.

Settled constraints:

- **Contract:** Core `0.2.0-draft` §8.2 (the four inputs and the ordered input preflight, complete
  before §8 step 1), §8.3 (the portable disposition, its four members and no others, both sets
  serialized sorted and duplicate-free, RFC 8785 canonicalization where a byte comparison is required),
  and §8.4 (four classes, exactly one per refused evaluation, in the fixed order
  `pack-not-conformant`, `malformed-input`, `unsupported-required-extension`, `resource-exhaustion`).
  §§7–8 semantics are unchanged by this draft and stay as ADR-0007 settled them.
- **Two versions in band, independently.** The contract is applied to a pack of either bundled
  version, whatever that pack declares. `specVersion` in the payload remains the pack's own;
  `evaluatorSpecVersion` names the contract applied, on both the evaluation payload and the
  `evaluationError` envelope. §11 says these semantics "existed for no consumer under `0.1.0-draft`",
  so a payload carrying only a pack's `0.1.0-draft` would read as a `0.1.0-draft` disposition, which
  does not exist. Refusing older packs (option D) is the alternative and is declined above.
- **Breaking change, recorded as one:** the disposition no longer echoes `escalation.target`, which
  §8.3 keeps out of it; the target moves to a sibling `handoffTarget` member. `handoff` gains
  `triggeredBy`, present exactly when the state is `requested`.
- **The byte limit is a preflight condition, not a read failure.** Every surface reports an input above
  the documented limit through the preflight, at that input's own place in the order, so §8.4 assigns
  the class — `pack-not-conformant` for the pack, `malformed-input` for the facts or evidence document
  — and a non-conformant pack still outranks an oversized facts document. A caller that reads its own
  inputs reports the condition to the engine instead of refusing it first.
- **Limits, including the ones this runtime does not have.** Admission is bounded by the carrier limits
  (10 MiB per input, 250,000 parsed nodes, depth 128, 1 MiB strings); the node cap is a whole-document
  budget, so it is a document-size limit and reaching it is `malformed-input`. No evaluation-work charge
  is levied outside `--rfc0008-quantifiers` and no per-collection size limit is enforced during
  evaluation on the Core path, so `resource-exhaustion` is reachable only under that opt-in (ADR-0009).
  §10 requires an implementation *claiming* the class to define and document both; this runtime claims
  nothing, and the gap is disclosed in the README rather than papered over.
- **Corpus:** `experimental evaluate-corpus` runs the bundled corpus for one exact version and reports
  every row, labelled `corpus results, no conformance claim` in both output formats. Comparison is
  disposition equality as §8.3 defines it: both sides go through the same canonicalizer, so a row's
  set order is not a difference. CLI only; the MCP surface does not expose it.
- **Step 4 of §3.4 is deferred, deliberately.** §3.4's fourth requirement — defining the §10 limits the
  class requires — is met for the phase split and the admission limits and is *not* met for the
  evaluation-phase limits on the Core path. That is one of the reasons this record claims nothing, and
  closing it is work the claim ADR would have to do first.
- **The non-claim is a constraint, not a caveat.** No command, help text, payload member, README
  sentence, or CHANGELOG entry may state or imply an evaluator-conformance claim. `conformanceClaim:
  "none"` and the corpus label are carried in band; the CLI tests assert the forbidden phrasings are
  absent.

This record **extends** ADR-0007 and supersedes two of its settled constraints — the output shape and
the error list — while leaving its surface, its labeling discipline, and its no-claim posture intact.
ADR-0007's context is scoped to `0.1.0-draft` and it is immutable, which is why the retarget is here
and not an edit there. Per `docs/adr/README.md` step 3, on merge this record becomes `accepted` and
ADR-0007 is marked `superseded by 0010`, with its surface and labeling constraints carried forward by
this record rather than withdrawn.

### Consequences

- Good, because the evaluator is held to a released, public contract instead of a draft RFC, and the
  payload says which contract by version rather than leaving a consumer to infer it.
- Good, because the four §8.4 classes make a refusal machine-readable at the coarse grain two
  implementations can agree on, with this runtime's `JPS-*` codes surviving as the detail §8.4 admits.
- Bad, because a consumer of the experimental payload breaks on the moved escalation target. That is
  what the experimental namespace is for.
- Bad, because a runtime that runs the bundled corpus clean and still claims nothing sits close enough
  to the permitted claim to be misread as making it, which is a position that has to be explained
  repeatedly. It is not that claim: as recorded above, the §10 evaluation-phase requirements this class
  imposes are deliberately unmet on the Core path, so a clean corpus run is required evidence and not
  the claim. Mitigated by carrying the no-claim label in band on every run and disclosing the gap
  rather than papering over it.
- Revisit when the claim question is taken up — as its own ADR, with the §10 evaluation-phase limits
  defined first — or when a later `specVersion` republishes the class or the corpus.

## More information

Contract source: JPS Core `0.2.0-draft` §§3.4, 3.4.1, 8.2, 8.3, 8.4, 10, 11, and the specification's
`conformance/evaluation/` corpus and its README. Implementation: `internal/evaluation` (preflight,
classes, corpus runner), `internal/result` (the disposition and the error envelope), `internal/jcs`
(RFC 8785 canonicalization). Extends [0007](0007-experimental-evaluator.md); the draft-grammar opt-in
and its work budget are [0009](0009-draft-rfc-quantifier-prototype.md)'s. The cross-vendor adversarial
review this decision requires attaches to the pull request, not to this file
(`docs/adr/README.md`, "Review of material decisions").
