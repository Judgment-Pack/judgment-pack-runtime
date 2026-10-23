---
status: accepted
date: 2026-10-23
deciders:
---

# Rehearse an explicit finite domain for optional pack-design advice

## Context and problem statement

Draft Spec RFC 0027 proposes six advisory families beyond the existing
Core 0.3.0 different-outcome overlap check. A finite sample is insufficient for
universal reachability or redundancy claims unless the caller explicitly declares
that exact finite set as the domain. Resolution gates and unknowns must remain
those of the evaluator, including candidates in unresolved conflicts.

## Decision outcome

Add `jpack experimental analyze-pack PACK --domain DOMAIN` and MCP
`experimental_analyze_pack`, backed by `internal/packadvice`. They consume exact
pack/domain JSON bytes, rehearse Core 0.3.0 base inputs read-only and return the
closed Draft RFC 0027 format-1 report. The proposal source is Spec commit
`5bc71c1cafbb3df2ac045e90326716c6e6c5dd83`; its unchanged schema is retained only
as a test oracle. This is not acceptance or incorporation of that Draft, a Core
0.4.0 implementation, or a new conformance claim. The optional report may represent
a narrower set of inputs than Core: named rule, exception and requirement IDs
must fit its 256-character source-ID bound. Core local IDs are ASCII. Packs with
longer IDs or more than 256 rules or exceptions are refused as unsupported by
this optional analyzer; document conformance is unaffected. Evidence requirements
have no new count ceiling beyond existing pack carrier admission.

Admission uses the real engine's `AdmitPack` once, with no declared extension
capabilities, and `EvaluateAdmitted` once per checked domain input. Required
unsupported extensions and unsupported profiles/versions refuse the report.
Actual rule trace entries whose condition is true enter the candidate set,
including during conflict or unknown blocking. Candidate contribution is local
to this advisory contract and does not assert successful final attribution.
Skipped/suppressed entries supply no condition truth or counterfactual candidate.
When no input evaluates a rule condition true and some inputs skip it, decline
`condition-never-true` with an `unsupported-condition` limitation. Actual complete
gate-aware traces can still prove `never-contributes`. One actual true condition
disproves never-true without inspecting skipped conditions.

The six families report compatible agreement, outcome redundancy, coverage gaps,
condition/contribution unreachability, exception interaction and unknown blocking.
Witnesses prove existential effects only. Universal findings require every input
and relevant predicate to finish; a work interruption drops all universal claims,
retaining already established witnesses. Redundancy requires at least one true
contribution and another same-outcome candidate at every contribution, possibly
different covering rules on different inputs. It never authorizes deletion:
unknown behavior, citations, exception references and attribution can differ.
A caller's intentional-fallback declaration affects only fallback severity in
that exact domain, never no-match or unknown blocking. Force bypass source groups
split at 255 rules plus their exception, preserving the schema's 256-source cap.

Domain admission requires retained UTF-8 bytes at most 1 MiB, depth 32 (root 0),
16384 value nodes, strings/member names at most 65536 UTF-8 bytes, at most 4096
ASCII digits per number token, 1..64 complete `{facts,evidence}` inputs and at
most 256 evidence entries per input. Duplicates, unpaired surrogates, malformed
values and undeclared evidence IDs refuse. Exact numbers use `json.Number`;
neither digest is derived from reserialization. The report echoes the parsed
domain and hashes the exact retained pack/domain byte strings separately.

Analyzer limits are `maxWork` 1..100000 and `maxFindings` 1..256. Charge before
each evaluator invocation, before each input/rule contribution and condition
predicate, before each input/ordered-rule-pair redundancy predicate, before each
input/unordered-rule-pair agreement and exception-pair conflict predicate, and
before each input/exception effect, root coverage/applicability-unknown or named
entity unknown predicate. Every logical cached-result check still pays its
charge. Pairs are generated incrementally, never as an allocated Cartesian
product. Linear aggregation of checked results is uncharged. The work cap does
not bound evaluator internals, total CPU or elapsed time: the evaluator retains
its independent default 20971520-unit Core work limit and carrier admission.

Report shape and serialized size are checked before marshaling, stopping at the
same depth/node/string/number and 1 MiB ceilings as the domain. Reserve one finding
slot and enough output room for the incomplete marker and limitation. The compact
CLI JSON document also reserves its trailing newline. Optional `--pretty` does
not indent this bounded report. A report exceeding a finding/shape/byte limit
retains whole findings only, sets `complete:false`, `findingsTruncated:true` and
`output-limit`, and appends `JPS-ANALYSIS-INCOMPLETE`. Work limits similarly carry
an explicit marker but do not set findingsTruncated. Per-rule declined-check
limitations share a root limitation when individual entries exhaust their
reserved capacity. If the domain echo leaves no room for a minimal incomplete
report, refuse the entire analysis. A post-admission evaluator failure also
refuses the entire report, never returning earlier partial results.

Refusals are bounded `{status:"unavailable",code,message}` objects using
`domain-invalid`, `domain-limit`, `pack-unsupported` or `analysis-limit`. These
are analyzer explanation categories, not Core evaluation error classes. No
refusal includes a report. CLI refused analyses exit 1; complete and incomplete
reports exit 0. MCP returns these objects as successful tool results; malformed
tool arguments remain tool errors. Both adapters share the same report/refusal
implementation. Neither reads project configuration, appends an audit record,
changes validation/strict/publication gates, or changes default evaluation.

## Consequences

Authors receive bounded proof-scoped advice with precise uncertainty. Some
Core-admitted packs or domains cannot produce an optional report. A skipped
condition can make the report incomplete even when never-contributes is proved.
A finding slot stays reserved conservatively even if later clean completion
could have fitted an additional finding. Symbolic solving, sampled universal
proof, automatic deletion and new strict/publication gates are excluded.

## More information

- [Runtime issue 314](https://github.com/Judgment-Pack/judgment-pack-runtime/issues/314)
- [Draft RFC 0027 source](https://github.com/Judgment-Pack/judgment-pack-spec/blob/5bc71c1cafbb3df2ac045e90326716c6e6c5dd83/rfcs/0027-pack-design-advisories.md)
- [Existing overlap advice](0066-bounded-rule-overlap-advice.md)
