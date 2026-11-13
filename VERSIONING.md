# Versioning and release policy

judgment-pack releases use Semantic Versioning identifiers with a leading `v` on Git tags. Only a tag
and GitHub release published by the maintainers establish an immutable release; similarly named
files or version strings on the mutable `main` branch do not.

## CLI versions

The CLI version identifies a released build of the judgment-pack command-line interface. Release builds
derive their reported version from the Git tag and expose it through `jpack version` and
`jpack --version`. Untagged development builds report `0.0.0-dev`.

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

The staged draft selector `0.4.0-draft`, which runtime 0.43.0 admitted EXPERIMENTALLY (ADR-0075),
was released by the specification as Core `0.5.0` (RFC 0031) and is removed (ADR-0077): a pack,
suite or corpus run that names `0.4.0-draft` or `0.4.0` is refused as an unsupported version, and
the refusal says that the draft was released as `0.5.0`. A pack moves by changing its `specVersion`
to `"0.5.0"` (Core §11). Every bundle this runtime embeds is a released version; a lock that states
a `stage` is refused at load, and runtime discovery carries no `stage`.

Document support and evaluator support are reported separately. The additive
`evaluatorCapabilities` entries name exact Core contracts; `supportedProfiles` on
the `0.3.0` and `0.5.0` entries list only `base` and make no profile promise for other formats.
Legacy authoring defaults remain `0.2.0-draft`; explicit standalone `spec_version`
selects the new contract, while pack-aware operations use the exact associated pack.
Matrix carrier versions `1`–`3` are unchanged: the explicit contract selects the
portable disposition grammar, rather than adding a closed matrix member. Additive
skip metadata is omitted from old suite reports when no case is skipped.

The Core `0.3.0` bundle is imported from the published Spec tag `v0.3.0` at
commit `27cb29bf13e4a2d58d69fe19c7ebfeb762b014fc`; its bytes are those of the
earlier commit pin `65c7cf5598a0f0fc532c80a9e2215090951104ce`, unchanged.
This source provenance does not by itself establish a conformance claim; see
[CONFORMANCE.md](CONFORMANCE.md).

Selected retained-column leaf support has a separate exact distribution/source/schema/corpus
identity. Core `specVersion: "0.3.0"` does not select or imply this support. The explicit CLI/MCP reader selects the installed official `v0.3.0`
leaf, source `27cb29bf13e4a2d58d69fe19c7ebfeb762b014fc`, with maturity `draft`
and document-validation role for Base/Extended. Compatibility flag names
`--draft-contract`/`draftContract` require the exact installed release selector;
historical Draft selectors and the `v0.3.0-rc.2` candidate selector refuse. Historical Draft test resources retain their
original labels. See [the selected reader](docs/dialect-retained-column.md).

## Compatibility dimensions

Release notes describe compatibility separately for:

- **command compatibility** — whether existing command names, flags, and exit classes retain their
  behavior;
- **machine-output compatibility** — whether versioned JSON output remains usable by existing
  consumers;
- **specification compatibility** — which exact Judgment Pack Specification versions are supported;
- **platform compatibility** — which operating systems and architectures receive tested release
  artifacts; and
- **Go API compatibility** — whether the exported identifiers of package `sdk` keep their meaning.

The JSON `outputVersion` is a protocol version, not the CLI release version. A change that breaks
machine-output compatibility must deliberately increment that protocol version.

Every versioned JSON artifact this runtime writes or reads carries its own such version, each
independent of the others and of the release version — but two kinds of artifact follow two
different rules, and conflating them has been a live source of error.

- **Output and record formats this runtime produces** — `outputVersion` for machine output,
  `recordVersion` for an audit record (ADR-0018). A consumer reads what it knows and ignores what it
  does not, so an added member is backward-compatible and does not move the version; a removed or
  renamed member is the break that does.
- **Closed input and declaration formats this runtime reads** — `configVersion` for a `jpack.json`
  (ADR-0012), `lockVersion` for a reviewed-set lock (ADR-0019), `formatVersion` for a graph document
  (ADR-0015). Their schemas are closed, so an older reader *rejects* a document carrying a member it
  does not know rather than ignoring it. Adding one therefore moves the version, whatever the
  addition is: `graphs` moved `configVersion` to `"2"`, `audit` moved it to `"3"`,
  `requireReviewed` moved it to `"4"`, `requireComparableFacts` moved it to `"5"`, and the audit
  member's `chain`, `signingKey` and `timestampAuthority` moved it to `"6"`, its `signingPublicKey`
  moved it to `"7"`, and its `requireSigning` moved it to `"8"`, for exactly this reason, and the version gate is what turns an
  unreadable-config refusal into an actionable "this runtime accepts" one. The audit record's chain
  members (ADR-0047) are the other kind of change: additive output, so `recordVersion` stays `"1"`.
  So is the discontinuity line a repair appends, a third `kind` beside `evaluation` and
  `graph-composite` with members of its own, as the composite has: a reader selects records by
  `kind` and passes over one it does not know. An audit checkpoint carries its own
  `checkpointVersion`, `"1"`, and is a closed input to `jpack audit verify --expect`. A line of the
  signature sidecar carries its own `sidecarVersion`, `"1"`, and its shape is exact: a reader
  counts a line of another version or shape as unreadable, signing nothing, so a later version is
  a new `sidecarVersion` beside the old one rather than a change to it. A line of the stamps file
  carries its own `stampVersion`, `"1"`, by the same rule.

## Go API

Package `sdk` is the one importable Go package of this module; every other package is `internal`
(ADR-0073). Its exported identifiers, which its package documentation lists, are experimental:
during `0.x` a minor release may change or remove any of them, and the release notes name the
change. The module version is the release tag, and `sdk.Describe` reports it from the importing
program's build information when that information records it. The bytes
`Evaluation.CanonicalDisposition` returns are the JPS §8.3 portable disposition, whose form the
specification fixes; the envelope `Evaluation.JSON` returns is CLI machine output and follows its
`outputVersion`.

The pack view (`sdk.Pack.View` and package `sdk/view`, and `jpack packs describe`, ADR-0074)
carries its own `viewVersion`,
`"1"`, in its JSON and in its Markdown. It moves when the bytes View gives for a pack an earlier view
version could describe would change, in either encoding: a member renamed, reordered or rendered
differently, a change to the escaping rule, a heading or label reworded. It does not move when the
view learns to describe packs it refused before, such as those of a newly admitted specification
version. This is stricter than the rule for `outputVersion`, because a reader of the view compares
its bytes. Which characters the Markdown writes as code points follows the Unicode tables of the Go
toolchain the runtime is built with, through `internal/display`; an update of those tables alone,
which can move a newly assigned format character into that set, does not move `viewVersion`. The `packs describe` JSON envelope follows `outputVersion`; the view's bytes ride in it
as a string and follow `viewVersion`. Describing the value sets and annotations of Core `0.5.0`
left `viewVersion` at `"1"`, because no pack an earlier release could describe holds them; the
same holds for ADR-0078, which writes annotation text that could reorder its line, or join the
syntax before it, only in ASCII.
Runtime 0.43.0 described packs under the staged selector `0.4.0-draft`; this runtime refuses them
rather than describing them with other bytes.

The filtered view (`sdk.Pack.FilteredView`, `view.FilteredView`, and `jpack packs describe
--filter`, ADR-0075) carries its own `filteredViewVersion`, `"1"`, in its JSON and in its
Markdown, and no `viewVersion`, so a reader of views never takes it for the pack. It moves by the
rule `viewVersion` moves by. Its bytes ride in the `packs describe --filter` JSON envelope's
`filteredView` member as a string. It is EXPERIMENTAL, and exists only for Core `0.5.0`, the one
version that defines annotations.

The decision record (`sdk.Pack.EvaluateWithRecord` and package `sdk/record`, ADR-0079) carries its
own `recordVersion`, `"1.0.0"`, in its JSON. It is not the audit trail record's `recordVersion`,
`"1"`, and the two move independently. Its major part moves when a member is removed or renamed,
or a member's value is taken over other bytes or written in another form, so that a reader of the
earlier form would misread it; its minor part when a member is added, which such a reader can pass
over; its patch part when the bytes change in any other way, every member and its meaning kept. It
does not move when the runtime learns to record evaluations it refused before, such as those of a
newly admitted specification version, or when the runtime version the record carries changes,
which changes the record's bytes and digest by design. The record is EXPERIMENTAL.

A pack signature document (`jpack packs sign`, `jpack packs verify-signature` and package
`sdk/packsig`, ADR-0083) carries its own `signatureVersion`, `"1.0.0"`, which names both the
document's form and the message its signature is over. It moves by the rule the decision record's
`recordVersion` moves by, and a reader admits only the version it knows: a document of another
`signatureVersion` is refused (`JPS-PACK-SIGNATURE-VERSION`), never read as the one it knows. A
change of the message's domain prefix is a major change. The document, the commands and the
package are EXPERIMENTAL; the pack, its digest and lock files do not change with them.

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
