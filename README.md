# judgment-pack

> **Status: pre-1.0**
>
> Tagged binaries are official only when they appear on this repository's GitHub Releases page
> with `checksums.txt`. Development checkouts may temporarily embed unreleased JPS snapshots, but
> the release workflow rejects them.

`judgment-pack` is the vendor-neutral **reference runtime** for the Judgment Pack Specification
(JPS). It is a reference implementation, not the only valid one: the normative specification,
schemas, and conformance corpus are owned by the separate
[`judgment-pack-spec`](https://github.com/Judgment-Pack/judgment-pack-spec) repository, and an
independent implementation is judged against the complete normative requirements of the conformance
class it claims — not against this runtime. Results from the corpus published for the exact version
it names are required evidence for such a claim and are not exhaustive evidence of it (§3.4.1).

The runtime **validates documents**. It does not fetch a source, authorize an action, or establish
truth, organizational authority, safety, or operational fitness. It bundles two specification
versions, `0.1.0-draft` and `0.2.0-draft`, and validates a document against the exact version that
document declares; `0.1.0-draft` defines carrier, structural, and semantic document conformance
only, and `0.2.0-draft` changes no part of the document format.

Its one evaluation surface is `judgment-pack experimental evaluate` (and the `experimental_evaluate`
MCP tool), which applies the specification's §§7–8 resolution model per
[ADR-0007](docs/adr/0007-experimental-evaluator.md). The `experimental` namespace is a **stability**
statement: this surface may change or be removed without compatibility promise. It is not a statement
about conformance. That evaluator implements the evaluator conformance class Core `0.2.0-draft` adds —
the §8.2 input preflight, the §8.3 portable disposition with its RFC 8785 byte agreement, the §8.4
error classes and their fixed precedence, and the §10 limits — per
[ADR-0010](docs/adr/0010-evaluator-aligned-to-core-0.2.0-draft.md) and
[ADR-0011](docs/adr/0011-first-evaluator-conformance-claim.md). Only a pack declaring `specVersion`
`0.2.0-draft` is evaluated: §11 makes the declared value exact and requires an unedited `0.1.0-draft`
pack to be re-declared — one edit, the `specVersion` string, and nothing else in the document — before
an implementation claiming this draft evaluates it, so any other version is refused as
`pack-not-conformant` in the `preflight` phase.

**This runtime's conformance claim is stated, in full and only, in
[CONFORMANCE.md](CONFORMANCE.md).** JPS §3.4.1 fixes the entire form such a claim may take — the class,
one exact `specVersion`, the corpus version, the results obtained, and in the claim's own words that
every row of that corpus version passed — so this README states no part of it and neither does any
other surface: a partial restatement would be the partial claim §3.4.1 forbids. Read that file for the
claim, its version scope, its evidence, and everything it does not assert, which includes anything at
all about a pack, its facts, or the wisdom of acting on a disposition (§3.5).
`judgment-pack experimental evaluate-corpus` runs the bundled evaluation corpus and reports its rows:
that claim's required, non-exhaustive evidence.

That command carries one further opt-in, `--rfc0008-quantifiers`, which is a **draft-RFC
prototype** per [ADR-0009](docs/adr/0009-draft-rfc-quantifier-prototype.md): it admits the
collection quantifiers `exists`, `every`, and `uniform` proposed by the specification's RFC 0008
(Draft). Those operators belong to no published JPS version. A pack using one is **not valid** under
JPS `0.1.0-draft`, `spec validate` rejects it, the evaluator without the flag refuses it, and every
successful evaluation payload produced under the flag says so in band through a `draftPrototype`
member — a refusal is an operational error and carries none. The flag is CLI only; the MCP tool does
not expose it.

The command binary is `judgment-pack`; release archives also ship a `jpack` short alias for the
same program.

## Implemented commands

```text
judgment-pack version
judgment-pack spec validate <pack-or->
judgment-pack spec test-conformance [suite]
judgment-pack spec schema <spec-version>
judgment-pack spec examples [name]
judgment-pack mcp
judgment-pack experimental evaluate <pack-or->   (EXPERIMENTAL SURFACE; claim: CONFORMANCE.md)
judgment-pack experimental evaluate <pack-or-> --rfc0008-quantifiers   (DRAFT-RFC PROTOTYPE; outside the claim)
judgment-pack experimental evaluate-corpus   (EXPERIMENTAL SURFACE; corpus results, the claim's evidence)
```

The namespace is `judgment-pack spec`, not `judgment-pack jps`. JPS remains the name of the
specification and the prefix of its provisional diagnostic codes.

The `mcp` command serves the same offline operations to a Model Context Protocol client over stdio,
so an agent can validate documents as a tool call; see
[docs/mcp-clients.md](docs/mcp-clients.md) for per-client setup and
[docs/agent-testing.md](docs/agent-testing.md) for the agent-driven testing protocol.

## Install a tagged release

Download the archive for your operating system and architecture from
[GitHub Releases](https://github.com/Judgment-Pack/judgment-pack-runtime/releases). Each archive
includes the `judgment-pack` and `jpack` binaries, README, the `CONFORMANCE.md` claim the source it was
built from carries, Apache-2.0 license, attribution notice, and third-party notices.

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

Run a bundled document-conformance corpus — `0.1.0-draft` by default, or an exact bundled version:

```bash
./bin/judgment-pack spec test-conformance
./bin/judgment-pack spec test-conformance --format json
./bin/judgment-pack spec test-conformance --spec-version 0.2.0-draft
```

Run the bundled **evaluation** corpus of JPS `0.2.0-draft` through the experimental evaluator. Every
row is compared by disposition equality as §8.3 defines it — both the row's expectation and the
produced disposition go through the same RFC 8785 canonicalizer, so a set's stored order is not a
difference — or by its expected §8.4 error class and phase.
This reports results: the required, non-exhaustive evidence for the claim in
[CONFORMANCE.md](CONFORMANCE.md), and not that claim. A mismatching row decides nothing by itself,
because §3.4 makes a divergence as likely to be a defect in the row as in this implementation, and only
a project-issued erratum can excuse a row:

```bash
./bin/judgment-pack experimental evaluate-corpus
./bin/judgment-pack experimental evaluate-corpus --format json
./bin/judgment-pack experimental evaluate-corpus --spec-version 0.2.0-draft
```

Inspect or copy the bundled schema without network access:

```bash
./bin/judgment-pack spec schema 0.1.0-draft
./bin/judgment-pack spec schema 0.2.0-draft
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

## Experimental evaluation behavior

The evaluator implements the evaluator conformance class of JPS Core `0.2.0-draft`. The conformance
claim for it is stated, in full and only, in [CONFORMANCE.md](CONFORMANCE.md) — with its evidence, its
version scope, and everything it does not assert — and this section states no part of it: this section
is the behavior.

**Only a pack declaring `specVersion` `0.2.0-draft` is evaluated.** The §8.2 preflight, the §8.3
disposition shape, and the §8.4 error classes are that version's, and §11 makes a declared version
exact: "an unedited `0.1.0-draft` pack is not structurally conforming to `0.2.0-draft` and must be
re-declared before an implementation claiming this draft evaluates it." So a pack declaring any other
version — `0.1.0-draft` included — is refused as `pack-not-conformant` in the `preflight` phase
(`JPS-EVALUATION-PACK-SPEC-VERSION`), and the message cites that rule and states the remedy: **one
edit, the `specVersion` string**, and nothing else in the document, since §11 says `0.2.0-draft` changes
no part of the document format. There is no second, unclaimed legacy path. Document validation is
untouched — `spec validate` still validates a `0.1.0-draft` pack against its own published schema, and
document conformance needs no evaluator (§3.4).

Every evaluation payload still names both versions — `specVersion` is the pack's own, and
`evaluatorSpecVersion` is the contract's — because they are two different facts and a consumer should
read the applied contract rather than infer it. A refusal names the contract too, on
`evaluationError.evaluatorSpecVersion`. Payloads also carry a `conformanceClaimReference` member whose
value is `CONFORMANCE.md`: a locator, not a claim.

**Inputs are admitted before anything is resolved (§8.2).** They are validated in one order — the
pack, then the facts document, then the evidence-availability document, then the pack's
`metadata.requiredExtensions` against the caller's `--supported-extension` set — and that validation
finishes before §8 step 1 runs. An omitted `--evidence` document is the implicit empty object, which
makes every declared requirement `unknown`; that is the only form its absence takes, and it is not an
error. An evidence input that is not a JSON object, names an undeclared requirement, or carries a
value outside `present`, `absent`, and `unknown` is refused.

**A produced result is the portable disposition (§8.3).** Under `--format json` without `--pretty`
the `disposition` member is written in its RFC 8785 canonical form: members ordered by name, both sets
sorted and duplicate-free, absent members omitted rather than serialized as `null`, and no whitespace.
`--pretty` indents the whole payload and that indentation reaches inside this member too, so the member
order and both sets stay canonical but those exact bytes are not present. §8.3 requires
canonicalization "where a byte comparison is required", so a byte comparison against another
implementation must recanonicalize either side it did not itself produce; under `--pretty` it must.
The pack's configured escalation target is reported beside the disposition, in `handoffTarget`, never
inside it. Human output is unchanged prose.

**A refusal is an evaluation error, and never a disposition (§8.4).** Every evaluation this runtime
refuses reports exactly one class in band — `evaluationError.class`, with `evaluationError.phase` and
`evaluationError.evaluatorSpecVersion` — and no disposition at all, not even a partial one. That
includes every §8.2 preflight condition on every surface: a document above the byte limit and an empty
supplied evidence document are classed and ordered like any other, not refused ahead of the preflight.
The finer `JPS-*` code stays beside the class as its detail. An invocation that never became an
evaluation — a missing flag, an input this runtime could not read as a bounded regular file — is an
ordinary operational error and carries no class, because §8.4 classes evaluation conditions and leaves
transport undefined. The four classes are evaluated in Core's fixed order, which is the preflight order
above:

| Class | Reached when |
| --- | --- |
| `pack-not-conformant` | the pack input is not a semantically conforming document, at any layer |
| `malformed-input` | an input failed the preflight: unusable JSON, a non-object or invalid evidence document, an undeclared evidence key, or a document limit reached while admitting an input |
| `unsupported-required-extension` | the pack requires a capability this caller does not support |
| `resource-exhaustion` | a documented §10 limit was reached while evaluating an admitted input: this runtime's evaluation-work limit, on the ordinary Core path as well as under `--rfc0008-quantifiers` |

The phase split is the one §10 draws: a limit reached while *admitting* an input is
`malformed-input`, because the input was refused rather than partly processed, and
`resource-exhaustion` is reserved for a limit reached while *evaluating* an input already admitted.
This runtime's admission limits are the carrier limits listed under [security
defaults](#security-defaults). The 250,000-node cap is one of them, and its §10 category is stated
rather than left implicit: it is a budget over the whole parsed document, so it is a document-size
limit like bytes and depth, reached while admitting an input, and it is reported as
`malformed-input` in the `preflight` phase. It is *also* this evaluator's collection-size bound, for the
reason the next section gives: a collection of n members is n+1 parsed nodes, so a document-size budget
over nodes bounds every collection inside it.

### The two §10 limits of the claimed class

§10 requires an implementation claiming this class to define and document at least its collection-size
and evaluation-work limits, and makes reaching one of them during an evaluation `resource-exhaustion`
rather than a disposition. Both are defined here and enforced in
[`internal/evaluation/limits.go`](internal/evaluation/limits.go):

- **Evaluation-work limit: 20,971,520 work units per evaluation.** A unit is one visited condition
  node, one §8 iteration over an authored evidence requirement, exception, or rule, one step of a
  pointer resolution, or one byte of a path, an object member name, or a scalar token that a comparison
  has to read. The charge for each condition tree is complete before any predicate in it runs and §8's
  own iteration is charged before step 1, so the total does not depend on evaluation order and an
  exhausted limit never truncates a disposition — it produces `resource-exhaustion` in the `evaluation`
  phase with no disposition at all.

  The number is **derived in code** from the carrier's byte cap rather than chosen: it is exactly twice
  10 MiB, the largest input this runtime admits, and `limits.go` computes it from that constant so the
  two cannot drift. Stated exactly, that guarantees one thing and not more: **one full read of every
  admitted byte always fits** — every unit is backed by at least one byte of an admitted input, two
  documents reach an evaluation, and each is admitted under the same cap, so reading the whole pack once
  and the whole facts document once cannot exceed the limit. But **a single maximal cross-document
  comparison sits at the boundary and may be refused**: one comparison between a near-cap authored value
  and a near-cap selected value charges both sides, approaching the same total with §8's fixed
  per-node and per-iteration charges still to pay, and such an input is refused as
  `resource-exhaustion`. This runtime does not claim that only amplification is refused. Amplification
  is what the limit refuses in practice — the same large selected value re-read once per `in` candidate
  or once per condition — and against a 100 KB facts document the limit still admits about two hundred
  whole-document comparisons. Every row of the bundled evaluation corpus charges under 1,000 units.
  Callers may configure a lower limit per evaluation; the draft-RFC prototype has its own, smaller
  budget of 100,000 units (ADR-0009).
- **Collection-size limit: 250,000 members** — the 250,000-node carrier cap above, stated as the §10
  limit it is. Every input is admitted under that cap, so no admitted document holds a larger
  collection, and every collection this evaluator traverses comes from an admitted document: Core
  constructs none of its own and has no operator that iterates one. Because the bound is enforced while
  *admitting* an input, reaching it is `malformed-input` in the `preflight` phase — §2.1 refuses such a
  document whole rather than processing part of it — which is stricter than an evaluation-phase check of
  the same bound, not weaker. That is why this runtime documents the determination instead of adding a
  second mechanism that could only report what the preflight already refuses.

Limits are not portable: two implementations may set different ones, and an input above either is
outside the portable claim (§10). The evaluation corpus therefore keeps every case well inside any
plausible limit rather than probing one.

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

Runtime validation uses only files embedded in the binary and verified against one lock per bundled
specification version:
[`internal/artifacts/jps/0.1.0-draft/lock.json`](internal/artifacts/jps/0.1.0-draft/lock.json) and
[`internal/artifacts/jps/0.2.0-draft/lock.json`](internal/artifacts/jps/0.2.0-draft/lock.json).
Each lock records the source repository, exact commit/ref and source state, plus SHA-256 and size
metadata for every imported file — 50 for `0.1.0-draft`, and 56 for `0.2.0-draft`, whose bundle adds
the evaluation corpus of §3.4.1 (its manifest, that manifest's schema, and the four pack fixtures its
rows name). The release gate checks every bundle: a development snapshot remains visibly labelled
`unreleased-local-snapshot` and cannot pass it.

> **Provenance note.** The `0.2.0-draft` bundle is pinned to the specification tag `v0.2.0-draft`.
> The `0.1.0-draft` bundle stays pinned to an exact commit rather than to its tag: the `v0.1.0-draft`
> tag carries schema `$id`s under a temporary repository-hosted URL, while the permanent
> `https://judgmentpack.org/schema/` identifiers landed after it, and the specification's release
> tooling requires the tag string to equal `specVersion`, so they cannot be published under a second
> `0.1.0-draft` tag. A full-length commit digest is an explicitly supported immutable reference here,
> and the release gate accepts it.

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
