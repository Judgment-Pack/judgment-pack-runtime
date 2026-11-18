# Pinned registry 1.1.0 rows

`provenance.json` records exact files from Judgment-Pack/judgment-pack-spec at revision
`7bc0874c719985f6909b3bc606e238773f4aeacd`, which merges the four pull requests that wrote them
([spec #383](https://github.com/Judgment-Pack/judgment-pack-spec/pull/383),
[spec #384](https://github.com/Judgment-Pack/judgment-pack-spec/pull/384),
[spec #386](https://github.com/Judgment-Pack/judgment-pack-spec/pull/386) and
[spec #387](https://github.com/Judgment-Pack/judgment-pack-spec/pull/387)): their original paths
and SHA-256 checksums. No release tag names them yet (`release` is "proposed (not tagged)").
`LICENSE` is that repository's license. The files are retained for this package's tests and
`internal/cli`'s, not read by the executable: the codes are written in `codes.go`, and a test holds
them to `1.1.0/codes.json`.

`1.1.0/` is `conformance/registry/1.1.0/` unmodified: `codes.json`, the code list of the text's
§12; `manifest.json`, its 162 rows; and the documents they name. `1.1.0/schema/` is
`schema/registry/1.1.0/` unmodified, the closed schemas of the format's documents.

`TestRows` runs every row of every class of §15: the assembler's 47, the index checker's 5, the
lock writer's 11, the lock checker's 36 and the lock signature verifier's 63. It fails when a class
is not run, or has another count. The tests in `internal/cli` run the same rows through the
`jpack registry` commands: `assemble`, `check-index`, `lock write`, `lock check` and `lock verify`.

The rows' second reader, `tools/registry_1_1_0_reader.py` in the Spec repository, gives the same
result on every row, but it, the rows and this reader are by the same vendor: agreement on these
rows is not a cross-vendor claim, and not agreement between independently written readers.
