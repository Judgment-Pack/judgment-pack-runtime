---
status: accepted
date: 2026-08-14
deciders: maintainer
---

# Run the declared graph matrix over MCP, and stop a runaway matrix early

## Context and problem statement

ADR-0021 added `experimental_test_packs` and deferred its graph twin explicitly:
"Revisit when a graph twin is wanted — `experimental graph test` has the same gap
one level up, and the `author_graph` prompt already says 'where a terminal is
available'; the deferral is recorded here rather than accidental." That condition
is now met. The graph CLI and the shared `graph.TestProject` / `result.GraphSuite`
model exist, so the gap closes by exposing what is already there rather than by
inventing graph or evaluator semantics (issue [#95]).

The substitute a client can build is worse here than it was for packs: replaying a
graph's rows through `experimental_evaluate` means re-implementing the walk's
node ordering, its fact and evidence propagation, the canonical composite
comparison, and the coverage derivation — four things this repository ships and
one client would get subtly wrong in private.

## Decision drivers

- The closing step of the authoring loop should happen where the author is.
- A matrix row is a rehearsal, not a decision: it must record nothing.
- A graph matrix multiplies where a pack matrix does not, and the MCP transport
  cannot be interrupted the way a terminal can.
- Nothing about graph format, evaluator semantics, disposition comparison or
  coverage derivation may change to serve a transport.

## Considered options

- **A. Leave the deferral standing.** Rejected: its stated condition is met, and
  leaving it would make the deferral accidental after all.
- **B. Reuse `experimental_test_packs` with a mode argument.** Rejected: one tool
  answering about two different declared things makes the payload's shape depend
  on an argument, and the two suites are different result types.
- **C. A second tool, `experimental_test_graphs`, mirroring the first.** Taken.

## Decision outcome

`experimental_test_graphs`, with optional `graph_id` and `supported_extensions`,
calling `graph.TestProject` and returning exactly the payload the graph project
walk emits.

It inherits the packs tool's disciplines deliberately: a literal JSON `null`
argument is refused rather than decoded into the most expensive run; decoding is
strict so a misspelled key is an error and not a silently different run;
`graph_id` keeps presence separate from value, so a present-but-empty id is
refused rather than becoming a whole-project run; `project.Present` rather than
`Exists`, so a configuration that is there and will not load refuses instead of
answering as a project without one; and an oversized report is refused with its
size, never truncated, because a truncated suite report under-reports silently.

**Writing nothing and consulting no reviewed set are two invariants, not one.**
`graph.Options.Audit` left nil is what makes the run append no audit record
(ADR-0018); `graph.Options.LawCheck` left nil is what makes it consult no
reviewed-set lock (ADR-0019). They are guarded independently in the graph layer,
so a future change could restore one without the other, and each has its own
regression test.

### The one thing not inherited: where the report is bounded

The packs tool checks its report size after marshaling. Copying that mechanically
would have been wrong, and this is the substance of the decision.

A graph matrix multiplies: up to 10,000 rows, each re-evaluating up to 64 nodes
and retaining each node's canonical disposition, with coverage repeating graph
node and pack identifiers across probes. A conforming outcome id repeated across
10,000 row results can retain gigabytes before a 16 MiB check on the marshaled
response could ever see it. ADR-0025 already names that "build gigabytes, then
check the MCP size" shape as a defect.

`graph.Options.ReportBudget` stops a run whose retained report components pass
the budget — every row's report as it is judged, every graph's coverage block as
it is derived, every entry's remaining envelope once that entry is finished — and
the remaining rows are then never evaluated. A matrix-derived `detail` is capped
besides, marker included, because several entries can name one hostile rows file.

**It is a mitigation, not a guarantee, and this ADR says so because five review
rounds went into learning not to claim otherwise.** The named gaps:

- each component is metered *after* it is composed, so a run can overshoot by
  whichever component crosses the line;
- the suite's own outer envelope — metadata, top-level summary, the `graphs`
  framing — is never charged;
- deriving coverage retains a witness per row and per `expectedNodes` entry, so
  its **working memory scales with the matrix** even though the probes it emits
  do not. Mitigating that means restructuring coverage derivation, which is out
  of scope here and is recorded as an accepted residue rather than left silent;
- the row slice is **preallocated for the whole declared matrix** before the
  first row is judged, so even a trip on row one costs memory proportional to the
  matrix's declared length. `MaxRowsBytes` is what bounds that, not this budget.

What the budget does buy, and what is tested, is the one thing that motivated it:
**the remaining rows of a large matrix are never evaluated once it trips.** That
is evaluation work avoided, not memory reclaimed.

The caller's check on the finished report backstops the **response size** and
nothing else. It cannot backstop peak working memory, because by the time it runs
the memory has already been spent — which is the whole reason the row loop needed
its own check.

Five earlier versions of this paragraph were wrong, each recorded rather than
edited away, because the pattern is the finding. The bound was checked between
graphs; then in the row loop with coverage outside it; then with coverage charged
and the entry envelope loose; then with the envelope charged and the outer
envelope loose and "not row count" asserted about a derivation that does scale
with rows. Every round the code moved closer and the prose still described a
finished thing. The prose was the defect, not the mechanism.

### Consequences

- Good, because the authoring loop closes over MCP for graphs as it does for
  packs, without a client re-implementing the walk — on every pass after the
  first. The tool runs *declared* graphs, and a graph is declared in `jpack.json`
  at the hand-over step, so the first run of a newly authored graph still wants
  the CLI's explicit-path walk. The `author_graph` prompt says so rather than
  recommending a tool that cannot see the document yet.
- Good, because the resource shape that distinguishes graphs from packs is
  handled where it arises rather than at the response boundary.
- Bad, because the claim surface grows from seven to eight, and every inventory
  that names the surfaces must be updated together — `CONFORMANCE.md` and its
  mechanical enumeration test are what make that failure loud rather than silent.
- Bad, because a second differently-marked name now exists for a third operation
  (`experimental graph test`, `experimental_test_graphs`), which is the same cost
  ADR-0021 accepted and the same rename will settle.

This closes ADR-0021's recorded reopening condition. ADR-0021's own dated
consequence, that seven surfaces reach the evaluator, is left as written: it was
true when it was written.

## More information

Issue [#95]. The design was reviewed cross-vendor before implementation, per the
interim review regime and the issue's own first scope item; the review's five
findings and their dispositions are recorded in the pull request. F1 — that the
post-marshal check is not an adequate graph resource bound — is the finding that
changed the design, and it is why `ReportBudget` exists.

[#95]: https://github.com/Judgment-Pack/judgment-pack-runtime/issues/95
