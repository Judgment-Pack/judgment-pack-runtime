---
status: accepted
date: 2026-07-25
deciders: Brian Jin
---

# Permanently decline an in-runtime HTTP API

> **Accepted.** Supersedes the 2026-07-24 "defer" stub. The runtime's surfaces are the CLI and the
> stdio MCP server; there is no HTTP API, and none is planned.

## Context and problem statement

The early structure sketch included an `api/` surface. The first cut of this ADR merely *deferred*
the decision. With MCP accepted as the integration and testing surface
([0003](0003-mcp-integration-and-testing-surface.md)) and the authoring lifecycle settled as
client-owned ([0006](0006-authoring-lifecycle-in-the-client.md)), the remaining reasons anyone would
reach for an HTTP API -- remote, batch, multi-tenant, spawn-free throughput -- are re-examined and
found to describe a *different product*, not this runtime.

## Decision drivers

- Every surface built is surface to secure, document, and keep stable.
- A resident server invites authentication, credentials, and state -- the exact posture this runtime
  is defined against (keyless, offline, stateless).
- The batch/throughput case that justified deferral is already served by the stateless "N documents
  is N calls" shape over the CLI and the MCP server.

## Considered options

- **A. Permanently decline** an in-runtime HTTP API; the surfaces are the CLI and the stdio MCP server.
- **B. Keep deferring** and reopen later.
- **C. Build a read-only `serve` adapter now.**

## Decision outcome

Chosen option: **A**. There is no HTTP API and none is planned. If validation-as-a-service is ever
genuinely required, it is a Go `serve` adapter over the same core (per
[0002](0002-language-plurality-at-the-wire.md)) living in a *separate, authenticated distribution*,
not folded into this offline runtime. This decision is not reopened for convenience; only a concrete,
separate-product requirement reopens it.

### Consequences

- Good, because the surface stays minimal and the keyless/offline/stateless invariants are never
  pressured by a resident server.
- Bad, because remote or batch consumers must invoke the binary or the stdio MCP server themselves;
  there is no hosted endpoint.
- Neutral, because the no-drift guarantee ([0002](0002-language-plurality-at-the-wire.md)) is
  untouched -- any future adapter still routes through the one core.
