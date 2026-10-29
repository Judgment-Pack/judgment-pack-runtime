# Releasing judgment-pack

Only maintainers release this repository. CLI releases and JPS specification releases are
independent: a CLI release may consume a JPS release, but it never creates or changes one.

## Prerequisites

- The intended JPS artifacts have already been committed to `Judgment-Pack/judgment-pack-spec` and
  are reachable by an immutable reference: either a published tag or a full-length commit digest.
  Prefer a tag when one carries the intended artifacts.
- GitHub Release Immutability is enabled for this repository. The tag workflow enforces this
  (see "Release immutability" below); it does not change the setting.
- `main` is clean, pushed, and passing CI.
- The GitHub CLI is authenticated with permission to push and manage releases.
- The release version is an exact SemVer tag such as `v0.1.0` or `v0.2.0-rc.1`.
- The release workflow pins a currently supported Go toolchain independently from the minimum
  source-compatible Go version in `go.mod`; both values are reviewed deliberately.
- The GHCR package `judgment-pack` is **publicly visible**. GHCR creates a package private on
  first push regardless of repository visibility, so the first release after ADR-0013 will fail
  its anonymous `image-smoke` job by design: flip the package to public
  (Package settings → Change visibility), then re-run the failed jobs. Every later release
  inherits the visibility.

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

For a specification version that publishes the evaluation corpus of Core §3.4.1, the tool also
imports that corpus — its manifest, the manifest's schema, and every pack fixture a case names —
under `evaluation/` in the destination. A version that publishes none contributes nothing there.

Review every imported file and `lock.json`. The lock must contain the official repository URL,
`immutable-git-ref`, the immutable reference used, its exact commit, `worktreeDirty: false`, and the
expected bundle digest. Review one lock per bundled version: the release gate checks all of them, not
only the version a surface selects by default. Update the embedded registry only when adding a new
specification version.

Policy draft resources are independently pinned under `internal/policy/official/`,
with the complete `policy-v1.0.0-draft.1` archive's original source paths. Its
provenance records the immutable release/tag, exact Spec commit, archive digest and
per-file bytes. `tools/check-release` also verifies the independent Policy manifest
pin and full embedded inventory. Updating these resources requires an explicit
reviewed artifact change and appropriate CLI version increment; it never rewrites
the retained prototype or historical candidate trees or the Core locks. Policy
source schema IDs stay distinct from derived Runtime editing schema IDs. See
[ADR-0061](adr/0061-adopt-official-policy-draft-resources.md) for compatibility.

## Refresh the selected retained-column leaf import

The current opt-in CLI/MCP reader selects the installed official `v0.3.0`
leaf with draft maturity and document-validation role. Its exact source/schema/corpus
and independent provenance pin are compiled in `retained_column_artifacts.go`.
Core locks, Policy resources and the general Core registry remain independent.
Historical Draft fixtures retain their original labels; they are not release evidence.
Runtime 0.39.0-alpha.2 installed the `v0.3.0-rc.2` candidate of this leaf; the
installed reader now refuses that candidate's identity.

A Spec pre-release takes one of two forms: `-rc.N` is a candidate of the complete
milestone, and `-alpha.N` is a scoped cut for a named consumer that never graduates;
the development kit's release policy defines both.

After the maintainer confirms adoption and immutable publication of the official
Spec release, verify its exact tag/source commit, immutable release state,
asset checksums and attestations using the Spec release process. Record those
observations separately: the offline importer cannot establish publication,
immutability or source authenticity from local bytes or a Git remote URL.
Review the release inventory digest independently. Use a clean official checkout
at that exact tag and the verified tar.gz asset. A reviewed artifact update never
overwrites a nonempty set: remove the previous `official/` import in the same
change, then run:

```bash
env GO111MODULE=on go run ./tools/sync-retained-column \
  --source ../judgment-pack-spec \
  --source-ref v0.3.0 \
  --source-commit "$SOURCE_COMMIT" \
  --archive "$VERIFIED_ARCHIVE" \
  --archive-sha256 "$VERIFIED_ARCHIVE_DIGEST" \
  --inventory-sha256 "$REVIEWED_INVENTORY_DIGEST" \
  --destination ./internal/dialect/retained-column/official
```

The importer admits only the release named by `internal/retainedcolumn`
(`Release`, `Version`, `InventoryPath`); adopting another release changes those
constants, the reader identity and its tests in the same reviewed change.

Digest arguments include `sha256:`. The destination must be new and exclusively
owned during import. The helper follows the existing staged import pattern; the
portable directory rename can replace an empty directory created concurrently,
so concurrent destination writers are outside this maintainer workflow. It
never overwrites a nonempty artifact set. All checks complete before staging;
materialized bytes are verified again before publication of the directory.

Review every imported byte and `provenance.json`. The helper retains the closed
leaf inventory, generated `release-identity.json` and all files the inventory
names at their original upstream paths, records exact archive and inventory
hashes and per-file lengths/hashes, and prints the generated provenance digest.
The tar.gz itself and external publication/attestation evidence remain release
evidence; they are not represented as verified by the local manifest.

A reviewed integration must install that independent provenance digest
and exact tuple in `internal/dialect/retained_column_artifacts.go`, switch the
selected reader identity and source URL/maturity truthfully, and update selected
CLI/MCP APIs/tests together. Every selected admission verifies the installed bytes;
Go embedding must include underscore files named by the inventory (`all:`). Run every applicable published document-validation
case with the installed identity and preserve historical ordinary/Draft regression
evidence. An imported directory alone enables no support. `tools/check-release`
refuses unpinned resources, changed or missing installed bytes, extra inventory,
unsupported identities and disagreement with the selected reader. No final Core,
producer or preparation-verifier claim follows from this document-validation role.

## Validate locally

Install the exact GoReleaser version used by the workflow, then run:

```bash
env GO111MODULE=on go fmt ./...
git diff --check
env GO111MODULE=on go vet ./...
env GO111MODULE=on go test ./...
env GO111MODULE=on go run ./cmd/jpack spec test-conformance --quiet
env GO111MODULE=on go run ./cmd/jpack spec test-conformance --spec-version 0.2.0-draft --quiet
env GO111MODULE=on go run ./cmd/jpack experimental evaluate-corpus
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
   from that exact artifact. Only after all native smoke tests pass does the attest-and-draft job
   attest the archives and create the draft GitHub release. The workflow then pauses at the
   `production` environment gate, awaiting maintainer approval before the publish job runs. No
   maintainer PAT or repository secret is used. To diagnose or revalidate an existing tag without
   creating a release, manually dispatch the same workflow; manual runs stop after the native smoke
   tests:

   ```bash
   gh workflow run release.yml --field tag=<cli-tag>
   ```

5. Review the drafted release on the Releases page — generated notes, checksums, attestations, and
   the six archives.
6. Approve the pending `production` deployment on the release run. The publish job reads the
   draft this run made through the API by its release id (no download): draft state, tag, title,
   then each asset's digest as GitHub reports it, compared with the run's own files. On any
   difference it refuses and publishes nothing. Otherwise it flips that draft to public by id,
   never by tag (a prerelease tag is never marked latest), then reads the published assets' digests
   again and fails the named step `Require the published release to hold what this run wrote`,
   printing both digest sets, if they differ. With Release Immutability enabled, publishing locks
   the tag and assets. The run's files are kept for thirty days, as long as an approval may wait.
7. After the release publishes, follow-on jobs run without further approval
   ([ADR-0013](adr/0013-oci-image-and-mcp-registry-distribution.md)), ordered so nothing
   user-facing exists before the pushed image has actually run: `publish-image` downloads the
   archives **from the published release** (the run artifact expires; the release does not),
   verifies their checksums, refuses to overwrite an existing version tag, and pushes the
   multi-arch image to `ghcr.io/judgment-pack/judgment-pack` under the version tag only;
   `image-smoke` pulls it **anonymously** by digest on native amd64 and arm64 runners and runs
   the bundled conformance suite in it; only then does `promote-image` attest the digest and
   (non-prereleases only) fast-forward `latest` to it, and `publish-mcp-registry` renders
   `build/mcp-registry/server.json.tmpl` at the release version (CI validates the template
   against the vendored registry schema on every PR) and publishes it to the official MCP
   registry as `io.github.Judgment-Pack/judgment-pack` via GitHub OIDC. Confirm all four
   completed green; the registry entry and the image tag are release-time state that nothing
   else refreshes.

   A prerelease **is** published to the registry, deliberately, so an rc exercises the registry
   job end to end — and the registry orders versions by SemVer with no prerelease notion, so an
   rc is the registry's newest version until its final follows. Cut the final promptly, and pin
   a version in registry-aware clients when this matters. The GitHub release and the image
   `latest` tag are unaffected: neither ever moves for a prerelease.

## Release immutability

The workflow checks the prerequisite twice, read-only and fail-closed
(`tools/check-immutability`; each failure is one sentence in the job log):

- **Before anything is drafted**, the `package` job reads the latest published release (not a draft
  or prerelease) through the REST API with the workflow token and requires its `immutable` field to
  be `true`. It reads the release's flag because the repository setting itself is not readable to
  the workflow's token (it needs the Administration permission). It fails when that release is not
  immutable, when the API errors or refuses the token (named as such), or when the field is
  missing; with no published release at all it passes and says so, and the check after
  publication is the only enforcement for a first release. A manual validation dispatch performs
  the same check.
- **After the publish job flips the draft**, it reads the new release and fails unless it reports
  `immutable: true`. This is the enforcement. The workflow never edits or deletes the release. The
  failed `publish` job blocks every follow-on job (`publish-image`, `image-smoke`,
  `promote-image`, `publish-mcp-registry`), so no image or registry entry is made for it. The
  release that already exists stays as it is; the fix is a new version once the setting is on, as
  for any released tag.

### The window that remains

The API cannot publish a release on condition that its assets are the ones just read, so one
window remains: between the last read of the draft's asset digests and the publish call, a few
moments of the same step. Whoever can write to the repository's releases can act in it: a
repository writer, which in this organisation is a maintainer, the same people the `production` gate
asks to approve. Nothing in the workflow closes that window. A file replaced in it is published,
and the read after publication then fails the run by name; with release immutability on, the
published release can no longer be changed, so the replacement stays visible as a failed release
and is not quietly corrected. That step fails in two ways, told apart by its message:

- `does not hold what this run wrote`: the read completed and the digests differ (both sets are
  printed). Treat it as an incident: tell the maintainers, point no one at the release, and
  release a new version from a new tag.
- `could not be verified`: a read failed or returned nothing usable (the API error is printed). No
  diagnosis is made and the release is unverified, not bad. A maintainer re-reads the same release
  id and compares its asset digests with the run's recorded ones (the run's `checksums.txt` and the
  archives' SHA-256 values, from the run artifact) before declaring it good or diagnosing a
  replacement:

  ```bash
  gh api repos/Judgment-Pack/judgment-pack-runtime/releases/<id>/assets --paginate \
    --jq '.[] | "\(.name) \(.digest)"'
  ```

  Only a confirmed mismatch takes the incident and new-tag path above.

In both cases, do not re-run the publish step.

A failure of the first check means: turn on Release Immutability in the repository settings and
re-run; the workflow never changes a repository setting. A release published before the setting was
enabled cannot be locked retroactively (v0.29.0, recorded in issue #252), and because the first
check reads the latest release, the first release after enabling the setting is the one that
shows the setting works.

Do not move or reuse a released tag. Fixes require a new version — including for the image tags
and the registry entry: the version-tag guard in `publish-image` enforces this for the image, so a
re-run after a downstream failure fails at the guard instead of moving the tag. An image whose
smoke test fails is left as a dead version tag: never promoted to `latest`, never attested, never
published to the registry.

## User-facing verification

Users can verify a downloaded archive with the platform SHA-256 tool and `checksums.txt`. With a
current GitHub CLI they can also verify build provenance:

```bash
gh attestation verify <downloaded-archive> --repo Judgment-Pack/judgment-pack-runtime
```
