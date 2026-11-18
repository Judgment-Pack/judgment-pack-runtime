# Pinned registry 1.1.0 rows

`provenance.json` records exact files from Judgment-Pack/judgment-pack-spec at revision
`c47d2b4154785b92a644d3c90642f0db355916d8`, which merges the two pull requests that wrote them
([spec #383](https://github.com/Judgment-Pack/judgment-pack-spec/pull/383) and
[spec #384](https://github.com/Judgment-Pack/judgment-pack-spec/pull/384)): their original paths
and SHA-256 checksums. No release tag names them yet (`release` is "proposed (not tagged)").
`LICENSE` is that repository's license. The files are retained for this package's tests and
`internal/cli`'s, not read by the executable: the codes are written in `codes.go`, and a test holds
them to `1.1.0/codes.json`.

`1.1.0/` is `conformance/registry/1.1.0/` unmodified: `codes.json`, the code list of the text's
§12; `manifest.json`, its 158 rows; and the documents they name. `1.1.0/schema/` is
`schema/registry/1.1.0/` unmodified, the closed schemas of the format's documents.

`TestRows` runs every row of a class this package implements, the assembler's 46 and the index
checker's 5, and holds a list of the classes not built yet (the lock writer, the lock checker and
the lock signature verifier); it fails when a listed class is built, or when a class is neither
listed nor run. `TestRegistryAssembleRows` and `TestRegistryCheckIndexRows` in `internal/cli` run
the same rows through `jpack registry assemble` and `jpack registry check-index`.

The rows' second reader, `tools/registry_1_1_0_reader.py` in the Spec repository, gives the same
result on every row, but it, the rows and this reader are by the same vendor: agreement on these
rows is not a cross-vendor claim, and not agreement between independently written readers.
