---
status: superseded by ADR-0077
date: 2026-11-11
deciders: maintainer
---

# Admit the staged draft selector `0.4.0-draft` experimentally: evaluate its evaluation form, and show and filter its annotations

## Context and problem statement

The specification stages a draft Core selector, `0.4.0-draft`, for Draft RFC 0031: annotations,
short labels on a pack, its outcomes, its rules and its value-set entries under the reserved
extension `org.judgmentpack.annotations`, and root `valueSets`, which a fact `in` condition names
with a sibling `valueSet` member instead of an inline list. No specification release carries it,
and the specification may rename or change it. Until this runtime admits some `specVersion` that
defines them, a pack that uses either is refused, so an author cannot try the RFC against a real
evaluator, a reviewer cannot read such a pack in the view of ADR-0074, and nobody can list the
rules that carry a given label (#376).

Material-decision impact: public-surface.

## Decision drivers

- `0.2.0-draft` and `0.3.0` behaviour, bytes and outputs, and the conformance claim in
  CONFORMANCE.md, stay as they are. The draft selector carries no claim, and every place that
  documents it says the specification may rename or change it.
- Provenance as strict as a released bundle's: the staged files come from one exact, clean
  specification commit, recorded in a lock that labels them as staged.
- One evaluator. Annotations never change a disposition or an error class, and a reference
  evaluates exactly as the inline list it names would.
- Identity and digest stay over the exact bytes of the pack as presented.
- The view stays faithful (ADR-0074): it shows the document as written, never an expanded form.
- A filtered view can never be read as the pack.

## Considered options

- Wait for a specification release that carries RFC 0031, and admit nothing before then.
- Admit the RFC behind a CLI opt-in, as the RFC 0008 and RFC 0016 prototypes are (ADR-0009,
  ADR-0039).
- Admit the staged selector by its own `specVersion`, base profile only, vendored from an exact
  commit and labelled staged, and evaluate a pack as its evaluation form.

For where the evaluation form is made: expand references at each site that reads a condition, or
compute it once at admission and keep the document beside it.

For a filter of several criteria: every criterion must match (all), any may match (any), or any
value of one key and every key.

For a filter on a pack under another selector: show it with nothing matched, or refuse it.

For `viewVersion`: move it to `"2"`, or keep `"1"`.

## Decision outcome

Chosen option: admit the staged selector by its own `specVersion`, because a pack declares
`0.4.0-draft` in its own bytes, so it cannot be confused with a `0.3.0` pack, needs no flag to be
read the same way by every surface, and is refused by a runtime that does not admit it; waiting
leaves authors with nothing to try, and a flag would let the same bytes mean two things.

- **Admission.** `tools/sync-spec-artifacts --staged 0.4.0-draft` imports the staged schema,
  document cases, evaluation rows and example from a full commit digest of a clean checkout, never
  a tag or a branch; the lock records the commit and `stage: staged-unreleased`. The selector is
  admitted with the `base` profile by validation, `spec test-conformance` and the evaluator, and by
  every surface that reaches the evaluator except graph evaluation: `evaluate-corpus`, the project
  walk, fact advice, matrix validation, coverage, the examples and the view. Graph evaluation, the
  dialect check and the policy check refuse it with `JPS-STAGED-DRAFT-UNSUPPORTED`; pack-design
  analysis, which evaluates only Core `0.3.0` inputs, and an MCP tool's standalone
  `spec_version`, which names a contract without a pack, keep their existing refusals. "Behaves as
  `0.3.0`" is one predicate, `result.HasCore030Rules`, so no site compares a version string by
  itself.
- **Evaluation form.** It is computed once, at admission, from the validated document: annotations
  removed, each reference replaced in place by the named set's entry values, `valueSets` dropped.
  An unknown set id is `JPS-SEMANTIC-UNRESOLVED-VALUE-SET` and `pack-not-conformant`. The evaluator
  reads only the evaluation form, so a trace of an expanded condition is the inline twin's;
  document limits apply to the pack as presented and work limits to the expanded form. The
  admission keeps the document beside it, and `Document` returns the document.
- **Corpus runs.** The staged document cases and evaluation rows run as `staged`, with no suite
  version, and are implementation experience, not corpus results. The row
  `annotations-supported-extension-set` is reported as held, with the specification's own reason:
  the released case schema cannot name the annotations extension in a supported-extension set.
- **View.** Value sets and annotations are typed fields of `view.PackView`; a reference is shown
  as the set's id, never expanded. An annotation is a string or a list of strings, and a
  one-element list stays a list. Its key and text are escaped like every pack string, and written
  in ASCII beside when they hold a code point outside printable ASCII, because a filter matches on
  them. Under any other selector a `valueSets` member is an unread member the view refuses, and an
  `org.judgmentpack.annotations` member stays an ordinary extension. `viewVersion` stays `"1"`:
  ADR-0074's rule moves it only when the bytes of a pack an earlier release could describe would
  change, and no such pack holds these members.
- **Filter.** `sdk.Pack.FilteredView` and `jpack packs describe --filter key=value` (repeatable)
  return the outcomes and rules whose annotations match every criterion: a key with that string,
  or with a list that holds it. All, because a reader narrowing a pack expects each label added to
  narrow further, and "any" can be had by running the filter once per value; keys and values
  compare by exact code point, with no case folding, Unicode normalization or trimming, because
  a looser comparison would match two labels that the canonical JSON tells apart. The filtered view carries `filteredViewVersion` and
  no `pack` or `viewVersion` member; it states the filter (criteria ordered by key and value, and
  the match rule `all`), how many outcomes and rules matched of how many, each match's JSON Pointer
  in the pack, and the pack's id, version, specVersion, stated profile and digest. Its Markdown
  opens by saying it is not the pack. It is taken from a view built afresh from the admitted
  document. A criterion no annotation could hold (the staged schema's key pattern and string
  rules), a repeated criterion, or none, is refused; so is a pack under any other selector, rather
  than shown with nothing matched, because such a pack cannot carry annotations and "0 of 3" would
  read as a statement about its labels. The CLI reports both as `JPS-INVOCATION-FILTER`.

### Consequences

- Good, because an author can write, validate, evaluate, review and filter a pack that uses RFC
  0031, against a bundle whose provenance is as checkable as a released one's.
- Good, because the evaluator never sees an annotation or a reference, so neither can change a
  disposition, and the inline-twin tests hold the expansion to the inline form.
- Good, because the view and the filter read the document, so a reviewer sees what the author
  wrote and the filtered view says what it left out.
- Bad, because the staged bundle can change under the runtime: a refresh is a re-import at a new
  commit, and the staged tests name every case a refresh changes.
- Bad, because the release gate accepts a staged bundle as it accepts a released one, so a runtime
  release can ship the draft selector; whether it should is a release decision, not this ADR's.
- Bad, because the criterion check repeats the staged schema's key and string rules in Go; a test
  holds the two equal on the cases it names.
- Revisit when the specification releases the version that carries RFC 0031, renames the
  selector, or changes the case schema that holds the held row; and when a reader needs "any" or
  value sets in a filter.

## More information

- [CONFORMANCE.md](../../CONFORMANCE.md), "Staged draft selector `0.4.0-draft` (EXPERIMENTAL)".
- [docs/releasing.md](../releasing.md), "Refresh a staged draft selector (EXPERIMENTAL)".
- [docs/building-with-packs.md](../building-with-packs.md), "Review".
- [ADR-0074](0074-describe-a-loaded-pack.md) (the view), [ADR-0073](0073-in-process-go-evaluation-api.md)
  (the Go API), [ADR-0063](0063-explicit-core-030-contracts.md) (explicit Core `0.3.0` contracts).
- Code: `internal/evalform` (the evaluation form), `internal/evaluation` (`evaluationForm`, called
  from the pack half of admission), `sdk/view/filter.go` (the filter).
