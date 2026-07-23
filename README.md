# judgment-pack

> **Status: pre-1.0**
>
> Tagged binaries are official only when they appear on this repository's GitHub Releases page
> with `checksums.txt`. Development checkouts may temporarily embed unreleased JPS snapshots, but
> the release workflow rejects them.

`judgment-pack` is the vendor-neutral **reference runtime** for the Judgment Pack Specification
(JPS). It is a reference implementation, not the only valid one: the normative specification,
schemas, and conformance corpus are owned by the separate
[`judgment-pack-spec`](https://github.com/Judgment-Pack/judgment-pack-spec) repository, and any
independent implementation that passes the published conformance corpus is equally valid.

The runtime **validates documents**. It does not evaluate rules, choose an outcome, fetch a source,
authorize an action, or establish truth, organizational authority, safety, or operational fitness.
This matches the scope of JPS `0.1.0-draft`, which defines carrier, structural, and semantic
document conformance only and defines no evaluator conformance class.

The command binary is `judgment-pack`; release archives also ship a `jpack` short alias for the
same program.

## Implemented commands

```text
judgment-pack version
judgment-pack spec validate <pack-or->
judgment-pack spec test-conformance [suite]
judgment-pack spec schema <spec-version>
```

The namespace is `judgment-pack spec`, not `judgment-pack jps`. JPS remains the name of the
specification and the prefix of its provisional diagnostic codes.

## Install a tagged release

Download the archive for your operating system and architecture from
[GitHub Releases](https://github.com/Judgment-Pack/judgment-pack-runtime/releases). Each archive
includes the `judgment-pack` and `jpack` binaries, README, Apache-2.0 license, attribution notice,
and third-party notices.

Asset names follow this pattern:

```text
judgment-pack_<version>_<os>_<arch>.tar.gz
judgment-pack_<version>_windows_<arch>.zip
```

For example, release `v0.1.0` uses `judgment-pack_0.1.0_linux_amd64.tar.gz`. Linux and macOS users
can extract an archive and install the binary into a user-owned directory already on `PATH`:

```bash
tar -xzf judgment-pack_0.1.0_linux_amd64.tar.gz
install -m 0755 judgment-pack "$HOME/.local/bin/judgment-pack"
judgment-pack version
```

On Windows, expand the `.zip`, move `judgment-pack.exe` into a directory on your user `PATH`, and
run:

```powershell
judgment-pack version
```

Verify the archive before extracting it. On Linux:

```bash
grep ' judgment-pack_0.1.0_linux_amd64.tar.gz$' checksums.txt | sha256sum --check
```

On macOS:

```bash
grep ' judgment-pack_0.1.0_darwin_arm64.tar.gz$' checksums.txt | shasum -a 256 --check
```

On Windows PowerShell:

```powershell
(Get-FileHash .\judgment-pack_0.1.0_windows_amd64.zip -Algorithm SHA256).Hash
Select-String -Path .\checksums.txt -Pattern ' judgment-pack_0.1.0_windows_amd64.zip$'
```

The two Windows hashes must match. Release archives also carry GitHub build-provenance
attestations, which a current GitHub CLI can verify:

```bash
gh attestation verify judgment-pack_0.1.0_linux_amd64.tar.gz --repo Judgment-Pack/judgment-pack-runtime
```

Packages for Homebrew, Scoop, `apt`, and `go install` are not published yet. In particular, source
installation with `go install` does not receive the release linker metadata, so use a release
archive when you need an accurately reported runtime version.

## Build and try it locally

Go 1.21 or newer is required to build from source.

If an older WSL setup has persisted `GO111MODULE=off`, clear it once with
`go env -u GO111MODULE`. The commands below explicitly enable module mode as a compatibility
measure.

```bash
env GO111MODULE=on CGO_ENABLED=0 go build -trimpath -o ./bin/judgment-pack ./cmd/judgment-pack
./bin/judgment-pack --help
./bin/judgment-pack version
```

From a sibling checkout of `judgment-pack-spec`, validate a synthetic example:

```bash
./bin/judgment-pack spec validate ../judgment-pack-spec/examples/minimal-expense-approval.json
./bin/judgment-pack spec validate --format json ../judgment-pack-spec/examples/minimal-expense-approval.json
```

Standard input is accepted explicitly with `-`:

```bash
./bin/judgment-pack spec validate --format json - < pack.json
```

Run the exact bundled JPS `v0.1.0-draft` corpus:

```bash
./bin/judgment-pack spec test-conformance
./bin/judgment-pack spec test-conformance --format json
```

Inspect or copy the bundled schema without network access:

```bash
./bin/judgment-pack spec schema 0.1.0-draft
./bin/judgment-pack spec schema 0.1.0-draft --write schema.json
./bin/judgment-pack spec schema 0.1.0-draft --write -
```

The schema command refuses to overwrite an existing file.

## Validation behavior

Validation short-circuits in this order:

1. strict UTF-8 JSON carrier parsing, including duplicate-member rejection;
2. exact `specVersion` dispatch;
3. Draft 2020-12 structural validation with URI, date, and date-time assertions;
4. normative semantic reference and extension-declaration checks; and
5. required-extension capability negotiation.

An unknown but syntactically usable `specVersion` is `unsupported`, not `invalid`. The conformance
runner deliberately pins each case to the suite's declared specification version, so a fixture that
violates that pinned schema remains a structural negative case.

`--through carrier` and `--through structural` produce explicitly partial results. They never print
an unqualified “valid document” message.

The public MVP supports no JPS extensions. A structurally and semantically conforming document that
requires an extension is therefore reported as `unsupported`. Extension code is never discovered,
downloaded, installed, or executed during validation.

## Process contract

| Exit | Meaning |
| ---: | --- |
| `0` | Command succeeded; validation passed the reported scope. |
| `1` | Document invalid, or a conformance expectation mismatched. |
| `2` | Exact JPS version or required extension unsupported. |
| `3` | Invocation or suite configuration invalid. |
| `4` | Input/output or resource-limit failure. |
| `5` | Internal runtime or bundled-artifact failure. |

`--format json` writes exactly one versioned JSON object plus a newline to standard output for
normal valid, invalid, unsupported, mismatch, and handled operational results. It never mixes human
prose or ANSI controls into that stream. Results include `diagnosticsTruncated` so automation can
detect a reached output limit. `--quiet` is available only with human output.

Human document results, including `invalid` and `unsupported`, use standard output. Invocation,
input/output, resource, and internal failures use standard error.

## Security defaults

The current implementation:

- performs no runtime network requests and never dereferences document locators;
- accepts one explicitly selected regular file or standard input, not URLs or special files;
- rejects duplicate decoded member names at every depth, invalid UTF-8, trailing JSON, and
  non-JSON constants;
- caps a document at 10 MiB, nesting at 128, parsed nodes at 250,000, and diagnostics at 100;
- caps local conformance suites at 10,000 cases and 100 MiB total;
- caps diagnostics retained across one conformance result at 1,000;
- validates suite metadata before resolving fixtures and rejects traversal and symlink paths;
- treats extension values as inert data; and
- emits sanitized, value-free human diagnostics.

See [SECURITY.md](SECURITY.md) for reporting and boundary details.

## Artifact provenance

Runtime validation uses only files embedded in the binary and verified against
[`internal/artifacts/jps/0.1.0-draft/lock.json`](internal/artifacts/jps/0.1.0-draft/lock.json).
The lock records the source repository, exact commit/ref and source state, plus SHA-256 and size
metadata for all 50 imported files. A development snapshot remains visibly labelled
`unreleased-local-snapshot` and cannot pass the release gate.

> **Provenance note.** The embedded bundle is currently pinned to the specification's pre-neutral
> upstream tag (`protossai/judgment-pack-spec@v0.1.0-draft`), whose schema `$id`s still use the
> pre-neutralization URLs. Re-vendoring to a neutral, digest-locked spec tag is a reviewed
> follow-up, tracked in [CHANGELOG.md](CHANGELOG.md), pending the specification project publishing
> one. Because validation is fully offline and never dereferences a `$id`, this affects provenance
> labelling only, not validation behavior.

Artifact bundle and conformance-corpus digests use `sha256-length-prefixed-v1`: each sorted path and
file body is encoded as an unsigned 64-bit big-endian byte length followed by those exact bytes.
The corpus digest covers `manifest.json`, `manifest.schema.json`, and every manifest fixture, so an
equivalent bundled and local corpus produces the same value. Human and JSON conformance output both
report it.

Before any release, those files must be re-imported from an approved immutable specification
commit or tag. The lock must say `immutable-git-ref`, record a clean source worktree, and identify
the exact ref and commit. Mutable `main` is never a runtime validation authority.

Maintainers can create a new, initially absent snapshot directory with:

```bash
env GO111MODULE=on go run ./tools/sync-spec-artifacts \
  --source ../judgment-pack-spec \
  --destination ./internal/artifacts/jps/<exact-version> \
  --allow-dirty
```

`--allow-dirty` is deliberately required for an unreleased snapshot. A release candidate instead
uses `--source-ref <exact-commit-or-tag>`; that mode verifies the official repository origin, a
clean worktree, and that the ref resolves to checked-out `HEAD`. Artifact updates are reviewed
source changes; the runtime never runs this tool.

## Public and commercial boundaries

This repository is intended to remain a self-contained Apache-2.0 public core. It must build,
install, validate, and run its conformance tests without private repositories, credentials,
services, package indexes, or feature flags.

Commercial capabilities should live in separate private repositories; this public repository must
never depend on them. The current supported integration boundary is the `judgment-pack` executable
and its versioned JSON output. Go packages are intentionally `internal`, and there is not yet a
stable in-process SDK or plugin API. Before commercial commands are composed into one binary, the
public project must define and version that contract deliberately. Future commands should use
distinct namespaces such as `judgment-pack cloud` or `judgment-pack org`; they must not override
`judgment-pack spec` conformance semantics or auto-load during validation.

The normative specification, schemas, and public corpus remain in the separate
[`judgment-pack-spec`](https://github.com/Judgment-Pack/judgment-pack-spec) repository.

## Development

```bash
env GO111MODULE=on go fmt ./...
env GO111MODULE=on go vet ./...
env GO111MODULE=on go test ./...
env GO111MODULE=on CGO_ENABLED=0 go build -trimpath ./cmd/judgment-pack
```

See [CONTRIBUTING.md](CONTRIBUTING.md), [docs/architecture.md](docs/architecture.md), and the
[maintainer release runbook](docs/releasing.md).

In VS Code, **Terminal → Run Task → judgment-pack: Build CLI** builds `bin/judgment-pack`; the test
and bundled conformance tasks are available from the same menu. The tasks explicitly enable Go
module mode for older WSL configurations.

## License

Apache License 2.0. See [LICENSE](LICENSE).
