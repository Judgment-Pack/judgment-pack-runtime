---
status: accepted
date: 2026-07-24
deciders: Brian Jin
---

# Language plurality lives at the wire and in thin clients, not polyglot packages

## Context and problem statement

The runtime will grow additional surfaces — an MCP adapter, perhaps an HTTP API, an in-process
`sdk` — and consumers will want to reach it from languages other than Go. The question is where the
language boundary belongs: could each surface be its own language (a polyglot monorepo), and how are
non-Go consumers served without creating a second implementation that can disagree with this one?

## Decision drivers

- This is the **reference** runtime; it participates in defining conformance, so two encodings of
  the validation logic that can drift apart is the worst outcome.
- The layers share one in-process, typed data model (carrier → validation → describe → result).
- The runtime ships as a single static binary.
- The specification roadmap prizes small implementation complexity and independent implementations
  in other languages — but as *whole conforming validators*, not as fragments of this one.

## Considered options

- **A. One language for the runtime; plurality at the wire and in thin clients.** Keep the runtime
  in Go. Serve other languages over wire protocols (MCP, HTTP) and via thin per-language client
  libraries that route back to this core; whole reimplementations live in their own repositories as
  independent conforming implementations.
- **B. Polyglot packages within this repository.** Core in one language, an SDK or adapter in
  another, wired together across in-repo boundaries.

## Decision outcome

Chosen option: **A**. The distinction that settles it is protocol versus library:

- **MCP and HTTP are wire protocols, not libraries.** A Go server speaking them is reachable by any
  client language with zero per-language work; the server's language is invisible to the caller.
- **An SDK is a library, so it is inherently per-language.** The Go `sdk` package is an in-process
  API for Go embedders. A Python or TypeScript client is a *separate distribution* in its own
  repository that reaches this runtime over the wire, by invoking the binary, or through C-ABI/WASM
  bindings — never by reimporting logic.
- Reimplementing validation in another language is legitimate, but it is then an **independent
  implementation** that must pass the published conformance corpus as one, not an "SDK."

As long as every client routes back to this single core, the no-drift guarantee holds. `api` and
`mcp`, when built, are Go adapters wired as subcommands of the one binary; they add transport, not
judgment.

### Consequences

- Good, because there is exactly one validation implementation to trust, and all-language client
  access comes free from the wire surfaces.
- Good, because the single-binary distribution and the shared in-process data model are preserved.
- Bad, because an idiomatic client in another language is real, separate work rather than a folder
  in this repository.
- Revisit if the single-binary or single-implementation goals are dropped, or if an in-repo
  multi-language workspace becomes necessary (which would also reopen
  [0001-idiomatic-go-single-module-layout.md](0001-idiomatic-go-single-module-layout.md)).

## More information

Governs the `internal/mcp` and `sdk` package seams scaffolded on the `feat/seam-prep` work. Related:
[0003-mcp-integration-and-testing-surface.md](0003-mcp-integration-and-testing-surface.md). The
independence of the CLI, `specVersion`, and machine-output versions is described in
[architecture.md](../architecture.md).
