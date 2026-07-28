# Changelog

All notable changes to tagged releases are documented here.

## Unreleased

- Add a DRAFT-RFC PROTOTYPE of the specification's RFC 0008 (Draft), bounded collection quantifiers,
  behind the new `judgment-pack experimental evaluate --rfc0008-quantifiers` flag (ADR-0009, under
  ADR-0007's experimental umbrella). The flag admits three condition operators — `exists`, `every`,
  and `uniform` — with the RFC's element re-rooting, its pinned empty-array values, its
  aggregate-depth bound of two, and a candidate work-accounting model the RFC itself leaves open.
  Every result produced this way carries a new output member, `draftPrototype`, naming the operators
  used and stating that a pack using one is NOT valid under any published JPS version. Three
  diagnostic codes are minted, `codeStability: "provisional"` like every other:
  `JPS-EVALUATION-RFC0008-GRAMMAR`, `JPS-EVALUATION-RFC0008-DEPTH`/`-SHAPE`, and
  `JPS-RESOURCE-EVALUATION-WORK-LIMIT`. **No conformance claim and no validation behavior changes**:
  `spec validate` is untouched and still rejects every pack using an operator, the evaluator without
  the flag refuses one for the same reason, the MCP surface does not expose the flag, and everything
  the draft grammar does not add is still held to full document conformance through the pack's Core
  projection. The operators belong to an open proposal that may never be accepted.
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
