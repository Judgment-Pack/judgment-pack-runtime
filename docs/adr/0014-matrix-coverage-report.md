---
status: accepted
date: 2026-07-29
deciders: maintainer
---

# Report derived matrix coverage in `packs test`, and never gate on it

## Context and problem statement

A project matrix is agent-authored, and the method that makes one meaningful — one row per declared
outcome, a conflict probe, an unknown probe, a missing-evidence probe, the rest of the `test_pack`
list — lives in prompt text. That is advisory by construction: [ADR-0008](0008-mcp-prompts-authoring-method.md)
records that the guidance reaches only clients that surface MCP prompts, and reaches nothing in an
agent that never loads or never follows it. `packs test` then proves *agreement* — every row's
expectation against the evaluator, byte for byte — but nothing proves *coverage*: a one-row matrix
over a three-outcome pack passes green, and what the matrix failed to probe is silent. The quality
control of the matrix rests entirely on the authoring agent's context, which is exactly the thing a
deterministic runtime should not rely on.

The two other layers of matrix quality are already placed. Agreement is mechanical and stays where
it is. Intent — whether the expectations are what the policy *means* — is irreducibly the policy
author's: a runtime that checked it would be interpreting policies, which this project has refused
twice (ADR-0008's rejected authoring tool, [ADR-0006](0006-authoring-lifecycle-in-the-client.md)).
Coverage is the one layer that is currently advisory but could be mechanical.

## Decision drivers

- Every behavior a pack's declarations make reachable and an expected disposition can witness is a
  deterministic function of the pack document and the matrix document — no model, no key, no
  interpretation, no network.
- A report the runtime computes reaches every client identically, prompts surfaced or not; the
  reliance on agent context is removed rather than mitigated.
- The reference-only rule of [ADR-0011](0011-first-evaluator-conformance-claim.md): nothing here may
  state or imply that a covered matrix is a correct one.
- The demotion discipline of [ADR-0012](0012-jpack-project-convention.md): `skipped` is not
  `passed`, and silence over a gap is the failure mode this repository refuses.
- Reachability derived from declarations is not reachability proven: a gate built on it would
  sometimes demand the impossible.

## Considered options

- **A. Derive probes from the pack document and report per-pack coverage in `packs test`,
  informationally.**
- **B. Status quo:** the probe list stays prompt-only.
- **C. Gate:** missing probes flip the run to `mismatch`, outright or behind a flag.
- **D. Trace-based coverage:** run the rows, read the traces, report which rules and exceptions were
  exercised.

## Decision outcome

Chosen option: **A**. B is the reliance being removed. C and D are each refused for one load-bearing
reason recorded below.

**C is refused because a derived probe is reachable by declaration, not by proof.** Two rules naming
different outcomes make a conflict probe derivable; whether any facts can make them fire together is
a property of their conditions that this derivation does not decide — and a pack whose
differently-outcomed rules exclude each other *on purpose* is well built, not under-tested. A gate
would demand a row no facts can produce. Informational coverage puts the gap in front of the author
and lets the policy text arbitrate, which is the same arbiter rule the `test_pack` prompt already
states.

**D is refused because the trace is the wrong witness.** §8.3 keeps the trace informative and
outside the disposition; expectations are the portable artifact, and coverage of expectations is
checkable against semantics the specification pins. A coverage built on trace shape would couple a
report to the one part of the payload the specification deliberately does not stabilize.

Settled determinations:

- **The probes are derived per pack from its declared outcomes and the reachable §8 reasons:** one
  probe per producible declared outcome — one some rule, force-outcome exception, or
  fallbackOutcome names, in declaration order (an outcome nothing references cannot be produced
  under §8, so its probe would be one no row could ever satisfy); `not-applicable` when the pack declares
  applicability; `missing-required-evidence` when any evidence requirement is required;
  `unknown` when applicability, required evidence, or an `onUnknown: escalate` rule or exception
  makes the reason reachable; `conflict` when two rules, or two force-outcome exceptions, name
  different outcomes; `exception-escalation` when an exception declares effect `escalate`;
  `no-match` exactly while no `fallbackOutcome` is declared. A probe whose behavior the declarations
  make unreachable is not derived at all — listing it as skipped would state a reachability claim in
  a status field.
- **The derivation overlaps the `test_pack` method's probe list without being it, in both
  directions, and both deltas are documented rather than papered over.** Two of the method's probes
  are deliberately absent because no expected disposition can witness them: a forced outcome reads
  exactly like a rule-produced one in a §8.3 disposition, and the ordered-comparison type probe
  lives in a row's facts, not its expectation — the prompt still carries both. And two probes here,
  `exception-escalation` and `no-match`, are reachable reasons the method's numbered list does not
  name; the derivation follows the resolver's reachable behavior, not the prompt's text.
- **A probe is witnessed by what a row expects, never by what it produced.** A row expecting an
  error class witnesses nothing. Coverage is computed whenever the matrix loaded as rows, mismatched
  rows included. It is absent when the matrix did not load — already a `mismatch` — and when the
  declarations derive no probe at all, a shape only a document far from conformant can reach, so a
  consumer must not read absence as "the matrix did not load".
- **Coverage never moves a status or an exit code.** A missing probe is a fact about what the rows
  expect, not a failed row. The CI gate's meaning is unchanged.
- **The reason vocabulary is stated once.** The §8 reason strings are exported from the evaluator
  package and read by the derivation, not restated in a second package that could disagree with the
  first — the one-statement discipline of ADR-0012 applied to a vocabulary.
- **`Coverage` is an additive member on the `packs test` entry and `outputVersion` stays `"2"`,**
  by the same two `VERSIONING.md` rules ADR-0012 cites for `packId` and `packVersion`.
- **The wording is bounded the way every claim-adjacent text here is:** a probe's detail states "no
  row expects X" and where the derivation came from; no text states or implies that covered means
  well-tested, correct, authorized, or conformant.

### Consequences

- Good, because the checkable core of the matrix method stops depending on prompt support and agent
  diligence: an agent in any client reads "coverage: 1/7" and the named gaps in the same payload it
  already reads, and a human reviewer sees what a green run did not probe.
- Good, because the gaps become visible where they already exist — the shipped demo matrices do not
  witness every derivable probe, and the report says so instead of nothing.
- Bad, because a derived probe is a static claim about reachability the evaluator never confirmed,
  and a missing `conflict` probe can be a prompt toward a row that is unconstructible. Mitigated by
  informational-only reporting and by detail wording that names the escape: confirm against the
  policy text that the rules exclude each other.
- Bad, because coverage can be gamed the way any coverage can: an agent can satisfy a probe by
  copying the evaluator's own output into an expectation — the circular oracle. Coverage forces the
  row to exist; the arbiter rule in the `test_pack` prompt governs what it may contain; nothing
  mechanical closes the remainder, and this record says so rather than implying otherwise.
- Bad, because the `packs test` payload grows a member and its human rendering grows lines, on a
  surface whose argument is that it has few. The member is additive and optional-by-omission end to
  end.
- Revisit when real projects ask for a gate — a flag that fails on missing probes would be a small,
  deliberate addition once the derivation has been lived with; when Core pins a trace minimum, at
  which point option D becomes principled rather than coupled to an unstabilized shape; or when the
  specification takes up per-pack case sets (ADR-0012's revisit clause), which is where a portable
  notion of coverage would belong.

## More information

Implementation: `internal/project/coverage.go` (the derivation), `internal/project/test.go` (the
wiring beside the rows), `result.MatrixProbe` and `PackTestEntry.Coverage` in
`internal/result/result.go`, the human rendering in `internal/cli/render.go`, and the exported
reason vocabulary in `internal/evaluation/resolve.go`. Method context: the `test_pack` prompt in
`internal/mcp/prompts.go` (ADR-0008, hardened against test-satisfying pack edits in the 0.7.0
release) and [`docs/building-with-packs.md`](../building-with-packs.md). Posture this depends on:
[0006](0006-authoring-lifecycle-in-the-client.md) (authoring stays in the client),
[0011](0011-first-evaluator-conformance-claim.md) (reference-only texts),
[0012](0012-jpack-project-convention.md) (the convention, the demotion discipline, and the
additive-member precedent). The cross-vendor adversarial review this decision requires attaches to
the pull request, not to this file (`docs/adr/README.md`, "Review of material decisions").
