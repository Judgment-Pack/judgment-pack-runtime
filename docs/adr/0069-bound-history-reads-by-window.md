---
status: proposed
date: 2026-10-23
deciders:
---

# Bound history reads by window independently of source size

## Context and problem statement

The experimental history tool reads and rechecks only a backward 4 MiB window,
but ADR-0065 also applies the raw export's 64 MiB source limit to history.
That rejects safely addressable large configured trails without reducing
history's source reads or retained projection state. Raw export and verification
perform different, source-proportional work and need separate limits.

Material-decision impact: public-surface, documented-claim, security.

## Decision drivers

- Admit larger configured history sources with finite, exact logical offsets.
- Preserve bounded selection work, guarded authority and immutable transport.
- Keep larger raw downloads and verification as separate issue-308 work.

## Considered options

- Keep history's shared 64 MiB source gate.
- Remove every source ceiling.
- Bound history source addresses separately while retaining the existing window.

## Decision outcome

Choose a dedicated int64 history source/offset ceiling of 9007199254740990
bytes (2^53−2), the existing MCP `before` maximum. This partially supersedes
only ADR-0065's history 64 MiB source-limit determination. ADR-0065's remaining
selection, window, projection, transport, authority and snapshot determinations
remain in force; its accepted text stays unchanged.

A shared range validator in direct history selection and captured history
rejects negative source sizes, sources above the ceiling, invalid direct
`before` values below the omitted-value sentinel −1, and offsets beyond the
captured size before allocating or reading. MCP continues to accept only
nonnegative explicit `before` values. Oversized sources refuse with a finite
path-free history-specific message even if `before` is smaller. The ceiling
keeps `sourceBytes` and every logical `next` exact for JSON-number clients;
it does not guarantee that any host filesystem supports files that large.

Selection still reads at most 4 MiB and capture rechecks that same section.
No prefix scan or whole-source hash table is introduced. Source size does not
increase transient selection work, retained projection bytes or transport work.
The source buffer limit is not a total-memory limit: splitting and typed
decoding also allocate bounded transient data. Projections retain the existing
8 MiB plus 64 KiB ceiling, 256 KiB decoded page ceiling, 4 MiB serialized result
ceiling, eight shared slots and five-minute expiry.

Guarded configured opens, identity/minimum-size rechecks and cooperative size
capture remain mandatory. Window rechecking does not promise an atomic snapshot
against arbitrary noncooperating writers. Continuations transport an immutable
captured projection and bind current configured authority; a fresh logical
`before` query captures a new size and window. Completion means that projection
was fully transported, not that all source records were read.

### Consequences

- History can select records from configured sources above 64 MiB without
  increasing its existing read/recheck window.
- Raw export and configured verification remain limited to 64 MiB sources.
  Supplied verification trails and expectations remain capped at 8 MiB and
  1 MiB. Their larger-source work and staged-input contracts remain outstanding.
- This history-only increment is not unrestricted issue-308 closure and adds no
  Core conformance claim.
- Hand-authored sparse MCP windows at 64 MiB and 64 MiB+1 verify exact selection,
  source sizes and transport. An offset-aware synthetic ReaderAt verifies the
  exact ceiling, high logical next offsets, pre-read refusals and bounded reads
  without requiring a huge filesystem object. Existing snapshot, authority,
  capacity and selection tests remain applicable.
- Material review follows the different-vendor regime in the ADR index; this
  proposed record does not waive review or release gates.

## More information

- [ADR-0065](0065-read-audit-trails-over-mcp.md)
- [MCP client history contract](../mcp-clients.md#server-side-definition-history)
