---
status: proposed
date: 2026-07-24
deciders: Brian Jin
---

# MCP is the runtime's integration and testing surface

> **Stub.** The direction and the constraints below are settled; the server itself is not built.
> Acceptance is gated on the Phase 0 review described under "Decision outcome".

## Context and problem statement

The specification and runtime need an interactive way to be exercised on real authoring tasks —
create a pack, revise it, remove parts, have an agent drive the loop — without building a bespoke
web application, accounts, or authentication. The runtime validates documents; it has no CRUD, no
evaluation, no network, and no API key of its own.

## Decision drivers

- Minimum configuration; no custom UI, auth, or hosting to build or secure.
- The runtime must stay keyless, offline, and deterministic.
- "Agent integration" should be first-class, since exercising the spec with an LLM author is a goal.

## Considered options

- **A. Expose the runtime over MCP** (a wire protocol) and let an existing MCP-capable agent client
  be the interface.
- **B. Build a dedicated testing web app** with its own UI and key handling.
- **C. CLI-as-tool only** — an agent shells out to the existing binary, no new surface.

## Decision outcome

Chosen direction: **A**, reached in two phases so the premise is proven before code is written.

- **Phase 0 (proven first, no Go): option C as a warm-up.** An agent in the reference client shells
  out to `judgment-pack spec validate` to pressure-test whether the runtime's diagnostics are good
  enough to close an authoring loop. Review the result, *then* decide Phase 1.
- **Phase 1 (pending review): the MCP server.** Fill the `internal/mcp` seam as a `judgment-pack
  mcp` subcommand over stdio, wrapping the existing engine (`validate`, `test_conformance`,
  `get_schema`, `describe_runtime`) and evaluating nothing.

Settled constraints, regardless of phase:

- **Transport:** stdio — no ports, no auth.
- **Keys:** the API key lives in the client, never in the runtime.
- **Reference client:** Cline (open-source, in-IDE, edits files and speaks MCP).
- **Examples:** read from a `judgment-pack-spec` checkout; the runtime does not vendor example
  packs, because the specification owns them.

Deferred: the MCP Go library choice (official `go-sdk` vs. `mark3labs/mcp-go`) — decided at
implementation time, not now.

### Consequences

- Good, because there is no UI, auth, or hosting to build, and BYO-key falls out of the client.
- Good, because it realizes [0002](0002-language-plurality-at-the-wire.md): all-language access
  from one Go wire surface.
- Bad, because testing depends on a third-party client's behavior and setup.
- Revisit / promote to `accepted` after the Phase 0 review decides Phase 1.

## More information

Follows from [0002-language-plurality-at-the-wire.md](0002-language-plurality-at-the-wire.md).
Target package: `internal/mcp`.
