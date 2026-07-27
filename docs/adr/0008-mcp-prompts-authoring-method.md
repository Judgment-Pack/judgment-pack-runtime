---
status: accepted
date: 2026-07-27
deciders: Brian Jin
---

# Serve authoring method as MCP prompts; the intelligence stays in the client

> **Accepted.** The MCP server gained the protocol's `prompts` capability with three non-normative
> method prompts — `author_pack`, `test_pack`, `fix_pack` — served as static, versioned text that
> the client's model executes with the client's key.

## Context and problem statement

Authoring a judgment pack is judgment-laden translation, and by [ADR-0006](0006-authoring-lifecycle-in-the-client.md)
it lives in the client: the runtime is a stateless oracle that never authors, decides, or holds a
key. But the expressiveness studies produced hard-won *method* knowledge that currently lives only
in prose documentation a filesystem-less client cannot reach: the single-outcome/detector
architecture that §8's conflict rule forces authors to rediscover, the `onUnknown` discipline and
its fallback-blocking behavior, the decimal-string rule for ordered comparisons, and the
prepared-facts ledger that RFC 0007 makes basic authoring hygiene. Every new authoring client
currently relearns these by tripping on them.

An "encode this policy" *tool* was considered and rejected earlier in the project: it would put a
model, a key, and nondeterminism inside the runtime, and would make the reference implementation
the canonical interpreter of what policies mean — spec-by-implementation one layer up.

MCP has a third primitive that threads this: **prompts** are static text the server serves and the
*client's* model executes. The server never calls a model.

## Decision drivers

- Equip the client-side authoring loop without violating keyless / offline / stateless.
- Turn study findings into method every author inherits, instead of folklore rediscovered per pack.
- Keep interpretation plural: the runtime may teach *how to work*, never *what a policy means*.

## Considered options

- **A. MCP `prompts` capability** serving method guidance as versioned static text.
- **B. Documentation only** (status quo): the method stays in `docs/`, unreachable from a
  filesystem-less client's tool surface.
- **C. An authoring tool** that produces packs server-side. Rejected again for the reasons above.

## Decision outcome

Chosen option: **A**. Serving a prompt is a read-only operation in the same class as `get_schema`:
static bytes, versioned with the binary, no model, no key, no network, no state. It *strengthens*
ADR-0006 rather than bending it — the prompt's entire content is instructions for the client-side
loop against the oracle tools.

Settled constraints:

- **Three prompts, method-only:** `author_pack` (the guided create → validate → evaluate loop, the
  §8-shape guidance, the decimal-string and `onUnknown` rules, the prepared-facts ledger),
  `test_pack` (the instance-matrix probe: per-outcome, conflict, unknown, missing-evidence,
  not-applicable, forced-outcome, ordered-comparison rows), `fix_pack` (diagnostics-driven repair
  in carrier → structural → semantic order).
- **Non-normative, and each prompt says so in its own text:** following the method does not make a
  pack conformant; only validation decides conformance; the produced document belongs to the
  client. No prompt implies the runtime blesses, stores, or interprets anything.
- **No policy interpretation:** prompts teach the format's mechanics and the loop's discipline.
  They never state what any domain's policy means. Argument values (a policy text passed by the
  caller) are echoed into the rendered prompt verbatim, not interpreted.
- **Versioned with the binary** like the embedded fixtures: prompt text changes ship as releases
  and appear in the changelog.

This amends [0003](0003-mcp-integration-and-testing-surface.md) a second time: the surface is now
tools + prompts. The tools' posture is unchanged.

### Consequences

- Good, because a filesystem-less client gets the authoring method at the same place it gets the
  oracle, and study findings stop being folklore.
- Good, because the division of labor becomes explicit and inspectable: the runtime ships the
  method, the client ships the mind, the tools stay the oracle.
- Bad, because the reference runtime's prompts become the de facto authoring *style*; mitigated by
  the non-normative marking, by the prompts being small inspectable text rather than behavior, and
  by teaching mechanics rather than interpretation.
- Bad, because client support for MCP prompts is uneven; degradation is graceful (tools are
  untouched) but the guidance reaches only clients that surface prompts.

## More information

Method content sources: the expressiveness studies in `judgment-pack-evaluator-experiments`
(studies 001–003) and spec RFC 0007. Protocol: MCP `prompts/list` and `prompts/get`, capability
`prompts` advertised at initialize. Follows ADR-0002 (one core), ADR-0003 (MCP surface), ADR-0006
(authoring in the client), ADR-0007 (the experimental evaluator the `test_pack` prompt drives).
