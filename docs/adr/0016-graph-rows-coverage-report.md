---
status: accepted
date: 2026-07-30
deciders: maintainer
---

# Report derived coverage in `experimental graph test`, and never gate on it

## Context and problem statement

ADR-0014's argument for the pack matrix applies verbatim to the graph matrix: the rows are
agent-authored, and "the quality control of the matrix rests entirely on the authoring agent's
context, which is exactly the thing a deterministic runtime should not rely on." For graph rows the
gap is worse than it was for pack matrices. The pack surface at least had a `test_pack` prompt whose
probe list an agent could follow unreliably; there is no `test_graph` prompt at all — the
`author_graph` method (ADR-0008) never mentions rows or `graph test` — so nothing anywhere, mechanical
or advisory, tells a graph-matrix author what their rows failed to probe. And a graph matrix has more
to fail to probe: beside each node's own outcomes and reasons, the rows are the only place the
project can pin what an edge does — the upstream-resolved branch that injects a fact and contributes
evidence, and the upstream-unresolved branch that injects no fact and contributes the declared
unresolved evidence state — and the shipped fixture's own
rows demonstrated the failure this record closes: the row named `unresolved-screening-escalates`
pinned nothing at all about the screening node it is named for.

## Decision drivers

- ADR-0014's driver, inherited whole: the one layer that is advisory but could be mechanical.
- The graph surface's own removability (ADR-0015): whatever is added must vanish with the surface,
  claim nothing, and gate nothing.
- One derivation: the pack surface and the graph surface must not be able to disagree about what a
  pack's declarations make reachable.

## Considered options

- **A. Derive probes from the graph and its packs, report covered/missing, never gate** (chosen).
- **B. Status quo** — rows mean whatever the authoring agent thought of.
- **C. Gate `graph test` on coverage** — rejected for ADR-0014's reasons, which are stronger here: an
  edge-fed pointer or requirement restricts what a row can construct, so unconstructible probes are
  more common, not less.
- **D. Derive from the evaluation results or feeds** — rejected: witnessing what ran instead of what
  the rows expect is ADR-0014's refused option in another costume; a matrix's meaning is its
  expectations.
- **E. Headline-only probes** — rejected: the headline is one node's disposition echoed, and a
  headline family beside a result-node family would count one behavior twice while the edges stayed
  uncounted.
- **F. Fold into ADR-0015 as an amendment** — rejected: this is a new determination with its own
  invariants, not a revision of the surface's shape.

## Decision outcome

Chosen option: **A**. `experimental graph test` reports a `coverage` member beside its rows,
reusing `MatrixProbe` unchanged under a graph-owned probe grammar, derived as follows.

**One probe family per node, none for the headline.** Each node's probes are ADR-0014's own pack
derivation — now behind one exported entry point (`project.PackProbes`, with `ProducibleOutcomes`
and `ReachableReasons` as its two halves) so no second derivation can drift — renamed
`node:<nodeId>:<packProbe>`. A row's `expectedNodes` entry witnesses the named node; the row's
required headline expectation witnesses the declared result node, because the composite headline is
a validated echo of that node's disposition. The covered detail says which witness it was.

**Two probes per edge, whatever devices the edge declares.** `edge:<index>:resolved` and
`edge:<index>:unresolved`. The evidence state an edge contributes and the fact-injection guard are
both pure functions of the upstream disposition kind, so a fact branch and an evidence branch would
have byte-identical witness sets; the missing sentence names what the branch does to each declared
device instead. The resolved branch is derived while the upstream's declarations can produce an
outcome, the unresolved branch while they can reach any non-outcome disposition — both read off the
upstream's own derived abilities. A resolved witness is a witness of the upstream node with kind
`outcome`; an unresolved witness has kind `unresolved` or `not-applicable`, enumerated rather than
negated. Witnesses are decode-gated expectations (`project.DecodeWitness`, the comparator's own
strict decoder), never feeds and never results.

**One graph-specific narrowing, and exactly one.** An edge-fed evidence requirement's state set is
exactly `{"present", the edge's onUnresolved value}`: a caller entry for an edge-fed requirement is
refused rather than merged, so nothing can widen it. Under the default `onUnresolved` the
requirement can never be absent and the node's `missing-required-evidence` probe is not derived —
deriving it would state an expectation no row could satisfy without mismatching, ADR-0014's own
rule. The narrowing enters the pack derivation through one seam (`project.Reach`), and it narrows
nothing else: no condition analysis, no fact-value reachability, no reading of what an injected
outcome id would compare equal to. That line is deliberate — condition analysis is the resolver's
job, and a coverage report that reimplemented half of §7 would be a second evaluator to keep honest.

**The invariants, inherited from ADR-0014 unchanged.** A missing probe is a fact, never a failed
row: coverage touches neither status nor summary nor exit code. There is no third status — a node
whose pack cannot be read, is not admitted, or does not decode derives nothing, silently, and so do
its edges. Absence of the member is not a claim. Every missing sentence stays inside the bounded
wording rules, and the unresolved-branch sentence carries the arbiter escape: an unconstructible
probe is a question for the policy text, never a row to force. `Coverage` is additive, so
`outputVersion` is unchanged under VERSIONING.md's machine-output rules. Everything here is part of
the experimental surface and vanishes with it (ADR-0015); nothing about it touches the conformance
claim, which is stated, in full and only, in CONFORMANCE.md.

This record supersedes no determination of ADR-0014 and adds no gate. It takes no position on where
graphs are declared — the project configuration question ADR-0015 decided is not reopened here.

### Consequences

- The shipped fixture now reports 13 probes, 7 covered, and its third row pins the screening node it
  is named for — the defect the report exists to make visible, found by the report.
- The circular-oracle admission, honestly: `expectedNodes` makes pasting a whole node disposition
  from a run easier than at pack level, and nothing mechanical closes that loop. The arbiter rule —
  expectations come from the policy text, a run only confirms encoding — currently lives in no graph
  prompt, because no `test_graph` prompt exists; growing `author_graph` to carry it is a method-layer
  follow-up (ADR-0008), deliberately not smuggled into this record.
- A rows document that is refused (empty, or over a structurally invalid graph) yields no payload at
  all, which is a different fact than a payload whose coverage member is absent; the payload doc
  comment says so.

## More information

`internal/graph/coverage.go` is the derivation; `internal/project/coverage.go` remains the pack
derivation and the shared entry points. ADR-0014 is the argument this record inherits; ADR-0015 is
the surface it reports on and vanishes with.
