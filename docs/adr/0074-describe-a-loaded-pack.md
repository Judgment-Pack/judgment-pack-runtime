---
status: accepted
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
- Safe to render: no Markdown syntax in a pack string can add structure to the Markdown a reviewer
  reads, and the rule that ensures it is stated once and tested class by class.
- One reader: the CLI writes the SDK's bytes, so a Go program and a shell see the same view.
- No new surface reaching the evaluator, no new dependency, and nothing outside the package's
  existing import closure.

## Considered options

- A typed view — `sdk.Pack.Identity`, and `sdk.Pack.View` returning a `view.PackView` of package
  `sdk/view` with canonical `JSON` and `Markdown` — read from the root the admission already
  decoded, with `jpack packs describe` writing its bytes.
- Re-encode the admitted document and leave typing to the caller.
- A CLI-only rendering in `internal/cli`.

## Decision outcome

Chosen option: the typed view, because a typed view that refuses what it cannot
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

A string the evaluator matches on — a fact's path, the strings and member names of a fact's value,
an extension's name and the strings and member names of its value — is also written, when it holds
any code point outside U+0020 to U+007E, as its canonical JSON in ASCII in a code span beside the
escaped text: each such code point as `\uXXXX` (a surrogate pair beyond U+FFFF), the backtick as
`\u0060`. Two matched strings that render alike, such as `/amount` and `/amount` followed by a
Hangul filler, then read differently; pure-ASCII strings are written as before. Free text is not
marked, so look-alikes and NFC/NFD differences in a title, description, label, rationale or citation
are not detected. The canonical JSON is unchanged and writes such characters as themselves; a tool
comparing matched strings compares the JSON.

**Format versioning.** Both encodings carry `viewVersion`. It moves when the bytes for a pack an
earlier view version could describe would change; describing packs of a newly admitted version
does not move it (VERSIONING.md). The set of characters written as code points follows the Go
toolchain's Unicode tables through `internal/display`; a table update alone does not move it.

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
- Bad, because the Markdown source is hard to read raw (`over\-limit`), and escaping does not stop
  every link: a GitHub-flavoured renderer still links an email address in pack text, and a forge
  that shows the Markdown links mentions, references and commit ids in it. A caller posting the
  view on a forge attaches it as a file or puts it in a fenced code block longer than any backtick
  run in it.
- Bad, because the canonical JSON writes bidirectional controls and line separators as themselves,
  as RFC 8785 does; a reader that shows the JSON raw needs the same care as with the pack itself.
- Neutral, because the view's types live in their own package, `sdk/view`, so their generic names
  (`Rule`, `Source`, `Metadata` and others) do not occupy package `sdk`'s namespace, and an
  escalation's target is the view's own `view.Target`, not the handoff's `sdk.Target`. Only
  `sdk.Pack.View` builds a `PackView`, through an internal package of `sdk`, so no caller can make
  one whose cached bytes disagree with its fields. Bad, because a caller now imports two packages
  to name the view's types.
- Bad, because the exit code of a refusal is derived from its code rather than carried.
- Good, because what holds the view to its format is written by hand or derived from the input
  document, never recorded from a run: the pack fixtures under `sdk/view/testdata/packs`; the
  Markdown and canonical JSON under `sdk/view/testdata/expected`, written by hand for one
  every-member pack of each admitted specification version and for the adversarial-strings
  fixture; the format and escaping tests; the schema walk, which holds the canonical JSON of every
  input to every member of its document; and a Markdown completeness check derived from each input
  document, which finds every string and number of each top-level member in that member's section
  in escaped form, every array in document order, and the label of every member the schema walk
  finds present. Generated goldens over every input were considered and not kept, because the
  repository's fixtures are written by hand and never recorded from a run of the code under test.
- Bad, because a byte change that only a pack outside the hand-written expectations shows, and
  that drops or reorders nothing, is caught only by the format tests written by hand.
- Revisit when spec #301 settles annotations or value sets, when a new specification version adds
  members, or when a caller needs the view of a pack `Load` refuses.

## More information

- `sdk/view.go` (`Pack.View`, `Pack.Identity`), `sdk/view/view.go`, `sdk/view/json.go`,
  `sdk/view/markdown.go`; the hand-written expectations under `sdk/view/testdata/expected`.
- `internal/cli/packs_describe.go`; docs/building-with-packs.md "Review" states the format and the
  escaping rule.
- [ADR-0073](0073-in-process-go-evaluation-api.md), the Go API this extends.
