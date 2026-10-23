---
status: accepted
date: 2026-10-23
deciders: maintainer
---

# Expose bounded static fact-type advice without authoring expectations

## Context and problem statement

Prepared facts have no independently authored expected result yet. The matrix
validator requires an expectation, so using it to check prepared facts would
pressure clients to invent one. Clients also need the Runtime's comparability
rules when reviewing proposed test rows, including deliberate wrong-type probes.

## Decision drivers

- Reuse the existing evaluator comparability walk without evaluating conditions.
- Preserve exact input representations and matrix admission semantics.
- Bound admission amplification, comparison work and advisory reporting.
- Keep the supported Core contract exact at the release-pinned version.

## Considered options

- Require a synthetic expectation before checking prepared facts.
- Duplicate comparison semantics in each authoring client.
- Expose the existing static walk as read-only experimental tooling.

## Decision outcome

Expose `AdmittedPack.CheckFactTypes`, `experimental_check_fact_types`, and an
optional `pack` argument to `experimental_validate_test_matrix`. The standalone
MCP tool requires only exact pack and facts JSON text and accepts optional
supported extensions. The matrix tool checks structurally valid matrices only,
using each row's supported extensions, and adds one ordered fact-check row for
every case. Matrix-only calls retain their existing behavior and shape.

The advice contract is `factCheckVersion: "1"` under the existing tooling envelope
(`contractVersion: "1"`). Its Core contract is exactly `0.2.0-draft`, the version
admitted by the current evaluator. Bundling a newer Core release is separate work;
this change neither implies Core 0.3 support nor changes conformance claims.

A finding identifies a comparison's pointer, operator, observed JSON type and
expected types, without copying the fact value. Missing facts are not findings;
null, booleans, JSON numbers, strings, arrays and objects remain distinct. Every
stated comparison is inspected, regardless of evaluation reachability. Findings
do not reject matrix rows, establish policy, fabricate expectations or evaluate
outcomes. Clients retain responsibility for deliberate negative probes.

A completed check has status `complete` with an array of findings, possibly empty.
Malformed or unsupported inputs and per-check limits produce `unavailable`, an
empty findings array and a bounded message identifying the Runtime failure code.
No partial findings are represented as a completed check. Batch limits produce a
tool error, with no partial report. Exact bounds and payload fields are documented
in [mcp-clients.md](../mcp-clients.md#test-proposal-correction) and enforced by
hand-authored adversarial tests.

### Consequences

- Prepared input snapshots can be reviewed before expectations exist.
- Runtime owns the advice semantics; clients need not reproduce the evaluator.
- No project files, lock or audit records, sources, models or network are accessed.
- Capability-set count is bounded before pack validation to bound repeated pack
  admission and retained roots; comparison work and report sizes are bounded too.
- Large batches may require splitting, and malformed capability responses must
  remain explicit unavailability in clients.
- This new public surface and its documented claims require a recorded
  cross-vendor review before merge under the repository's review regime.
