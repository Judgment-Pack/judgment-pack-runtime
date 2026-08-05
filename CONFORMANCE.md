# Evaluator-conformance claim

This file is the whole of this runtime's conformance claim. No other file, command, payload, or
sentence in this repository makes one, and no other form of claim is definable: Judgment Pack
Specification Core §3.4.1 permits exactly one, and forbids every other.

Every other surface is **reference-only**: the CLI help, the MCP tool descriptions and rendered
prompts, the `conformanceClaimReference` member of every evaluation payload, the draft-RFC prototype
note, the README, the architecture notes, the CHANGELOG entry for this change, and the decision record
itself. Each says where the claim is — this file — and states no part of it. That is deliberate rather
than terse: §3.4.1 fixes the entire form a claim may take, so a surface that named the class and the
version while omitting the corpus version, the results, and the every-row statement would be making the
partial claim §3.4.1 forbids. The rule is enforced rather than intended: a test walks those surfaces,
the maintained prose, and the rendered prompts, and fails on a denial, on a claiming sentence, or on any
statement that assembles the class, the exact version, and the corpus version or its results together
(`internal/cli/app_test.go`). Two dated records are outside that walk and say so on their face:
[ADR-0007](docs/adr/0007-experimental-evaluator.md) and
[ADR-0010](docs/adr/0010-evaluator-aligned-to-core-0.2.0-draft.md) recorded, accurately for the posture
each recorded, that nothing was claimed; released CHANGELOG sections are dated the same way. Neither is
a statement about this claim, and neither is edited to match it.

Decision record: [ADR-0011](docs/adr/0011-first-evaluator-conformance-claim.md).

## The claim

`jpack` claims **evaluator conformance** as JPS Core `0.2.0-draft` §3.4 defines that class,
against Core **`0.2.0-draft`** — the exact `specVersion` — and against the evaluation corpus published
for that version, whose `suiteVersion` is **`0.2.0-draft`**.

This runtime has run that corpus for that exact `specVersion`. The result obtained: **all twenty rows
passed** — every row's disposition matched the row's expectation compared as §8.3 defines the
comparison, byte for byte after RFC 8785 canonicalization of both sides. **Every row of `suiteVersion`
`0.2.0-draft` passed.** No row was excluded, and no project-issued erratum is cited because none
applies: the corpus's own `errata.md` records no erratum for this `suiteVersion`, so "every row" means
every row.

The claim is **effective as of the commit that merges this file to `main`**, and **builds from that
activating commit and its descendants carry it until it is withdrawn or replaced — here, in this
file.** Persistence is not unconditional inheritance: a descendant commit that edits this file
withdraws or replaces the claim for every build from that commit onward, so what any build carries is
what this file says at the commit it was built from. There is one activation point and no second one:
§3.4.1 attaches a claim to an implementation and an exact `specVersion`, and requires no tag, no
release, and no publication step, so the source that satisfies the class is the thing that claims — a
development build from this history makes the same claim its released artifacts do, which is why every
such build emits the same reference to this file. Anyone can reproduce its evidence offline:

```bash
jpack experimental evaluate-corpus --spec-version 0.2.0-draft --format json
```

The claim applies to **conforming Core-class inputs reaching this runtime's one shared evaluator**
through the seven surfaces that reach it — `experimental evaluate`, `experimental evaluate-corpus`,
`packs test`, `experimental graph evaluate`, `experimental graph test`, and the
`experimental_evaluate` and `experimental_test_packs` MCP tools — and **not to draft-RFC prototype
inputs**, which this class does
not define at all and which the last exclusion below states. One evaluator sits behind all seven, so a
surface selects what reaches it rather than carrying a claim of its own: the project and graph
surfaces choose which packs and inputs reach the evaluator, and the dispositions they report as
evaluated — a row's actual disposition, a node's, a composite's echoed headline — are that
evaluator's; a row's expected disposition is the project's own authored expectation, which no
evaluator produced. Every input any of them admits
declares `specVersion` `0.2.0-draft`, and nothing else is evaluated at all — the evaluator's
admitted version scope and this claim's version scope are the same set, for the reason §11 gives
below. The `experimental` namespace
names the **stability** of that surface — it may change or be removed without a compatibility promise —
and has never meant, and no longer says, that no claim is made. The limits the claim is made under are
the documented ones below; §10 leaves an implementation to define its own limits, so a caller that
configures a lower one has selected a documented limit of its own, and an input above either
implementation's limit is outside the portable claim in the first place.

## The limits this class requires (§10)

§10 requires an implementation claiming this class to define and document at least its collection-size
and evaluation-work limits, and makes reaching one of them during an evaluation the
`resource-exhaustion` error of §8.4 rather than a disposition. Both are defined and enforced:

- **Evaluation-work limit: 20,971,520 work units per evaluation** (`DefaultCoreWorkLimit`,
  configurable per evaluation). A unit is one visited condition node, one §8 iteration over an
  authored evidence requirement, exception, or rule, one step of a pointer resolution, or one byte of
  a path, a member name, or a scalar token a comparison reads. Reaching it is `resource-exhaustion` in
  the `evaluation` phase, with no disposition and no partial state. The draft-RFC opt-in has its own,
  smaller budget of 100,000 units.

  The number is derived in code from the carrier's byte cap rather than chosen: it is exactly twice
  10 MiB, the per-document byte cap every admitted input passes, and `limits.go` computes it from that
  constant so the two cannot drift. That ratio is an **arithmetic fact about two numbers**, and nothing
  more: **it gives no guarantee about any whole evaluation**, and none is stated here. Two reasons, both
  structural: three documents may be admitted rather than two — the pack, the facts document, and an
  optional evidence-availability document — each under the same cap; and a work unit is charged per
  *use* rather than once per admitted byte, since the bytes of a pointer and of a selected value are
  charged again every time a condition reads them, with §8's fixed per-node and per-iteration charges
  on top. An earlier statement of this claim inferred from the ratio that "one full read of every
  admitted byte always fits", and that a single maximal cross-document comparison therefore sits
  exactly at the boundary; **both are withdrawn** — the premise bounds nothing under that charge model,
  and neither statement was exercised against an admitted input through the accounting path
  ([ADR-0011](docs/adr/0011-first-evaluator-conformance-claim.md) records the correction). What the
  limit refuses in practice is amplification — the same large selected value re-read once per candidate
  or once per condition — and this runtime does not claim that only amplification is refused. Reaching
  it is `resource-exhaustion` and §10 permits that: the limit is documented, it is not portable, and an
  input above it is outside the portable claim.
- **Collection-size limit: 250,000 members** — the carrier's parsed-node cap, which every input is
  admitted under, so no admitted document contains a larger collection and every collection this
  evaluator traverses comes from an admitted document. Because the bound is enforced while *admitting*
  an input, §10's own phase split makes reaching it `malformed-input` in the `preflight` phase: §2.1
  refuses such a document whole rather than processing part of it. That is stricter than an
  evaluation-phase check of the same bound, not weaker, and it is why this runtime states the
  determination instead of adding a second mechanism that could only report what the preflight has
  already refused.

Neither limit is portable, and §10 says so: two conforming implementations may define different
limits, and an input above either one is outside the portable claim. Both are stated in full in
[README.md](README.md#the-two-10-limits-of-the-claimed-class) and in `internal/evaluation/limits.go`.

## What this claim asserts

That this implementation satisfies **every** requirement of §§7–10 — the semantics of §§7–8, the §8.3
portable disposition, the §8.4 error classes and their fixed precedence, and the documented limits
above — for every input it admits, not merely for the inputs it happened to run (§3.4, §3.5).

## What this claim does not assert

- **That the corpus run exhausts it.** Corpus results are required evidence for this claim and are
  **not exhaustive evidence** of it (§3.4.1). The corpus is a
  *seed* corpus: version-pinned, not exhaustive, and grown by RFC. Passing every row demonstrates
  nothing directly about an input no row contains. The corpus publishes its own gap list, and it is
  long: no error-class rows at all, three mandatory operators with no row, no `literal` and no `not`
  row, no composite-equality row, no fallback-selection row, and handoff coverage that rests on a
  single row.
- **Nothing about any pack, any facts, any evidence, or any consequence of acting on a disposition**
  (§3.5). This claim is not proof that a claim in a pack is true, that evidence is authentic or
  sufficient, that an author or reviewer had authority, that an outcome is legally or ethically
  permissible, or that use of a pack is safe. It asserts nothing about the accuracy, quality, or
  fitness of any policy a pack encodes.
- **No authorization.** Conformance is not authority (§10). This runtime authorizes nothing, executes
  nothing, and fetches nothing.
- **Nothing about a deployment or a particular run** in production, and nothing about the facts and
  evidence a caller supplied for it (§3.5).
- **Nothing under any other `specVersion`.** A claim attaches to one exact version and is not
  inherited, forward or backward (§3.4.1, §11). The evaluator's admitted inputs match that scope
  exactly: **only a pack declaring `specVersion` `0.2.0-draft` is evaluated at all.** §11 makes the
  declared value exact and says an unedited `0.1.0-draft` pack "is not structurally conforming to
  `0.2.0-draft` and must be re-declared before an implementation claiming this draft evaluates it", so
  a pack declaring any other version — `0.1.0-draft` included — is refused as `pack-not-conformant` in
  the `preflight` phase, with a message citing that rule. There is no second, unclaimed legacy path:
  one evaluator, one admitted version, and no input inside the claim that the claim's version scope
  does not cover.

  Re-declaring is **one edit**: the `specVersion` string, and nothing else in the document. §11 says
  `0.2.0-draft` changes no part of the document format, so every member, every cross-field rule, and
  every document-conformance verdict of §§3.1–3.3 is unchanged. This runtime still bundles
  `0.1.0-draft` and `spec validate` still validates such a pack against its own published schema;
  document conformance is untouched by this class (§3.4), and it is only evaluation that requires the
  re-declaration.

  When a later `specVersion` publishes this class, this claim does not extend to it: the corpus
  published for that exact version must be run and this file restated before anything is claimed under
  it.
- **Nothing about the draft-RFC prototype.** With `--rfc0008-quantifiers` this runtime admits
  condition operators no published JPS version defines, so a pack using one is not a semantically
  conforming document and is not an input this class describes. That is not an exception to a
  requirement of the class; it is a surface whose inputs the class does not define, and no claim is
  made on it.

## Corroboration, which is not the claim's basis

§3.4.1 forbids claiming this class on the strength of agreement with another implementation in place
of corpus results. The basis of this claim is the corpus run above. Separately, and as corroboration
only: a clean-room Python evaluator, derived from the `0.2.0-draft` text alone, reproduces all twenty
rows, and a committed driver shows 20/20 byte-agreement between the two implementations
(`harness/CLASS-AGREEMENT.md` in the evaluator-experiments repository). Both implementations trace to
one maintainer's direction, so that agreement corroborates the specification's precision rather than
independently confirming it.

## If a row fails

A failing row blocks this claim, and this runtime does not get to decide that the row is wrong: §3.4
makes a divergence as likely to be a defect in the row as in the implementation, and only a
project-issued erratum can mark a row defective. Report the divergence to the specification project
and to this repository. Until an erratum exists, the correct outcome is a fixed implementation or a
withdrawn claim — edited here, in this file — and never a self-issued exclusion.
