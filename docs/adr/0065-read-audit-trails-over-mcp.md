---
status: proposed
date: 2026-10-23
deciders: maintainer
---

# Read exact audit bytes and structured history through bounded MCP tools

## Context and problem statement

Clients currently parse the Runtime's retained audit files and invoke CLI
verification. A client without the server's filesystem needs both the complete
record members for history and the exact retained bytes for downloads. Reencoding
objects changes record whitespace, line digests and malformed retained content.

## Decision drivers

- Keep one verifier and the existing configured-project filesystem authority.
- Export every retained byte without making malformed data a successful record.
- Provide structured history across byte-page boundaries without client JSONL parsing.
- Bound memory, filesystem work, retained sessions, arguments and serialized output.
- Preserve the CLI and avoid a new signed format or conformance claim.

## Considered options

- Return decoded objects alone, losing byte custody and malformed retained data.
- Accept arbitrary server filesystem paths, expanding caller authority.
- Page exact prefixes of the three configured files with structured line results.

## Decision outcome

Use the third option through `experimental_list_audit` and
`experimental_verify_audit`, as detailed in the
[MCP reference](../mcp-clients.md#experimental-audit-reading-and-verification).
This is a Runtime convention, independent of every Core version.

Listing visits `evaluations.jsonl`, `signatures.jsonl` and `stamps.jsonl` in that
order. Base64 chunks preserve exact bytes, including whitespace, CR/LF,
malformed UTF-8/JSON, oversized lines and incomplete final bytes. Every completed
line has a status, offset, byte count and SHA-256 excluding only LF; supported
bounded objects also carry all members and separate exact raw-line bytes.
Incomplete final bytes carry `incomplete` and are never a completed decision.
Invalid UTF-8 and unpaired Unicode surrogate escapes are `malformed`, retaining
raw bytes without emitting repaired or unsupported structured string values.
Line state spans raw pages, so ordinary records crossing a byte boundary remain
available to structured history. No record identity is deduplicated or filtered.

Each export captures fixed sizes, file identities and 64 KiB block hashes. It
streams one initial pass, reads and verifies each served block into one bounded
cache, and rechecks all blocks before successful completion. This is linear
filesystem work, including for small pages. Presence, identity and minimum size
are checked on every advancing call. Appends are excluded. Changes to already
served blocks may be detected only at completion; intermediate pages are
provisional, and clients must discard an export that never finishes successfully.
This is integrity detection for retained prefixes, not an atomic transaction
against noncooperating writers. The between-writes flag retains the existing
cooperative trail/sidecar size-capture meaning; stamps use their independent lock.

A per-server pager serializes advancement, retains at most eight exports for
five minutes, and keeps the latest answered cursor's response for idempotent
retry, including the final response. Older cursors are refused. Completed exports
may be evicted to admit a new export, and no cursor survives a server restart.
No file handles or whole-file buffers survive a call. Bounds are explicit:
64 MiB per captured file; 256 KiB raw bytes and 128 line results per page;
64 KiB per decoded/raw-line result. Longer valid records are `oversized`, with
their bytes still transported through raw chunks. History parity is bounded to
supported parsed lines: Desk's existing local reader has a 4 MiB read window,
so a larger valid line may be visible locally but only raw-downloadable here.
The structured Unicode rule is stricter than Go's historical surrogate
replacement behavior. Neither limitation changes exact downloads.
A 4 MiB serialized MCP result
limit counts the text and structured representations, base64 and JSON escaping;
amplified results are refused whole and can be retried as a new export with a
smaller raw-page size. The existing 16 MiB request-line limit also applies.

Verification uses `audit.Verify` and `audit.ParseCheckpoints`, exactly as the CLI
does. `expect` supplies checkpoint JSONL, bounded to 1 MiB;
`require_checkpoint_through` has CLI semantics. `trail_base64` supplies at most
8 MiB of exact bytes as the content equivalent of CLI `--trail`; it never names
a host path. Without it, the guarded configured trail is read up to 64 MiB.
Both modes require a present, loadable project declaring a contained audit
directory. Reports preserve every verifier field; only source metadata differs:
the MCP command name, a configured relative `trailPath` or `(supplied trail)`,
and `snapshotBetweenWrites:false` for supplied memory bytes. No public keys,
trust roots or witness inputs are supplied by this surface; protections remain
explicitly unchecked. No signing key is opened, signing settings supply no
verification trust, and no timestamp is fetched.

### Consequences

- Good: clients can reproduce downloads byte for byte and select supported
  bounded history lines from structured members and exact digests without
  parsing retained JSONL.
- Good: root-relative openers retain containment, final-symlink and regular-file
  guards; no caller path, repair, write or key-management scope is added.
- Bad: large files/records have explicit experimental limits, and clients must
  finish an export before treating it as complete.
- Revisit when larger exports, more retry history, or public-key/trust-root
  verification inputs are required. Those need separate bounded contracts.

## More information

Issue [#308](https://github.com/Judgment-Pack/judgment-pack-runtime/issues/308).
This extends ADR-0047's verification surfaces; its cryptographic formats,
verifier semantics and CLI remain unchanged. Runtime
contract rows and focused parity tests are evidence of this wire convention,
not a new JPS conformance claim. Material-decision categories: public-surface,
security. Required review remains a separate integration gate.
