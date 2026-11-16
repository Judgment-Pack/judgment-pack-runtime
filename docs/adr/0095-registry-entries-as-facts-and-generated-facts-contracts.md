---
status: accepted
date: 2026-11-16
deciders: maintainer
---

# Registry entries as facts, generated facts contracts, and a pack check

## Context and problem statement

The specification accepted RFC 0037 with RFCs 0034 to 0036 and 0038. It connects a registry
(ADR-0093) to packs through one document, the facts contract:

- §2 maps a resolved entry to a Core facts document;
- §3 writes a kind's entries as a batch for the outcome gate (ADR-0092);
- §4 generates a contract from a kind;
- §5 checks a pack against a contract;
- §6 adds typed builders through an emitter.

The companion text (`spec/registry.md`) and its vectors are not released yet. This runtime builds
§1 to §5. §6 depends on RFC 0036's emitter host, which this runtime has not built.

Material-decision impact: public-surface.

## Decision drivers

- A batch that `experimental evaluate --batch ... --require-outcome` reads unchanged.
- No secret reference in a facts document, and no value in a report.
- Fail closed: either every line is written, or none is.
- Deterministic bytes: RFC 8785 for facts documents, batch lines and contracts.
- No existing bytes or behaviour change.

## Considered options

- Two subcommands of `jpack registry`: `facts` and `check-pack`.
- A third subcommand that only generates a contract.
- The check as part of `experimental evaluate`.

## Decision outcome

Chosen option: "two subcommands of `jpack registry`". `facts --contract FILE` generates the
contract, as RFC 0037 §3 says, so a third command would add nothing. The check reads no facts, so it
does not belong to evaluate.

**`jpack registry facts`** takes the flags RFC 0037 §3 writes:
- `--registry`;
- `--kinds`, a file or a directory whose `*.json` files are read in name order, not recursively;
- `--kind`, a kind's name;
- exactly one of `--environment` or `--all-environments`.

This differs from `validate` and `resolve`, which take the registry as a positional argument and
`--kind FILE`.

Its exit codes:
- 0: the batch was written, an empty one included. The gate itself refuses an empty batch.
- 2: nothing was judged. The RFC gives this code to an invalid registry. An entry over the
  facts-document limit, and a contract over its bounds, exit 2 too. The report goes to standard
  error, so standard output holds batch lines only.
- 3: the command line is unusable. This includes a kind the registry does not pin and an
  environment it does not declare.
- 4: an input could not be read, or an output written.

Every facts document is built and checked before anything is written. The contract file comes next,
through a temporary file and a rename. The lines come last. A contract that cannot be written
leaves standard output empty.

**Mapping (§2):**
- A number is written in normal form from its literal's exact value. No double is involved.
- A secret reference becomes `{"store", "scope"}`.
- `references` is always present, keyed by the pointer of each referring position. The target's
  value is mapped by its own kind's secrets, and its references are left as strings.
- The facts-document limit is evaluate's: 10 MiB, and the carrier limits of depth 128, 250,000
  nodes and 1 MiB per string. A document's size is counted as it is built: its value's encoded
  size, then each reference target's encoded size, once per reference. When that sum passes 10
  MiB, the entry is reported before the document is encoded. A target's size is encoded once per
  target and pointer, so an entry that refers many times to one large entry costs no more than the
  limit.

**Generation (§4).** The schema is read into shapes:
- `allOf` and `$ref` narrow a shape, `anyOf` is the union of its branches, and `not` is ignored.
- `enum` and `const` keep the values every other keyword here allows.

So a contract may list a value no entry holds, and never omits one an entry can hold. Other choices:
- **Presence** is relative to the parent object. This matches every row of the RFC's example
  (`/value/password/store` is `always` under an `optional` parent). An array element is `optional`.
- **An array** lists `items` and `values` when its elements have one type, and lists its elements
  under `/*` as well.
- **An open position** has no `type`, no `additionalProperties` or no `items`, or is a member
  declared `{}`. It is listed once per type, and again once per type at `<position>/*`. Nothing
  deeper is listed, and rows are not expanded to any depth bound. A pointer below an open
  position is listed (ruling on the review of this change, reading 5 of §1): the check reads every
  pointer below such a `*` row as listed, with every type.
- **Depth.** An entry's value nests no deeper than the registry document that holds it (32). The
  deepest a value sits in a facts document is `/references/<pointer>/value`, three tokens down. So
  a contract lists no position deeper than 35 tokens, whatever depth the schema reaches.
- **A literal reference pattern** is keyed by its pointer, written as a token (`~1database`). It is
  `always` when every step to it is a required member of a position that can only be an object.
- **A wildcard reference pattern** is keyed `*`, and is optional.
- **A reference's `kind`** lists the kinds of the pattern that the registry pins.
- **A reference's `value`** is the union of the target kinds' mapped shapes and every position
  under them, since a reference with a `#pointer` carries any one of them.
- **`id` and `version`** are the kind's. `source.kind` binds the kind's digest. The contract also
  depends on the registry's environments and on the target kinds, which `source` does not name.
- **Bounds:** 8 MiB, 65,536 facts, and a bound on the generation's work. Each fact is charged its
  pointer and the shortest item's other bytes as it is listed. Generation stops once that sum
  passes 8 MiB, before the contract is encoded.
- **A target that is open anywhere** widens `/references/<p>/value` to every type, since a
  reference with a `#pointer` may carry any position of it. This runtime keeps that widening. The
  registry companion text is to settle whether a reference pattern declares that a `#pointer` is
  allowed, so that the target's exact shape can be generated when it is not. That text is pending,
  in a spec issue, and this runtime follows it under a new selector when it is released.

**Reading a contract.** Admission works as for a registry document: 8 MiB, depth 32, the number
rules and the exact selector `factsContractVersion` `"1.0.0"`. Then the members are checked against
§1's list, with items in strictly increasing (pointer, type) order. In a contract pointer, `*` is
the wildcard and `~2` a literal `*`. A contract pointer has no 512-byte bound. Its bytes are
bounded by the contract's 8 MiB. Its depth is bounded at 35 tokens, the deepest position a facts
document holds (see **Depth**), so the bound loses no position a contract could list. The tokens
are counted by a scan that stops past the bound, before the pointer is split, and a pointer deeper
than 35 tokens is refused as `FACTS-CONTRACT-LIMIT` at its item's `pointer`. Without the scan, an
8 MB contract whose one pointer had 4 million tokens allocated about 1.8 GB while it was read.

**The check (§5).** The pack's evaluation form is read through the evaluator's own admission. A
pack it refuses is reported as evaluate reports it. The check visits:
- every fact condition in `applicability`, `exceptions[].when` and `rules[].when`;
- through `not`, `all` and `any`.

Each condition gets at most one advisory, located at the condition. That position is the same in
the pack as written. The report names the contract and the pack by digest only. It exits 0 with no
diagnostic and 1 otherwise.

**Reading a pointer.** A pointer's view is the set of contract positions it matches. Each step
follows the literal child and the `*`, and a `*` row listed once for every type, with no `values`
or `items` and nothing under it, matches every pointer below it. That is an open position. Each
pointer is looked up once per check. One check has one work bound: 4,194,304 contract positions
and facts visited, which also bounds the lookups it keeps. A fact pointer, operand included, is
read no deeper than a facts document can nest (128); its tokens are counted by a scan before it is
split, so a deeper pointer costs its bytes, not its tokens. A malformed pointer is still
`FACTS-POINTER-UNKNOWN`, whatever its depth. A check that passes either bound reports
`FACTS-CHECK-LIMIT`, an error, at the condition where it stopped, and checks no condition after
it.

An operand is unreachable in these cases:
- a decimal operand outside the §2.2 grammar. Normal form is not required, so an authored contract
  is read as written;
- a JSON number, because facts hold decimals as strings;
- an object operand with a member the contract does not list, or without a member listed as
  `always`.

`FACTS-VALUE-UNREACHABLE` is reported for an `in` when at least one item is unreachable.

An object with nothing listed under it may hold anything.

**Codes this runtime adds**, where the RFC names none:
- `FACTS-DOCUMENT-LIMIT`;
- `FACTS-CONTRACT-LIMIT`;
- `FACTS-CONTRACT-FORM`;
- `FACTS-CONTRACT-ORDER`;
- `FACTS-CHECK-LIMIT`.

**Selectors.** The check report has its own selector, `factsCheckFormatVersion` `"1.0.0"`. It is
not the validation report's `reportFormatVersion`. RFC 0037 gives facts documents and batch lines
no selector. They are bytes mapped from a resolved registry, so a change to them for the same
inputs moves `resolvedFormatVersion`.

**Not built:**
- the project declaration of a pack's contract (RFC 0037, Unresolved question 1). The contract is
  given on the command line.
- §6's builders.

### Consequences

- Good: a CI step can judge every entry of a kind with the outcome gate, and a pack that reads a
  member a kind no longer has fails its check before it runs.
- Bad: a kind whose schema leaves positions open gets a contract that says little under them. A
  pack reading there is not told when it reads a member no entry has. A schema that allows every
  type at a `*` position but closes its objects and arrays is read as open, so the check lists
  more there than the schema allows. That costs an advisory, never an evaluation.
- Neutral: the choices above are this runtime's reading. If the registry companion settles them
  differently, this runtime follows it under a new selector.

## More information

Spec RFC 0037 (accepted 2026-11-16), §1 to §5; §6 waits for RFC 0036's emitter host.
`internal/registry/facts.go`, `facts_contract.go`, `facts_check.go`, `internal/cli/registry_facts.go`.

## Amendment: bounds from the v0.47.0 release review (2026-11-16, #432)

- **The generation's work bound covers the reading of the schema.** A contract is generated from
  every schema that applies at a position. Collecting them follows `$ref` and `allOf`, so a schema
  whose definitions name each other many times collected each one once per path. Each node is now
  collected once, however many paths reach it. Each node collected, each closed-list value checked
  against a schema, and each value merged into a union is charged to the work bound, 2^20, that
  already held the shapes. Past it the kind gets `FACTS-CONTRACT-LIMIT`. Each value is checked
  against each schema's own keywords once, rather than validated through every `$ref` path again.
  The collected schemas hold every node that applies, so the result is the same.
- **A contract's report is bounded as validate's is** (ADR-0093, amendment of #427). `check-pack`
  and every reader of a contract report at most the first 1000 diagnostics in the report's order,
  then one `REG-REPORT-TRUNCATED` warning located at `{"document":"contract"}`. Before, an 8 MiB
  contract of `-0`s produced millions of diagnostics. The contract is refused when any error was
  found, kept or not. A contract's listed values are checked for repeats in one pass.
- **A number with no normal decimal is an error.** The facts mapping already refused one. A
  contract's closed list gave it an open decimal, which widened the list past it. Now the contract
  is not generated, an internal error. Admission keeps every number as a literal of at most 25
  bytes (ADR-0093, amendment of #432), so an admitted kind holds no such number.
- **Not bounded here:** a pack's advisories have no count bound of their own. Each is one fact
  condition of the pack, which the evaluator's admission already bounds (250,000 nodes by
  default), and the check's work bound holds what one check reads.
