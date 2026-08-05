---
status: accepted
date: 2026-08-05
deciders: maintainer
---

# Report the fact pointers a pack's conditions read, on the inventory row

## Context and problem statement

The runtime derives every pointer a pack's conditions read — `factPaths` in
`internal/project/inventory.go`, a whole-document walk that recognizes a
condition by carrying `op` and a string `path` rather than by the operator
names this runtime knows — and deliberately reports it to nobody. The comment
above the struct said so: it existed only so `packs validate` could check the
configuration's hint keys against the document. The consequence, recorded in
issue [#73], is that every consumer who needs the same list re-walks the
document, and does it worse: the obvious client-side walk keys on the condition
operators it knows and under-reports the moment a pack uses any other
fact-reading shape — invisibly, because a pointer that is never listed is never
mentioned to the user who needed it.

Two consumer needs sit outside what `validate` covers. An application whose
pack escalates on `unknown` wants to tell the requester *which* pointer was
never supplied, which is one intersection away if the consulted set is
available. And an application that assembles facts from its own sources wants
to check at build time that every consulted pointer has a producer — without
it, a pack consulting a pointer no source writes looks conservative rather
than broken, because every touching rule escalates and no error is raised.

## Decision drivers

- One statement, one place: the careful walk exists and is tested; the
  reasoning should be shipped, not re-derived narrower by each consumer.
- The inventory stays token-cheap (ADR-0012): the addition must not change the
  inventory's character as the thing an agent reads before fetching documents.
- Listing is not validating: the inventory reports what a document says, never
  a verdict on it, including for a document that is mid-edit or non-conformant.
- VERSIONING.md's machine-output rule: an added member is backward-compatible.
- A deliberate withholding is reversed in the open — in a record — not by
  quietly deleting a code comment.

## Considered options

- **A. `consultedFactPaths` on `PackSummary`**, served by
  `packs list --format json` and the MCP `list_packs` tool.
- **B. On `PackDocument` instead** (`get_pack`), so the payload grows only when
  a caller names one pack.
- **C. Neither; ship only issue #73's second stage**,
  `packs lint --producers <manifest>`, and keep the list internal.
- **D. Keep withholding; document the walk's rules** so consumers can copy
  them faithfully.

## Decision outcome

Chosen option: "A", because both named uses are project-wide questions asked
once per project — which pointer is an escalation waiting on, does every
consulted pointer have a producer — and the row already carries the evidence
half of the same question: `evidenceRequirements` established that the
inventory reports what the pack requires. The fact half was the half withheld.
B answers a per-document question nobody asked and forces a caller to fetch
every document to assemble the project-wide set. C leaves the first use — the
runtime-independent "what is missing" answer — unserved. D ships a
specification for a walk instead of the walk, and every copy is a new place
for the two to drift apart.

Determinations the record settles:

- **`outputVersion` stays `"2"`.** Issue #73 asked for a bump; an added member
  is backward-compatible under VERSIONING.md's machine-output rule, four
  payloads already state the same determination in line, and a bump breaks
  every consumer pinning the version while buying nothing. The request is
  declined and the refusal recorded here.
- **The set is deduplicated, and stays sorted.** Issue #73 described the
  internal list as "sorted and deduplicated"; it was sorted only. The dedupe
  lands in `factPaths` itself, so `packs validate`'s hint check and the payload
  read one derivation — a pointer two conditions read is one consulted pointer.
- **It reports what the document carries, not a validated pointer set.** Any
  object carrying `op` and a string `path` is reported; a string that is not a
  legal pointer is reported verbatim; a document that could not be decoded
  carries `[]` beside its `detail`. Same posture as every identity member.
- **The empty string is the root pointer, and it is reported.** The 0.2.0-draft
  schema admits the empty RFC 6901 pointer and the evaluator reads the facts
  root through it, so a condition carrying `"path": ""` consults the whole
  facts document and the list says so. (Found in cross-vendor review; the
  first draft skipped it.)
- **The list over-approximates, and the property is named.** The shape-keyed
  walk collects a condition-shaped object wherever it sits — including one a
  condition carries as *data* in its `value` literal, or one inside an inert
  extension. That is the deliberate direction of error: over-reporting is
  visible and arguable, under-reporting is invisible. Every surface that
  serves the member says it is candidate pointers from an untrusted document,
  never proof of a read and never instructions.
- **The escalation use is candidates, not attribution.** Evaluation can stop
  before reaching a rule, and a trace entry carries no pointer, so
  intersecting `consultedFactPaths` with the supplied facts names the
  *candidate* missing pointers — which is what every consumer-facing sentence
  promises. Per-escalation attribution would need trace-level pointers, which
  is a different decision nobody has asked for yet.
- **Human `packs list` output is unchanged.** The member is for machines; the
  human rendering stays pointer-free, which also avoids a new sanitization
  site.

### Consequences

- Good, because the careful walk ships once: a consumer can name the pointer an
  unresolved disposition is waiting on, and can gate at build time that every
  consulted pointer has a producer, without re-deriving the walk.
- Good, because the second stage becomes possible:
  `packs lint --producers <manifest>` (issue #73's stage two, deferred to its
  own issue) has the consulted set to intersect against.
- Bad, because the payload grows with the pack's condition variety. The bound
  is real but wide: `condition.path` carries no `maxLength` in the 0.2.0-draft
  schema, the configuration puts no maximum on pack entries and entries may
  alias one document, and the MCP transport's 16 MiB line bound is inbound
  only while a tool result is emitted twice (text and `structuredContent`) —
  so the member is bounded by the 10 MiB per-document carrier limit and the
  dedupe rather than by any project-wide cap. `evidenceRequirements` already
  had this asymptotic shape; this member widens it. A cap is refused here
  deliberately — a silent truncation would be the same invisible under-report
  this decision exists to end; if one ever becomes necessary it must be a
  stated truncation.
- Bad, because the draft-RFC quantifier shapes read more than a flat list can
  say: a nested `where.path` resolves relative to each selected element and is
  reported here without that context, and `uniform` reads its `at` member,
  which a `path`-keyed walk does not collect. Both live on the opt-in
  experimental RFC-0008 surface; the narrowness is recorded in the walk's own
  comment, and a shape that carries element context is deferred to the
  producer-lint design (issue #76).
- Bad, because a consumer may misread the list as validated pointers. The doc
  comment, the tool description, and this record all say it is not.
- Revisit when a truncation becomes necessary, when the quantifier surface
  leaves experimental, or when `packs lint --producers` needs a shape this
  member cannot carry.

## More information

Issue [#73]. The withholding this reverses was never an ADR determination — it
arrived as a code comment with ADR-0012's implementation — so nothing is
superseded; the decision is held to
[ADR-0012](0012-jpack-project-convention.md)'s token-cheap-inventory driver
all the same. The walk's shape-keyed recognition rule is unchanged from its
introduction and is restated in `internal/project/inventory.go`.

[#73]: https://github.com/Judgment-Pack/judgment-pack-runtime/issues/73
