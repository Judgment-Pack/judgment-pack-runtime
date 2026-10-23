---
status: proposed
date: 2026-10-23
deciders: maintainer
---

# Retain every actual outcome contributor outside the disposition

## Context and problem statement

Two true unsuppressed rules choosing the same outcome both contribute under Core.
Choosing one ID for attribution discards evidence even when the disposition is correct.
Issue #305 originally requested one representative; the maintainer instead requires
complete credit, aligned with the proposed complete-attribution contract in Spec #251,
[unreleased Draft RFC 0028](https://github.com/Judgment-Pack/judgment-pack-spec/blob/84c5b5db8a2eb0f38e000a66375bebbb6f3d6157/rfcs/0028-complete-workflow-contributors.md).
That proposal supersedes the earlier workflow draft's least-ID representative convention.

## Decision drivers

- Preserve every actual contributing rule or compatible forcing exception.
- Keep Core resolution, work accounting and the closed disposition unchanged.
- Retain exact localIds and distinguish the rule and exception namespaces.
- Preserve historical audit bytes and the trace's complete document order.

## Considered options

- Pick the first, last or least-ID representative.
- Require consumers to reconstruct membership independently from the full trace.
- Supply the complete typed list beside the disposition and retain it in the audit.

## Decision outcome

Supply optional `contributors: [{kind, id}]` metadata on completed pack evaluations,
their graph node views and their audit evaluation records. Each member has exactly
`kind: "rule"` or `kind: "forcing-exception"`, and the exact Core localId. Sort by kind
then Unicode code-point ID. No representative or redundant count is added: the array's
length is the count. One successful evaluation has a homogeneous kind because forcing
exceptions skip normal rules. Multiple compatible forcing exceptions all receive credit.

Derive membership after successful resolution, from its final disposition and complete
trace. A rule must have evaluated true, supplied that outcome and been neither skipped
nor suppressed. An exception must have evaluated true with `force-outcome` and supplied
that outcome. Stage and effect matter; outcome equality or condition truth alone is
insufficient. Fallback outcomes, non-outcomes, evaluation failures and unresolved draft
outcome values receive no attribution. Admitted localIds are unique within each namespace;
the resolver records each authored entry once. Metadata derivation adds no evaluation
work charge, rule priority or new contributor bound.

The machine-output and audit record versions remain unchanged because this is additive.
Historical audit lines are never rewritten; absence on an older line means attribution
is unavailable, not that a complete empty set was observed. New fallback/non-outcome
evaluations omit the member rather than carrying an empty or null array. Existing chains
and signatures cover contributor bytes when present, without proving causal truth.

### Consequences

- CLI JSON and both MCP serializations expose every actual contributor. Human CLI output
  keeps the document-ordered trace. Graph nodes and node audit records retain their own
  pack's evidence; graph headlines and composite records invent no aggregate attribution.
  Matrix/corpus and pack-comparison summaries keep their narrower existing contracts.
- Core `0.3.0` graph reports count the added bytes toward their existing report limit;
  reports near it can be refused earlier. Existing admission, evaluation-work and audit
  line bounds remain. No contributor is truncated or elected to fit an output limit.
- Complete attribution can disclose more pack structure than representative attribution.
  IDs are already in the evaluated pack and trace; public examples remain synthetic.
- A workflow consumer must separately enforce its model-2 4096-member and 65536-byte
  envelope ceilings. Runtime evidence alone does not imply every evaluation fits that
  envelope and does not implement workflow execution version 3. No frozen Spec artifact
  or Core conformance claim is changed by this metadata.
- Hand-authored memberships, exact audit bytes, transport/node regressions, historical
  goldens and a contributor-removal mutation verify the evidence boundary. Independent
  replay remains necessary to establish completeness against untrusted retained output.

## More information

[Architecture](../architecture.md),
[audit guide](../building-with-packs.md#keeping-a-record-of-what-you-decided), and
[trace contract](0027-pin-the-evaluation-trace-contract.md).
