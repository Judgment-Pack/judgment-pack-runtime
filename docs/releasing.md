# Releasing judgment-pack

Only maintainers release this repository. CLI releases and JPS specification releases are
independent: a CLI release may consume a JPS release, but it never creates or changes one.

## Prerequisites

- The intended JPS artifacts have already been committed and published under an immutable tag in
  `protossai/judgment-pack-spec`.
- GitHub Release Immutability is enabled for this repository.
- `main` is clean, pushed, and passing CI.
- The GitHub CLI is authenticated with permission to push and manage releases.
- The release version is an exact SemVer tag such as `v0.1.0` or `v0.2.0-rc.1`.
- The release workflow pins a currently supported Go toolchain independently from the minimum
  source-compatible Go version in `go.mod`; both values are reviewed deliberately.

Never release a CLI whose artifact lock says `unreleased-local-snapshot`, records a dirty source
worktree, or names a moving branch. The tag workflow independently enforces this rule.

## Refresh the bundled specification

From a clean checkout at the approved JPS tag, remove the existing destination only as part of a
reviewed artifact update, then import a new directory with the maintainer tool:

```bash
env GO111MODULE=on go run ./tools/sync-spec-artifacts \
  --source ../judgment-pack-spec \
  --destination ./internal/artifacts/jps/<exact-spec-version> \
  --source-ref <exact-jps-tag>
```

Review every imported file and `lock.json`. The lock must contain the official repository URL,
`immutable-git-ref`, the tag, its exact commit, `worktreeDirty: false`, and the expected bundle
digest. Update the embedded registry only when adding a new specification version.

## Validate locally

Install the exact GoReleaser version used by the workflow, then run:

```bash
env GO111MODULE=on go fmt ./...
git diff --check
env GO111MODULE=on go vet ./...
env GO111MODULE=on go test ./...
env GO111MODULE=on go run ./cmd/judgment-pack spec test-conformance --quiet
env GO111MODULE=on go run ./tools/check-release --tag <cli-tag>
goreleaser check
goreleaser release --snapshot --clean
```

Inspect `dist/checksums.txt`, all six platform archives, their license files, and one extracted
binary. A release build must report the tag version without the leading `v`.

## Create the release

1. Move the changelog entries from `Unreleased` to the intended version and date.
2. Commit the reviewed release source with DCO sign-off and push `main`.
3. Create an annotated tag on that exact commit and push only that tag:

   ```bash
   git tag -s <cli-tag> -m "judgment-pack <cli-tag>"
   git push origin <cli-tag>
   ```

   If no signing key is configured, stop and resolve the signing policy; do not silently replace a
   signed tag with an unsigned tag.

4. The tag workflow repeats the tests, verifies immutable JPS provenance, and packages the binaries
   without publishing them. Included file timestamps are normalized to the tagged commit for
   reproducible archives. The workflow uploads the six archives and `checksums.txt` as one
   short-lived workflow artifact. Native Linux, macOS, and Windows jobs verify and run archives
   from that exact artifact. Only after all native smoke tests pass does the final job attest the
   archives and create the draft GitHub release. No maintainer PAT or repository secret is used.
   To diagnose or revalidate an existing tag without creating a release, manually dispatch the same
   workflow; manual runs stop after the native smoke tests:

   ```bash
   gh workflow run release.yml --field tag=<cli-tag>
   ```

5. Review the successful workflow, generated notes, checksums, attestations, and draft assets.
6. Publish the draft. With Release Immutability enabled, publishing
   locks the tag and assets.

Do not move or reuse a released tag. Fixes require a new version.

## User-facing verification

Users can verify a downloaded archive with the platform SHA-256 tool and `checksums.txt`. With a
current GitHub CLI they can also verify build provenance:

```bash
gh attestation verify <downloaded-archive> --repo Judgment-Pack/judgment-pack-runtime
```
