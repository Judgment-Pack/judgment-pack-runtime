# Changelog

All notable changes to tagged releases are documented here.

## Unreleased

- **Claim evaluator conformance against JPS Core `0.2.0-draft`** (ADR-0011), in the one form §3.4.1
  permits and in one place only: the new root [`CONFORMANCE.md`](CONFORMANCE.md), which names the class,
  the exact `specVersion`, the corpus `suiteVersion` `0.2.0-draft`, the results obtained — **every one of
  its twenty rows passed**, no erratum cited because none exists — and, at the same length, what the
  claim does not assert. The evidence: the bundled corpus 20/20 byte-exact, a clean-room Python lineage
  independently reproducing all twenty rows, and 20/20 byte-agreement between the two implementations;
  both trace to one maintainer's direction, so that agreement corroborates rather than independently
  confirms, and the corpus is a twenty-row seed whose own gap list the ADR quotes. The claim is scoped
  to one exact version and is not inherited (§11), asserts compliance for every input this evaluator
  admits and not merely the ones it ran, and asserts nothing whatever about a pack, its facts, its
  evidence, an authorization, or the wisdom of acting on a disposition (§3.5).
  - **The §10 limits the class requires are supplied first**, which is the precondition ADR-0010
    deferred and named. `resource-exhaustion` is now reachable on the ordinary Core path and not only
    under `--rfc0008-quantifiers`: an **evaluation-work limit of 20,000,000 units** per evaluation
    (`DefaultCoreWorkLimit`, configurable per evaluation) is charged over every condition node, §8
    iteration, pointer resolution, and compared byte, using the accounting model the draft-RFC prototype
    already documented — so one model now serves both paths, plus one unit per authored evidence
    requirement, exception, and rule, charged before §8 step 1. Each condition tree's charge completes
    before any predicate in it runs, so an exhausted limit is an explicit `resource-exhaustion` error in
    the `evaluation` phase with no disposition and no partial state, never a truncated result. The
    number is derived from the admission limits rather than picked, and every row of the bundled corpus
    charges under 1,000 units.
  - The **collection-size limit is the carrier's 250,000-node cap**, documented as the §10 limit it is
    rather than duplicated as a second mechanism: every input is admitted under that cap, so no admitted
    document holds a larger collection and Core constructs none of its own. Reaching it is
    `malformed-input` in the `preflight` phase, which is §10's own phase split and stricter than an
    evaluation-phase check of the same bound.
  - **The `experimental` namespace stays and no command is renamed**; what changes is what it says.
    "Experimental" now means only what it always meant about stability — this surface may change or be
    removed without compatibility promise — and the blanket no-claim wording is replaced by a pointer to
    the claim and its exact scope on every surface it appeared on: the CLI help, the human output header,
    the corpus label, the MCP tool descriptions, the `test_pack` prompt, the README, and the docs.
    **Breaking for a consumer of the experimental payload:** the in-band `conformanceClaim` member was
    `"none"` and is now `"evaluator-conformance:0.2.0-draft"`, naming the claim and the exact version it
    is scoped to.
- Bundle JPS `0.2.0-draft` alongside `0.1.0-draft`, imported from the specification tag
  `v0.2.0-draft` with the maintainer tool. The new bundle adds the evaluation corpus of §3.4.1 — its
  manifest, that manifest's schema, and the four pack fixtures its twenty rows name — which the
  import tool now carries for any specification version that publishes one. `spec validate` accepts a
  pack declaring either version and still selects the schema by the version the document itself
  declares; `spec schema`, `spec test-conformance --spec-version`, `version`, `get_schema`, and
  `describe_runtime` all reach both. **No default changes**: every surface that selects a version for
  a caller who named none still selects `0.1.0-draft`. The release gate now verifies the lock of
  every bundled version rather than only the default one, and the one `artifactProvenance` string in
  the runtime description now describes every bundle, so a development snapshot in a
  non-default version can no longer hide behind the default version's provenance.
- Align the EXPERIMENTAL evaluator to the evaluator conformance class of Core `0.2.0-draft`
  (ADR-0010), under the `experimental` namespace and claiming nothing *as that change landed* — JPS
  §3.4.1 defines the one form an evaluator-conformance claim may take, and taking that decision was
  left to a separate ADR, which the entry below is. The contract is applied to a
  pack of either bundled version whatever that pack declares, so every evaluation payload and every
  `evaluationError` now names the contract's own version in a new `evaluatorSpecVersion` member beside
  the pack's `specVersion`: §11 says these semantics existed for no consumer under `0.1.0-draft`, so a
  payload carrying only the pack's version would read as a `0.1.0-draft` disposition, which does not
  exist. Three normative sections are implemented:
  - **§8.2 input preflight.** Inputs are admitted in Core's order — pack, facts, evidence,
    required-extension set — and the preflight completes before §8 step 1, so no result can outrace
    an input error: a pack whose applicability is false, presented with an evidence document carrying
    an undeclared key, is now the input error rather than the `not-applicable` disposition. An omitted
    evidence document is the implicit empty object, the only form its absence takes.
  - **§8.3 portable disposition.** Under `--format json` without `--pretty`, the `disposition` member
    is now written in its RFC 8785 canonical form, produced by a new stdlib-only `internal/jcs` encoder
    over the strings, arrays, and objects a disposition is made of. `--pretty` indents the whole
    payload and reaches inside that member too, so under it the member order and both sets stay
    canonical and the exact bytes do not appear; §8.3 requires canonicalization where a byte comparison
    is required, so a comparison recanonicalizes either side it did not produce. `handoff` gains
    `triggeredBy`, the retained reasons that triggered a request, present exactly when the state is
    `requested`. Canonicalization is also the one place that refuses a disposition §8.3 forbids, so no
    value assembled outside the engine can violate any invariant that section states about the
    disposition alone: the `kind`, `handoff.state`, and reason vocabularies, its three presence rules,
    the exact reason set of a `not-applicable` result, and `triggeredBy` being a subset of `reasons`.
    **Breaking for a consumer of the experimental payload:** the disposition no longer echoes the
    pack's escalation target, which §8.3 keeps out of it; the target moved to a sibling
    `handoffTarget` member. Human output is unchanged prose, now naming the triggering reasons.
  - **§8.4 error contract.** Every refused evaluation reports exactly one of the four Core classes in
    band, as a new `evaluationError` member carrying `class` and `phase`, with no disposition at all;
    the existing `JPS-*` codes stay beside the class as the finer detail. The classes are evaluated in
    Core's fixed order, which is the preflight order, so an unsupported required extension is now
    reported as `unsupported-required-extension` rather than folded into
    `JPS-EVALUATION-PACK-NOT-CONFORMANT`, and a malformed input outranks it on the same inputs.
    Reaching a limit while admitting an input is `malformed-input`, and that now includes the input
    byte limit on every surface: the CLI reports an oversized input inside the preflight instead of
    refusing it at the read, so it carries a class and takes its place in the fixed order — an
    oversized facts document no longer outranks a non-conformant pack. `resource-exhaustion` is the one
    class of the evaluation phase, and as this change left it, it was reached only by the
    `--rfc0008-quantifiers` work budget — no evaluation-work charge was levied on the Core path at all.
    The conformance-claim entry below closes that gap in the same release, so the `resource-exhaustion`
    a consumer sees is the Core path's too. Both surfaces report the same envelope: an `experimental_evaluate` refusal
    is an in-band MCP tool error whose `structuredContent` is the `evaluationError` payload the CLI
    writes, so the class, the phase, and `evaluatorSpecVersion` are machine-readable there too rather
    than sentences a client has to classify itself.
  - **§8.2 on the MCP surface.** `experimental_evaluate` now decodes each document argument's presence
    separately from its value, so the two meanings §8.2 gives an absent and an empty evidence document
    stay apart on the wire: omitting the key is the implicit empty object and evaluates, while a key
    present with an empty string is a supplied document and is `malformed-input`. A present-but-empty
    `pack` or `facts` likewise enters the preflight — `pack-not-conformant` and `malformed-input` —
    instead of being reported as an unclassified missing argument, an absent required key stays an
    invocation failure with no class, and an explicit `null` is rejected as the argument-type error the
    declared string schema makes it.
- Add `judgment-pack experimental evaluate-corpus`, which runs the bundled evaluation corpus for one
  exact specification version and reports every row: the disposition compared as §8.3 defines
  disposition equality — both sides through the same canonicalizer, so a set's stored order is not a
  difference — or the expected §8.4 class and phase. Its output is labelled as corpus results in both
  formats and a mismatch exits 1. **A run is not the claim**: §3.4.1 makes corpus results the required
  and non-exhaustive evidence for the one claim it permits, and that claim is `CONFORMANCE.md`'s (see
  the entry below). The bundled `suiteVersion` `0.2.0-draft` corpus runs clean in this build, 20 rows
  and 20 passed; run the verb for the current result, since a mismatching row would decide nothing by
  itself (§3.4). The verb is CLI only; the MCP surface does not expose it.

## 0.3.0 - 2026-07-28

- Add a DRAFT-RFC PROTOTYPE of the specification's RFC 0008 (Draft), bounded collection quantifiers,
  behind the new `judgment-pack experimental evaluate --rfc0008-quantifiers` flag (ADR-0009, under
  ADR-0007's experimental umbrella). The flag admits three condition operators — `exists`, `every`,
  and `uniform` — with the RFC's element re-rooting, its pinned empty-array values, its
  aggregate-depth bound of two, and a candidate work-accounting model the RFC itself leaves open.
  Every successful evaluation payload produced this way carries a new output member,
  `draftPrototype`, naming the operators used and stating that a pack using one is NOT valid under
  any published JPS version; a refusal carries no such member, because a grammar or work-limit
  failure is reported through the ordinary operational-error envelope. Four diagnostic codes are
  minted, `codeStability: "provisional"` like every other: `JPS-EVALUATION-RFC0008-GRAMMAR`,
  `JPS-EVALUATION-RFC0008-DEPTH`, `JPS-EVALUATION-RFC0008-SHAPE`, and
  `JPS-RESOURCE-EVALUATION-WORK-LIMIT`. **No conformance claim and no validation behavior changes**:
  `spec validate` is untouched and still rejects every pack using an operator, the evaluator without
  the flag refuses one for the same reason, the MCP surface does not expose the flag, and everything
  the draft grammar does not add is still held to full document conformance through the pack's Core
  projection. The operators belong to an open proposal that may never be accepted.
- Make §7.4 equality total in the EXPERIMENTAL evaluator. JSON numbers are compared as normalized
  tokens — sign, significant digits, adjusted exponent — rather than through arbitrary-precision
  arithmetic, so `1e3`, `1000`, and `1.0e3` are one value, `-0` equals `0`, and a pair no arithmetic
  type can hold (`1e999999999` against `2e999999999`) compares `false` instead of degrading to
  `unknown`. `equals`, `not-equals`, and `in` therefore always decide, and `uniform` needs no arm
  beyond RFC 0008's five clauses: an evaluator's inability to represent an admitted value is a
  resource condition, not a semantics two implementations would have to agree on. Ordered comparison
  is unchanged — still defined only over §2.2 decimal strings — and `spec validate`, the conformance
  corpus, and the exit classes are untouched.
- Add the MCP `prompts` capability (ADR-0008): three non-normative authoring-method prompts —
  `author_pack`, `test_pack`, `fix_pack` — served as static, versioned text that the client's model
  executes with the client's key. The server still calls no model, holds no key, and opens no
  network connection; serving a prompt is read-only, like `get_schema`. Method content distills the
  expressiveness studies: the resolution-model shapes that avoid conflicts, the `onUnknown`
  discipline, the decimal-string rule for ordered comparisons, the prepared-facts ledger, and the
  instance-matrix logic probe. Following a prompt does not make a pack conformant, and no prompt
  interprets any policy.

## 0.2.0 - 2026-07-27

- Add an EXPERIMENTAL evaluator (ADR-0007): `judgment-pack experimental evaluate` and the
  `experimental_evaluate` MCP tool apply the JPS Core §§7–8 experiment, as pinned by the
  specification's RFC 0006 (Draft), to one conformant pack, one facts document, and an optional
  tri-state evidence-availability input, returning a disposition (kind, reasons, handoff) and a
  trace. It claims NO evaluator conformance — JPS `0.1.0-draft` forbids such claims — authorizes
  nothing, evaluates only fully conformant packs, and may change or be removed without
  compatibility promise. The nine walked instances committed in RFC 0006's appendix run as this
  surface's acceptance tests. Validation behavior, claims, and exit classes are unchanged.

## 0.1.0 - 2026-07-25

- Add a `NOTICE` file identifying Brian Jin as the copyright holder and carrying
  the attribution required for the embedded Judgment Pack Specification artifacts, and ship it in
  the release archives.
- Record the embedded specification bundle in `THIRD_PARTY_NOTICES`. The bundle is Apache-2.0
  material from a separate project and appears in neither `go.mod` nor `go.sum`, so the previous
  Go-modules-only framing omitted it.
- Re-vendor the embedded specification bundle from `Judgment-Pack/judgment-pack-spec`, replacing the
  pre-neutralization pin. The two schema `$id` values now carry the permanent
  `https://judgmentpack.org/schema/` identifiers. Machine-visible: `spec schema` reports a new
  `schemaId`, `sha256`, and `bytes`, and the bundle digest changes. No validation behavior changes
  and all 47 conformance cases still pass — the corpus is byte-identical to the previous pin.
- Pin the bundle to an exact commit rather than a tag. The specification's release tooling requires
  a tag string to equal `specVersion`, so the permanent identifiers on its `main` branch cannot be
  republished under a second `0.1.0-draft` tag. The release gate already treats a full-length commit
  digest as an immutable reference; the pin moves back to a tag once a specification version
  carrying those identifiers is published.
- Pin the machine contract in tests: `outputVersion`, `tool.name`, the six exit classes, and the
  four envelope members every JSON payload carries. These were previously enforced only by
  convention, so a rename or an accidental version bump passed silently.
- Add a `judgment-pack mcp` subcommand: a hand-rolled, dependency-free Model Context Protocol server
  over stdio that exposes the offline validation, conformance, and description operations as MCP
  tools. It holds no credential, opens no network connection, and evaluates nothing. See ADR-0003.
- Make validator diagnostics self-sufficient. Structural and carrier messages now name the offending
  value and the fix — the allowed enum values, the identifier grammar with an example, the required
  and actual collection counts, the valid condition `op` values, the exact effect/operand mismatch for
  an exception, and (for carrier parse errors) the line, column, and byte offset with the decoder's
  reason. Messages and provisional codes only; all 47 conformance cases still pass and the
  machine-output contract is unchanged.
- Unify diagnostic code prefixes under `JPS-`. The CLI-invocation codes previously prefixed `JPR-`
  are now `JPS-INVOCATION-*`; the process exit class, not the prefix, carries the error category.
  Codes remain provisional. See ADR-0005.
- Populate the `extensions` summary in `spec validate` JSON output even when a document is invalid at
  the semantic layer, so a declared required extension is no longer dropped from the summary.
- Surface the embedded valid conformance fixtures read-only: `list_examples` / `get_example` MCP
  tools and a `spec examples [name] [--write …]` CLI command. They give a filesystem-less client a
  conformant starting point for Create, return the exact digest-locked bytes the bundle embeds, and
  label every payload `kind: version-pinned-conformance-fixture` so a fixture is never mistaken for
  an authored template. Read-only, offline, keyless, dependency-free. See ADR-0006 and the new
  `docs/authoring-lifecycle.md`.

## 0.0.1 - 2026-07-23

- Establish `judgment-pack-runtime` as the vendor-neutral reference runtime for the Judgment Pack
  Specification (JPS) under the `Judgment-Pack` organization. This project originated as the
  `protoss-cli` reference validator and was renamed and relocated to a vendor-neutral home; it is a
  reference implementation, not the only valid one.
- Provide the offline `judgment-pack spec` command namespace (`validate`, `test-conformance`,
  `schema`) plus `version`, built as the `judgment-pack` binary with a `jpack` short alias.
- Implement strict carrier, structural, semantic, and extension-capability validation. The runtime
  validates documents only; it does not evaluate rules, choose an outcome, or authorize an action,
  matching the JPS `0.1.0-draft` scope (no evaluator conformance class).
- Embed and integrity-check JPS `v0.1.0-draft`, pinned byte-for-byte to its immutable upstream
  release tag.
- Provide bundled and local conformance-suite execution, versioned JSON output, and stable process
  exit classes.
- Provide cross-platform release archives, SHA-256 checksums, and build-provenance attestations.

### Known follow-ups

- The embedded specification bundle is pinned to the pre-neutralization upstream tag
  (`v0.1.0-draft`), whose schema `$id` values use temporary repository-hosted URLs. (Resolved in
  Unreleased by re-vendoring from `Judgment-Pack/judgment-pack-spec` at an exact commit. The
  original entry attributed the delay to the specification project not having published a neutral
  tag; the permanent identifiers were in fact already on its `main` branch.)
- No `GOVERNANCE`/`MAINTAINERS`/`CODEOWNERS` files exist yet. (The `NOTICE` file is added in
  Unreleased. `LICENSE` is deliberately left byte-identical to the canonical Apache-2.0 text: its
  appendix is a template for marking your own files, not a field to fill, and editing it costs a
  clean Apache-2.0 match on automated license scanners. The copyright holder is named in `NOTICE`.)
