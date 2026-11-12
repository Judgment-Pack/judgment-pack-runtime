---
status: proposed
date: 2026-11-12
deciders: maintainer
---

# Keep annotation text from reordering its line, and keep a run of spaces visible, in the Markdown views

## Context and problem statement

Core 0.5.0 §9.2 (D1) says that a consumer that shows an annotation key or string inline with other
text MUST isolate it, so that the direction of its characters does not reorder the text around
it, and SHOULD isolate the other display data the same way. D2 says that wherever a consumer lists
distinct annotation keys or values or states a filter, every character outside printable ASCII
(U+0020 to U+007E) must be recoverable.

The Markdown of the pack view and of the filtered view (ADR-0074, ADR-0075, ADR-0076) writes an
annotation key or string escaped, followed by its ASCII form when it holds a code point outside
printable ASCII. That meets D2, but not D1: a value holding right-to-left letters is written as
itself, and on a line such as `- label: <value> (ASCII ...)` it can reorder the colon, the
parenthesis and the ASCII form beside it (issue #380). A second gap was found beside it: two values
that differ only in a run of interior spaces, such as `in take` and `in  take`, are both printable
ASCII, get no ASCII form, and render alike, because an HTML renderer collapses the run.

Isolation cannot be added with isolate characters: the Markdown writes every format character as
its `U+` number (ADR-0074), so a raw U+2068 or U+2069 would break the view's own rule.

Material-decision impact: public-surface.

## Decision drivers

- Meet D1 for annotation keys and strings, and the filter's statement, in common Markdown
  renderers and in a plain terminal reading the Markdown source.
- Two values a filter tells apart never render alike.
- No raw control, format or invisible character in the Markdown; the JSON stays canonical and
  unchanged.
- No byte of a view of a `0.3.0`, `0.2.0-draft` or `0.1.0-draft` pack changes.
- The smallest change that meets the rule.

## Considered options

- (a) Wrap each annotation key and string in `<bdi>` in the Markdown.
- (b) Write a value that holds a direction-affecting character only in its ASCII form, and give a
  value that holds two adjacent spaces its ASCII form with each space of the run escaped.
- (c) Put each such value on a line of its own.
- (d) State that the Markdown is a source format whose renderer is the consumer D1 binds.

## Decision outcome

Chosen option: (b), because it is the only one that holds in every renderer and in a terminal: a
line that holds no right-to-left or Arabic-number character cannot be reordered by one, and a code
point written as `\u0020` cannot be collapsed.

- (a) depends on the renderer: CommonMark passes `<bdi>` through as raw HTML, but renderers that
  strip or escape raw HTML show nothing or the tag as text, and a terminal shows the tag and does
  not isolate. It also writes renderer HTML into a format whose every other character is text.
- (c) does not isolate in CommonMark: consecutive lines of one list item join into one paragraph,
  so a value is on a line of its own only if it becomes a list item of its own, which changes the
  layout of every annotation; a terminal that applies the bidirectional algorithm per line still
  reorders the list marker and indentation beside the value.
- (d) leaves the gap where it is: a reader of the Markdown source in a terminal is shown the line
  as written.

**The rule (`annotationText` in `sdk/view/markdown.go`).** For each annotation key and string the
view lists, and each criterion key and value the filtered view states:

- If the string holds a character whose bidirectional class (UAX #9) is R, AL or AN, it is written
  only in ASCII: `` (ASCII only `"..."`) ``, the canonical JSON string with every code point outside
  printable ASCII as a `\u` escape and the backtick as `\u0060`. These are the classes that resolve
  to a right-to-left level, or move the neutral characters beside them there, in a left-to-right
  line. No other class an annotation string or a criterion may hold does: every explicit format
  character (the marks, embeddings, overrides and isolates) is refused there (§9.2), and the
  remaining weak and neutral classes resolve to the line's direction.
- Otherwise, if it holds a code point outside printable ASCII or two adjacent spaces, it is
  written escaped, then `` (ASCII `"..."`) ``, where every space next to another space is also
  written `\u0020`.
- Otherwise it is written escaped only, as before.

The ASCII form still decodes, as a JSON string, to the exact text, so D2 holds as before. The
classes come from `golang.org/x/text/unicode/bidi` (Unicode 15.0.0 tables, the version of the Go
toolchain's own tables). That package is part of `golang.org/x/text` v0.14.0, which the schema
validator already brings into the module graph and the SDK's build, so no module or `go.sum` line
is added; `go.mod` lists it as a direct requirement because the view now imports it. The SDK's
import-closure test lists the package beside the validator's. A hand-written table of the R, AL
and AN ranges was the alternative; it would repeat about a hundred ranges that the module graph
already carries, with no source to keep it current.

**Versions.** Neither `viewVersion` nor `filteredViewVersion` moves. VERSIONING.md moves
`viewVersion` when the bytes for a pack an earlier view version could describe would change.
Annotations, and so the filtered view, exist only under Core 0.5.0, which no earlier release
could describe; the views of every other admitted version are byte-identical. v0.43.0 described
packs under the staged selector `0.4.0-draft`, which this release refuses (ADR-0077), so no view
of such a pack is written with different bytes: it is not written at all.

### Consequences

- Good, because no annotation key, string or criterion can reorder the text of its line, in any
  renderer or terminal, and two values a filter tells apart never render alike.
- Good, because the views of released selectors other than 0.5.0 are unchanged, byte for byte.
- Bad, because a reader of a right-to-left value sees only escapes in the Markdown; the canonical
  JSON carries the text, and a consumer that renders it (with `<bdi>` in HTML, for example) can
  show it isolated.
- Bad, because the class table follows `golang.org/x/text`; a table update can move a code point
  into or out of R, AL or AN, as an update of the display tables can (VERSIONING.md), and does
  not move `viewVersion`.

### What it does not cover

- Other display data, which D1 says SHOULD be isolated: titles, descriptions, labels, rationales,
  citations, value-set and entry descriptions, value-set entry values and fact values are written
  as before, so a right-to-left description can still reorder its own line (the label before it on
  that line, never another line).
- Interior space runs outside annotations: a value-set entry value or a fact value differing only
  in a run of spaces still renders alike in an HTML renderer. A fact value's ASCII form is shared
  with `0.3.0` views, whose bytes this decision keeps.
- Look-alikes within printable ASCII (`l` and `1`, `O` and `0`) are a font matter and are not
  marked.
- The messages of the filter's refusals (ADR-0076) quote a criterion in ASCII already; a terminal
  shows a run of spaces in them as it is.
- Revisit when the specification states a display form for a Markdown consumer, or a surface that
  renders the views as HTML is added.
