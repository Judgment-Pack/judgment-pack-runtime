# Architecture

## Repository boundary

`judgment-pack-runtime` owns the executable, command UX, result rendering, resource limits, packaging, and
future public extension contracts. It is one nonnormative consumer of JPS.

`judgment-pack-spec` separately owns normative prose and schemas plus nonnormative public examples,
conformance metadata, and release records. The CLI consumes reviewed immutable snapshots and does
not write to or release the specification repository.

Private commercial repositories may depend on the executable and versioned output contract. The
current Go packages are internal implementation details; a versioned in-process composition or
plugin API remains future work. The public CLI never depends on private source, packages, services,
or credentials.

## Rule-overlap authoring advice

Core `0.3.0` semantic validation shares a bounded non-normative overlap pass with
CLI and MCP. It preserves three-valued true/false sets, exact decimal ordering
and lexical string equality. Strict conformance runs gate exact overlaps while
retaining normative document results; optional assertions have separate coverage.
[ADR-0066](adr/0066-bounded-rule-overlap-advice.md) records limits, exclusive
rewrites and the frozen expectation pointer adapter. Evaluation and disposition
resolution stay in the existing evaluator.

## Dialect document tooling

`internal/dialect` implements the offline tools of ADR-0052 and the document
validation of ADR-0053. The CLI `dialect` namespace and eight MCP tools share
strict JSON handling, canonical bytes, digests and document checks for a pinned
draft companion. Encoding supports versions 2 and 3; document validation supports
version 2. Capability discovery states those sets separately. ADR-0054 adds pinned reference/schema resources and declared pack-inventory
coverage through the same pure snapshot inventory used by project tools. Profile
compatibility, actual value-type checks and preparation verification remain
unimplemented by this boundary. The schema is a Runtime-derived editing aid.
It does not prepare facts, acquire sources or interpret an audit binding.

## Experimental offline Policy prototype

`internal/policy` implements a pinned, nonnormative RFC 0023 prototype `0.0.2`
([ADR-0059](adr/0059-experimental-offline-policy-tools.md)). CLI
`experimental policy` and seven explicitly experimental MCP tools share the same
reports, diagnostics and limits. They consume exact supplied bytes, perform strict
document/schema and local-reference checks, and optionally verify retained-material
closure, byte identities, section anchors, structural Core packs and specific
consumer references. Full Core semantic validation, evaluation, source access,
publication and approval stay outside this boundary. Checking a governing-adoption
document establishes no governing-adoption authority or standard conformance.

The required material JSON carrier is a private Runtime supplied-byte transport,
not a portable release envelope; it contains content references and base64 bytes,
never locators. CLI reads explicit bounded files/stdin only; MCP reads no project
files. Raw identities and optional canonical identities remain distinct, so an
ordinary retained Core pack is not inadvertently held to the narrower canonical
number domain. Source/schema/example provenance pins specification revision
`8a56a95d555c17640d2db4984363b779536b3c2a` and per-file byte identities. No mutable
resource is fetched at runtime. The shared 16 MiB stdio line bound is unchanged,
so JSON/base64 overhead reduces the maximum retained material a call can carry.

Explicit format `1.0.0` selects the official draft resources of
`policy-v1.0.0-draft.1`, pinned to Spec commit
`0799b37b74e35c4a86bfbf70f7bee80bdb7bbe94`
([ADR-0061](adr/0061-adopt-official-policy-draft-resources.md)). The complete
published archive, original source paths and per-file provenance live under
`internal/policy/official`; the independent manifest pin and every file are
verified before schema compilation and by the release gate. The HTTPS source
schema identities stay distinct from derived release-specific Runtime editing
URNs, exposed alongside `resource.sourceId`. The same offline engine handles the
unchanged 1.0.0 grammar, material identity, anchors and typed Pack coverage.
The archived proposed candidate and prototype trees retain their exact bytes and
original metadata. Prototype defaults and reports remain byte compatible;
all-format discovery still requires explicit CLI `--all-formats` or MCP
`allFormats:true` and returns registry version 1.

Explicit 1.0.0 reports now carry official source provenance and `draft` maturity,
retaining `candidateCapabilities` and `candidateResult` member names. Only full
checks return the exact portable valid/refusal object. Encoding/local validation
retain their narrower scopes. The promotion deliberately changes 1.0.0 metadata,
capability exclusions, scope strings and derived editing schema identity; strict
clients must update their pins. It rewrites no artifact or historical report and
migrates no approval, Job or host authority. The experimental surface and false
full-document-conformance/authority flags remain unchanged.

Root physical bounds, strict JSON, selection and canonical numeric domain precede
ordered registration; root schema and semantic checks follow registration.
Portable tests defer source acquisition to its phase and use shared raw
registration directly. Duplicate media registrations count bytes once but may
repeat base64 on the wire; agreement never widens the private 24 MiB material
JSON or 16 MiB MCP ceiling. No draft result establishes current authority,
permission, publication, review trust, stable conformance or independent agreement.

## Artifact version arithmetic

`internal/artifactversion` owns exact three-component parsing, comparison and
patch/minor/major calculation. CLI `artifact version` and MCP
`get_artifact_version_options` share it (ADR-0055). This stateless authoring aid
never reads a workspace or chooses a release policy. Durable job usage belongs
to Runner; application publication policy and approvals belong to Desk.

## Runtime flow

```text
bounded file/stdin bytes
        │
        ▼
strict UTF-8 JSON carrier parser
        │
        ▼
exact specVersion registry ──► embedded schema and metadata
        │
        ▼
Core 0.3.0 profile negotiation (base only)
        │
        ▼
Draft 2020-12 structural validator
        │
        ▼
JPS semantic references and declarations
        │
        ▼
required-extension capability check
        │
        ▼
one result model ──► human or versioned JSON renderer
```

Each layer stops when the preceding layer fails. No validation layer evaluates a condition,
resolves a decision outcome, fetches a locator, loads an extension, or authorizes an action;
evaluation exists only behind the explicitly experimental surface of
[ADR-0007](adr/0007-experimental-evaluator.md).

That surface has its own flow, selected under the exact Core contract
([ADR-0010](adr/0010-evaluator-aligned-to-core-0.2.0-draft.md),
[ADR-0063](adr/0063-explicit-core-030-contracts.md)):

```text
§8.2 input preflight, in this order and complete before resolution begins
   pack ──► facts document ──► evidence-availability document ──► required-extension set
        │
        ▼
§8 resolution over the admitted inputs
        │
        ├──► §8.3 portable disposition ──► RFC 8785 canonical bytes (internal/jcs)
        │
        └──► §8.4 evaluation error ──► one class, no disposition at all
```

The preflight order is also §8.4's error precedence — `pack-not-conformant`, `malformed-input`,
`unsupported-required-extension`, `resource-exhaustion` — so the first failure encountered is the one
reported, and an input error is never overtaken by a result. Every surface reports the byte limit
inside that preflight rather than at the read, so an oversized input is classed and ordered like any
other preflight condition. Pack-aware operations select `0.2.0-draft` or `0.3.0`
from the exact pack declaration. Core `0.3.0` profile negotiation precedes structural
interpretation and facts/evidence admission; only `base` is supported. A payload
names both the pack's `specVersion` and the applied `evaluatorSpecVersion`. The
evaluation phase also carries the two limits §10 requires of the class — the evaluation-work limit and
the collection-size limit of `internal/evaluation/limits.go` — and reaching the work limit is
`resource-exhaustion` on the ordinary Core path, not only under the draft-RFC opt-in. The conformance
claim for this evaluator is stated, in full and only, in [`CONFORMANCE.md`](../CONFORMANCE.md) (ADR-0011);
this document states no part of it and every payload carries a reference to that file rather than a
claim. `experimental evaluate-corpus` runs the bundled evaluation corpus and reports row results: the
evidence §3.4.1 requires of a claim of this class, non-exhaustive by that section's own terms, and not a
claim — see [`CONFORMANCE.md`](../CONFORMANCE.md).

## Packages

Completed pack evaluations derive a complete typed `contributors` list from the final
outcome and its walk trace, outside the portable disposition
([ADR-0067](adr/0067-complete-outcome-contributors.md)). CLI/MCP evaluation JSON, graph
nodes and audit evaluation records retain this metadata. Attribution sorts by kind then
exact ID while the trace preserves document order; fallback and non-outcome results carry
none. Graph aggregates and disposition comparison summaries do not synthesize attribution.

- `internal/carrier` performs bounded strict JSON decoding and JSON Pointer tracking.
- `internal/artifacts` embeds exact files and verifies their lock before use.
- `internal/validation` compiles offline schemas and performs semantic checks.
- `internal/conformance` validates suite metadata and safely runs pinned cases.
- `internal/fssecure` opens selected local files defensively and enforces bounded regular-file reads, and roots a project's own reads at one directory so a configured path cannot leave it. It also holds the two writes bounded by a project's handle — appending a record beneath that same held directory (ADR-0018), and replacing a generated file whole (ADR-0019) — under exactly the refusals a read is held to, and it resolves nothing to a pathname for a caller to open. The runtime's other writes, `--write` on `spec schema`, `spec examples`, `packs schema`, and `packs suggest`, are create-exclusive copies at a pathname the operator named and are in `internal/cli`, outside this containment rule because they are outside any project. `packs suggest` adds one refusal in front of its own: a destination the configuration declares as a pack, a matrix, a graph, or a `rows` document is refused by name, resolved through symlinks at both ends, because the exclusive open would happily create a declared document that is not there yet (ADR-0024).
- `internal/result` defines machine output version 2 and exit classes.
- `internal/describe` composes the machine descriptions the CLI and MCP server share, so neither drifts.
- `internal/evaluation` implements the EXPERIMENTAL JPS §§7–8 evaluator (ADR-0007), with explicit Core `0.2.0-draft` and base-only `0.3.0` contracts (ADR-0010, ADR-0063), plus the bundled evaluation-corpus runner; and behind a further CLI opt-in the draft-RFC prototype of the specification's RFC 0008 collection quantifiers (ADR-0009), whose packs no published JPS version accepts and which the claim in `CONFORMANCE.md` does not cover. A second CLI opt-in, which cannot be combined with the first, is the draft-RFC prototype of the specification's RFC 0016 outcome values (ADR-0039), on the same terms.
- `internal/jpscanon` shares the existing safe-integer `jps-canon` document encoding between Dialect and the experimental Policy tools. It is distinct from the portable-disposition encoding below.
- `internal/jcs` canonicalizes the one value JPS §8.3 compares byte for byte, the portable disposition, over the strings, arrays, and objects a disposition is made of, and the Booleans the `value` member of draft RFC 0016 adds (ADR-0039), and nothing else.
- `internal/project` implements the `jpack.json` project convention (ADR-0012): a NON-NORMATIVE convention of this runtime, not part of the specification, that names a project's packs by decision id, checks the files it points at, and runs each pack's instance matrix through the same row machinery the bundled evaluation corpus uses. It resolves nothing at decision time, reads no source a hint names, and holds no pack's identity — the pack document does, and everything here is a validated reference to it. `packs suggest` (ADR-0024) derives candidate row *inputs* from a pack's own compared literals and emits them as a document that is deliberately not a matrix; it runs no evaluator and states no expectation, because the expectation is the half only the policy text answers, and the matrix loader's existing refusals are what keep a candidate from ever loading as a row. When rows declare an `origin`, `packs test` also profiles the matrix against that history (ADR-0034): agreement and coverage per origin, and each origin's cases placed against each comparison boundary the pack draws, read from the run already made and gating nothing; a row may cite the receipts its facts came under (`cites`, `matrixVersion "3"`, the gateway's shape held as ADR-0033 holds a record's), carried as the values declared and verified by nothing here.
- `internal/graph` implements the EXPERIMENTAL graph surface (ADR-0015): a NON-NORMATIVE composition prototype for the specification's RFC 0002 (Draft) that evaluates a DAG of configured packs in deterministic topological order, feeding each node's disposition forward as declared facts and evidence availability. Every node runs through `internal/evaluation` unchanged; the composite envelope is this runtime's own, and every payload says so. `graph test` reports derived coverage beside its rows (ADR-0016) — per-node probes through the same exported derivation `packs test` uses, plus up to one probe per reachable edge branch (resolved and unresolved) — and coverage informs and never gates. Under `configVersion "2"` a project may declare its graphs and rows in `jpack.json` (ADR-0017), and `graph test`/`graph validate` with no argument walk them exactly as the path-named forms run one.
- `internal/audit` composes and appends the opt-in evaluation records of ADR-0018: one JSON line per completed non-rehearsal evaluation (ADR-0028) — for a non-rehearsal graph evaluation, one per node and one for the composite (ADR-0041) — carrying the pack's identity and the digest of its exact bytes, the facts and evidence documents as they reached the engine, and the disposition as the canonical bytes the run produced rather than a re-serialization of them. It is reached only from a configuration that declared an audit directory, only after the evaluator has returned a result, and only from the three deciding surfaces — the test verbs run the same evaluator and record nothing, and a declared rehearsal records nothing either (ADR-0028). It evaluates nothing, changes no payload, and cannot influence a disposition; it does read a clock, which nothing on the payload path does. A record may carry `cites` (ADR-0033): the gateway receipts the caller said the decision relied on, in the gateway's own `{sessionId, callIndex, signature}` shape, held to that shape and otherwise recorded as given — the runtime verifies no receipt and reads no store. A record of `experimental evaluate` or the MCP `experimental_evaluate` tool may also carry `binding` (ADR-0050): a caller's `{kind, digest}` commitment to material of its own, held to that shape (`audit.ParseBinding`, which `audit.Verify` applies to a recorded one, reporting `binding-malformed`) and otherwise recorded as given — the runtime never interprets, verifies or recomputes it. The trail is chained unless the configuration says `chain: false` (ADR-0047 §1): each record carries `trail`, `sequence` and `previous`, the SHA-256 of the exact bytes of the line before it, assigned while the writer holds an exclusive lock on the trail file that `internal/fssecure` takes (`Root.AppendLocked`); nothing read from the trail is re-encoded. `audit.Verify` checks a trail by the same rules, over its exact bytes, from a snapshot whose size `fssecure.SizeBetweenWrites` reads under the lock, shared; `Writer.Repair` appends a discontinuity record after a torn last line and rewrites nothing; and the `jpack audit` commands in `internal/cli` drive them. The same reading lists the checkpoint of every chained record after a sequence, for a deliverer to hand to a holder, and holds a trail to the checkpoints a holder kept, reporting the records none covers as unwitnessed (ADR-0047 §2a); no hand-over is recorded or waited for. A writer given a signing key (`Writer.SignWith`, the key the project's `audit.signingKey` or `JPACK_SIGNING_KEY` names, loaded by `audit.LoadSigner`, which walks the key's path a directory at a time with each held open, refuses a symbolic link anywhere in it and the project's directory by identity, holds the key to its owner alone, and, when `audit.signingPublicKey` or `JPACK_SIGNING_PUBLIC_KEY` names the public key it is expected to have, refuses a seed whose public key is another; on a platform without unix ownership and modes, Windows among them, every key is refused) signs each chained record it appends in the `signatures.jsonl` sidecar beside the trail (ADR-0047 §2b): the signing runs in the step `fssecure.Root.AppendLockedThen` takes after the trail's write and syncs and before the lock is released, so the sidecar follows the trail line for line, and a signature that cannot be written leaves the record unsigned, never the decision failed. `Writer.Rotate` hands signing to a next key with a line the current key signs. `audit.Verify` reads the sidecar in step with the trail, a line at a time, when the verifier supplies public keys, from sizes `fssecure.SizesBetweenWrites` reads under the one shared lock. `jpack audit stamp` asks an RFC 3161 authority to stamp the current checkpoint's digest, through `internal/timestamp` (the request, the HTTP transport and the token's parsing and verification, on `encoding/asn1` and `crypto/x509` alone), and keeps the token with `Writer.RecordStamp` in `stamps.jsonl` under that file's own lock; nothing on the decision path reaches it (ADR-0047 §2a). `audit.Verify` checks those tokens against the verifier's roots before the trail is read, matches their checkpoints against it as it is read, and reports the lag between each covered record's `at` and its first stamp. `experimental_verify_audit` exposes this same verifier without keys, trust roots or witness inputs; `experimental_list_audit` reads the three configured files as bounded exact bytes and structured line results (ADR-0065). Repair, signing, key management and timestamp fetching have no MCP tool.
- `internal/lock` implements the reviewed-set lock (ADR-0019): a generated, deterministic sibling of `jpack.json` pinning the digest of the configuration's exact bytes and of every declared pack and graph — not a declared matrix or rows document, which no deciding surface applies. Its presence is the whole of the opt-in — no configVersion moves and the configuration schema does not name it, because a configuration that named its own lock could rename it. `packs lock` writes it, `packs verify` names every difference from it, and the three deciding surfaces hold the law one evaluation applies to it before evaluating — a declared rehearsal excepted (ADR-0028); the rehearsal verbs consult it never. It decides nothing about whether a change was right: an amendment and a tampering are the same act to a runtime, and it is not a wall against an editor that re-locks.
- `internal/compare` evaluates one set of inputs, a matrix admitted under its own rules whose expectations play no part, or a `packs suggest` candidates document, under two versions of a pack through `internal/evaluation` unchanged, and reports the inputs whose §8.3 canonical dispositions, handoff targets or refusals differ (ADR-0045). It opens no project, so it records nothing and consults no lock, and a difference it reports is not an expectation.
- `internal/policy` owns the experimental offline Policy prototype and official draft document and supplied-material checks of ADR-0059 and ADR-0061; it performs no evaluation or source acquisition.
- `internal/cli` owns commands, streams, and human/JSON rendering.
- `internal/mcp` adapts the offline core onto Model Context Protocol tools over stdio.
- `tools/sync-spec-artifacts` is an explicit maintainer-only snapshot importer.

## Version independence

The CLI version, JPS `specVersion`, machine-output version, and any future plugin API version are
independent. During JPS `0.x`, registry dispatch requires the entire `specVersion`; prefix matching
and nearby-version substitution are forbidden.

Development builds report version `0.0.0-dev`. GoReleaser injects the exact tag version into
official binaries through a Go linker flag; source builds without that release metadata remain
development builds.

## Decisions

This document describes the runtime as it is. The cross-cutting decisions behind it -- why the layout
is idiomatic Go rather than a `packages/` monorepo, why language plurality lives at the wire, why MCP
is the integration surface -- are recorded as architecture decision records in
[`docs/adr/`](adr/README.md). Implementation decisions are ADRs; normative, cross-implementation
proposals belong to the specification's RFC process instead.

The create / read / update / delete authoring loop this runtime supports -- and why it lives in the
client rather than in a runtime store -- is walked end to end in
[authoring-lifecycle.md](authoring-lifecycle.md).

## Core contract boundaries

The `0.3.0` resources are an exact immutable Spec commit pin,
`65c7cf5598a0f0fc532c80a9e2215090951104ce`, rather than a published Core tag.
The conformance runner registers the exact Core schema as an explicit in-memory
resource for manifest references. Local suites must match the bundled manifest
schema; they cannot retrieve remote schemas or traverse to sibling resources.
Optional advisory rows carry explicit skipped/not-checked results, counted
separately from assertions that pass.

An evaluator contract travels through pack admission, cache identity, resolution,
expected/actual disposition decoding, canonicalization, failures and audit metadata.
Legacy APIs preserve the `0.2.0-draft` default. Version is never inferred from route
presence. Core `0.3.0` handoff routes retain nil versus empty presence, exact names,
authored order and duplicates; reason and trigger sets retain their set semantics.
Graph fan-out retains the ordered routes; aggregated display targets remain sanitized
and never replace the exact portable names. Core `0.3.0` graph suite entries add
`evaluatorSpecVersion`, omitted from legacy entries. Singular target assertions mismatch
on routed results. Project, graph and comparison walks refuse mixed exact contracts
before evaluation. Old audit records keep their original contract and canonical bytes.

Runtime discovery separates document versions from evaluator capabilities. Profile
metadata is scoped to Core `0.3.0` and does not change Policy or Dialect contracts.
The full conformance posture and outstanding review are recorded only in
[CONFORMANCE.md](../CONFORMANCE.md).

Core schema compilation applies the shared `internal/uriformat` ASCII RFC 3986
component parser. CLI/MCP validation, ordinary Core evaluation admission and the
official Policy reader use the same predicate without altering identifier bytes.
Historical prototype Policy `0.0.2` schema and retained Core consumer checks opt
into the former predicate through a named compatibility override. It preserves
prototype behavior and is not an RFC-conformance implementation. No artifact pins,
network semantics, trust or authority change; [ADR-0064](adr/0064-share-uri-component-grammar.md)
records the acceptance change and explicit migration scope.

## Experimental audit wire reading

[ADR-0065](adr/0065-read-audit-trails-over-mcp.md) adds read-only MCP audit
listing and verification. `internal/audit.Pager` transports exact
fixed prefixes of the three configured audit files, using retained block hashes,
one block cache, a bounded cross-page line reader and a final integrity pass.
Structured line results carry exact line bytes/digests and every decoded member;
raw base64 chunks retain malformed, oversized and incomplete data as well.
Intermediate pages remain provisional until successful completion.

`internal/mcp` supplies only root-authorized project openers and bounded caller
data. No caller input selects a server filesystem path. Presence, project loading,
directory containment, final-symlink and regular-file guards precede reads.
`experimental_verify_audit` calls `audit.Verify` and `audit.ParseCheckpoints`,
sharing the CLI's full verification report. Supplied exact bytes replace CLI
`--trail` file access without adding path authority; their source metadata says
no between-writes file snapshot was taken. No repair, write, private-key loader,
timestamp fetching or new cryptographic format is exposed. The
[MCP reference](mcp-clients.md#experimental-audit-reading-and-verification)
defines budgets, provisional pages, retry lifetime and explicit line statuses.

`experimental_list_audit_history` applies the retained Desk history reader's
exact 4 MiB backward window and typed Go selection on the server, within the
64 MiB source ceiling. It preserves raw record strings/digests and unread/next
semantics, including Unicode escape replacement only for selection. One bounded
server-authored JSON result travels in base64 chunks, avoiding client JSONL
parsing while retaining the 4 MiB serialized MCP result cap. History projections
share the existing eight-slot/five-minute pager with raw exports; immutable
answers bind configured-project authority and reject cross-kind cursors.
