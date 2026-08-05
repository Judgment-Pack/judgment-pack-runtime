---
status: accepted
date: 2026-08-05
deciders: maintainer
---

# Lint every consulted pointer against a producer declaration

## Context and problem statement

A pack consulting a fact pointer no source feeds raises no error anywhere: the
condition is unknowable, every rule touching it escalates, and the system looks
conservative rather than broken — the worst way for a defect to present (issue
[#76], named as ADR-0020's second stage). `packs validate` checks the one
direction it can — every declared hint key names something the document has —
and nothing checks the other: that everything the document consults has a
producer. That defect class lived in each application's own test suite, when it
lived anywhere; the first external consumer wrote exactly that test by hand.

## Decision drivers

- The defect never errors at run time, so a build-time gate is the only place
  it fails loudly.
- ADR-0020 shipped `consultedFactPaths` precisely so this intersection became
  possible; the reasoning should be one command, not each consumer's re-write.
- Zero-setup value: the flagship consumer's hints already equal its consulted
  set, so a mode that needs no new file serves it on day one.
- An application's true producer set can be wider than its hints (the same
  consumer supplies sixteen pointers and hints eight), so an explicit
  declaration must also be possible.
- ADR-0020's recorded quantifier narrownesses were deferred to this design and
  need their disposition.

## Considered options

- **A. `packs lint`, hints-as-producers by default, `--producers <manifest>`
  for an explicit set** (chosen).
- **B. Manifest only** — always require the file; every project authors a new
  artifact before the lint says anything.
- **C. Hints only** — no new format, but a producer set wider than the hints
  cannot be expressed and the consumer keeps its hand-written test.
- **D. Fold the checks into `packs validate`** — one command fewer, but
  validate's contract is "the configuration against the document", it must
  stay green on a hint-free project, and the lint's skipped-is-not-passed
  status discipline conflicts with validate's.

## Decision outcome

Chosen option: "A", because a hint *is* a producer claim in the project's own
words — the project saying where that answer lives — so the zero-setup mode is
not a shortcut but the same declaration read in its other direction; and the
manifest covers the application whose producers outgrow its prose.

Determinations the record settles:

- **The checks.** `fact-producers`: every consulted pointer is covered by a
  producer in one of two directions that mean two different things —
  a producer at the pointer or an ancestor of it is the **declared subtree
  contract** (a producer claims the whole value at its pointer, descendants
  included; the lint checks declarations, never running systems), and a
  producer at a descendant is **structurally true** (writing `/request/type`
  necessarily creates `/request`). The empty pointer is the root: as a
  producer it covers everything, as a consulted pointer any producer covers
  it. `evidence-producers`: every declared requirement has a supplier (both
  modes, per pack); the reverse — nothing supplied that nothing declares —
  is per pack in hints mode, where the declaration is pack-local, and one
  **configuration-level check** in manifest mode, because evidence ids are
  pack-local names and one application-wide list held to every pack
  separately would fail any project whose packs declare different
  requirements. That pack-locality is also a stated limit: one manifest
  string can satisfy two packs' same-named requirements that mean different
  evidence. The issue's third invariant — every referenced requirement is
  declared — is the JPS semantic layer's own check and is deliberately not
  duplicated here.
- **The manifest** is a closed input:
  `{"producersVersion": "1", "facts": [...], "evidence": [...]}`, decoded
  through the strict carrier (duplicate member names refused, exact-case
  member names only, one JSON text) with its value domains held to the same
  patterns the configuration schema holds hint keys to — an entry that could
  never name a real pointer or a declarable requirement is refused, and lists
  are deduplicated. Its version moves on any member change (VERSIONING.md's
  closed-input rule); it is read via the same flag pattern every document
  flag uses (`-` for stdin, URL and remote paths refused as invocations),
  bounded like the configuration; a malformed manifest is an invocation
  failure in the invocation exit class, distinguishable from a completed
  failing lint.
- **The status discipline is `packs test`'s.** `passed` only when a check
  passed somewhere; any failed check fails the run; a run in which nothing was
  checkable is `skipped` and exits 1 — a green lint over zero checks would say
  a project was linted when nothing was.
- **A quantifier pack's fact half is skipped, by name — and the detection is
  operator-keyed, deliberately.** ADR-0020 recorded that a flat consulted
  list reports draft-RFC element-relative pointers without their element
  context; holding those to a flat producer set would fail healthy packs on
  pointers no producer could name, and passing them would trust a list known
  untrustworthy for that shape. The detector keys on the known quantifier
  operators (`exists`, `every`, `uniform`) carrying `where` or `at`, unlike
  ADR-0020's shape-keyed walk, because the two errors are not symmetric
  here: a data literal that merely carries `where` must not silently
  suppress the fact gate for the whole pack, while a future quantifier
  operator this list does not know falls into the flat check and fails
  visibly there — the direction that gets noticed.
- **An unreadable document fails here, on `packs test`'s own discipline.**
  Skipping it would let a broken pack lint clean behind a passing sibling —
  the exact bypass `packs test` closes by classing an unreadable pack as a
  mismatch. `packs validate` remains where the read failure itself is
  diagnosed; the lint's failure says so and points there.
- **Own payload type** (`PackProducersLint`), on PackLock's recorded
  precedent: a surface this new should be removable without taking a member
  out of a payload consumers already read. Its summary is a lint-specific
  `LintCounts` carrying `skipped`, because a lint entry has three outcomes
  and reusing a two-outcome count type would misdescribe its own total.
  `outputVersion` stays `"2"` — a new payload adds no member to any existing
  one.
- **No MCP counterpart yet.** No project-level validation reaches MCP today;
  the lint follows whatever decision first puts one there, rather than
  setting that precedent as a rider.

### Consequences

- Good, because the silent everything-escalates defect fails a build with the
  unproduced pointers named, and the first consumer's hand-written gate
  retires in favor of the runtime's own reasoning.
- Good, because the hints gain a second reader, which is pressure to keep
  them true.
- Bad, because a hints-mode pass says the *declaration* is complete, not that
  the producers actually run — the lint reads claims, not systems, and its
  prose says so. The ancestor-producer direction inherits this squarely: a
  producer claiming `/request` whose system in fact supplies only some leaves
  passes the lint and starves the pack at run time; that is a false
  declaration, which no declaration checker can catch.
- Bad, because ADR-0020's over-approximation crosses here as a failing gate:
  a conforming pack that carries a condition-shaped object as *data* (in a
  condition's value literal, or an extension) fails `fact-producers` on a
  pointer nothing ever reads. That is the deliberate direction of error —
  visible and arguable beats invisible — and the failure detail says so and
  names the remedies: declare the phantom as a producer to acknowledge it,
  restructure the value, or fix the pack.
- Bad, because the quantifier skip leaves the draft-RFC surface unlinted;
  revisit when those shapes leave experimental or when a structured consulted
  form exists to lint against.
- Revisit when a project-level surface first reaches MCP (the lint should
  follow), or when the manifest needs members this shape cannot carry.

## More information

Issue [#76]; ADR-0020 (the consulted set and its recorded narrownesses, both
deferred here); ADR-0012 (hints and the one-statement discipline);
ADR-0019/ADR-0021 (the skipped-is-not-passed status discipline this follows).

[#76]: https://github.com/Judgment-Pack/judgment-pack-runtime/issues/76
