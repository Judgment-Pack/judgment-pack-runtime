---
status: proposed
date: 2026-11-12
deciders: maintainer
---

# Let the dialect check read Core 0.5.0 packs

## Context and problem statement

`jpack dialect check` (and the MCP `dialect_check` tool) compares a dialect document's mapping
targets with the fact pointers and evidence-requirement ids a pack declares. Since
[ADR-0077](0077-admit-core-0-5-0-and-retire-the-staged-selector.md) it refuses a pack declaring
`specVersion` `0.5.0` with `JPS-SURFACE-SPEC-VERSION-UNSUPPORTED`, although pack validation and
evaluation read that version. The same pack declared `0.3.0` is checked (issue #390).

ADR-0077 refused it because the dialect companion is written against Core `0.3.0`. The companion
governs the dialect document, and this release reads that document exactly as before. What the
check reads from the pack is narrower: the inventory `project.InspectDocument` derives, the same
over-approximating walk `packs list` and `packs validate` use for every version
([ADR-0020](0020-report-consulted-fact-pointers.md)).

Material-decision impact: public-surface.

## Decision drivers

- A pack the runtime validates and evaluates is not refused by a tool that compares pointers, unless
  its members can change which pointers it reads.
- A Core version this check has not been reviewed against is still refused by name, not admitted
  because validation came to admit it.
- The dialect document's own contract does not move.

## Considered options

- **A. Admit `0.5.0` by name**: the check keeps an explicit list of the Core versions it reads, now
  `0.1.0-draft`, `0.2.0-draft`, `0.3.0` and `0.5.0`, and refuses any other validated version with
  the existing code.
- **B. Admit every version validation admits.** A later Core version would be checked without
  anyone reading its members against the walk.
- **C. Read the pack's evaluation form** (Core §8.5) instead of its bytes. It removes annotations and
  expands value-set references into the condition's `value`; neither moves a pointer, so the
  inventory is the same, and the reported digest would no longer be over the supplied bytes.

## Decision outcome

Chosen option: **A**.

1. **What `0.5.0` adds, read against the walk.** Annotations hold only strings and lists of
   strings, so they carry no object a fact condition could be mistaken for. A fact condition that
   names a value set (`"operator": "in"`, `"valueSet"`) keeps its `op` and `path` and is collected
   like any other. A value-set entry's `value` is data, read like a condition's `value` literal: an
   entry shaped like a condition is collected, the over-approximation ADR-0020 records for every
   literal and that `inventoryOverApproximates: true` states. Evidence requirements are unchanged.
2. **What moves.** A valid `0.5.0` pack gets the coverage report a `0.3.0` pack gets. The refusal
   message for a version outside the list names `0.5.0` among the versions read.
3. **What does not move.** Pack validation runs first, as before: a pack declaring `0.6.0`, `0.4.0`,
   `0.4.0-draft` or any other unknown version fails validation as `unsupported` and is refused with
   `JPS-DIALECT-PACK-INVALID`, exit 2, with no coverage. The dialect document, its schema and its
   versions are unchanged.

This supersedes ADR-0077's "the dialect check ... refuse[s] a `0.5.0` pack" for the dialect check
only. The policy check's refusal, the graph commands' refusal and pack-design analysis's refusal
stand as ADR-0077 records them.

### Consequences

- Good, because a project that moves its packs to `0.5.0` keeps its dialect coverage check.
- Bad, because a value-set entry shaped like a condition is reported as a consulted pointer, as a
  literal of that shape already is.
- Revisit when a later Core version is admitted by validation: it is refused here until it is read
  against the walk and added to the list.

## More information

Issue #390. Implementation: `readableVersions` and `readsVersion` in `internal/dialect/check.go`.
ADR-0020 (the inventory walk), ADR-0052 (the dialect check), ADR-0077 (Core `0.5.0`).
