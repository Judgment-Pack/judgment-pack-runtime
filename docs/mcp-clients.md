# Connecting MCP clients

`jpack mcp` serves the offline validator to any Model Context Protocol client over stdio:
no port, no credential, no network — and no write, unless the project it is launched in asks for one
(see [The project tools](#the-project-tools) below). The client supplies its own model and
API key; the runtime never sees either (see
[ADR-0003](adr/0003-mcp-integration-and-testing-surface.md) and
[ADR-0006](adr/0006-authoring-lifecycle-in-the-client.md)).

Every client below launches the same binary. Install a released `jpack` (see the
[README](../README.md)) and make sure the command the client launches resolves — if `jpack`
is not on the PATH the client's launcher inherits, use the absolute path instead; a wrong path is
the most common failure and most clients report it poorly.

Two installation-free alternatives ([ADR-0013](adr/0013-oci-image-and-mcp-registry-distribution.md)):
registry-aware clients can install the server by name — it is published to the official MCP
registry as `io.github.Judgment-Pack/judgment-pack` — and any client that can run a container can
use the released image directly, with a project mounted at `/project` (the launch directory the
`jpack.json` convention reads):

```json
{
  "mcpServers": {
    "jpack": {
      "type": "stdio",
      "command": "docker",
      "args": ["run", "-i", "--rm", "-v", "/abs/path/to/project:/project",
               "ghcr.io/judgment-pack/judgment-pack:latest"]
    }
  }
}
```

The image is the released static binary on `scratch` — no shell, no package manager, no network
use — and its digest is attested; the binary inside is byte-identical to the release archive's.

Client configuration is personal wiring, not project source, so the local config files below are
gitignored in this repository. Copy a snippet, don't commit one.

## The tools

| Tool | Purpose |
| --- | --- |
| `get_artifact_version_options` | Exact x.y.z patch/minor/major choices and optional candidate comparison; no compatibility or save authorization |
| `get_dialect_contract` / `dialect_init` | Pinned encoding capabilities and a starter template following the reference Runner spelling; no project or save |
| `dialect_canonicalize` / `dialect_digest` | Canonical bytes and digest, with version-3 identity spelling checks; not full document validation |
| `dialect_validate` | Version-2 document forms, limits and references; no profiles or preparations |
| `get_dialect_schema` / `get_dialect_reference` | Version-2 editing schema and pinned dialect reference text |
| `dialect_check` | Declared version-2 mapping coverage against pack inventory; no execution or value-type checking |
| `validate` | Carrier, structural, and semantic conformance for one document |
| `test_conformance` | Run a version-pinned conformance corpus |
| `get_schema` | The exact bundled JSON Schema, with digest |
| `describe_runtime` | Document versions, version-scoped evaluator capabilities and artifact provenance |
| `list_examples` / `get_example` | The embedded valid fixtures, read-only; `spec_version` chooses the bundled version; evaluator guidance matches `0.3.0` examples and retains the legacy contract for default examples |
| `list_packs` / `get_pack` | This project's own packs, by decision id, through its `jpack.json` |
| `experimental_get_policy_contract` / `experimental_policy_init` / `experimental_get_policy_schema` | EXPERIMENTAL: exact Policy prototype or explicitly selected official draft capabilities, starter templates and schemas; no project reads or writes |
| `experimental_policy_canonicalize` / `experimental_policy_digest` / `experimental_policy_validate` / `experimental_policy_check` | EXPERIMENTAL: supplied Policy document identity, local checks and retained-material reference checks; no evaluation or authority |
| `experimental_evaluate` | EXPERIMENTAL SURFACE (ADR-0007): the §§7–8 resolution model; claim and scope in [`CONFORMANCE.md`](../CONFORMANCE.md) |
| `experimental_get_test_matrix_contract` | EXPERIMENTAL: discover the project matrix version, closed members, types and authoring rules without reading a project |
| `experimental_validate_test_matrix` | EXPERIMENTAL: validate matrix carrier and disposition-local constraints; optional exact pack text adds ordered per-case fact-type advice; no evaluation or writes |
| `experimental_check_fact_types` | EXPERIMENTAL: static fact-type advice from exact pack and facts JSON text, without test expectations, evaluation or writes |
| `experimental_validate_expectations` | EXPERIMENTAL SURFACE (ADR-0035): validate proposed dispositions under an explicit Core contract before authoring admission; no project or evaluation |
| `experimental_test_cases` | EXPERIMENTAL SURFACE: rehearse an exact `pack` JSON string against a `matrix` JSON string, with the same matrix admission, canonical comparison and coverage as `packs test`; reads no project files and appends no audit record |
| `experimental_test_packs` | EXPERIMENTAL SURFACE (ADR-0021): run declared instance matrices, the same payload `jpack packs test --format json` emits |
| `experimental_test_graphs` | EXPERIMENTAL SURFACE (ADR-0026): run declared graph matrices, the same payload the graph project walk emits |
| `experimental_list_graphs` / `experimental_get_graph` | EXPERIMENTAL SURFACE (ADR-0029): this project's configured graphs, by configured id — the graph siblings of `list_packs`/`get_pack`, read-only |
| `experimental_list_audit` | EXPERIMENTAL (ADR-0065): bounded exact byte export of configured evaluations, signatures and stamps, plus supported structured history lines; provisional until complete |
| `experimental_verify_audit` | EXPERIMENTAL (ADR-0065): the CLI verifier's full report for the configured trail or bounded supplied exact bytes, with supplied checkpoint expectations; no keys, trust roots, repair or writes |

None of these evaluate, decide, or authorize anything — `experimental_list_graphs` and
`experimental_get_graph` carry the experimental marker for their surface's stability, not because
they evaluate — except the four that reach the evaluator:
`experimental_evaluate`, which evaluates one case; `experimental_test_packs` and
`experimental_test_graphs`, which run declared instance and graph matrices respectively — each
through the same evaluator and comparison its own CLI counterpart uses, `packs test` and the graph
project walk; and `experimental_test_cases`, which runs a supplied matrix against supplied pack
text through the evaluator and comparison `packs test` uses, and reads no project file (ADR-0036).
The three matrix tools report
each row's agreement or divergence with the derived coverage report (ADR-0014, ADR-0023)
beside it, writing nothing, appending no audit record, and consulting no reviewed set, because a
matrix row is a rehearsal and not a decision. Each payload names the contract version it applied and
carries a
`conformanceClaimReference`
member pointing at [`CONFORMANCE.md`](../CONFORMANCE.md), where the conformance claim is stated in full
and only; no tool description, and no line of this document, states any part of it. Whatever that claim
says, it is about this implementation and not about the pack, the facts, or the wisdom of acting on a
disposition (JPS §3.5). Pack-aware tools select the exact `0.2.0-draft` or base-only Core `0.3.0` contract from the associated pack declaration.

On the wire, one request is one line of valid UTF-8 JSON, bounded at 16 MiB. A line that is not
valid UTF-8, or that carries an unpaired surrogate escape (`\ud800`–`\udfff` without its pair)
anywhere in it — in a tool argument, in a method name, in a string `id`, in any member at all — is
refused with a JSON-RPC parse error (`-32700`) under a null `id`, and the session continues with
the next request — so no tool ever receives text Go's decoder repaired into U+FFFD, which is what
a validator answering about a document nobody sent would come to
([ADR-0038](adr/0038-refuse-malformed-unicode-at-the-stdio-transport.md)). The check is the whole
line's, so it runs before the request is read at all: a line carrying the defect outside its
arguments is answered under a null `id` too, where an unknown method or a request that is not an
object used to be answered as `-32601` under the request's own `id` or as `-32600`. An error under
a null `id` names no request: JSON-RPC §5 answers under null whenever the request's id could not
be read — for a parse error and for an invalid request alike — and it gives no rule tying such an
answer to anything you sent. Attribute one to a message only when you know that no other message
you sent could have earned it — a notification included, because a notification carrying either
defect is refused under null exactly as a request is, so having one request outstanding is not
enough to make the refusal that request's. Otherwise the error stays uncorrelated, and never fail
a request on the strength of it: a valid request written behind a refused message is still
answered under its own `id`. Well-formed input is untouched: a surrogate pair is a character, and
a literal U+FFFD you authored is ordinary UTF-8. An unpaired escape, by contrast, is admitted by
JSON's own grammar — RFC 8259 §8.2 says the behaviour of software that receives one is
unpredictable, not that the text is malformed — so refusing it anywhere in the line is this
runtime's transport policy, taken because a canonical disposition §8.3 requires to compare byte
for byte cannot be built out of a repaired escape. The same escape one level deeper — written
inside a document's or an expectation's own JSON text, which reaches the tool as the six
characters `\ud800` — is refused by the carrier where it always was, as that tool's own
diagnostic rather than as a parse error.

### Proposed test expectations

`experimental_validate_expectations` is a read-only check of complete expected
§8.3 dispositions. Discover it in `tools/list`; older runtimes do not have it.

```json
{"spec_version":"0.2.0-draft","expectations":["{\"kind\":\"unresolved\",\"reasons\":[],\"handoff\":{\"state\":\"none\"}}"]}
```

This returns an aggregate `status: "invalid"`, `specVersion: "0.2.0-draft"`, and
one `results` entry per input. Each entry has its input `index` and `status`.
Valid entries include `canonical` JSON text. Invalid entries include a `code`
and `message`; the example is `JPS-EXPECTATION-INVALID` because an unresolved
disposition must retain reasons. Invalid expectations are reported rather than
dropped; the report itself is a successful tool call. Standalone authoring accepts `spec_version: "0.3.0"`; omission preserves the
`0.2.0-draft` default. Route presence never selects the contract. Unknown versions,
unknown arguments, empty/oversized batches, and non-string entries are tool errors.

A call accepts 1–256 strings; each is limited to 16 KiB, depth 16, 1,024 JSON
value nodes and 8 KiB per string. Limit findings use `JPS-EXPECTATION-LIMIT` and
mean the input was not admitted, not that its meaning violates Core, so branch on
`code` rather than on `status`.

Three shapes that §8.3's grammar admits are reported under a third code,
`JPS-EXPECTATION-UNREACHABLE` (ADR-0037): an `unresolved` result retaining
`not-applicable`, which §8 step 1 produces only under kind `not-applicable`;
`no-match` beside any other reason, which §8 records only at step 10, after every
step that records another reason has already produced `unresolved`; and an
`outcomeId` outside §5's local-id grammar `^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`, a
string no conforming pack can declare. Sending
`{"kind":"unresolved","reasons":["not-applicable"],"handoff":{"state":"none"}}`
returns that code with the step it names. The code means no pack can produce the
expectation, so it is the expectation and not the candidate that must change —
here, kind `not-applicable`. The check runs after the §8.3 grammar gate, so an
input that is both malformed and unreachable is reported for the grammar defect
under `JPS-EXPECTATION-INVALID`; only an input that is legal §8.3 and unreachable
gets the new code. An unreachable entry carries no `canonical`: it is not text to
store.

A valid finding is still necessary and not sufficient: it cannot prove *your*
pack produces that disposition, because pack-dependent reachability is not
checked — an admitted `outcomeId` may name no declared outcome of your pack, and
a handoff may be one your pack does not configure — and it does not validate
business intent or supply a corrected expectation. Reason and trigger sets are
normalized rather than refused, so `canonical`, not the text you sent, is what
this runtime compared: store that. It accesses no project, source, credential,
evaluator, audit record or network. See
[ADR-0035](adr/0035-validate-proposed-expectations-before-admission.md) and
[ADR-0037](adr/0037-report-pack-independent-unreachable-expectations.md).

Those limits are per expectation; the message carrying them has one of its own,
and it belongs to the transport rather than to this tool. One request is one
line, and the longest line this server reads is 16,777,216 bytes counting the
delimiter that ends it — a carriage return before the newline counts toward it
too — so 16 MiB exactly, 16,777,215 bytes of JSON and a newline, which a full
batch need not fit: sent as one `tools/call` line with every character written
as a `\u00XX` escape, a spelling JSON admits for any of them, 256 expectations
at the 16 KiB limit measure 25,166,754 bytes as one line, that call's framing
and delimiter counted with them. A longer line is never parsed and is answered
by nothing: the server reports `mcp: input error: bufio.Scanner: token too long`
on stderr and exits 4, leaving every request queued behind that line unanswered,
so a client that sends one restarts the server rather than waits for a reply.
This tool's reply is bounded by neither of those limits and can be larger than
the call that asked for it, because the report is carried twice, once as
`content[0].text` and once as `structuredContent`, and each valid entry carries
its canonical text: 256 expectations each naming an outcome id of 8,192 letters,
escaped only where JSON requires it, are a 2,120,866-byte call answered by
4.08 MiB, 2.02 times what it sent. The ratio is the construction's, not a
constant: canonical text can be shorter than what was sent — an expectation
padded with whitespace loses the padding — so a reply can also be smaller than
its call, as a short disposition padded to the bound is answered by less than it
sent. Size the reply from what is sent, not from the bound; this tool bounds
nothing on the way out. Where a reply on this surface is bounded, the bound
refuses rather than truncates: `experimental_test_packs` and
`experimental_test_graphs` refuse a marshaled report over 16 MiB
([ADR-0021](adr/0021-run-the-declared-matrix-over-mcp.md)), and
`experimental_list_graphs` refuses an inventory whose bytes read and identities
echoed would pass its 8 MiB budget.

A stored expectation that a matrix, graph or corpus row already carries is read
by the same decoder, which now refuses four shapes it accepted before: `reasons`
missing or null on an outcome, `outcomeId` as `""` or `null` on a non-outcome
kind, `triggeredBy` as `[]` or `null` beside `"state": "none"`, and a retained
`exception-escalation` reason without a `requested` handoff naming it — §8.1
makes that reason a direct request whatever the pack configures. Such a row is
reported as a mismatch naming the rule.

### The project tools

`list_packs` and `get_pack` read the [`jpack.json` project
convention](../README.md#the-jpackjson-project-convention) — this runtime's convention, not part of
the specification ([ADR-0012](adr/0012-jpack-project-convention.md)). `list_packs` returns the
resolved, token-cheap inventory: the project's decision id, the pack document's own id and version,
the description, the ids of the evidence the pack requires, the fact pointers the pack's conditions
read (`consultedFactPaths`, sorted and deduplicated — how an application names the candidate
pointers an escalation may be waiting on, and checks that every consulted pointer has a producer;
it over-approximates by design, so its values are untrusted document content), whether an
instance matrix exists, and the project's non-normative hints about where each fact and each piece
of evidence is held.
`get_pack { pack_id }` returns one document as text, read-only — served exactly when its bytes are
valid UTF-8, which is the one thing a text result can carry losslessly (invalid bytes are refused
with the configured path rather than silently transcoded); a served document that did not decode
carries a status saying so and a `detail`, rather than posing as a valid document with empty
identity members. `experimental_evaluate` accepts `pack_id` in place of `pack`; the two are
mutually exclusive, and supplying both is refused rather than given a precedence rule. That exclusion
is stated in both property descriptions and enforced by the server, and deliberately not advertised as
a composed schema keyword: every tool's `inputSchema` here is a flat object, so a client or a bridge
that re-emits these schemas into a provider's function-declaration format has nothing to drop or
reject.

The server reads the file named by `JPACK_CONFIG`, or `jpack.json` in the directory it was launched
in. There is no path argument over the wire, for the reason no tool takes a document by path: a
long-lived endpoint whose answers depend on the client's idea of the server's filesystem is not
portable across client topologies (ADR-0006). Set it per server if the launch directory is not the
project root:

```json
{
  "mcpServers": {
    "jpack": {
      "type": "stdio",
      "command": "jpack",
      "args": ["mcp"],
      "env": { "JPACK_CONFIG": "/abs/path/to/jpack.json" }
    }
  }
}
```

With no configuration, `list_packs` answers **empty with an explanation of where the runtime
looked** — a project that does not use the convention is an ordinary project, and pack text still
works everywhere. Every file is read through a reader rooted at the configuration's own directory;
a configured path that leaves it is refused. The hints are text the project wrote: this server holds
no credential, opens no connection, and never reads a source one names. Gathering those values is
the client's, with the client's own access — and a value you cannot source is reported `unknown`
rather than guessed, so the pack can escalate instead of deciding on an invention.
[building-with-packs.md](building-with-packs.md) is the full guide.

One thing a configuration can ask this server to write, and nothing else can: under `configVersion
"3"` an `audit` member names a directory relative to `jpack.json`, and each completed
`experimental_evaluate` call — a declared rehearsal excepted (ADR-0028) — then appends one record to `evaluations.jsonl` in it — the pack's
identity and digest, the documents it was evaluated against, and the disposition in canonical form
([ADR-0018](adr/0018-opt-in-evaluation-audit-trail.md)). The record goes through the same rooted
handle every read is bound to, into the project's own tree; a refused evaluation records nothing, and a
record that cannot be written is reported as a tool error instead of a disposition. A project that
declares no `audit` member is written to by nothing.

## The prompts

The server also serves six **method prompts** (MCP `prompts` capability) — static, versioned,
non-normative guidance that your client's model executes with your key. In Claude Code they appear
as slash commands (`/mcp__jpack__author_pack`, `…test_pack`, `…fix_pack`,
`…explain_disposition`, `…present_pack`, `…author_graph`); other clients surface them differently or not at all, and everything
works without them.

| Prompt | Guides |
| --- | --- |
| `author_pack` | Encoding one policy decision: the create → validate → evaluate loop, the resolution-model shapes that avoid conflicts, the decimal-string and `onUnknown` rules, the prepared-facts ledger |
| `test_pack` | Probing a pack's logic with an instance matrix (per-outcome, conflict, unknown, missing-evidence, not-applicable, forced-outcome, ordered-comparison rows) |
| `fix_pack` | Repairing a non-conformant pack from the validator's diagnostics, in carrier → structural → semantic order |
| `explain_disposition` | Narrating an evaluation payload strictly from the record it carries — the authoritative disposition, the informative (possibly partial) `trace[]`, the pack's members: reproduce the complete reason set, echo the handoff as recorded, and never soften or extend the disposition |
| `present_pack` | Presenting one pack to an audience, grounded in the document alone: every statement traceable to a member, the representation labeled as one reading, omissions stated, semantics kept the format's — and no outcome ever simulated in place of an evaluation |
| `author_graph` | Composing existing packs into an experimental graph document: declare only relationships the source states, check both ends of every verbatim edge, record what cannot be declared, validate to exit 0, give the graph its rows and read the coverage report, and hand the proposal — the `jpack.json` declaration included — to a human; the packs themselves are never edited |

Following a prompt does not make a pack conformant — only validation decides that — and the
documents you produce are yours; the runtime stores and interprets nothing (ADR-0008).

## Claude Code

One command, from any directory:

```bash
claude mcp add --scope user jpack -- jpack mcp
```

Or per project, in `.mcp.json` at the project root (gitignored here):

```json
{
  "mcpServers": {
    "jpack": {
      "type": "stdio",
      "command": "jpack",
      "args": ["mcp"]
    }
  }
}
```

Restart the session, approve the server when prompted, and confirm with `/mcp`.

## OpenAI Codex CLI

In `~/.codex/config.toml`:

```toml
[mcp_servers.jpack]
command = "jpack"
args = ["mcp"]
```

Or equivalently: `codex mcp add jpack -- jpack mcp`. Start `codex` and confirm
with `/mcp`. Judge an automatic approval mode on three facts. Every tool here is read-only except
the experimental evaluator. The evaluator writes only where the project told it to: in a project
whose `jpack.json` declares no `audit` directory nothing is written at all, and in one that
declares an `audit` directory each completed evaluation — a declared rehearsal excepted (ADR-0028) — appends one record there and nowhere else.
A call carrying `"rehearsal": true` also consults no reviewed set (ADR-0019), so an approval mode
that trusts the lock to catch drifted law is trusting a check a rehearsal deliberately skips —
which is safe exactly because the payload it returns is labeled a rehearsal, never a decision.
And `test_conformance` reads any suite path the caller names, so it is not confined to the
directory the server was launched in — the rest of the tools read the project's own files and the
documents you pass.

## GitHub Copilot (VS Code)

In `.vscode/mcp.json` at the workspace root (gitignored here) — note the root key is `servers`,
not `mcpServers`:

```json
{
  "servers": {
    "jpack": {
      "type": "stdio",
      "command": "jpack",
      "args": ["mcp"]
    }
  }
}
```

For a user-level server instead, run **MCP: Open User Configuration** from the Command Palette.
Copilot uses MCP tools in agent mode; see the
[VS Code MCP documentation](https://code.visualstudio.com/docs/agents/reference/mcp-configuration)
and [GitHub's Copilot MCP guide](https://docs.github.com/copilot/customizing-copilot/using-model-context-protocol/extending-copilot-chat-with-mcp)
for the current UI flow. The Copilot CLI has its own
[MCP configuration](https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-mcp-servers).

## Remote-only clients

Some clients (the ChatGPT desktop app's connectors, Microsoft Copilot Studio) cannot launch a
local process and require a public HTTPS MCP endpoint. This runtime deliberately does not serve
HTTP ([ADR-0004](adr/0004-decline-http-api.md)); reaching those clients means an external
stdio-to-HTTP bridge plus a tunnel, which is developer tooling outside this repository, or a
separately governed hosted distribution.

## Troubleshooting

- **Server fails to start:** run `jpack mcp` in a terminal yourself; if it starts and
  waits on stdin, the binary is fine and the problem is the client's command path or config file.
- **Config edits not picked up:** clients read MCP configuration at session start — restart the
  session.
- **Old binary:** `jpack version` must report a release that has the `mcp` command
  (v0.1.0 or later); a stale build on the PATH is easy to pick up by mistake.

The end-to-end authoring loop these tools support — create, read, update, delete, with the
runtime as a stateless oracle for every step of it — is described in
[authoring-lifecycle.md](authoring-lifecycle.md), and the agent-driven testing protocol in
[agent-testing.md](agent-testing.md).

### Test proposal correction

Clients can fetch `experimental_get_test_matrix_contract` before generating tests
and call `experimental_validate_test_matrix` with a `matrix` JSON string afterward.
The versioned report has `contractVersion: "1"`, aggregate `status`, root `findings`,
and ordered `results` containing `index`, optional `id`, `status`, and `findings`.
Each finding has `code`, `path` (JSON Pointer) and `message`. Root/carrier failures
may prevent row admission; otherwise every row is accounted for. `MATRIX-LIMIT`
means the input was not admitted and is not a policy defect to repair.

The validator reuses `DecodeMatrix` and the exact disposition decoder. It does not
evaluate the pack, infer expected answers, verify source authority, or establish
reachability. Desk source mappings and other application metadata stay outside the
matrix. Clients retain rejected proposals and control bounded correction and
human review; a valid report never means tests passed or cases were saved.

Supplying optional `pack` JSON text to `experimental_validate_test_matrix` adds
`factCheckVersion: "1"` and `factChecks` only after the whole matrix is structurally
valid. There is one fact-check row per case, in input order, carrying `index`, `id`,
`status` (`complete` or `unavailable`) and an array of `findings`. Unavailable rows
also carry a nonempty `message`. The matrix's structural status is unchanged:
deliberately wrong-type probes remain legal tests, including cases expecting an
evaluation error. Unsupported or malformed packs produce unavailable advice for
every row, without changing independently authored expectations.

Prepared inputs need no expectation. Call `experimental_check_fact_types` with
required `pack` and `facts`, both exact JSON strings, and optional
`supported_extensions`, an array of strings. Its report carries `outputVersion`,
`tool`, `command`, `experimental: true`, `contractVersion: "1"`,
the selected `specVersion`, `factCheckVersion: "1"`, `status` and `findings`,
plus `message` when unavailable. A complete check can have findings; it is not an
evaluation result or a judgment about policy. Advice selects the exact associated
pack contract (`0.2.0-draft` or base-only `0.3.0`), and preserves an unsupported
profile refusal. It changes no independently authored expectation.

Both tools reuse the evaluator's static comparability walk: every stated
comparison is checked, even where evaluation would not reach it. Missing facts
are not findings. Null, false, zero, JSON numbers, decimal strings, arrays and
objects retain their representation; nothing is filled in or coerced. Each
finding has `code: "FACT-NOT-COMPARABLE"`, `path` relative to facts, `operator`,
`factType`, nonempty `expectedTypes`, and `message`. An optional `within` identifies
the collection context for the evaluator's internal draft aggregate path; these
MCP tools do not enable draft grammars. Findings report types, never fact values.
The caller reviews accidental encodings and deliberate type probes separately.

The ordinary input carrier limits apply: 10 MiB per pack or facts document,
depth 128, 250,000 nodes and 1 MiB per string. Each check permits 100,000 comparison
work units, 100 findings and 256 KiB of JSON-serialized findings. A limit discards
the partial check and returns `unavailable` with an empty findings array. Supported
extensions are limited to 256 strings, each 1,024 UTF-8 bytes. Pack-aware matrix
advice permits 256 cases, 16 distinct extension sets, 1,024 findings total and
4 MiB of JSON-serialized fact-check rows; exceeding a batch bound is a tool error,
never a clean or partial result. A caller can split a batch to fit these advisory
bounds. Matrix-only validation retains its existing limits and response shape.
No project configuration, reviewed-set lock, audit trail, source, model or network
is accessed by either advisory path.

## Dialect document tools

Discover `get_dialect_contract` before using the additive `dialect_init`,
`dialect_canonicalize`, `dialect_digest`, `dialect_validate`, `get_dialect_schema`,
`get_dialect_reference` and `dialect_check` tools. `dialect_init` accepts an optional
integer `version` (2 by default; 3 is also available). Encoding and validation tools require
`document` as exact JSON text, not a parsed object or file path. They share the
CLI's report and require no project, credentials, network or model.

`dialect_validate` checks version-2 and version-3 documents' closed forms, internal
references and limits. A pass reports `document-valid` and
`validationScope: "version-2-document"` or `"version-3-document"`, with canonical bytes and their digest.
It does not check profile/request compatibility, installed capabilities, pack
coverage, source truth or preparation verification. This validation pass makes no claim about those checks; inspect the separate
operation capabilities. Version 3 supports operation, selected-file and research
source declarations; HTTP, SQL and web declarations remain unsupported. Research
validation checks declarations and reconciliation rules, not acquired results.
Inspect `validationVersions` and `validationSourceKinds`, not `encodingVersions`, before
calling the validator. Schema/reference retrieval and declared pack-inventory checks are available
through their separate tools (ADR-0054).

An `encoded` response is never a validation pass. Neither response authorizes
saving or execution. Preserve the client's own approval and persistence rules.
Unknown operations must remain unavailable rather than falling back to an
encoding result as a validation pass. See ADR-0052 and ADR-0053.

`get_dialect_schema` accepts an optional integer `version` (2 only). Its resource
is a Runtime-derived editing aid; semantic checks still require validation.
`get_dialect_reference` takes no arguments and serves exact pinned text offline,
including proposed forms that Runtime may not implement. Both return resource
text, origin, media type, byte length and SHA-256.

`dialect_check` requires exact JSON strings `document` and `pack`. It validates
both and reports declared inventory coverage without project lookup. Inspect
`compatibility.status`: `complete`, `review-required`, `incomplete` or
`not-applicable`. An acknowledged omission or partial aggregate is not full
coverage. Check failure uses `isError`; a completed incomplete report still
contains findings and both checked identities. Refusals contain no coverage or
dialect artifact. Actual types, source execution and preparation verification
remain unchecked. Discover `packCheckVersions` and `packCheckScope` first; never
substitute encoding success for a missing check capability.

## Artifact version arithmetic

`get_artifact_version_options` takes a required `base` string and optional
`candidate` string. Both use the Core pack's exact x.y.z grammar without leading
zeroes or suffixes. It returns `choices.patch`, `choices.minor`, `choices.major`
and, when a candidate is provided, its numeric `comparison` (`lower`, `equal`,
`greater`). Components are exact integers, including beyond JavaScript precision.
Each version is bounded to 256 ASCII bytes. A base is refused if any next choice
exceeds that bound; a returned choice at the boundary may have no further bump.

The report scope is `version-arithmetic` and `compatibilityChecked` is false.
This read-only tool accesses no project, job history or registry. Version choice,
compatibility review, immutable publication and permission to save stay with the
caller. Clients discover the capability through `tools/list`. CLI equivalent:
`jpack artifact version --base 0.1.0 --candidate 1.0.0 --format json`.

## Experimental Policy prototype tools

Discover `experimental_get_policy_contract` before using this additive offline
surface ([ADR-0059](adr/0059-experimental-offline-policy-tools.md)). All seven
tools retain the exact nonnormative RFC 0023 prototype `0.0.2`. No stable Policy format is implied. Schema resources are self-contained
Runtime editing copies derived from pinned prototype schemas. Reports carry
experimental status,
pinned provenance, capability scope and diagnostics shared with the CLI.

| Tool | Required arguments |
| --- | --- |
| `experimental_get_policy_contract` | None; an optional empty object |
| `experimental_policy_init` | `kind`, `formatVersion`, `id`, `version`, all strings |
| `experimental_get_policy_schema` | `kind`, `formatVersion`, both strings |
| `experimental_policy_canonicalize` | `document`, exact JSON text string |
| `experimental_policy_digest` | `document`, exact JSON text string |
| `experimental_policy_validate` | `document`, exact JSON text string |
| `experimental_policy_check` | `document` and `materials`, exact JSON text strings |

`kind` is exactly `policy` or `governing-adoption`; `formatVersion` is exactly
`0.0.2` for prototype use, or `1.0.0` for explicit official draft selection below.
The explicit artifact `id` and `version` are separate from that format.
Unknown members, duplicate members, wrong case, missing required arguments and
null argument values are refused. Malformed or missing arguments are tool errors. Well-typed unsupported selectors,
invalid artifact identities, and invalid document or retained-material content
return a typed refusal report with diagnostics, capabilities and `isError: true`.

Init returns a `starter-template` with missing retained materials; it does not
publish a document or establish a checked adoption. Canonicalize and digest
report canonical identity separately from raw byte identity; encoding success
is not validation. Validate checks document forms, schemas and local references.
Check verifies the exact supplied retained-material closure, raw byte identity,
Policy section anchors, structural Core packs and specific consumer references.
Full Core semantics, evaluation, source acquisition, approval and
governing-adoption authority are outside this surface. A raw Core pack is not
restricted to the canonical number domain unless an explicit canonical reference
requests that identity.

The `materials` **string** contains the private Runtime supplied-byte transport:

```json
{"materials":[{"content":{"bytesDigest":"sha256:<64 lowercase hex>","byteLength":12,"mediaType":"text/plain"},"dataBase64":"<base64 of exact retained bytes>"}]}
```

Replace the placeholders with exact content references and bytes. This transport
is not a portable release envelope and accepts no paths or URLs. None of these
tools reads a project, resolves a locator, writes a file, contacts a provider,
changes billing or returns retained-material bytes in reports or errors.

The decoded document limit is 256 KiB. Retained source originals are bounded at
4 MiB, narrative and review-basis material at 1 MiB each, and distinct retained
bytes at 16 MiB. The material JSON text bound is 24 MiB. The existing **16 MiB
stdio request-line limit**, including its delimiter, remains the tighter wire
limit: base64 expands retained bytes and JSON string escaping, document text and
RPC framing consume more space. Consequently, not every bundle under the 16 MiB
retained-byte limit fits a single MCP request. A client must bound the complete
encoded request; CLI named-file input supports the larger material JSON envelope.

### Explicit official draft selection

The seven existing tools also serve official Policy draft format `1.0.0`, pinned
to `policy-v1.0.0-draft.1` at Spec commit
`0799b37b74e35c4a86bfbf70f7bee80bdb7bbe94`
([ADR-0061](adr/0061-adopt-official-policy-draft-resources.md)). Calling contract
without arguments or with `{}` returns the exact existing prototype report.
`{allFormats:true}` explicitly requests registry version 1;
`{formatVersion:"1.0.0"}` selects the official draft contract. The selectors are
mutually exclusive. `{formatVersion:"0.0.2"}` selects the prototype contract.

Init/schema require the exact selected `formatVersion`. Document tools accept
an optional `formatVersion` string for explicit dispatch, including draft
classification of malformed or selector-less bytes. The document must still carry
its own selector; no tool inserts or rewrites it. Without an operation selector,
a strict document carrying `formatVersion` dispatches to the draft. Draft Policies
use `workspace-managed` or `externally-managed`, never the prototype's
`desk-managed`. Draft adoption requires retained 1.0.0 Policy bytes.

Explicit draft reports carry `profile:"jps-policy-companion"`,
`profileVersion`/`formatVersion:"1.0.0"`, `maturity:"draft"`, the exact new
`specRevision` and its immutable `spec/policy.md` URL. The existing
`candidateCapabilities` member is the exact official artifact. Only full checks
add `candidateResult`, exactly `{status:"valid",formatVersion:"1.0.0",maturity:"draft"}`
or `{status:"refused",code:"…"}`. Finite codes follow the
[pinned draft contract](../internal/policy/official/spec/policy.md#5-resource-limits-and-refusal-classifications).
Encoding/local validation omit that result; operational failures supply none.
MCP sets `isError:true` on refusals. Validate's scope is
`closed-draft-document-and-local-references`; check's scope is
`draft-supplied-exact-bytes-anchors-and-scoped-core-references`.
`experimental` remains true, and `fullDocumentConformance`, approval, execution,
live-authority and network-acquisition capabilities remain false.

Strict v0.36.0 consumers must explicitly update their Runtime/Spec pins and exact
metadata expectations: maturity, provenance URL/commit, capability exclusions,
scope strings and the derived editing schema ID change. Existing member names,
`outputVersion:"2"`, `contractVersion:"1"`, refusal codes and document grammar
stay unchanged. `resource.sourceId` identifies the immutable official HTTPS schema;
the self-contained editing copy has a separate Runtime URN containing the release
tag. The complete published archive and manifest are retained under
`internal/policy/official`. Historical candidate reports and vendor bytes keep
their original proposed maturity and provenance. No Policy/adoption/retained bytes,
approvals, Jobs or host authority migrate automatically.

Draft physical bounds and strict JSON parsing precede selector/canonical checks,
then ordered registration precedes document schema and reference checks. Retained
artifact byte pins precede parsing and optional canonical identity. Pack bytes go
unchanged to Core structural validation. The private material carrier and the
16 MiB stdio request ceiling remain unchanged; repeated base64 registrations can
exceed a transport ceiling while their distinct retained bytes fit the draft.
The Go reader's agreement with all 124 exact official results and the retained
reference reader supplies same-vendor evidence. It establishes no stable maturity,
independently authored interoperability, current authority, permission or trusted
review. Runtime's conformance claim remains solely in [CONFORMANCE.md](../CONFORMANCE.md).

## Core 0.3.0 selection and routed reports

`describe_runtime` reports document `supportedSpecVersions` separately from
`evaluatorCapabilities`. The `0.3.0` evaluator entry carries
`supportedProfiles: ["base"]`; profile support is scoped to that entry, with no
Policy or Dialect implication. Missing pack profile means base without changing
its bytes. A declared extended profile is refused with exactly the diagnostic
`{"code":"profile-unsupported","profile":"extended"}`, before structural
interpretation or facts/evidence admission.

Standalone matrix-contract discovery, matrix validation without a pack, and
expectation validation accept an explicit `spec_version`, defaulting to
`0.2.0-draft`. When exact pack text is supplied, that pack selects the contract.
Matrix carriers stay at versions `1`–`3`; they gain no implicit route-based dispatch.
Project walks, graphs and comparison refuse mixed exact Core versions before
running any evaluation, even if no input row would run.

Core `0.3.0` routed dispositions retain route presence, exact target names,
declaration order and duplicates. A routed handoff with no selected target carries
`routes: []`; a target-only pack keeps its previous canonical bytes. Singular
target assertions mismatch for routed results, including null assertions. Graph
handoffs fan out in declaration order, retaining duplicate targets. Aggregated target
displays are sanitized; portable route names remain exact. Core `0.3.0` graph suite
entries add `evaluatorSpecVersion`, omitted from legacy entries. Audit records
retain exact canonical bytes and the actual selected contract.

Core `0.3.0` project matrix reports allow at most 10 MiB of expected plus actual
canonical disposition text (`JPS-RESOURCE-MATRIX-DISPOSITIONS`). Graph reports and
Core `0.3.0` graph matrix/suite reports are bounded at 16 MiB, with incremental
node/handoff/headline charging and a final envelope check. Comparisons retain their
existing report limit. These bounds refuse whole reports rather than truncate
portable dispositions. The conformance runner reports optional advisory rows as
`skipped`, `actualStatus: "not-checked"`, with `skipReason` and a separate summary
count; they never count as passed assertions. An advisory-only suite is refused
with `JPS-SUITE-NO-CHECKS`, with no successful zero-check report.

See [ADR-0063](adr/0063-explicit-core-030-contracts.md) for provenance and contract
choices, and [CONFORMANCE.md](../CONFORMANCE.md) for conformance posture and evidence.

## Experimental audit reading and verification

`experimental_list_audit` and `experimental_verify_audit` are read-only Runtime
conventions ([ADR-0065](adr/0065-read-audit-trails-over-mcp.md), #308). They add
no Core conformance claim or signed audit format. The CLI and retained audit
formats remain unchanged. Neither tool evaluates, writes, repairs, loads a
signing key, fetches a timestamp nor accepts a server filesystem path.

Both require a present, loadable project, located by `JPACK_CONFIG` or the
server's `jpack.json`, declaring a contained `audit.dir`. No argument changes
that authority. All configured file reads retain the project's rooted
containment, final-symlink and regular-file guards. The supplied-data verification
mode also requires this configured project and contained directory. Errors are
finite `isError:true` tool results and never repeat retained contents, rejected
values, private-key settings or host paths. A valid verification report whose
`status` is `invalid` remains `isError:false`: it reports findings successfully.

### Listing and exact downloads

Call `experimental_list_audit` with `{}` or `{"page_bytes":65536}`. Only
`cursor` and `page_bytes` are accepted, once with exact spellings. Omit the
cursor to start; a supplied cursor must be a nonempty string of at most 85 ASCII
bytes. `page_bytes` is an integer from 1 to 262144, default 65536. Null, strings
for integers, fractional/exponent notation, unknown or repeated members and
caller paths are refused, including in standalone handler calls.

The `auditWireVersion:"1"` report also carries the usual `outputVersion`,
`tool` and `command`, plus:

| Member | Meaning |
| --- | --- |
| `files` | Fixed order: `evaluations.jsonl`, `signatures.jsonl`, `stamps.jsonl`. Each has `name`, `present`, `bytes` and, when present, `digest` of the captured exact prefix, including every delimiter. Missing and present-but-empty are distinct. |
| `file`, `offset`, `dataBase64` | This page's fixed filename, starting byte offset and canonical padded base64 of exact bytes. Append decoded bytes at the stated offset. Never reconstruct a download by reencoding `members`. |
| `records` | Line results whose final bytes were transported on this page, including a line begun on an earlier raw page. No filtering or identity deduplication occurs. |
| `pendingLine` | A line has begun and a later page will finish it. Its structured result has not been omitted. |
| `fileComplete` | This captured file prefix has been fully transported; the next page visits the next fixed file. Missing/empty files each have a zero-byte page. |
| `complete`, `nextCursor` | Only `complete:true` with no cursor finishes the whole export after final integrity checks. Every provisional page carries a continuation. |
| `snapshotBetweenWrites` | The initial trail/sidecar sizes were captured under the existing cooperative trail lock. False when no trail exists or locking is unsupported. Stamps are captured separately under their own shared lock, after releasing the trail lock, even when evaluations is absent; this flag describes only the trail/sidecar capture. This is not a claim of an atomic snapshot against arbitrary writers. |

A line result has `offset`, `bytes`, `digest` and `status`. The byte count and
SHA-256 exclude only the LF delimiter: whitespace and a preceding CR remain
part of the exact line and its digest. Status is:

| Status | Retained information |
| --- | --- |
| `parsed` | A complete UTF-8 JSON-object line of at most 65536 bytes; all members in `members`, exact bytes in `rawLineBase64`, with no schema reinterpretation. |
| `malformed` | A complete bounded line that is empty, nonobject, invalid JSON/UTF-8 or contains an unpaired Unicode surrogate escape; exact bytes in `rawLineBase64`, no `members`, no silent Unicode repair. |
| `oversized` | A complete line longer than 65536 bytes; count, offset and digest only. Its complete bytes still travel in raw chunks. A valid Runtime record may exceed this structured limit. |
| `incomplete` | Final bytes after the last LF, with count, offset and digest; exact `rawLineBase64` only within the structured limit, no `members`. This is never a completed decision. |

The base64 encoding of an empty raw line is the empty string; that optional
field may be absent for a zero-byte malformed line. Consumers can select ordinary
definition history directly from `members.recordVersion`, `members.pack.id` or
`members.graph.id`, retain the separately supplied exact raw line and digest,
and order/filter by offsets. They need not parse JSONL. Cross-page lines are
reported exactly once when finished. For supported parsed lines, reproduce
Desk's history selection with version `"1"`, the desired pack or graph identity,
newest first, retaining duplicate identities. Faithful selection uses Desk's
whole-object typed JSON decoding, rather than exact-key map lookups: Go matches
the `recordVersion`, `pack`, `graph` and nested `id` tags with exact-name or
case-insensitive fallback, including its ordinary null/duplicate-member behavior.
Both optional identity objects must decode successfully, even if only the other
kind is requested. For example, `{"RecordVersion":"1","pack":{"id":"same"}}`
is selected for that pack, while
`{"recordVersion":"1","pack":{"id":"same"},"graph":{"id":1}}` is omitted
because the unrelated typed `graph.id` is malformed. The wire preserves every
member; it does not filter by these history rules. This is explicitly bounded history
parity: Desk's local reader has a 4 MiB read window, while this wire's decoded
line limit is 64 KiB. A larger valid line can appear in local Desk history but
has only raw downloadable bytes and `oversized` metadata here. Skipping it is
not exact history parity; consumers must say that structured history is limited.
The Unicode rule is also stricter than Go's historical replacement behavior:
unpaired surrogate escapes remain exact raw bytes but have no structured members.

Follow the returned cursor on the same server instance, optionally changing
`page_bytes`. The most recently answered cursor can be retried idempotently,
returning its original response even if the retry requests another page size;
this includes the final response. Older cursors are refused. The initial request
without a cursor starts a new export and is not an idempotent resume. At most
eight export states are retained for five minutes from creation; completed
states can be evicted to admit a new export. Cursors do not survive restart.
Concurrent calls for one current cursor return the same page and advance once.

Each captured file is bounded at 64 MiB. The initial pass streams fixed 64 KiB
block hashes and a full digest, serving verifies each block into a single cache,
and finalization checks every captured block again. Thus file I/O is linear;
small byte pages do not rehash the whole file. Identity, presence and minimum
size are checked on every advancing call. Later appends are excluded. Replacement,
removal, truncation or a changed unserved block causes refusal; in-place edits
to already served blocks may be detected only by the final pass. An intermediate
page is provisional. Discard the whole export if any page fails or completion
is not reached. Retrying the latest answered cursor returns that historical
page, without making a new statement about current filesystem contents.

Memory remains bounded by block hashes, one 64 KiB block, one 64 KiB pending
line and the last response per export, rather than full-file materialization.
A page returns at most 128 line results and can therefore contain fewer raw
bytes than requested. The complete serialized MCP tool result is limited to
4 MiB, counting text/structured duplication, base64 and escaping. An amplified
response is refused whole. The server invalidates that export and releases its
state, including on a rejected continuation/final page; the old cursor cannot
resume. Start a new export with smaller `page_bytes`. Rejected responses retain
no inaccessible active slots and do not invalidate other accepted exports.
No refusal can claim a partially exported file is complete.

### Verification and checkpoint expectations

`experimental_verify_audit` accepts only these optional members:

| Argument | Meaning and bound |
| --- | --- |
| `expect` | Supplied checkpoint JSONL, content equivalent of CLI `--expect`, at most 1 MiB. Uses the same `audit.ParseCheckpoints`: every supplied checkpoint must match, blank lines pass, empty/no-checkpoint or unsupported versions refuse. Combine repeated CLI expectation files as newline-separated supplied text. |
| `require_checkpoint_through` | Integer 0–9007199254740990, default 0. Same `audit.Verify` requirement as CLI `--require-checkpoint-through`; 0 requests no minimum. |
| `trail_base64` | Canonical padded base64 of at most 8 MiB of exact supplied bytes, content equivalent of CLI `--trail` without a path selector. Omit to verify the guarded configured `evaluations.jsonl`, bounded to 64 MiB. The empty string supplies an empty trail. |

The existing 16 MiB complete request-line ceiling applies in addition to these
decoded bounds, and the 4 MiB serialized result limit applies here too. Null,
unknown members, paths, malformed/case-folded/repeated arguments, noncanonical
base64 and out-of-bound values refuse with no partial report. No signature
public keys, stamp roots or witness inputs are accepted; their coverage remains
explicitly unchecked. Signatures/stamps remain downloadable independently.

The tool calls the CLI's same `audit.Verify` implementation and returns every
field of `AuditVerification`, including coverage, segments/discontinuities,
held/required checkpoint results, findings/totals, incomplete-tail digest and the
statements of what the verification does and does not establish. Source metadata
is honest: `command` is `mcp experimental_verify_audit`; configured `trailPath`
is project-relative, while supplied bytes use `(supplied trail)` and
`snapshotBetweenWrites:false`. The corresponding CLI file read can report true
because it captured a file size under a cooperative lock. No verifier field is
replaced to hide those source differences. A missing configured trail refuses;
a present empty trail receives the same report as CLI verification of empty bytes.

For example, `{"expect":"{\"checkpointVersion\":\"1\",...}\n","require_checkpoint_through":1}`
supplies held checkpoint text; the actual checkpoint must contain exactly the
CLI's supported four members. `{"trail_base64":""}` checks an empty supplied
trail under the same configured-project requirement.
