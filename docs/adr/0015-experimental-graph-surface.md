---
status: accepted
date: 2026-07-29
deciders: maintainer
---

# Prototype pack composition as an experimental graph surface

## Context and problem statement

The Judgment Pack Specification deliberately ships the single decision and defers composition:
its RFC 0002 (Draft) proposes a Judgment Graph — packs composed into a directed acyclic
structure where one decision's outcome feeds another's inputs — and leaves the evaluation
semantics as open questions: ordering and conflict, the fact and evidence namespace, cycles,
partial failure, and whether a composite result is an artifact at all. Those questions cannot
be answered from an armchair, and this runtime already holds every piece a prototype needs:
the evaluator, the project convention that names packs by decision id (ADR-0012), and the
experimental-surface posture that shipped the evaluator itself before the specification
accepted its conformance class (ADR-0007, RFC 0006). The pressure is also concrete rather
than hypothetical: real projects smuggle an upstream decision's verdict into a downstream
pack as a plain fact, because a fact is the only device the format offers for it.

## Decision drivers

- Produce measured design evidence for RFC 0002's owed semantics, on the path RFC 0006
  walked: prototype in the runtime, report findings to the specification.
- Change no normative surface and state no new claim: composition must ship as clearly
  non-normative, labeled in band, and removable.
- Lean on decisions already made — identity in the pack document, resolution through the
  project's jailed handle, errors are never dispositions, unknown is first-class — rather
  than inventing parallel machinery.
- Keep each node's evaluation exactly the specified evaluation: the composition layer must
  never reach inside §§7-8 or alter what any pack means.

## Considered options

- Do nothing until the specification settles RFC 0002.
- Declare graphs inside `jpack.json` by extending the project configuration schema.
- A standalone graph document named by path, evaluated by a new `experimental graph` CLI
  surface in this runtime.
- Expose composition as an MCP tool immediately alongside the CLI.

## Decision outcome

Chosen option: "a standalone graph document with an `experimental graph` CLI surface",
because a prototype that waits for the specification inverts the project's evidence-first
process, while extending `jpack.json` would couple a stable convention's configVersion to the
least stable surface in the runtime — a graph document with its own `formatVersion` can change
or disappear without touching anything. The MCP surface waits until the shape survives use;
a CLI is enough to generate evidence, and tool-surface growth is harder to walk back.

The format: a closed-schema document (`formatVersion` `"1"`, deliberately not named
`graphVersion` — the document's own `version` member is the graph's identity version, and the
two must never share a name in a document or a payload) of nodes, edges, and one declared
result node. A node references one pack by the project's decision id; identity stays in the
pack document, and the reference is resolved through the project's own directory handle. An
edge feeds one upstream node's disposition downstream in one or both of two declared ways —
the outcome id written at an RFC 6901 facts pointer, and a tri-state contribution to a
declared evidence requirement — and must feed at least one.

The evaluation semantics take positions on RFC 0002's open questions, each encoded in the
conformance-style tests of `internal/graph`:

- **Ordering and conflict.** Deterministic topological order, smallest node id first. Two
  edges feeding the same fact pointer, overlapping fact pointers (one inside the other,
  compared on decoded tokens), or the same evidence requirement of one node are each a
  validation error; a feed colliding with a caller-supplied input is an evaluation refusal.
  Merge rules are a semantics nobody declared, so there are none, and every collision between
  a fact edge and the caller's own inputs is refused before any node runs — unconditionally,
  because a guard that waited for an upstream disposition would let a caller's value stand in
  for an outcome an unresolved upstream never produced. Assembly is bounded: a fact pointer is
  at most 64 tokens deep, one node's injected outcome ids are charged against the carrier's
  byte limit before they are written, and an assembled document past what the preflight admits
  is handed to the engine as oversized rather than carried, so composing inputs allocates no
  more than the inputs document's own limit already admitted.
- **Namespace.** Mapped, never shared. Every node evaluates against its own facts and
  evidence documents; the only values that cross a node boundary are the ones an edge
  explicitly places.
- **Cycles.** Refused at validation. A graph is a DAG or it is not a graph this runtime runs.
- **Partial failure.** An upstream that produced no outcome injects no fact — the pointer is
  simply absent, which §7 already reads as unknown — and contributes the edge's declared
  `onUnresolved` tri-state, `unknown` by default, `absent` when the edge says a decision that
  resolved nothing is itself the missing evidence. Unresolved upstreams therefore reach a
  downstream pack only through that pack's own declared unknown handling, and every requested
  handoff is aggregated in the composite so an upstream escalation cannot be missed. A node
  the engine refuses refuses the whole run with that node named and its §8.4 class intact:
  errors are not dispositions, composites included.
- **Composite result.** An envelope, not a new judgment: the per-node §8.3 dispositions side
  by side, the declared result node's disposition echoed as the headline on the ADR-0012
  validated-reference pattern, and a label in every payload saying that no JPS version
  defines a graph or a composite result. Whether such an envelope should ever become a
  portable artifact is exactly the question the prototype exists to inform, so the payload
  carries everything that answer would need and asserts nothing about it.

### Consequences

- Good, because the demo seam — a screening decision faked as a downstream fact — can be
  declared as an edge, and the fixture pair in `internal/graph/testdata` proves the
  escalation propagation end to end.
- Good, because every RFC 0002 position is now a failing test away from revision, which is
  the cheapest form a design argument can take.
- Bad, because the runtime carries a fourth document format (pack, matrix, configuration,
  graph) with its own schema, version gate, and byte limits to maintain.
- Bad, because two prototypes by one author are not independent evidence for the
  specification's graduation gates; this surface informs RFC 0002's design, and nothing
  about it advances the RFC's acceptance by itself.
- Revisit when RFC 0002 moves — its text should absorb or overrule these positions — or when
  the surface needs an MCP tool, which reopens the tool-growth question deliberately deferred
  here.

## More information

Spec RFC 0002 (Judgment Graph composition, Draft) and RFC 0006's acceptance note, which
left graph interaction to RFC 0002. ADR-0007 (experimental evaluator), ADR-0012 (project
convention), and the experimental evaluator's conformance claim, which is stated, in full
and only, in CONFORMANCE.md and is unaffected by this surface: a composite payload
references that file exactly as every other experimental-evaluation payload does.
