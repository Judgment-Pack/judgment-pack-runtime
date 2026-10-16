---
status: proposed
date: 2026-10-16
deciders: maintainer
---

# Check version-3 Dialect declarations through the shared authoring tools

## Context

ADRs 0052–0054 provide encoding for integer Dialect formats 2 and 3, but validation,
authoring schema and pack inventory checks only support format 2. Runner already
accepts format-3 bound and unbound operations and selected files. The immutable
Spec revision already pinned by Runtime contains those forms and two format-3
vectors. New portable artifact identity/version semantics remain Spec #180;
these format numbers do not identify reusable artifact releases.

## Decision

Extend the existing shared validator, schema operation and declared inventory
checker to format 3 for `operation` and `selected-file`. An unbound operation is a
closed declaration with all four `platform`, `operation`, `shape`, `record`
members and no profile pins. A partial declaration or a mixture with profile
members fails. Its scopes, arguments, calculation, reads, targets, admissions and
limits pass through the same checks as a bound operation. Names and shapes are
checked for document form only; no catalog, profile or binding is resolved.

Keep format-3 identity spelling checks at the canonical boundary: root legacy
members and the defined optional empty members are refused rather than rewritten.
Validation and coverage retain the exact canonical identity. All existing pinned
vectors and format-2 schema bytes remain unchanged. The format-3 schema derives
its shared definitions from the format-2 editing aid and has its own resource
identity; it remains an implementation aid, not a normative schema.

Defined but unimplemented format-3 kinds `http`, `sql`, `web`, `research` return
`JPS-DIALECT-SOURCE-UNSUPPORTED` and exit 2, before any partial pack coverage is
emitted. Unknown kinds remain malformed forms (exit 1). This operation reports
the first finding, not the preparation verifier's multi-input phase order. The
capability contract adds the supported source kinds, unbound-declaration support
and the explicit absence of binding verification. Successful scope strings name
the actual format; `fullDocumentConformance` stays false.

This extends the version scope of ADRs 0053 and 0054, not their ownership boundary.
Binding, acquisitions, preparation and execution stay with Runner/Gateway.
Validation cannot authorize a write, prove source truth or imply installed
execution support. CLI and MCP remain thin, equivalent, offline surfaces over
bounded supplied bytes. No dependency, service, workspace write, migration,
release, artifact bump, or Spec change is introduced.

## Verification and review

Tests admit both staged format-3 vectors and the original unbound document without
changing their identities, exercise both selected-file forms and all declared
operation shapes, refuse mixed/partial pins and invalid empty spellings, preserve
parameter ordering and limits, and compare CLI/MCP and schema responses. Existing
format-2 and canonical fixture tests remain required.

Material-decision impact: public-surface, documented-claim, conformance. The PR
records an independent clean-room Codex review under the maintainer's explicit
instruction to use that review process for this continuation. It is a same-vendor
review and does not claim to satisfy the repository's different-vendor criterion.
