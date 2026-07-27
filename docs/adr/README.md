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
pull request that makes the decision. *Vendor* means the organization that controls the model's
weights and training — the developer, not the API host and not a reseller; hosted copies of the same
model share lineage and count as the same vendor. *Assisted the drafting* means generated or revised
text that survives in the merged artifact; applying an accepted finding in one's own words does not
make the reviewer a drafter, while adopting reviewer-generated text verbatim is noted in the record.
ADRs are written after the decision and are immutable once accepted, so the review attaches to the
pull request, not to the ADR text.

This applies to every pull request merged after 2026-07-27 that makes a material decision, including
one that supersedes an existing ADR. The specification repository's `GOVERNANCE.md` governs only that
repository; this section is what places this runtime under the obligation, and it binds from the
commit that merged it.

There is no per-pull-request ADR-impact declaration. Materiality is classified by the maintainer, who
is also the author — the weak point of this arrangement, stated rather than papered over — and the
classification is contestable on the pull request. Model review substitutes for review breadth while
the project has a single maintainer; it is not decision authority, and following it confers no
conformance status on anything.

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
