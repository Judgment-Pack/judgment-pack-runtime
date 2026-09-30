---
status: accepted
date: 2026-09-30
deciders: maintainer
---

# Record the executable's digest on every audit record

## Context and problem statement

`docs/building-with-packs.md` ("Replaying a decision: pin the tuple, not the pack") names the unit of
replay: the pack's digest, the evaluator release, and the SHA-256 of the executable that ran, recorded
together. It then says the audit trail (ADR-0018) writes two of the three on every record, and that a
project "needs to add only the executable digest, which lives wherever the binary is staged and
verified". The same section explains why keeping the third fact somewhere else is the risk: "nothing
checks that the pair you eventually replay is the pair that ran". A version string names a release;
it does not say which bytes ran (issue #181).

## Decision drivers

- The record should carry the whole tuple its own guidance asks for.
- A record stays valid where the fact cannot be established.
- Nothing is paid by a run that records nothing.
- The record claims no more than the runtime can know about itself.

## Considered options

- **A. An optional `digest` in the record's `tool` member**, computed by the runtime from its own
  executable.
- **B. A digest supplied by the caller or the project**, for example in `jpack.json`. It is the fact
  stated twice again: nothing checks that the configured digest is the binary that ran.
- **C. Leave it to the project**, as today. The guidance then keeps describing a gap it tells every
  project to close by hand.

## Decision outcome

Chosen option: **A**.

1. **The member.** A record's `tool` carries `digest`: `"sha256:"` and the lowercase hexadecimal
   SHA-256 of the executable's bytes, the form every other digest on the record takes. It is
   present on every record kind and every recording surface, because every record is stamped in one
   place. An evaluation record then carries the whole replay tuple. A graph composite carries no
   pack, as before; its node records carry the packs, and replaying a graph run reads both.
2. **Where the bytes come from.** On Linux the runtime reads `/proc/self/exe`, which names the running
   file even when its path has since been replaced, as an upgrade under a running MCP server does.
   If that cannot be opened there is no fallback: the path `os.Executable` reports may by then name
   another file, and a digest of it would name bytes that did not run. Elsewhere the runtime reads
   the path `os.Executable` reports, which is the running program unless that path was replaced or
   retargeted after the process started; the digest then names what is at the path when the first
   record is composed. That is a limit of those platforms, stated here rather than hidden.
3. **When.** Once per process, when the first record is composed. An invocation that composes no
   record never reads its own executable, and a long-lived server hashes itself once. A graph run
   composes node records as its nodes complete, so a graph run refused after its first node has
   read the executable though it writes nothing.
4. **When it cannot.** Where the executable cannot be opened or read whole, `digest` is omitted and
   the record is otherwise whole. A file whose size or modification time changed while it was read,
   or that yielded a different number of bytes than its size, counts as not read whole. An absent
   member says nothing was established; an empty string or a digest of part of a file would read as
   a digest.
5. **What it is.** The running program's account of itself: evidence of which build ran, not proof.
   A modified binary can report any digest it likes, as it can report any version. It is compared by
   nobody inside this runtime.
6. **Versioning.** The member is additive, so `recordVersion` stays `"1"` under VERSIONING.md's rule
   for record formats, as it did for `reviewed` (ADR-0019) and `cites` (ADR-0033). Payloads are
   unchanged: their `tool` member does not gain it.

### Consequences

- Good, because an evaluation record now carries the whole replay tuple, and the guidance can say
  so.
- Good, because the fact is written by the process that ran, at the moment it ran, beside the other
  two.
- Bad, because the first record of each process costs one read of the executable, in time
  proportional to its size (a third of a second for a 256 MiB file, measured in review).
- Bad, because outside Linux the digest can name a replaced file rather than the running one.
- Neutral, because a build run from `go run` or a test binary names that binary, which is what ran.
- Revisit if the runtime is ever distributed in a form where the running bytes are not one file.

## More information

Issue #181. ADR-0018 (the audit trail), ADR-0019 and ADR-0033 (additive record members),
`docs/building-with-packs.md` (the replay tuple), VERSIONING.md (record formats).
