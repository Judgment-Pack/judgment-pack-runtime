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

- **The checks.** `fact-producers`: every pointer in the pack's consulted set
  is equal to or a descendant of a producer — `producesPointer`, the exact
  inverse of validate's `readsPointer`, because a producer writing a subtree
  answers every pointer beneath it. `evidence-producers`: declared
  `evidenceRequirements` and supplied evidence ids agree in both directions.
  The issue's third invariant — every referenced requirement is declared — is
  the JPS semantic layer's own check and is deliberately not duplicated here.
- **The manifest** is a closed input:
  `{"producersVersion": "1", "facts": [...], "evidence": [...]}`, strictly
  decoded, its version moving on any member change (VERSIONING.md's closed-
  input rule), read via the same flag pattern every document flag uses
  (`-` for stdin, remote paths refused), bounded like the configuration.
- **The status discipline is `packs test`'s.** `passed` only when a check
  passed somewhere; any failed check fails the run; a run in which nothing was
  checkable is `skipped` and exits 1 — a green lint over zero checks would say
  a project was linted when nothing was.
- **A quantifier pack's fact half is skipped, by name.** ADR-0020 recorded
  that a flat consulted list reports draft-RFC element-relative pointers
  without their element context; holding those to a flat producer set would
  fail healthy packs on pointers no producer could name, and passing them
  would trust a list known untrustworthy for that shape. The lint detects the
  shape (a condition carrying `where` or `at`) and reports a named
  `quantifier-scope` skip instead of either verdict.
- **An unreadable document skips here, with the reason.** `packs validate` is
  where a broken pack is an error; two commands failing one defect would
  report it twice in two vocabularies.
- **Own payload type** (`PackProducersLint`), on PackLock's recorded
  precedent: a surface this new should be removable without taking a member
  out of a payload consumers already read. `outputVersion` stays `"2"` — a
  new payload adds no member to any existing one.
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
  prose says so.
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
