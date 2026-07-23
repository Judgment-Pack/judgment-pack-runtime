# Changelog

All notable changes to tagged releases are documented here.

## Unreleased

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

- The embedded specification bundle is still pinned to the pre-neutralization upstream tag
  (`protossai/judgment-pack-spec@v0.1.0-draft`). Re-vendoring to a neutral, digest-locked spec tag
  is pending the specification project publishing one.
- The Apache-2.0 `LICENSE` copyright-owner line and a `NOTICE` file are not yet filled in, and no
  `GOVERNANCE`/`MAINTAINERS`/`CODEOWNERS` files exist yet.
