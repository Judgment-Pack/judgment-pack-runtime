# Pinned annotation-vocabulary rows

`provenance.json` records exact files from Judgment-Pack/judgment-pack-spec at release `v0.5.2`,
revision `561b2a7245cf6ee13cb5be78da9c7a89f9adcdef`: their original paths and SHA-256 checksums.
`LICENSE` is that repository's license. The files are retained for review and for this package's
tests, not read by the executable: the checker's levels, codes and selector are written in
`vocabulary.go`, and a test holds them to `1.0.0/vocabulary.json`.

`1.0.0/` is `conformance/annotation-vocabulary/1.0.0/` unmodified: `vocabulary.json`, the
machine-readable form of annotation vocabulary 1.0.0; `manifest.json`, its 32 rows and its one
inertness pair; and `packs/`, the pack each row names. The Spec stages these rows: its manifest
says they are not a conformance corpus, are in no bundle, and back no claim, and the vocabulary is
`proposed`.

The tests run every row through the checker as `packs validate` runs it, and through
`sdk.Pack.CheckVocabulary` for each row `sdk.Load` admits. One row, `profile-extended`, is not run:
its expectations are those of a reader that supports the extended profile, and this runtime reads
the base profile only, so it reports the vocabulary not applied, for the reason
`profile-unsupported`, as the row says such a checker does. The pair's three packs are evaluated
with its five facts documents, and the companion's removal of the key (§2.6) is applied to every
row's pack Core 0.5.0 admits.

This runtime is the vocabulary's second reader, after the Spec's own, and both are by the same
maintainer: agreement on these rows is not agreement between independent implementations.
