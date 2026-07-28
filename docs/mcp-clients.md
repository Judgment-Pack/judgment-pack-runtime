# Connecting MCP clients

`judgment-pack mcp` serves the offline validator to any Model Context Protocol client over stdio:
no port, no credential, no network. The client supplies its own model and API key; the runtime
never sees either (see [ADR-0003](adr/0003-mcp-integration-and-testing-surface.md) and
[ADR-0006](adr/0006-authoring-lifecycle-in-the-client.md)).

Every client below launches the same binary. Install a released `judgment-pack` (see the
[README](../README.md)) and make sure the command the client launches resolves — if `judgment-pack`
is not on the PATH the client's launcher inherits, use the absolute path instead; a wrong path is
the most common failure and most clients report it poorly.

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
| `experimental_evaluate` | EXPERIMENTAL SURFACE (ADR-0007): the §§7–8 resolution model; claim and scope in [`CONFORMANCE.md`](../CONFORMANCE.md) |

None of these evaluate, decide, or authorize anything, except `experimental_evaluate`, which
evaluates. Its payload names the contract version it applied and carries a `conformanceClaimReference`
member pointing at [`CONFORMANCE.md`](../CONFORMANCE.md), where the conformance claim is stated in full
and only; no tool description, and no line of this document, states any part of it. Whatever that claim
says, it is about this implementation and not about the pack, the facts, or the wisdom of acting on a
disposition (JPS §3.5). It evaluates only a pack declaring `specVersion` `0.2.0-draft` (§11).

## The prompts

The server also serves three **method prompts** (MCP `prompts` capability) — static, versioned,
non-normative guidance that your client's model executes with your key. In Claude Code they appear
as slash commands (`/mcp__judgment-pack__author_pack`, `…test_pack`, `…fix_pack`); other clients
surface them differently or not at all, and everything works without them.

| Prompt | Guides |
| --- | --- |
| `author_pack` | Encoding one policy decision: the create → validate → evaluate loop, the resolution-model shapes that avoid conflicts, the decimal-string and `onUnknown` rules, the prepared-facts ledger |
| `test_pack` | Probing a pack's logic with an instance matrix (per-outcome, conflict, unknown, missing-evidence, not-applicable, forced-outcome, ordered-comparison rows) |
| `fix_pack` | Repairing a non-conformant pack from the validator's diagnostics, in carrier → structural → semantic order |

Following a prompt does not make a pack conformant — only validation decides that — and the
documents you produce are yours; the runtime stores and interprets nothing (ADR-0008).

## Claude Code

One command, from any directory:

```bash
claude mcp add --scope user judgment-pack -- judgment-pack mcp
```

Or per project, in `.mcp.json` at the project root (gitignored here):

```json
{
  "mcpServers": {
    "judgment-pack": {
      "type": "stdio",
      "command": "judgment-pack",
      "args": ["mcp"]
    }
  }
}
```

Restart the session, approve the server when prompted, and confirm with `/mcp`.

## OpenAI Codex CLI

In `~/.codex/config.toml`:

```toml
[mcp_servers.judgment-pack]
command = "judgment-pack"
args = ["mcp"]
```

Or equivalently: `codex mcp add judgment-pack -- judgment-pack mcp`. Start `codex` and confirm
with `/mcp`. All tools are read-only except the experimental evaluator, so an automatic approval
mode is reasonable.

## GitHub Copilot (VS Code)

In `.vscode/mcp.json` at the workspace root (gitignored here) — note the root key is `servers`,
not `mcpServers`:

```json
{
  "servers": {
    "judgment-pack": {
      "type": "stdio",
      "command": "judgment-pack",
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

- **Server fails to start:** run `judgment-pack mcp` in a terminal yourself; if it starts and
  waits on stdin, the binary is fine and the problem is the client's command path or config file.
- **Config edits not picked up:** clients read MCP configuration at session start — restart the
  session.
- **Old binary:** `judgment-pack version` must report a release that has the `mcp` command
  (v0.1.0 or later); a stale build on the PATH is easy to pick up by mistake.

The end-to-end authoring loop these tools support — create, read, update, delete, with the
runtime as a stateless oracle — is described in [authoring-lifecycle.md](authoring-lifecycle.md),
and the agent-driven testing protocol in [agent-testing.md](agent-testing.md).
