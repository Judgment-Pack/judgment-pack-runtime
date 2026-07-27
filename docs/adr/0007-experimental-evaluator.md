---
status: proposed
date: 2026-07-27
deciders: Brian Jin
---

# Ship an experimental evaluator behind an explicit experimental surface

> **Proposed.** Implement JPS Core's §§7–8 experiment — as pinned by the specification's
> [RFC 0006 (Draft)](https://github.com/Judgment-Pack/judgment-pack-spec/blob/main/rfcs/0006-evaluator-conformance.md)
> — as `judgment-pack experimental evaluate` and the `experimental_evaluate` MCP tool, claiming no
> evaluator conformance.

## Context and problem statement

The specification published RFC 0006 (Draft): a proposed normative evaluator conformance class
whose acceptance is gated on evidence from two independent implementations. Core `0.1.0-draft`
§3.4 forbids any evaluator-conformance claim, and the specification's tooling-architecture doc
pre-states the guardrails for exactly this situation: an experimental evaluator must be
unmistakably labeled, produce no conformance claim, and remain outside the default validation
path. Someone has to build the first implementation to generate the RFC's evidence; this runtime
is the natural first implementer. Separately, agent-driven authoring (ADR-0006) today proves that
a pack is *well-formed* but not that its logic is *correct* — an evaluator closes that loop.

## Decision drivers

- Generate RFC 0006's acceptance evidence: real implementation experience against its pinned
  semantics and its nine committed instances.
- Preserve the runtime's identity: stateless, keyless, offline `bytes -> result`; evaluation is
  still a pure function over supplied inputs, with no store and no side effect.
- Never contaminate the conformance surface: validation behavior, claims, and exit classes stay
  untouched; the experiment must be impossible to mistake for a standard.

## Considered options

- **A. An `experimental` command namespace in this runtime** — CLI `judgment-pack experimental
  evaluate` plus one clearly-labeled MCP tool.
- **B. A separate experimental binary or repository.**
- **C. Wait for the RFC to be accepted first.**

## Decision outcome

Chosen option: **A**. Option C is circular — acceptance requires implementations. Option B
fragments distribution for no isolation gain: the guardrail is the labeled, non-default surface,
not a separate artifact, and the shared engine seam (one core, per ADR-0002) is the point.

Settled constraints:

- **Semantics:** §§7–8 exactly as pinned by RFC 0006's sketch — three-valued conditions including
  the §7 preamble's `literal` and §7.5 evidence presence; required-evidence check restated as
  `missing-required-evidence` iff presence is false and `unknown` iff presence is unknown with
  none false; ordered comparison defined only over §2.2 grammar-conforming JSON strings (JSON
  numbers yield `unknown`); conflict never tie-broken; §8's order treated as contract, not
  algorithm.
- **Inputs:** a pack (validated to full document conformance first — a non-conformant pack is
  refused as an error, never evaluated), one JSON facts document, an optional tri-state
  evidence-availability document keyed by declared requirement ids (omitted key = `unknown`;
  undeclared key = input error), and the supported-extension set.
- **Output:** RFC 0006's disposition (`kind`, `outcomeId`, deduplicated `reasons`, `handoff`)
  plus a minimal trace, in an envelope that carries `"experimental": true` and
  `"conformanceClaim": "none"`. Producing a disposition exits 0 — the disposition is data, not a
  process verdict; operational failures use the existing exit classes.
- **Errors are not dispositions:** invalid pack, unsupported required extension, malformed or
  oversized inputs, and undeclared evidence keys are explicit errors.
- **Labeling:** the command group, tool name (`experimental_evaluate`), help text, tool
  description, and result envelope all carry the experimental marker and the no-claim statement.
  Diagnostic codes minted for this surface (`JPS-EVALUATION-*`) stay `codeStability:
  "provisional"` like every other code; the experimental marking is carried by the envelope
  (`experimental`, `conformanceClaim`) and the surface naming, not by a new stability tier.
- **Scope:** single-pack evaluation only. Judgment Graph composition (spec RFC 0002) and planner
  selection (spec RFC 0004) are explicitly out of scope — the graph composes the dispositions
  this surface produces and cannot be implemented before their semantics exist.

This amends [0003](0003-mcp-integration-and-testing-surface.md) narrowly: the existing validation
and description tools still evaluate nothing, and the MCP surface as a whole no longer "evaluates nothing" — it
gains exactly one tool that evaluates *experimentally and says so*.

### Consequences

- Good, because RFC 0006 gains its first implementation and executable evidence (the nine
  instances become this surface's acceptance tests).
- Good, because agents can close the logic-correctness loop (author → validate → evaluate against
  sample facts) entirely offline and keyless.
- Bad, because an experimental surface invites misuse as if it were conformant; mitigated by
  labeling in every artifact and by §3.4's permanent prohibition for `0.1.0-draft`.
- Bad, because the surface may change or be removed without compatibility promise as RFC 0006
  evolves; that is the meaning of experimental.

## More information

Semantics source: JPS Core §§7–8 and spec RFC 0006's Specification sketch and appendix. Guardrails:
the specification's tooling-architecture document. Follows ADR-0002 (one core), ADR-0003 (MCP
surface), and ADR-0006 (client-held lifecycle; evaluation inputs cross the wire as text).
