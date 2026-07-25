---
status: accepted
date: 2026-07-25
deciders: Brian Jin
---

# The authoring lifecycle lives in the client; the runtime is a stateless oracle

> **Accepted.** Create, read, update, and delete of judgment packs are client actions; the runtime
> witnesses each transition through validation and owns no store.

## Context and problem statement

Agents need a full create / read / update / delete authoring loop for judgment packs over the CLI and
the stdio MCP server. "Full CRUD end to end" is easily misread as pressure to give the runtime a pack
store. This ADR fixes where CRUD lives and what "client-agnostic" guarantees, so that pressure never
lands on the runtime.

## Decision drivers

- Identity: the runtime is a stateless `bytes -> result` validator; a store is state
  ([0002](0002-language-plurality-at-the-wire.md), [0003](0003-mcp-integration-and-testing-surface.md),
  [architecture.md](../architecture.md)).
- Keyless and offline: destructive operations imply authorization -> identity -> credentials, and a
  store implies data at rest -- both contradict the runtime's posture.
- No drift ([0002](0002-language-plurality-at-the-wire.md)): store semantics would become a
  conformance surface every other-language implementation must copy.

## Considered options

- **A. CRUD in the client.** The runtime stays a small set of read-only tools; the bytes live in the
  client's filesystem or the model's own context.
- **B. CRUD in the runtime.** Add save / load / list / delete of named packs -- a store.
- **C. Hybrid.** MCP file-path or resource reads, or an in-memory session document.

## Decision outcome

Chosen option: **A**. Create, Read, Update, and Delete are client actions on bytes the client holds.
The runtime never stores, overwrites, or deletes user content; it is consulted at each transition to
answer one question -- *is what you now hold conformant?* -- and it cannot even distinguish Create
from Update, since both arrive as "validate this document."

- **Create / Update:** the client writes the document (a file, or text in the model's context) and
  calls `validate`; the self-sufficient diagnostics drive the fix loop, with `get_schema` as the
  reference of last resort.
- **Read:** the client reads its own copy and may `validate` to confirm it is still conformant; the
  runtime never serves a pack back.
- **Delete:** the client removes its files; re-validating the survivors makes the semantic layer the
  safety net for now-dangling references. The runtime tracks no cross-file relationship.

Documents cross the MCP wire as **text, never a path or handle** -- values, not references: a path
would make a tool's result depend on the server's ambient filesystem and break portability across
client topologies. The CLI accepts a path because it is the user's own local one-shot process; the
long-lived wire endpoint must not. There is no session document and no store.

**Client-agnostic** means any MCP-capable client (Claude Code, Cline, Cursor, Continue, a raw client)
with the user's own key and no client-specific assumptions. A filesystem-less client keeps the
evolving document in the model's context; durable storage of a *library* of packs is a host/client
responsibility, explicitly not the runtime's.

### Consequences

- Good, because statelessness, keyless operation, offline operation, and the no-drift guarantee are
  preserved by construction; there is nothing new to secure.
- Bad, because a filesystem-less client cannot durably persist a library of packs on its own -- a
  host/client concern, not the runtime's.
- Follow-up (does not block this decision): surface the valid conformance fixtures the runtime already
  embeds and digest-locks as read-only `list_examples` / `get_example` tools (and CLI
  `spec examples`), labeled as version-pinned fixtures, to give a filesystem-less client a starting
  point for Create. This is permitted by the amended
  [0003](0003-mcp-integration-and-testing-surface.md) -- surfacing already-embedded fixtures, distinct
  from vendoring authored templates -- and stays read-only, offline, keyless, and dependency-free.

## More information

Complements [0003](0003-mcp-integration-and-testing-surface.md) (the MCP surface) and
[0004](0004-decline-http-api.md) (no HTTP API). The one result shape shared by the CLI and the MCP
server lives in [internal/describe](../../internal/describe) and
[internal/result](../../internal/result).
