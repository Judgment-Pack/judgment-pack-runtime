---
status: proposed
date: 2026-11-16
deciders: maintainer
---

# A reader for Terraform plan and state JSON, with every value marked sensitive removed first

## Context and problem statement

The specification accepted RFC 0038 on 2026-11-16. It proposes one dialect source kind,
`terraform-file`, that reads the JSON `terraform show -json` writes for a saved plan or for state.
The reader:
- binds the acquisition to the file's sha256;
- removes every value Terraform marks sensitive before anything reads it;
- accepts a closed table of `format_version` values.

The companion text (`spec/dialect.md` §4.5) is proposed, not released, and no vector exists yet.
The RFC's Implementation section gives this runtime two things: the reader of §2 to §5, and the
batch tool of §6 (`jpack terraform facts`).

This record covers the reader alone: package `internal/terraformfile`. Nothing calls it yet. The
batch tool comes in a later change, under its own amendment. `jpack dialect validate` does not
admit the kind, because which dialect version the kind joins is still open (RFC 0038, Unresolved
question 1).

## Decision drivers

- No value marked sensitive, and no member name read before removal, may reach a value, a record
  or a diagnostic.
- Every refusal is decided by the file's bytes alone, so each vector states one expected code.
- Memory is bounded and measured for a file the RFC accepts. The largest such file is 256 MiB.
- The bytes are deterministic: the acquired value is RFC 8785 canonical JSON.
- No existing bytes or behaviour change.

## Considered options

- A package of its own, with a strict scanner and an arena of byte offsets.
- The carrier (`carrier.Decode`) into `map[string]any`.
- The `encoding/json` token stream.

## Decision outcome

Chosen option: "a package of its own". The other two fail the drivers:
- The carrier's diagnostics carry a pointer, and a member name before removal can be secret data.
- The carrier's 250,000-node bound would refuse a plan the RFC accepts.
- `encoding/json` accepts duplicate member names, lone surrogates and invalid UTF-8.
- A tree of maps costs several times the arena per value.

**API.**
- `Read(r io.Reader, src Source) (*Acquisition, *Refusal, error)`. Exactly one result is non-nil.
- `Source{Document, Select{Types, Mode, Address, Actions, Max}, MaxBytes}`.
- `Source.Validate()` refuses a malformed source with `ErrInvalidSource`. That error makes the
  dialect invalid, not the file (RFC 0038 §4), so it is never a refusal. Its message names the
  member and the rule broken.
- An absent member is a zero value:
  - `MaxBytes`, `Mode` and `Max` read as their defaults;
  - `Address` is a pointer, where nil means absent and the empty pattern matches only the empty
    address;
  - for `Actions`, nil means absent, and an empty non-nil slice is an empty array, which selects
    no change.

  A dialect reader holds a declared `maxBytes` and `max` to their ranges before it fills these
  fields.
- `Acquisition` holds the record's members, `Record()` (canonical JSON) and `Value()` (the canonical
  acquired value). `Resources` gives each selected resource's address and its object's bytes, as a
  slice of the value, for the batch tool.
- `Refusal` is `{code, sha256, bytes, message}`:
  - `sha256` is bare lower-case hex of the whole input;
  - `bytes` is its length;
  - `message` is one fixed text per code.

  Nothing else is carried. The one exception is a stream's `TF-SIZE` (reading 18): it is
  `{code, message}`, with no `sha256` or `bytes`, and its fixed message says the input was
  standard input.
- `ReadStream` is `Read` for a stream that need not end, such as standard input. The caller says
  which it has; the reader never guesses from the `io.Reader`'s type.

**The steps**, in the RFC's order. The first failure refuses, and later steps do not run.
1. The whole input is hashed, even past `maxBytes`, and kept only while it is within `maxBytes`.
   - A stream (`ReadStream`) is read to `maxBytes` and one byte more at most, and no further
     (reading 18).
   - A reader that tells its size (a file, or an in-memory reader) is read into one buffer.
   - Any other reader is read in chunks that grow to 8 MiB and are joined once.
   - `TF-SIZE` comes before any parse.
2. `TF-JSON`. One validating parser scans the input twice:
   - First it counts the nodes. Then it fills an arena of exactly that many 12-byte nodes
     `{start, end, subtreeSize}`, in pre-order, with the kind read from the first byte.
   - It refuses a byte-order mark, invalid UTF-8, a lone or reversed surrogate escape, a raw
     control character, trailing data, and a value deeper than 128. The top-level value is depth
     1. A member name is not a value.
   - Duplicate member names are compared decoded, so `"a"` and `"\u0061"` are duplicates. Each
     object's names are sorted, in O(m log m).
   - Numbers stay as their text.
3. `TF-FORMAT`: `format_version` is compared decoded against the table below.
4. `TF-DOCUMENT`: as the RFC writes it, with the readings below.
5. `TF-SENSITIVITY`: every resource of the read section carries its mirrors. This is checked
   before selection, so `select` cannot change the outcome.
6. Removal, by the rules of RFC 0038 §2 step 6. Removed nodes are marked in a bitset, and the
   writer skips them.
   - `removedSensitive` counts over the whole read section, selected or not.
   - A count taken under a position is dropped when a removal escalates past that position.
7. Selection, then the acquired value:
   - `TF-SELECT-BOUND` when more resources are selected than `max`.
   - Then the stable sort by address, in code-point order.
   - Then `TF-NUMBER` over the numbers the value would hold.
   - Then `TF-VALUE-LIMIT`.

**The address pattern.** `*` never crosses `.`, `[` or `]`. So a pattern and an address that it
matches have the same delimiters in the same order. The matcher splits both at those delimiters.
Within each segment, it finds the pattern's literal pieces leftmost, in order. That is exact for a
glob with no other operator, and nothing backtracks. A fuzz target holds it to a regular expression
that states the RFC's rule directly.

**The acquired value** is written straight from the arena:
- Members are ordered by the UTF-16 code units of their decoded names.
- Strings carry ECMAScript's minimal escapes.
- Every number is written as its normal form, in a string.

A counting pass measures the value first. A value over the limit is refused before anything is
allocated for it, and an accepted one is written into one buffer of exactly its size.

**The normal form** of a number is RFC 0037's, which is also the registry companion's §10.1. It is
computed on the literal's digits alone:
- an exponent saturates at 2^40;
- the length is known before anything is built;
- `1e1000000` costs its 9 bytes, not a million digits.

**Bounds**, as named constants in `codes.go`:

| Constant | Value | Source |
|---|---|---|
| `DefaultMaxBytes` | 67108864 | RFC 0038 §1 |
| `MaxBytesLimit` | 268435456 | RFC 0038 §1 |
| `MaxDepth` | 128 | RFC 0038 §2 step 2 |
| `MaxTypes` | 64 | RFC 0038 §4 |
| `MaxPatternChars` | 512, counted in code points | RFC 0038 §4 |
| `DefaultSelectMax` | 1000 | RFC 0038 §4 |
| `SelectMaxLimit` | 10000 | RFC 0038 §4 |
| `MaxNumberChars` | 64 | RFC 0038 §5 |
| `MaxValueBytes` | 268435456 | this runtime |
| `ValueBytesPerInputByte` | 16 | this runtime |

**Memory.** The arena's worst case is `[0,0,...]`, at one node per two input bytes: 6 bytes of
arena per input byte. A test measures TotalAlloc, not time, at 4 MiB, and the last case at the
518 KiB that shows it:

| Case | Measured, per input byte | Budget |
|---|---|---|
| `[0,0,...]`, read through step 3 (arena, input and removal bits) | 7.07 | 7.5 |
| One object of about 470,000 members | 5.53 | 7.5 |
| A selected resource whose value is the whole file (arena, input, and a value twice the input) | 9.07 | 9.5 |
| `after_unknown` `{"<512 KiB name>":{"k0":true,...,"k479":true}}`, a 240 MiB value, refused `TF-VALUE-LIMIT` (input, and the pointer path) | 3.04 | 3.5 |

So a worst-case file costs about 450 MiB at the 64 MiB default, and about 1.8 GiB at the 256 MiB
cap; a read whose value is the whole file adds about twice the file. The value, and the
`afterUnknown` pointers collected for it, are each held to 16 times the file (reading 14), and the
pointers are counted before they are built. Large key lists, and the pointer path, grow by
doubling, not by `append`, which past 256 elements grows by about a quarter at a time and would
allocate several times a large object's keys.

**The format table.** The companion has not released one (RFC 0038, Unresolved question 2). This
runtime's provisional table is `formatTable` `"proposed-1"`: plan `1.0`, `1.1` and `1.2`, and state
`1.0`. Every other version is `TF-FORMAT`, a newer minor of a listed major included. The table
becomes the companion's when the companion releases one, and the release notes say so.

**Codes.** RFC 0038 names `TF-SIZE`, `TF-JSON`, `TF-FORMAT`, `TF-DOCUMENT`, `TF-SENSITIVITY`,
`TF-SELECT-BOUND` and `TF-NUMBER`. This runtime adds `TF-VALUE-LIMIT`. Every code and message is
provisional (VERSIONING.md).

**Readings.** The first six are the premise's open questions, each taken at its recommendation.

1. The format table is `"proposed-1"`, above.
2. **When a mirror and its value differ in shape**, positions are matched as JSON Pointer tokens
   would match them:
   - a mirror member named `"0"` names element 0 of an array value;
   - a mirror array's element 0 names the member `"0"` of an object value;
   - a token that is not an index (`"01"`, `"-"`) names nothing in an array;
   - an object or array mirror that marks any position `true` over a string, number, boolean or
     `null` removes that value, since no reading of the shapes says what the mark covers.

   This reading removes at least as much as any narrower one. The companion should say which it
   means.
3. **A nested mirror position of another type** (null, a string or a number) is `TF-SENSITIVITY`.
   The RFC says so only for the top level. Refusing it fails closed.
4. **Deposed objects share their address.** The sort is stable, so they keep the read section's
   order. The `deposed` member is not carried, since §5 does not list it, so two such objects are
   told apart only by their position. A spec question should settle this.
5. **A malformed resource identity** is `TF-DOCUMENT`, at step 4. This covers:
   - a read-section item that is not an object;
   - an `address`, `type`, `name`, `mode` or `provider_name` that is missing or not a string;
   - an `index` that is present but neither a string nor a number;
   - a plan item whose `change` is not an object, or whose `change.actions` is not an array of
     strings.
6. **An unknown value that Terraform writes as null in a list** is kept as written.

The rest are this runtime's own readings:

7. **`after_unknown`**, when present, must be true, false, or an object or array of those, at
   every position. Otherwise the file is `TF-DOCUMENT`. When it is absent, `afterUnknown` is empty.
8. **A state's `values` without a `root_module`** reads as a state with no resources, as RFC 0038
   §2 step 4 says of absent `values`: `{"format_version":"1.0","values":{}}` is accepted, with an
   empty acquired value. A `root_module` that is present must be an object, so a malformed one is
   `TF-DOCUMENT`. A module's `resources` and `child_modules`, when present, are arrays, of resource
   objects and of modules. A `null` is a member of the wrong type, not an absent one. A plan's read
   section is `resource_changes`, so a plan's `planned_values` need not hold a `root_module`
   either.
9. **The read section's order**, for a state, is a depth-first walk that takes each module's
   resources before its child modules. That order decides ties in the sort.
10. **A value of the read section counts as an object member of its resource.** A mirror of `true`
    over the whole of `before`, `after` or `values`, or a removal that escalates to it, leaves that
    member out of the object and counts one. A position the mirror marks but the value lacks, a
    member an object lacks or an element an array lacks, is ignored. A `null` that the mirror marks
    is removed.
11. **`afterUnknown`**:
    - Pointers are taken from `after_unknown`'s own structure, since an unknown value is absent
      from `after`.
    - Names are escaped as RFC 6901 tokens.
    - Pointers are sorted in code-point order of the pointer strings, so `/l/10` sorts before
      `/l/2`.
    - A `true` at the root gives the pointer `""`.
    - A pointer that passes through, or ends at, a position the sensitivity mirror marks `true` is
      cut at that position, so no member name below it is carried. The pointers cut at one
      position give one pointer, and none when `after_unknown` marks nothing there. A position
      removed from `after` gives none (RFC 0038 §2 step 6); the mirror is consulted as well because
      an unknown value is absent from `after`.
12. **`index`** is a number for `count` and a string for `for_each`. A number becomes its normal
    form, as §5 says of every number, so `count.index` 0 and a `for_each` key `"0"` both read `"0"`.
13. **`TF-NUMBER`** checks only numbers that reach the acquired value. A removed position, an
    unselected resource and an unread section are not checked.
14. **`TF-VALUE-LIMIT`.** RFC 0038 bounds the file, not the acquired value. Normal forms (`1e63` is
    4 bytes and becomes 66) and `afterUnknown` pointers can make the value longer than the file, and
    pointers that repeat a long prefix can make it quadratic in the file. This runtime holds the
    canonical value to the lesser of `MaxValueBytes`, the largest file a source may declare, and
    `ValueBytesPerInputByte` (16) times the file.
    - The multiple is above what numbers can do. An array of `1e63` grows about 13.4 times, 5
      bytes to 67, the most any number can; string escapes never grow. So an ordinary plan, whose
      value is a part of the file, passes, and only pointers that repeat a long prefix
      reach the bound.
    - The pointers are collected within the bound, each charged its bytes and the 8 bytes of its
      span, so a value with many short pointers is refused up to 5 bytes per pointer below the
      bound.
    - A dry pass counts the pointers first, so a list past the bound is refused before any of it is
      built, and an accepted one is collected into buffers allocated once at their counted sizes.
    - The walk keeps one path buffer for both passes, so a long prefix is never copied once per
      child.

    The check comes after `TF-NUMBER`.
15. **A `null`** `planned_values`, `resource_changes` or `values` is present and of the wrong type.
16. **`select.actions`** may repeat a value. The change's `actions` are carried as written.
17. **Mapping a refusal to a source outcome** (dialect §9.2) is left open by the companion. This
    package returns the refusal, and the caller maps it.
18. **A stream is read to one byte past `maxBytes`, and no further** (2026-11-16).
    Standard input need not end, and reading it to its end before refusing it would never return.
    So a stream longer than `maxBytes` is `TF-SIZE`, the code and exit of an oversized file, but
    its refusal leaves out both `sha256` and `bytes`: the stream's whole length was never read, so
    neither its digest nor its size is known, and a digest of its first `maxBytes + 1` bytes would
    read as the whole input's. Its fixed message says the input was standard input and why the two
    members are absent. A file is unchanged: its `TF-SIZE` carries the whole file's sha256 and
    size. A stream within `maxBytes` is read to its end, so every other refusal of it, and its
    acquisition, carry its digest and size as a file's do. The spec text is unreleased; spec issue
    #353 proposes the same: the digest present for a file and absent for standard input.

**Not built:** `jpack terraform facts` and the batch of §6 (since built, see the amendment), the
kind in `jpack dialect validate`,
outputs (RFC 0038, Unresolved question 4), and OpenTofu (Unresolved question 3).

### Consequences

- Good: part 2 can write the batch from `Resources` alone. No secret, member name or pointer from
  an unremoved file can leave the package. The package is fuzzed on the reader and on the pattern.
- Bad: the reader holds the whole file and its arena, about 7 times a worst-case file, so a 256 MiB
  plan needs about 1.8 GiB. Streaming would need the sensitivity mirror before the value it
  mirrors. Terraform writes `after` before `after_sensitive`, so it is not done.
- Neutral: readings 2 to 18 are this runtime's. If the companion settles any of them differently,
  this runtime follows it and says so in its release notes. A changed table release is named in
  `formatTable`.
- Revisit when the companion releases the kind, its table or its vectors, or when part 2 adds the
  command.

## More information

Spec RFC 0038 (accepted 2026-11-16), §2 to §5; the proposed companion text, `spec/dialect.md` §4.5.
`internal/terraformfile`. ADR-0095 for the number normal form, and as the precedent for a runtime
code where the RFC names none.

## Amendment: the batch tool, `jpack terraform facts` (2026-11-16)

RFC 0038 §6 gives the runtime a batch tool: one evaluation per resource, not one over the whole
plan. It is built as a command over the reader, and the reader's steps are unchanged.

```
jpack terraform facts (--plan FILE | --state FILE) --type T[,T...] [--mode managed|data]
  [--address PATTERN] [--actions a,b] [--max N] [--max-bytes N]
```

- **The flags are the source's members.** `--plan` or `--state` names the `document` and the file.
  `--type`, `--mode`, `--address`, `--actions` and `--max` fill `select`, and `--max-bytes` fills
  `maxBytes`, with RFC 0038 §1 and §4's forms: 1 to 64 distinct types, `--actions` with `--plan`
  only and from `create`, `update`, `delete`, `read` and `no-op`, `--max` from 1 to 10000
  (default 1000), and `--max-bytes` from 1 to 268435456 (default 67108864). A flag that breaks a
  rule is an unusable command line (exit 3), never a refusal of the file, because a malformed
  source makes the dialect invalid and not the file (§4). An empty `--address` or `--actions`
  would select nothing, so each is refused as a mistake rather than read as a source that selects
  nothing.
- **The file is read by the reader**, from a regular file or standard input (`-`). The reader
  holds it to `maxBytes` as it reads, and keeps nothing past it. The command, not the reader,
  decides which it has: `-` is read as a stream (`ReadStream`), to one byte past `maxBytes` at
  most, so a longer standard input is `TF-SIZE` with its code and fixed message alone (reading
  18).
- **One line per selected resource**, in the acquired value's order (by address):
  `{"facts":{"resource":<the §5 object>,"terraform":{"document","formatVersion","sha256"}},"id":"<address>"}`.
  This is RFC 8785 canonical, the line shape of `registry facts` (ADR-0095). `sha256` is the
  file's bare lower-case hex, as in the acquisition record.
- **A refusal writes no line.** Every check runs before the first line is written, so a refused
  run leaves standard output empty. On standard error it writes the refusal's canonical JSON alone: code, sha256, size and the code's
  fixed message. It never writes a value, a member name, a pointer or the path. The exit code is
  2, the "could not judge" that `registry facts` uses for an invalid registry (ADR-0092). A
  selection that matches nothing is an empty batch and exit 0, since "no database is destroyed"
  is a valid fact (§4). A file that cannot be opened or read, or a batch that cannot be written,
  exits 4. A write that fails partway is not a refusal: the lines written before it, the last
  perhaps cut short, stay on standard output. A pipeline uses the batch only after exit 0, as the
  example's `&&` does, or runs under `set -e` or `pipefail`.
- **Each facts document is held to the facts-document limit** of `registry facts`
  (`FACTS-DOCUMENT-LIMIT`: 10 MiB, depth 128, 250,000 nodes, 1 MiB per string), the limit evaluate
  reads a facts document under. One resource over it refuses the whole run. Its line is not
  skipped, because a batch that leaves a resource out would let the gate pass without judging it.
  Standard output stays empty, standard error carries the same four members as a refusal (the
  code, the file's sha256 and size, and the code's fixed message), and the exit code is 2, as
  `registry facts` uses for the same code.
- **Each address is held to the batch id limit.** A line's id is the resource's address, and
  `evaluate --batch` reads an id of 1 to 256 bytes (`MaxBatchIDBytes`), refusing a longer line
  with `JPS-INVOCATION-BATCH-ITEM`. A `for_each` key can make an address longer than that. Before
  anything is written, every selected address is checked, and one outside the bound refuses the
  whole run with `FACTS-ID-LIMIT`, this runtime's provisional code, ahead of the document check.
  The output is the same as for `FACTS-DOCUMENT-LIMIT`, with a fixed message, and it never carries
  the address. `registry facts` does not need the check: its longest id,
  `<environment>:<kind>/<entry-id>`, is 32 + 1 + 63 + 1 + 128 = 225 bytes under the registry reader's name
  bounds.
- **`*` never crosses `.`, `[` or `]`** (§4). So `module.*.google_sql_user.*` matches
  `module.db.google_sql_user.app` but not a counted module (`module.db[0]...`) or a counted
  instance (`...app[0]`). Those need `[*]`, as in `module.*[*].google_sql_user.*[*]`.

```
jpack terraform facts --plan plan.json --type google_sql_user --actions delete,update > changes.jsonl &&
  jpack experimental evaluate --pack-id sql-user-change --batch changes.jsonl --format json --require-outcome allowed
```

The pack reads `/resource/actions`, `/resource/before/...` and `/resource/after/...` for a plan,
and `/resource/values/...` for a state. A value marked sensitive, or known only after apply, is
absent, so a rule that reads it meets `unknown`.

**Not built:** the kind in `jpack dialect validate`, which waits on the dialect version the kind
joins (RFC 0038, Unresolved question 1).
