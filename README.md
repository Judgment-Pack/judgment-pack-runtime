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

One shared evaluator applies the specification's §§7–8 resolution model per
[ADR-0007](docs/adr/0007-experimental-evaluator.md), and ten surfaces reach it: `jpack experimental
evaluate` and `evaluate-corpus`, the project walk `jpack packs test`, the graph verbs
`jpack experimental graph evaluate` and `graph test`, the version comparison
`jpack experimental compare`, and the `experimental_evaluate`,
`experimental_test_packs`, `experimental_test_cases` and `experimental_test_graphs` MCP tools. The `experimental` namespace is a **stability**
statement: such a surface may change or be removed without compatibility promise. It is not a statement
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
`jpack experimental evaluate-corpus` runs the bundled evaluation corpus and reports its rows —
the evidence §3.4.1 requires of a claim of this class, and explicitly not exhaustive evidence of one;
see [CONFORMANCE.md](CONFORMANCE.md).

That command carries one further opt-in, `--rfc0008-quantifiers`, which is a **draft-RFC
prototype** per [ADR-0009](docs/adr/0009-draft-rfc-quantifier-prototype.md): it admits the
collection quantifiers `exists`, `every`, and `uniform` proposed by the specification's RFC 0008
(Draft). Those operators belong to no published JPS version. A pack using one is **not valid** under
JPS `0.1.0-draft`, `spec validate` rejects it, the evaluator without the flag refuses it, and every
successful evaluation payload produced under the flag says so in band through a `draftPrototype`
member — a refusal is an operational error and carries none. The flag is CLI only; the MCP tool does
not expose it.

A second opt-in, `--rfc0016-outcome-values`, is a **draft-RFC prototype** on the same terms, per
[ADR-0039](docs/adr/0039-draft-rfc-outcome-values-prototype.md). It admits the value declarations
proposed by the specification's RFC 0016 (Draft): an outcome declares named values under
`org.judgmentpack.outcome-values`, each a constant or a copy of one fact, and the disposition of that
outcome carries them in a `value` member. That member is the draft's and no member of §8.3. A
declared value that does not resolve withholds the outcome, and the result is `unresolved` with
reason `unknown`. The name is reserved, so a pack carrying a declaration is **not valid** under any
published JPS version, `spec validate` rejects it, and the evaluator without the flag refuses it.
A value drawn from a fact is a copy of that fact and is not verified. The two flags are mutually
exclusive, and this one is CLI only too.

The command binary is `jpack`. The project, repository, and release archives keep the
`judgment-pack` name; the executable they carry is `jpack`.

## Implemented commands

```text
jpack version
jpack spec validate <pack-or->
jpack spec test-conformance [suite]
jpack spec schema <spec-version>
jpack spec examples [name] [--spec-version V]
jpack packs list        (jpack.json project convention; ADR-0012, not part of the spec)
jpack packs validate [--id X]
jpack packs test [--id X] [--require-matrix]   (EXPERIMENTAL SURFACE; claim: CONFORMANCE.md)
jpack packs suggest [--id X] [--base ROW] [--write F|-] [--max N] [--include-hugs]   (candidate row INPUTS, never rows; ADR-0024)
jpack packs lock        (declare the current documents as the project's reviewed set; ADR-0019)
jpack packs verify      (check the project against that reviewed set)
jpack packs lint [--producers M]   (every consulted pointer has a producer; ADR-0022)
jpack packs schema
jpack mcp
jpack experimental evaluate <pack-or->   (EXPERIMENTAL SURFACE; claim: CONFORMANCE.md)
jpack experimental evaluate --pack-id X   (EXPERIMENTAL SURFACE; resolves one decision id through jpack.json)
jpack experimental evaluate <pack-or-> --rfc0008-quantifiers   (DRAFT-RFC PROTOTYPE; not an input the class defines)
jpack experimental evaluate <pack-or-> --rfc0016-outcome-values   (DRAFT-RFC PROTOTYPE; a pack that declares values is not an input the class defines)
jpack experimental evaluate-corpus   (EXPERIMENTAL SURFACE; corpus results, the evidence §3.4.1 requires)
jpack experimental compare <old-pack> <new-pack> --inputs <file>   (EXPERIMENTAL SURFACE; the inputs two versions decide differently; ADR-0045)
jpack experimental graph list   (EXPERIMENTAL: the configured graphs, resolved; ADR-0029)
jpack experimental graph validate <graph-or->   (EXPERIMENTAL composition prototype; spec RFC 0002, Draft; ADR-0015)
jpack experimental graph evaluate <graph-or-> [--inputs <file-or->] [--rehearsal]   (EXPERIMENTAL SURFACE; claim: CONFORMANCE.md)
jpack experimental graph explain <graph-or->   (the evaluation plan; nothing is evaluated)
jpack experimental graph test <graph-or-> --rows <file-or->   (EXPERIMENTAL SURFACE; claim: CONFORMANCE.md)
jpack experimental graph schema
```

The pack argument and `--pack-id` are mutually exclusive: one pack, one source, and supplying both
or neither is an invocation error rather than a precedence rule. `--pack-id` honors `--config` and
`JPACK_CONFIG` like every other command that reads a configuration.

The namespace is `jpack spec`, not `jpack jps`. JPS remains the name of the
specification and the prefix of its provisional diagnostic codes.

The `mcp` command serves the same offline operations to a Model Context Protocol client over stdio,
so an agent can validate documents as a tool call; see
[docs/mcp-clients.md](docs/mcp-clients.md) for per-client setup and
[docs/agent-testing.md](docs/agent-testing.md) for the agent-driven testing protocol.

## Install a tagged release

Download the archive for your operating system and architecture from
[GitHub Releases](https://github.com/Judgment-Pack/judgment-pack-runtime/releases). Each archive
includes the `jpack` binary, README, the `CONFORMANCE.md` claim the source it was
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
install -m 0755 jpack "$HOME/.local/bin/jpack"
jpack version
```

On Windows, expand the `.zip`, move `jpack.exe` into a directory on your user `PATH`, and
run:

```powershell
jpack version
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

## Evaluate one decision

With `jpack` on your `PATH`, this is the whole loop: a pack, a facts document, and a disposition.
Save a pack that approves an expense unless its amount is over 5000:

```bash
cat > pack.json <<'EOF'
{
  "specVersion": "0.2.0-draft",
  "id": "https://example.invalid/judgment-packs/expense-approval",
  "version": "0.1.0",
  "title": "Expense approval",
  "decision": {
    "intent": "Approve ordinary expenses without review.",
    "question": "May this expense be approved?"
  },
  "outcomes": [
    { "id": "approve", "label": "Approve" },
    { "id": "decline", "label": "Decline" }
  ],
  "rules": [
    {
      "id": "over-limit",
      "description": "An amount over 5000 is declined.",
      "when": { "op": "fact", "path": "/expense/amount", "operator": "greater-than", "value": "5000" },
      "outcome": "decline",
      "onUnknown": "escalate"
    }
  ],
  "fallbackOutcome": "approve",
  "escalation": {
    "triggers": ["unknown"],
    "target": { "kind": "human-role", "name": "Finance reviewer" }
  }
}
EOF
jpack spec validate pack.json
```

Give it the facts of one expense and evaluate. The amount is a decimal string, as the
specification requires for a number a rule compares:

```console
$ echo '{"expense": {"amount": "120"}}' > facts.json
$ jpack experimental evaluate pack.json --facts facts.json
EXPERIMENTAL SURFACE evaluation (claim and scope: CONFORMANCE.md; this result authorizes nothing)
disposition: outcome approve
trace: rule over-limit: false
artifacts: immutable-git-ref · sha256 081cf18af9fe667a5da5acab465f4cf6118a00742bb6e5527d4d45fc09f25185
```

Now remove the amount. The rule cannot be decided, its `onUnknown` is `escalate`, and the pack
names who takes the case, so the answer is a handoff rather than a guess. The trace line names the
fact that was missing:

```console
$ echo '{"expense": {}}' > facts.json
$ jpack experimental evaluate pack.json --facts facts.json
EXPERIMENTAL SURFACE evaluation (claim and scope: CONFORMANCE.md; this result authorizes nothing)
disposition: unresolved (unknown)
handoff: requested -> human-role "Finance reviewer" (triggered by unknown)
trace: rule over-limit: unknown onUnknown=escalate [fact /expense/amount absent]
artifacts: immutable-git-ref · sha256 081cf18af9fe667a5da5acab465f4cf6118a00742bb6e5527d4d45fc09f25185
```

Releases up to 0.23.1 print the last trace line without the bracket naming the missing fact.
Both runs exit 0: producing a disposition is success, whichever it is. `--format json` gives the
same result as a payload, with the disposition in its canonical form. A disposition is data. It
authorizes nothing, and acting on it is the caller's decision.

From here, [docs/building-with-packs.md](docs/building-with-packs.md) covers a project of several
packs, their test matrices, and the audit trail; `jpack spec examples --spec-version 0.2.0-draft`
lists larger starting points the evaluator accepts, from 0.24.0.

## Build and try it locally

Go 1.24 or newer is required to build from source: `internal/fssecure` binds every project file
read to a directory handle through `os.Root`, which is a Go 1.24 standard-library type.

If an older WSL setup has persisted `GO111MODULE=off`, clear it once with
`go env -u GO111MODULE`. The commands below explicitly enable module mode as a compatibility
measure.

```bash
env GO111MODULE=on CGO_ENABLED=0 go build -trimpath -o ./bin/jpack ./cmd/jpack
./bin/jpack --help
./bin/jpack version
```

From a sibling checkout of `judgment-pack-spec`, validate a synthetic example:

```bash
./bin/jpack spec validate ../judgment-pack-spec/examples/minimal-expense-approval.json
./bin/jpack spec validate --format json ../judgment-pack-spec/examples/minimal-expense-approval.json
```

Standard input is accepted explicitly with `-`:

```bash
./bin/jpack spec validate --format json - < pack.json
```

Run a bundled document-conformance corpus — `0.1.0-draft` by default, or an exact bundled version:

```bash
./bin/jpack spec test-conformance
./bin/jpack spec test-conformance --format json
./bin/jpack spec test-conformance --spec-version 0.2.0-draft
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
./bin/jpack experimental evaluate-corpus
./bin/jpack experimental evaluate-corpus --format json
./bin/jpack experimental evaluate-corpus --spec-version 0.2.0-draft
```

Inspect or copy the bundled schema without network access:

```bash
./bin/jpack spec schema 0.1.0-draft
./bin/jpack spec schema 0.2.0-draft
./bin/jpack spec schema 0.1.0-draft --write schema.json
./bin/jpack spec schema 0.1.0-draft --write -
```

The schema command refuses to overwrite an existing file. It is a different write from the audit
trail below, which appends to a file the project's own configuration named and never replaces one.

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
  10 MiB, the per-document byte cap every admitted input passes, and `limits.go` computes it from that
  constant so the two cannot drift. That ratio is an **arithmetic fact about two numbers**, and nothing
  more: **it gives no guarantee about any whole evaluation.** Three documents may be admitted rather
  than two — the pack, the facts document, and an optional evidence-availability document — each under
  the same cap; and a unit is charged per *use* rather than once per admitted byte, since the bytes of a
  pointer and of a selected value are charged again every time a condition reads them, with §8's fixed
  per-node and per-iteration charges on top. An earlier version of this section inferred from the ratio
  that "one full read of every admitted byte always fits" and that a single maximal cross-document
  comparison therefore sits exactly at the boundary; **both are withdrawn** — the premise bounds nothing
  under that charge model, and neither was exercised against an admitted input through the accounting
  path (ADR-0011 records the correction). Amplification is what the limit refuses in practice — the same
  large selected value re-read once per `in` candidate or once per condition — and this runtime does not
  claim that only amplification is refused. Against a 100 KB facts document the limit still admits about
  two hundred whole-document comparisons, and every row of the bundled evaluation corpus charges under
  1,000 units: measurements of those inputs, not bounds on inputs no row contains.
  Callers may configure a lower limit per evaluation; the draft RFC 0008 prototype has its own,
  smaller budget of 100,000 units (ADR-0009), and the draft RFC 0016 prototype is charged to this
  limit (ADR-0039).
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

## The `jpack.json` project convention

A project that owns several packs needs a name for each one that works the same from a shell, from
CI, and from an agent's tool call. `jpack.json` is that index — and it is a **convention of this
runtime, not part of the Judgment Pack Specification** ([ADR-0012](docs/adr/0012-jpack-project-convention.md)).
No other implementation is obliged to understand it, and a project that never writes one loses
nothing: every command still takes a pack by path, and every MCP tool still takes one as text.

```json
{
  "configVersion": "1",
  "packs": {
    "expense-approval": {
      "path": "packs/expense-approval-1.2.0.pack.json",
      "matrix": "packs/expense-approval.matrix.json",
      "description": "May this expense be reimbursed without a manager's sign-off?",
      "expectedVersion": "1.2.0",
      "facts": { "/expense/amountUsd": { "source": "Snowflake FINANCE.EXPENSES", "hint": "amount_usd as a decimal string" } },
      "evidence": { "itemised-receipt": { "source": "SharePoint /Finance/Receipts" } }
    }
  }
}
```

The file is selected by `--config`, then `JPACK_CONFIG`, then `./jpack.json`. Its schema is closed
and printable with `jpack packs schema`: every member it does not name is rejected.
`configVersion` is a single integer as a string, on the `outputVersion` precedent rather than
semantic versioning; `"1"` is the shape without graphs, `"2"` the shape with them (ADR-0017), `"3"`
the shape that may also ask for an audit trail (ADR-0018), `"4"` the shape that may also require the
reviewed set (ADR-0044), `"5"` the shape that may also require comparable facts (ADR-0046), `"6"`
the shape whose audit member may also say whether its trail is chained (ADR-0047), and this runtime
reads all six.

There is **no templating, no target or environment blocks, and no selection**. A templated pack was
never the pack anyone reviewed; environments are one file per environment by convention
(`jpack.staging.json`, `--config`); and choosing which decision to ask is the application's, because
applicability is not authorization. Approval is your pull request — there is no approval state in
the file.

**A pack's identity is stated once, in the pack document's `id` and `version` members.** Everything
else that names a version is a validated reference to that statement, never a second one:
`expectedVersion` is a pin `packs validate` compares; the optional `<decision-id>-<semver>.pack.json`
filename is cross-checked when followed and skipped when not; and the `packId` and `packVersion`
members on every evaluation payload are echoes read off the document that was evaluated. Any of the
three may disagree with the document, and a disagreement is an error — none of them can win one.

The `facts` and `evidence` hints are non-normative guidance for an agent gathering inputs: they say,
in your words, where a value is held. **The runtime never reads a source** — it holds no credential
and opens no network connection — and every file the convention names is read through a reader bound
to a handle held open on the configuration's own directory. Containment is two checks and neither
substitutes for the other: a lexical one refuses an absolute or escaping path before anything is
read, and resolving against the held directory at read time refuses a path that reaches outside
through a symlinked component, which a lexical check cannot see. The second is a handle rather than a
pathname so that containment holds *through* the open: a path that is checked and then opened by
pathname can have an intermediate directory swapped for an outward symlink in between, and resolving
against the handle makes that impossible rather than unlikely. A final component that is a symlink is
refused whatever it points at, and only a regular file is read. Every surface that reaches a pack
through the configuration takes this one reader, `--pack-id` included, and none of them is handed a
pathname to open for itself.

The one thing this file can ask the runtime to **write** is a record of what it evaluated
([ADR-0018](docs/adr/0018-opt-in-evaluation-audit-trail.md)). Under `configVersion "3"`, an
`audit` member names a directory relative to the configuration — `"audit": { "dir": "audit" }` —
and each completed evaluation of `experimental evaluate` and the MCP `experimental_evaluate`
tool — unless it was declared a rehearsal (ADR-0028) — then appends one JSON line to
`evaluations.jsonl` in it, and each completed `experimental graph evaluate` appends one per node
and one for the composite, unless declared a rehearsal (ADR-0041). Each line holds the
pack's id, version, `specVersion` and the digest of its exact bytes, the facts and evidence
documents as evaluated, and the disposition in its canonical form. Test runs never record —
`packs test`, `experimental graph test`, and `experimental evaluate-corpus` are checks on packs,
not decisions anyone took — a refused evaluation records nothing, because it has no disposition to
record, and a record that cannot be written refuses the run (exit 4) rather than reporting a
disposition nothing kept. The write goes through the same held handle every read does, into the
project's own tree and nowhere else. Declare no `audit` member and nothing is written at all.
Where the evaluation's trace noted a comparison across JSON types or a cause of an unknown, the
record also carries `typeMismatches` and `unknownCauses`, gathered from the trace: pointers, types
and causes, never values (ADR-0046). A graph run's node records carry their own node's notes, and
the composite carries none. A record with nothing to note is byte for byte what it was.

**The trail is chained** ([ADR-0047](docs/adr/0047-make-a-decision-record-defensible.md) §1). Each
record also carries `trail` (the trail's identity, 128 random bits in hex), `sequence` (its line
number, from 1) and `previous` (the SHA-256 of the exact bytes of the line before it, without its
newline). A trail written before chaining is never rewritten: the first chained record commits to
the whole file before it at once, so its `previous` is the SHA-256 of the file as it stood. A record
is otherwise byte for byte what it was, and `recordVersion` stays `"1"`. The writer holds an
exclusive, cooperative lock on the trail while it reads the last line, numbers the lines it writes
(a graph run's nodes, then its composite), writes, and syncs the file and its directory. It refuses
to append (`JPS-AUDIT-WRITE`, exit 4, with a message saying why) after a last line with no newline,
a write that did not complete; when a line it must read is longer than 128 MiB, or the record would
be; and when the lock cannot be taken for a reason that may pass. Only where the platform or file
system offers no lock at all does it write records unchained. A project that does not want the chain
sets `"audit": { "dir": "audit", "chain": false }` under configVersion `"6"`. Recomputing each
`previous` shows whether the lines are consistent with one another: a line edited, inserted, deleted
or moved anywhere before the last breaks a link. That is not authenticated history. The last line
can be edited without breaking any link, and a trail cut short, or rewritten from any line on with
its links recomputed, is as consistent as the real one; only a commitment held by someone other than
the operator, covering those lines, tells them apart. Keep the trail, and anything copied from it,
byte for byte: a decoded and re-encoded line is other bytes with another digest.

**Checking a trail.** `jpack audit verify` reads the project's trail, or `--trail <file>`, over its
exact bytes and checks every `trail`, `sequence` and `previous` from the first chained record on;
it exits 1 on any failed check, each named in the report, and 0 otherwise. Its size is read under
the writer's lock, shared, so it falls between two writes, and the bytes before it are read without
the lock: writers only append, so a verification neither delays nor races them. Without `--expect`
the report is the integrity of one supplied chain, and says what it does not establish: that the
last line, or lines rewritten from some point on with their links recomputed, are the ones first
written, or that the trail is complete. `jpack audit checkpoint` prints the checkpoint of the last
chained record, one canonical JSON line naming the trail's identity, the record's sequence and the
SHA-256 of its exact bytes, for handing to someone who will keep it. `jpack audit verify --expect
<checkpoint>` then also fails a trail that is shorter, has another identity, or has another record
at that sequence, and reports the lines up to it as checkpointed. The coverage it reports names
signatures as not available (#209); time-stamped checkpoints are #208. After a write that did not
complete, `jpack audit repair` keeps the damaged bytes in place as a line of their own and appends
a discontinuity record that names their digest and links over them, so the writer goes on (it never
repairs a torn discontinuity, since a repair does not repair a repair);
`verify` then reports the trail as segments, never as intact across the break. A discontinuity line
records no decision: it has `kind` `"discontinuity"` and no pack, inputs or disposition.

**Refusing a fact no comparison can match.** There is no coercion between JSON types: a flag sent
as `"true"`, `1` or `null` makes `equals true` false, not unknown, so `onUnknown: escalate` never
sees it and a detector falls through to its fallback. A project whose `jpack.json` sets
`"requireComparableFacts": true` (configVersion `"5"`, ADR-0046) has `experimental evaluate`, the
MCP `experimental_evaluate` tool, and `experimental graph evaluate` for each node refuse such an
input before evaluating (`JPS-FACTS-COMPARABLE-REQUIRED`, exit 1, no record). A run is refused when
a fact that some comparison anywhere in the pack reads is present and of a type that comparison can
never match: a type no `equals`, `not-equals` or `in` operand has, or, for an ordered comparison,
anything but a decimal string, so a string outside the decimal grammar is refused as well as a JSON
number. The check covers every comparison, including ones evaluation would not reach. Its work is
bounded by the evaluation's work limit, and a check that would pass that limit refuses under the same
code, whatever the pack compares, because it cannot then say the facts are matchable. The refusal is
the project's and not an evaluation error, so it carries no §8.4 class. An absent fact is not
refused, because it is unknown and `onUnknown` governs it. Declared rehearsals are refused too. `packs
test`, `experimental graph test`, `experimental evaluate-corpus`, `experimental compare` and the MCP
test tools are not, because their rows may probe such facts on purpose. The refusal names each
pointer, the fact's type and what the comparison can match, never a value.

### The reviewed set

`jpack packs lock` writes `jpack.lock.json` beside the configuration: the digest of the
configuration's exact bytes and of every pack and graph it declares
([ADR-0019](docs/adr/0019-reviewed-set-lock.md)). Running it **is** the amendment — it is how a
project says, in a file a reviewer diffs, that the law changed on purpose — and it approves nothing:
your pull request is still the approval. The file is generated and deterministic, so re-running it
over an unchanged tree leaves no diff. `jpack packs verify` names every difference from it:
`config-drift`, `document-drift`, `document-missing`, `lock-entry-missing`,
`locked-but-undeclared`, and `path-mismatch`.

Its **presence** is the opt-in, and it is found by convention rather than declared: no
`configVersion` moves, the schema does not change, and a project with no lock file behaves exactly
as it did. With one, the deciding surfaces — `experimental evaluate`, `experimental graph evaluate`,
and the MCP `experimental_evaluate` tool — hold the law they are about to apply to it, declared
rehearsals excepted (ADR-0028, ADR-0041), and refuse a
mismatch (`JPS-LOCK-VERIFY`, exit 1) with the two honest ways forward: declare the amendment, or
restore the reviewed bytes. `packs test`, `experimental graph test`, and `experimental
evaluate-corpus` consult it never: the author's loop is free and decisions are classified. A pack
named by path, or passed as text over MCP, is a draft — evaluated, never refused for being unlocked,
and recorded as a draft. Where an audit trail is configured, each record carries `reviewed`: `true`
when every document applied was declared and matched, `false` for a draft, absent when the project
declares no lock. The evaluation payload carries the same `reviewed`, with `reviewedSet` naming the
lock's revision when it is `true`, so a caller learns it without reading the trail (ADR-0044). A
rehearsal consulted no lock and carries neither.

**Requiring it.** A project whose `jpack.json` sets `"requireReviewed": true` (configVersion `"4"`,
ADR-0044) has the deciding surfaces refuse, before evaluating, every run that applies a draft and
every run while the project has no lock (`JPS-LOCK-REVIEW-REQUIRED`, exit 1). A declared rehearsal
is not a decision and is not refused. This binds a caller that neither chooses the configuration a
run reads nor can edit it or its lock, such as an agent limited to the tools of an MCP server
someone else launched. It cannot bind whoever chooses the configuration (`--config`, `JPACK_CONFIG`, the working
directory) or can edit `jpack.json` or the lock, and turning it off is itself an edit the lock
records, so a by-id run is refused until the project locks again.

**What it is not.** It is not a wall. Anything that can edit a pack can run `packs lock` again, and
this runtime cannot tell that from an author amending policy on purpose — they are the same act.
What the lock buys is that the amendment stops being silent.

Seven commands, one CI line, and one amendment step:

```bash
jpack packs list                                            # the resolved inventory
jpack packs validate && jpack packs lint && jpack packs test && jpack packs verify   # the CI gate
```

`packs lock` is deliberately not in that line. It is the amendment — you run it when the law changes
on purpose, and you commit its output with the change it pins. Running it immediately before
`packs verify` would make the verification vacuous, which is the one way to hold this convention
backwards.

`packs validate` reports six named checks per pack — path containment, document validation, the
`expectedVersion` pin, the filename cross-check, the hint keys, and matrix well-formedness — each as
`passed`, `failed`, or `skipped`. `packs test` runs each pack's instance matrix through the experimental
evaluator and compares every row the way the bundled evaluation corpus is compared: the RFC 8785
canonical §8.3 disposition byte for byte, or the expected §8.4 error class and phase, plus the one
optional per-row assertion about the escalation target §8.3 keeps *outside* the disposition
(`expectedHandoffTarget`, which needs `matrixVersion: "2"`). A row may also carry `cites` (which
needs `matrixVersion: "3"`): the gateway receipts its facts were transcribed under, validated when
the matrix loads and carried on the row's result as values — metadata the comparator never reads
(ADR-0034). The two carriers share the fields the
comparator reads, which is what makes the comparison one implementation rather than two — not a
claim that a row moves between them untouched. Corpus admission additionally requires its own
members (`pack`, `origin`, `supportedExtensions`, `focus`, `specSection`), and its closed schema
forbids `expectedHandoffTarget` and `cites`, so lifting a project row means supplying those and
removing any target assertion and any citations. Both commands exit `1` on any failure, a pack with no
matrix is reported *skipped* rather than passed by default (with `packs test --require-matrix` it is
reported *mismatch* instead; ADR-0042), and a `packs test` run in which no row ran at all is reported
*skipped* and exits `1` unless a mismatch was found: a green gate over zero rows would say a project
was tested when nothing was. A new project may start with an empty `packs` object: inventory and structural
validation accept it, while `packs test` remains skipped with exit `1`. `packs lint` closes the gap neither of them covers: a pack consulting a pointer no
source feeds raises no error anywhere — the condition is unknowable, so a rule touching it
escalates, or under `onUnknown: ignore` is skipped and the fallback answers without it — so the lint holds every consulted
pointer to a producer declaration (the configuration's own hints, or an explicit `--producers`
manifest) and fails the build where that defect otherwise hides (ADR-0022).

From a shell, `jpack experimental evaluate --pack-id expense-approval --facts facts.json`
reaches the same pack by the same name. Over MCP the same inventory is `list_packs`, one document is
`get_pack`, and `experimental_evaluate` accepts `pack_id` instead of pasted `pack` text; the graph
convention has the same pair, `experimental_list_graphs` and `experimental_get_graph`, beside the
CLI's `experimental graph list` (ADR-0029). With no configuration,
`list_packs` answers empty with an explanation of where the runtime looked, rather than failing.

[docs/building-with-packs.md](docs/building-with-packs.md) is the builder's guide: the packs-as-code
lifecycle, the three-owner model (the application selects, the agent gathers and never invents, the
pack judges), hints in practice, and the data-sufficiency-as-another-pack pattern.

## Process contract

| Exit | Meaning |
| ---: | --- |
| `0` | Command succeeded; validation passed the reported scope. |
| `1` | Document invalid, an expectation mismatched, or a check the command makes failed — a conformance mismatch, a difference from the reviewed set, or law that left it. |
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
- writes only where it was told to, in three ways and no others: a copy of a bundled schema or
  example at the target an operator names with `--write`, which refuses to overwrite an existing
  file; one appended record per completed non-rehearsal evaluation (ADR-0028), and for a non-rehearsal graph evaluation one per node and one for the composite (ADR-0041), when a project's `jpack.json` declares an
  `audit` directory ([ADR-0018](docs/adr/0018-opt-in-evaluation-audit-trail.md)), into that
  directory, through the handle held open on the configuration's own directory, under an advisory
  lock on the trail file itself when the trail is chained (ADR-0047) — a record is not a
  diagnostic, and it carries the documents the project asked to have recorded; and the reviewed-set
  lock `jpack packs lock` generates beside the configuration when an operator runs that command
  ([ADR-0019](docs/adr/0019-reviewed-set-lock.md)), replaced in place through the same handle and
  refused outright if it would land on a document the configuration declares;
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
never depend on them. The current supported integration boundary is the `jpack` executable
and its versioned JSON output. Go packages are intentionally `internal`, and there is not yet a
stable in-process SDK or plugin API. Before commercial commands are composed into one binary, the
public project must define and version that contract deliberately. Future commands should use
distinct namespaces such as `jpack cloud` or `jpack org`; they must not override
`jpack spec` conformance semantics or auto-load during validation.

The normative specification, schemas, and public corpus remain in the separate
[`judgment-pack-spec`](https://github.com/Judgment-Pack/judgment-pack-spec) repository.

## Development

```bash
env GO111MODULE=on go fmt ./...
env GO111MODULE=on go vet ./...
env GO111MODULE=on go test ./...
env GO111MODULE=on CGO_ENABLED=0 go build -trimpath ./cmd/jpack
```

See [CONTRIBUTING.md](CONTRIBUTING.md), [docs/architecture.md](docs/architecture.md), and the
[maintainer release runbook](docs/releasing.md).

In VS Code, **Terminal → Run Task → judgment-pack: Build CLI** builds `bin/jpack`; the test
and bundled conformance tasks are available from the same menu. The tasks explicitly enable Go
module mode for older WSL configurations.

## License

Apache License 2.0. See [LICENSE](LICENSE).
