---
status: proposed
date: 2026-11-16
deciders: maintainer
---

# A Go typed loader emitter, as a standalone program under the emitter contract

## Context and problem statement

Spec RFC 0036 §6, item 1 (registry companion §11.6) calls for a reference emitter that turns the
kinds a service reads into a Go typed loader: a Go type per kind, generated from its schema, and a
loader that reads one environment's resolved registry through the start-up module (ADR-0096) and
decodes each entry of the kind into the type. A member at one of the kind's secret positions (RFC
0035, companion §3.5 and §6.3) is the start-up module's `Secret`, which only a `Reader` fills.

The emitter runs under RFC 0036 with `"input": "resolved"`. Since ADR-0101, the request carries
each read kind's declaration, and the SHA-256 of its RFC 8785 form is its pin's digest (companion
§11.2). The emitter must refuse a missing or mismatched declaration.

Several things are open without this record: which schema shapes have a Go form, how a value is
decoded, when a secret is read, what the generated names are, what the program refuses, and how
generated code that imports the start-up module is compiled in this repository, whose root module
does not require it.

Material-decision impact: public-surface, security.

## Decision drivers

- The emitter is a pure function of the request bytes. It reads no clock, environment variable,
  file or network, and no map order reaches its output.
- The generated code imports the Go standard library and the start-up module only.
- No declaration text can alter the generated source. An id, a member name or a value that ends a
  comment or a string literal must not change the program. This is the source-injection boundary.
- A declaration the emitter cannot render faithfully is refused, never rendered approximately. The
  same holds for names it cannot form, or that would collide.
- A value is decoded as the kind's schema means it, not as `encoding/json` reads it: names match
  exactly, an integer is any number of integer value, `required` holds, and an enumeration is
  closed.
- A secret is read only when the program asks for the entry that refers to it.

## Considered options

- A standalone program, `cmd/jpack-emit-go-loader`, that imports only the standard library and
  `internal/jcs`, with a generated decoder.
- Generated types with `encoding/json` struct tags. Rejected: `encoding/json` matches a member name
  regardless of case, so `{"Password": …}` would fill a field the schema names `password`. It also
  refuses `120.0` for an `int64`, which JSON Schema's `integer` admits, ignores `required`, and
  cannot fill a `startup.Secret`, whose `UnmarshalJSON` refuses on purpose.
- Declarations passed in the emitter's options. Rejected: ADR-0101 carries them in the request, bound
  to the pins by digest, so an options copy would only duplicate them under a smaller bound.

## Decision outcome

Chosen option: the standalone program with a generated decoder, because it is the emitter
contract's own shape, and the decoder is the only way to hold a value to its schema's meaning
without a dependency outside the standard library.

**The program.**
- `emit(request) (response, error)` holds the logic, and `main` only wires standard input and
  output to it. The JSON is read strictly, as `cmd/jpack-emit-go-facts` reads it (ADR-0100), with a
  depth of at most 34: a declaration or a resolved document nests 32 deep, two below the root.
- The request must be `emitRequestVersion` `1.0.0` with exactly `emitter`, `options`,
  `declarations` and `resolved`.
- It refuses in one of two ways. A request it does not read writes one fixed line to standard
  error, writes nothing to standard output, and exits 1, which the host reports as `EMIT-PROCESS`:
  the request's form, its options, a declaration that is not of format `1.0.0`, and declarations
  that do not match the pins. A declaration it reads but cannot generate code for writes a
  response with no file and one error diagnostic, located at the kind
  (`{"document": "kind", "kind": K, "pointer": ""}`, which the host resolves because K is pinned):
  `x-go-loader-schema` for a shape it does not generate, `x-go-loader-secret` for a secret pattern
  it cannot type, `x-go-loader-name` for a name it cannot form, and `x-go-loader-collide` for two
  names that collide. Neither quotes the request.
- Files are sorted by path and held to companion §11.3's bounds and text rules, and each path
  segment to 255 bytes, as ADR-0100 holds them.

**The manifest.** `"environments": "all"`. The files depend on the declarations alone, so under
`"each"` every environment's run would write the same paths, which the host refuses (ADR-0098). The
generated loader takes its environment at run time.

**Declarations and pins** (companion §11.2). Before any schema is read:
- each declaration is held to companion §3.1's members and forms, and its digest is taken here, as
  the SHA-256 of its RFC 8785 form by `internal/jcs`, never read from the request;
- the declarations are sorted by kind, each kind once;
- every resolved document of the request has exactly the declarations' kinds as its pins, one to
  one, each pin with its declaration's id, version and digest. A pin with no declaration, a pin of
  another digest, a declaration no pin names, and declarations with no resolved document at all
  are each refused.

**Options.** `package` and `directory`, with ADR-0100's rules: no keyword, `main` or
`documentation` package, and no directory segment that begins with `_` or `.`, that is `testdata`
or `vendor` in any case, or that is over 255 bytes. `directory` defaults to `generated/loader`. The
manifest's schema and the program hold the same patterns, and a test compiles the manifest's under
the host's pattern subset and compares them on the edges.

**Files.** `<directory>/loader.go` holds what every request shares: `Registry`, `Load`, `LoadFile`,
the four errors and the decoder, and the table of the request's kinds. Each kind gets
`<directory>/<stem>_kind.go`, so no file name ends in a GOOS or GOARCH suffix or in `_test`. Every
file starts with one header line naming the emitter's id and version. A kind's type's doc comment
names its kind, id, version and digest, each quoted. The registry's digest is in no file, so an
edit to an entry does not change them. Every file is formatted with `go/format`.

**Types.**

| Schema position | Go |
|---|---|
| object, `additionalProperties: false` | a struct; an optional member is a pointer, nil when absent |
| object, `additionalProperties` a typed schema, no `properties` | `map[string]V` |
| array, `items` a schema (or absent, or `true`) | `[]E` (`[]json.RawMessage` for any element) |
| string with `enum` or `const` of strings (with or without `type: string`) | `type T string` and a constant per value |
| string, integer, number, boolean | `string`, `int64`, `float64`, `bool` |
| `true`, or a schema with no shape keyword | `json.RawMessage`, kept as written |
| a string at a secret position | `startup.Secret` |
| `$ref: #/$defs/<name>` | the definition, expanded at each use |

- An optional slice, map or raw member is nil when absent, without a pointer.
- `default` is never applied. Validation-only keywords and annotations change nothing.

**The decoder.** Each struct and enumeration has a generated `decode<T>`:
- a member is found by its exact name; one the type does not have is ignored, because a minor
  version may add it (companion §3.6);
- a required member that is absent is refused;
- an integer is a JSON number of integer value within `int64` in any spelling: `120`, `120.0`,
  `1.2e2` and `12000e-2` are each 120, and `1.5` or a value past `int64` is refused;
- an enumeration's value outside its list is refused;
- a value at a secret position must be a string that begins with `secret:`.

A refusal wraps `ErrValue` and names the kind, the entry's id and the JSON pointer, never the value.

**The loader.**
- `Load(data, environment, reader)` and `LoadFile(path, environment, reader)` read the document
  through `startup.ReadResolved` or `ReadResolvedFile`. They require a pin of each kind the
  package was generated for, with its id and the same major version (`ErrKind`): a minor version
  only widens (companion §3.6). They decode every entry of those kinds, and read no secret. Kinds
  the package does not read are not looked at.
- `(*Registry).<T>IDs()` lists a kind's entries, and `(*Registry).<T>(ctx, id)` decodes one. It
  reads each secret the entry refers to through the reader, at the entry's decode and not before,
  so a service loads a registry that refers to other services' secrets without bindings for them.
  A missing entry is `ErrNoEntry`. A reader error, a nil reader and a zero `Secret` are each
  `ErrSecret`, and a reader's error is wrapped as well.

**The naming rule.** ADR-0100's, applied to a schema:
- A word is a maximal run of ASCII letters and digits. A text's part is its words, each with the
  first letter upper-cased, run together.
- A kind's type is the part of its name. A struct's or an enumeration's type below it is its
  parent's type and the part of the member name, or `Item` for an array's or a map's element.
- A field is the part of its member name, with `X` before a leading digit.
- A constant is its type and the part of the value.
- A kind's Registry methods are its type and its type followed by `IDs`.
- The file stem is the kind name's words in lower case, joined by `_`.
- An empty part is refused. These collisions are refused, never renamed: two package-level names
  (types and constants across every kind, and the names `loader.go` exports); two fields of one
  struct; two Registry methods (kind `x-i-ds` against kind `x`'s `XIDs`); and two kinds' files.
- Every name formed is then held to the identifier check: an exported Go identifier of ASCII
  letters and digits. These are the only names from a declaration the source holds outside a string
  literal. Every other text from a declaration (a kind name, an id, a version, a digest, a member
  name, a value, a pointer) reaches the source only through `strconv.QuoteToASCII`, so the files are
  ASCII. A schema's descriptions are never copied.

**Refused shapes** (`x-go-loader-schema`):
- `anyOf`, `allOf` and `not`, a type array, and the type `null`;
- an open object: `additionalProperties` absent, `true` or `{}`, or `properties` beside an
  `additionalProperties` schema;
- a root that is not an object closed with `additionalProperties: false`;
- `$ref` beside a keyword that shapes the type, a `$ref` of another form, to an undefined name or in
  a cycle, and `$defs` below the root;
- `properties`, `additionalProperties`, `items` or `required` without a type, a `required` name the
  closed object does not declare, `required` at a map, a `type` of the empty string, a tuple
  `items`, and `false` as a subschema;
- `enum` or `const` at an integer, number or boolean position, which the decoder would not check;
- `enum` or `const` at a string position whose values are not all strings, and `enum` beside
  `const` there;
- a keyword outside companion §3.2's subset, and more than 4 096 positions with `$ref` expanded.

**Refused secret patterns** (`x-go-loader-secret`): one that matches a position that is not a string
(an enumeration at a secret position becomes a plain secret), one that reaches a raw position, one
that names a single member of a map or a single element of an array (their elements share a type),
and one that matches nothing.

**The golden module.** The vectors' expected files and the golden package import the start-up
module, which the root module does not require. They live in a nested module,
`cmd/jpack-emit-go-loader/golden/`, whose `go.mod` requires the start-up module through a local
`replace`, so it needs no `go.sum` and the root `go.mod` is unchanged. Its `vectors/` are in RFC
0036 §5's layout. `dbroleloader/` is a copy of one vector's files, which the emitter's tests hold
byte for byte to the emitter's output, with a decode test against the start-up module. CI vets and
tests the module in the `test` job on every platform, and `dev.json` runs the same two checks.

### Consequences

- Good, because a service reads its registry through types the compiler checks, with each secret
  reference typed as a secret and read only through a `Reader`.
- Good, because a value that does not fit its kind's schema is refused at `Load`, before the
  service uses any entry.
- Good, because a newer minor version of a kind loads without regenerating the code, and a new
  major version is refused.
- Bad, because kinds with open objects, `anyOf` or `not` get no loader. Supporting them needs a Go
  form for any value at such a position, which this version does not have.
- Bad, because a newer minor version that widens an enumeration refuses the new value at `Load`
  until the code is regenerated.
- Bad, because the vectors give their requests whole, so the harness's checks 4 and 5 (kind
  filtering and references) skip them. A vector of the registry form would run them.
- Revisit when the TypeScript emitter of RFC 0036 §6, item 2 is built, so that the three reference
  emitters can share one naming rule and one decoder of numbers.

## More information

- Spec RFC 0036 §1–§3, §5 and §6, RFC 0035, and registry companion §3, §6.3, §8 and §11.
- ADR-0096 (the start-up module), ADR-0098 (the emitter host), ADR-0099 (the conformance harness),
  ADR-0100 (the Go typed facts builder emitter) and ADR-0101 (declarations in the request).
