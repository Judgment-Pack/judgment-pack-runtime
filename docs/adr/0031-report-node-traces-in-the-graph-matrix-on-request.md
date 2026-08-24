---
status: proposed
date: 2026-08-24
deciders: maintainer
---

# Report node traces in the graph matrix on request

## Context and problem statement

Every node evaluation inside a graph run already holds the trace ADR-0027 pinned — "the same
disposition, handoff target, and trace a standalone evaluation reports" — but the graph matrix
payload drops it: `runRow` compares each named node's disposition and discards the
`GraphNodeEvaluation` it read the disposition off, so a wire client showing *why* one node of a
row went unknown has nothing to render (issue #127). The record exists at exactly the right
moment; only the report omits it.

Size is the real design question. A graph may declare up to 64 nodes and thousands of rows, a
trace multiplies per node per row, and ADR-0026's report budget was introduced on this surface
precisely because retained row content multiplies where a pack matrix does not.

ADR-0027 §0 settled the boundary this decision moves: "corpus and matrix row results carry
dispositions, not traces." That determination is partially superseded here — for graph matrix
node comparisons, when the caller asks — and is unchanged everywhere else.

## Decision drivers

- The echo rule, twice over: report what this run produced, off the one evaluation — a separate
  fetch-the-trace call would rerun the node and could describe a different revision than the run
  it explains, the exact join defect ADR-0030 just retired for documents.
- The report budget's invariant (ADR-0026): retained report components are charged as they are
  composed; a member that multiplies must live inside that accounting, not beside it.
- The rehearsal precedent (ADR-0028): an explicit boolean the caller declares, never inferred,
  with the default preserving today's bytes exactly.
- ADR-0027's contract: a successful evaluation's trace is never omitted and never `null`, `[]`
  at minimum; a transport of that member should not invent a third state.

## Considered options

- **A. Opt-in `include_traces`, traces on the node comparisons** — a boolean on the matrix
  surfaces; each compared node's comparison carries the evaluation's own trace.
- **B. Always-on traces** — no flag, every run pays.
- **C. A separate per-node trace tool** — fetch the trace after the run.

## Decision outcome

Chosen option: **A**. Option B taxes every consumer for what most never render — the matrix is a
gate first and a debugger second — and multiplies exactly the bytes ADR-0026's budget exists to
bound. Option C reruns the evaluation to explain it, and a rerun is a different run: different
read, possibly different revision, ADR-0027's determinism holding only when every admitted input
is byte-identical. The trace belongs to the run it explains or it explains nothing.

Settled constraints:

1. **The ask.** MCP `experimental_test_graphs` accepts an optional boolean `include_traces`,
   decoded exactly as ADR-0028's rehearsal boolean (a JSON boolean; null and every other type
   refused; the member name held to its exact spelling). The CLI form is `--include-traces` on
   `experimental graph test`, both the project walk and the path-named run. Omitted is off, and
   off is today's payload, byte for byte.
2. **Where the trace lands.** Each reported node comparison whose node the walk evaluated
   carries `trace` — the `GraphNodeEvaluation`'s own member, untransformed, under ADR-0027's
   pinned contract. It is carried by pointer so presence tracks the request exactly: asked and
   evaluated is present, `[]` at minimum, mirroring ADR-0027 §1; not asked is absent. A
   comparison naming a node the graph does not declare was never evaluated, has no trace even
   when asked, and is already a mismatch whose detail says why.
3. **What a trace never reaches.** Comparisons are reported only for nodes a row names, and only
   when the walk got that far: a headline mismatch, an expected-refusal row, or a refused
   evaluation returns before node comparisons exist, so those rows carry no traces — stated as
   the shape's limit, not hidden. Corpus rows and pack matrix rows are untouched: ADR-0027 §0's
   determination is superseded only for graph matrix node comparisons, on request.
4. **Order.** Node comparisons stay lexicographic by node name (the report's order); each trace
   stays walk-ordered internally (the evaluator's order, ADR-0027 §3). The two orders are
   different facts and neither is changed by the other.
5. **Cost is charged, not waved through.** Traces ride inside each row's marshaled report, so
   the existing per-row budget accounting charges them with no new machinery — a matrix that
   fits without traces may be refused with them, by the budget refusal that already exists, and
   the MCP response bound applies unchanged. The overshoot character ADR-0026 states (metering
   after composition) is unchanged and is amplified by exactly the bytes the caller asked for.

### Consequences

- Good, because a wire client can finally render *why* — per node, per row, from the run itself,
  with no second read and no re-evaluation.
- Good, because the default run is byte-identical to today on every surface, and the ADR-0027
  contract gains a transport without gaining a variant.
- Bad, because an asked-for run can trip the report budget where the bare run did not; that is
  the budget doing its job, and the refusal names it.
- Neutral, because human renderings are unchanged: the trace is for machine consumers, the JSON
  report carries it, and the CLI's help says so.
- Neutral, because `outputVersion` stays: additive members under VERSIONING.md's MINOR rule.
