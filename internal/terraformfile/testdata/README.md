# Pinned terraform-file vectors

`provenance.json` records exact files from Judgment-Pack/judgment-pack-spec at revision
`5d7feab2b51b0f5ec81b5ce8d616f9e4045c62af`, the commit the `terraform-file-v1.0.0` tag names
(`release`), which merges the pull request that last changed them
([spec #378](https://github.com/Judgment-Pack/judgment-pack-spec/pull/378)): their original paths
and SHA-256 checksums. `LICENSE` is that repository's license. The files are retained for this package's tests and `internal/cli`'s,
not read by the executable: the reader's table and codes are written in `codes.go`, and a test
holds them to `1.0.0/formats.json` and `1.0.0/codes.json`.

`1.0.0/` is `conformance/terraform-file/1.0.0/` unmodified: `codes.json`, the code list of the
companion's §9; `formats.json`, the accepted table of its §4, release `1.0.0`; `manifest.json`,
its 206 vectors; `declarations/`, the declarations they name; and `vectors/`, their inputs and
expected results. The `terraform-file-v1.0.0` tag releases them with the companion text
(`spec/terraform-file.md`, reading `1.0.0`), released and not yet stable.

`TestSpecVectors` runs every `read` and `read-stream` row through `Read` and `ReadStream`, 159 in
all, and `TestTerraformFactsSpecVectors` in `internal/cli` runs the 10 `batch` rows through `jpack
terraform facts`. Three rows' inputs are not committed: a file of about 1 MiB, and files of the
default `maxBytes`, 67108864 bytes, and one byte more. The tests build them by the manifest's
`generate` rule. A row whose declaration is malformed reads no file, and this runtime reads no
declaration document, so those 37 rows run through the command's flags where a command line can
write the declaration (21 of them, each an unusable command line, exit 3), and the other 16 are not
run: the test logs each one's reason.

The companion's second reader, `tools/terraform_file_reader.py` in the Spec repository, gives the
same result on every row, but it, the vectors and this reader are by the same vendor: agreement on
these rows is not a cross-vendor claim, and not agreement between independently written readers.
