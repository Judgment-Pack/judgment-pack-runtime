---
status: accepted
date: 2026-09-14
deciders: maintainer
---

# Profile a matrix against its history — agreement by origin, values at each threshold, rows that cite receipts — and never gate on it

## Context and problem statement

A pack drafted from policy documents is tested against the decisions that were actually made:
each past decision becomes a matrix row whose facts are what was on file and whose expectation is
what was recorded, never what the draft produces. `packs test` already proves agreement row by
row and reports what the rows fail to probe ([ADR-0014](0014-pack-matrix-coverage-report.md)), and
a row already names where it came from (`origin`, counted per pack since
[ADR-0024](0024-derive-candidate-test-inputs-from-a-pack.md)). Three things a reader of that report
cannot get from it today. Which *history* disagrees — rows from one source may disagree where
rows from another do not, and a count of rows per origin says nothing about agreement. Where the
disagreements sit against the pack's own thresholds — a rule that compares a value against a
literal draws a line, and how many past cases sit on each side of it, and how many of those
disagree, is the one question a policy owner asks before moving the line. And what a row rests
on — a row transcribed from a page the gateway receipted can name that receipt, as a decision
record names the receipts it relied on ([ADR-0033](0033-a-record-cites-the-receipts-it-relied-on.md)),
so that a disagreement is traced from the receipt to the rule it questions; today a row has
nowhere to say so.

## Decision drivers

- The runtime reports; people decide. A disagreement between a draft and history is one of three
  things — a defect in the draft, an inconsistent past decision, or a policy that changed in between
  — and only the policy owner can say which. Nothing here resolves one, weights one, or edits the
  pack to make history pass.
- Documents write the rules, past decisions test them, and the two never cross: a row's expectation
  is what was recorded. The report must make that provenance visible, never fold it away.
- Everything reported is a deterministic function of the pack document, the matrix document and
  the evaluation that `packs test` already ran — no second evaluation, no model, no reading of any
  receipt or store.
- The runtime verifies nothing about a citation (ADR-0033). It records what it was given.
- Additive output: `outputVersion` stays, and a project that declares no origin and cites nothing
  gets a report byte for byte what it gets today.

## Considered options

- Members on the `packs test` report, derived from the run it already made.
- A separate `packs profile` command that re-evaluates the matrix and reports the profile alone.
- A gate: fail the suite when history agreement falls below a stated rate.

## Decision outcome

Chosen option: members on the `packs test` report, because the profile is a reading of the same
rows, the same statuses and the same derived boundaries that run already produced, and a second
command would evaluate the same matrix again to say the same thing in another place. A gate is
refused for ADR-0014's reason, sharpened: a threshold on agreement would be a number nobody asked
for, and the policy owner's ruling on each disagreement is the only thing that settles one.

Each pack entry of `packs test` gains one optional member, `profile`, present when at least one of
the pack's rows declares an `origin`, with:

- `agreement`: for each origin, in sorted order, the rows that declare it and how many passed
  and how many mismatched — the two statuses the run assigns a row, grouped. A row that expects
  a refusal (`expectedErrorClass`) passes when the evaluator refuses as expected and mismatches
  otherwise, and is counted by that status like any row; a pack whose matrix does not load runs
  no row and profiles nothing. A row with no origin is not in any group and not in the profile:
  absence of the marker is not a claim.
- `coverage`: for each origin, how many of the derived probes (ADR-0014, ADR-0023) some row of
  that origin witnesses, out of the probes there are — the same derivation, restricted to that
  origin's rows. Coverage over history is what this reads: which of the pack's reachable behaviors
  the past ever exercised.
- `thresholds`: for each comparison boundary the pack draws (a fact pointer against a decimal
  literal, the groups ADR-0023 derives), and for each origin: how many of that origin's rows place
  the fact below, at and above the literal, how many of each disagree, and the nearest value on
  each side — the value closest to the literal below it and above it, and the closest disagreeing
  value on each side, spelled as the row wrote them. "Near" needs no window: the nearest values are the ones
  a policy owner would ask about, and the counts say how many sit with them. A row whose fact is
  absent or not comparable at that pointer (§7.4's comparison decides) is counted as neither.

Each matrix row may carry `cites`: an array of `{sessionId, callIndex, signature}` in the gateway's
citation shape, held to the rules ADR-0033 fixed for a decision record's member — the flat-token
session, the integer index, the 128-hex signature, exactly the three members, each once — and
carried on the row's entry in the report as the values it declared, in the citation's own shape,
which is what the gateway resolves; the row's spelling of them is not a claim and is not kept. An
empty array cites nothing and carries no member. `matrixVersion` moves to `"3"` for it, the
closed-input rule of VERSIONING.md, as `"2"` did for `expectedHandoffTarget`. The runtime resolves
no citation; the gateway's `verify` is what reads one.

A `replay_history` MCP prompt states the method, non-normatively as every prompt does (ADR-0008):
draft from the documents alone; transcribe past decisions to rows mechanically, `origin` naming the
source and `cites` naming the page receipt each record came under; hold out a slice until the draft
is frozen; read the profile as questions for the policy owner — each disagreement is a pack defect,
a past inconsistency or a policy change, and only they can say which; each threshold's nearest
disagreeing cases are a question about the line, never an instruction to move it; and never edit
the draft to make history pass.

### Consequences

- Good, because a disagreement is traced in one report from its receipt, through its origin, to
  the boundary it sits against, with nothing decided on the reader's behalf.
- Good, because the profile costs no second evaluation and changes no status: a suite passes or
  fails exactly as it did.
- Bad, because a profile is a product — rows against boundaries, and rows against the pack's
  probes — of inputs each bounded only by its document's size, so its work is counted before
  anything is witnessed or placed (per row with an origin: its agreement, a witnessing of every
  coverage probe, a placement per boundary) and a profile beyond `MaxProfileWork` refuses the
  run as one that does not fit (`JPS-RESOURCE-MATRIX-PROFILE`, the handoff-target budget's
  precedent), rather than truncated: a profile cut short reads exactly like a complete one. The
  pack itself is read once for the suite's coverage and every origin's alike, its probes are
  rendered once for the suite and never for an origin, whose coverage is a count of predicates
  (its boundary witnessing compares through the evaluator's own comparison, charged per row);
  within the threshold placement the literal is read once per boundary and each fact once per
  row per boundary, the nearest value on a side is kept as the number it was read into beside
  its spelling, so no retained spelling is read again, and each origin's spelling is rendered
  once and reused by every entry that names it.
- Bad, because a matrix that cites receipts declares `matrixVersion "3"` and is refused by an older
  runtime with the version it would take — the closed-input rule's price, paid once.
- Bad, because "nearest value" is a reading of the rows' own facts, and a row transcribed wrongly
  profiles wrongly; the profile is only as good as the transcription, which is the method prompt's
  point and not the report's.
- Revisit when the graph matrix wants the same profile per node, or when a page receipt's
  `pageItems` gives a row a digest to cite as well as a session and index.

## More information

The lineage plan's phase 3 ("drafting from documents, testing against history"); ADR-0014 and
ADR-0023 (the derived coverage and the boundary probes this reads); ADR-0024 (the candidate inputs
and the `origin` marker); ADR-0033 (the citation shape and its rules); ADR-0008 (prompts are
method guidance and nothing more).
