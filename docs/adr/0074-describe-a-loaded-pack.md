---
status: proposed
date: 2026-11-11
deciders: maintainer
---

# Describe a loaded pack, every member as written, through the Go API and `packs describe`

## Context and problem statement

A reviewer of a pack reads its JSON, and a host that shows a pack to a person writes its own
rendering of that JSON (#372). Each such rendering is a second reader of the document: it can drop a
member it does not know, reorder rules, normalize `1.50` to `1.5`, show a schema default as if the
author had stated it, or pass a newline, a pipe or a bidirectional control in a pack string through
into the page a reviewer reads. The Go API of ADR-0073 loads a pack, but says nothing about the
pack it loaded beyond what evaluation reports.

Material-decision impact: public-surface.

## Decision drivers

- Faithful: every member of the admitted document is carried, nothing is inferred or summarised,
  an absent member stays absent and a present empty one stays empty, arrays keep document order,
  and numbers and decimal strings stay exactly as written.
- Deterministic: the same pack bytes give the same view bytes, in each encoding, so a reader can
  compare them.
- Safe to render: no pack string can add structure to the Markdown a reviewer reads, and the rule
  that ensures it is stated once and tested class by class.
- One reader: the CLI writes the SDK's bytes, so a Go program and a shell see the same view.
- No new surface reaching the evaluator, no new dependency, and nothing outside the package's
  existing import closure.

## Considered options

- A typed view in package `sdk` — `Pack.Identity`, and `Pack.View` returning a `PackView` with
  canonical `JSON` and `Markdown` — read from the root the admission already decoded, with `jpack
  packs describe` writing its bytes.
- Re-encode the admitted document and leave typing to the caller.
- A CLI-only rendering in `internal/cli`.

## Decision outcome

Chosen option: the typed view in package `sdk`, because a typed view that refuses what it cannot
represent is the only one of the three that can be held complete against the schema, and putting it
in `sdk` gives the CLI and a Go program one source of the bytes. Re-encoding alone would type
nothing and would carry a member the view does not understand without anyone noticing; a CLI-only
rendering would leave every Go host to write its own.

**Identity.** `Load` reads the admitted document's `id`, `version`, `specVersion` and, when the
document states it, `profile`, and takes the digest `sha256:` plus the hex SHA-256 of the exact
bytes given, from the admission's private copy, the form the reviewed-set lock and the audit record
use. A trailing newline is another digest. The schema's default profile is not a statement the
document made, so it is never filled in.

**The view is the admitted document.** `View` reads the root the admission decoded, through the new
`AdmittedPack.Document` (the `PackFailure` step that also returns that root, added as a new file of
`internal/evaluation`, so the admission code is unchanged), and builds the typed view member by
member. Every member read is recorded, and a member the view does not read is refused, as is an op
outside the condition grammar, a present empty string where the view would take empty for absent,
and a value of the wrong type. A refusal there is a fault of this runtime, an error wrapping
`ErrInternal`, never a `*Refusal` and never a partial view: the admission accepted the document,
so the view, not the caller, is what failed. Values whose shape is the author's — an extensions
member, a condition's value — are carried as canonical JSON text. `View` never evaluates and
changes nothing.

**Canonical JSON, view version `1`.** `{"digest", "pack", "viewVersion"}` with no trailing newline,
written from the typed fields, not from the root, so a member the typed view lost is missing from
the bytes. Members are ordered by UTF-16 code units and strings carry RFC 8785's escapes. Numbers
depart from RFC 8785 deliberately: each is written as the literal token the document holds, because
RFC 8785 would write `1.50` as `1.5`, and the view's first promise is the document as written. A
test holds the writer to `internal/jcs` over the value space both write.

**Markdown, view version `1`.** A plain layout in the order a reviewer reads a pack, written by
this runtime and following CommonMark and the GitHub-flavoured table extension. Every pack string
passes through one rule: each ASCII punctuation character gets a backslash; each run of characters
`internal/display` replaces in terminal output becomes one code span naming the code points
(`` `U+202E` ``), so the view keeps them visible without letting them act; a space at either end
becomes `&#32;`; everything else is itself. `internal/display` exports the set it already used
(`Replaces`) so the two cannot disagree. Escaping every punctuation character, rather than the ones
that are significant where they stand, is chosen because it is provable without reasoning about
position: no pack character can begin any construct.

**Format versioning.** Both encodings carry `viewVersion`. It moves when the bytes for a pack an
earlier view version could describe would change; describing packs of a newly admitted version
does not move it (VERSIONING.md).

**The command.** `jpack packs describe <pack-or-> [--format markdown|json]` reads one local file
or standard input, within the hard byte limit, calls `sdk.Load` with no supported extensions and
`Pack.View`, and writes the Markdown as is, or the standard envelope whose `view` member is the
canonical JSON as a string, the precedent of the dialect report's `document`, so indentation of the
envelope cannot change those bytes. It reads no configuration. A pack the loader refuses is reported
as an operational error with the loader's code and message, not as an evaluation error, since
nothing was evaluated; an unimplemented profile is reported as every command reports it. The exit
code is the one `jpack experimental evaluate` gives the same refusal, derived from what the refusal
carries and held equal to the engine's over every refused conformance case.

`packs describe` loads and never evaluates, so it is not a surface that reaches the evaluator in
CONFORMANCE.md's sense and constructs no engine of its own; a test holds the CLI's one import of
`sdk` to loading and viewing. A pack that requires an extension is admitted by `Load` and described,
since describing it decides nothing.

**Excluded.** Explaining an evaluation, covering a test matrix and summarising across rules are not
the view: each says something the document does not, and each has or will have its own surface.
Annotations and value sets wait for the specification's decision on them (spec #301). JPS
`0.1.0-draft` documents, which `Load` refuses, are not viewed.

### Consequences

- Good, because a reviewer reads the whole pack in one rendering whose escaping is stated and
  tested, and a host can compare two views' bytes.
- Good, because the schema-derived completeness test makes a member a later schema adds fail until
  a fixture holds it and the view carries it.
- Bad, because the Markdown source is hard to read raw (`over\-limit`), and escaping does not stop a
  host that posts it from linking `@` mentions, `#` references or bare URLs in rendered text.
- Bad, because the canonical JSON writes bidirectional controls and line separators as themselves,
  as RFC 8785 does; a reader that shows the JSON raw needs the same care as with the pack itself.
- Bad, because the exported view type names (`Rule`, `Source`, `Metadata` and others) occupy
  package `sdk`'s namespace, and the exit code of a refusal is derived from its code rather than
  carried.
- Revisit when spec #301 settles annotations or value sets, when a new specification version adds
  members, or when a caller needs the view of a pack `Load` refuses.

## More information

- `sdk/view.go`, `sdk/view_json.go`, `sdk/view_markdown.go`; the goldens under
  `sdk/testdata/view/golden`, written only by `-update-view-goldens`.
- `internal/cli/packs_describe.go`; docs/building-with-packs.md "Review" states the format and the
  escaping rule.
- [ADR-0073](0073-in-process-go-evaluation-api.md), the Go API this extends.
