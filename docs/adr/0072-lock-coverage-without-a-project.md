---
status: proposed
date: 2026-11-10
deciders: maintainer
---

# Answer whether a reviewed-set lock covers a document's exact bytes, without opening a project

## Context and problem statement

[ADR-0019](0019-reviewed-set-lock.md) gives a project a reviewed-set lock, and `packs verify`
compares the project's tree with it. A host that runs a pack outside the project, such as a runner
that copied the pack before a run, holds bytes that are no longer in that tree. It needs to know
whether the lock pins those exact bytes, and the runtime gives it no way to ask (#364). Without one,
each host would have to parse `jpack.lock.json` itself: a second reader of a format whose strict
decode, version gate and digest rules live in `internal/lock`. Two readers can disagree about one
file.

Material-decision impact: public-surface, documented-claim.

## Decision drivers

- One reader of the lock format: a lock the deciding surfaces refuse is refused here with the same
  code, exit and words, and a lock they accept decodes to the same set.
- Read-only: nothing is evaluated, written or recorded, and no project is opened unless the caller
  names its configuration.
- A file that cannot be read is a refusal, never "not covered" and never "covered".
- Where the condition is the one `packs verify` reports, the finding has its name and its words.

## Considered options

- A new verb, `jpack packs covers --lock FILE --id ID --pack FILE [--config FILE]`, with
  `--graph-id ID --graph FILE` for a graph.
- A form of `packs verify` that takes `--lock` and `--pack`.
- Leave it to each host to read the lock and compare digests itself.

## Decision outcome

Chosen option: a new verb, `packs covers`, because `packs verify` selects a project
(`--config`, then `JPACK_CONFIG`, then `./jpack.json`), finds the lock beside it by convention and
re-reads every declared document. A form of it whose `--config` meant something else would open a
project whenever a flag was left off, and the two answers have different payloads. Leaving it to
hosts was rejected for the first driver.

`lock.Decode(lockPath, data)` holds the decode `lock.Open` applied: the carrier's strict JSON
decode (one value, no duplicate member), the `lockVersion` gate before the shape, and the
validation of every digest and path. `lock.Open` now reads the project's lock and calls it, so the
two cannot drift. Before the shape is decoded, `lock.Decode` also refuses a member it does not
know, at the root, in the configuration entry or in any pack or graph entry, including case and
Unicode-fold variants of the known names (`JPS-LOCK-SHAPE`, exit 1). The carrier decode refuses
only a name given twice exactly, and `encoding/json` matches member names without regard to case
and keeps the last match, so such a variant would otherwise be read in place of the exact member.
The known names are the json names of the types `packs lock` encodes, which have not changed since
the lock was introduced, so every lock it has written still decodes. `packs covers` reads the lock named by `--lock` within `project.MaxLockBytes`
and the document within the limit a project reads a declared one within
(`carrier.HardMaxBytes` for a pack, `graph.MaxGraphBytes` for a graph). Each is read as one bounded
regular file and never through a final symbolic link. The document's bytes are not parsed:
coverage is a property of bytes. `lock.Covers` compares their SHA-256 with the entry for the id
in the lock's packs, or its graphs for a graph, and reports `lock-entry-missing` or
`document-drift`.

`--config` is loaded with `project.Load`, the one reader of `jpack.json`, and never located. Nothing
it declares is read. It adds `config-drift` when its bytes are not the configuration the lock pins,
`path-mismatch` when the path it declares for the id is not the entry's path, and
`locked-but-undeclared` when it does not declare the id. For a project's own lock and one of its
documents these are the findings `packs verify` gives for that document.

The answer is an `outputVersion "2"` envelope with `status` `valid` or `invalid`, the lock's
digest and version, the configuration digest the lock pins, the id, the document's digest, the
entry when the lock has one, and the findings, each with its detail. Exit 0 means covered and 1 not
covered. A refusal uses the code its class already has: 3 for an invocation, 4 for a file that
cannot be read or is past its limit, 2 for a `lockVersion` this runtime does not read, and for a
lock that is not the shape `packs lock` writes, the exit `packs verify` gives it, which is 1.
Under `--config`, a configuration refusal keeps the code every `--config` command gives it: 4 for a
file that cannot be read or is past its limit, 3 for one that is not acceptable JSON, 1 for one that
does not satisfy its schema, and 2 for a `configVersion` this runtime does not read. Exit 2 carries
`status: "unsupported"`; every other refusal carries `status: "error"`.

There is no MCP tool. `packs lock` and `packs verify` have none,
[ADR-0003](0003-mcp-integration-and-testing-surface.md) sets no rule that every command has one, and a caller
limited to MCP tools holds no file of its own to name.

### Consequences

- Good, because a host asks the runtime instead of reading the format itself, and a change to the
  format reaches every reader at once.
- Good, because the answer depends only on the files named, so it holds in a directory whose own
  `jpack.json` or lock is broken.
- Bad, because exit 1 means both "not covered" and "the lock is malformed", as it does on
  `packs verify`. A host that needs to know which reads `status`: `invalid` with findings, or
  `error` with a diagnostic. Either way the bytes were not shown to be covered.
- Bad, because the answer is about the bytes read. A host that runs another copy, or the same path
  after it changed, has to keep the bytes it checked.
- Bad, because the caller chooses the lock. `packs covers` says whether that lock pins these bytes,
  not whether it is the lock a given project holds. `--config` says whether that configuration is
  the one the lock pins.
- Revisit when a host needs many ids in one run, or a caller limited to MCP tools needs the answer.

## More information

Code: `internal/lock` (`Decode`, `Covers`), `internal/cli/packs.go` (`packs covers`) and
`internal/result` (`PackLockCoverage`). README, "The reviewed set", and the guide's "Declare the
reviewed set".
