---
status: accepted
date: 2026-11-13
deciders: maintainer
---

# Compare two pack documents member by member, annotations included, and three-way

## Context and problem statement

`jpack experimental compare` (ADR-0045) evaluates one set of inputs under two packs and reports the
inputs whose §8.3 dispositions or handoff targets differ. A Core 0.5.0 pack is evaluated as its
evaluation form, from which annotations are removed, so two packs that differ only in an outcome
annotation compare as `"different": 0`; so do two packs whose threshold moved where no input falls
between the two values. A reviewer of a revision, and an author reconciling two edits of one base,
needs what the documents say differently, member by member, with annotations at every position Core
0.5.0 gives them, and no key treated apart from another: a later change defines an outcome
annotation whose meaning a reviewer will care about, and the comparison must not need to know it.

Material-decision impact: public-surface.

## Decision drivers

- `experimental compare` writes the bytes it wrote before unless the new part is asked for.
- The comparison is of documents: it evaluates nothing and needs no inputs.
- Every member is covered, keyed by what names it, with old and new values for what changed.
- Order is reported where a reader sees it and ignored where Core says it carries no meaning.
- One stable, versioned JSON form and a short text, both deterministic; the text writes every pack
  string recoverably (ADR-0076).
- Nothing is reported or refused for a reason that is not in the documents.

## Considered options

- An option of `experimental compare` only.
- A new command, `jpack packs diff`, with the Go API's `sdk.Diff` and `sdk.ThreeWayDiff`, and an
  option of compare that adds its output beside the behavioural report.

## Decision outcome

Chosen option: a new command and Go functions, and `experimental compare --structural`, because a
structural comparison needs neither inputs nor an evaluator, the three-way form has no behavioural
meaning at all, and compare's bytes stay as they are whichever is chosen. The command sits under
`packs` beside `packs describe`, whose view it is built from.

- **Surface.** `sdk.Diff(older, newer)` and `sdk.ThreeWayDiff(base, first, second)` return a
  `view.Diff` or `view.ThreeWayDiff`, built only by package `sdk` from views built afresh from the
  documents `Load` admitted, through `sdk/internal/viewhook`, as `Pack.FilteredView` is. Their Go
  types carry the identities, `DifferentDecisions`, `Differs` and, three-way, `Conflicts`; the JSON
  is the report. `jpack packs diff <old> <new>` writes the same bytes; `--base <pack>` makes it
  three-way, the two packs given being first and second. `experimental compare --structural` puts the
  two-way bytes in the payload's `structural` member, as a string, and the text after the human
  report; without the option the payload has no such member. Compare's help and the guide now say
  that annotations, and every member that never reaches a disposition, are outside what it compares;
  its output does not say so, because its bytes stay as they were. There is no MCP tool, as ADR-0045
  §6 has none for compare.
- **specVersions and ids.** Packs of two specVersions are refused (`view.ErrDiffSpecVersion`;
  `JPS-DIFF-MIXED-SPEC-VERSION`, exit 2, in the CLI), because the members a document may hold depend
  on its version: a cross-version comparison would report value sets or routes as added for a reason
  that is the version. Packs whose ids differ are compared, as ADR-0045 compares them: the id is a
  changed pack member, `differentDecisions` is `true`, and the text says so on its first line.
- **Keys.** Each keyed collection (evidence requirements, sources, outcomes, rules, exceptions, value
  sets, and the entries of each set) is compared by id; an entry by its value's canonical JSON text,
  and by its occurrence among the entries holding that value when Core §6.9's repeated values occur.
  A member absent from a version is reported whole, as the view writes it; one present in every
  version is compared member by member, by JSON Pointer relative to it. The pack's own members are
  compared the same way, relative to the document.
- **Annotations.** At the pack, on each outcome and rule, and on each value-set entry, under Core
  0.5.0, each key is compared as one value, a string or a list, with no key treated apart. An
  extensions object that holds only annotations is the empty object it is without them: against
  `"extensions": {}` it is the same object, and against an absent one it is read as absent, so a
  member's first annotation, or its last, is reported once, as an annotation. An empty object against
  an absent one is reported, at `/extensions`. Under a version with no annotations or no value sets,
  the `annotations` and `valueSets` members are absent, not empty.
- **Order.** Collection order is reported in an `order` member, apart from content, over the members
  both of the last two versions hold (old and new; first and second), each version listing those of
  them it holds: Core §4 keeps it for authoring and display. Two versions' orders differ when they put
  the members both hold in different orders, so a member both descendants add at different places is
  an order conflict, though the base never held it. Every other array keeps its
  order, escalation routes, the conditions of `all` and `any` and an annotation list's strings among
  them: arrays of one length are compared position by position, and arrays of two lengths are
  reported whole. `escalation.triggers` and a route's `triggers` are sets (Core §6.7), and their order
  is never a difference. Object member order is no difference anywhere (Core §5 for annotations).
- **Three-way.** For each member, annotation, order and array presence that differs anywhere, the
  status is `first` or `second` when only that descendant differs from the base, `both` when both
  differ from it alike, and `conflict` when they differ from it and from each other. The walk descends
  where all three are objects, or all three arrays of one length; elsewhere it reports at that point,
  comparing the values whole with every set inside them, at any depth, read as a set. Nothing is
  merged.
- **Formats.** Canonical JSON as the view writes it (numbers as written), with `diffVersion` or
  `threeWayDiffVersion` `"1"`, moving by the rule of `viewVersion`. The text writes every string taken
  from a pack, ids and pointers with characters outside a plain set among them, as JSON in printable
  ASCII. `docs/building-with-packs.md` states the members.

### Consequences

- Good, because an annotation-only revision and a moved threshold no input reaches are now visible,
  by id, with old and new values, and compare's existing bytes are unchanged.
- Good, because the comparison has no list of keys or members to keep up: a new annotation key, or a
  new member the view carries, is compared without a change here.
- Bad, because two versions always differ in `/version`, and a three-way comparison of two releases
  of one base reports it as a conflict; the report does not treat identity members apart.
- Bad, because an entry keyed by occurrence pairs repeated values by document order, so removing the
  first of two equal entries reads as the first changed and the second removed.
- Bad, because an extensions object absent in one version and empty in another, beside a vendor
  member the other version holds, are not told apart: each vendor member is reported by name, under
  `/extensions/<name>`, whether the object went or stayed empty. Reporting the object as well would
  report one change twice, and three-way would call two descendants that both dropped the member a
  conflict when one of them kept the object for its annotations.
- Revisit when a reader needs the comparison of packs of two specVersions, or a merge.

## More information

ADR-0045 (behavioural comparison), ADR-0074 (the view), ADR-0077 (Core 0.5.0, with its value sets
and annotations), ADR-0076 (recoverable annotation text). Core 0.5.0 §4, §5, §6.7, §6.9 and §9.2.
