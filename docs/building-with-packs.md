# Building with packs

This is the builder's guide: how to put judgment packs into a real application, keep them under
review, and wire an agent to them. It is the companion to
[authoring-lifecycle.md](authoring-lifecycle.md), which covers writing one pack; this covers owning
several of them in a project over time.

Everything here rests on one file, `jpack.json`, and on
[ADR-0012](adr/0012-jpack-project-convention.md), which decides what it is and — at least as
importantly — what it is not. **It is a convention of this runtime, not part of the Judgment Pack
Specification.** No other implementation is obliged to understand it, nothing in it changes what a
pack means, and a project that never writes one loses no capability: every command still takes a
pack by path, and every MCP tool still takes one as text.

What it buys is a name. Without it, "the expense approval decision" is a path in a shell script, a
different path in a CI job, and a blob of text a model was handed. With it, all three say
`expense-approval`.

## The shape of the file

```json
{
  "configVersion": "1",
  "packs": {
    "expense-approval": {
      "path": "packs/expense-approval-1.2.0.pack.json",
      "matrix": "packs/expense-approval.matrix.json",
      "description": "May this expense be reimbursed without a manager's sign-off?",
      "expectedVersion": "1.2.0",
      "facts": {
        "/expense/amountUsd": {
          "source": "Snowflake FINANCE.EXPENSES",
          "hint": "amount_usd, formatted as a decimal string — a JSON number compares as unknown"
        },
        "/employee/costCentre": {
          "source": "Databricks hr.dim_employee",
          "hint": "cost_centre for the requester's employee_id, as of the expense date"
        }
      },
      "evidence": {
        "itemised-receipt": {
          "source": "SharePoint /Finance/Receipts",
          "hint": "one PDF per expense id; absent is a real answer, do not substitute the summary"
        }
      }
    }
  }
}
```

`jpack packs schema` prints the schema this is held to, and it is **closed**: every member
it does not name is rejected, so a misspelled key is an error rather than an intention silently
dropped. `configVersion` is a single integer as a string — `"1"` is the only value this runtime
accepts, and a later shape will be `"2"`. There is no minor or patch component, because there is
nothing to negotiate: a program either knows the shape or does not.

Three things the file deliberately does **not** have:

- **No templating.** A pack assembled from variables at load time was never the pack anyone
  reviewed, and the reviewed artifact is the whole point of encoding a decision as a document.
- **No targets and no environments.** One file per environment, by convention:
  `jpack.json`, `jpack.staging.json`, selected with `--config`. Target blocks buy per-environment
  variation at the cost of a second place a pack's identity is decided.
- **No selection.** The file lists what a project can decide. It does not choose a pack for a
  request. That is the application's, for the reason the next section gives.

## The three owners

A decision that reaches an outcome has passed through three parties, and each owns exactly one
thing. Most trouble in practice comes from one of them quietly doing another's job.

### 1. The application selects

The application decides *which decision is being asked*. Not the runtime, not the model, and not the
pack: a pack that could nominate itself would be deciding whether it is authorized to decide, and
applicability is not authorization. A pack's top-level `applicability` says whether the pack applies
to the facts it was handed; it says nothing about whether it was the right pack to hand them to.

So selection is a line of your code, or a route, or a configuration table — and `jpack.json` is
where the ids that line uses are written down.

### 2. The agent gathers, and never invents

Everything a pack reads arrives as one facts document plus a tri-state evidence-availability
document. Someone has to produce them. When that someone is an agent, the `facts` and `evidence`
hints are what tells it where to look.

The hints are **non-normative guidance in your own words**. This runtime carries them and never
acts on one: it holds no credential, opens no network connection, and does not know what Snowflake
is. Because nothing else ever resolves a hint key, `packs validate` checks it against the pack
document — an `evidence` key must be a declared evidence-requirement id, and a `facts` pointer must
be one some condition reads (or an ancestor of one). A misspelled key is a failed check rather than
an instruction an agent follows. Reading the source is the agent's job, with the agent's own access
([ADR-0004](adr/0004-decline-http-api.md), [ADR-0006](adr/0006-authoring-lifecycle-in-the-client.md)).

The rule that makes the whole arrangement safe is one sentence:

> **A fact the agent cannot source is reported unknown. It is never guessed, inferred, or defaulted.**

That is not a nicety. The resolution model is built to handle `unknown` well — a rule with
`onUnknown: escalate` stops the decision and hands it to a human, and a fallback outcome is blocked
by an escalating unknown. All of that machinery is worthless if the gathering step fills the hole
with a plausible value first. An invented fact turns an escalation into an outcome, and the trace
will show a clean decision that nobody made.

Say so in the prompt, in the words of the pack: *if the Snowflake query returns no row, omit the
pointer; do not use zero.*

The evidence document is the same discipline in tri-state form: `present`, `absent`, or `unknown`.
`absent` means you looked and it is not there. `unknown` means you could not tell. They are
different answers and the pack may treat them differently.

### 3. The pack judges

Given facts and evidence, the pack alone decides. That includes deciding it does not apply: a
`not-applicable` result is the misrouting net. If the application selected the wrong pack — an
expense pack handed a procurement request — a well-written `applicability` catches it and returns
`not-applicable` instead of an outcome computed from facts that mean something else. Treat
`not-applicable` in production as a selection bug to investigate, not as a decline.

## The lifecycle: packs as code

A pack is source. It belongs in the repository beside the code that calls it, and it moves through
the same gate everything else does.

### Author

Use the `author_pack` prompt (in Claude Code, `/mcp__jpack__author_pack`). It walks the
scoping, the resolution-model shapes that avoid conflicts, the decimal-string and `onUnknown` rules,
and the prepared-facts ledger. Validate to `valid`; the diagnostics are self-sufficient.

### Build the matrix

Use the `test_pack` prompt. It builds the instance matrix: one row per declared outcome, a conflict
probe, an unknown probe per escalating rule, a missing-evidence probe, a not-applicable probe, a
forced-outcome probe per exception, and an ordered-comparison probe. Write those rows to the
`matrix` file, and the matrix becomes the pack's regression suite.

A matrix document is a `cases` array of rows in the **same case-carrier shape the bundled evaluation
corpus uses**, which is deliberate: your rows are compared the same way its rows are, and a row you
write is a corpus row once you name its pack fixture — the one member a corpus row adds, because a
project matrix names its pack in `jpack.json` instead.

```json
{
  "matrixVersion": "1",
  "cases": [
    {
      "id": "over-limit-needs-signoff",
      "facts": { "expense": { "amountUsd": "250.00" }, "employee": { "costCentre": "R&D" } },
      "evidenceAvailability": { "itemised-receipt": "present" },
      "expectedDisposition": {
        "kind": "outcome",
        "outcomeId": "requires-manager-signoff",
        "reasons": [],
        "handoff": { "state": "none" }
      }
    },
    {
      "id": "receipt-unknown-escalates",
      "facts": { "expense": { "amountUsd": "250.00" } },
      "expectedDisposition": {
        "kind": "unresolved",
        "reasons": ["unknown"],
        "handoff": { "state": "requested", "triggeredBy": ["unknown"] }
      }
    },
    {
      "id": "undeclared-evidence-key-is-refused",
      "facts": { "expense": { "amountUsd": "10.00" } },
      "evidenceAvailability": { "not-a-requirement": "present" },
      "expectedErrorClass": "malformed-input",
      "expectedErrorPhase": "preflight"
    }
  ]
}
```

Per row: `id` (unique, named so a mismatch can be pointed at), `facts` (required), optional
`evidenceAvailability` and `supportedExtensions`, and **exactly one** of `expectedDisposition` and
`expectedErrorClass` — a disposition and an evaluation error are never both produced, so a row
expecting both is refused. `expectedErrorPhase` is optional beside a class. `matrixVersion` is
optional and, when present, must be `"1"`. Unknown members are rejected: a misspelled
`expectedDispositon` has to be an error, not a row that silently expects nothing.

A row with a disposition passes when the disposition produced canonicalizes, under RFC 8785, to the
same bytes as the row's. A row with a class passes when the evaluation is refused with that class,
and with that phase when the row names one.

### Review

**Approval is the pull request.** There is no approval state in the file, no `approved: true`, and
no workflow in this runtime. A pack merged to your default branch is the approved pack because your
branch protection says so — the review, the reviewers, and the audit trail are your version
control's, which already does this properly and is already what your auditor asks for. A boolean in
a JSON file that its own author flips is a decoration.

### Gate it in CI

```bash
jpack packs validate && jpack packs test
```

`packs validate` checks the configuration and, per pack, six named steps: the declared path
resolves inside the configuration's directory; the document validates through the semantic layer;
an `expectedVersion` pin equals the document's `version`; the filename, when it follows the
convention, agrees with both; every hint key names something the document has; and a declared matrix
loads as rows. `packs test` then runs every row. Both exit `1` on any failure. Every check is
reported with its status — `passed`, `failed`, or `skipped` — so you can tell a check that passed
from one the configuration never asked for.

A pack that declares no matrix is reported **skipped**, never passed — and a run in which no row ran
at all is reported `skipped` and exits `1`, so a project with no matrices anywhere cannot get a green
gate for a suite that tested nothing.


To reach one decision from a shell without a path, name it:

```bash
jpack experimental evaluate --pack-id expense-approval --facts facts.json
```

`--pack-id` resolves through the same `jpack.json` (honoring `--config` and `JPACK_CONFIG`) and is
mutually exclusive with the pack argument — one pack, one source.

### Ship

Nothing to deploy: the packs are files in your repository and your application reads them. When you
change one, bump its `version`, and read the next two sections.

## `expectedVersion`: a reference, never a truth

A pack's identity is stated in exactly one place — the `id` and `version` members of the pack
document. Everything else that names a version is a **validated reference** to that statement:

| Where | What it is |
| --- | --- |
| `expectedVersion` in `jpack.json` | a pin the project asserts; `packs validate` compares it and reports a difference as an error |
| `<decision-id>-<semver>.pack.json` | an optional filename; cross-checked when followed |
| `packId` / `packVersion` in an evaluation payload | an echo read off the document that was actually evaluated |

None of the three is ever preferred over the document, and none is a place you can change a
version. That is the whole discipline: a fact stated twice is a fact that can disagree with itself,
so the second statement is not allowed to be a statement at all — it is a check.

Use the pin when a change to a pack should be a deliberate, reviewed edit in two places:

1. Someone edits the pack and bumps `version` to `1.3.0`.
2. `packs validate` fails: the configuration still pins `1.2.0`.
3. The same pull request updates the pin, and the diff now shows both halves.

Leave `expectedVersion` out when you want the pack to move freely — a pack still under active
authoring, say. An entry with no pin reports that check as `skipped`, not as passed.

## The filename convention: optional, and binding when followed

If a pack file is named `<decision-id>-<semver>.pack.json`, `packs validate` holds it to that name:
the id must equal the configuration key, and the semver must equal the document's `version`. If it
is named anything else — `expense.json`, `packs/current.json` — the check is **skipped** and nothing
is wrong.

This exists for one failure: a version bump that edits the document and leaves the file called
`expense-approval-1.2.0.pack.json`, or a pack copied to start a new decision and never renamed. Both
leave a filename that quietly contradicts the document it holds, and a filename is what people read
in a diff.

Follow it or don't. Following it half-way is what the check catches.

## Wiring an agent

Launch the MCP server in the project root, or point `JPACK_CONFIG` at the configuration:

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

Three tools then matter:

- **`list_packs`** — the resolved inventory: decision id, the document's own id and version, the
  description, the evidence-requirement ids, whether a matrix exists, and the hints. It is
  token-cheap on purpose; a model can learn what a project can decide without fetching a single
  document. With no configuration it answers **empty, with an explanation of where the runtime
  looked** — not an error, because a project that does not use the convention is an ordinary
  project.
- **`get_pack { pack_id }`** — the full document, read-only, exactly the bytes on disk.
- **`experimental_evaluate { pack_id, facts, evidence }`** — evaluation by id instead of by pasted
  text. `pack` and `pack_id` are mutually exclusive: supplying both is refused rather than given a
  precedence rule nobody asked for.

A workable agent loop: `list_packs` → the application (not the model) names the decision id →
gather each hinted fact, reporting `unknown` for anything you could not source → `experimental_evaluate`
→ read the disposition, and hand a `requested` handoff to a human.

Every file the server reads goes through a reader bound to a handle held open on the configuration's
own directory. A configured path that leaves that directory is refused, and by two checks that catch
two different escapes: a lexical one at configuration time, and resolution against the held directory
at read time, which is what catches an escape through a symlinked component. Because the second is a
handle rather than a pathname, containment holds through the open itself — there is no moment between
"this path is inside the project" and "open it" for the directory structure to be rearranged
underneath the answer. The server stays read-only, keyless, and offline.

## When the data isn't good enough: another pack

A recurring question is where "do we have enough information to decide this?" lives. It is tempting
to put it in the gathering step as a threshold, or in the pack as a rule about its own inputs.

Both are worse than the obvious answer: **it is another decision, so it is another pack.** A
data-sufficiency pack takes facts about the gathering — which sources answered, how stale each one
is, whether the requester's own attestation is the only evidence — and returns an outcome:
`sufficient`, `insufficient-escalate`, `insufficient-refuse`. Its facts are the metadata of your
gathering step, which your agent already has.

Your application then runs it first and only calls the substantive pack when it says `sufficient`.
That is composition by the application, which is where composition lives today; the specification's
own graph work (RFC 0002) is where composition may eventually be described. Two sequential calls
from your code are perfectly adequate — and when you want the wiring itself declared, reviewed,
and versioned instead of coded, `jpack experimental graph evaluate` (ADR-0015) runs both decisions
from a graph document: nodes reference your configured decision ids, and an edge feeds the
sufficiency decision's outcome into the substantive pack's inputs, where that pack's own rules and
`onUnknown` declarations decide what an insufficient or unresolved gathering means. Note what the
graph deliberately does not do: every node always evaluates, so it declares dataflow, not
conditional execution — skipping the substantive call remains your application's choice. The whole
surface is an experimental, non-normative prototype of that proposal, which may change or be
removed without compatibility promise.

The gain is that "enough to decide" becomes a reviewed, versioned, testable artifact with its own
matrix, instead of a threshold in a prompt.

## What this runtime still never does

- No store: your packs are yours, on your disk, in your version control.
- No credential and no network: hints are text; the runtime never reads a source.
- No selection: naming a pack is the application's.
- No approval workflow: your pull request is the approval.

## See also

- [ADR-0012 — the jpack.json project convention](adr/0012-jpack-project-convention.md)
- [ADR-0015 — the experimental graph surface](adr/0015-experimental-graph-surface.md) — declaring composition instead of coding it
- [authoring-lifecycle.md](authoring-lifecycle.md) — writing and repairing one pack
- [agent-testing.md](agent-testing.md) — the agent-driven testing protocol
- [mcp-clients.md](mcp-clients.md) — per-client setup
- [`CONFORMANCE.md`](../CONFORMANCE.md) — where the evaluator's conformance claim is stated, in full
  and only; nothing in this guide states any part of it
