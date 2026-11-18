# Pinned terraform-file vectors

`provenance.json` records exact files from Judgment-Pack/judgment-pack-spec at revision
`866ba96ad832e54e0beaa105a388e575b2d2874c`, the head of the pull request that adds them
([spec #373](https://github.com/Judgment-Pack/judgment-pack-spec/pull/373)): their original paths
and SHA-256 checksums. No release carries them yet, so `release` is `null`; the pin moves to the
commit that merges them, and to the `terraform-file-v1.0.0` tag once it is published. `LICENSE` is
that repository's license. The files are retained for this package's tests and `internal/cli`'s,
not read by the executable: the reader's table and codes are written in `codes.go`, and a test
holds them to `1.0.0/formats.json` and `1.0.0/codes.json`.

`1.0.0/` is `conformance/terraform-file/1.0.0/` unmodified: `codes.json`, the code list of the
companion's §9; `formats.json`, the accepted table of its §4, release `1.0.0`; `manifest.json`,
its 188 vectors; `declarations/`, the declarations they name; and `vectors/`, their inputs and
expected results. The Spec proposes them with the companion text (`spec/terraform-file.md`,
reading `1.0.0`, proposed): until a terraform-file tag releases them they are not a conformance
corpus, are in no bundle, and back no claim.

`TestSpecVectors` runs every `read` and `read-stream` row through `Read` and `ReadStream`, 144 in
all, and `TestTerraformFactsSpecVectors` in `internal/cli` runs the 8 `batch` rows through `jpack
terraform facts`. Three rows' inputs are not committed: a file of about 1 MiB, and files of the
default `maxBytes`, 67108864 bytes, and one byte more. The tests build them by the manifest's
`generate` rule. A row whose declaration is malformed reads no file, and this runtime reads no
declaration document, so those 36 rows run through the command's flags where a command line can
write the declaration (20 of them, each an unusable command line, exit 3), and the other 16 are not
run: the test logs each one's reason.

This runtime is the reading's one reader, and the vectors and the reader are by the same
maintainer: agreement on these rows is not agreement between independent implementations.
