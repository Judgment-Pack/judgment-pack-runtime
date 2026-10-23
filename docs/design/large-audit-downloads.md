# Larger raw audit downloads: bounded continuation proposal

**Status: PROPOSED / NOT IMPLEMENTED.** This is a design candidate for Runtime
[#308](https://github.com/Judgment-Pack/judgment-pack-runtime/issues/308), based
on `b51646e57d207f5b733d25fde68f26d4d2d2fdb5`. It is not an accepted ADR, an
adopted public contract, a release promise or a Core conformance claim. Tool
names, limits and response members below describe the candidate only. Adoption
requires a material-decision review and an implementation PR with an ADR; this
document supplies neither approval nor a substitute for that review.

## Existing contract and the remaining problem

[ADR-0065](../adr/0065-read-audit-trails-over-mcp.md) and the
[MCP client reference](../mcp-clients.md#listing-and-exact-downloads) provide
byte-exact export of three configured files, with a 64 MiB ceiling **per file**.
`internal/audit/wire.go` streams a preparation pass into 64 KiB block hashes and
per-file SHA-256 digests, verifies blocks while serving, and rechecks all blocks
before completion. It retains a bounded cache rather than entire source files.
However, preparation and final checking each run inside one call, and all pager
advancement currently holds one mutex. Raising only the size ceiling would
increase per-call I/O, retained hash tables and time spent blocking other work.

[ADR-0069](../adr/0069-bound-history-reads-by-window.md) independently admits
history sources up to 9007199254740990 bytes while retaining a 4 MiB backward
read/recheck window. History transports an immutable encoded selection. It does
not download every source byte or make large raw downloads available. Configured
verification remains capped at 64 MiB, supplied trails at 8 MiB and supplied
checkpoint expectations at 1 MiB. None of those limits changes under this
proposal. A downloaded file can also exceed the existing supplied verifier's
budget; downloading it does not imply that MCP can verify it.

The next bounded increment should export sources above 64 MiB with finite work
per continuation and finite work and state per session. It should preserve
rooted configured authority, exact retained bytes, append exclusion, latest-page
retry and honest snapshot language. Unrestricted source sizes, durable jobs,
caller paths, keys, network access and application-level audit writes are outside
this increment.

## Options and recommendation

| Option | Compatibility and cost | Assessment |
| --- | --- | --- |
| Raise the current raw ceiling alone | Small code change, but the first and last calls still scan entire sources under the pager mutex; hash state grows with the ceiling. | Reject: it does not bound the newly enlarged work per call. |
| Add an opt-in phased mode to `experimental_list_audit` | Can reuse the current block engine and advisory line projection, but old clients expect every successful response to contain a byte page. New phase-only replies require a new negotiated mode and a separate wire shape. | Feasible, but couples large raw custody to structured-line state and response amplification. |
| Add a separate raw-only phased tool using bounded block commitments | Reuses guarded openers and block verification, adds no source-sized spool, and keeps the current listing and history shapes intact. Hash tables still require a finite source ceiling. | Recommended for the next increment, subject to the limits and gates below. |
| Copy into a private disk spool, then serve the copy | Makes completed copy bytes independent of later source changes and removes serving-time source dependence. Adds sensitive temporary writes, disk quotas, secure creation/cleanup, crash recovery and a changed read-only operational posture. Copying alone still does not make capture atomic. | Defer to a separate decision if independence from later source changes or larger sizes is required. |
| Hold cooperative locks for the whole multi-call session | Could exclude cooperative changes for a long interval; cannot stop arbitrary writers. Requires persistent handles, stalls writers during client delays and complicates expiry/cancellation. Stamps still have a separate lock domain. | Reject for this increment. |

Recommend the third option, provisionally named
`experimental_download_audit`, with `auditDownloadVersion:"1"`. It exports raw
bytes only. Existing `experimental_list_audit` with `auditWireVersion:"1"`,
history and verification keep their current arguments, responses, ceilings and
semantics. A client discovers the new tool and deliberately selects it; the
server never changes a listing response into a preparation response or silently
routes an oversized listing request to a different protocol.

The recommended contract remains provisional integrity detection for captured
prefixes. It does not promise immutable source files, continuous monitoring or a
point-in-time snapshot against arbitrary writers. If those are acceptance
requirements, this option is unsuitable and a snapshot/spool decision must come
first.

## Candidate budgets

MiB and GiB below are binary units. Limits apply before allocation or source
reads whenever size metadata permits. Negative sizes, arithmetic overflow and
out-of-range offsets refuse. All public numbers fit exactly in JSON integers.

| Resource | Proposed bound |
| --- | --- |
| Captured source | 1 GiB per file; at most 3 GiB across the fixed three files |
| Hash block | 64 KiB; a final short block uses only its captured bytes |
| Preparation or finalization advancement | At most 8 MiB of source reads / 128 blocks per call, across all files |
| Transport page | `page_bytes` integer 65536–262144, default 262144; one fixed file per page |
| Serving reads | At most five 64 KiB blocks (320 KiB) per call, including an unaligned first block; retain one block cache between calls |
| Whole serialized MCP result | Existing 4 MiB ceiling, counting text/structured duplication, base64 and JSON escaping |
| Request line | Existing 16 MiB ceiling; strict argument parsing and bounded token/ID lengths still apply |
| Live large-download sessions | Two per server, also consuming slots from the existing shared eight-slot audit pool |
| Lifetime | Five minutes without an advancing commit; absolute thirty minutes from creation; replay does not renew either deadline |
| Advancing calls | At most 65536 per session, including start and terminal transitions |
| Answer replays | At most 1024 per session, including duplicate starts; exhaustion invalidates that session |
| Retained payload accounting | At most 8 MiB per large session, including hash arrays, cache, initial reply and latest reply; also a fixed bound on metadata and hash-state objects |
| Transient work payload | At most 16 MiB additional accounted payload for the one admitted advancing call, including candidate state, output and encoding buffers |
| Concurrent source work for this tool | One admitted advancing call per server; another call gets a finite busy refusal without entering an unbounded queue |

At maximum size there are 49152 block hashes: 1572864 bytes (1.5 MiB) per
session, at most 3 MiB across the two large sessions. Store them in exactly sized
arrays, avoiding append-capacity growth. Keep at most one 64 KiB cache, three
small SHA-256 states, fixed three-file metadata, one initial control reply and
one latest reply per session. Every control reply has a 16 KiB serialized
ceiling; a data reply has the 4 MiB ceiling. The 8 MiB
accounting bound must count any retained decoded/encoded duplicate rather than
assuming the serialized size is the whole object graph. A refusal releases the
candidate state if accounting would exceed the cap. This is a retained-payload
bound, not an exact Go heap/RSS ceiling: allocator, object, transport and GC
overhead require measurement. Enforce the additional 16 MiB transient accounting
cap with bounded buffers and output-size preflight or a limited encoder;
marshalling an arbitrarily large object and checking only afterwards is not a
memory bound. The raw-only shape, fixed metadata and bounded input strings make
such preflight possible without parsing source JSON.
Do not advertise a total-process memory ceiling from this arithmetic. Existing
history states retain their separate bounded projections in the shared pool.

For source total `S`, a successful uninterrupted session reads at most `3*S`
source bytes: preparation, serving and final recheck. With one sequential cache,
serving reads each block once even for small pages. Replays perform no source
reads. The maximum successful source work is 9 GiB; no hidden full rehash occurs
on a phase transition. Initial and per-call presence/identity checks are a
separate bounded metadata cost. With full work quanta, preparation and final
checking each need at most 384 calls, and transport at most 49155 calls including
zero-byte file pages. Start/terminal control steps then fit within the 65536-call
limit. Cooperative deadline yields can require more calls; the hard advancing
cap still applies and may terminate an unusually slow session before completion.
The 64 KiB transport minimum prevents a three-billion-call one-byte export. Together
with the replay cap, the 4 MiB response limit yields a conservative finite
260 GiB allowance for successful advancement/replay responses per session;
normal raw transport is much smaller. This excludes pre-work argument/busy
refusals. Invalid messages do not acquire state or trigger source reads; an
unbounded hostile stream of new messages is not a bounded session.

These are logical byte, state and operation limits. Regular-file opens, reads
and stats can still block inside the operating system; there is no hard
wall-clock guarantee. Observe request cancellation and a two-second cooperative
work deadline between blocks/guard steps. Reaching that deadline after progress
may return a smaller phase advancement. A syscall that does not return cannot
be made safe by abandoning a leaking goroutine. No step may internally repeat
an entire failed pass.

## Authority and start admission

Every operation reloads the configured project. It requires a loadable project
declaring a contained audit directory, including cancellations and cached
replays. Reuse `auditWireSource`'s current authority binding (configured root and
configuration digest), and retain the initially opened project root's filesystem
identity for comparison against the newly loaded root on each continuation.
Replacing a project directory at the same textual path must not retain access
to the old session. Authority is internal; no path, digest of private settings or
filesystem identity is returned. This proposal does not introduce a caller
principal: a cursor is a bearer capability within the present server instance.
If a future transport has principals, they must additionally bind session and
start-ID lookup before this tool is exposed through it.

Use only the existing configured guarded openers for `evaluations.jsonl`,
`signatures.jsonl` and `stamps.jsonl`. Keep rooted containment, final-symlink,
regular-file, identity, minimum-size and absent/present checks. No opener may
fall back to a caller pathname, cached absolute path or earlier file handle.
Reopen names on each advancing call; close all descriptors before returning.

A start request is exactly `{"request_id":"<32 lowercase hex digits>"}`.
The caller generates a fresh random 128-bit ID. This is an idempotency label,
not a file selector or authority grant. Lookup is scoped to current authority,
root identity and server instance, and is subject to the same bounded slot
admission as the session. A repeated ID returns the saved initial control reply
without capturing again, even after later progress; it does not roll progress
back. That initial cursor may have become stale, so a client that has already
advanced must keep its latest cursor. A client that loses the initial response
can retry the same ID rather than leak a second session. Deduplication lasts
only while that session's record remains retained; after expiry or eviction,
reusing the ID can start a fresh capture. Clients never reuse an ID intentionally.

Start admits a slot and takes only presence, identity and size metadata. It does
not scan the files or return byte data. Capture trail/signature sizes under the
cooperative trail shared lock; release it before taking the separate stamps
shared lock. No nested locks and no lock survives the call. If evaluations is
absent, capture the sidecar without claiming a trail transaction; stamps capture
still uses its own lock when available. Preserve the existing
`snapshotBetweenWrites` flag's trail/sidecar-only meaning. It says nothing about
an atomic three-file transaction or whether all bytes were read while locked.

**Required implementation gate:** current `fssecure.OpenSnapshot` uses blocking
shared locks. A new bounded-attempt capture helper is needed for this tool,
without changing existing callers' semantics. Try each supported shared lock
without waiting; a held writer lock is `busy`, never “locking unsupported.”
Unsupported locking may use the current explicit unlocked fallback and false
flag. Other lock failures refuse. Limit identity-race retries to three complete
metadata attempts, check cancellation between attempts, and release the pending
slot on admission failure. Do not copy the current blocking helper into the new
path and claim cancellation makes it bounded. Platform support and refusal
classification need focused Unix/Windows evidence before implementation ships.

## Cursor protocol and response states

Continuation is exactly `{"cursor":"<opaque token>"}` with optional
`page_bytes`. The integer is accepted in every phase, used only for transport,
and defaults on each request. A cached replay ignores a newly requested page
size and returns the original answer. Start IDs, phase selectors, source
offsets, filenames and caller digests are forbidden in continuations. Null,
unknown, duplicate, case-folded, fractional/exponent and oversized arguments
refuse before work. Token validation uses a distinct download kind, a
cryptographically random 256-bit session ID and a bounded sequence number;
tokens fit within 85 ASCII bytes. Cross-tool cursors refuse.

Successful replies have the normal `outputVersion`, `tool`, `command`,
`auditDownloadVersion:"1"`, a `phase`, `complete`, and `snapshotBetweenWrites`.
Every nonterminal reply has `nextCursor`; terminal replies have none. `files`
is always the fixed three-file metadata array with `name`, `present` and captured `bytes`.
A digest is absent until preparation of that file is finished; once provided,
it never changes. After the `ready` reply all present files have a
`sha256:<lowercase hex>` digest, including the digest of zero bytes for an empty
present file. Missing files have no digest. `complete` remains false until the
last successful final check.

| Reply phase | Work and required additional members |
| --- | --- |
| `preparing` | Start returns `preparedBytes:0`; subsequent steps hash the next bounded range and return cumulative `preparedBytes` / `sourceBytes`. No byte chunk is returned. |
| `ready` | The last preparation step completed all hashes. Return all file digests and a continuation for the first transport page; still no byte data or completion claim. |
| `downloading` | Return `file`, `offset`, canonical padded `dataBase64`, and `fileComplete`. No structured line results. Even the last raw page has `complete:false` and a finalization cursor. |
| `finalizing` | Recheck the next bounded range against the preparation block hashes; return cumulative `checkedBytes` / `sourceBytes`, without repeating byte chunks. |
| `complete` | Final block checks and final name/identity/minimum-size checks succeeded. Return unchanged final metadata and `complete:true`; no byte chunk and no cursor. |
| `cancelled` | Explicit cancellation released the session; `complete:false`, no byte chunk and no cursor. |

The last finalizing step can directly return `complete`; it must not read beyond
its 8 MiB budget. Empty exports still pass through ready, three zero-byte file
pages, and a final identity-only completion call. Phase changes do not restart
source scans. A client follows cursors until `complete`, not merely until the
last raw chunk or a file's `fileComplete` flag.

Each advancing call first checks current authority, root identity and all three
captured file identities/presence/minimum sizes, then does its bounded work,
then reopens and checks the names again. Preparation updates the per-file
SHA-256 state and the block hash from the **same** bytes. The final partial block
is length-bound. Serving verifies a block into the sole cache before emitting
its bytes. Finalization walks the committed block arrays in fixed order and
compares each re-read block. After the last range, repeat the guarded metadata
check before publishing completion. Append bytes beyond captured sizes are never
read, hashed, transported or allowed to alter a digest.

Hash states and block arrays are server-owned memory, never trusted from a
cursor or caller. Keep hash state only for the unfinished file; completed file
digests and immutable block arrays survive into serving/finalization. Stage a
step's hash-state and table updates and output in bounded temporary memory so
an unsuccessful response cannot silently advance committed offsets. SHA-256
state cloning/serialization is an internal process detail, not a persisted or
version-stable input contract. No hash state or cursor survives restart.

## What successful completion establishes

For each present file, concatenating the decoded chunks at their declared
offsets produces exactly its captured byte count and preparation digest,
assuming SHA-256 collision resistance. Bytes include whitespace, CR/LF,
malformed UTF-8/JSON, blank lines, oversized records and an incomplete tail.
The tool does not decode, repair, normalize, deduplicate or drop retained bytes.
There is no record-size limit on raw transport inside the per-file ceiling.
It supplies no verification verdict or statement that a malformed tail is a
completed evaluation.

Preparation commits a sequentially read byte transcript. Serving reproduces
that transcript block by block, and finalization observes each block matching
its commitment again. This gives the same kind of integrity detection as the
current raw exporter, with the final observation interval now spanning calls.
It does **not** establish that every committed byte existed simultaneously,
that a previously rechecked block stayed unchanged, or that an adversary could
not change and restore data between observations. In particular, a block changed
after its finalization step may evade detection. Hashing and identity checks
cannot provide an atomic snapshot against an uncooperative writer.

With cooperative append-only writers and unchanged prefixes, later appends are
excluded and each file exports its captured prefix. The trail/signature size
pair is captured between their cooperative writes when the flag is true; stamps
have their own capture time. A cooperative repair, rotation, truncation or
replacement can invalidate an export. There is no three-file atomicity and no
claim that exported sidecars cover exactly the exported trail or each other.
Without supported locks, even the between-writes size-capture guarantee is
absent. The final reply establishes completion of this observation protocol,
not continuous integrity of a live filesystem.

Clients keep three provisional destinations in the advertised fixed order:
`evaluations.jsonl`, `signatures.jsonl`, `stamps.jsonl`. Each file's offsets start
at zero and advance without gaps/overlap; a page never spans files. Missing and
present-but-empty files each get one zero-byte page with `fileComplete:true`,
but remain distinguishable by metadata. A client must not create a retained
empty file for an advertised missing source. Apply an answered cursor's bytes
once, including when its response is retried. After `complete`, check all
lengths and per-file digests before promoting destinations. On any terminal
refusal, cancellation, restart or expiry, discard the entire provisional export.
Resuming a new session at an old local offset is forbidden; there is no random
access or cross-session content reuse in this candidate.

## Retry, concurrency, failures and cleanup

Keep the original start control reply and exactly the latest answered
continuation response, including the final one. A retry first revalidates
authority/root identity and expiry, then returns the historical bytes without
source reads or an assertion about the current source. Older continuations
refuse. Once progress advances beyond a cursor, it cannot be resumed. Completed
records may be evicted for new admission, so final retry is available only while
the record is retained. No replay extends lifetime or bypasses the replay cap.
On completion, release block arrays, hash states and the source cache, and stop
counting the record against the two live large sessions. Retain only fixed
metadata, the initial control reply and the final retry reply in its shared
slot. Otherwise completed records could retain more than the two-session hash
budget even though no longer live.

Do not hold the existing pager's global metadata mutex during large source I/O.
Reserve a shared slot under a short lock, use a per-session advancing guard and
one nonqueued download-work permit, and publish under the short lock. This
requires factoring shared eight-slot admission/terminal eviction into a small
common registry; merely calling the existing pager while it holds its I/O mutex
does not provide the proposed isolation. Preserve v1/history operation
serialization and response semantics, and test the changed ownership plumbing.
Two calls with the same
cursor must never run the work twice: an overlapping call returns a retryable
busy error, or, after the first commit, the cached response. Calls for unrelated
sessions also receive busy rather than queue while the download-work permit is
held. No refusal caused by contention changes the current cursor, deadlines or
source work. Initial capture uses the same work permit.

Serialize and size-check the **whole** candidate MCP result before committing
the next sequence and retry cache. An encoding/response-budget refusal
invalidates only that session, including initial-ID mapping and retry state;
no unpublished cursor can occupy capacity. Allocate/cache ownership must be
explicit so concurrent cancellation cannot free state still in use. Once a
response is committed, a lost transport response is recovered through replay;
do not “roll back” because delivery is uncertain.

`{"cursor":"<issued token>","cancel":true}` is the only cancellation form;
it forbids `page_bytes` and returns a terminal cancelled reply. Accept any
authentic issued sequence for that session, so a client can cancel after losing
a reply; do not accept arbitrary future sequence numbers. If work is active,
signal cancellation and return retryable busy until the worker closes its
handles and acknowledges termination. Check the signal between blocks and
before publication; never claim cancellation complete while reads continue.
Retain a small replayable cancellation tombstone, evictable like a completed
record, while releasing the large-session quota and heavy state immediately.
It still uses one of the eight shared slots until evicted. Restart loses it.

A request context cancelled before commit aborts the candidate step and
invalidates that session after handles close; a new session is needed. A context
cancelled after commit leaves the answered response replayable. Source read,
identity, authority, hash, counter or resource-limit failure likewise terminates
that session. Busy and malformed arguments before work are nonterminal. All
refusals use `isError:true`, with the same bounded versioned error object in
`structuredContent` and the text content's JSON. That object carries the normal
output metadata, `auditDownloadVersion:"1"` and
`error:{code,message,retryable,terminal}`. Messages are finite and path-free,
without retained data, rejected values or key settings. Candidate codes are
`arguments`, `busy`, `cursor`, `changed`, `limit` and `read`. `busy` covers either
work/lock contention or capacity: it is retryable and nonterminal. `arguments`
is nonretryable and nonterminal; correcting the request can use the same valid
cursor. `cursor` covers expired/unknown/stale tokens and is terminal for that
attempted resumption. `changed`, `limit` and `read` are terminal and nonretryable
for the named session. Do not expose a changed-authority session's metadata in
its error. All error variants and cancellation publication races need vectors
before adoption; an absent result caused by transport loss is recovered only
through the stated replay rules.

Check both deadlines on every operation and before publishing; completed reply
and cancellation replay do not extend retention. An idle sweeper runs at most
every thirty seconds to reclaim abandoned state, including starts whose clients
disappear. It performs no source reads and observes per-session ownership; active
work is signalled to abort and releases resources when its syscall returns.
Expiry cannot promise immediate descriptor release for an OS call that stalls.
An expired in-flight operation keeps its work permit, slot reservation and
large-session quota until ownership is relinquished; replacement admissions
cannot multiply buffers behind stalled operations.
No permanent worker, file descriptor, lock, private spool or audit-file write
survives a normal call. Capacity admission may evict terminal records but never
silently evicts a live export. At two live large sessions or eight live shared
slots, a new start refuses without doing source I/O or evicting another live
raw/history session.

## Acceptance evidence and deliberate mutations

Implementation needs hand-authored vectors for every phase, new member, enum,
argument refusal and error category, plus focused Go/MCP tests. Reuse existing
v1/history/verifier evidence where code is unchanged; do not broaden their
claims from the new tool's tests. Use small injected budgets and offset-aware
readers for exhaustive boundaries, plus a sparse filesystem case above 64 MiB;
avoid routine multi-GiB fixtures. At least one real guarded three-file path
test must exercise the same engine as the synthetic readers.

| Acceptance case | Evidence / mutation that must fail |
| --- | --- |
| Exact three-file custody | Hand-authored CR/LF, malformed UTF-8/JSON, blank/duplicate lines, a record crossing several blocks/pages, and incomplete tail; reconstruct exact bytes, sizes and independent SHA-256 values. Mutation: normalize CR or omit an oversized/tail byte. |
| Presence and order | All missing/empty/present combinations, stamps with no evaluations, zero-byte pages and fixed names/order. Mutation: conflate missing with present-empty or skip its page. |
| Limits before work | 64 MiB+1 succeeds on the new tool; exact 1 GiB/file and 3 GiB aggregate succeed through a bounded reader; one byte above, negative size and overflow refuse before reads/hash allocation. Existing listing still refuses above 64 MiB. Mutation: bypass either new limit. |
| Bounded phases | Counters assert at most 128 hash/recheck blocks, at most five serving blocks and at most three capture attempts per call; final raw page is provisional; max-size call/read formulas hold. Mutation: whole-file initial/final scan or one-byte transport admission. |
| Hash continuation | End quanta at full-block, short-final-block and file boundaries; preserve per-file digest processing across calls. Page-size changes do not alter digest/bytes. Mutation: hash overlapping bytes twice, hash a different buffer from the block commitment, lose a SHA state member, or reset checked progress. |
| Append exclusion | Append separately during every phase and at file boundaries; captured bytes/digests unchanged. Mutation: read live size instead of captured length. |
| Detected changes | Before/unserved/served/finalization blocks, removal, missing-to-present, replacement, truncate/regrow and same-size in-place edits at observable checkpoints; each refusal invalidates only that export. Mutation: omit guarded identity, per-block verification or final recheck. |
| Honest race limits | Deterministic writer changes and restores bytes between checks, or edits an already-finalized block; document that success can occur, while downloaded bytes still match commitments. Mutation: strengthen completion/flag text to an atomic or continuous-integrity claim. |
| Cooperative locking | Trail lock contention yields busy without fallback; unsupported lock yields false flag; other lock failure refuses. Stamps wait-free try-lock is separate, with no nested trail lock, even when evaluations is missing. Exercise Unix and Windows classification. Mutation: interpret contention as unsupported or hold a lock across a cursor. |
| Authority | Configuration changes, root-directory replacement at the same path, audit declaration removal, symlink/nonregular swaps and cross-tool tokens refuse; even replay/cancel cannot bypass current authority. Mutation: compare root string alone or serve replay before authority check. |
| Idempotency | Lost initial reply with same start ID uses one slot; latest prep/data/final reply is identical under changed `page_bytes`; concurrent duplicate gets busy or identical cached response and advances once. Mutation: rehash/re-read a retry or allocate a second start. |
| Finite lifetime/work | Fake clock reaches idle and absolute deadlines, including during a step; exact call/replay cap and one-past cases; sweeper releases abandoned state and no replay refreshes deadlines. Mutation: slide absolute expiry or omit a counter. |
| Capacity and isolation | Two large sessions, eight shared slots, terminal eviction and busy permit; no v1/history state is lost. Mutation: hold the shared mutex over a slow large read or evict live state. |
| Publication and cancellation | Encoding/size/accounting failures on start/continuation/final response release state; inject cancellation before/after commit and during a read, concurrent stale-token cancel, lost cancel response and repeated cancel. Mutation: publish cursor before serialization, advance on failure, or acknowledge cancel while work continues. |
| Resource ownership | Instrument handles, locks, hash-array lengths/capacities, cache and retained replies through success/failure/expiry/restart; enforce 8 MiB payload accounting without calling it RSS. Mutation: retain whole source/spool, unbounded history of replies or leaked pending reservation. |

## Implementation slices and adoption gates

1. Agree on this proposal's tradeoffs and concrete budgets. Record whether
   detection rather than point-in-time capture meets the download requirement.
   Do not mark #308 closed based on this design or history-only #324.
2. Add a bounded internal block-phase engine with injected readers/budgets and
   hand-authored digest/phase vectors. No public tool yet. Keep v1 behavior and
   its existing fixtures unchanged; extract shared code only when evidence shows
   its contracts remain identical.
3. Add the new nonblocking guarded size-capture helper and platform lock tests;
   factor shared admission ownership and add root identity, slot accounting,
   cursor ownership, cancellation and expiry tests. Prove no existing writer or
   CLI lock semantics change, and preserve v1/history retry/capacity behavior.
4. Add the raw-only MCP definition, strict arguments, versioned result/error
   vectors and complete response accounting. Run focused contract, mutation and
   three-file tests, then the repository's required integration checks once on
   the stable candidate. Review complete diff and interactions, rather than
   treating passing helper tests as the wire review.
5. Adopt through a material implementation PR with proposed ADR and recorded
   different-vendor adversarial review/dispositions for the exact reviewed SHA.
   Expected material categories are public-surface, documented-claim and
   security; a new dependency would additionally require dependency review.
   Update the MCP reference and release notes with actual limits and client
   discard/retry behavior. This document remains historical proposal context.

The new tool is an additive public interface, so [VERSIONING.md](../../VERSIONING.md)
calls for a minor CLI release. It gets an independent download wire version;
existing `outputVersion`, `auditWireVersion`, history version, retained audit
record/sidecar/stamp formats and project `configVersion` need no change for this
candidate. A breaking change to the candidate after adoption must deliberately
version its wire rather than silently reinterpret a phase. Feature discovery
lets clients keep using current tools on older servers; no existing cursor is
migrated, translated or made durable. Client integration, including Desk, is a
separate authorized change in its owning repository.

## Questions that remain open before adoption

- Are 1 GiB/file, 3 GiB/export, two live large sessions and thirty-minute hard
  lifetime adequate on the intended local/networked MCP transports? Measure
  transfer latency and retained/transient memory before choosing final values;
  never remove the finite bounds because an example does not fit.
- Can supported platforms provide the proposed nonblocking cooperative capture
  without weakening current guarded identity checks? If not, revise admission
  semantics explicitly rather than advertise a bounded cancellable call.
- Does the requester need independent, immutable retained download bytes even
  after source rotation, or a true point-in-time snapshot? The recommended
  commitment protocol cannot supply either. A private spool improves the former;
  the latter needs storage snapshots or a stronger cooperative capture protocol,
  and three-file consistency needs a shared writer contract of its own.
- Is latest-answer-only retry with a bounded replay budget sufficient for clients
  that reconnect through intermediaries? Durable or random-access resumption
  requires a separate authority, storage and retention contract.
- Does the proposed cancellation rule work for all server transports: busy
  acknowledgement while a syscall is active, then a terminal tombstone only
  after it returns? Validate exact publication races before adopting the wire;
  acknowledging immediate release while a read remains active is unsuitable.

Large configured verification needs its own bounded parser/chain/report state,
continuation semantics and report-size decisions. Staged supplied verification
needs authenticated chunk ordering, input digests, quotas and cleanup, without
turning a download cursor into a filesystem or verification capability. Neither
follows automatically from this raw protocol, and both remain separate #308
work. No verification verdict, public-key/trust-root support, new Core status or
cross-file atomicity is claimed here.
