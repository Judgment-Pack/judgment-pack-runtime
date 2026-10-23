---
status: proposed
date: 2026-10-23
deciders: maintainer
---

# Add explicit base-only Core 0.3.0 contracts

## Context and problem statement

Core `0.3.0` adds profile negotiation and routed handoffs. Adding a third bundled
schema alone would leave evaluator admission, expected disposition decoding and
reports under different contracts. Existing defaults and historical canonical
bytes must keep their meaning. This preparation is material compatibility work;
required review and integration remain gates before adoption.

## Decision drivers

- Pin exact source bytes without fetching mutable specification resources.
- Keep document support, evaluator support and profile support separately scoped.
- Apply one explicit contract to admission, resolution, expectations and reports.
- Preserve old bundles, canonical dispositions, audit interpretation and defaults.
- Bound route and report amplification without changing a portable disposition.

## Considered options

- Replace the legacy evaluator version globally.
- Infer a disposition contract from route presence or change the matrix carrier.
- Carry an explicit exact contract, selected from an associated pack when available.

## Proposed decision

Use the third option. Bundle exact Spec commit
`65c7cf5598a0f0fc532c80a9e2215090951104ce`, with an immutable-git-ref provenance
lock and byte-exact hashes. No published Core `0.3.0` tag is represented by that
source pin. Historical artifact bytes are unchanged. Conformance-manifest schema
references resolve only through explicitly registered pinned schema resources;
local suites cannot fetch schemas or walk to sibling files. Their manifest schema
must match the exact bundled version.

Support only the Core `0.3.0` base profile. Absent profile means base without
inserting a default or changing the pack digest. Known unsupported extended is
refused with exactly `{"code":"profile-unsupported","profile":"extended"}`,
without a message or other diagnostic, before schema interpretation or admission
of facts/evidence. Invalid declarations remain structural failures; syntax remains
a carrier failure. Discovery adds exact `evaluatorCapabilities` entries and scopes
`supportedProfiles: ["base"]` to Core `0.3.0`. This makes no global promise about
Policy, Dialect or Runner profiles.

Pack-aware operations select the contract from the exact associated pack bytes.
Standalone authoring accepts explicit `spec_version`, retaining `0.2.0-draft` when
omitted. Legacy entry points retain that default. Version is never inferred from
route presence. Matrix carrier versions `1`–`3` stay unchanged; the contract selects
the expectation grammar. Project walks, graph compositions and comparisons require
one exact contract and refuse mixed versions before any evaluation, including
zero-input runs. Static advice follows the same admission and selected metadata;
it does not fabricate an expectation.

Under Core `0.3.0`, routed handoffs retain authored order, duplicate destinations
and exact names. Absent routes and empty routes are distinct. No selected route
retains the original disposition kind/reasons, with `handoff.state: "none"`,
`routes: []`, and no `triggeredBy`. A selected request retains its pre-filter
trigger set. Direct exception requests still apply route filters against all
retained reasons. Target-only pack canonical bytes stay unchanged. Singular target
assertions on routed results mismatch, including null assertions; they never choose
a first route. Graph reports fan out routes in declaration order with duplicates.
Aggregated display target names stay sanitized and never replace exact portable
names. Core `0.3.0` graph suite entries add `evaluatorSpecVersion`, omitted for legacy
entries.
Audit records carry exact produced canonical bytes and the applied evaluator
version; old records retain their original version and interpretation.

Core `0.3.0` project matrix reports bound expected plus actual canonical disposition
text at 10 MiB, refusing with `JPS-RESOURCE-MATRIX-DISPOSITIONS`. Graph evaluation
and Core `0.3.0` graph matrix/suite reports use 16 MiB: nodes, handoffs and headline
are charged incrementally and the final envelope is checked. Comparison retains
its existing `MaxReportBytes`. Whole-report refusal preserves complete portable
values rather than truncating them.

The document runner keeps strict diagnostic equality checks and names optional
`expectedAdvisories` rows as skipped/not checked, with a separate count and row
reason. Those assertions are not implemented here; rule-overlap work remains
[#304](https://github.com/Judgment-Pack/judgment-pack-runtime/issues/304). Skip
fields are omitted from old reports when unused. Supplemental normative-requirement
vectors remain separate from each named published corpus.

## Consequences and review gates

Clients may inspect exact evaluator/profile capabilities and deliberately author
under the new contract while existing defaults preserve their behavior. Core
profile and route checks have bounded supplemental vectors; corpus runs alone do
not exhaust their requirements. No historical accepted ADR is rewritten.

The known URI component grammar defect
[#297](https://github.com/Judgment-Pack/judgment-pack-runtime/issues/297) also
applies to the shared Core `0.3.0` validator. Its global compatibility change is
separate work. Required independent material review must see this limitation and
the complete integrated surface diff. The conformance posture, exact evidence,
and outstanding claim decision are stated solely in
[CONFORMANCE.md](../../CONFORMANCE.md).

Material-decision impact: explicit Core contract/profile admission, routed portable
values and downstream interpretation require the repository's recorded review gate.
