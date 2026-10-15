# Pinned dialect document evidence

`provenance.json` records exact files from Judgment-Pack/judgment-pack-spec at
`9fb1142546e79ab4d03f744415afc098a81efe3e`, their original paths and SHA-256
checksums. `LICENSE` is that repository's license. The reference text is retained
for review, not fetched by the executable.

The twelve mapping/preparation pairs are the Spec's staged Runner vectors. Tests
include six distinct version-2 documents across ten vectors and two version-3
vectors. The unbound-at-import dialect and binding are also retained. Tests
compare this writer's mapping digest with each independently recorded
`preparation.mappingDigest`, and check every retained file's original checksum.
`upstream-manifest.json` is the unmodified staged vector manifest, including its
provenance and scope. The remaining preparation-verifier inputs are not vendored
because these tests do not prepare inputs or verify preparations. Passing these
checks is writer agreement on these cases, not a preparation-verifier claim.

The adversarial Go fixtures add number-literal, Unicode ordering/escaping,
duplicate-member, absence/null, byte/depth and protocol boundary coverage. They
are authored tests, separate from the independent upstream evidence.
