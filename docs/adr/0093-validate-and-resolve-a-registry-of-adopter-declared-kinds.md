---
status: accepted
date: 2026-11-16
deciders: maintainer
---

# Validate and resolve a registry of adopter-declared kinds, as a top-level command

## Context and problem statement

The specification accepted RFCs 0034 and 0035: a registry of entries of kinds that adopters declare,
each kind with a closed JSON Schema subset, per-environment overlays, entry references, secret
references pinned to a version and scoped to an environment's projects, a fixed validation order
with a value-free report, and resolution to one document per environment. The companion text
(`spec/registry.md`), its schemas and its vectors are not released yet. The runtime needs a reader
now, so emitters, facts contracts and adopters can build against it. It must not claim conformance
to a text that is not released yet.

Material-decision impact: public-surface.

## Decision drivers

- One reader, following the RFCs' order and codes exactly, so the companion's vectors can be
  checked against it when they exist.
- Nothing fetched: a secret reference is checked for form, pin and scope and left as written.
- A report never carries a value from the documents: no secret reference, entry reference or
  member value, only codes, fixed messages and locations.
- Deterministic bytes: the resolved document is RFC 8785 canonical JSON, independent of the order
  kinds are supplied in.
- No existing bytes or behaviour change.

## Considered options

- A top-level `jpack registry` group now.
- `jpack experimental registry` until the companion text is released.
- A library only, with no command.

## Decision outcome

Chosen option: "a top-level `jpack registry` group", because the RFCs are accepted and the
companion is intended for stable maturity at its first release. Adopters' CI steps and emitters
need a command name that will not move. The experimental surface is for designs that may be
withdrawn, which this one is not. A library alone would leave emitter authors and CI steps without
a command.

`internal/registry` implements RFC 0034 §3's eight steps:
1. admission of each document (size bound, strict I-JSON, depth 32, the number rules, the exact
   selector);
2. kind declarations;
3. the registry's shape;
4. pins;
5. entries;
6. overlays;
7. resolved values per environment, against the schema and the references;
8. bounds.

`internal/jcs` gains `EncodeDocument`, full RFC 8785 for any JSON value (null and non-integers
included); `Encode` is unchanged.

Choices the RFCs leave to the reader, recorded here so the companion can settle them:

- **Locations.** A location names its document (`registry`, `kind` or `value`). A kind document is
  named by the sha256 of its raw bytes, because one that fails admission has no readable `kind`.
- **What stops validation in step 3.** A form error at the registry level (a member, the
  environments, owners or scopes) stops before step 4. A malformed entry, overlay or patch is
  dropped and reported, and validation goes on. An order error is reported and does not stop it.
- **Pointer reachability is permissive** through `$ref`, `allOf` and `anyOf`. A pattern is
  unreachable only when no branch could hold it.
- **`secrets` and `references` are optional**, empty by default. The empty pointer pattern (the
  whole value) is refused.
- **A malformed overlay member** is reported as `REG-REGISTRY-FORM` in step 6.
- **A document over its size bound** is a validation finding (`REG-DOC-SIZE`, exit 1), not an
  operational error, and validation goes on with the other documents. The reader stops at the bound
  and digests nothing it did not read. A resolved document over its bound is a step 8 finding for
  its environment, so `resolve` never meets one.
- **Nothing that could be a value is named.** A location names an entry only when its kind and id
  are well formed, and an overlay `set` key that is not a pointer, or that no overlayable pattern
  matches, is located at the `set` member. The report names the registry by its digest only.
- **A failed patch fails its (entry, environment) pair.** Steps 7 and 8 skip the entry in that
  environment, as RFC 0034 §6 runs each check only on what passed the earlier ones.

Commands:

- `validate REGISTRY --kind K...` writes the report (`reportFormatVersion` `1.0.0`). It exits 0 when
  the registry is valid (warnings allowed) and 1 when it is not.
- `resolve REGISTRY --kind K... --environment E` writes the canonical resolved document and one
  newline. An invalid registry writes the report and exits 1.
- An invocation error exits 3 and an unreadable input exits 4, as elsewhere.

### Consequences

- Good, because emitters (RFC 0036) and facts contracts (RFC 0037) can be built on one resolved
  form now.
- Good, because a CI step can gate on `validate`'s exit code without its log showing a secret.
- Good, because the formats carry RFC 0034 §8's promise: within 1.x nothing is removed or changes
  meaning, and additions come only under a new minor selector (VERSIONING.md).
- Bad, because the RFCs define only the `REF-*` codes and `REG-DOC-NUMBER`; every other `REG-*` code
  and the location members are this runtime's own reading. That list is provisional until
  `spec/registry.md` is released, and a different list there is adopted under a new selector.
- Revisit when `spec/registry.md` and its vectors are released: the claim goes into CONFORMANCE.md,
  and YAML authoring (RFC 0034 §7) is read through the same steps.

## More information

Spec RFCs 0034 and 0035 (accepted 2026-11-16). `internal/registry`, `internal/jcs/full.go`,
`internal/cli/registry.go`.

## Amendment: a bounded report (2026-11-16, #427)

A registry document within its 8 MiB bound can fail on every entry. It can also find thousands of
warnings, one `REF-SECRET-UNSCOPED` for each secret position of each entry in each environment.
Without a bound, the report grows with the document and can outgrow it, and the reader's memory
grows with the report.

`validate` and `resolve` report at most the first 1000 diagnostics in the report's own order
(location, then code, then message). The collector keeps the 1000 smallest distinct diagnostics in
a sorted list that it trims as it inserts. A diagnostic that sorts past the 1000th costs no memory.
So a capped report is exactly the first 1000 diagnostics of the uncapped one, in the same order. It
does not depend on the order the checks found them in, or on the order the kinds were supplied in.
When anything was dropped, the report ends with one `REG-REPORT-TRUNCATED`, a warning located at
`{"document":"registry","pointer":""}`. Like every location, it carries no value. The report has no
member for counts, and none is added.

- **Every check still runs.** The report's order is not the order the checks run in: entries are
  ordered by kind and id, `/entries/10` sorts before `/entries/2`, and step 8's registry findings
  sort before step 7's value findings. Stopping at the 1000th diagnostic found would therefore not
  report the first 1000. The work is linear in the input, which the 8 MiB admission bound already
  caps, and `resolve` needs every check's result anyway. So the work does not stop early; only what
  is kept is bounded.
- **Validity is judged on every diagnostic found**, counted by severity whether kept or not. A
  registry with more than 1000 warnings and no error is valid, and `resolve` resolves it. A
  registry whose only error sorts past the 1000th diagnostic is invalid. The exit code follows
  validity, as before. This is why `REG-REPORT-TRUNCATED` is a warning: it says the report is
  partial, not that the registry is invalid.
- **Ties are broken by code, then by message.** So distinct diagnostics never compare equal, and the
  kept 1000 and their order are fully determined. One comparison serves both the bounded list and
  the final sort.
- **Import's bound differs** (ADR-0094). `import` stops reading at its 1001st refusal and reports
  `REG-YAML-TRUNCATED` where it stopped. A YAML refusal is located by line and column, which reading
  reaches in order, so stopping early reports the first refusals. Once a file is refused there is
  nothing left to judge, and an alias-heavy file could hold millions of refusals. Validation reports
  in an order its checks do not follow, and it judges validity and resolves on everything found. So
  it bounds what it keeps instead of stopping.

`REG-REPORT-TRUNCATED` is provisional, like the other `REG-*` codes, until `spec/registry.md` is
released.

## Amendment: the resolved document's depth (2026-11-16)

Step 8 also holds each environment's resolved document to the depth bound, 32 levels with the root
object counted as admission counts them. An overlay can set a value at a deep pointer, so a registry
within its own depth bound can resolve past it. Such an environment is a `REG-DOC-DEPTH` finding
located at `{"document":"registry","environment":E,"pointer":""}`, and the registry is not resolved,
as for a resolved document over its size bound.

## Amendment: bounds from the v0.47.0 release review (2026-11-16, #432)

The review found inputs within the size bounds whose cost grew far past their size. Each is now
bounded where it is read, and each refusal uses a code a kind can already get, except one.

- **A schema is counted expanded** (`MaxSubschemas`, 4096). Validation, pointer reachability and a
  facts contract's reading follow each `$ref` into the definition it names, so they visit the
  schema expanded, not as written. A definition named sixteen times by each of eight others is
  about 3 KB written and 16^8 nodes expanded. The written count stays, and the expanded count is
  held to the same 4096, refused as `REG-KIND-SCHEMA-LIMIT`. The expanded count is every node:
  each `properties` member, `additionalProperties`, `items`, `not`, each `anyOf` and `allOf`
  branch, a boolean schema, `propertyNames` as one, and at each `$ref` the expanded count of its
  definition. A definition's count is computed once and reused at every `$ref` to it, so counting
  costs the schema as written, and it stops just past the bound. A definition no `$ref` reaches is
  held only by the written count. 4096 is the bound a schema already had, so a value is checked
  against at most 4096 nodes at each position, as the written bound meant. A kind is refused only
  when its expansion passes that; every kind in this repository's tests is admitted as before.
- **A pattern's program is bounded** (`MaxPatternInstructions`, 4096 per pattern, and
  `MaxKindPatternInstructions`, 65,536 per kind). RFC 0034 §2.2 admits `{n,m}` up to 1000 in 256
  code points, and RE2 compiles `x{n,m}` as `m` copies of `x`. So `(a…a){1000}` compiles to a
  quarter of a million instructions, about 55 MB, and a 256 KiB kind holds 880 of them. A
  pattern's size is counted before anything is compiled (on the pattern as written since the
  amendment below; it was first counted on its parse, as RE2 counts it). A pattern
  past 4096, or one that would take the kind's admitted patterns past 65,536, is refused with the
  new code `REG-KIND-PATTERN-LIMIT`. `REG-KIND-PATTERN` means a pattern outside the subset, which
  this one is not, so the new code says which bound was passed. A refused pattern is not counted.
  A typical id pattern is about 130 instructions, so 4096 leaves room for a long one, and 65,536
  is sixteen patterns at the bound. Matching is RE2's, linear in the input times the program, so
  the program bound also bounds each match.
- **A long literal is kept short.** Admission keeps an integer as written, which is at most 16
  digits, and any other number as the RFC 8785 form of its value. RFC 0034 §1 admits a decimal
  only when its exact value is that of the shortest decimal of its double, so the two are one
  value and no reading changes. Every later step reads a literal of at most 25 bytes. Before,
  `1.` and a million zeros was admitted, and the range check skipped it because big arithmetic
  refuses an exponent past 10^6. That check now fails closed: a number that cannot be compared
  with a range is `REG-VALUE-RANGE`, never let through.
- **Distinct lists are checked in one pass**: owners, environments, an entry's environments, scope
  projects, `required`, `type`, a kind's pointer patterns, reference kinds and sources, and an
  overlay's pointers (a prefix in either direction, through a tree of their tokens). `uniqueItems`
  keys each item by its RFC 8785 form. For admitted values that is the equality JSON Schema uses,
  since an admitted number is exactly its double's shortest decimal. An item with no RFC 8785 form
  is compared with the earlier items one by one.
- **Two documents of one kind** are one kind, read once. When two byte-different documents share
  a canonical digest, the one read is the one whose bytes' digest sorts first, so the
  `kindDocument` its findings name does not depend on the order of `--kind`.
- **Step 7's schema checks share one work bound** (`maxValidationWork`, 2^26 node visits over the
  registry, every environment included). The bounds above each hold one input: 4096 nodes per
  position, a value's size, the environments. Validation costs their product, so a 3 KB kind
  whose items expand to about 2187 nodes, with an array of four million zeros in each of 32
  environments, ran for 43 minutes and was found valid. Each node visited at one position of a
  value is charged once, and so is the work within a visit that grows with the value: an `enum`
  lookup and the key it builds, a `const` comparison by the nodes it reads, each `uniqueItems` item
  (32 units, measured) and its key's bytes, an object's members and `required` names, a pattern
  match by the input's length times its program, and each finding held, by its path's tokens. The
  key that finds an already-checked value is charged by its bytes. An `enum` is looked up by each
  value's RFC 8785 form, so checking a value against 1024 values costs one lookup. Where the bound runs out, the entry is reported once, at its value's
  root, with the new code `REG-BOUND-VALIDATION-WORK`, and no value after it is checked against
  its schema; the registry is then not valid. The same value of the same kind (its canonical
  digest and the kind's) is checked once and its findings reused, so an environment that changes
  nothing costs nothing. 2^26 is about a second of checking; a registry of ten thousand entries
  of a hundred positions each, against ten nodes at each, in ten environments, uses about a
  sixth of it. A facts contract's closed-list check charges the same visits to its own work
  bound.

Not bounded here, because each is a product of two inputs already bounded: a kind's secrets
against its reference patterns, the overlayable patterns against a patch's pointers, and pointer
reachability, which is a pattern's tokens times the expanded schema.

## Amendment: resolution shared across environments (2026-11-16, #434)

Step 7 held a deep copy of every entry's value for each environment, and step 8 encoded each
environment's resolved document and decoded it again for the depth check. An 8 MB entry in 32
environments took 102 s and 4.7 GB. Now:

- **A value is shared, read-only.** Where no overlay changes an entry in an environment, that
  environment holds the entry's own value. An overlay copies only the containers on its pointers'
  paths, each once however many of its operations pass through it, and shares the rest; nothing
  writes into a shared value afterwards (the facts mapping copies, as before).
- **A value is measured once.** Its canonical length, nesting and digest are computed once for
  every environment that shares it, and once per environment an overlay changes it in. Step 7's
  reuse of checked values keys on that digest, and step 8's `REG-BOUND-ENTRY-BYTES` reads that
  length.
- **Step 8 measures each environment's document without encoding it.** The document is encoded
  with every value as `null`, and each value's length takes the place of null's four bytes: RFC
  8785 writes a container as its members' forms joined, so the sum is the document's exact length.
  A value sits in the root object, the entries array and its entry, so the document nests three
  more than its deepest value, counted as carrier admission counts depth (the root object as one,
  32 admitted). A test holds both to the encoded document and to carrier's reading of it.
  `Resolve` still encodes the one environment it is asked for.

Environments are at most 32, so an overlay on a large entry in every environment still costs
that entry once per environment; every other cost is the registry's own.

## Amendment: a pattern's size is counted as written (2026-11-16)

The registry companion (§3.4, "Program size") fixes how a pattern's size is counted, so that every
reader refuses the same patterns: on the pattern as written, not on one engine's parse. Each
literal code point, escape, `.`, class, `^` and `$` counts 1; a group counts its content; `*`
counts 2 plus its atom and `+` and `?` 1 plus it; `{n}` counts n times its atom, `{n,}` 1 plus n
times it (`{0,}` as `*`), and `{n,m}` m times it plus m − n; a sequence sums its parts and an
alternation of k alternatives sums them plus k − 1; every construct counts at least 1. The count
is made in the subset's lexical reading, saturating just past 4096. RE2's parse factors common
prefixes, so `(ab|ac){1000}` was counted as `a(b|c)` repeated, 4000, and admitted; as written it
is 5000 and is now `REG-KIND-PATTERN-LIMIT`. Size is held only for a pattern within the subset:
the parse still runs, and Go's refusal of a nested repetition past 1000 stays `REG-KIND-PATTERN`
whatever the size. The kind's total of 65,536 is of the same sizes. The written size is still an
upper bound on the compiled program, and close to it, as a test holds.
