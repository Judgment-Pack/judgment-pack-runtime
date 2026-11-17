---
status: proposed
date: 2026-11-16
deciders: maintainer
---

# A TypeScript constants emitter that leaves every secret position out of the type and the value

## Context and problem statement

Spec RFC 0036 §6 item 2 (registry companion §11.6) calls for a reference emitter that writes
TypeScript constants from a resolved registry. For each kind it reads, the output holds types made
from the kind's schema (the subset of companion §3.2) and a read-only constant object of the
kind's resolved entries. Every member at a secret position must be left out of both the type and
the value. The output is for frontend code, so a secret reference that reached it would ship in a
bundle.

The emitter runs under RFC 0036 with `"input": "resolved"`. It needs each read kind's schema and
`secrets` patterns, which a resolved request carries since ADR-0101 (spec #352). The RFC leaves
open how the environments are split, the generated code's names and form, what a secret position
inside an array or a `$ref` does, and what the program refuses.

## Decision drivers

- No string at a secret position, and no name of a member at one, reaches any output file. The
  kind's `secrets` patterns are the only source of secret positions. A value is never inspected to
  guess one, since a string is a reference only at a declared position (RFC 0035 §1).
- The declaration that is read is the one the pin names: its RFC 8785 SHA-256 is the pin's digest.
- No request text can change the generated program. Only a kind name becomes an identifier, and
  only through a fixed rule and a check. Every other text becomes a string literal.
- A schema, a secret position or a name the emitter cannot render faithfully is refused with a
  diagnostic, never rendered approximately and never renamed.
- The emitter is a pure function of the request bytes. It uses the standard library and
  `internal/jcs` only.

## Considered options

- `"environments": "each"`: one request and one file per environment.
- `"environments": "all"`: one request holding every environment, written to one file or several.
  Rejected. A file holding every environment would put production values in a development bundle.
  Split files gain nothing over "each", and each run's request would be larger.
- Writing a `$ref` as a named type alias. Rejected, because a definition can be used at a secret
  position in one place and at a plain position in another. What is left out follows the position,
  so the type is written out where it is used.
- Refusing by exiting 1 with a fixed line, as ADR-0100 does. Rejected for content refusals: a
  diagnostic can name the kind or the entry position the kind author must change. An unreadable
  request still exits 1.

## Decision outcome

Chosen option: "each", with `$ref` written out where used and content refusals as diagnostics.

**The program.** `cmd/jpack-emit-ts-constants`, with its manifest `emitter.json`.
- It reads request `1.0.0` strictly. The request must have exactly its five members and one
  resolved document. JSON is read with no repeated member name, no invalid UTF-8, no unpaired
  surrogate escape, and nothing after the value. The depth is at most 35: the resolved document's
  32 containers (RFC 0034 §1), two levels above it, and a scalar.
- A request it cannot read gets one fixed line on standard error, nothing on standard output, and
  exit 1 (`EMIT-PROCESS`).
- A request it refuses gets a response with no files and error diagnostics. Each diagnostic is an
  `x-ts-*` code and a companion §7.3 location that names only what the request holds, and the host
  fails the run.

**Declarations.** Before a declaration is read, every pin must have one, and each declaration must
have a pin.
- A declaration whose SHA-256 over its RFC 8785 form is not its pin's digest is refused
  (`x-ts-declaration-mismatch`). So is one whose id or version is not its pin's.
- A pin with no declaration is refused (`x-ts-declaration-missing`).
- A declaration with no pin is refused (`x-ts-declaration-unpinned`).
- These are refused with `x-ts-declaration-form`:
  - two declarations of one kind, or a declaration that is not an object with a valid `kind`;
  - after the digest check, a `kindFormatVersion` other than `1.0.0`, a `schema`, `$defs` or
    `secrets` of the wrong form, or a member companion §3.1 does not list. A later format could
    give such a member a meaning for secrets that this emitter would not apply.

**Output.** Each environment gets `<directory>/<environment>.ts`.
- The file opens with a header line naming the emitter and the registry digest. A `JsonValue`
  alias follows, only when it is used.
- Then comes each pinned kind in name order:
  - a comment with the kind's version and digest;
  - `export type T = …;`;
  - `const xEntries = {…} as const;`, keyed by entry id;
  - `export const x: Readonly<Record<keyof typeof xEntries, T>> = xEntries;`.
- Entries and members are in RFC 8785 order. Every line ends with LF, and the file ends with one.
- `as const` with a typed alias is used rather than `Object.freeze`, which is shallow and acts only
  at run time. A consumer sees the schema's type, and the compiler checks every value against it.
- Annotations (`title`, `description` and the like) are not written.

**Types.**

| Schema | TypeScript |
|---|---|
| `type` | a union of `null`, `boolean`, `number` (for `number` and `integer`), `string`, `readonly E[]` and an object type |
| `enum`, `const` | a union of the values' literal types |
| `properties`, `required` | `readonly "name": T`, with `?` unless required |
| `additionalProperties` | a readonly index signature (below), or none when it is `false` |
| `items` | `readonly E[]` |
| `anyOf`, `allOf` | a union, an intersection |
| `$ref` (`#/$defs/<name>`) | the definition's type, written where used. A cycle is refused. |
| `true`, or no narrowing keyword | `JsonValue` |
| `false` | `never` |
| `not`, a tuple `items`, any keyword outside the subset | refused (`x-ts-schema-unsupported`) |

- A constraint TypeScript cannot state (a bound, a pattern, `uniqueItems`, `propertyNames`) is not
  stated.
- An index signature's type is the additional members' type, plus `undefined` when a declared
  member is optional. Unless that type is `JsonValue`, the declared members' types are joined to
  it as well, so every declared member is admitted.
- `JsonValue`'s object members admit `undefined`, so that an object type with optional members is
  a `JsonValue`.
- A schema is read within 4096 visits and 16 branches a composition (companion §3.3). Past that it
  is refused.

**Secret positions.** Each position carries the patterns that can still match below it.
- A member at which a pattern ends is left out of its object's type and of every value, name
  included.
- A `*` that ends at an object leaves every member out and removes its index signature. An object
  with nothing left is `{ readonly [name: string]: never }`.
- These patterns are refused (`x-ts-secret-in-array`), in the type and again in the value:
  - one that ends at an array item, since an item cannot be left out without changing the array;
  - one below a position the schema does not narrow, whose last token is `*` or an index, since it
    may end at an item.
- A pattern that would end inside an `enum` or `const` value is refused
  (`x-ts-schema-unsupported`).
- A string that looks like a secret reference at a position that is not a secret one is written
  like any other value. The `nested-object` vector holds one.

**Text.**
- A string becomes an ASCII double-quoted literal. Every code point outside ASCII, U+2028 and
  U+2029 among them, becomes `\uXXXX` UTF-16 escapes, and each control character becomes a short
  or `\u00XX` escape. Each literal is also a JSON string literal of the same string.
- Numbers are written as the request gives them.
- `__proto__` is refused (`x-ts-member-proto`) wherever an object literal would read it as the
  prototype: as an entry id, as a value member, as a schema property, and inside an `enum` or
  `const`. It is never written as a computed key. A `__proto__` member at a secret position is
  left out, like any other secret member.

**Names.**
- A kind's words are its runs between hyphens. Its type is the words with each first letter
  upper-cased (`db-role` gives `DbRole`). Its constant is the same with the first word as written
  (`dbRole`). Its entries object is the constant and `Entries`.
- A constant that is a reserved word of ECMAScript or of strict-mode code (`eval` and `arguments`
  included) is refused (`x-ts-name-reserved`). So are `require`, `exports`, `module`, `undefined`
  and `globalThis`, and a type named `JsonValue`, `Readonly` or `Record`.
- A name that an earlier kind or the generated code holds is refused (`x-ts-name-collision`).
- Every name is checked against `^[A-Za-z][A-Za-z0-9]*$`.

**Bounds and paths.**
- The file is at most 16 MiB and the response 64 MiB. The writer stops at the bound
  (`x-ts-bounds`), and so does the walk of the types: a member's type is shared with the index
  signature's union, so a chain of `$ref`s within the visit bound would otherwise walk 2^n types. A response has at most 100 diagnostics.
- An environment that is a Windows device name is refused (`x-ts-path`), since the host would
  refuse its path.
- `directory` defaults to `generated/constants`, which `owns` names. It is a literal relative path
  with no segment beginning with `.` or `_`, no segment over 255 bytes, and no segment
  `node_modules`, `vendor` or `testdata`, in any case. The manifest's schema and the program check
  the same patterns.

### Consequences

- Good, because a frontend reads typed, read-only configuration in which no secret reference and
  no secret member name appears. The test of that uses the registry's own reading of secret
  positions, not the emitter's.
- Good, because the vectors in `cmd/jpack-emit-ts-constants/vectors/` use RFC 0036 §5's layout,
  and `jpack emit conformance` passes all six checks on them.
- Bad, because a kind with a secret at an array item cannot be emitted. The kind must hold the
  secret in an object member, or the manifest must not read it.
- Bad, because a `$ref` used in many places is written out many times. The file bound limits
  this.
- Bad, because the constants are read-only only to the compiler. JavaScript that ignores the types
  can still change them at run time.
- Bad, because no TypeScript compiler runs in CI. `internal/golden` type-checks the output and a
  consumer of it with `tsc --strict` only where `tsc` is on PATH, and skips otherwise. The syntax
  targets TypeScript 3.7 or later, and the consumer file needs 3.9 or later.
- Revisit when CI has a TypeScript toolchain, or when the Go typed loader of RFC 0036 §6 item 1
  and this emitter share one naming rule.

## More information

- Spec RFC 0036 §1–§3, §5 and §6, RFC 0034 §1 and §3, RFC 0035 §1, and registry companion §3, §7.3,
  §11.2 and §11.6.
- ADR-0098 (the emitter host), ADR-0099 (the conformance harness), ADR-0100 (the Go facts emitter)
  and ADR-0101 (declarations in the request).
