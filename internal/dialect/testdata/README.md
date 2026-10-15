# Pinned dialect document evidence

`provenance.json` records exact files from Judgment-Pack/judgment-pack-spec at
`065aade751fb3a510872842ddb88e09b55574165`, their original paths and SHA-256
checksums. `LICENSE` is that repository's license. The reference text is retained
for review, not fetched by the executable.

The ten mapping/preparation pairs are the Spec's staged Runner vectors. Tests
compare this writer's mapping digest with each independently recorded
`preparation.mappingDigest`, and check every retained file's original checksum.
`upstream-manifest.json` is the unmodified staged vector manifest, including its
provenance and scope. The remaining preparation-verifier inputs are not vendored
because these tests do not prepare inputs or verify preparations. Passing these
checks is writer agreement on these cases, not a preparation-verifier claim.

The adversarial Go fixtures add number-literal, Unicode ordering/escaping,
duplicate-member, absence/null, byte/depth and protocol boundary coverage. They
are authored tests, separate from the independent upstream evidence.
