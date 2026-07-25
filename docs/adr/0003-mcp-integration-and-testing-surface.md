---
status: accepted
date: 2026-07-24
deciders: Brian Jin
---

# MCP is the runtime's integration and testing surface

> **Accepted.** Phase 0 passed and Phase 1 is built: `internal/mcp` is a hand-rolled stdio MCP
> server wired as the `judgment-pack mcp` subcommand.

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
- **Phase 1 (built): the MCP server.** `internal/mcp` is a `judgment-pack mcp` subcommand over
  stdio, wrapping the existing engine as the `validate`, `test_conformance`, `get_schema`, and
  `describe_runtime` tools, and evaluating nothing.

Settled constraints, regardless of phase:

- **Transport:** stdio — no ports, no auth.
- **Keys:** the API key lives in the client, never in the runtime.
- **Reference client:** Cline (open-source, in-IDE, edits files and speaks MCP).
- **Examples:** the runtime does not vendor *authored* example packs -- the specification owns those.
  It may, however, surface read-only the valid conformance fixtures it *already embeds and
  digest-locks* (as `get_schema` already surfaces the embedded schema); those are version-pinned
  corpus fixtures, not authored templates. See
  [0006](0006-authoring-lifecycle-in-the-client.md).

Implementation: **hand-rolled, no SDK.** Both Go MCP SDKs (`modelcontextprotocol/go-sdk`,
`mark3labs/mcp-go`) require a Go 1.25 toolchain and pull heavy dependencies — including `oauth2`,
which a keyless offline server never uses — conflicting with this runtime's minimal, offline,
Go 1.21 posture. The MCP surface here is small (four read-only tools over stdio), so the server
speaks MCP's newline-delimited JSON-RPC directly and adds zero dependencies. An SDK belongs in a
future commercial, authenticated API/MCP service (a separate layer per
[0002](0002-language-plurality-at-the-wire.md) and the repository boundary), not in the public
offline runtime.

### Consequences

- Good, because there is no UI, auth, or hosting to build, and BYO-key falls out of the client.
- Good, because it realizes [0002](0002-language-plurality-at-the-wire.md): all-language access
  from one Go wire surface.
- Bad, because testing depends on a third-party client's behavior and setup.
- Revisit if a commercial, authenticated MCP/API surface is needed; that is a separate service, not this runtime (see the implementation note above).

## More information

Follows from [0002-language-plurality-at-the-wire.md](0002-language-plurality-at-the-wire.md).
Target package: `internal/mcp`.
