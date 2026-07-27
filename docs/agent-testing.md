# Exercising the runtime with an agent

This runtime validates Judgment Pack Specification (JPS) documents; it does not author them. A
practical way to test both the specification's *authorability* and this runtime's *diagnostics* is
to let an LLM agent author packs with the validator as its only feedback, then report where the
diagnostics helped and where they did not.

This is a manual, exploratory protocol, distinct from the automated contributor checks in
[CONTRIBUTING.md](../CONTRIBUTING.md) and the bundled `spec test-conformance` suite. It complements
the specification's schema-validator protocol (`judgment-pack-spec`'s `TESTING.md`), which drives a
generic Draft 2020-12 validator rather than an agent. Why the runtime, rather than a bespoke UI, is
the integration and testing surface is recorded in
[ADR-0003](adr/0003-mcp-integration-and-testing-surface.md).

The agent drives the command-line binary directly below. The runtime also ships an MCP server
(`judgment-pack mcp`, see ADR-0003) exposing the same operations as tools to any MCP client; the
CLI loop here needs neither an SDK nor a running server.

## What you need

- A `judgment-pack` binary (installed or built below).
- Any agent client that can run a shell command and read and write files, configured with **your
  own** model API key. The runtime holds no key and opens no network connection — the key lives
  entirely in the client. (Its only evaluation surface is the explicitly experimental,
  non-conformance-claiming evaluator of ADR-0007.) Any of the many such clients works — for example Claude
  Code, Cline, Cursor, or Continue.

## Get the validator

Either install a released binary or build from source.

**Install a tagged release.** Download the archive for your operating system and architecture from
the repository's [GitHub Releases](https://github.com/Judgment-Pack/judgment-pack-runtime/releases)
and put the binary on your `PATH`. Each archive also ships a `jpack` short alias.

```bash
tar -xzf judgment-pack_<version>_<os>_<arch>.tar.gz   # on Windows, expand the .zip instead
install -m 0755 judgment-pack "$HOME/.local/bin/judgment-pack"
judgment-pack version
```

**Build from source.** With a currently supported Go toolchain, from a checkout of this repository.
If `go env GO111MODULE` reports `off`, force module mode as shown:

```bash
env GO111MODULE=on go build -o ~/.local/bin/judgment-pack ./cmd/judgment-pack
judgment-pack version
```

## How the loop works

The agent edits a candidate document and validates it after every change:

```bash
judgment-pack spec validate <path-or-> --format json
```

- `status` is `valid`, `invalid`, or `unsupported`; the loop stops at `valid`.
- The process exit code is `0` for a valid document and non-zero otherwise.
- `layers[]` reports `carrier`, `structural`, and `semantic`; a later layer runs only after the
  earlier ones pass.
- `diagnostics[]` carries one entry per problem, each naming a `code`, its `layer`, a `severity`, an
  `instancePath` (a JSON Pointer to the exact location), and a `message`:

```json
{
  "code": "JPS-SEMANTIC-UNRESOLVED-OUTCOME",
  "codeStability": "provisional",
  "layer": "semantic",
  "severity": "error",
  "instancePath": "/rules/0/outcome",
  "message": "Outcome reference does not resolve."
}
```

The agent fixes each diagnostic at its `instancePath` and re-runs. `--format human` prints the same
result as a scannable summary; `spec schema 0.1.0-draft --write -` prints the exact bundled schema;
`--through carrier|structural|semantic` stops after a chosen layer.

## Brief the agent

Give the agent these instructions however your client accepts project instructions -- a rules file
(for example Cline's `.clinerules` or Cursor's `.cursorrules`), the client's custom-instructions
setting, or simply the agent's first message. Replace `<spec>` with the path to a
`judgment-pack-spec` checkout.

```markdown
# JPS pack authoring -- validation loop

You author and revise Judgment Pack Specification (JPS) documents. The judgment-pack CLI is your
validation oracle. It checks document conformance only -- carrier (bytes/JSON), structural (schema),
and semantic (references) -- for spec version 0.1.0-draft. It does not evaluate rules, choose an
outcome, fetch sources, or judge whether a pack is correct, authorized, safe, or fit. Structural
validity means well-formed, nothing more.

## The loop
After every edit, validate and fix what it reports:

    judgment-pack spec validate <path-or-> --format json

- status: "valid" | "invalid" | "unsupported". Stop at "valid".
- exit code: 0 = valid, non-zero = not valid.
- diagnostics[]: each has code, layer (carrier|structural|semantic), severity, instancePath (a JSON
  Pointer), and message. Fix by instancePath and re-run.
- layers[]: a later layer runs only after earlier layers pass.

To see the full schema:  judgment-pack spec schema 0.1.0-draft --write -

## Where to start
Read one example first to learn the shape, then build your own -- do not copy it verbatim:
- <spec>/examples/minimal-expense-approval.json   (smallest)
- <spec>/examples/supplier-invoice-approval.json

## Guardrails
- Work on a scratch copy. Never modify the checked-in examples.
- Never put real, personal, confidential, regulated, or production data in a pack. Invented,
  low-risk data only.
- Never add or fetch a real URL in a source locator. Validation stays offline.

## Report as you go
The goal is to judge whether these diagnostics are enough to author by. Call out every diagnostic
that was unclear, misleading, wrongly located, or missing when you expected one. That list is the
deliverable.
```

## Tasks

Give the agent one or more of these, in rough order of difficulty:

1. **Create** -- author a new pack for an everyday approval scenario (travel reimbursement, time-off,
   ...), seeded by reading the minimal example. Iterate to `status:valid`.
2. **Update, then break a reference** -- add a rule or exception to a scratch copy of a larger
   example and keep it valid; then point that rule at an outcome id that exists nowhere, and confirm
   the semantic layer reports the dangling reference.
3. **Delete** -- remove an outcome or rule and its now-orphaned references; confirm the diagnostics.
4. **Adversarial** -- break a valid pack three structural ways and predict each diagnostic's
   `instancePath` before running.
5. **Report** -- list which diagnostics were clear, which confused, and any case where an error was
   expected but none appeared.

## Guardrails

The same rules the agent is given apply to whoever runs the protocol: work on scratch copies, never
edit the checked-in examples, use only invented low-risk data, and keep validation offline -- never
add or fetch a real URL. This mirrors the data-handling warning in `judgment-pack-spec`'s
`TESTING.md`.

## Reading the result

The deliverable is the agent's list of diagnostics that confused it, were mislocated, or were missing
when it expected one. Judge each diagnostic by whether a first-time author could act on it *without*
reading the schema or an example:

- A strong diagnostic names the exact location and the specific problem -- for instance, distinct
  semantic codes per reference kind (`JPS-SEMANTIC-UNRESOLVED-OUTCOME`,
  `JPS-SEMANTIC-UNRESOLVED-EVIDENCE`), each pointing at the offending array element.
- A weak diagnostic states only that something is wrong -- for instance, a structural type error that
  does not name the expected type -- leaving the author to guess or to copy an example.

Findings of the second kind are the point: they are the backlog for improving the runtime's
diagnostics or the specification's clarity.

## See also

- `judgment-pack-spec`'s `TESTING.md` -- the schema-validator protocol for a generic Draft 2020-12
  validator.
- [ADR-0003](adr/0003-mcp-integration-and-testing-surface.md) -- why MCP is the integration and
  testing surface.
- [CONTRIBUTING.md](../CONTRIBUTING.md) -- the automated checks expected before a pull request.
