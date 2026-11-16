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
`-TAG`, `-ANCHOR` (an anchor or an alias), `-MERGE`, `-SCALAR`, `-EMPTY`, `-KEY` and `-SYNTAX`.
A YAML file also gets `REG-DOC-SIZE`, `-DEPTH`, `-DUPLICATE`, `-STRING`, `-NUMBER` and
`-SELECTOR`, with their JSON meaning. A location in a YAML file is `{"document":"yaml","line":L,
"column":C}`, counted from 1 in code points. `line` and `column` are omitted everywhere else, so
the `validate` report is unchanged.

Choices §7 leaves to the reader, recorded so the companion can settle them:

- **The report.** §6's location lists no line or column, but §7 locates a refusal by them. Import
  writes §6's report (`reportFormatVersion` `"1.0.0"`) with `line` and `column` in place of the
  pointer. Like the other `REG-*` codes, they are provisional until the companion's list is
  released (VERSIONING.md).

- **"YAML 1.1 reads differently."** The reader refuses a plain scalar that YAML 1.1's types or the
  YAML 1.2 core schema read as something else:
  - booleans, including `y`, `n`, `yes`, `on` and their capitalized forms;
  - null, including `~`;
  - integers in binary, octal, hex, sexagesimal or with underscores, plus `+5` and `007`;
  - floats, including exponents, `.inf` and `.nan`;
  - timestamps, `<<` and `=`.

  A version such as `1.2.0` stays a string. Quoting gives a string.
- **Empty nodes are refused** (`REG-YAML-EMPTY`). YAML reads them as null, and §7 lists only
  `null`.
- **Numbers.** An integer is `-?(0|[1-9][0-9]*)` and a decimal adds `.[0-9]+`. Both are held to
  RFC 0034 §1's number rules, so `-0` and out-of-range integers give `REG-DOC-NUMBER`.
- **Escapes.** A quoted `😀` pair is combined, as JSON does. A lone surrogate gives
  `REG-DOC-STRING`.
- **Keys.** A plain key that reads as anything but a string gives `REG-YAML-KEY`, as does an
  explicit (`?`), flow or multi-line key, or one over YAML's 1024-character implicit-key bound.
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
