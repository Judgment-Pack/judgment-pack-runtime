---
status: proposed
date: 2026-10-09
---

# Bind graph evaluations to the bytes actually loaded

A client can retain a rehearsal while the project changes. Pack version strings
and a configured graph id cannot tell it whether the displayed result belongs
to the current files. Re-reading a file to hash it after evaluation is also
incorrect: the second read may contain different bytes.

## Decision

Add three provenance members to the experimental graph evaluation answer:

- `graphSha256`: the graph document's loaded bytes.
- `configSha256`: the configuration bytes used to resolve the project.
- `nodes[].packSha256`: the bytes passed to that node's pack evaluator.

Values are lowercase, bare-hex SHA-256, consistent with graph document and
matrix bindings. Compute pack digests from the existing evaluation buffer and
reuse graph/configuration load digests. Do not add filesystem reads. Repeated
nodes referencing one pack carry separate bindings for their respective reads.

This identifies bytes; it does not assert an atomic project snapshot, reviewed
status, correct policy/facts, authorization, or conformance of composition.
The runtime's experimental/non-normative labels and per-node conformance claim
remain unchanged. Rehearsals remain outside the decision audit trail. Existing
audited decision semantics are unchanged.

A client may compare every binding against its current reads and show that the
revisions match or differ. It must keep old or incomplete payloads separate
from claims about the current document. Independent current reads do not
establish an atomic snapshot either.

## Verification

`evaluation_digest_test.go` changes graph/configuration files after loading,
and pack files after reading but before evaluation, to detect accidental
rereads. It also evaluates a repeated pack whose bytes change between nodes.
`testdata/composition` exercises explicit fan-in, fan-out, repeated packs and
unresolved propagation. The existing project fixture covers sequences.

Graph nesting and scheduled execution are separate follow-ups. No graph
format or specification version changes here, and no Demo repository is
required for examples.
