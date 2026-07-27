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
| [0008](0008-mcp-prompts-authoring-method.md)             | Serve authoring method as MCP prompts; the intelligence stays in the client     | proposed |
