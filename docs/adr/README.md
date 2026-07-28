# Architecture decision records

This directory records the cross-cutting implementation decisions behind `judgment-pack-runtime` —
the ones no single commit or pull request captures well, such as the repository layout or the
adapter strategy.

These are **ADRs**, not **RFCs**. The split follows the repository boundary:

- **This runtime → ADRs.** Implementation decisions for one nonnormative consumer of JPS, recorded
  after they are effectively made. Lightweight; no comment-before-commit process.
- **The specification → RFCs.** Normative, public, cross-implementation proposals open for comment
  before a decision, in `judgment-pack-spec` under `rfcs/` (see its `rfcs/0000-rfc-process`).

A runtime decision graduates into a spec RFC only if it ever needs agreement across independent
implementations (for example, a standard MCP surface every JPS validator should expose).

[architecture.md](../architecture.md) describes the system **as it is now**; ADRs record **why and
when** it became that way. architecture.md links out to the relevant ADR rather than restating it.

## Adding a record

1. Copy [template.md](template.md) to `NNNN-short-title.md` using the next free number.
2. Write it, set `status: proposed`, and open it in the pull request that makes the decision.
3. On merge, set `status: accepted`. ADRs are immutable once accepted: a later change is a **new**
   ADR that supersedes the old one (which is then marked `superseded by NNNN`), never an edit.

## Review of material decisions

A **material** decision in this runtime follows the interim review regime recorded in the
specification repository's
[`GOVERNANCE.md`](https://github.com/Judgment-Pack/judgment-pack-spec/blob/main/GOVERNANCE.md#interim-review-regime)
and designed in
[RFC 0009](https://github.com/Judgment-Pack/judgment-pack-spec/blob/main/rfcs/0009-interim-review-regime.md).
A decision is material when it changes a public surface, a documented claim, conformance-relevant
behavior, the security posture, or a dependency boundary. The trigger is the decision, not the
paperwork: a material decision made without an ADR is still material, and skipping the ADR does not
skip the review.

Such a decision requires a recorded adversarial review by a model from a **different vendor** than
any model that assisted the drafting, with a written maintainer disposition for each finding, on the
pull request that makes the decision. The record states the commit SHA that was reviewed. A material
change after the reviewed SHA requires a fresh review, with one exception mirrored from the regime:
a change that implements a dispositioned finding of a review already recorded on the same pull
request is covered by that finding's disposition. *Vendor* means the organization that controls the model's
weights and training — the developer, not the API host and not a reseller; hosted copies of the same
model share lineage and count as the same vendor. *Assisted the drafting* means generated or revised
text that survives in the merged artifact, or planning, analysis, structure, or design choices
supplied by a model and relied on to produce it; paraphrase does not launder assistance, so a model
that shaped the outline or settled a design question assisted the drafting even when none of its
sentences survive. Applying an accepted finding in one's own words does not make the reviewer a
drafter, and adopting reviewer-generated text verbatim under an accepted finding does not
retroactively invalidate the review that produced it — no further review is required on that
account — but the record says where each such adoption happened. ADRs are written
after the decision and are immutable once accepted, so the review attaches to the pull request, not
to the ADR text.

The review obligation applies to the pull request that introduces this section and every later
pull request that makes a material decision, including one that supersedes an existing ADR.
Beginning with the introducing pull request, every pull request — material or not — carries the
declaration below. "Later" is determined by commit ancestry from the commit introducing this
section, not by a calendar date. The specification repository's `GOVERNANCE.md` governs only that
repository; this section is what places this runtime under the obligation.

Every pull request in this repository states its impact in the description, on one line:

```
Material-decision impact: none
Material-decision impact: <category>[, <category>...]; review: <link to the review record on this PR>
```

Categories: `public-surface`, `documented-claim`, `conformance`, `security`, `dependency`. `none`
appears alone; otherwise list every category that applies, comma-separated, followed by `review:`
and a permanent link (or links) to review records covering every listed decision. A test-only or
behavior-preserving change is `none` unless it changes what conformance checking accepts, which is
`conformance`. A routine dependency version bump is `none`; a bump that fixes a vulnerability is
`security`; adding, removing, or replacing a dependency is `dependency`. An automated pull request
(dependency bots) gets its declaration added by the maintainer before merge. `none` is a claim, not
a formality: it is the author classifying their own change, and it is wrong whenever the change
turns out to be material.

Materiality is classified by the maintainer, who is also the author — the weak point of this
arrangement, stated rather than papered over. The declaration does not fix that; a checkbox its own
author ticks measures the author's care, not the change. What it does is make silence a policy
violation: when the rule is followed, the classification is explicit, dated, recorded on the pull
request with the reviewed SHA, and contestable there — and omitting the ADR and the review no
longer omits the classification with them. Model review
substitutes for review breadth while the project has a single maintainer; it is not decision
authority, and following it confers no conformance status on anything.

## Index

| #                                                        | Decision                                                                        | Status   |
| -------------------------------------------------------- | ------------------------------------------------------------------------------- | -------- |
| [0000](0000-record-decisions-with-madr.md)               | Record runtime decisions with MADR-format ADRs                                  | accepted |
| [0001](0001-idiomatic-go-single-module-layout.md)        | Idiomatic Go single-module layout; no `packages/` umbrella                      | accepted |
| [0002](0002-language-plurality-at-the-wire.md)           | Language plurality lives at the wire and in thin clients, not polyglot packages | accepted |
| [0003](0003-mcp-integration-and-testing-surface.md)      | MCP is the runtime's integration and testing surface                            | accepted |
| [0004](0004-decline-http-api.md)                         | Permanently decline an in-runtime HTTP API                                      | accepted |
| [0005](0005-single-jps-diagnostic-code-prefix.md)        | Use a single `JPS-` prefix for diagnostic codes                                 | accepted |
| [0006](0006-authoring-lifecycle-in-the-client.md)        | The authoring lifecycle lives in the client; the runtime is a stateless oracle  | accepted |
| [0007](0007-experimental-evaluator.md)                   | Ship an experimental evaluator behind an explicit experimental surface          | accepted |
| [0008](0008-mcp-prompts-authoring-method.md)             | Serve authoring method as MCP prompts; the intelligence stays in the client     | accepted |
| [0009](0009-draft-rfc-quantifier-prototype.md)           | Prototype spec RFC 0008 quantifiers behind an opt-in experimental flag          | proposed |
