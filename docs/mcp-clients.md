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
| `validate` | Carrier, structural, and semantic conformance for one document |
| `test_conformance` | Run a version-pinned conformance corpus |
| `get_schema` | The exact bundled JSON Schema, with digest |
| `describe_runtime` | Versions and artifact provenance |
| `list_examples` / `get_example` | The embedded valid fixtures, read-only |
| `list_packs` / `get_pack` | This project's own packs, by decision id, through its `jpack.json` |
| `experimental_evaluate` | EXPERIMENTAL SURFACE (ADR-0007): the §§7–8 resolution model; claim and scope in [`CONFORMANCE.md`](../CONFORMANCE.md) |
| `experimental_test_packs` | EXPERIMENTAL SURFACE (ADR-0021): run declared instance matrices, the same payload `jpack packs test --format json` emits |

None of these evaluate, decide, or authorize anything, except the two that reach the evaluator:
`experimental_evaluate`, which evaluates one case, and `experimental_test_packs`, which runs the
declared instance matrices through the same evaluator and comparison the CLI's `packs test` uses —
reporting each row's agreement or divergence with the derived coverage report (ADR-0014, ADR-0023)
beside it, writing nothing, appending no audit record, and consulting no reviewed set, because a
matrix row is a rehearsal and not a decision. Each payload names the contract version it applied and
carries a
`conformanceClaimReference`
member pointing at [`CONFORMANCE.md`](../CONFORMANCE.md), where the conformance claim is stated in full
and only; no tool description, and no line of this document, states any part of it. Whatever that claim
says, it is about this implementation and not about the pack, the facts, or the wisdom of acting on a
disposition (JPS §3.5). It evaluates only a pack declaring `specVersion` `0.2.0-draft` (§11).

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
`get_pack { pack_id }` returns one document, read-only and unaltered — and a document that was read
and did not decode is served with a status saying so and a `detail`, rather than as a valid document
with empty identity members. `experimental_evaluate` accepts `pack_id` in place of `pack`; the two are
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
`experimental_evaluate` call then appends one record to `evaluations.jsonl` in it — the pack's
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
declares an `audit` directory each completed evaluation appends one record there and nowhere else.
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
