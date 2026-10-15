---
status: accepted
date: 2026-10-15
deciders: maintainer
---

# Validate versioned dialect documents without preparing them

## Context

ADR-0052 supplies a canonical writer but deliberately refuses to call encoding
validation. Runtime #275 also requests generic authoring checks that Desk and
other clients can use before saving a dialect. The pinned companion defines
version-2 document forms independently of installed input profiles and acquired
responses. Its proposed version-3 kinds do not yet have independent vectors.

## Decision

Add `jpack dialect validate <dialect-or->` and the read-only MCP tool
`dialect_validate`, with `document` supplied as exact JSON text. Both call the
same bounded, offline validator for version 2 at the Spec revision pinned by
ADR-0052. There is no project lookup, store, acquisition, model call or evaluation.

The checks cover closed object forms, pointers, case/source parameters and their
scopes, dependencies on earlier source targets, source declarations, request
templates, copy/rule reads, derivation conditions and claims, calculation
bindings, non-overlapping fact targets, evidence ownership, admission references
and all document-local count limits. `operation` and both `selected-file`
providers are admitted by their document form. Their inclusion says nothing about
installed support or preparation-verifier claimability.

The validator does not accept profiles, acquisitions, binding documents or packs.
Checks that need them are outside its scope: profile/request fit, selected-file
acquisition, whether a profile is a calculator, admission against source classes,
source truth, request execution and preparation verification. The document's
`unmapped` declarations are checked for form only, as the companion specifies.
The condition `freshWithin` checks declared parameter references; parameter types
that make the condition false do not make its form invalid (§6.3).

A successful report says `status: "document-valid"` and
`validationScope: "version-2-document"`. It retains exact canonical bytes and
their digest and continues to say `fullDocumentConformance: false`: this is no
preparation-verifier conformance claim. It neither fills defaults nor rewrites
saved data. A refusal contains diagnostics and no usable bytes or digest.
Diagnostics carry deterministic JSON pointers and fixed messages, not source
values. Existing exit classes and MCP `isError` apply. Validation returns the
first finding; its diagnostic precedence is this tool's, not the preparation
verifier's multi-input phase order.

Capability discovery adds `validationVersions: [2]`, distinct from
`encodingVersions: [2, 3]`, and says that document structural/semantic checks
are present. Profile compatibility, pack compatibility and preparation
verification remain false. Version 3 is explicitly unsupported by validation.
Clients must not turn an encoding pass into a validation pass for that version.
`schema` and pack compatibility are subsequent deliveries of #275.

## Evidence and consequences

The ten independently pinned version-2 mappings pass the document checks without
changing their canonical identity. Focused tests cover malformed forms, target
and parameter dependencies, template references, conditions, source providers,
exact count boundaries and refusals. The CLI and MCP parity suite exercises
validation alongside encoding; fuzzing checks refusal and stability invariants.
The pinned Spec's independent reader is used as an additional differential check,
not shipped as a runtime dependency. No preparation-verifier claim is made.

The operation is additive and calls for a minor release under VERSIONING.md. It
changes no evaluator behavior, audit bytes, current command or stored document.
The generic document boundary stays in Runtime; Runner and Desk remain its
consumers. No release, migration or automatic saving is part of this change.

Before merge, the material public-surface, documented-claim and conformance
changes require a recorded cross-vendor review tied to the reviewed commit under
this directory's README. A clean-room Codex review of Codex-assisted work is
additional evidence, not that review.
