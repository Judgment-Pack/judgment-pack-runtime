# Changelog

All notable changes to tagged releases are documented here.

## Unreleased

- **Add the `explain_disposition` method prompt** (MCP `prompts` capability, ADR-0008), the
  fourth beside `author_pack`, `test_pack`, and `fix_pack`: non-normative guidance for
  narrating an evaluation payload strictly from the record it carries — ground every
  sentence in a payload or pack member, walk the `trace[]` in order, show how the
  disposition follows from it, and hold the line: the narrative must not soften, overrule,
  or extend the disposition, and the payload asserts nothing about the wisdom of acting on
  it (JPS §3.5). The evaluator's conformance claim is stated, in full and only, in
  `CONFORMANCE.md`, which no line of this entry restates.

## 0.5.0 - 2026-07-28

- **Distribute the released binary as an OCI image and publish it to the MCP registry**
  (ADR-0013). Post-gate release jobs build a `scratch` image from the published release
  archives — never a rebuild — push it to `ghcr.io/judgment-pack/judgment-pack`
  (linux amd64/arm64) under the immutable version tag, execute it anonymously on native
  runners of both architectures, and only after that passing run attest the digest,
  fast-forward `latest` (non-prereleases only), and publish the server to the official
  MCP registry as `io.github.Judgment-Pack/judgment-pack` via GitHub OIDC. Released
  image version tags are refused rather than overwritten, and an image whose smoke test
  fails is left unpromoted, unattested, and unpublished. `docs/mcp-clients.md` documents
  the container and registry install paths. The image carries the same `CONFORMANCE.md`,
  `LICENSE`, `NOTICE`, and `THIRD_PARTY_NOTICES` the archives do; the conformance claim
  is stated, in full and only, in `CONFORMANCE.md`, which no line of this entry restates.
  Nothing about the runtime itself changes: local execution, keyless, network-free, no
  HTTP surface (ADR-0004).
- The pre-gate release archive smoke matrix gains native `linux/arm64`
  (`ubuntu-24.04-arm`); previously that platform was published without ever being
  executed.
- CI validates the rendered MCP registry `server.json` template against the vendored
  registry schema — and against the server-side rules the registry enforces beyond it,
  learned live during the release candidates: an OCI package carries its version in the
  `identifier` tag and must not have a `version` member.

## 0.4.0 - 2026-07-28

- **Add the `jpack.json` project convention** (ADR-0012), a **non-normative convention of this
  runtime** and not part of the Judgment Pack Specification. It gives a project one name per decision
  that works identically from a shell, a CI step, and an agent's tool call. It is optional end to end:
  every command still takes a pack by path, every MCP tool still takes one as text, and a project
  without the file loses no capability. The configuration is selected by `--config`, then
  `JPACK_CONFIG`, then `./jpack.json`, and its schema is closed — every member it does not name is
  rejected, so a misspelled key is an error rather than an intention silently dropped. The published
  schema pins `configVersion` to the one value this runtime accepts, so a configuration held to those
  exact bytes in an editor or a CI step is held to what the runtime will actually read.
  - **New top-level `packs` namespace.** `packs list` reports the resolved inventory (the project's
    decision id, the pack document's own id and version, path, description, and `expectedVersion`
    status). `packs validate [--id X]` checks the configuration and then six named steps per pack —
    path containment, document validation, the `expectedVersion` pin, the optional filename
    cross-check, the hint keys, and matrix well-formedness — each reported as `passed`, `failed`, or
    `skipped`, so a check that passed is distinguishable from one the configuration never asked for.
    `packs test [--id X]` runs each pack's instance matrix through the **experimental** evaluator,
    which its help says plainly. `packs schema` prints the embedded configuration schema, on the same shape and with
    the same `--write` guards `spec schema` has. `packs validate` and `packs test` each exit `1` on any
    failure, so the CI gate is one line: `judgment-pack packs validate && judgment-pack packs test`.
  - **A project's matrix rows are the specification corpus's own case-carrier shape**, and they are run
    by the same code: `id`, `facts`, `evidenceAvailability`, `supportedExtensions`, and exactly one of
    `expectedDisposition` and `expectedErrorClass` (with an optional `expectedErrorPhase`). A row with a
    disposition passes on RFC 8785 canonical byte equality; a row with a class passes when the
    evaluation is refused with that class and phase. A builder's row is a corpus row once it names
    its pack fixture — the one member a corpus row adds, because a project matrix names its pack in
    the configuration — and a pack that declares no matrix is reported **skipped** rather than passed.
    A run in which no row ran at all is reported **skipped** and exits `1`: a green gate over zero rows
    would say a project was tested when nothing was.
  - **`configVersion` is a single integer as a string**, on the `outputVersion` precedent rather than
    semantic versioning; `"1"` is the only accepted value, and anything else is refused as `unsupported`
    with a message naming what this runtime does accept. **There is no templating, no target or
    environment blocks, and no selection**: a templated pack was never the pack anyone reviewed,
    environments are one file per environment by convention, and choosing which decision to ask stays
    with the application. Approval is your pull request; there is no approval state in the file.
  - **Identity is stated once and referenced three times.** The pack document's `id` and `version`
    members are what a pack is. `expectedVersion` in the configuration, the optional
    `<decision-id>-<semver>.pack.json` filename, and the new `packId`/`packVersion` payload members are
    each a *validated reference* to that statement: any of them may disagree with the document, every
    disagreement is an error, and none of them can win one. The filename convention is never required —
    a file named anything else has that check `skipped` — and it is binding when followed, on both the
    decision id and the version.
  - **Additive payload members, no protocol-version change.** Every evaluation payload, and every
    evaluation-corpus row, now carries `packId` and `packVersion`, read off the document that was
    actually evaluated. Two `VERSIONING.md` rules, read together, keep **`outputVersion` at `"2"`**:
    its compatibility-dimensions clause makes a break in machine-output compatibility the thing that
    increments the protocol version, and its MINOR bullet classes an added output field as a
    backward-compatible change. A consumer that never reads these members reads the payload it read
    before. A row or payload that produced no disposition carries neither member: there was no
    evaluation to read an identity off.
  - **`experimental evaluate --pack-id X`** resolves one decision id through the configuration
    (honoring `--config` and `JPACK_CONFIG`). It is mutually exclusive with the pack argument;
    supplying both, or neither, is an invocation error rather than a precedence rule. It is documented
    in the README command block and in the builder's guide, and it reads through the same rooted
    reader every other project read uses. It yields the pack's *bytes*, never a pathname for the
    generic reader to open again: a returned pathname would have to be reopened as a second
    operation on a filesystem that may have changed in between, which is precisely the interval the
    handle-bound reader below exists to remove.
  - **New MCP tools `list_packs` and `get_pack`**, and an optional `pack_id` argument on
    `experimental_evaluate` (mutually exclusive with `pack`, with the same presence and null-rejection
    discipline the existing arguments have). The server reads `JPACK_CONFIG` or the directory it was
    launched in and takes no path over the wire (ADR-0006); with no configuration, `list_packs` answers
    **empty with an explanation of where the runtime looked** rather than failing. `get_pack` serves a
    document that was read and did not decode with a status of `undecodable` and a `detail` saying why,
    rather than calling it valid with empty identity members — the same thing `list_packs` says about
    the same file. Every tool's `inputSchema` stays a flat object: the pack/`pack_id` exclusion is
    stated in both property descriptions and enforced by the handler rather than advertised as a
    composed schema keyword a bridge may drop. The server stays read-only, keyless, and offline.
  - **Hints are non-normative agent guidance and the runtime never resolves one.** A `facts` hint is
    keyed by the RFC 6901 pointer a pack reads, an `evidence` hint by a declared evidence-requirement
    id, and each says in the project's own words where the value is held. This runtime holds no
    credential, opens no network connection, and never reads a source a hint names (ADR-0004,
    ADR-0006). The gathering is the agent's, and an unsourceable fact is reported `unknown` rather than
    guessed, so the pack escalates instead of deciding on an invention. Because nothing else ever
    resolves a hint key, `packs validate` checks it against the pack document: an `evidence` key must
    be a declared evidence-requirement id, and a `facts` pointer must be one some condition reads or an
    ancestor of one. A misspelled key is a failed check rather than an instruction an agent follows.
  - **Every file access is bound to a handle held open on the configuration's own directory** through
    the new `fssecure.Relative` and `fssecure.Root`. A path that escapes is refused **twice**: when the
    configuration is validated, before anything is read, and again at read time, where resolution
    against the handle catches an escape through a symlinked component that a lexical check cannot
    see. The read-time half is a handle rather than a pathname so that **containment holds through the
    open and not merely up to it** — a pathname checked and then opened leaves an interval in which an
    intermediate directory component can be swapped for a symlink pointing out of the root, and the
    open follows it, with a post-hoc check on the opened file unable to notice. `internal/fssecure`
    opens the project directory once, at load, and resolves every later read relative to that handle
    through Go's `os.Root`. A final component that is a symlink is still refused whatever it points at,
    and only a regular file is read. Every surface that reaches a pack through the configuration takes
    this one reader — none is handed a pathname to open itself — and `packs validate` asks its
    containment question of the same handle, so a symlinked-component escape fails the check named for
    containment rather than the one after it. The project's root is the handle's own directory rather
    than a second derivation from the configuration path, so the root and the configuration bytes
    cannot describe two different directories.
  - **The minimum Go version is now 1.24** (from 1.21). `os.Root` is the standard library's
    handle-bound opener and is the containment primitive above; hand-rolling one per platform to keep
    an older floor would be reimplementing a security primitive for no gain. The release workflow
    continues to pin its own supported toolchain independently.
  - **A configuration must declare at least one pack.** The schema's `packs` object carries
    `minProperties: 1`, and the test runner independently demotes any run that executed zero rows to
    `skipped` regardless of how many packs were selected. Either alone would leave a hole: without the
    schema constraint an empty project is a valid configuration, and without the runner's demotion any
    other route to an empty selection reports a clean run over nothing.
  - New documentation: [`docs/building-with-packs.md`](docs/building-with-packs.md), the builder's
    guide — the packs-as-code lifecycle, the three-owner model (the application selects, the agent
    gathers and never invents, the pack judges, with `not-applicable` as the misrouting net), hints in
    practice, `expectedVersion` discipline, the filename convention, and the
    data-sufficiency-as-another-pack pattern. A README section and an
    [`docs/mcp-clients.md`](docs/mcp-clients.md) section cover the same ground briefly. Every new help
    text, tool description, and label is **reference-only**: `packs test` says where the evaluator's
    conformance claim is stated and states no part of it, and the claim-surface inventory now walks the
    new CLI help texts and the new MCP tool descriptions and asserts them by name.

- **Take the §3.4.1 conformance decision, and add the root [`CONFORMANCE.md`](CONFORMANCE.md) it is
  stated in** (ADR-0011). This entry is **reference-only**, deliberately: §3.4.1 fixes the entire form
  such a statement may take, so the class, the version scope, the corpus, the results, the evidence and
  its limits, the activation point, and everything not asserted are in that file and in no other
  sentence in this repository — a partial restatement here would be the partial form §3.4.1 forbids.
  Read `CONFORMANCE.md` for all of it; what follows is what changed in this runtime to make the
  decision takeable, and each item is a change to behavior, output, or documentation rather than a
  restatement of the file. Activation is not release-conditional — §3.4.1 requires no tag — so a
  development build from this history behaves exactly like a released one, and `CONFORMANCE.md` states
  where activation begins and how long it persists.
  - **Breaking: the evaluator now evaluates only a pack declaring `specVersion` `0.2.0-draft`.** §11
    makes the declared value exact — an unedited `0.1.0-draft` pack "must be re-declared before an
    implementation claiming this draft evaluates it" — so a pack declaring any other version is refused
    as `pack-not-conformant` in the `preflight` phase with the new
    `JPS-EVALUATION-PACK-SPEC-VERSION` code, on `experimental evaluate`, `experimental evaluate-corpus`,
    and `experimental_evaluate` alike, with and without `--rfc0008-quantifiers`. There is no legacy path.
    **Migration is one edit per pack:** change the `specVersion` string to `0.2.0-draft` and nothing
    else, because §11 says that version changes no part of the document format — every member, every
    cross-field rule, and every document-conformance verdict of §§3.1–3.3 is unchanged. Document
    validation is untouched: `spec validate` still validates a `0.1.0-draft` pack against its own
    published schema, and document conformance needs no evaluator (§3.4). This supersedes ADR-0010's
    rejection of that behavior, which was sound only while nothing was claimed.
  - **The §10 limits the class requires are supplied first**, which is the precondition ADR-0010
    deferred and named. `resource-exhaustion` is now reachable on the ordinary Core path and not only
    under `--rfc0008-quantifiers`: an **evaluation-work limit of 20,971,520 units** per evaluation
    (`DefaultCoreWorkLimit`, configurable per evaluation) is charged over every condition node, §8
    iteration, pointer resolution, and compared byte, using the accounting model the draft-RFC prototype
    already documented — so one model now serves both paths, plus one unit per authored evidence
    requirement, exception, and rule, charged before §8 step 1. Each condition tree's charge completes
    before any predicate in it runs, so an exhausted limit is an explicit `resource-exhaustion` error in
    the `evaluation` phase with no disposition and no partial state, never a truncated result. The
    number is **derived in code** as twice the carrier's 10 MiB per-document byte cap, so it cannot
    drift from it. That ratio is an arithmetic fact about two numbers and **gives no whole-evaluation
    guarantee**: three documents may be admitted, and a unit is charged per use rather than once per
    admitted byte. Every row of the bundled corpus charges under 1,000 units, which measures those rows
    and bounds nothing else.
  - The **collection-size limit is the carrier's 250,000-node cap**, documented as the §10 limit it is
    rather than duplicated as a second mechanism: every input is admitted under that cap, so no admitted
    document holds a larger collection and Core constructs none of its own. Reaching it is
    `malformed-input` in the `preflight` phase, which is §10's own phase split and stricter than an
    evaluation-phase check of the same bound.
  - **The `experimental` namespace stays and no command is renamed**; what changes is what it says.
    "Experimental" now means only what it always meant about stability — this surface may change or be
    removed without compatibility promise — and every surface that denied a claim is now **reference-only**:
    the CLI help, the human output header, the corpus label, the MCP tool descriptions, the `test_pack`
    prompt, the draft-RFC prototype note, the README, and the docs each say that the conformance claim is
    stated, in full and only, in `CONFORMANCE.md`, and no sentence outside that file states any part of
    it. That is §3.4.1's requirement and not a style choice: it fixes the entire form a claim may take, so
    a surface naming the class and the version while omitting the corpus version, the results, and the
    every-row statement would be making a partial claim §3.4.1 forbids. The rule is **enforced over the
    maintained prose too**, not only over the runtime's output: the inventory test walks this CHANGELOG's
    Unreleased section, the README, every document under `docs/`, the decision record, and the *rendered*
    MCP prompts, and it fails on an assembled claim — one statement holding the class, the exact version,
    and the corpus version or its results — as well as on a claiming or denying phrase. Two dated records
    (ADR-0007, ADR-0010) and released sections of this file are outside the walk, since neither is a
    statement about the current posture and rewriting decision history to match one would be the worse
    defect.
  - **Breaking, with `outputVersion` incremented from `"1"` to `"2"` on every payload.** The in-band
    `conformanceClaim` member — which carried `"none"` — is **removed** and replaced by
    `conformanceClaimReference`, whose value is the fixed string `"CONFORMANCE.md"`: a locator, not a
    claim. Renaming a member breaks a consumer that read the old name, and
    [`VERSIONING.md`](VERSIONING.md) requires a change that breaks machine-output compatibility to
    increment the protocol version deliberately, so it is incremented — for every command's payload, not
    only the evaluation ones, because one protocol version describes one output contract.
    **Migration:** read `conformanceClaimReference` where `conformanceClaim` was read and expect the fixed
    value `"CONFORMANCE.md"`; a consumer that asserted `conformanceClaim == "none"` should instead treat
    the reference as opaque and read the claim from that file. Branch on `evaluatorSpecVersion` (still
    `"0.2.0-draft"`) for the contract version, and on `specVersion` for the pack's own — the two must now
    agree for a pack to be evaluated at all. A consumer that pins `outputVersion` must accept `"2"`; every
    other member of every payload is unchanged.
- Bundle JPS `0.2.0-draft` alongside `0.1.0-draft`, imported from the specification tag
  `v0.2.0-draft` with the maintainer tool. The new bundle adds the evaluation corpus of §3.4.1 — its
  manifest, that manifest's schema, and the four pack fixtures its twenty rows name — which the
  import tool now carries for any specification version that publishes one. `spec validate` accepts a
  pack declaring either version and still selects the schema by the version the document itself
  declares; `spec schema`, `spec test-conformance --spec-version`, `version`, `get_schema`, and
  `describe_runtime` all reach both. **No default changes**: every surface that selects a version for
  a caller who named none still selects `0.1.0-draft`. The release gate now verifies the lock of
  every bundled version rather than only the default one, and the one `artifactProvenance` string in
  the runtime description now describes every bundle, so a development snapshot in a
  non-default version can no longer hide behind the default version's provenance.
- Align the EXPERIMENTAL evaluator to the evaluator conformance class of Core `0.2.0-draft`
  (ADR-0010), under the `experimental` namespace and claiming nothing *as that change landed* — JPS
  §3.4.1 defines the one form an evaluator-conformance claim may take, and taking that decision was
  left to a separate ADR, which the entry above is. The contract is applied to a
  pack of either bundled version whatever that pack declares, so every evaluation payload and every
  `evaluationError` now names the contract's own version in a new `evaluatorSpecVersion` member beside
  the pack's `specVersion`: §11 says these semantics existed for no consumer under `0.1.0-draft`, so a
  payload carrying only the pack's version would read as a `0.1.0-draft` disposition, which does not
  exist. Three normative sections are implemented:
  - **§8.2 input preflight.** Inputs are admitted in Core's order — pack, facts, evidence,
    required-extension set — and the preflight completes before §8 step 1, so no result can outrace
    an input error: a pack whose applicability is false, presented with an evidence document carrying
    an undeclared key, is now the input error rather than the `not-applicable` disposition. An omitted
    evidence document is the implicit empty object, the only form its absence takes.
  - **§8.3 portable disposition.** Under `--format json` without `--pretty`, the `disposition` member
    is now written in its RFC 8785 canonical form, produced by a new stdlib-only `internal/jcs` encoder
    over the strings, arrays, and objects a disposition is made of. `--pretty` indents the whole
    payload and reaches inside that member too, so under it the member order and both sets stay
    canonical and the exact bytes do not appear; §8.3 requires canonicalization where a byte comparison
    is required, so a comparison recanonicalizes either side it did not produce. `handoff` gains
    `triggeredBy`, the retained reasons that triggered a request, present exactly when the state is
    `requested`. Canonicalization is also the one place that refuses a disposition §8.3 forbids, so no
    value assembled outside the engine can violate any invariant that section states about the
    disposition alone: the `kind`, `handoff.state`, and reason vocabularies, its three presence rules,
    the exact reason set of a `not-applicable` result, and `triggeredBy` being a subset of `reasons`.
    **Breaking for a consumer of the experimental payload:** the disposition no longer echoes the
    pack's escalation target, which §8.3 keeps out of it; the target moved to a sibling
    `handoffTarget` member. Human output is unchanged prose, now naming the triggering reasons.
  - **§8.4 error contract.** Every refused evaluation reports exactly one of the four Core classes in
    band, as a new `evaluationError` member carrying `class` and `phase`, with no disposition at all;
    the existing `JPS-*` codes stay beside the class as the finer detail. The classes are evaluated in
    Core's fixed order, which is the preflight order, so an unsupported required extension is now
    reported as `unsupported-required-extension` rather than folded into
    `JPS-EVALUATION-PACK-NOT-CONFORMANT`, and a malformed input outranks it on the same inputs.
    Reaching a limit while admitting an input is `malformed-input`, and that now includes the input
    byte limit on every surface: the CLI reports an oversized input inside the preflight instead of
    refusing it at the read, so it carries a class and takes its place in the fixed order — an
    oversized facts document no longer outranks a non-conformant pack. `resource-exhaustion` is the one
    class of the evaluation phase, and as this change left it, it was reached only by the
    `--rfc0008-quantifiers` work budget — no evaluation-work charge was levied on the Core path at all.
    The conformance-claim entry above closes that gap in the same release, so the `resource-exhaustion`
    a consumer sees is the Core path's too. Both surfaces report the same envelope: an `experimental_evaluate` refusal
    is an in-band MCP tool error whose `structuredContent` is the `evaluationError` payload the CLI
    writes, so the class, the phase, and `evaluatorSpecVersion` are machine-readable there too rather
    than sentences a client has to classify itself.
  - **§8.2 on the MCP surface.** `experimental_evaluate` now decodes each document argument's presence
    separately from its value, so the two meanings §8.2 gives an absent and an empty evidence document
    stay apart on the wire: omitting the key is the implicit empty object and evaluates, while a key
    present with an empty string is a supplied document and is `malformed-input`. A present-but-empty
    `pack` or `facts` likewise enters the preflight — `pack-not-conformant` and `malformed-input` —
    instead of being reported as an unclassified missing argument, an absent required key stays an
    invocation failure with no class, and an explicit `null` is rejected as the argument-type error the
    declared string schema makes it.
- Add `judgment-pack experimental evaluate-corpus`, which runs the bundled evaluation corpus for one
  exact specification version and reports every row: the disposition compared as §8.3 defines
  disposition equality — both sides through the same canonicalizer, so a set's stored order is not a
  difference — or the expected §8.4 class and phase. Its output is labelled as corpus results in both
  formats and a mismatch exits 1. **A run is not a claim**: §3.4.1 makes corpus results the required
  and non-exhaustive evidence a statement of that class rests on, and this repository states one in full
  and only in `CONFORMANCE.md` (see the entry above). Run the verb for the current result rather than
  reading one out of a changelog, since a mismatching row would decide nothing by itself (§3.4). The
  verb is CLI only; the MCP surface does not expose it.

## 0.3.0 - 2026-07-28

- Add a DRAFT-RFC PROTOTYPE of the specification's RFC 0008 (Draft), bounded collection quantifiers,
  behind the new `judgment-pack experimental evaluate --rfc0008-quantifiers` flag (ADR-0009, under
  ADR-0007's experimental umbrella). The flag admits three condition operators — `exists`, `every`,
  and `uniform` — with the RFC's element re-rooting, its pinned empty-array values, its
  aggregate-depth bound of two, and a candidate work-accounting model the RFC itself leaves open.
  Every successful evaluation payload produced this way carries a new output member,
  `draftPrototype`, naming the operators used and stating that a pack using one is NOT valid under
  any published JPS version; a refusal carries no such member, because a grammar or work-limit
  failure is reported through the ordinary operational-error envelope. Four diagnostic codes are
  minted, `codeStability: "provisional"` like every other: `JPS-EVALUATION-RFC0008-GRAMMAR`,
  `JPS-EVALUATION-RFC0008-DEPTH`, `JPS-EVALUATION-RFC0008-SHAPE`, and
  `JPS-RESOURCE-EVALUATION-WORK-LIMIT`. **No conformance claim and no validation behavior changes**:
  `spec validate` is untouched and still rejects every pack using an operator, the evaluator without
  the flag refuses one for the same reason, the MCP surface does not expose the flag, and everything
  the draft grammar does not add is still held to full document conformance through the pack's Core
  projection. The operators belong to an open proposal that may never be accepted.
- Make §7.4 equality total in the EXPERIMENTAL evaluator. JSON numbers are compared as normalized
  tokens — sign, significant digits, adjusted exponent — rather than through arbitrary-precision
  arithmetic, so `1e3`, `1000`, and `1.0e3` are one value, `-0` equals `0`, and a pair no arithmetic
  type can hold (`1e999999999` against `2e999999999`) compares `false` instead of degrading to
  `unknown`. `equals`, `not-equals`, and `in` therefore always decide, and `uniform` needs no arm
  beyond RFC 0008's five clauses: an evaluator's inability to represent an admitted value is a
  resource condition, not a semantics two implementations would have to agree on. Ordered comparison
  is unchanged — still defined only over §2.2 decimal strings — and `spec validate`, the conformance
  corpus, and the exit classes are untouched.
- Add the MCP `prompts` capability (ADR-0008): three non-normative authoring-method prompts —
  `author_pack`, `test_pack`, `fix_pack` — served as static, versioned text that the client's model
  executes with the client's key. The server still calls no model, holds no key, and opens no
  network connection; serving a prompt is read-only, like `get_schema`. Method content distills the
  expressiveness studies: the resolution-model shapes that avoid conflicts, the `onUnknown`
  discipline, the decimal-string rule for ordered comparisons, the prepared-facts ledger, and the
  instance-matrix logic probe. Following a prompt does not make a pack conformant, and no prompt
  interprets any policy.

## 0.2.0 - 2026-07-27

- Add an EXPERIMENTAL evaluator (ADR-0007): `judgment-pack experimental evaluate` and the
  `experimental_evaluate` MCP tool apply the JPS Core §§7–8 experiment, as pinned by the
  specification's RFC 0006 (Draft), to one conformant pack, one facts document, and an optional
  tri-state evidence-availability input, returning a disposition (kind, reasons, handoff) and a
  trace. It claims NO evaluator conformance — JPS `0.1.0-draft` forbids such claims — authorizes
  nothing, evaluates only fully conformant packs, and may change or be removed without
  compatibility promise. The nine walked instances committed in RFC 0006's appendix run as this
  surface's acceptance tests. Validation behavior, claims, and exit classes are unchanged.

## 0.1.0 - 2026-07-25

- Add a `NOTICE` file identifying Brian Jin as the copyright holder and carrying
  the attribution required for the embedded Judgment Pack Specification artifacts, and ship it in
  the release archives.
- Record the embedded specification bundle in `THIRD_PARTY_NOTICES`. The bundle is Apache-2.0
  material from a separate project and appears in neither `go.mod` nor `go.sum`, so the previous
  Go-modules-only framing omitted it.
- Re-vendor the embedded specification bundle from `Judgment-Pack/judgment-pack-spec`, replacing the
  pre-neutralization pin. The two schema `$id` values now carry the permanent
  `https://judgmentpack.org/schema/` identifiers. Machine-visible: `spec schema` reports a new
  `schemaId`, `sha256`, and `bytes`, and the bundle digest changes. No validation behavior changes
  and all 47 conformance cases still pass — the corpus is byte-identical to the previous pin.
- Pin the bundle to an exact commit rather than a tag. The specification's release tooling requires
  a tag string to equal `specVersion`, so the permanent identifiers on its `main` branch cannot be
  republished under a second `0.1.0-draft` tag. The release gate already treats a full-length commit
  digest as an immutable reference; the pin moves back to a tag once a specification version
  carrying those identifiers is published.
- Pin the machine contract in tests: `outputVersion`, `tool.name`, the six exit classes, and the
  four envelope members every JSON payload carries. These were previously enforced only by
  convention, so a rename or an accidental version bump passed silently.
- Add a `judgment-pack mcp` subcommand: a hand-rolled, dependency-free Model Context Protocol server
  over stdio that exposes the offline validation, conformance, and description operations as MCP
  tools. It holds no credential, opens no network connection, and evaluates nothing. See ADR-0003.
- Make validator diagnostics self-sufficient. Structural and carrier messages now name the offending
  value and the fix — the allowed enum values, the identifier grammar with an example, the required
  and actual collection counts, the valid condition `op` values, the exact effect/operand mismatch for
  an exception, and (for carrier parse errors) the line, column, and byte offset with the decoder's
  reason. Messages and provisional codes only; all 47 conformance cases still pass and the
  machine-output contract is unchanged.
- Unify diagnostic code prefixes under `JPS-`. The CLI-invocation codes previously prefixed `JPR-`
  are now `JPS-INVOCATION-*`; the process exit class, not the prefix, carries the error category.
  Codes remain provisional. See ADR-0005.
- Populate the `extensions` summary in `spec validate` JSON output even when a document is invalid at
  the semantic layer, so a declared required extension is no longer dropped from the summary.
- Surface the embedded valid conformance fixtures read-only: `list_examples` / `get_example` MCP
  tools and a `spec examples [name] [--write …]` CLI command. They give a filesystem-less client a
  conformant starting point for Create, return the exact digest-locked bytes the bundle embeds, and
  label every payload `kind: version-pinned-conformance-fixture` so a fixture is never mistaken for
  an authored template. Read-only, offline, keyless, dependency-free. See ADR-0006 and the new
  `docs/authoring-lifecycle.md`.

## 0.0.1 - 2026-07-23

- Establish `judgment-pack-runtime` as the vendor-neutral reference runtime for the Judgment Pack
  Specification (JPS) under the `Judgment-Pack` organization. This project originated as the
  `protoss-cli` reference validator and was renamed and relocated to a vendor-neutral home; it is a
  reference implementation, not the only valid one.
- Provide the offline `judgment-pack spec` command namespace (`validate`, `test-conformance`,
  `schema`) plus `version`, built as the `judgment-pack` binary with a `jpack` short alias.
- Implement strict carrier, structural, semantic, and extension-capability validation. The runtime
  validates documents only; it does not evaluate rules, choose an outcome, or authorize an action,
  matching the JPS `0.1.0-draft` scope (no evaluator conformance class).
- Embed and integrity-check JPS `v0.1.0-draft`, pinned byte-for-byte to its immutable upstream
  release tag.
- Provide bundled and local conformance-suite execution, versioned JSON output, and stable process
  exit classes.
- Provide cross-platform release archives, SHA-256 checksums, and build-provenance attestations.

### Known follow-ups

- The embedded specification bundle is pinned to the pre-neutralization upstream tag
  (`v0.1.0-draft`), whose schema `$id` values use temporary repository-hosted URLs. (Resolved in
  Unreleased by re-vendoring from `Judgment-Pack/judgment-pack-spec` at an exact commit. The
  original entry attributed the delay to the specification project not having published a neutral
  tag; the permanent identifiers were in fact already on its `main` branch.)
- No `GOVERNANCE`/`MAINTAINERS`/`CODEOWNERS` files exist yet. (The `NOTICE` file is added in
  Unreleased. `LICENSE` is deliberately left byte-identical to the canonical Apache-2.0 text: its
  appendix is a template for marking your own files, not a field to fill, and editing it costs a
  clean Apache-2.0 match on automated license scanners. The copyright holder is named in `NOTICE`.)
