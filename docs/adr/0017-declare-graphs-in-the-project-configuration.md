---
status: accepted
date: 2026-07-30
deciders: maintainer
---

# Declare graphs in the project configuration, and walk them like matrices

## Context and problem statement

Every layer of a project has a harness the CI gate can run without arguments — `packs validate`
walks every configured pack, `packs test` every declared matrix — except the graph. A graph's rows
(ADR-0016 reports their coverage; `graph test` runs them) attach only through the `--rows` flag on
a path-named invocation, so no project-level gate can run "all graph suites": the demo repo's drift
CI had to hardcode both paths, and a second graph means a second hardcoded step. The wiring is the
one artifact whose harness the project cannot declare.

ADR-0015 considered declaring graphs inside `jpack.json` and rejected it: "extending `jpack.json`
would couple a stable convention's configVersion to the least stable surface in the runtime." The
0.9.0 CHANGELOG restates it for the graph-test entry: "the stable `jpack.json` shape is
deliberately untouched by this experimental surface." This record revisits exactly that
determination — the *file* half of it — and reverses it knowingly; the *verb and exit-code* half of
ADR-0015's separation stands untouched: nothing graph-shaped enters `packs test` or its payloads,
and a stable command's CI gate still cannot fail on an experimental document.

## Decision drivers

- The declared-harness gap above: rows exist, coverage exists, and no gate can find them.
- ADR-0015's coupling objection is real and must be paid deliberately, not dismissed.
- A "1" configuration must keep loading on every runtime that ever read one, unchanged.
- One statement per rule: the version gate must live in exactly one artifact.

## Considered options

- **A. A `graphs` member under a new configVersion "2", walked by the experimental verbs** (chosen).
- **B. Status quo** — graph suites stay CLI-argument-only; every gate hardcodes paths.
- **C. Walk graphs from `packs test`** — rejected: that is the half of ADR-0015's determination
  worth keeping. An experimental document failing a stable command's gate, and removable members
  inside a stable payload, are exactly the coupling the original rejection named.
- **D. A separate graphs manifest file** — rejected: a second index file beside `jpack.json` is a
  second place a project's inventory lives, and the two would drift.
- **E. `graphs` as an optional member under configVersion "1"** — rejected on measured behavior: the
  closed schema makes an unknown member `JPS-PROJECT-CONFIG-SCHEMA`, exit `invalid` — and that one
  refusal takes down every packs command and all three MCP project tools, indistinguishable from a
  genuinely broken configuration. A version bump is refused as `JPS-PROJECT-CONFIG-VERSION`, exit
  `unsupported`, "It accepts: 1." — the designed, actionable path `declaredConfigVersion` exists to
  give. Opting in must cost the actionable refusal, not the broken-looking one.

## Decision outcome

Chosen option: **A**.

**The shape.** `configVersion` becomes `"2"`; this runtime reads both `"1"` and `"2"` (the refusal
message now says "It accepts: 1, 2."). A root `graphs` member — keyed by the project's own graph
name, each entry a closed `{path, rows?, description?}` — is gated to `"2"` by an `if/then` in the
schema itself, so the rule is stated once, in the bytes `packs schema` prints, and the diagnostic
for graphs-under-"1" names the exact member to change (`at '/configVersion': value must be '2'`;
note `schemaDetail` truncates at 400 characters, so a configuration with many other defects may
push that cause past the cut — the first defect shown is still actionable). The embedded schema's
`$id` becomes `urn:judgmentpack:runtime:jpack-config:2` — it names the newest shape the bytes
describe — and the `packs schema` payload gains `supportedConfigVersions` beside `configVersion`,
so the payload cannot imply "1" stopped being read. Deliberately no `expectedVersion` pin on a
graph entry: the graph format is experimental (ADR-0015), and a pin is one more member to remove
with it. A project that declares no graph never edits its configuration and keeps loading on every
older runtime; only opting in costs compatibility, and it costs it as `unsupported`.

**The walk.** `experimental graph test` and `experimental graph validate` with no argument walk the
configured graphs — `--id` selects one — each entry exactly the path-named form's run: the test
walk calls the same `Test` the single-graph verb calls, so rows and coverage (ADR-0016) ride
unchanged, and the validate walk applies the same document, rule, and reference checks plus the one
check only the configuration can ask for, that a declared rows path stays inside the project. An
entry whose graph or rows cannot be loaded is that entry's mismatch, never a refusal of the whole
walk. The path-named forms are byte-identical to before; the demo's existing CI step survives
unchanged.

**The exit-code determination, made here.** A test walk in which no row ran reports `skipped` and
exits 1 — `packs test` demotes to `skipped` at the status layer and its CLI already exits non-zero,
but for the graph walk both halves are this record's own statement: a green gate over nothing
tested is the failure mode a declared harness exists to prevent. The validate walk reports zero
declared graphs as `skipped` at exit 0, because a project without graphs has nothing here to be
wrong about — validation of nothing found nothing, which is an answer.

**What this supersedes, exactly.** The file half of ADR-0015's jpack.json determination;
ADR-0012's settled determination that `"1"` is the only accepted `configVersion` value (its
reasoning — an integer version so a program can say "I do not know this shape" — is exactly what
this record relies on, and the value list it settled is what changes); and the 0.9.0 CHANGELOG
sentence quoted above — reversed by this record, not edited: released CHANGELOG sections are dated
statements and stay as written. The verb half stands: graphs still live under
`experimental`, their payloads carry every non-normative label they carried, `GraphSuite` and
`GraphValidationSuite` are new payloads rather than members of `PackTest`/`PackValidation`, and
removing the graph surface removes the `graphs` member and configVersion "2"'s one addition with
it — a "2" without graphs is legal today, which is also the removal story.

### Consequences

- The runtime's own fixture project declares its graph and rows; the walk runs it at 7/13 coverage.
- Adopting `graphs` in a real project (the demo) requires configVersion "2", which a 0.9.0 reader
  refuses as unsupported — the demo bump is a deliberate, separate decision after the release that
  carries this record.
- `ReadGraph`/`ReadGraphRows` take the byte limit from the caller, unlike `ReadPack` — a deliberate
  divergence, because the graph limits live with the graph surface and restating them in the
  project package would be a second place for a number to drift.
- The conformance claim is untouched: the walk constructs the evaluator at no new site (the test
  verb's two forms share one constructor), so CONFORMANCE.md's six-surface enumeration and its
  guard test are unchanged.

## More information

ADR-0012 (the project convention), ADR-0015 (the surface, and the determination partially
superseded here), ADR-0016 (the coverage the walk carries). `internal/graph/suite.go` is the walk;
`internal/project/jpack.schema.json` is the one statement of the version gate.
