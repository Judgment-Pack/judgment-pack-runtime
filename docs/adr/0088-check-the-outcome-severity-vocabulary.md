---
status: accepted
date: 2026-11-13
deciders: maintainer
---

# Check each outcome's severity against the Spec's annotation vocabulary in packs validate, and list it in the view

## Context and problem statement

The Spec's annotation-vocabulary companion (`spec/annotation-vocabulary.md`, vocabulary `1.0.0`,
released with the Spec at `v0.5.2`, status proposed) names one annotation key, `severity`, for an
outcome, with four levels in ascending order: `low`, `medium`, `high`, `critical`. Core `0.5.0`
admits any annotation string, so `spec validate` admits `"hihg"`, `"High"` and `["high"]` alike,
and the view shows the value among the outcome's extensions, with no order across outcomes (issue
#405). The companion asks a reader that applies it to hold each outcome's severity to the levels,
to say why it did not apply them to a pack, and, in a view, to show each outcome's level in a fixed
place and to order the outcomes by it without inventing one.

Material-decision impact: public-surface.

## Decision drivers

- `spec validate` keeps its bytes: Core admits any annotation value, and the vocabulary is not
  Core.
- Evaluation is unchanged: removing the key (the companion's §2.6) changes no disposition.
- The companion's order of steps (§6.2): the selector, then Core, then the vocabulary. A pack Core
  refuses gets Core's diagnostic and no other.
- Every offending outcome is reported, at its pointer, naming the outcome and the value.
- A reader can tell which vocabulary version the binary applied, and why it applied none.
- The view never shows a value that is not a level as a level, never supplies a level, writes no
  number for one, and presents the level as the author's statement.
- This runtime is the second reader of the companion, written by the same maintainer as the Spec.
  Running the Spec's rows here is not an independent implementation of it.

## Considered options

Where the refusal lives:

1. A named check of `packs validate`, `annotation-vocabulary`, on by default, and
   `sdk.Pack.CheckVocabulary` in the Go API; `spec validate` unchanged.
2. The same check, applied only when a flag asks for it.
3. A diagnostic of `spec validate`.

How the view lists severity:

- A. A `severity` member beside `pack` in the view's JSON, a Severity column and two order lines
  in the Markdown, and `viewVersion` `"2"` for every view.
- B. The same, with the version moved only for Core `0.5.0` views: a version per selector.
- C. Each outcome's level written inside the `pack` member.

Where the identifier advisory is carried:

- (i) In the vocabulary report's own `advisories`, which `--fail-on-advisory` counts.
- (ii) In the entry's ADR-0081 `advisories`, beside the overlap advice.
- (iii) Not reported: the companion makes the advisory optional.

## Decision outcome

Chosen options: 1, A and (i).

Option 1, because the issue asks for the refusal in `packs validate` and the companion lets a
reader refuse (§6.4). Option 3 changes `spec validate`'s bytes. Option 2 leaves a misspelled level
admitted unless someone asks, which is the defect the issue names; a flag can be added later
without a break, the reverse cannot.

Option A, because the `pack` member is the document, member for member (ADR-0074), which C would
break. VERSIONING.md gives the view one `viewVersion`, which moves when the bytes for a pack an
earlier version could describe would change; a Core `0.5.0` pack is such a pack, so the version
moves. B would make two version schemes for one format.

Option (i), because ADR-0081 states that the entry's `advisories` are exactly what `spec validate`
reports for the same bytes; the vocabulary's advice there would make that false.

### The check

- It applies to `specVersion` `"0.5.0"` only. Any other selector is `not-checked`, reason
  `spec-version`.
- Core runs first. When Core refuses the pack, the result is `not-checked`, reason
  `core-refused`, with Core's layer in `coreLayer`, and no vocabulary finding; an operational
  failure of the Core check is reported as `core-refused` at the `carrier` layer. A pack whose
  only Core finding is a required extension this runtime does not support is checked (§6.2 step
  4). A pack whose profile this runtime does not read, the extended profile, is `not-checked`,
  reason `profile-unsupported`.
- Only `/outcomes/<i>/extensions/org.judgmentpack.annotations/severity` is read. The key on a rule,
  on the root or on a value-set entry is not.
- Values match the four levels by exact code point. Every list is refused, a one-element list
  included. Each refused outcome gets one `JPS-VOCABULARY-SEVERITY-VALUE` error at its pointer,
  naming the outcome's id and the value, written in printable ASCII.
- `JPS-VOCABULARY-SEVERITY-IDENTIFIER`, a warning at `/outcomes/<i>/id`, is reported exactly when
  the outcome's severity is a level, the id's last hyphen-separated segment (the whole id when it
  has no hyphen) is a level, and that level is not the outcome's.
- The report names `vocabulary` (`annotation-vocabulary`), `version` (`1.0.0`), `result` (`pass`,
  `refused` or `not-checked`), `reason`, `coreLayer`, `findings` and `advisories`.
- In `packs validate` it is the seventh named check. A refusal fails the check and the pack, so the
  run exits `1` as for any failed check; not applied, the check is `skipped` with the reason. The
  entry's `vocabulary` member carries the report, and the human output prints each finding and
  advisory. `--fail-on-advisory` counts the vocabulary's advice with the overlap advice.
- The four levels are written in the code; a test holds them equal to the Spec's
  `vocabulary.json`, vendored with its SHA-256 and provenance.
- `spec validate`, evaluation, `packs diff`, the filtered view and MCP are unchanged. MCP's validate
  tool calls the same Core validation as `spec validate`, which does not apply the vocabulary, and
  MCP has no tool for `packs validate` or the view.

### The view

- Each outcome's `Severity` is its level when its severity annotation is exactly a level, and empty
  otherwise: no severity stated, another case, a list, a look-alike letter. The value as written
  stays in the outcome's annotations.
- `SeverityOrder` lists the outcomes most severe first, one level in document order, and the
  outcomes without a level apart, in document order. Nothing is derived for a rule, the pack or a
  disposition.
- The JSON's `severity` member holds `mostSevereFirst`, `outcomes` (each `id`, and `severity` where
  it has a level, in document order), `unranked` and `vocabularyVersion`. The Markdown's outcomes
  table has a Severity column between Label and Description, followed by one line for the order
  and one for the outcomes without a level.
- A view of a pack under another `specVersion` holds what it held; only its version line changes.

### The Spec's rows

The rows, the packs and `vocabulary.json` are vendored under `internal/vocabulary/testdata` from
Spec release `v0.5.2` (revision `561b2a7245cf6ee13cb5be78da9c7a89f9adcdef`), each file with its
upstream SHA-256. Of the 32 rows, 31 run and each gives the result its row states; one,
`profile-extended`, is not run, because this runtime reads the base profile only. The inertness
pair runs: its three packs give identical dispositions on its five evaluations, and removing the
key as §2.6 says from each of the 25 row packs Core admits leaves the evaluation form unchanged.

### Consequences

- Good, because a misspelled, miscased or listed severity fails `packs validate`, with the outcome
  and the value named, where it was admitted without a word.
- Good, because a reviewer reads each outcome's level in one column, and the outcomes in order,
  and the view never supplies a level.
- Bad, because a project that uses another scale under the same key now fails `packs validate`,
  with no option to turn the check off; it can rename its key.
- Bad, because every view's bytes change: `viewVersion` moves to `"2"`, and a view of a Core
  `0.3.0` or `0.2.0-draft` pack changes in its version line alone.
- Bad, because the binary applies vocabulary `1.0.0` only; another version takes a runtime release.
- Revisit when the companion is accepted or changes its levels, when another vocabulary version is
  released, or when an independent reader runs the rows.

## More information

- The companion: `spec/annotation-vocabulary.md`, Spec release `v0.5.2`; issue #405.
- Code: `internal/vocabulary`, `internal/project/validate.go`, `sdk/vocabulary.go` and `sdk/view`.
- [ADR-0074](0074-describe-a-loaded-pack.md) (the view),
  [ADR-0077](0077-admit-core-0-5-0-and-retire-the-staged-selector.md) (Core `0.5.0`),
  [ADR-0081](0081-carry-rule-overlap-advice-in-packs-validate.md) (advice in `packs validate`) and
  [ADR-0086](0086-compare-two-pack-documents-member-by-member.md) (the diff).
