---
status: proposed
date: 2026-07-24
deciders: Brian Jin
---

# Defer the HTTP API until validation-as-a-service is needed

> **Stub.** Records a deferral, not a design. Reopen with a full ADR when the need below is concrete.

## Context and problem statement

The early structure sketch included an `api/` surface (routes, models, middleware, server). The
runtime is a stateless validator whose binary plus versioned JSON output already serves local use,
and whose agent-integration need is covered by MCP
([0003](0003-mcp-integration-and-testing-surface.md)).

## Decision drivers

- Every surface built now is surface to secure, document, and keep stable.
- No present use case requires a long-running server.

## Considered options

- **A. Defer** the HTTP API until a concrete validation-as-a-service need appears.
- **B. Build it now** alongside MCP for completeness.

## Decision outcome

Chosen option: **A**. Nothing today needs remote, batch, multi-tenant, or spawn-free-throughput
validation; the CLI and MCP cover the current cases. When a real need appears, the API is built as a
Go adapter (a `serve` subcommand over the same core, per
[0002](0002-language-plurality-at-the-wire.md)), and this ADR is superseded by a full one.

### Consequences

- Good, because the surface stays small and there is no server to secure yet.
- Bad, because remote/batch consumers have no first-class option until it is built.
- Revisit when a concrete validation-as-a-service requirement exists.
