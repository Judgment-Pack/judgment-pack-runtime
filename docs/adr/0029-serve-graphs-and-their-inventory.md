---
status: proposed
date: 2026-08-24
deciders: maintainer
---

# Serve the configured graphs and their inventory over the wire, read-only

## Context and problem statement

A wire-only client of the graph surface can run every configured graph's matrix
(`experimental_test_graphs`, [ADR-0026](0026-run-the-declared-graph-matrix-over-mcp.md)) and can
fetch every node's pack (`get_pack`), but it cannot read the one artifact that states the
composition: no tool serves a configured graph document, and no tool lists what a project
configures. The gap is concrete (issue #126, found building exactly such a client): the only
account of a graph's structure that reaches the wire today is the coverage report's probe
namespacing, which can omit a node the run never admitted — so a client renders "the nodes
represented in coverage" and must refuse to draw an edge, because edge endpoints exist only in a
document it cannot fetch. The configuration schema has anticipated the missing half since
[ADR-0017](0017-declare-graphs-in-the-project-configuration.md): a graph entry's `description`
exists, in the schema's own words, "for a human or an agent reading the inventory" — an inventory
that did not exist.

The pack side already settles every shape question: `list_packs` and `get_pack` serve the project
convention read-only, lenient about identity ("listing is not validating"), rooted in the
configuration's own directory, with a missing configuration answered emptily by the listing and
as an explained error by the fetch. What has to be decided is where the graph siblings live, what
they are named, and which of two live digest conventions their payload follows.

## Decision drivers

- The separation [ADR-0015](0015-experimental-graph-surface.md) drew and ADR-0017 kept: nothing
  graph-shaped enters a stable command's payload, because an experimental member inside a payload
  that must not shrink is a removal this runtime could no longer afford.
  [ADR-0021](0021-run-the-declared-matrix-over-mcp.md) and ADR-0026 each refused the adjacent
  folding for the same reason.
- [ADR-0007](0007-experimental-evaluator.md)'s marker discipline as ADR-0021 restated it for MCP:
  the tool *name* carries the experimental marker, because MCP has no command group to carry it
  instead — and every graph surface, the non-evaluating verbs included, sits inside the CLI's
  `experimental` group.
- The one-inventory rule the pack side proves in test: the CLI and MCP answers are one function's
  output, so the two surfaces cannot disagree.
- Serving is not validating: the mid-edit document is the one a client most needs to see, and the
  moment validation would refuse it is the moment a serving tool must not go silent.
- The conformance perimeter: these tools reach no evaluator, so the engine-constructor census
  (three `evaluation.NewEngine` sites in the MCP package, guarded by test) and CONFORMANCE.md's
  "eight surfaces" sentence must not move.

## Considered options

- **A. `experimental_list_graphs` and `experimental_get_graph`, a separate `GraphInventory` /
  `GraphDocument` payload pair, and a CLI twin `experimental graph list`.**
- **B. Fold graph rows into `list_packs` / `PackInventory`.**
- **C. Unprefixed `list_graphs` / `get_graph`.**
- **D. Option A without the CLI twin.**

## Decision outcome

Chosen option: **A**.

Option B puts a removable member inside the payload the stable `packs list` renders — the exact
coupling three records refused, and the reason the payload types are new rather than members of
`PackInventory`. Option C reads better and lies about stability: `get_pack_diagram` shipped
unprefixed in 0.6.x and its withdrawal is the cautionary precedent ADR-0021 cites; these tools
serve a convention whose every other surface is marked experimental, and the marker is a
stability statement the name must carry on MCP. Option D leaves the inventory reachable over the
wire and not from a shell, inverting the parity the pack side keeps by construction; the fetch
half needs no CLI twin because a shell already has the file, but the *resolved* inventory — the
document identities read beside the configured ids — is worth the same one-function parity, so
`experimental graph list` ships with it.

Settled constraints:

1. **Two tools, read-only.** `experimental_list_graphs` (no arguments) resolves every configured
   graph: configured id beside the document's own `id` and `version` (two names, two members —
   the pack inventory's rule), the declared `formatVersion` and `result` node, node and edge
   counts as carried, paths, whether rows are declared, and the configuration's description.
   `experimental_get_graph { graph_id }` serves the exact bytes beside their metadata, through
   the same rooted reader every project read uses, under the graph surface's own byte limit
   (ADR-0017 deliberately left the limit with the caller). Neither holds a credential, opens a
   connection, writes anything, or constructs an engine.
2. **Lenient identity, exactly as packs.** Identity is read off the served bytes by a bare
   carrier decode — never the closed-schema `Load`, which would make a schema-invalid graph
   unservable. An unreadable or undecodable document is a listed row whose `detail` says why
   (identity members empty, never guessed) and a served document with `status` `"undecodable"`;
   `experimental graph validate` is where a broken graph is an error.
3. **Missing configuration, the established asymmetry.** The listing answers empty with a note
   saying where the runtime looked; the fetch is an error carrying the same note, because a fetch
   by id cannot succeed emptily.
4. **The payload's digest is `sha256`, bare hex** — the payload-member convention
   (`PackDocument`, the schema payloads), not the `sha256:`-prefixed `digest` the lock and audit
   records use. Two conventions exist; a payload member named `sha256` carrying a prefixed value
   would be the one incoherent combination.
5. **`formatVersion` here is the document's own declaration**, read off the served bytes the way
   a `PackDocument`'s `specVersion` is — not the format version a walk applied, which is what
   the member means on an evaluation payload, where the distinction is load-bearing and stays as
   the result package states it.
6. **One inventory function serves both surfaces.** The CLI's `experimental graph list` renders
   the same `GraphInventory` the MCP tool returns, built in `internal/graph` — the import points
   from the graph surface to the project convention, never the reverse, which is also why the
   builders do not live in `internal/project`.
7. **Arguments are held exactly.** `graph_id` is held to its spelling (the `exactMembers` hold,
   so a case-folded alias is refused) and its type (an explicit null is a bad invocation), and
   the required-argument error names `experimental_list_graphs` as where the ids come from.

### Consequences

- Good, because a wire-only client can finally read the composition it renders: real edges from
  the document, not an axis apologizing for their absence — and issues #127 and #128 shrink to
  what they actually are, payload questions on the walk, now that the document itself is
  servable.
- Good, because the inventory the configuration schema promised exists, on both surfaces, from
  one function.
- Bad, because the tool roster grows by two and every roster text moved with it — the help, the
  package docs, the client guide's table, and the tools/list census in test.
- Neutral, because the conformance perimeter is untouched: no new engine site, no claim stated,
  and the reference-only scan admits the new descriptions as it admits every other.
- Neutral, because the experimental marker keeps its meaning: these tools may change or vanish
  with the convention they serve, and their names say so where MCP has nowhere else to say it.
