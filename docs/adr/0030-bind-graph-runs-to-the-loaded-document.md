---
status: proposed
date: 2026-08-24
deciders: maintainer
---

# Bind every graph run to the document it loaded, on the wire

## Context and problem statement

A wire-only client can fetch a configured graph document (`experimental_get_graph`,
[ADR-0029](0029-serve-graphs-and-their-inventory.md), digest included) and run its matrix
(`experimental_test_graphs`, [ADR-0026](0026-run-the-declared-graph-matrix-over-mcp.md)) — two
calls, two reads, one file that may be edited between them. The matrix payload carries no digest
of the graph document its run loaded, so a client joining the two — nodes by name, edge witnesses
by coverage index — cannot prove the rows and the structure describe one revision, and a review
of exactly such a client (issue #132) found the join silently combining revision-A rows with
revision-B arrows across an edit. The client's interim mitigation is connection-epoch gating,
which bounds staleness without ever proving sameness.

The value already exists at exactly the right moment: `graph.Load` computes the digest of the
exact bytes it decoded, the lock ([ADR-0019](0019-reviewed-set-lock.md)) already pins graph
digests per configured id, and the walk holds the loaded document in hand when it builds each
entry. Only the wire omits it.

## Decision drivers

- The echo rule every graph payload already follows: report what this run actually read, off the
  one read, never off a second one that could name a different revision.
- The payload digest convention settled in ADR-0029's rounds: a member named for its algorithm
  carries bare hex; the `sha256:`-prefixed spelling belongs to the lock and audit records.
- Absence must be honest: a document that did not load has no bytes to bind, and a digest of
  nothing would be an invention beside the detail that says why.
- VERSIONING.md's MINOR rule: additive members move no `outputVersion`.

## Considered options

- **A. `graphSha256` on the three run payloads** — the suite entry, its validation twin, and the
  direct single-graph test envelope — read off the loaded document's own digest.
- **B. Client-side binding only** (epoch gating, as the desk does today).
- **C. A digest member on the rows instead of the entry.**

## Decision outcome

Chosen option: **A**. Option B bounds staleness and proves nothing — it is the mitigation this
member exists to retire, not an answer. Option C repeats one fact per row on a surface whose
report budget was redesigned once already for exactly that multiplication (ADR-0026); the
document is loaded once per entry, and the entry is where a per-load fact belongs.

Settled constraints:

1. **Member and format.** `graphSha256`, bare hex, on `GraphSuiteEntry`, `GraphValidationEntry`,
   and `GraphTest` — the digest `graph.Load` computed from the exact bytes this run decoded,
   with the lock/audit `sha256:` prefix stripped at the payload boundary, matching every other
   payload digest member.
2. **Present exactly when the document loaded.** An entry whose document could not be read or
   loaded carries no digest, beside the detail or diagnostics that say why.
3. **The binding it enables, stated for consumers:** equality with `experimental_get_graph`'s
   `sha256` proves the served document and this run's results are about one revision; inequality
   proves an edit happened between the calls. It is a binding of bytes, not a verdict about
   either revision.
4. **Scope.** The matrix and validation surfaces, exactly. The graph *evaluate* composite
   already binds differently — the audit record carries the graph digest per ADR-0018's record
   design — and extending the envelope there is its own decision if a consumer ever needs it.

### Consequences

- Good, because the client's document/matrix join becomes provable instead of epoch-bounded, and
  the desk's recorded mitigation can be retired for a real binding.
- Good, because the member costs one string per entry, charged by the existing report budget like
  every retained member.
- Neutral, because human renderings are unchanged: the member exists for machine consumers doing
  the join; a person reading the walk report is not comparing digests.
- Neutral, because `outputVersion` stays: additive members under the MINOR rule.
