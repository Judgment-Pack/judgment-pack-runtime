---
status: proposed
date: 2026-11-12
deciders: maintainer
---

# Admit Core 0.5.0, base profile, and retire the staged selector 0.4.0-draft

## Context and problem statement

Runtime 0.43.0 admitted the staged draft selector `0.4.0-draft` (ADR-0075), vendored from an
unreleased specification commit, for RFC 0031: annotations and root value sets. The specification
has since released that text as Core `0.5.0` (tag `v0.5.0`, commit
`171d4c21d4b13e03ee20e1adb6fc5cc6b182cfae`). Its §11 and release notes say that `0.4.0-draft` was
never released, that no `specVersion` `0.4.0` exists (the tag `v0.4.0` carries `0.3.0`), and that a
`0.5.0` reader refuses `0.4.0-draft` by its exact selector; a pack moves by changing its
`specVersion` to `"0.5.0"`.

The released text also adds what the staged text did not have: a refusal of line terminators beside
the pack version, local identifier, decimal operand and extension-name patterns; `expectedDiagnostics`
for a case that expects several findings; `readerProfiles` on the case manifest; the evaluation row
`annotations-supported-extension-set`, which the staged rows held; and `pairs` and
`ownEvaluationForm` on the evaluation manifest. The workflow and dialect companions stay on Core
`0.3.0`.

Material-decision impact: public-surface.

## Decision drivers

- Read exactly what the release defines, and invent no support a companion does not give.
- No byte of `0.3.0`, `0.2.0-draft` or `0.1.0-draft` behaviour changes.
- A refusal of a retired selector says where the pack goes.
- Remove code that only the staged selector used, rather than keep a second admission path for
  versions that no longer exist.

## Considered options

- Keep `0.4.0-draft` beside `0.5.0` for one release, refused only by a deprecation notice.
- Admit `0.5.0` by moving the staged reading to the released selector, and remove `0.4.0-draft`
  and the staged-only admission code.

## Decision outcome

Chosen option: the second, because the specification refuses `0.4.0-draft` by name, so a runtime
that still read it would read a pack the specification says no reader admits.

- **Bundle.** `internal/artifacts/jps/0.5.0` is vendored from the tag with its own lock (420 files).
  The `0.4.0-draft` bundle is removed. Every embedded bundle is a released version: a lock that
  states a `stage` is refused at load. `tools/sync-spec-artifacts --staged` stays, as a generic
  import; admitting a staged selector again needs its admission code back under its own decision.
- **Read.** Validation, `spec test-conformance`, the evaluator and `evaluate-corpus`, `compare`, the
  project walk and coverage, fact advice and matrix validation, examples, the pack view and the
  filtered view, and the MCP tools that reach them, read `0.5.0` with the `base` profile. Every
  surface but the view evaluates or checks a `0.5.0` pack as its evaluation form (§8.5); the view
  shows the document as written. The `extended` profile stays unsupported: its 18 document cases are
  reported skipped, with the profile refusal.
- **Refused.** Every graph command that reads node packs (graph validate, explain, test and
  evaluate, and the MCP graph-test tool) refuses a graph with a `0.5.0` node whole, before anything
  else it reads; graph list and the MCP graph inventory and get-graph tools read no node pack and
  list such a graph as declared. The dialect check and the policy check refuse a `0.5.0` pack. All of
  them use `JPS-SURFACE-SPEC-VERSION-UNSUPPORTED` (renamed from `JPS-STAGED-DRAFT-UNSUPPORTED`, which named a
  selector that no longer exists), and pack-design analysis with `pack-unsupported`: the graph
  prototype (Draft RFC 0002), the dialect and policy companions and Draft RFC 0027 are written
  against `0.3.0`.
- **Retired selectors.** A pack, a suite or a corpus run naming `0.4.0-draft` or `0.4.0` is refused
  as an unsupported version (`JPS-CAPABILITY-SPEC-VERSION`), and the message adds that the draft
  was released as `0.5.0` and how a pack moves. Evaluation, which cannot re-declare such a pack to
  `0.2.0-draft`, says the same.
- **Released rules taken up.** The line-terminator refusals are reported once, under the code and
  with the message of the definition's own pattern, never as a second generic finding.
  `expectedDiagnostics` is read: a case passes only when every listed finding is reported, findings
  compare as (code, pointer) pairs, and the list must begin with the case's `expectedDiagnostic`.
  `readerProfiles` decides which cases a `base` reader skips. The supported-extension-set row runs.
  The evaluation manifest's `pairs` and `ownEvaluationForm` are not checked by the corpus runner;
  every row runs and passes on its own.
- **Removed.** The staged suite status and held rows (`suiteStatus`, `held`), the staged examples,
  the `stage` member of the evaluator description and `sdk.Evaluator.Stage`.
- **MCP standalone contract.** A standalone `spec_version` may name `0.5.0` (base), as it names
  `0.3.0`: the disposition and evaluator rules of `0.5.0` are those of its evaluation form.
- **Defaults.** No default moves. Admitting `0.3.0` moved none (the pack-creating helpers kept
  their selectors), and admitting `0.5.0` follows that precedent.

### Consequences

- Good, because the runtime reads the released text and refuses what the specification refuses,
  with a message that says where a pack goes.
- Good, because one admission path remains: every bundle is a released version.
- Bad, because a `0.4.0-draft` pack that runtime 0.43.0 read is refused until its `specVersion` is
  changed, and the `sdk.Evaluator.Stage` field, the describe `stage` member and the suite
  `suiteStatus` and `held` members are gone (all EXPERIMENTAL).
- Bad, because the evaluation manifest's pairs and own-form statements are not yet checked.
- Revisit when the specification releases the `extended` profile's reader requirements, or a
  companion moves to `0.5.0`.

## More information

- [ADR-0075](0075-admit-the-staged-draft-selector-0-4-0-draft.md) (the staged admission this
  replaces), [ADR-0076](0076-show-annotation-text-recoverably-on-every-surface.md),
  [ADR-0078](0078-keep-annotation-text-from-reordering-its-line.md).
- [CONFORMANCE.md](../../CONFORMANCE.md), "Core 0.5.0 preparation evidence".
- The files ADR-0075 and ADR-0076 name under a staged name now carry the released one:
  `internal/validation/staged_structural.go` is `internal/validation/core050_structural.go`, and the
  view's test packs and expected views are under `sdk/view/testdata/packs/0.5.0` and
  `sdk/view/testdata/expected/0.5.0`.
