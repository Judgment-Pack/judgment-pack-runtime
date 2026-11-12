---
status: accepted
date: 2026-11-12
deciders: maintainer
---

# Show every annotation character outside printable ASCII recoverably: exact in the JSON, in ASCII in the Markdown and the messages

## Context and problem statement

The staged draft selector `0.4.0-draft`, as revised (#379), makes a display rule of RFC 0031
normative (D2, the draft's §9.2): wherever a consumer lists distinct annotation keys or values, or
states a filter, it MUST show every character outside printable ASCII (U+0020 to U+007E) in a key
or value in a form from which the reader can recover each such code point. ADR-0075 wrote
annotation text in ASCII beside itself in the Markdown, but said nothing of the other places this
runtime lists or states them: the JSON of the view and the filtered view, which `jpack packs
describe` carries in its envelope, and the messages that echo a criterion or a refused key.

Material-decision impact: public-surface.

## Decision drivers

- Meet the MUST wherever this runtime lists annotation keys or values or states a filter.
- No byte of a view or a filtered view changes, so `viewVersion` and `filteredViewVersion` keep
  their meaning (VERSIONING.md), and output under the released selectors is unchanged.
- The view's JSON stays canonical (ADR-0074): RFC 8785 writes every code point as itself except
  the controls, the quotation mark and the backslash.
- The smallest change that meets the rule.

## Considered options

- Write every code point outside printable ASCII as a `\u` escape in the view's JSON.
- Add a member beside each annotation key and string, and each criterion, giving its ASCII form.
- Keep the JSON as exact data and the Markdown as ADR-0075 made it, and quote every key and
  value a message echoes in ASCII or by its `U+` number.

## Decision outcome

Chosen option: the third, because each surface then either carries the exact code points as data
or shows them in printable ASCII, and only one message changes. The first and the second both
change the bytes of views that v0.43.0 wrote for staged packs, which would move both versions to
`"2"`; the first is also not canonical JSON, and changes the view of every `0.3.0` pack that holds
a character outside ASCII.

- **JSON** (`PackView.JSON`, `FilteredView.JSON`, and the `packs describe` envelope that carries
  them as strings). Each key and string is its exact code points in UTF-8, which a JSON reader
  recovers exactly. The JSON is data for a program, not a display: a consumer that displays a key
  or value read from it lists it, and D1 and D2 bind that consumer. This is this runtime's reading: the staged text
  speaks of a consumer that lists values or states a filter and does not say whether a machine
  interchange form is such a statement. The reading has been raised with the specification.
- **Markdown** (both views, which `packs describe` writes unchanged). Unchanged. Annotation text is
  escaped, a control, format character or separator written as its `U+` number in place
  (ADR-0074), and the text is written again in ASCII beside it whenever it holds a code point
  outside printable ASCII. That form holds only printable ASCII and decodes, as a JSON string, to
  the exact text. The filter's statement writes each criterion the same way, so a criterion is
  shown as the list it was picked from shows it.
- **Messages.** The filter's refusals already quote a malformed criterion or a refused key with
  `strconv.QuoteToASCII` and name a refused character by its `U+` number; they print a key as
  itself only after it has passed the key grammar, which admits only printable ASCII. The staged
  validator's message for a refused annotation key (`JPS-STRUCTURE-LOCAL-ID`) quoted the key with
  Go's `%q`, which leaves a printable letter outside ASCII, such as a Cyrillic look-alike, as
  itself; it now quotes it with `strconv.QuoteToASCII`. The diagnostic's `instancePath` stays the
  key itself, as every JSON Pointer this runtime reports is.
- **Versions.** Neither `viewVersion` nor `filteredViewVersion` moves: no byte of either view
  changes. The changed message is the prose of a diagnostic reported only under the staged
  selector; its code, pointer and members are unchanged, and no documented contract fixes the
  prose, so `outputVersion` does not move either.

### Consequences

- Good, because two keys or values that look alike never read alike in the Markdown or in a
  message, and the JSON never needs a second spelling of the same string.
- Good, because no reader of a view sees a byte change, and the released selectors' output is
  untouched.
- Bad, because a program that shows annotations from the JSON must apply D2 itself; the JSON
  cannot do it for it.
- Bad, because the diagnostic's `instancePath` shows a refused key as itself; its message beside
  it names the key in ASCII.
- Revisit when a surface that lists annotations is added (an MCP tool, a summary of keys), or the
  specification releases the version that carries RFC 0031.

## More information

- [ADR-0075](0075-admit-the-staged-draft-selector-0-4-0-draft.md) (the filter and the view's
  annotations), [ADR-0074](0074-describe-a-loaded-pack.md) (the view and its escaping).
- [VERSIONING.md](../../VERSIONING.md), the view and the filtered view.
- [docs/building-with-packs.md](../building-with-packs.md), "Review".
- Code: `sdk/view/markdown.go` (`annotationText`, `asciiForm`), `sdk/view/filter.go` (the
  criterion check), `internal/validation/staged_structural.go` (the refused-key message).
