---
status: accepted
date: 2026-07-28
deciders: Brian Jin
---

# Adopt a `jpack.json` project convention in the runtime, deliberately outside the specification

## Context and problem statement

A project that owns more than one pack has no name for a pack. It has paths — one in a shell
script, another in a CI job, and a blob of text handed to a model — and nothing that ties them
together or says which packs the project owns at all. Every builder who reaches that point invents
the same index file, and each invention is slightly different, so an agent cannot read one without
being told its shape first.

The runtime already refuses to hold state, credentials, or a store
([ADR-0004](0004-decline-http-api.md), [ADR-0006](0006-authoring-lifecycle-in-the-client.md)), and
the specification deliberately says nothing about how packs are stored. The question is therefore
not *whether* projects need this — they build it anyway — but whether this runtime should carry one
version of it, where, and with what refused.

The pressure is real in the other direction too. An index file is exactly the artifact that grows a
templating engine, per-environment target blocks, an `approved: true` flag, and a selection rule,
and each of those would move something out of the reviewed pack document and into a configuration
file nobody reviews with the same care. This record exists as much to refuse those as to adopt the
file.

## Decision drivers

- One name per decision, usable identically from a shell, a CI step, and an agent's tool call.
- An agent needs a **token-cheap** answer to "what can this project decide?" that does not require
  fetching every pack document.
- The **one-statement discipline** the rest of this repository already follows: a fact stated in two
  places can disagree with itself. A pack's identity is stated in the pack document, and every other
  mention of it has to be a check rather than a second statement.
- The runtime's posture is load-bearing and non-negotiable: keyless, offline, no store, no
  credential, read-only over the wire (ADR-0004, ADR-0006).
- The specification is not the place to settle this yet, and settling it there first would freeze a
  shape nobody has used.
- Nothing here may create a second way for a claim-adjacent surface to overstate what the evaluator
  is; the reference-only rule of [ADR-0011](0011-first-evaluator-conformance-claim.md) applies to
  every text this adds.

## Considered options

- **A. Adopt `jpack.json` as a runtime convention**, closed-schema, non-normative, with `packs list`,
  `packs validate`, `packs test`, `packs schema`, evaluation by decision id, and the two MCP
  inventory tools.
- **B. Adopt nothing.** Paths everywhere; every project invents its own index.
- **C. Take it to the specification first**, as an RFC defining a normative project format every
  implementation must read.
- **D. Adopt a richer configuration**: templating, per-environment targets, an approval state, and a
  selection rule the runtime evaluates.

## Decision outcome

Chosen option: **A**.

**B is rejected because the file gets built regardless**, and building it eleven different ways is
worse than building it once. The convention costs a project nothing to ignore: every command still
takes a pack by path, every MCP tool still takes one as text, and a project with no `jpack.json`
loses no capability — `list_packs` answers *empty, with an explanation of where the runtime looked*
rather than failing.

**C is rejected as premature, and this record is the alternative to it.** RFC 0005 asks how packs
are organized and distributed, and the honest answer today is that nobody has run a project this way
for long enough to know which parts matter. A specification written from a design conversation would
freeze a guess. A runtime convention, used, complained about, and revised, is what produces the
evidence an RFC should be written from — so this is deliberately **runtime-convention, not spec**,
and it is intended to generate RFC 0005's first empirical evidence rather than to pre-empt it.

**D is rejected in four separate parts**, each recorded below, because each is a different mistake
and a later reader deserves to know which of them was considered.

Settled determinations:

- **`configVersion` is a single integer as a string, and `"1"` is the only accepted value.** The
  precedent is `outputVersion` in `internal/result`, not semantic versioning. A version exists here
  so a program can say "I do not know this shape"; there is no minor or patch component because
  there is nothing to negotiate between them — a reader either knows the shape or does not, and a
  shape that added an optional member would still be `"1"` for every reader that ignores it. A value
  this runtime does not accept is refused as `unsupported` with a message naming what it does accept,
  so a configuration from a newer toolchain produces one actionable sentence instead of a list of
  unrecognized members. The version is read **before** the schema runs, for exactly that reason.

- **No templating.** A pack assembled from variables at load time was never the pack anyone reviewed,
  and the reviewed artifact is the entire reason a decision is encoded as a document. Templating also
  makes a pack's behavior a function of its environment, which means the matrix that passed in CI
  tested a different pack than the one that ran. There is no variable substitution, no
  interpolation, and no include.

- **No targets and no environments.** One file per environment, by convention — `jpack.json`,
  `jpack.staging.json`, selected with `--config` or `JPACK_CONFIG`. A target block is a second place
  a pack's identity is decided, and it costs an agent context: `list_packs` would have to explain a
  resolution order before a model could read a single row, and every hint and description would
  arrive conditioned on a target the model has no way to choose. Two files that a diff can compare
  are cheaper for a human too.

- **Identity lives in the pack document; every other mention of it is a validated reference.** This
  is the one-statement discipline applied to identity. The pack document's `id` and `version` members
  are what a pack *is*. Three things name a version beside them, and none of them is a statement:
  - `expectedVersion` in the configuration is a pin the project asserts, and `packs validate`
    reports a difference from the document as an error rather than preferring either value;
  - the optional `<decision-id>-<semver>.pack.json` filename is cross-checked when it is followed —
    both halves against the configuration key and the document's `version` — and **skipped entirely**
    when a file is named anything else, because a naming convention that became a requirement would
    be a fourth place identity is declared;
  - `packId` and `packVersion` on every evaluation payload are read off the document that was
    actually evaluated, so a payload cannot name a pack the evaluation did not read.

  Each of the three can disagree with the document, and each disagreement is an error. None of them
  can win one.

- **Selection stays with the application.** The configuration lists what a project can decide; it
  does not choose a pack for a request, and this runtime never will. The reason is RFC 0004's:
  applicability is not authorization. A pack's top-level `applicability` member says whether the pack applies
  to the facts handed to it, which is a different question from whether it was the right pack to hand
  them to — a pack that could nominate itself would be deciding whether it is authorized to decide.
  What the convention adds is that a `not-applicable` result becomes a usable **misrouting net**: when
  the application selects wrongly, a well-written applicability returns `not-applicable` instead of an
  outcome computed from facts that mean something else.

- **Approval is the pull request.** There is no approval state in the file. Branch protection, review,
  reviewers, and the audit trail already exist in version control, already work, and are already what
  an auditor asks for; a boolean in a JSON file that its own author flips measures the author's care
  and nothing else. This is the same reasoning the ADR review declaration rests on in
  [`README.md`](README.md).

- **Hints are non-normative agent guidance, and the runtime never touches a connector.** A `facts`
  hint is keyed by the RFC 6901 pointer a pack reads and says, in the project's own words, where the
  value is held and how to get it; an `evidence` hint does the same for a declared
  evidence-requirement id, and **both keys are checked against the pack document by `packs
  validate`** — the one-statement discipline applied to a cross-reference nothing else ever resolves.
  An evidence key names a declared requirement or the check fails; a facts pointer is read by some
  condition, or is an ancestor of one an agent must gather, or the check fails. Leaving it unchecked
  would make a misspelled key an authoritative instruction to an agent, which is the one way a hint
  this runtime never acts on can still cause harm. The runtime carries them and acts on none: it
  holds no credential, opens no network connection, and does not know what a warehouse is. Keyless and offline is load-bearing
  (ADR-0004, ADR-0006), and a hint that the runtime resolved would end that in one release — the
  first connector needs a credential, the credential needs an identity, and the identity needs a
  store. The gathering is the agent's, with the agent's own access, and the rule that makes it safe is
  that **an unsourceable fact is reported unknown rather than guessed**: the resolution model handles
  `unknown` deliberately, and a gathering step that fills the hole first turns an escalation into an
  outcome nobody made.

- **Every file access is bound to a directory *handle* on the configuration's own directory, and an
  escaping path is refused twice.** Once when the configuration is validated, before anything is
  read, and again at every read. Both are necessary and neither substitutes for the other: a check
  only at read time would let `packs validate` report a clean project no surface can use, and a
  lexical check alone would follow a symlinked directory out of the root.

  The read-time half is a handle and not a pathname, and that is the substance of this
  determination rather than an implementation note. Containment decided against a pathname is a
  statement about the filesystem as it was, and the open that follows acts on the filesystem as it
  is; in between, an intermediate directory component can be renamed away and replaced with a
  symlink pointing out of the root, and the open follows it. Re-checking the opened file afterward
  does not catch that, because the file that was opened really is the file the pathname then named.
  So `internal/fssecure` opens the project directory once, at load, and resolves every later read
  relative to that handle through Go's `os.Root`: the root a read is bounded by is the directory
  that was opened, not whatever its pathname denotes at the moment of the read, and rearranging
  components afterward cannot widen it. The window is removed rather than narrowed, which is why
  this is stated as containment holding *through* the open. It is also why the module's minimum Go
  version is 1.24 — `os.Root` is the standard library's handle-bound opener, and hand-rolling one
  per platform to keep an older floor would be reimplementing a security primitive for no gain.

  Two checks remain about *what* is read rather than where it is; both are resolved through the same
  handle and both fail closed. A final component that is a symlink is refused even when it points
  inside the root, and anything that is not a regular file is refused.

  The corollary is what the implementation is held to: **no surface may stop at the lexical half,
  and no surface may hand back a pathname for something else to open.** A pathname returned from a
  containment check reintroduces the interval the handle exists to remove, so a caller reaching a
  pack by decision id — `experimental evaluate --pack-id`, on both the CLI and MCP — receives the
  bytes read through the project's own handle. `packs validate` asks its containment question of
  that same handle, so the named check states the containment truth rather than the lexical half of
  it, and cannot describe a different directory from the one the read is bounded by. The project's
  root is likewise the handle's own directory rather than a second derivation from the
  configuration path: the configuration is read *through* the handle, so the root and the bytes are
  one fact and cannot disagree.

- **The MCP surface stays read-only, keyless, and offline.** `list_packs` and `get_pack` read; nothing
  writes. The server takes no configuration path over the wire — it uses `JPACK_CONFIG` or the
  directory it was launched in — for ADR-0006's reason: a long-lived wire endpoint that accepted a
  path would make its answers depend on the client's idea of the server's filesystem, which is the
  same reason no tool accepts a document by path. `experimental_evaluate` gains `pack_id` as an
  argument mutually exclusive with `pack`; supplying both is refused rather than given a precedence
  rule, and the presence rules match the existing argument discipline exactly — an explicit `null` is
  rejected, and omitting the key is the only form absence takes.

- **`packId` and `packVersion` are additive members and `outputVersion` stays `"2"`.** Two
  `VERSIONING.md` rules give this, and they are cited separately because no single sentence there says
  it: its "Compatibility dimensions" clause makes a break in machine-output compatibility the thing
  that increments the protocol version, and its MINOR bullet under "Version increments" classes an
  added output field as a backward-compatible change to the CLI release. A consumer that never read
  these members reads the same payload it read before, so nothing breaks and the protocol version does
  not move. That is the opposite of ADR-0011's member *rename*, which did break a reader and did cost the
  increment.

- **This is a runtime convention and is deliberately not the specification's.** It is named in
  ADR-0012 and in `docs/building-with-packs.md`, its payloads carry
  `kind: non-normative-runtime-convention` in band so a consumer learns it from the payload without
  reading either, and no JPS implementation is obliged to understand a `jpack.json`. Data sufficiency
  — "do we have enough information to decide this?" — is handled the same way it is handled anywhere
  else: it is another decision, so it is another pack, run first by the application. Composing them is
  the application's today; the specification's RFC 0002 graph work is where composition may eventually
  be described, and this convention deliberately stops short of it rather than growing a
  composition mechanism of its own.

- **Every text this adds is reference-only** (ADR-0011). `packs test` runs the experimental evaluator,
  so its help says so plainly and points at `CONFORMANCE.md`; no help text, tool description, label, or
  payload added here states any part of what that file says. The claim-surface inventory in
  `internal/cli/app_test.go` walks the new CLI help texts and — through a live `tools/list` response —
  the new MCP tool descriptions, and it now asserts the new surfaces by name rather than only by count.

### Consequences

- Good, because a project gets one name per decision and an agent gets a token-cheap inventory it can
  read before fetching anything. The CI gate is one line —
  `judgment-pack packs validate && judgment-pack packs test` — and it is the same line on every
  project. A `packs test` run in which no row ran at all is reported `skipped` and exits non-zero, so
  that line cannot go green over a project whose packs declare no matrix: a per-pack `skipped` row
  under a `passed` top line is not the thing a CI step reads.
- Good, because a project matrix is judged by the *same code* the bundled evaluation corpus is judged
  by: the RFC 8785 canonical §8.3 disposition compared byte for byte, or the expected §8.4 class and
  phase. A builder's row is a corpus row once it names its pack fixture — the single member a corpus
  row adds, because a project matrix names its pack in the configuration — rather than being scored by
  a looser comparison written for projects.
- Good, because the refusals are recorded. A later maintainer asked for templating or targets can read
  why they were declined instead of relitigating from scratch.
- Bad, because this is a new public surface in a runtime whose whole argument is that it has few:
  four CLI verbs, two MCP tools, two flags on an existing command (`--pack-id` and `--config`), one
  more argument on an existing MCP tool, an embedded schema, and a file format to keep compatible. Every one of those is surface to secure, document, and hold stable, and
  the mitigation is only that the convention is optional end to end.
- Bad, because a convention that is not a specification will be read as one anyway. `kind` in every
  payload, the wording in every help text, and this record are the mitigation, and none of them stops
  a reader determined to treat a runtime file as portable.
- Bad, because the hints invite exactly the feature this record refuses. The first support request
  after a builder writes `"source": "Snowflake FINANCE.EXPENSES"` will be "why does it not just read
  it", and the answer — keyless and offline, permanently — has to be given every time.
- Bad, because `packs test` puts the experimental evaluator in a project's CI, where an experimental
  surface changing will break builds. That is what "experimental" means and the help says so, but a
  green CI step is a strong invitation to forget it.
- Revisit when RFC 0005 has enough evidence from real projects to describe a portable project format
  — at which point a normative format may supersede this one and this record with it; when RFC 0002's
  graph composition arrives and the "another pack, run first" pattern has a described mechanism; or
  when a second implementation wants to read the same file, which is the point at which a runtime
  convention has outgrown being one.

## More information

Implementation: `internal/project` (the loader, the closed schema in `jpack.schema.json`, the
inventory, the checks, and the matrix carrier), `internal/fssecure/root.go` (`Relative`, the lexical
check usable before the file exists; and `Root`, the directory handle every project read is resolved
against, with `Contains`, `Open`, and `Read` on it), `internal/cli/packs.go` and the `--pack-id`
flag in `internal/cli/app.go`, `internal/mcp/tools.go` (`list_packs`, `get_pack`, and the `pack_id`
argument), and `evaluation.MatrixCase` / `Engine.RunCase` in `internal/evaluation/corpus.go`, which
is the row machinery the bundled corpus and a project matrix now share. Builder guide:
[`docs/building-with-packs.md`](../building-with-packs.md). Posture this record depends on and does
not change: [0004](0004-decline-http-api.md) (no HTTP, keyless, offline),
[0006](0006-authoring-lifecycle-in-the-client.md) (no store, no path over the wire),
[0007](0007-experimental-evaluator.md) (the experimental surface), and
[0011](0011-first-evaluator-conformance-claim.md) (the reference-only rule every new text here
obeys). Specification context: RFC 0002 (graph composition, out of scope here), RFC 0004
(applicability is not authorization), and RFC 0005 (pack organization and distribution), which this
convention is meant to supply evidence for rather than to answer. The cross-vendor adversarial review
this decision requires attaches to the pull request, not to this file (`docs/adr/README.md`, "Review
of material decisions").

## Correction — what "a corpus row" costs (2026-08-09)

One sentence in the consequences above is wrong and is corrected here rather than edited there, on
the pattern [ADR-0008](0008-mcp-prompts-authoring-method.md) set for a record that has been accepted:
the text stays as it was decided, and the correction is dated beside it.

The sentence reads "a builder's row is a corpus row once it names its pack fixture — the single
member a corpus row adds". `pack` is not the single member. The bundled evaluation corpus's own
manifest schema (`internal/artifacts/jps/0.2.0-draft/evaluation/manifest.schema.json`) *requires*
`id`, `origin`, `pack`, `facts`, `supportedExtensions`, `focus`, and `specSection`, and closes the
case object against everything else. A project matrix requires `id`, `facts`, and one expectation;
`origin`, `focus`, and `specSection` are optional there and a minimal row declares none of them. So
lifting a project row into a corpus means supplying the corpus-owned members, and — since
[ADR-0025](0025-assert-the-handoff-target-in-the-matrix.md) — removing any `expectedHandoffTarget`,
which that closed schema refuses outright.

**What the sentence was reaching for is true and is the part worth keeping**: the two carriers share
the fields the comparator reads, so a project's rows are judged by the same code and the same RFC
8785 byte comparison the corpus's are, rather than by a looser comparison written for projects. That
is the determination. "One member apart" was a claim about document shape that this record did not
need and did not check, and it has been repeated downstream from here; the surfaces that repeated it
are corrected in the change that carries this note.
