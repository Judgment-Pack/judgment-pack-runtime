# Retained-column installation state

`official/` retains the exact original paths from official Spec `v0.3.0-rc.2`,
source `bcac15b7537ce21879201b5e5dfe795496225121`. `provenance.json` records the
verified archive/inventory identities and each imported file's bytes and digest.
The independently compiled provenance pin and selected tuple are in
`retained_column_artifacts.go`; admission and release checks verify them.

The selected role is document validation for Base/Extended, with declared pack
inventory coverage only. Local byte verification establishes no producer authority,
execution, preparation verification or final Core conformance. External immutable
publication/tag/attestation observations remain separate release evidence.
Historical Draft resources remain under `testdata/retained-column-draft` unchanged.
