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
  nodes and 1 MiB per string.

**Generation (§4).** The schema is read into shapes:
- `allOf` and `$ref` narrow a shape, `anyOf` is the union of its branches, and `not` is ignored.
- `enum` and `const` keep the values every other keyword here allows.

So a contract may list a value no entry holds, and never omits one an entry can hold. Other choices:
- **Presence** is relative to the parent object. This matches every row of the RFC's example
  (`/value/password/store` is `always` under an `optional` parent). An array element is `optional`.
- **An array** lists `items` and `values` when its elements have one type, and lists its elements
  under `/*` as well.
- **An open position** has no `type`, no `additionalProperties` or no `items`. It is listed once
  per type, and nothing under it is listed.
- **A literal reference pattern** is keyed by its pointer, written as a token (`~1database`). It is
  `always` when every step to it is a required member of a position that can only be an object.
- **A wildcard reference pattern** is keyed `*`, and is optional.
- **A reference's `kind`** lists the kinds of the pattern that the registry pins.
- **A reference's `value`** is the union of the target kinds' mapped shapes and every position
  under them, since a reference with a `#pointer` carries any one of them.
- **`id` and `version`** are the kind's. `source.kind` binds the kind's digest. The contract also
  depends on the registry's environments and on the target kinds, which `source` does not name.
- **Bounds:** 8 MiB, 65,536 facts, and a bound on the generation's work.

**Reading a contract.** Admission works as for a registry document: 8 MiB, depth 32, the number
rules and the exact selector `factsContractVersion` `"1.0.0"`. Then the members are checked against
§1's list, with items in strictly increasing (pointer, type) order. In a contract pointer, `*` is
the wildcard and `~2` a literal `*`. A contract pointer has no 512-byte bound.

**The check (§5).** The pack's evaluation form is read through the evaluator's own admission. A
pack it refuses is reported as evaluate reports it. The check visits:
- every fact condition in `applicability`, `exceptions[].when` and `rules[].when`;
- through `not`, `all` and `any`.

Each condition gets at most one advisory, located at the condition. That position is the same in
the pack as written. The report names the contract and the pack by digest only. It exits 0 with no
diagnostic and 1 otherwise.

An operand is unreachable in these cases:
- a decimal operand outside the §2.2 grammar. Normal form is not required, so an authored contract
  is read as written;
- a JSON number, because facts hold decimals as strings;
- an object operand with a member the contract does not list, or without a member listed as
  `always`.

An object with nothing listed under it may hold anything.

**Codes this runtime adds**, where the RFC names none:
- `FACTS-DOCUMENT-LIMIT`;
- `FACTS-CONTRACT-LIMIT`;
- `FACTS-CONTRACT-FORM`;
- `FACTS-CONTRACT-ORDER`.

The check report's own selector is `reportFormatVersion` `"1.0.0"`.

**Not built:**
- the project declaration of a pack's contract (RFC 0037, Unresolved question 1). The contract is
  given on the command line.
- §6's builders.

### Consequences

- Good: a CI step can judge every entry of a kind with the outcome gate, and a pack that reads a
  member a kind no longer has fails its check before it runs.
- Bad: a kind whose schema leaves positions open gets a contract that says little under them, and
  a pack reading there is told the pointer is unknown.
- Neutral: the choices above are this runtime's reading. If the registry companion settles them
  differently, this runtime follows it under a new selector.

## More information

Spec RFC 0037 (accepted 2026-11-16), §1 to §5; §6 waits for RFC 0036's emitter host.
`internal/registry/facts.go`, `facts_contract.go`, `facts_check.go`, `internal/cli/registry_facts.go`.
