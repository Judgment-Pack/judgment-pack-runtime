# Versioning and release policy

judgment-pack releases use Semantic Versioning identifiers with a leading `v` on Git tags. Only a tag
and GitHub release published by the maintainers establish an immutable release; similarly named
files or version strings on the mutable `main` branch do not.

## CLI versions

The CLI version identifies a released build of the judgment-pack command-line interface. Release builds
derive their reported version from the Git tag and expose it through `judgment-pack version` and
`judgment-pack --version`. Untagged development builds report `0.0.0-dev`.

During the `0.x` research period, any minor release may change commands, flags, exit behavior, or
human-readable output incompatibly. Patch releases should contain compatible corrections and
security fixes. Release notes must identify user-visible changes and include migration guidance
when existing automation needs to change.

The `main` branch describes work in progress. It is not an immutable CLI release and must not be
used as a reproducibility anchor.

## Version increments

For a release version `MAJOR.MINOR.PATCH`:

- increment **PATCH** for backward-compatible bug fixes, security fixes, documentation corrections,
  and build or packaging corrections that do not intentionally change a public interface;
- increment **MINOR** for backward-compatible commands, flags, output fields, or support for an
  additional exact specification version; during `0.x`, a minor release may also contain a
  documented breaking change; and
- increment **MAJOR** after `1.0.0` for an incompatible change to a supported public interface.

When several changes ship together, the release uses the highest increment required by any one of
them. Removing support for a specification version or incompatibly changing commands, exit classes,
or machine output requires at least a minor increment during `0.x` and a major increment after
`1.0.0`.

## Prereleases

Prerelease identifiers follow Semantic Versioning, for example `v0.2.0-alpha.1`, `v0.2.0-beta.1`,
and `v0.2.0-rc.1`. Prereleases have lower precedence than the corresponding final release and do not
carry a compatibility or support guarantee. A new prerelease increments its trailing number; a
published prerelease tag is never moved or reused.

## Specification versions

The CLI version and Judgment Pack Specification `specVersion` are independent. A CLI release may
support one or more exact specification versions, and adding or removing support requires a new CLI
release. Consumers must inspect the supported specification versions reported by the CLI; they must
not infer support from the CLI version number.

Bundled specification artifacts are pinned to an immutable upstream tag or exact commit and are
verified byte-for-byte at runtime and during release checks. Updating those artifacts does not
rewrite an existing CLI release.

## Compatibility dimensions

Release notes describe compatibility separately for:

- **command compatibility** — whether existing command names, flags, and exit classes retain their
  behavior;
- **machine-output compatibility** — whether versioned JSON output remains usable by existing
  consumers;
- **specification compatibility** — which exact Judgment Pack Specification versions are supported;
  and
- **platform compatibility** — which operating systems and architectures receive tested release
  artifacts.

The JSON `outputVersion` is a protocol version, not the CLI release version. A change that breaks
machine-output compatibility must deliberately increment that protocol version.

## Release artifacts

An immutable CLI release contains, at minimum:

- the tagged source commit;
- platform archives produced from that commit;
- SHA-256 checksums for the archives;
- build-provenance attestations for the published artifacts; and
- release notes describing compatibility, changes, and known limitations.

The release workflow verifies formatting, static analysis, tests, the bundled conformance suite,
the release tag, and immutable specification provenance before it creates a draft GitHub release.

Published tags must never be moved or reused. A correction to a published release requires a new
SemVer tag.

## Release process

Release changes are merged into the protected `main` branch through a pull request. Required CI
checks must pass, and user-visible changes must be recorded in `CHANGELOG.md`, before maintainers
create a release tag from the merged commit. The tag triggers the release workflow; maintainers
review its draft artifacts, checksums, attestations, and notes before publication.

The Git tag is the source of truth for a released CLI version. The repository intentionally does
not duplicate it in a `VERSION` data file.

## Deprecation and support

Pre-1.0 releases have no compatibility or security service-level guarantee. Maintainers may
deprecate or remove behavior in a later `0.x` release, but must record the change in release notes.
A stable release requires a separate support and deprecation policy before publication.
