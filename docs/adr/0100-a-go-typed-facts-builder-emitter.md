---
status: proposed
date: 2026-11-16
deciders: maintainer
---

# A Go typed facts builder emitter, as a standalone program under the emitter contract

## Context and problem statement

Spec RFC 0037 §6 (registry companion §10.6) calls for a reference emitter that turns facts
contracts into typed facts builders. In Go, that means a struct per object position, a constructor
whose arguments are the members the contract always holds, typed constants for `values` with an
unexported check, a decimal type that cannot be a `float64`, and a `Build` that returns the
canonical facts JSON. The emitter runs under RFC 0036 (companion §11). Its manifest has
`"input": "factsContracts"`, and it reads one request on standard input and writes one response on
standard output.

Several things are open without this record. The RFC leaves out the generated code's names, how
optional members look in Go, which contract positions have a Go form, and what the program
refuses. Nothing yet holds the emitter's output to vectors.

## Decision drivers

- The emitter is a pure function of the request bytes. It reads no clock, environment variable,
  file or network, and no map order reaches its output. The host's check mode then holds.
- The generated code depends only on the Go standard library. A service can adopt it without
  taking this runtime as a dependency.
- No contract text can alter the generated source. A pointer, member name, value or id that ends a
  comment or a string literal must not change the program.
- A contract the emitter cannot render faithfully is refused, never rendered approximately. The
  same holds for names it cannot form, or that would collide.
- `Build`'s output is exactly the RFC 8785 form that the runtime's `internal/jcs` writes for the
  same document.

## Considered options

- A standalone program, `cmd/jpack-emit-go-facts`, that imports only the standard library and
  `internal/jcs`, the runtime's RFC 8785 writer, with generated code that writes canonical JSON itself.
- A `jpack` subcommand. Rejected: an emitter is a separate program the host starts (RFC 0036 §1),
  and a subcommand would tie the generated code's release to the runtime's.
- Generated code that calls `encoding/json` and then canonicalizes. Rejected: `encoding/json`
  writes HTML-escaped strings and does not order members by UTF-16, so a second pass would be
  needed in every adopter's binary.

## Decision outcome

Chosen option: the standalone program, because it is the emitter contract's own shape, and the
generated code stays free of this runtime.

**The program.**
- `emit(request) (response, error)` holds the logic, and `main` only wires standard input and
  output to it.
- The request must be `emitRequestVersion` `1.0.0` with exactly `emitter`, `options` and
  `factsContracts`. The JSON is read strictly: no repeated member name, no invalid UTF-8, no
  unpaired surrogate escape, a depth of at most 32, and nothing after the value.
- A refusal writes one fixed line to standard error, which never quotes the request, writes nothing
  to standard output, and exits 1. The host reports that exit as `EMIT-PROCESS`.
- The response has no diagnostics. Its files are sorted by path and held to companion §11.3's
  bounds and text rules.

**Options.**
- `package` is required, matches `^[a-z][a-z0-9]*$`, is at most 64 bytes, and is not a Go keyword.
- `directory` is a literal relative path whose segments start and end with a letter, a digit or
  `_`. So no segment is `.`, `..` or `.git`. It defaults to `generated/facts`, which the
  manifest's `owns` glob names.
- The program checks both again, with the same rules as the manifest's schema.

**Files.**
- Each contract gets `<directory>/<stem>_facts.go`.
- `facts_support.go`, the same in every package apart from its package clause, holds `Decimal`,
  `BuildError`, `Ptr` and the canonical writer.
- Every file starts with one header line naming the emitter's id and version. A generated
  contract's root doc comment also names the kind, version and digest it came from. Every file is
  formatted with `go/format`.
- The Go names come from the naming rule below, and are ASCII letters and digits. Every other text
  from a contract (a pointer, a member name, a value, an id) reaches the source only through
  `strconv.QuoteToASCII`, so the files are ASCII.

**Types.**

| Contract position | Go |
|---|---|
| object with literal members | a struct `*T` with a constructor `NewT` |
| object whose only member is `*` | `map[string]V`, written in UTF-16 order |
| string with `values` | `type T string`, a constant per value, and `valid()` |
| string, boolean | `string`, `bool` |
| decimal | `Decimal`, made only by `ParseDecimal` (the Core §2.2 grammar), `MustParseDecimal` or `DecimalFromInt64` |
| null | no field when always present; a `bool` set flag when optional |
| array with `items` | `[]E`, its element from the `<p>/*` row, or from `items` when there is no row |
| `secretReference` object | a struct of exactly `Scope` and `Store` |

- An optional member is a pointer, and nil leaves it out. An optional null is the exception: it is
  a `bool`.
- A struct-typed member is a pointer either way, and `Build` refuses nil for one the contract
  always holds.
- A nil array or map the contract always holds is written as `[]` or `{}`.
- Decimal and boolean `values` are checked inline, with no constants. A contract's decimal values
  are compared in normal form, so "1.0" and "1" are one value.

**`Build`.**
- It writes the members of each object in RFC 8785 order.
- It writes strings with RFC 8785's escapes, and decimals as JSON strings in RFC 0037 §2's normal
  form.
- It returns a `*BuildError` and no bytes for three things: a nil value the contract requires, a
  value outside the contract's values, or a string that is not valid UTF-8. The error carries the
  JSON pointer of the value and a fixed reason, never the value.

**The naming rule.**
- A word is a maximal run of ASCII letters and digits. A text's part is its words, each with the
  first letter upper-cased, run together.
- The contract's prefix is the part of the last segment of its id, cut at `?` and split at `/` and
  `:`. The root struct is the prefix and `Facts`.
- A position's type is the prefix and the part of each pointer token, with `*` as `Item`.
- A field is the part of its token, with `X` before a leading digit.
- A constant is its type and the part of the value.
- A constructor argument is the field with its first letter lower-cased. When that is a Go keyword
  or predeclared name, the argument is the field itself.
- The file stem is the id segment's words in lower case, joined by `_`.
- An empty part is refused, as is a prefix that begins with a digit.
- These collisions are refused, never renamed:
  - two package-level names (types, constructors and constants across every contract, and the
    support file's exported names);
  - two fields of one struct;
  - a root field named `Build`;
  - a constructor argument named as its own type;
  - two contracts with one file.

**Refused contract positions** (`errUnsupported`):
- a pointer listed with more than one type: an `anyOf`, or an open position's six-type rows;
- an object with both literal members and `*`, or `*` at the root;
- an array without `items`, or an array of objects or arrays without its element row;
- an element row whose type is not `items`, or whose values differ from the array's;
- `values` on an array of objects or arrays;
- a secret reference that is not exactly the two always-present strings.

### Consequences

- Good, because a service builds the facts a pack reads through types the compiler checks, and a
  value the contract excludes never reaches a pack.
- Good, because the vectors in `cmd/jpack-emit-go-facts/vectors/` use RFC 0036 §5's layout. The
  harness can therefore run them, and `go vet ./...` type-checks every expected file as a package.
  `internal/golden/dbrolefacts` compiles one vector's output and tests what its `Build` writes.
- Bad, because generated contracts of kinds whose objects are open are refused. Those are objects
  without `additionalProperties: false`, which become six-type `*` rows. Supporting them needs a
  Go type for any facts value, which this version does not have.
- Bad, because an optional array or map is a pointer to a slice or map, which is unusual Go. The
  pointer keeps "absent" apart from "empty".
- Bad, because a request whose generated names collide must be fixed in the contract: the emitter
  has no renaming option.
- Revisit when a kind with open objects needs a builder, or when the TypeScript builder or the
  Go typed loader of RFC 0036 §6.1 is built, so that the three can share one naming rule.

## More information

- Spec RFC 0037 §6, RFC 0036 §1–§3 and §5, and registry companion §10 and §11.
- ADR-0095 (facts contracts), ADR-0098 (the emitter host) and ADR-0099 (the conformance harness).
