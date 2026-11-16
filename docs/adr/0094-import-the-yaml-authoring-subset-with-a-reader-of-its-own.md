---
status: accepted
date: 2026-11-16
deciders: maintainer
---

# Import the registry's YAML authoring subset with a reader of its own, refusing everything outside it

## Context and problem statement

RFC 0034 §7 (accepted) lets adopters write a registry or a kind declaration in a strict YAML
subset, which import converts to the canonical JSON that validation reads. The subset has these
rules:

- one document;
- no directives or tags;
- no anchors, aliases or merge keys;
- plain scalars only `true`, `false`, `null`, a decimal integer within ±(2^53−1), a decimal with
  no exponent, or a string, with any scalar YAML 1.1 would read differently refused;
- no duplicate keys, string keys only, no complex or multi-line keys;
- UTF-8 with no byte-order mark;
- deterministic output, and a refusal with line and column that writes nothing.

§7 names no diagnostic codes. A general YAML library accepts all of YAML, and its reading of
plain scalars is the problem the subset exists to avoid. ADR-0093's reader reads only JSON.

Material-decision impact: public-surface.

## Decision drivers

- Refuse everything outside the subset. Never coerce a construct into a value.
- Report every construct outside the subset that the reader can read past, not only the first.
- Locate a refusal by line and column. Never carry a value, and never a name a pointer could hold.
- Admission is the one ADR-0093 runs on JSON: size bound, depth, the number rules and the exact
  selector, run on the bytes import writes.
- Give the same bytes for the same file. Add no dependency where a small reader does.

## Considered options

- A reader of its own for the subset, with no new dependency.
- A YAML library (`go.yaml.in/yaml/v3` is already in `go.sum`, not in the build) used as a
  tokenizer, with the subset checks on its node tree.
- Accept full YAML and convert what it reads.

## Decision outcome

Chosen option: "a reader of its own". The subset is small, and a library's node tree has already
lost what the refusals need. It has resolved tags, merged `<<` keys and folded a BOM. It reports
some duplicates and syntax errors as the first error only. It locates some constructs where its
own grammar does, not where the file has them. Re-checking its output would still need a scalar
classifier and a line map of our own. Full YAML is what §7 rules out.

`internal/registry` gains `Import` (`yaml.go`, `yaml_parse.go`, `yaml_scalar.go`). `jpack registry
import FILE [--output OUT]` writes the canonical JSON plus one newline. A refusal writes a
value-free report and exits 1. `--output` is replaced, by a temporary file and a rename, only on
success. A remote path exits 3, and a read or write failure exits 4.

New codes, listed in `diag.go` before step 1: `REG-YAML-ENCODING`, `-DOCUMENTS`, `-DIRECTIVE`,
`-TAG`, `-ANCHOR` (an anchor or an alias), `-MERGE`, `-SCALAR`, `-EMPTY`, `-KEY`, `-SYNTAX` and
`-TRUNCATED`.
A YAML file also gets `REG-DOC-SIZE`, `-DEPTH`, `-DUPLICATE`, `-STRING`, `-NUMBER` and
`-SELECTOR`, with their JSON meaning. A location in a YAML file is `{"document":"yaml","line":L,
"column":C}`, counted from 1 in code points. `line` and `column` are omitted everywhere else, so
the `validate` report is unchanged.

Choices §7 leaves to the reader, recorded so the companion can settle them:

- **The report.** §6's location lists no line or column, but §7 locates a refusal by them. Import
  writes §6's report (`reportFormatVersion` `"1.0.0"`) with `line` and `column` in place of the
  pointer. Like the other `REG-*` codes, they are provisional until the companion's list is
  released (VERSIONING.md).

- **"YAML 1.1 reads differently."** The reader refuses a plain scalar that YAML 1.1's types, the
  YAML 1.2 core schema or go-yaml v3 read as something else:
  - booleans, including `y`, `n`, `yes`, `on` and their capitalized forms;
  - null, including `~`;
  - integers in binary, octal, hex, sexagesimal or with underscores, plus `+5` and `007`, with a
    radix prefix in either case;
  - floats, including exponents, `.inf` and `.nan`;
  - a number with an underscore anywhere, which go-yaml reads after dropping every underscore;
  - timestamps, with one or two digits in a date's month and day and a time's parts;
  - `<<` and `=`.

  A version such as `1.2.0` stays a string. Quoting gives a string. The table records how each
  reader reads a sample, checked against go-yaml v3.0.1 and PyYAML 6.0.1, both its pure loader and
  its libyaml loader. libyaml is a parser and resolves no scalar, so its column applies only to
  structure; its plain scalars are read by PyYAML's YAML 1.1 rules.

  | Sample | YAML 1.1 types | YAML 1.2 | go-yaml v3 | libyaml (PyYAML's C loader) | Import |
  |---|---|---|---|---|---|
  | `y`, `yes`, `on` | boolean | string | string | `yes`, `on` boolean, `y` string | `REG-YAML-SCALAR` |
  | `True`, `Null` | boolean, null | boolean, null | boolean, null | as YAML 1.1 | `REG-YAML-SCALAR` |
  | `0x1F`, `0b101`, `017` | integer | integer (`0x1F`, `017`) | integer | as YAML 1.1 | `REG-YAML-SCALAR` |
  | `0X1F`, `0O17`, `0B101` | string | string | integer | as YAML 1.1 | `REG-YAML-SCALAR` |
  | `1_000`, `1_` | integer | string | integer | as YAML 1.1 | `REG-YAML-SCALAR` |
  | `1_0e5`, `0o1_7` | string | string | number | as YAML 1.1 | `REG-YAML-SCALAR` |
  | `+5`, `.5`, `5.` | number | number | number | as YAML 1.1 | `REG-YAML-SCALAR` |
  | `08`, `1e3` | string | number | number | as YAML 1.1 | `REG-YAML-SCALAR` |
  | `2001-12-14` | timestamp | string | timestamp | as YAML 1.1 | `REG-YAML-SCALAR` |
  | `2001-1-2`, `2001-01-02T3:4:5Z` | string | string | timestamp | as YAML 1.1 | `REG-YAML-SCALAR` |
  | `1.2.0`, `_5`, `0x`, `1,000` | string | string | string | as YAML 1.1 | string |
  | `{?b: c}`, `[?b]` | n/a | plain `?b` | key `b` | key `b` | `REG-YAML-KEY` |
  | `[:b]`, `{a: :c}` | n/a | plain `:b` | error | error | `REG-YAML-KEY` |
  | `{A:[0]}` | n/a | error | error | error (pure loader: `{A: [0]}`) | `REG-YAML-SYNTAX` |
  | `A: \|` then spaces at the end of the file | n/a | `""` | `""` | `""` | `""` |

  The YAML 1.2 column is the core schema for a scalar and the grammar for structure.

- **Empty nodes are refused** (`REG-YAML-EMPTY`). YAML reads them as null, and §7 lists only
  `null`.
- **Numbers.** An integer is `-?(0|[1-9][0-9]*)` and a decimal adds `.[0-9]+`. Both are held to
  RFC 0034 §1's number rules, so `-0` and out-of-range integers give `REG-DOC-NUMBER`.
- **Escapes.** A quoted `😀` pair is combined, as JSON does. A lone surrogate gives
  `REG-DOC-STRING`.
- **Keys.** A plain key that reads as anything but a string gives `REG-YAML-KEY`, as does an
  explicit (`?`), flow or multi-line key, or one over YAML's 1024-character implicit-key bound.
  In a flow collection a scalar may not start with `?` or `:`, which libyaml reads as indicators
  (`REG-YAML-KEY`), and only a quoted key takes its value right after the `:` (`{A:[0]}` is
  `REG-YAML-SYNTAX`).
- **Which document.** The selector member decides: `registryFormatVersion` means a registry and
  `kindFormatVersion` means a kind. Having neither or both gives `REG-DOC-SELECTOR`. Import does
  not guess.
- **Size.** Any YAML file is bounded at 8 MiB, the registry's bound. The JSON written is then held
  to its own document's bound (256 KiB for a kind), as admission does.
- **Order.** A registry's kinds and entries are written in §3's canonical order when every item
  has those members as strings. Otherwise the list is kept as written, for validation to report.
- **Fatal and recoverable refusals.** A tag, anchor, alias, merge key, ambiguous scalar, empty node,
  bad key, duplicate or out-of-range number is recorded, and reading goes on. Encoding, document
  structure, depth and syntax errors stop the reading.
- **A bounded report.** Reading also stops at the 1001st refusal, which is reported as one
  `REG-YAML-TRUNCATED` where reading stopped, after the first 1000. A file within the size bound
  could otherwise hold millions of refusals (one per alias in `[*a,*a,…]`), and the report would
  outgrow the file. 1000 is far more than a hand-written file needs fixed at once. `validate`'s
  report is not capped: what it reports is bounded by the documents' members and checks. (Since
  amended: `validate` keeps the first 1000 in its report's order and runs every check, so its bound
  differs from this one; see ADR-0093's amendment.)
- **Numbers past the bounds.** RFC 0034 §1's number rule is applied by a scan before any big
  arithmetic, which is quadratic in a literal's digits: an integer of more than 16 digits, a
  decimal of more than 17 significant digits, or one whose leading digit lies outside 10^-324 to
  10^308, cannot be admitted and is refused at once. What passes is compared exactly. This holds
  for `validate`'s JSON too, with the same results.
- **Overlay files** (§ Alternatives, a MAY) are not implemented.

### Consequences

- Good, because nothing outside the subset reaches a value. The refusal set is in one file with
  its tests, and the binary gains no dependency.
- Good, because what import writes goes through the same admission and validation as hand-written
  JSON.
- Bad, because the runtime maintains a YAML reader (about 1,200 lines). Its grammar is held by
  tests, not by a reference implementation.
- Revisit when the companion text names codes or a scalar table for §7, or when its vectors are
  released: map these codes and choices to it.

## More information

Spec RFC 0034 §1, §3, §6 and §7. ADR-0093 is the registry reader whose admission import reuses.

## Amendment: NEL, LS and PS (2026-11-16, #432)

YAML 1.2 reads U+0085, U+2028 and U+2029 as content. YAML 1.1 and go-yaml v3 read them as line
breaks. So `description: allowed` followed by U+2028 and `  denied` is one scalar to one reader
and two lines to another. Import refuses a file that holds any of the three, as
`REG-YAML-ENCODING` at the character, the code for a character YAML does not allow. A
double-quoted scalar can still write each one as an escape (`\N`, `\L`, `\P`), which every reader
reads one way.
