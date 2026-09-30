---
status: accepted
date: 2026-09-30
deciders: maintainer
---

# Declare a graph evaluation a rehearsal, on ADR-0028's terms

## Context and problem statement

[ADR-0028](0028-declare-an-evaluation-a-rehearsal.md) let a caller declare one evaluation a
rehearsal: `experimental evaluate --rehearsal` and `experimental_evaluate`'s `rehearsal` run the
same evaluation, append no audit record, consult no reviewed set, and label the payload. Its clause 5
limited the scope to that surface and left this open: "the graph *evaluation* surface, if it ever
wants a rehearsal, takes this ADR's shape as precedent rather than being amended into it here".

`experimental graph evaluate` appends a record per node and one for the composite in a project that
declares an audit directory (ADR-0018), and it consults the reviewed set for the configuration, the
graph document and each node's pack (ADR-0019). Every exploratory graph run was therefore written down
as a decision, and under drifted law it could not be explored at all without authoring a matrix row
(issue #182). The README already described graph evaluation as one of the surfaces a declared
rehearsal exempts, which was not true.

## Decision drivers

- ADR-0028's drivers, unchanged: exploration must never write a decision record, the declaration is
  explicit rather than inferred, and a rehearsal's payload says in band that it was not a decision.
- One word, one meaning: "rehearsal" means the same thing on every surface that accepts it.

## Considered options

- **A. `--rehearsal` on `experimental graph evaluate`, with ADR-0028's meaning, whole.**
- **B. A rehearsal that skips the audit trail but still consults the reviewed set.** ADR-0028
  rejected this split as its option B, for the same reason it applies here.
- **C. Leave graph evaluation without a rehearsal.** Exploring a graph then means writing a record or
  authoring a matrix row, and the README's description stays false.

## Decision outcome

Chosen option: **A**, ADR-0028's shape applied to the composite.

1. **The declaration.** CLI `--rehearsal` on `experimental graph evaluate`. There is no MCP graph
   evaluation tool, so there is no MCP form.
2. **What it skips.** No audit record is appended, for any node or for the composite. No reviewed set
   is read or consulted, for the configuration, the graph document, or any node's pack. This is the
   standing `experimental graph test` already has: the run leaves the audit writer and the law check
   unset, as that verb does. Nothing else changes: the same engine, the same options, and the same
   composite, node dispositions and traces the recorded run would produce.
3. **The label.** The composite payload carries `"rehearsal": true` exactly when declared, beside
   `experimental`, and the human rendering says so in the same line ADR-0028's rendering uses. It is
   absent otherwise: additive output under VERSIONING.md's MINOR rule, and `outputVersion` stays
   `"2"`.
4. **Citations.** `--cites` is still held to its shape; a rehearsal records nothing, citations
   included, as ADR-0028 settled for the standalone surface.

### Consequences

- Good, because a graph can be explored under any law, drifted or not, without the trail recording a
  decision nobody took, and the README's account of the deciding surfaces is now true.
- Bad, because a second surface now carries a flag that turns off recording. The flag is per call and
  the payload says so, which is the protection ADR-0028 relies on.
- Revisit if an MCP graph evaluation tool is added: it takes a `rehearsal` boolean on these terms.

## More information

Issue #182. ADR-0018 (the audit trail), ADR-0019 (the reviewed set), ADR-0021 (a matrix row's
standing), ADR-0028 (the rehearsal).
