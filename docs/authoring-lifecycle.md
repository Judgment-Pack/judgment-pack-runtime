# The authoring lifecycle

This document walks the full create / read / update / delete loop for a judgment pack, end to end,
over both surfaces this runtime exposes — the CLI and the stdio MCP server. It is the concrete
companion to [ADR-0006](adr/0006-authoring-lifecycle-in-the-client.md), which decides *where* the
lifecycle lives; this shows *how* it runs.

## The one question the runtime answers

The runtime is a **stateless oracle**: `bytes -> result`. Every step below consults it for one thing —

> *Is what you now hold conformant?*

It cannot distinguish Create from Update, because both arrive as "validate this document." It keeps
no store, holds no credential, and opens no network connection; its one evaluation surface is the
explicitly EXPERIMENTAL `experimental_evaluate` (ADR-0007), which claims no conformance. So the authoring
loop — the part that holds a document, edits it, saves it, and deletes it — lives entirely in the
**client**. That is the whole of ADR-0006, and everything here follows from it.

## Where the bytes live

"Client-agnostic" means any MCP-capable client with the user's own key. Two client shapes matter,
because they hold the evolving document differently:

- **A filesystem client** (Cline, Claude Code, an IDE assistant) keeps the document as a *file* it
  edits and re-reads. It can use the CLI directly, or the MCP server, or both.
- **A filesystem-less client** keeps the document in the *model's own context*. It has no file to
  point at, so it works entirely through MCP tools that pass documents as **text**.

The runtime serves both without knowing which it is talking to.

## The two surfaces

| Concern | CLI | MCP server (`judgment-pack mcp`) |
| --- | --- | --- |
| Invocation | one-shot process per call | long-lived, JSON-RPC 2.0 over stdio |
| Validate | `spec validate <path-or->` | `validate { document }` |
| Seed a draft | `spec examples [name] [--write …]` | `list_examples` / `get_example { name }` |
| Reference schema | `spec schema <version> [--write …]` | `get_schema { spec_version }` |
| Self-check | `spec test-conformance` | `test_conformance` |
| How a document is passed | a **path** is allowed | **text only**, never a path |

The CLI accepts a path because it is the user's own local, short-lived process. The long-lived wire
endpoint accepts only text: a path there would make a tool's result depend on the server's ambient
filesystem and break portability across client topologies (ADR-0006). Both surfaces route through the
one shared core in [`internal/describe`](../internal/describe) and
[`internal/result`](../internal/result), so neither can drift from the other.

## The examples are fixtures, not templates

`list_examples` / `get_example` (and CLI `spec examples`) surface the eight **valid conformance
fixtures** the runtime already embeds and digest-locks. They are version-pinned — they change only
when the artifact bundle is re-pinned — and every payload labels itself
`kind: version-pinned-conformance-fixture` so a client never mistakes one for an authored template.
The runtime does not vendor authored example packs; the specification owns those. Surfacing
already-embedded fixtures read-only is distinct from vendoring templates, and is permitted by the
amended [ADR-0003](adr/0003-mcp-integration-and-testing-surface.md).

They exist to give a filesystem-less client a conformant *starting point* for Create. Copy one into a
document of your own and edit from there; it is not your pack, and the runtime never stores or serves
your pack back.

> **One fixture reports `unsupported`, not `valid`, and that is correct.**
> `required-extension-supported` declares a *required extension*, so `validate` reports `unsupported`
> (exit 2, `JPS-CAPABILITY-REQUIRED-EXTENSION`) unless the consumer supports that extension — and the
> validate surface has no option to declare support (that capability context lives in the conformance
> suite's manifest, not on the one-shot validate call). This is a truthful capability outcome, not an
> authoring error: the document is conformant through the semantic layer, and the `extensions` summary
> names the unsupported extension. Treat "validate until `valid`" as the loop for the ordinary case;
> for a pack that declares a required extension, `unsupported` from a non-supporting consumer is the
> expected verdict.

## The loop

### Create

The client produces the initial bytes, then validates until the runtime reports `valid`.

- **Filesystem-less client:** call `list_examples`, pick a fixture whose focus is closest to the
  target, call `get_example { name }` to receive its text, copy that into a working document in
  context, edit, and call `validate { document }`. The [self-sufficient diagnostics](agent-testing.md)
  drive the fix loop; `get_schema` is the reference of last resort.
- **Filesystem client:** the same, but the working document is a file — either seeded from a fixture
  (`spec examples <name> --write pack.json`) or written from scratch — and validated with
  `spec validate pack.json`.

### Read

The client reads its **own** copy (a file, or the document in context) and may `validate` it to
confirm it is still conformant against the current bundle. The runtime never serves a pack back:
`get_example` returns a fixture, never something you authored.

### Update

Identical to Create — edit the bytes the client holds and re-validate. The runtime cannot tell the
two apart and does not need to; it re-answers the one question against the new bytes.

### Delete

The client removes its own file (or drops the document from context). Then **re-validate the
survivors**: the semantic layer is the safety net for a delete that left a dangling reference — an
exception whose target rule is gone, an outcome or fact reference to a declaration you removed. The
runtime tracks no cross-file relationship and owns no store, so re-validation is *how* you catch a
delete that broke a survivor, not something the runtime notices on its own.

## A worked run (CLI)

Seed a draft from a fixture, validate it, break it, read the diagnostic, fix it, and re-validate —
real output from this runtime:

```console
$ judgment-pack spec examples minimal-literal --write pack.json
JPS example minimal-literal
focus: minimal structurally and semantically conforming document
spec: §§2–7
sha256: 516a9d6c92052b291f7e065d514ba89169975e50c216b9b4f59b729a274a52dd
bytes: 611
kind: version-pinned-conformance-fixture
artifacts: immutable-git-ref
written: pack.json

$ judgment-pack spec validate pack.json
valid: JPS document conformance passed (0.1.0-draft)
artifacts: immutable-git-ref · sha256 abc3d3371db5be6c0b63639d399fbe42e3f3e136a162d8d6c2b50503634bbe70

# ...edit pack.json so a rule points at an outcome that was never declared...

$ judgment-pack spec validate pack.json ; echo "exit=$?"
invalid: JPS document conformance was not established
JPS-SEMANTIC-UNRESOLVED-OUTCOME /rules/0/outcome: Outcome reference "approve" does not resolve to a declared outcome. Declared outcome ids: accept, reject.
exit=1

# ...the diagnostic named the offending value, the location, and the valid set; fix it...

$ judgment-pack spec validate pack.json ; echo "exit=$?"
valid: JPS document conformance passed (0.1.0-draft)
artifacts: immutable-git-ref · sha256 abc3d3371db5be6c0b63639d399fbe42e3f3e136a162d8d6c2b50503634bbe70
exit=0
```

(The `artifacts:` line reports the provenance and digest of the bundle that answered; its hex value is
specific to the pinned bundle.)

The exit code carries the machine verdict: `0` valid, `1` invalid. The diagnostic is self-sufficient
— it names the unresolved value, its JSON Pointer location, and the declared ids that would resolve
it — so the fix loop needs nothing beyond the message.

## The same run (MCP)

A filesystem-less client drives the identical loop with tool calls; the document crosses the wire as
text at every step:

1. `list_examples` → choose `minimal-literal` from the returned catalog.
2. `get_example { "name": "minimal-literal" }` → the fixture text arrives as the tool result's
   `content`; its digest and byte size arrive as `structuredContent`.
3. Copy that text into a working document in context, edit it.
4. `validate { "document": "<the edited text>" }` → `structuredContent.status` is `invalid` with the
   same `JPS-SEMANTIC-UNRESOLVED-OUTCOME` diagnostic. A reported-invalid document is a **successful**
   tool call, not a tool error; `isError` is reserved for a failure to run the tool itself.
5. Fix and re-`validate` until `structuredContent.status` is `valid`.

`get_schema` is available throughout as the reference of last resort. No step writes, stores, or
deletes anything on the server; Delete is the client dropping the document and re-validating whatever
remains.

## What the runtime never does

- No store: it never saves, names, lists, overwrites, or deletes user content.
- No credential and no network: the client owns the key; the server opens no connection.
- No path over the wire: MCP documents are values (text), never references to the server's filesystem.
- No evaluation in the authoring loop: validation never resolves a condition, chooses an outcome,
  or authorizes anything. The separate `experimental_evaluate` tool evaluates experimentally, says
  so in every payload, and still authorizes nothing.

## See also

- [ADR-0006 — the authoring lifecycle lives in the client](adr/0006-authoring-lifecycle-in-the-client.md)
- [ADR-0003 — MCP is the integration and testing surface](adr/0003-mcp-integration-and-testing-surface.md)
- [ADR-0004 — permanently decline an in-runtime HTTP API](adr/0004-decline-http-api.md)
- [agent-testing.md](agent-testing.md) — the agent-driven authoring protocol
- [architecture.md](architecture.md) — the runtime as it is
