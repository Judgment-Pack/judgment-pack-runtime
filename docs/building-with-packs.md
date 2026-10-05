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
pack by path, and every MCP tool still takes one as text. It is also the only place this runtime can
be asked to *write a record of what it decided* — the audit trail of
[ADR-0018](adr/0018-opt-in-evaluation-audit-trail.md), described at the end of this guide — and a
configuration that does not ask leaves nothing recorded anywhere. (The runtime's only other write
is the `--write` flag on `spec schema`, `spec examples`, and `packs schema`, which copies bundled
bytes to a new file an operator names and has nothing to do with a project.)

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
dropped. `configVersion` is a single integer as a string — `"1"` is the shape without graphs, `"2"`
the shape with them (ADR-0017), `"3"` the shape that may also ask for an audit trail
(ADR-0018), `"4"` the shape that may also require the reviewed set (ADR-0044), `"5"` the shape
that may also require comparable facts (ADR-0046), and `"6"` the shape whose audit member may also
say whether its trail is chained (ADR-0047); this runtime reads all six. There is no minor or patch
component, because there is nothing to negotiate: a program either knows the shape or does not.

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
is. No hint is read, followed, or recorded — not even in the audit trail described below, which
records what was evaluated and never where a value was said to live. Because nothing else ever resolves a hint key, `packs validate` checks it against the pack
document — an `evidence` key must be a declared evidence-requirement id, and a `facts` pointer must
be one some condition reads (or an ancestor of one). A misspelled key is a failed check rather than
an instruction an agent follows. Reading the source is the agent's job, with the agent's own access
([ADR-0004](adr/0004-decline-http-api.md), [ADR-0006](adr/0006-authoring-lifecycle-in-the-client.md)).

The rule that makes the whole arrangement safe is one sentence:

> **A fact the agent cannot source is reported unknown. It is never guessed, inferred, or defaulted.**

That is not a nicety. The resolution model is built to handle `unknown` well — a rule with
`onUnknown: escalate` stops the decision and requests a handoff to the pack's escalation target (nothing
delivers it), and a fallback outcome is blocked
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

A matrix document is a `cases` array of rows that **share, with the bundled evaluation corpus, the
fields the comparator reads**. That is deliberate: your rows run through the same comparator its
rows do, so you get the byte comparison §8.3 defines rather than a looser one written for projects.

It is not a claim that a row moves between the two untouched, and this document used to overstate
it. Corpus admission requires members a project matrix has no place for — `pack`, plus `origin`,
`supportedExtensions`, `focus`, and `specSection`, all required there and optional or absent here —
and its schema closes the case object, so `expectedHandoffTarget` (below) is refused outright.
Lifting one of your rows into a corpus means supplying those members and removing any target
assertion and any citations (`cites`, below, which the closed schema refuses too). What you never
rewrite is the expectation, which is the half the shared comparator judges.

```json
{
  "matrixVersion": "2",
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
      },
      "expectedHandoffTarget": { "kind": "human-role", "name": "Finance approver" }
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
expecting both is refused. `expectedErrorPhase` is optional beside a class, and
`expectedHandoffTarget` is optional beside a disposition (below). Three further members
are optional and none of them decides anything: `origin`, a free string saying where the row's
*input* came from (`jpack packs suggest` writes `"generated"`, and `packs test` reports a count per
origin — see below); `focus`, one line saying what the row is probing; and `specSection`, the
section of the specification the row is about, which is what the bundled evaluation corpus uses the
member for. `matrixVersion` is optional and, when present, must be `"1"`, `"2"` or `"3"`; an omitted
version is read as `"1"`. Unknown members are rejected: a misspelled `expectedDispositon` has to be
an error, not a row that silently expects nothing — and so is a member spelled in another case, so
`Facts` and `ExpectedHandoffTarget` are refused rather than read as the members they resemble.

A row with a disposition passes when the disposition produced canonicalizes, under RFC 8785, to the
same bytes as the row's. A row with a class passes when the evaluation is refused with that class,
and with that phase when the row names one.

**`expectedHandoffTarget` is the one further assertion a row can make**, and it exists because §8.3
keeps your pack's configured escalation target *outside* the disposition — it is reported beside it,
as the payload's own `handoffTarget`. A pack edit that changes only `escalation.target.name`
therefore leaves every disposition byte-identical, and a matrix that compares only dispositions
stays green while your requests route to the wrong desk (ADR-0025). Declare the member and the row
compares it too:

- an **object** with `kind` and `name` — both required, neither empty — asserts that exact target;
- the literal **`null`** asserts that the evaluation reports **no** target, which is what an outcome
  that requests no handoff produces;
- **absent** asserts nothing, which is what every row written before this member existed does.

**It needs `matrixVersion: "2"`.** A matrix is a closed input — an older reader *rejects* a document
carrying a member it does not know rather than ignoring it — so adding a member moves the version,
and a matrix that declares `"1"` or declares nothing is refused by name if a row asserts a target.
The refusal tells you which version to declare.

It rides beside `expectedDisposition` only — a refused evaluation reports no target to compare, so a
row declaring it beside `expectedErrorClass` is refused when the matrix loads. It is an
**assertion**, not a coverage line: a mismatch fails the row, the pack, the run, and the exit code,
exactly as a disposition mismatch does, and the report carries `expectedHandoffTarget` and
`actualHandoffTarget` side by side so you can see which destination each names. A third value can
appear on the actual side: **`unavailable`**, printed as `unavailable (evaluation refused)`, which
means the evaluation was refused and reported nothing at all. That is deliberately not `null` —
`null` would say an evaluation ran and reported no target. Very long targets are reported truncated
with a digest tail, so a report stays a size you can read while two different targets still compare
as two.

What it holds is the target your pack *configures*: nothing here observes that a handoff was
delivered, that the named role or queue exists, or that anyone acted on it. It is the value
downstream integrations read to route the request, and this is what pins it.

The **graph** matrix has it too (`experimental graph test`, ADR-0032, `graphMatrixVersion "2"`):
`expectedHandoffTarget` asserts the composite's reported target — the result node's own — and
`expectedNodeHandoffTargets` asserts named nodes', each such node also named in `expectedNodes`. The
same semantics apply: decoded-value comparison, capped renderings in the report, and the target the
run reported — configured exactly when the disposition requested a handoff, null otherwise.

Beside the rows, `packs test` reports **coverage**: the probe classes your pack's own declarations
derive, and which of them some row states. There are two families.

The **disposition** family is one probe per producible declared outcome (one a rule, exception, or
fallback names), then `not-applicable`, `missing-required-evidence`, `unknown`, `conflict`,
`exception-escalation`, and `no-match`, each only where the pack makes it reachable, each witnessed
by what a row *expects*. It follows reachable behavior, not the `test_pack` prompt's list — the
prompt's forced-outcome probe cannot be witnessed by an expectation and is not derived, the prompt's
ordered-comparison *type* probe (a JSON number where a decimal string belongs) is deliberately still
not derived either, and `exception-escalation` and `no-match` are derived though the prompt's list
does not name them. The type probe stays a prompt question because the schema already pins an
ordered comparison's operand to a decimal string, so that mistake lives in how your facts are
*produced*, upstream of the pack; it is a named follow-up rather than a refusal.

The **boundary** family is `boundary:<pointer>:<literal>`: one probe per distinct fact pointer and
decimal value your conditions compare with `greater-than`, `greater-than-or-equal`, `less-than`, or
`less-than-or-equal`, witnessed by a row whose own *facts* place that pointer's value exactly at the
literal. A rule saying "5000 or more requires review" and comparing `greater-than "5000"` is two
individually valid members that disagree at exactly one input, and a suite with rows at 4999 and
5001 is green while saying nothing about it. Sites sharing a pointer and a value are one probe
however many operators compare them and however the decimal is spelled (`70` and `70.0` are one
boundary), and the missing sentence names the sites still unwitnessed — one entry per owning
declaration and operator, the first six of them — so you can open the pack at the right member.

The row also has to be one that could have reached the comparison, which §8's order decides.
`applicability` runs first, so any row whose expectation decodes exercised it. Every exception's
`when` is evaluated next, and missing required evidence does not stop that — §8 records it and halts
only after every exception has been inspected — so only a row expecting `not-applicable` fails to
witness an exception's boundary. A normal rule's `when` runs last, so a row whose expected reasons
include `missing-required-evidence` or `exception-escalation` does not witness a rule's boundary:
both prove the evaluation stopped before the rules. One threshold compared in two of those places
needs a row for each, because the row that stopped early genuinely never compared the other copy.
What it does not catch: a threshold written at the wrong
*number*, or a comparison reading the wrong pointer — both are questions about what the policy says,
not about which input distinguishes two encodings.

Coverage informs and never gates: a missing probe is a fact about what your rows state, not a failed
row. A covered matrix is one that probes every derivable behavior; whether the rows are *right* is
still yours to judge against the policy text (ADR-0014, ADR-0023).

### Let the pack suggest the inputs

Coverage names the gap; `jpack packs suggest` offers the inputs that would close it, derived from
the pack's own literals ([ADR-0024](adr/0024-suggest-candidate-row-inputs.md)):

```bash
jpack packs suggest --id expense --write candidates.json
```

What comes back is a `candidatesVersion`/`candidates` document, and every candidate carries an `id`,
`origin: "generated"`, a `facts` document, sometimes an `evidenceAvailability`, and a `rationale` —
a sentence saying what it places and why the pack implies it, closed by the sentence every candidate
ends with: no expectation is stated, write one from the policy text or delete this candidate.
**It carries no expectation, and that absence is
the whole point.** The expectation is the member that says what your pack *should* decide; deriving
one from the pack would only tell you what the pack already does. So the generator supplies the half
a machine can supply — the input — and leaves you the half only the policy text answers.

Refusals hold that line, and none of them is new code. Point a `jpack.json` `matrix` at a raw
candidate file and the matrix loader rejects the root members it does not know. Paste a candidate
into a `cases` array and you meet two more, in this order:

```
the matrix has a member this runtime does not know, or a member of the wrong type: ... unknown field "rationale"
row "suggest:expense:value:/expense/amount:5000" must declare exactly one of expectedDisposition and expectedErrorClass: ...
```

The first is what a **verbatim** paste hits: `rationale` is a member of no row, so the whole matrix
is refused before any row is examined — which is also the layer that keeps the generator's own prose
out of anything that could be scored. Delete the `rationale` and you meet the second, which is the
one that names the work still to do. Then you write the expectation. For an outcome disposition that
is `kind`, `outcomeId`, `reasons`, and `handoff`, authored from the policy text — those four are the
shape of *your* act, not a check the loader performs; what it enforces is the weaker "exactly one of
`expectedDisposition` and `expectedErrorClass`". And **deleting a candidate the policy does not
decide is a first-class outcome** — a candidate is an offer, not a demand.

Per pointer the values are: the compared literal itself; one unit either side of it *at the
precision the pack authored it in* (`"5000"` steps by 1 to 4999 and 5001, `"70.0"` steps by 0.1 to
69.9 and 70.1); the midpoints between adjacent literals, which are always themselves decimal strings
because 2 divides 10; and one unit outside the outermost literals. That is at most `4n+1` values for
a pointer compared against *n* distinct literals. One value spelled two ways is one literal (`70`
and `70.0` derive one lattice), and its step comes from the *finest* spelling your pack authored, so
reordering two rules changes neither the values you are offered nor the step between them. What it
can change is the *text*. The candidate at the literal is placed and named in the first-authored
spelling a reviewer can read — a spelling past the 128-byte budget is reported and skipped over for
this purpose, so which one is offered does not depend on declaration order either — and every
rationale sentence that quotes that authored literal — the at-literal
candidate's own, and each candidate stepped from it — follows the same spelling. Rationale
sentences also name the rules that own a comparison in *declaration* order, so reordering can
rewrite rationale text even where every spelling is identical. Both are strings only: no offered
value and no step moves
([ADR-0024](adr/0024-suggest-candidate-row-inputs.md) records that the group carries the
first-declared spelling, exactly as ADR-0023's probe does).

`--include-hugs` adds one more pair per literal, two decimal places finer than the authored
precision — 4999.99 and 5000.01 beside 4999 and 5001 — taking the bound to `6n+1`. It carries the
same `10^-6` floor the unit step does, so "two places finer" is exact only below five authored
digits: a literal authored at five digits is hugged one place finer instead of two, and one at six or
more has no finer pair to offer and gets none. Both narrowings are reported as skipped dimensions
(`clamped-hug`, `unavailable-hug`) and the clamped pair's rationale names the distance it actually
carries — a pair quietly delivered one place finer would read as the pair the flag names. It is
**off by default**, and the reason is evidence rather than taste: the corpus study behind
[ADR-0024](adr/0024-suggest-candidate-row-inputs.md) found authored test values already piled up on
the thresholds and within 0.01 of them, and almost nothing in the gap between. Authors already hug;
what authorship misses is the unit step at the precision the policy was written in. Pass the flag
when you want the hug mechanized anyway.

Each stated member of an `in`, `equals`, or `not-equals` operand gets a candidate too, and so does
the absence of a pointer, and the three tri-states of each declared evidence requirement.

**Composition is one factor or axis at a time.** A value or membership candidate varies exactly one
pointer and holds the rest at a base assignment; an evidence candidate varies no pointer at all and
moves the availability axis instead; and with no `--base`, the single absence candidate states no
facts at all, because there is nothing to hold the other pointers at. So the count grows with the
number of pointers and axes and never as their product. `--base <rowId>` makes that base an
already-reviewed row of your matrix, which is what makes a candidate read as "this reviewed row,
with one pointer moved to a value the pack's own literals imply". Candidates hold that row's
`evidenceAvailability` as well as its facts:

- a value, membership or absence candidate carries the row's document unchanged, or states none when
  the row states none;
- an evidence candidate is the row's document with the one requirement moved, or a document naming
  only that requirement when the row states none;
- a row whose document is not an object declines the evidence axis under `unmovable-base-evidence`,
  and the other candidates carry the document unchanged.

So a candidate no longer loses the evidence its row stated. Whether it then reaches the rule it was
derived to probe is up to the row: a row that marks a required requirement `absent` or `unknown`,
or a row the pack does not apply to, stops every candidate made from it where it stopped the row.
Without `--base`, the facts carry only the varied pointer. The generator never synthesizes a plausible-looking full record: that would be
it inventing a policy world.

Where your base row already *states* something at the pointer's path that the placement would have
to replace — a scalar to descend through, an explicit `null`, or an array position RFC 6901 does not
address — the candidate is declined and the reason is reported under `unplaceable-pointer`, never
forced: overwriting a stated answer would change the base beyond the one thing a candidate varies.

The report and the candidate document are two artifacts. `--format human|json` renders the *report*
about the run — the counts, the pack identity each candidate came from, and every skipped dimension
— while `--write` emits the document. `--write -` and `--format json` are **refused together**,
because one stream cannot carry two documents; and when `--write -` takes stdout for the document,
the report goes to **stderr**, so a piped stdout is exactly the document's bytes and the skipped
dimensions are still stated:

```bash
jpack packs suggest --id expense --write - > candidates.json   # report on stderr, document in the file
```

It decides nothing and gates nothing. It runs no evaluator, moves no exit code, and writes nothing
unless you pass `--write <file>` or `--write -`; a destination your configuration declares as a
pack, a matrix, a graph, or a `rows` document is refused by name — including when you reach it
through a symlinked spelling of your project directory, and including when your *configuration*
names that document through an alias of its own, because both ends are resolved before they are
compared. Past `--max` (500 by default) the run
refuses rather than truncating, because a truncated candidate set looks exactly like a complete one;
the cap is charged as candidates are composed, so a low `--max` stops the derivation there rather
than letting it run to the end and refusing after the fact. `--max 0` and a negative one are refused
outright rather than read as the default. There is a 16 MiB
bound on the emitted document as well, charged on the file as it would be *written*, which `--max`
cannot stand in for: every candidate carries a whole facts document, so a `--base` row that is wide
— or merely deeply nested, which the emitted document's indentation multiplies again — multiplies
by the candidate count, and crossing it refuses whole. The number that refusal names is a **bound**
rather than a measurement — each candidate is charged its written encoding plus a fixed envelope,
deliberately a little more than the framing costs — and the remedy that lets a run of this shape
finish is a narrower `--base` row. A pack using draft-RFC collection quantifiers has
that dimension reported as *skipped*, never silently left out. Two runs over an unchanged pack write
identical bytes.

**The honest limit.** A generator that makes coverage cheaper to reach, under unchanged review
discipline, makes cheaply-justified coverage. Nothing here stops someone running each candidate
through the evaluator and pasting the answer — that is ADR-0014's circular oracle, and it is exactly
as open as it always was. What this measures rather than prevents is how much of a suite the
machine supplied: `packs test` reports **origin counts** per pack, in both formats —

```
- expense [mismatch]: 13/14
  coverage: 5/5 derived probes are witnessed by a row
  origin generated: 12/14 row(s) declare it
```

— and a suite where 12 of 14 rows declare a generated origin is a suite worth a longer look. The
count never gates: `origin` is deletable in one edit, so a gate would teach the deletion and destroy
the only signal there is. If you find yourself writing an expectation for *every* candidate rather
than deleting some, that is the signature this design is watching for.

### Before you move a line, compare the versions

A revised rule changes answers, and `packs test` only finds the changes where a row with an
expectation already sits. To see every input the revision decides differently before you write any
new expectation, evaluate one set of inputs under both versions
([ADR-0045](adr/0045-compare-two-versions-of-a-pack.md)):

```bash
git show HEAD:packs/expense.json > expense-before.json
jpack experimental compare expense-before.json packs/expense.json --inputs candidates.json
```

The inputs are your matrix, whose expectations play no part in the comparison, or the candidates document
`packs suggest` wrote, which holds the inputs nearest each line the pack draws. The report lists the
inputs whose results differ, with both results and what changed, and counts the rest. A difference
says the two versions disagree; the policy text says which is right, and a difference is not an
expectation. The command opens no project, so it records nothing and consults no lock, and it exits
0 whenever it ran.

Beside those totals it counts the inputs that were `unresolved` under both versions
(`inputs.unresolvedUnderBoth` in the JSON payload), whether they differ or not: an input that
reaches no outcome under either version cannot show a change in which outcome it gets. When that is
every input, the human output says the comparison could not see a change in any outcome. Candidates
from plain `packs suggest` can do that to a pack that reads more than one fact: each states only the
pointer it varies, so a rule that reads another pointer is unknown, and where that rule escalates an
unknown every candidate is unresolved under both versions and a moved threshold reports `0 differ`.
Candidates written with `--base <rowId>` carry a reviewed row's other facts and its evidence, and
reach the rules the row reaches.

Two packs with different ids are compared too, since comparing two decisions may be what you meant,
but they are two decisions rather than two versions of one. When both ids were read and differ, the
human output says so on its first line, and the JSON payload carries `"differentDecisions": true`.
An id is read from an evaluation that succeeds, so when no input is evaluated (an empty candidates
document) or every evaluation under one pack is refused, there is neither the warning nor the
member, and their absence does not establish that the ids match.

### Review

**Approval is the pull request.** There is no approval state in the file, no `approved: true`, and
no workflow in this runtime. A pack merged to your default branch is the approved pack because your
branch protection says so — the review, the reviewers, and the audit trail are your version
control's, which already does this properly and is already what your auditor asks for. A boolean in
a JSON file that its own author flips is a decoration.

### Declare the reviewed set

One command, run whenever the law changes on purpose:

```bash
jpack packs lock
```

It writes `jpack.lock.json` beside your configuration: the digest of the configuration's exact bytes
and of every pack and graph it declares ([ADR-0019](adr/0019-reviewed-set-lock.md)). Matrices and
`rows` documents are not pinned — they are read by `packs validate`, `packs test`, and `graph test`,
none of which records or decides, so a fixture edit is not an amendment. Commit it with
the change it pins. Running it *is* the amendment — it is how the project says, in a file a reviewer
diffs, that this is the law now — and it approves nothing, exactly as the paragraph above says. The
file is generated and deterministic, so re-running it over an unchanged tree leaves no diff to read.

`jpack packs verify` is the other half, and it belongs in the CI line below: it names every
difference between the tree and the reviewed set — `config-drift`, `document-drift`,
`document-missing`, `lock-entry-missing` for a pack the configuration declares and the lock does
not, `locked-but-undeclared` for one the lock names and the configuration dropped, and
`path-mismatch` for an entry recorded at a path the configuration does not declare — and exits `1`
on any of them. It reports what changed and never whether the change was right: a runtime cannot tell
an amendment from tampering, and only the people reading the diff can.

The file's *presence* is the whole of the opt-in. No `configVersion` moves, nothing in `jpack.json`
points at it — a configuration that named its own lock could rename it — and a project without one
behaves exactly as it did before this existed.

### Gate it in CI

```bash
jpack packs validate && jpack packs lint && jpack packs test && jpack packs verify
```

`packs validate` checks the configuration and, per pack, six named steps: the declared path
resolves inside the configuration's directory; the document validates through the semantic layer;
an `expectedVersion` pin equals the document's `version`; the filename, when it follows the
convention, agrees with both; every hint key names something the document has; and a declared matrix
loads as rows. `packs test` then runs every row. Both exit `1` on any failure. Every check is
reported with its status — `passed`, `failed`, or `skipped` — so you can tell a check that passed
from one the configuration never asked for.

By default a pack that declares no matrix is reported **skipped**, never passed — and a run in which no
row ran at all is reported `skipped` and exits `1` unless a mismatch was found, so a project with no
matrices anywhere cannot get a green gate for a suite that tested nothing. A run where other packs' rows passed still exits `0` beside a
pack with no matrix. To make the gate fail on such a pack, run `packs test --require-matrix`
(`require_matrix: true` over MCP): the pack is then reported `mismatch` and the run exits `1`
(ADR-0042). The coverage report never moves the exit code: a green gate
with missing probes is a passing suite that has not probed everything, and the report says which.

`packs verify` is there so a pull request that changes a pack and forgets `packs lock` fails the
gate rather than merging an undeclared amendment. Drop it from the line if the project keeps no
lock; with no lock file it refuses with "there is nothing to verify against" rather than passing
silently, which is the honest answer and a red CI step either way.


To reach one decision from a shell without a path, name it:

```bash
jpack experimental evaluate --pack-id expense-approval --facts facts.json
```

`--pack-id` resolves through the same `jpack.json` (honoring `--config` and `JPACK_CONFIG`) and is
mutually exclusive with the pack argument — one pack, one source.

In a project that keeps a lock, that command is a **decision** and it is held to the reviewed set:
the configuration's bytes and the named pack's are checked before anything is evaluated, and a
mismatch refuses the run (exit `1`, `JPS-LOCK-VERIFY`) with the two honest ways forward — declare
the amendment, or restore the reviewed bytes. Naming a pack **by path** instead is a **draft**:
evaluated, never refused for being unlocked, because writing a pack and trying it is the whole of
authoring. The payload's `reviewed` member says which of the two a run was (ADR-0044). A project
that wants drafts refused on its deciding surfaces sets `"requireReviewed": true` under
configVersion `"4"`: every run that applies a draft, and every run while no lock exists, is then
refused (`JPS-LOCK-REVIEW-REQUIRED`), and the author's loop moves to `--rehearsal`, which records
nothing and consults no lock. It binds a caller that neither chooses the configuration a run reads
nor can edit it or its lock, not whoever chooses it; `SECURITY.md` states the boundary. `packs
test`, `experimental graph test`, and `experimental evaluate-corpus` consult the lock never — the
author's loop is free and only decisions are classified, which is the same split the audit trail
draws.

### Ship

Nothing to deploy: the packs are files in your repository and your application reads them. When you
change one, bump its `version`, and read the next two sections.

### Rows that cite receipts, and the history profile

A row transcribed from a past decision can say where it came from and what it rests on
(ADR-0034). `origin` names the history — a system, a table, a period — and `cites` names the
gateway receipts the row's facts were transcribed under, in the gateway's own citation shape:

```json
{"id": "case-2026-0117", "origin": "warehouse", "cites": [{"sessionId": "s-2026-09-a", "callIndex": 17, "signature": "<128 hex>"}], "facts": {"...": "..."}, "expectedDisposition": {"...": "..."}}
```

**`cites` needs `matrixVersion: "3"`**, by the same closed-input rule. It is held to the grammar a
decision record's citations are held to (ADR-0033) when the matrix loads — an array of objects with
exactly `sessionId`, `callIndex` and `signature`, each once — and carried on the row's entry in the
`packs test` report as the values you declared, in that shape; an empty array cites nothing and
carries no member. The runtime verifies nothing about a citation; the gateway's `verify` is what
resolves one.

When any row declares an `origin`, the pack's `packs test` entry carries a `profile`: `agreement`
(rows, passed, mismatched, per origin), `coverage` (how many derived probes that origin's rows
witness, out of the probes there are), and `thresholds` — for each comparison boundary the pack
draws, where each origin's rows place the compared fact (below, at, above the literal, by the
evaluator's own comparison), how many of those disagree, and the nearest value on each side with
the nearest disagreeing one, spelled as the row wrote it. The profile reads the run already made
and moves no status: a disagreement is a pack defect, a past inconsistency or a policy change,
and only the policy owner can say which. The `replay_history` MCP prompt states the method.

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

Three tools then matter for packs (and where a project configures graphs, the same pair exists
one surface over: `experimental_list_graphs` for the inventory and `experimental_get_graph` for
one document, ADR-0029):

- **`list_packs`** — the resolved inventory: decision id, the document's own id and version, the
  description, the evidence-requirement ids, the fact pointers the pack's conditions read
  (`consultedFactPaths`), whether a matrix exists, and the hints. It is
  token-cheap on purpose; a model can learn what a project can decide without fetching a single
  document. `consultedFactPaths` is the list to intersect when an unresolved disposition should
  name the candidate pointers it may be waiting on, or when a build wants to check that every
  consulted pointer has a producer — the check `jpack packs lint` now performs against the
  configuration's own hints, or against an explicit `--producers` manifest when an application
  produces more than it hints (ADR-0022). The list reports what the document carries, not a
  verdict on it, and it over-approximates by design, so its values are untrusted document content
  rather than proof of a read. With no configuration it answers **empty, with an explanation of where the runtime
  looked** — not an error, because a project that does not use the convention is an ordinary
  project.
- **`get_pack { pack_id }`** — the full document as text, read-only: exactly the bytes on disk
  when they are valid UTF-8, and a refusal naming the configured path when they are not, because
  a text result carries nothing else losslessly.
- **`experimental_evaluate { pack_id, facts, evidence }`** — evaluation by id instead of by pasted
  text. `pack` and `pack_id` are mutually exclusive: supplying both is refused rather than given a
  precedence rule nobody asked for. It is the one tool that can write: in a project whose
  `jpack.json` declares an `audit` directory it appends one record per completed call, and in a
  project that declares none it writes nothing. A call declaring `"rehearsal": true` (the CLI's
  `--rehearsal`) writes nothing even there and consults no reviewed set — a rehearsal is not a
  decision, the standing a matrix row already has (ADR-0021, extended by ADR-0028) — and its
  payload carries `"rehearsal": true`, stating in band that this was not a decision — a
  consumer that reads the member cannot mistake a rehearsal for one. Use it for what-if exploration: edit the facts, re-evaluate, compare — and leave the
  trail exactly as you found it.

A workable agent loop: `list_packs` → the application (not the model) names the decision id →
gather each hinted fact, reporting `unknown` for anything you could not source → `experimental_evaluate`
→ read the disposition, and hand a `requested` handoff to a human. When authoring rather than
deciding, `experimental_test_packs` closes the loop the `test_pack` prompt teaches: it runs the
declared matrix through the same comparison `jpack packs test` uses and reports the derived
coverage beside the rows, so re-running the suite after any change happens where the method is
served.

Every file the server reads goes through a reader bound to a handle held open on the configuration's
own directory. A configured path that leaves that directory is refused, and by two checks that catch
two different escapes: a lexical one at configuration time, and resolution against the held directory
at read time, which is what catches an escape through a symlinked component. Because the second is a
handle rather than a pathname, containment holds through the open itself — there is no moment between
"this path is inside the project" and "open it" for the directory structure to be rearranged
underneath the answer. The server stays keyless and offline, and it reads only — with the single
exception a project asks for in writing: when `jpack.json` declares an `audit` directory, each
completed non-rehearsal `experimental_evaluate` call (ADR-0028) appends one record to it, inside
the project's own tree and through that same handle.

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
matrix, instead of a threshold in a prompt. The wiring is testable the same way: `jpack
experimental graph test` runs a graph matrix and reports the same kind of derived coverage `packs
test` does — over each node's probes and each edge's resolved and unresolved branches — and, like
all coverage here, it informs and never gates. A project can declare the wiring's harness too:
under `configVersion "2"`, `jpack.json` may declare its graphs and their rows (ADR-0017), and the
same two verbs with no argument then walk every declared graph exactly as `packs test` walks every
declared matrix — one CI step, no hardcoded paths.

## Keeping a record of what you decided

Every payload this runtime writes goes to a stream and is gone. If you want to be able to answer
"what did this pack decide, on what, in which version" a month later, ask for it in the file that
already says what the project owns:

```json
{
  "configVersion": "3",
  "audit": { "dir": "audit" },
  "packs": {
    "expense-approval": { "path": "packs/expense-approval-1.2.0.pack.json" }
  }
}
```

That is the whole of the opt-in ([ADR-0018](adr/0018-opt-in-evaluation-audit-trail.md)). With it,
each completed non-rehearsal evaluation (ADR-0028) of `experimental evaluate`, `experimental graph
evaluate`, and the MCP `experimental_evaluate` tool appends one JSON line to `audit/evaluations.jsonl`, relative to the
configuration: a run id and a timestamp, which surface ran, which build of this runtime ran it and
against which bundled specification artifacts, the pack's id, version, `specVersion` and the SHA-256
of its exact bytes, the facts and evidence documents as evaluated, and the disposition in the same
RFC 8785 canonical form two implementations compare byte for byte. A graph run writes one line per
node — the facts there are the assembled document, after the upstream outcomes were injected,
because that is what the node was evaluated against, and each line names the graph's `formatVersion`
and the digest of its exact bytes — plus one for the composite headline. An evaluation run under
`--rfc0008-quantifiers` carries the same draft-RFC label its payload carries, because a disposition
produced by operators no published JPS version defines is not an ordinary one. One run under
`--rfc0016-outcome-values` carries its label for the same reason, and its disposition is recorded
whole, with the `value` member where the outcome declared values (ADR-0039).

The documents are recorded as JSON *values*: the encoder compacts them, so `{ "x": 1 }` is written
as `{"x":1}` and the line is the source compacted rather than the source itself. Replaying a record
gives the evaluation that ran, because evaluation is a function of the value; keeping the caller's
exact bytes, if you need those, is the caller's job.

Five things it deliberately does not do. It does not record a declared rehearsal: `--rehearsal`
and the tool's `rehearsal` argument say this run decides nothing, and the payload's own label is
what that run leaves behind (ADR-0028). It does not record test runs: `packs test`, `experimental
graph test`, and `experimental evaluate-corpus` run the same evaluator over the same project and
write nothing, because a matrix row is a check on a pack and not a decision anyone took. It does
not record refusals: an evaluation the preflight refused has no disposition at all, and a trail
whose lines were sometimes results and sometimes failures would not be a trail of what you decided
— a graph run refused at its third node records nothing for the first two either, because a run's
records are held until the run has a composite and written in one go. It does not carry on when it
cannot write: a record that fails to append refuses the run with exit 4 rather than handing you a
disposition nothing kept. And it does not rotate, compact, or expire anything — the file is in your
tree, under your version control and your retention policy, exactly like your packs.

`packs validate` reports the directory's containment as a named check on the configuration itself,
`audit-dir-inside-root`, so a path that leaves the project is a CI failure rather than an
unexplained refusal at the first decision — a symlinked directory pointing outside included, since
that is what everything written beneath it would resolve through. A directory that is not there yet
passes it: the first record creates it. On unix the trail file is kept owner-only; on Windows a file
mode sets only the read-only attribute and does not restrict the ACL, so put the directory somewhere
whose ACL is already what you want. A directory you created yourself keeps the mode you gave it.

Where the project also keeps a lock, each line carries `reviewed`: `true` when every document the
evaluation applied was one the lock declares and the exact bytes it applied matched the reviewed set
— the check is on those bytes, not on a re-read of the file they came from — `false` when any of
them was a draft. A reviewed line also carries `reviewedSet`: the lock's own digest, its `lockVersion`,
and the configuration digest compared, so a reader holding the record and a lock file can tell
whether that lock is the one the decision was judged under. It is absent — not `false` — in a project with no lock, because "does not use the
convention" is not the same fact as "ran on unreviewed law". There is no "declared but drifted"
value: a deciding surface refuses such a run before it evaluates, so `reviewed: true` is a claim
about what actually ran rather than a label.

Where the evaluation's trace noted something about the inputs, the line says so too (ADR-0046).
`typeMismatches` lists each `equals`, `not-equals` or `in` comparison the walk evaluated whose fact
had a JSON type no operand had. `unknownCauses` lists each cause of an unknown: an absent pointer, a
value an ordered comparison could not compare, an evidence requirement of unknown presence. Each
note appears once, in the order the trace first names it, in the trace's own shape (pointers, types
and causes, never values). A graph run's node lines carry their own node's notes, and the composite
carries none. A line with nothing to note is byte for byte what it was before. The notes cover what
the walk compared, so a fact of the wrong type behind a branch the walk short-circuited is not among
them.

A project that would rather refuse such an input than record it sets `"requireComparableFacts":
true` under configVersion `"5"`. There is no coercion between JSON types, so a detector written as
`equals true` with `onUnknown: escalate` answers its fallback when the flag arrives as `"true"`, `1`
or `null`: the comparison is false, not unknown. With the member set, `experimental evaluate`, the
MCP `experimental_evaluate` tool, and `experimental graph evaluate` for each node refuse an
evaluation, after its inputs are admitted and before anything is evaluated, when a fact that some
comparison anywhere in the pack reads is present and of a type that comparison can never match: a
type no `equals`, `not-equals` or `in` operand has, or, for an ordered comparison, anything but a
decimal string, so a string outside the decimal grammar is refused as well as a JSON number. The
refusal is `JPS-FACTS-COMPARABLE-REQUIRED`, exit `1`, with no disposition and no record. It is the
project's refusal and not an evaluation error, so it carries no §8.4 class. It names each pointer,
the fact's type and what the comparison can match, never a value. The check is static: a comparison
evaluation would not reach is checked all the same, so the same facts are refused whatever the other
facts are. Its work is bounded by the evaluation's work limit, and a check that would pass that limit
refuses under the same code, whatever the pack compares, because it cannot then say the facts are
matchable. An absent fact is not refused, because it is unknown and
the pack's `onUnknown` governs it. A rehearsal is refused too, because its answer is read as well.
`packs test`, `experimental graph test`, `experimental evaluate-corpus`, `experimental compare` and
the MCP test tools are not, so a matrix row can still probe a wrong type on purpose. A pack that
compares one pointer with operands of different types in different rules cannot satisfy the
requirement for that pointer. Write one `in` whose operand carries both types instead.

Each line carries a `run` id: one value per invocation, on every record that invocation writes. For
a graph run that is what marks the run finished — the `graph-composite` line carries the same id as
its nodes' lines, so node lines whose id has no composite belong to a run that did not complete, and
a trailing line that is not whole JSON is a write that did not complete. A line whose `kind` is
`discontinuity` records a repair, not a decision, and the line before it is the damage it names.
Read a trail by that rule rather than by assuming every line is a decision.

### The chain

The trail is chained (ADR-0047 §1). Each line this runtime appends also carries three members, right
after `recordVersion`:

- `trail`: the trail's identity, 128 random bits in hex, minted when the trail is first chained and
  carried by every chained line after it;
- `sequence`: the line's number in the file, counted from 1;
- `previous`: the SHA-256 of the exact bytes of the line before it, without its newline, as
  `sha256:<hex>`.

The digest is over the bytes in the file, never over a decoded and re-encoded copy: a copy can spell
`&` as `\u0026` or `1.0` as `1` and is then other bytes with another digest. So keep a trail, and any
line you copy out of it, byte for byte. A line is otherwise exactly what it was before the chain
existed, and `recordVersion` stays `"1"`.

A trail you already have is never rewritten. The first chained line after lines that are not chained
(lines written before this release, while chaining was off, or by a runtime that does not chain)
commits to all of them at once: its `previous` is the SHA-256 of the whole file before it, which is
what `sha256sum` prints for the file as it stood, and its `sequence` continues the line count. A new
trail's first line has the SHA-256 of the empty string. A chain started again after unchained lines
keeps the identity it had.

The writer holds an exclusive, cooperative lock on the trail while it reads the last line, numbers
every line it is writing (a graph run's node lines and its composite, the composite last), writes,
and syncs the file and then its directory, so two runtimes writing one trail at once never give two
lines one predecessor, and a record reported written is on disk with the file's entry. The lock
binds only writers that take it. A line is recognised as chained by its JSON members, however it is
spelled, the same way whether the chain is being continued or started again ("Record signatures,
exactly" below gives their forms). What follows:

- **A torn last line stops the trail.** If the trail's last line has no newline, which is what a
  write cut short leaves, no line is chained after it: every recording run is refused with
  `JPS-AUDIT-WRITE`, exit 4, and a message that says so, and the trail is left as it was.
  `jpack audit repair` starts a new segment after it (below).
- **A line too long to read stops the trail too.** A line longer than 128 MiB that the writer must
  read to continue the chain is refused rather than taken for unchained, with the same code and a
  message that says so. The writer never writes a line that long: a record that would be one, which
  only a pack built for it can produce, is refused instead.
- **A lock that cannot be taken now refuses the run.** If the lock exists but cannot be taken, for
  example because the system has run out of lock records, the run is refused rather than recorded
  without it.
- **Where no lock exists, nothing is chained.** On a platform or file system that offers no
  exclusive lock at all, records are written as before, without the three members.
- **A failed sync is a failed write.** If the file or its directory cannot be synced, the run is
  refused. The record's bytes are in the file by then and stay, as for any write that fails after it
  started.

A project that does not want the chain says so under configVersion `"6"`:

```json
{
  "configVersion": "6",
  "audit": { "dir": "audit", "chain": false },
  "packs": {}
}
```

With `"chain": false` the trail is written exactly as before this release: no members, no lock, and
nothing read. A trail written that way stays readable, and it is unchained.

What the chain lets someone show, and what it does not:

- Recomputing each `previous` shows whether the lines are consistent with one another: a line
  edited, inserted, deleted or moved anywhere before the last breaks the link of the line after it.
- That is consistency, not authenticated history. The last line can be edited without breaking any
  link, and a trail cut short, or rewritten from any line on with its links recomputed, is as
  consistent as the real one. Telling them apart takes a checkpoint covering those lines (the trail's
  identity, a sequence and that line's digest) held by someone other than the operator.
- A record's `at` is still the operator's clock, and the chain says nothing about decisions that were
  never written to the trail.

### Checking a trail, and handing over a checkpoint

```sh
jpack audit verify                                   # this project's trail
jpack audit verify --trail evaluations.jsonl         # a trail file, such as a copy you were given
jpack audit checkpoint > checkpoint.json             # the last chained record, for someone to keep
jpack audit verify --expect checkpoint.json          # later: is the trail still the one checkpointed?
```

`audit verify` checks every `trail`, `sequence` and `previous` from the first chained record on, over
the bytes in the file, by the writer's own rules: the legacy prefix as one block, a chain started
again after unchained lines keeping its identity, chained lines recognised by their JSON members,
and a line over 128 MiB refused rather than guessed at. It exits 1 when any check fails, each a
named finding (`previous-mismatch`, `sequence-mismatch`, `trail-mismatch`, `incomplete-last-line`,
`line-too-long`, and the discontinuity and checkpoint findings below), and 0 otherwise, with the
report in `--format json` under `outputVersion` `"2"`. The size is read under the writer's lock,
shared, so it falls between two writes, and the bytes before it are read without the lock:
writers only append, so a verification neither delays a decision nor sees half of one.

The report gives the coverage: lines before the first chained line, committed as one block;
chained lines; unchained lines a later chained line commits to; lines nothing commits to (after
the last chained line); and lines a repair names as damaged. With `--public-key` it checks the
signatures too, and reports how far they reach (Signing the trail, below); without it, it says no
signature was checked. It also says, in fixed sentences, what the result establishes and what it
does not:

- **Without `--expect`, the integrity of one supplied chain.** The lines are consistent with one
  another. It does not show that the trail is complete, or that its last line, or lines rewritten
  from some point on with their links recomputed, are the ones first written: you were handed one
  chain, and a different consistent chain would verify as well.
- **With `--expect`, checkpoints held independently.** `audit checkpoint` prints one: a single
  line, `{"checkpointVersion":"1","recordDigest":"sha256:…","sequence":N,"trail":"…"}`, in its
  RFC 8785 canonical form. Its `recordDigest` is the SHA-256 of the record's exact line bytes, the
  digest the next record's `previous` holds and the one a gateway action receipt's
  `decision.recordDigest` names for the same record. Given to someone you do not control, the
  counterparty or an auditor, it later fails a trail that is shorter than its sequence, has another
  identity, or has another record there, and the report counts lines 1 to N as checkpointed. Lines
  after N are as unauthenticated as before. A checkpoint you keep yourself proves nothing to anyone
  who does not trust you. `audit checkpoint` refuses a trail that fails a check, and a note says
  how many lines after the checkpointed record are not chained.
- **In every report, what the trail is silent about.** Whatever was supplied, `--expect`,
  `--public-key` or `--tsa-roots`, and whatever was found, the last thing the report says it does
  not establish is the same sentence: "Whether any evaluation was refused at the gate, rehearsed, or
  failed before a disposition: the trail records decisions, not attempts, so its silence is not
  evidence that none were (ADR-0048)." A checkpoint, a key or a stamp covers the lines a trail
  holds, and no line is written for an attempt.

A trail records decisions, not attempts: a rehearsal, a test run and an evaluation refused before it
had a disposition write no line, so a chained, signed and checkpointed trail establishes nothing
about how many evaluations were refused or rehearsed, and a caller that needs that count keeps it
itself, as Runner keeps each Job's failed runs in its own store (ADR-0048).

### Handing every new checkpoint to a holder

A holder, the counterparty, an auditor, or a store you do not control, keeps the checkpoints it is
handed. Something has to hand them over: Desk, a scheduled job, a hook after each decision. The
runtime gives that deliverer every new checkpoint and keeps nothing itself:

```sh
jpack audit checkpoint --since 0                     # every chained record's checkpoint, one per line
jpack audit checkpoint --since 41 --limit 1000       # the ones after the last sequence handed over
```

- **The deliverer polls; nothing waits for it.** `--since <sequence>` prints the checkpoint of every
  chained record after that sequence, in order, at most `--limit` of them (1000 by default; a note on
  standard error, or `"more": true` in `--format json`, says more follow). The deliverer remembers
  the last sequence it handed over and asks again after it. Recording a decision never writes,
  waits for, or depends on a hand-over, so a decision is recorded when nothing delivers: its record
  is pending, reported as unwitnessed until a holder has a checkpoint covering it.
- **Retries are idempotent.** A checkpoint is a function of its record's exact bytes, so the same
  record always gives the same line, and the SHA-256 of that line is the key a deliverer or holder
  dedupes by. Handing the same checkpoint twice hands over the same bytes.
- **The holder's copy is what counts.** The runtime keeps no record of what was handed over: a
  record the operator keeps is one the operator can rewrite. Nothing stops an operator from rewriting
  the trail and handing out new checkpoints; what it cannot do is change the copies a holder already
  has. Any rewrite of a record a held checkpoint covers fails `audit verify --expect` against the
  holder's file, and a holder given two different checkpoints for one trail and one sequence holds
  proof that the trail was rewritten.

The holder keeps the lines in one file, appended as they arrive, and later holds the trail to all of
them:

```sh
jpack audit verify --trail evaluations.jsonl --expect held.jsonl --require-checkpoint-through 120
```

`--expect` takes a file of checkpoints, one per line, and may be given more than once. Every held
checkpoint must match: the line at its sequence must be a chained record of its trail with its
digest. The records up to the highest one that matched, with no failed check at or before it, are
**witnessed**; the chained records after it are **unwitnessed**, and the report counts both.
`--require-checkpoint-through <sequence>` fails the verification (`checkpoint-coverage-missing`,
exit 1) while any record up to that sequence is unwitnessed.

What a held checkpoint establishes, against an operator who does not hold the holder's copy: the
records up to it are the ones that existed when it was handed over. What it does not:

- anything about the records after the last checkpoint the holder kept, which are unwitnessed;
- that the holder kept every checkpoint it was handed: the coverage reaches only the checkpoints
  supplied to `verify`;
- when any checkpoint was made, or handed over. A time-stamping authority's stamp gives an upper
  bound instead (below): the checkpoint existed by the stamp's time.

### Stamping checkpoints with a time-stamping authority

A held checkpoint shows that records existed when it was handed over, but not by what time. An
RFC 3161 time-stamping authority (TSA) gives an upper bound: it signs a checkpoint's digest together
with the time it states, so the checkpoint existed by then (ADR-0047 §2a, C2). That bounds
existence only: it says nothing of when the checkpoint, or any record in it, was made, nor when it
was handed to anyone. It is a configured option:

```json
{
  "configVersion": "6",
  "audit": { "dir": "audit", "timestampAuthority": "https://tsa.example/stamp" },
  "packs": {}
}
```

```sh
jpack audit stamp                                    # stamp the current checkpoint, run by a scheduler, Desk or you
jpack audit verify --tsa-roots tsa-roots.pem         # check every stamp against the authorities you trust
```

- **No network on the decision path.** A decision is appended first. Stamping is a separate act,
  `jpack audit stamp`, run by whatever schedules it, at whatever interval or after whatever records
  that caller chooses: the runtime keeps no schedule of its own, because it runs no resident process
  and a decision must never wait on an authority. A record not stamped yet is pending, never failed.
  An authority that cannot be reached, that refuses, or that answers with a token for anything but
  the request leaves the trail and every decision in it as they were, and writes nothing.
- **What is sent.** `audit stamp` reads and verifies the trail first, and refuses a trail that fails
  a check. It then sends the authority the SHA-256 of the current checkpoint's canonical form
  (the `audit checkpoint` line, without its newline), a random nonce, and a request for the
  authority's certificate, and nothing else of the trail. The digest commits to the checkpoint's
  trail, sequence and record, and through the chain to every line before it. `--tsa <address>`
  replaces the configured authority; `--timeout` bounds the wait (30 seconds by default).
- **What is kept.** The reply must grant the request, stamp exactly that digest, return the nonce,
  and carry a token whose signature holds under the certificate it carries. The token is then kept
  in `stamps.jsonl` beside the trail, one line per stamp:
  `{"checkpoint":{…},"stampVersion":"1","token":"<base64 of the token's DER>"}`. The stamps file is
  written under its own lock, never the trail's.
- **Idempotent by the checkpoint's digest.** A checkpoint already stamped is not asked for again, and
  a token for it from a concurrent stamp is not kept twice. A lost reply is recovered by running
  `audit stamp` again: it stamps the same checkpoint, or the newer one if records were added.

`audit verify --tsa-roots <file>`, the PEM roots of the authorities you trust, checks every line of
the stamps file beside the trail, or of `--stamps <file>`. Which authorities are trusted is the
verifier's choice, never the project's configuration. Each token must:

- stamp, with SHA-256, the digest of the checkpoint kept with it (`stamp-imprint-mismatch`);
- be within the subset read: a SignedData of version 3 whose digest algorithms name the signer's;
  one SignerInfo, of version 1 naming its certificate by issuer and serial or version 3 by subject
  key identifier; a TSTInfo of version 1 with no critical extension and an accuracy a time span
  holds (`stamp-malformed`). What is read is held to DER: each structure decoded must be what its
  own DER encoding gives back byte for byte, which refuses an element after the last field, a
  non-minimal length, a default written out, and a SET out of order. What verification does not need
  is refused rather than carried: unsigned attributes, revocation data embedded in the token, signed
  attributes other than those read (the content type, the message digest, the ESS binding, a signing
  time and an RFC 6211 algorithm protection, each decoded and held to what it states), algorithm
  parameters other than absent or NULL (`05 00`) where NULL is allowed and absent on ECDSA, an ESS
  binding of more than one certificate or with policies, and a reply envelope with anything beside
  its status (with its text and failure bits) and its token. An algorithm named twice, in the
  SignerInfo and in the algorithm protection, is the same identifier when its parameters are, absent
  and NULL being one for the SHA-2 digests and RSA. Three things are carried and not held to DER
  here, and nothing is concluded from their contents: the certificates, as `crypto/x509` parses
  them, which accepts some forms DER does not (an explicit default such as critical FALSE, a name's
  attributes out of order); the issuer names of the SignerInfo and of an ESS issuerSerial, compared
  byte for byte with the signing certificate's own encoding of its issuer; and the TSTInfo's `tsa`
  name and the values of its non-critical extensions, which are never read;
- have a signature, signed attributes and signing-certificate binding that hold, every binding
  present (ESS `signingCertificate`, `signingCertificateV2`) naming the certificate that signed
  (`stamp-signature-invalid`);
- come from a certificate whose extended key usage is time-stamping alone, marked critical, as RFC
  3161 requires (`stamp-usage-invalid`);
- chain to a root supplied, through the certificates the token carries, with every certificate
  valid at the time the token states, not now (`stamp-untrusted`; a certificate expired or not yet
  valid at the stamp's time among them);
- be under one of the policies `--tsa-policy <oid>` names, when any is given
  (`stamp-policy-mismatch`).

**Revocation is checked only where it can be.** `--tsa-crls <file>` supplies certificate revocation
lists, PEM or DER. A certificate of the chain is checked only against a list from its issuer,
signed by it, issued at or after the stamp's time and while the certificate was still valid, so a
revocation would still be listed, and complete: not a delta list, not one an issuing distribution
point scopes, not an indirect one, with no critical extension of its own, and with no critical
entry extension but the reason code, the one an entry extension is decoded and applied. Such a list that shows it revoked at or before the stamp's time,
or for a compromised key at any time, is `stamp-revoked`. A stamp no such list speaks for is
reported with its status **not checked**, never as good, and the report says how many there are. The
runtime fetches nothing to find out.

A trusted stamp's checkpoint must then match the trail. A mismatch is `stamp-checkpoint-mismatch`:
the authority attests that checkpoint existed, so the trail was rewritten since. That includes a
last line edited, which the chain alone cannot see, and a trail cut short. Such a mismatch is a
failed check of the trail, and it voids the coverage after it.

The coverage's `stamped` is `through` the highest sequence a trusted, matching stamp covers, with no
failed check of the trail at or before it. A stamp lends its time only to the records the chain
links to its checkpoint: a stamp after a failed check of the trail, though it still matches, lends
its time to no record, and neither the time reported nor the lag reaches past the failure. The report then gives the time those records existed by
(the time the authority states, plus the accuracy it states) and the **lag** between each covered
record's `at` and the first trusted stamp covering it: how many records, the longest lag and the
shortest, each with its record's sequence, and whether some record's `at` is later than the stamp
that covers it. `--require-stamped-through <sequence>` fails (`stamp-coverage-missing`, exit 1)
while the records up to that sequence are not all stamped, as the other requirements do.

What a stamp establishes is that the checkpoint, and every line before it, existed by the time the
authority states, as that authority attests. What it does not:

- anything before that time: when the records were made, or how long before the stamp. A record's
  `at` stays the operator's word, and the lag is reported for the reader to judge, not judged;
- anything against an authority that is not independent of the operator: one that colludes can
  stamp what it is asked, when it is asked, and a root is trusted because the verifier chose it;
- a certificate's revocation status as of the stamp's time, where no supplied list speaks for it;
- anything about records after the last checkpoint stamped.

A stamp also discloses the checkpoint's digest to the authority. The digest covers a whole record,
including a random `run` id, so it is not trivially guessable, but it is not confidential either
(ADR-0047, "Privacy").

### Signing the trail

A checkpoint protects what it covers only as well as its holder keeps it. A signature answers a
narrower question with nothing held anywhere else: was this record signed by whoever holds the
project's key? It is opt-in (ADR-0047 §2b):

```sh
jpack audit key generate /var/lib/jpack/decisions.seed > decisions.pub   # a new key; prints its public key
```

```json
{
  "configVersion": "6",
  "audit": { "dir": "audit", "signingKey": "/var/lib/jpack/decisions.seed" },
  "packs": {}
}
```

- **The key.** An Ed25519 seed, 64 hexadecimal characters, the form the gateway's `keygen` writes.
  `audit.signingKey` names it by absolute path, under configVersion `"6"` and only for a chained
  trail; the `JPACK_SIGNING_KEY` environment variable, when set, names it instead, so the path need
  not be in the project's file at all.
- **The environment variable is process-wide.** `JPACK_SIGNING_KEY` is read for every project this
  runtime records for, whatever configVersion the project declares and whether or not it ever named
  a key: set in a shell, a service unit or a CI job, and inherited by everything started from there,
  it turns signing on for every project those processes record for that keeps a chained trail,
  projects under `"3"` to `"5"` included. Set it only where that is what you want.
- **What the key must be.** It is read only as it is opened, so what is checked is what is read. Its
  path must be absolute and have no symbolic link anywhere in it, so name it by its real path (on
  macOS, `/private/var/…` rather than `/var/…`): the runtime walks the path from the filesystem's
  root a directory at a time, holding each one open, refuses a link at any component, and refuses a
  component that changed between its look and its open. No directory it walks may be the project's
  own directory, compared by device and inode rather than by name. Every directory on its path, from
  the root to the key's own, must be owned by root or by the user the runtime runs as and writable
  by nobody else unless its sticky bit is set, so nobody else can remove or replace the key; a key
  in a directory its group or others can write, or under one, or in or under a directory another
  user owns, is refused, and the refusal names the directory. Each has its own fix: `chmod go-w` on
  a directory others can write, which the refusal names; for a directory another user owns, giving
  it to root or to the runtime's user, which `chmod` does not do, or moving the key. Since every
  directory from the root is held, moving the key into a private directory beneath a refused one
  does not help; move it where every directory on its path passes. The sticky bit excuses a
  directory, the key's own included: nobody else can then remove or rename a key they do not own,
  and a file someone else puts at the key's name is refused as not owned. So a key in a private
  directory under `/tmp` is accepted. Group membership is not read: a directory its group can write
  is refused whoever is in the group. The owners and modes are read each time the key is opened; an
  access-control list the permission bits do not show (POSIX ACLs show in the group bits, macOS ACLs
  do not) is not seen. `jpack audit key generate` holds those directories before it writes, writes
  the seed in the directory it held, and removes the seed again if it is then not read back as a key
  by its path. The key must be one regular file with one name (no hard link elsewhere), owned by the
  user the runtime runs as, and neither readable nor writable by its group or by others. A key that
  is not is refused and signs nothing. `packs validate` reports why, as the `audit-signing-key`
  check, naming the key by its `keyId` and never by anything read from its file; nothing this
  runtime prints or logs carries key material. `jpack audit key public <seed>` prints a key's public
  half for whoever will verify.
- **A mount is not seen.** The location check refuses a key reached through any link, and a key
  whose path passes through the project's directory. It cannot see a bind mount, or any other mount,
  that shows a directory or file from inside the project at a path outside it: a bind of the
  project's `keys` directory, or of the seed file itself, at an outside path is accepted, while a
  bind of the whole project is refused, since the walk then passes through the project's directory.
  Making such a mount takes control of the mounts the runtime sees, so the check guards against a
  key put in the wrong place, not against whoever controls the machine's mounts.
- **No key signs on Windows.** Who may read a file there is whatever its ACL allows, and this
  runtime does not read ACLs, so it cannot show that a key is its owner's alone. Every key is refused
  there, as on any platform without unix ownership and modes: records are unsigned, and `packs
  validate` fails the `audit-signing-key` check and says why. `jpack audit key generate` and
  `public` still work there, since they sign nothing, and `audit verify` checks signatures made
  elsewhere.
- **The sidecar.** After each chained record is written and synced, and still under the trail's
  lock, one line signing it is appended to `signatures.jsonl` beside the trail: the record's `trail`,
  its `sequence`, and the SHA-256 of its exact line bytes, signed. The record line itself is not
  changed. A graph run's lines are signed in their order, and a repair's discontinuity record is
  signed like any chained record.
- **Pending, never failed.** A signature that cannot be written leaves its record unsigned and the
  decision recorded: signing never refuses a run. So does a key that is refused or is not the key in
  force. `audit verify` counts such a record unsigned, and a later signed record still covers it
  through the chain. A sidecar line a failed write left without its newline was never written, as
  a verifier reads it, and the writer reads it the same way: it decides nothing about the key in
  force, and before the next line the writer ends it with `~` and a newline, so it stays a line of
  its own that can never be read as a sidecar line, and the next line stays whole.
- **Rotation.** `jpack audit key rotate --next <seed>` appends a `key-rotation` line, made with the
  key the project names and naming the next key's public key and the trail's last line: the records
  up to that line are signed with the old key, and the records after it with the next. A writer signs
  only with the key in force, the one the sidecar's last readable line names or signed with, so the
  old key signs nothing from the moment the rotation is written, and the next key signs once the
  project names it; records written in between are unsigned. A key put in place without a rotation is
  not the key in force and signs nothing, and `packs validate` says so. Only the key in force can
  rotate: a lost key cannot be rotated away from, and then the trail and its sidecar are moved aside
  together, so the next record starts a new trail with a new sidecar.
- **The writer does not check a rotation's signature.** To choose the key in force, the writer
  follows the sidecar's last readable rotation line as it stands; it does not verify that line's
  signature or its next key. A well-formed rotation line someone forged therefore stops the
  configured key from signing, and records are written unsigned, while `audit verify` reports the
  line as `rotation-invalid` and keeps the old key. It cannot make any record count as signed. It
  means whoever can write the sidecar can stop signing, as they could by deleting the sidecar;
  `--require-signed-through` is what turns records left unsigned that way into a failure.
- **Revocation is the verifier's.** A rotation stops the old key signing here; it cannot stop a copy
  of that key signing elsewhere. Whoever verifies says which keys they trust, from the outside:
  `--public-key` once per key, in the order the trail used them, and `--revoked <file>` for a key
  not to trust from a sequence on. A key under which anyone could sign, one of small order or not
  canonically encoded, is refused in either (below).

```sh
jpack audit verify --public-key decisions.pub                            # every signature, under the first key
jpack audit verify --public-key first.pub --public-key next.pub \
  --revoked revoked.jsonl --require-signed-through 120                   # pinned keys, a revocation, a requirement
```

With `--public-key`, `audit verify` reads the sidecar beside the trail, or `--signatures <file>`, in
step with the trail, and checks every line by the rule below. A record signature must be for the
record at its sequence, by its exact bytes, and verify under the key in force there; a rotation must
be signed by the key in force, and hands the records after its line to the key it names. Each failed
check is a named finding: `signature-invalid` (a bad signature, or another key's),
`signature-record-mismatch` (a signature for another record than the one at its sequence),
`signature-no-record` (a line naming a sequence the trail does not have, or no chained record of its
trail), `rotation-invalid` (a rotation not signed by the key in force, of another trail, to a key a
verifier refuses, or to a key other than the next one pinned), `signature-key-revoked` (a line made
with a key revoked at its sequence), `sidecar-out-of-order`, and `signature-missing` (below). A
sidecar line of no shape the rule reads, a torn last line among them, is counted unreadable and is
not a failure.

The report gives `signed` as `through` the highest sequence whose own record carries a valid
signature with no failed check of the chain at or before it, which then covers every line up to it,
and counts `signedRecords` (chained records with a valid signature of their own) and
`unsignedRecords`. A record with no signature is unsigned, never a failure by itself, because a
signature that could not be written leaves its decision recorded. `--require-signed-through
<sequence>` turns that into a pass or a fail: `signature-missing`, exit 1, while the signed coverage
does not reach that sequence. It mirrors `--require-checkpoint-through`, and it is what a reader that
needs a record signed, a gateway acting on it or an auditor, asks for.

What a signature establishes, against someone who can edit the trail and its sidecar but holds none
of the keys: the lines up to the signed coverage are as they stood when the record there was signed.
Such a person can edit, insert, delete or reorder lines, the last signed one included, only by
failing a check or by pulling the signed coverage back before them, which
`--require-signed-through` finds. What it does not:

- **anything against the operator,** who holds the key. A record the operator altered and signed
  again, or a trail the operator rewrote from some point on and signed, verifies like the one first
  written; only a checkpoint held by someone else shows the difference.
- **anything after a key is copied or stolen.** Whoever holds it can sign altered records, or a
  rotation to a key of their own, and only the verifier's own trust configuration refuses what they
  sign.
- that the trail is complete: a trail and its sidecar cut short together verify, and only a held
  checkpoint past the cut, or `--require-signed-through`, finds it; nor when any record was written.

An attacker holding a key the trail has rotated away from can, until the verifier is told
otherwise, fork the key history at any line where that key was in force: keep signing with it as if
no rotation had happened, or forge a rotation from it to a key of their own, and with the trail
rewritten from that line on, the forgery verifies against the first public key alone. Revoking the
key from the line after its rotation (`--revoked`) refuses everything it signs from there on, record
or rotation; pinning the keys in order (`--public-key` once per key) refuses a rotation it forges to
a key of its own before that line. With both, it can re-sign records only up to its rotation, and
only by also leaving every record after it unsigned or cutting it off, which
`--require-signed-through` past the rotation, or a held checkpoint past it, finds. It can never
forge a later key's signature, and never alter a record a held checkpoint covers without that being
found.

#### Record signatures, exactly

For another implementation, such as a gateway that requires a signed record or Runner's verifier,
this is the whole rule; nothing in it depends on reading this runtime's code.

**Keys.** Ed25519 (RFC 8032), verified by the one equation **Signatures** states below. A public key
is its 32 bytes as 64 lowercase hexadecimal characters; a key file given to `--public-key` may also
be in upper case, with whitespace around it, while a sidecar's `next` and a revocation's `publicKey`
are in lower case alone. Its `keyId` is the first 32 lowercase hexadecimal characters of the SHA-256
of those 32 bytes, the gateway's `keyId`.

A verifier refuses a public key unless its 32 bytes are the canonical encoding (RFC 8032 §5.1.2) of
a point of the curve whose order does not divide 8, and refuses it before it reads anything signed
with it. The encoding is read as written, before anything reduces it: y is its low 255 bits,
little-endian, and its top bit is the sign of x. A key is refused when:

- **it is not canonical:** y is p = 2^255 − 19 or more (`edff…ff7f` is y = p), or x is 0 (y is 1 or
  p − 1) and the sign bit is set. A lenient reader, Go's `crypto/ed25519` among them, takes such an
  encoding for another: y = p for y = 0, the all-zero key;
- **it is of small order:** it is one of the eight points whose order divides 8, listed below. Under
  such a key `crypto/ed25519` accepts, for most messages, a signature anyone can make without a
  private key: R a point of small order and s zero. The all-zero key, a likely placeholder, is one;
- **it is no point:** (y² − 1) / (d·y² + 1) has no square root modulo p, so nothing verifies under
  it.

The eight points of small order, by their canonical encodings; every other encoding of them is not
canonical:

```text
0100000000000000000000000000000000000000000000000000000000000000
ecffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f
0000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000080
26e8958fc2b227b045c3f489f2ef98f0d5dfac05d3c63339b13802886d53fc05
26e8958fc2b227b045c3f489f2ef98f0d5dfac05d3c63339b13802886d53fc85
c7176a703d4dd84fba3c0b760d10670f2a2053fa2c39ccc64ec7fd7792ac037a
c7176a703d4dd84fba3c0b760d10670f2a2053fa2c39ccc64ec7fd7792ac03fa
```

This runtime refuses such a key wherever it reads one: as `--public-key`
(`JPS-AUDIT-PUBLIC-KEY-INVALID`), in `--revoked` (`JPS-AUDIT-REVOKED-INVALID`), and as a rotation's
`next` (step 3 below). No key a seed derives is refused.

**The sidecar.** `signatures.jsonl`, in the trail's directory. Every line this runtime writes ends
with a newline and is the RFC 8785 canonical form of one of two objects, seven members each:

| member | `record-signature` | `key-rotation` |
|---|---|---|
| `kind` | `"record-signature"` | `"key-rotation"` |
| `sidecarVersion` | `"1"` | `"1"` |
| `keyId` | the signing key's `keyId` | the key in force, which makes the rotation |
| `trail` | the record's `trail`, 32 lowercase hex | the trail's identity |
| `sequence` / `at` | `sequence`: the record's sequence, its line number | `at`: the trail's last line when the rotation was made |
| `record` / `next` | `record`: `sha256:` and the SHA-256 of the record's exact line bytes, without the newline, 64 lowercase hex | `next`: the next key's public key |
| `signature` | 128 lowercase hex | 128 lowercase hex |

`sequence` and `at` are integers from 1 to 2^53−2, spelled as JSON integers: digits, with no
fraction, exponent, sign or leading zero, so `3.0`, `3e0` and `03` are not integers. A reader reads
a line as one JSON object with exactly those seven members, each once and of those forms, whatever
its whitespace and its escapes. A member's name and a string are what JSON decodes them to: an
escape is the character it spells, so `\u0061` is `a`; a name given twice, however spelled, makes
the line unreadable; and the forms are held, and the signed bytes built, on the decoded values. Any
other line, a line with no newline after it, and a line longer than 4096 bytes, its newline not
counted, are unreadable: they sign nothing and are not a failure. A writer ends a line it finds
without its newline with `~` and a newline before it appends, so that line stays unreadable.

**The signed bytes.** A record signature signs the ASCII bytes
`judgment-pack-runtime/record-signature/1:` followed by the RFC 8785 canonical form of
`{"record":R,"sequence":S,"trail":T}`, which, for these values, is
`{"record":"sha256:<64 hex>","sequence":<S in decimal>,"trail":"<32 hex>"}` exactly. A rotation
signs `judgment-pack-runtime/key-rotation/1:` followed by `{"at":<A in decimal>,"next":"<64
hex>","trail":"<32 hex>"}`.

**Signatures.** A signature is 64 bytes: `sigR`, the first 32, and `sigS`, the last 32 read as a
little-endian integer. It verifies under a public key's 32 bytes `A` over the signed bytes `M`
exactly when `sigS` is below L = 2^252 + 27742317777372353535851937790883648493, and the canonical
encoding of [`sigS`]B − [h]A is `sigR` byte for byte, B being the base point and h the SHA-512 of
`sigR` ‖ `A` ‖ `M` read as a little-endian integer and reduced modulo L. That is RFC 8032's check
without the cofactor and with a canonical scalar, as Go's `crypto/ed25519` verifies. RFC 8032 §5.1.7
also allows the cofactored check, [8][`sigS`]B = [8]`sigR` + [8][h]A, which accepts more: a `sigR`
with a part of small order, or a key of mixed order with a signature that holds only up to that
part. A verifier must not use it; the test vector below has a signature only the cofactored check
accepts, and it is `signature-invalid` here. `sigR` is not otherwise decoded, so an encoding of it
that is not canonical never verifies.

**The trail's lines.** A trail line is the bytes before a newline. Bytes after the last newline, if
there are any, are not a line: they are an incomplete last line, `incomplete-last-line`, whatever
they hold, and are classified neither as chained nor as too long. A line longer than 128 MiB, its
newline not counted, is not read at all, and is the failed check `line-too-long`; a line of exactly
134,217,728 bytes and its newline is read.

Any other line is a **chained record** when it is one JSON object, with no member given twice, whose
`trail` is 32 lowercase hexadecimal characters, whose `sequence` is an integer from 1 to 2^53−2
written as one (digits, with no fraction, exponent, sign or leading zero), and whose `previous` is
`sha256:` and 64 lowercase hexadecimal characters. Member names and strings are read by their
decoded values, as a sidecar line's are, and the line's other members are not read for this.
Otherwise it is **unchained**, a line with one of those members missing or of another form among
them. An unchained line is no failed check: the next chained line links over it to the whole file
before it, as below, and an unchained last line is one nothing commits to.

Each chained line L is held to what a record in its place would follow. Its trail must be that of
the last chained line before it, if there is one. Its `previous` must be the SHA-256 of the exact
bytes of the line before it when that line is chained, or of the whole file before it when it is
not: a legacy prefix, or a chain started again after unchained lines. A **discontinuity record**, a
chained record whose `kind` is `discontinuity`, is **well formed** when its `discontinuity` member
is one JSON object of exactly four members, none given twice: `reason`, the string
`incomplete-last-line`; `line`, an integer from 1 to 2^53−2 that is L−1; `bytes`, an integer from 0
to 134,217,728; and `digest`, `sha256:` and 64 lowercase hexadecimal characters, its integers
written as a `sequence` is; and when line L−1 is itself no longer than 128 MiB and is not itself a
discontinuity: one JSON object, with no member given twice, whose `kind` is the string
`discontinuity`, whether it is chained or not. A line of another shape, a JSON object with a member
given twice among them, can be named whatever its `kind`. A well-formed discontinuity record is held
to what line L−1 would have followed, instead of to the line before it. These are **the chain's
checks**, each a finding at line L:

- `sequence-mismatch`: its `sequence` is not L;
- `previous-mismatch`: its `previous` is not that digest;
- `trail-mismatch`: its `trail` is not that trail;
- `discontinuity-malformed`: a discontinuity record that is not well formed. It excuses nothing, and
  is held to the line before it as any chained line is;
- `discontinuity-mismatch`: a well-formed discontinuity record whose line L−1 is not of the `bytes`
  and `digest` it states;
- `line-too-long`: line L is longer than 128 MiB; the line after it is held to no `previous` and no
  `trail`, since what it would follow cannot be read.

The line a well-formed discontinuity record names is excused: its own findings are not reported,
even when the discontinuity then fails its length or digest comparison, which is the discontinuity's
own failed check, and the damage itself is no failed check. Nor is anything else a check of the
chain: a finding about the sidecar, a held checkpoint (`checkpoint-…`), a stamp (`stamp-…`), or an
incomplete last line, which comes after every complete line.

**Verification.** The inputs are the trail, the sidecar, the public keys K₁…Kₙ (n ≥ 1) in the order
the trail used them, and any revocations, each a public key and the sequence `from` which it is not
trusted. Start with K₁ in force and the last place 0. A record signature's place is 2·S, and a
rotation's 2·A+1. Read the sidecar's lines in order; pass over each unreadable one, and for each
readable one:

1. If its place is below the last place, or it is a record signature and its place equals the last
   place, it is `sidecar-out-of-order`; go to the next line. Otherwise its place becomes the last
   place.
2. A record signature for sequence S: the trail's line S must exist, be a chained record (above)
   that no discontinuity names as damaged, and carry trail T, or it is `signature-no-record`. The
   SHA-256 of that line's exact bytes must be R, or it is `signature-record-mismatch`. Its `keyId`
   must be the key in force's and its signature must verify under that key over the signed bytes, or
   it is `signature-invalid`. If a revocation of the key in force has `from` ≤ S, it is
   `signature-key-revoked`. Otherwise record S is signed.
3. A rotation at A: the trail's line A must exist, or it is `signature-no-record`. T must be the
   trail of the last chained line at or before A that no discontinuity names as damaged (with none,
   the rotation fails), its `keyId` the key in force's, and its signature valid under that key over
   the signed bytes, or it is `rotation-invalid`. If a revocation of the key in force has `from` ≤
   A, it is `signature-key-revoked`. `next` must be a key a verifier accepts (Keys), and with n > 1
   the next key not yet rotated to in K₁…Kₙ, or it is `rotation-invalid`. Otherwise `next` is the
   key in force.

The lines are taken in the sidecar's order, so a rotation is before a record signature when it is
earlier in the file, and step 1 then holds it to an A below that signature's S: a record signature
for S read after a rotation at A ≥ S is out of order. A line that fails a check changes nothing but
the last place: a rotation that fails hands nothing on, and the key in force stays the one before
it. The writer, which does not check a rotation, follows one that is the sidecar's last readable
line all the same, and stops signing (above).

Each finding about the sidecar but one is placed at the trail line it is about: a record signature's
at its S, a rotation's at its A, and `sidecar-out-of-order` at the S or A of the line out of order;
and its detail names the sidecar line, counted from 1 over all of the sidecar's lines, unreadable
ones included. The one is `signature-missing`, which is about a requirement, not a line of the
sidecar: it is placed at the sequence required, and its detail names that sequence and no sidecar
line.

The signed coverage is through the highest S whose record is signed with none of the chain's checks
failed at a line at or before S; a requirement through R fails, `signature-missing`, unless that
coverage reaches R.

**One record, without the trail.** A reader that holds a record and not its trail, such as a gateway
acting on a decision, takes T and S from the record's own `trail` and `sequence`, read by the
chain's rule (a record without a chained `trail`, `sequence` and `previous` has no signature), and R
as the SHA-256 of the record's exact bytes without a newline; handed a digest, such as a gateway
receipt's `decision.recordDigest`, it holds R to it. It reads the sidecar's lines in order as above,
leaving out every check of the trail's lines:

- every readable line is held to step 1;
- a rotation is held to step 3 with T as the trail it must name and none of the trail's lines
  checked; when it holds, its `next` is the key in force;
- a record signature for another sequence is passed over once step 1 has placed it;
- the first record signature for S that step 1 admits decides: its `trail` must be T, its `record` R
  and its `keyId` the key in force's, its signature must verify under that key over the signed
  bytes, and that key must not be revoked at S, or the record is not signed. A later line for S is
  out of order and signs nothing, however valid, and the lines after the deciding one change
  nothing.

This establishes only that a key in force by the sidecar's own order, K₁ as handed on by the
rotations before the record's line that hold without the trail, signed these exact bytes as record S
of trail T. It establishes nothing the trail shows: not that line S holds this record, not that it
or any other line is undamaged, not the trail's status, and not that each rotation it followed is
anchored where `audit verify` holds a rotation, on an undamaged chained line of T at or before its
A.

Its answer and `audit verify`'s for record S can therefore differ, either way. A record a
discontinuity names as damaged, or one after a rotation that no undamaged chained line of T anchors,
which step 3 refuses, can be signed here and not there; and once the two have followed different
rotations, a record can be signed there and not here. They agree when the trail holds the record at
line S undamaged and, for each rotation that step 1 admits before the record's line, the last
undamaged chained line at or before its A is of trail T. Only the trail shows that, so only `audit
verify` gives the trail's answer.

A reader that trusts a set of keys by its own configuration instead of following the trail's key
history, as a gateway's policy can, follows no rotation and has no key in force to order lines by:
any readable record signature for T, S and R under a key it trusts, not revoked at S, signs the
record. That establishes only that a key it trusts signed these exact bytes as record S of trail T,
and nothing the trail shows.

**A copied record.** A signature is never in the record: it is a line of the sidecar of the trail
the record was written to, and nothing is added to the record line. A record copied out of its trail
is checked with its sidecar's lines up to and including its own signature line, copied as they are
and in their order, and kept beside the copy under the same name, `signatures.jsonl`; with them, the
check of one record gives the answer it gives with the whole sidecar. The record is copied as its
exact bytes: re-encoded, however equal as JSON, it has another digest and is not the record signed.

**A test vector.** The keys are the seeds of RFC 8032's first two test vectors:

- first seed `9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60`, public key
  `d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a`, `keyId`
  `21fe31dfa154a261626bf854046fd227`;
- second seed `4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb`, public key
  `3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c`, `keyId`
  `39f713d0a644253f04529421b9f51b9b`.

The trail, two lines:

```text
{"recordVersion":"1","trail":"00112233445566778899aabbccddeeff","sequence":1,"previous":"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","kind":"evaluation"}
{"recordVersion":"1","trail":"00112233445566778899aabbccddeeff","sequence":2,"previous":"sha256:9305565078dd0531a50e67ee2d227d4fd0cd0d504541b92801660236a0f0eaed","kind":"evaluation"}
```

The first line's digest is `sha256:9305565078dd0531a50e67ee2d227d4fd0cd0d504541b92801660236a0f0eaed`,
and the bytes its signature signs are:

```text
judgment-pack-runtime/record-signature/1:{"record":"sha256:9305565078dd0531a50e67ee2d227d4fd0cd0d504541b92801660236a0f0eaed","sequence":1,"trail":"00112233445566778899aabbccddeeff"}
```

The sidecar, three lines: the first record signed with the first key, a rotation to the second key
after line 1, and the second record signed with the second key:

```text
{"keyId":"21fe31dfa154a261626bf854046fd227","kind":"record-signature","record":"sha256:9305565078dd0531a50e67ee2d227d4fd0cd0d504541b92801660236a0f0eaed","sequence":1,"sidecarVersion":"1","signature":"be509a591ce3d1ecc67a3bd2c35c914e001d2737a62ff8759e5be775448e5d5531370372971fdee8ac5b3c7c63fe16c9f83fed9243e114b8e6c02a2156ffbb0b","trail":"00112233445566778899aabbccddeeff"}
{"at":1,"keyId":"21fe31dfa154a261626bf854046fd227","kind":"key-rotation","next":"3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c","sidecarVersion":"1","signature":"d6cf4da642a0f84dbaaf03a88b7afe0d7eded03897301ac80ab26f7248631502ebc482063a0dc37a2234fff2c62f8bff35ae825261e9d0b6d7908495a7f21201","trail":"00112233445566778899aabbccddeeff"}
{"keyId":"39f713d0a644253f04529421b9f51b9b","kind":"record-signature","record":"sha256:d927c7b963914116f0f47219b0564b40741ae2b56de1c38ccaf27fd0f0630702","sequence":2,"sidecarVersion":"1","signature":"b28b5765844abecd9ca69b642ae1685597045babda87635b8760da9d747a6dc1c244cbbbb0b2c2d73766855665bad9487f61a373209244f7c41af0bb406d8400","trail":"00112233445566778899aabbccddeeff"}
```

Verified with the first public key alone, every line holds: both records are signed, one rotation is
followed, the second key is in force at the end, and the signed coverage is through sequence 2.
Verified with the second public key alone, the first line is `signature-invalid` and the rotation
`rotation-invalid`, both at line 1, and the second record is signed. Ed25519 signatures are
deterministic, so an implementation that signs these lines with these seeds writes these bytes.
Checked one record at a time with the first public key and these three lines, both records are
signed, record 2 under the second key, which the rotation before it put in force; with record 2's
own line alone it is not, since no rotation then puts the second key in force.

A signature only the cofactored check accepts, which a verifier must refuse: the first sidecar line
with its `signature` replaced by

```text
2faf65a6e31c2e133985c42d3ca36eb1ffe2d8c859d0078a61a4188abb71a2aa310568784fb6c286895b994aba5032fd65558742dcdce63ea7f0c71fb8428904
```

Made with the first key's secret and a point of order 2 added to its `sigR`, it holds under
[8][`sigS`]B = [8]`sigR` + [8][h]A and not under the check above. Verified with the first public
key, that line is `signature-invalid` at line 1, the rotation after it still holds, and record 2 is
signed.

### Repairing a torn trail

```sh
jpack audit repair
```

After a write that did not complete, `audit repair` starts a new segment. It removes and rewrites
nothing: under the writer's lock it ends the damaged bytes with a newline, so they are kept in
place as a line of their own, and appends a discontinuity record:

```json
{"recordVersion":"1","trail":"…","sequence":5,"previous":"sha256:…","run":"…","at":"…","kind":"discontinuity","surface":"audit repair","tool":{…},"discontinuity":{"reason":"incomplete-last-line","line":4,"bytes":29,"digest":"sha256:…"}}
```

It names the damaged line, its length and the digest of its bytes, and its `previous` links over the
damaged line to what a record in its place would have followed. The writer then chains after it as
after any record. `audit verify` holds the damaged line to the record's length and digest, reports
the trail as segments, `"status": "segmented"` and exit 0, and never as intact across the break; a
checkpoint made before the damage still verifies the segment it covers. A discontinuity line is not
a decision: it has no pack, inputs or disposition, and a reader that selects records by `kind`
passes over it.

A repair is refused when the last line is complete, so it never runs on a trail with nothing damaged
at its end; a broken link elsewhere is for `audit verify` to report, not for a repair to paper over.
It is refused too when the project's audit member says `"chain": false`, where no lock can be
taken, and when the damaged bytes are longer than any line a chained trail holds. And a repair does
not repair a repair: when the incomplete last line is itself a discontinuity record whose write did
not complete, it is refused (`JPS-AUDIT-REPAIR-DISCONTINUITY`). A discontinuity decides which line
is not held to the chain, so one that could itself be named damaged would leave the line it excused,
and everything that line binds, checked by nothing; `audit verify` reports a discontinuity naming
another discontinuity, or naming a line over the bound, as malformed and excuses nothing. Move such a
trail aside and keep it, and the next record starts a new one. A repair works on the trail the
project's `jpack.json` declares, never on a file named by path. Since anyone who can run it can run
it at will, a discontinuity says a break was acknowledged, not why.

A report lists the first hundred discontinuities and segments, as it lists the first hundred
findings, and counts them all (`discontinuitiesTotal`, `segmentsTotal`), so a trail of many repairs
costs a verification no more memory than a trail of few. Line endings are bytes too: a trail whose
newlines were converted to CRLF, as a checkout can convert them, is a different trail and does not
verify; keep trails out of any line-ending conversion.

None of the `audit` commands is offered as an MCP tool: a verification an agent runs on the trail of
the server it is using shows nothing to someone who does not trust that server's operator, which is
who a verification is for, and repair, stamp and the key commands write.

The records hold your input documents. That is what they are for, and it is why the directory is
one you name rather than one this runtime picks: the human-readable diagnostics stay sanitized and
value-free, because they go to whoever is watching a terminal, while a record goes where you sent
it.

## What this runtime still never does

- No store you did not ask for: your packs are yours, on your disk, in your version control, and
  the two files this runtime writes are ones you asked for — the audit trail your own `jpack.json`
  declared, and the reviewed-set lock `packs lock` generates. Ask for neither and nothing is written
  at all.
- No wall around your own tree: the lock makes an amendment explicit and recorded, and anything that
  can edit a pack can also re-run `packs lock`. Keeping the deciding party out of the law's write
  domain is a property of where the decision runs, not of what this runtime checks.
- No credential and no network: hints are text; the runtime never reads a source.
- No selection: naming a pack is the application's.
- No approval workflow: your pull request is the approval.

## See also

- [ADR-0012 — the jpack.json project convention](adr/0012-jpack-project-convention.md)
- [ADR-0015 — the experimental graph surface](adr/0015-experimental-graph-surface.md) — declaring composition instead of coding it
- [ADR-0018 — the opt-in evaluation audit trail](adr/0018-opt-in-evaluation-audit-trail.md) — recording what was decided
- [ADR-0019 — the reviewed-set lock](adr/0019-reviewed-set-lock.md) — declaring which law counts, and refusing to decide under law that left it
- [authoring-lifecycle.md](authoring-lifecycle.md) — writing and repairing one pack
- [agent-testing.md](agent-testing.md) — the agent-driven testing protocol
- [mcp-clients.md](mcp-clients.md) — per-client setup
- [`CONFORMANCE.md`](../CONFORMANCE.md) — where the evaluator's conformance claim is stated, in full
  and only; nothing in this guide states any part of it

## Replaying a decision: pin the tuple, not the pack

A pack hash alone does not make a decision replayable. The evaluator applies a specification
contract that moves with releases, and JPS §11 makes a pack's declared `specVersion` exact — so a
byte-frozen pack that evaluated cleanly under one release can be *correctly refused* by a later one
(`JPS-EVALUATION-PACK-SPEC-VERSION`, in preflight, before any evaluation) while `spec validate`
still passes it. That is working as designed, and it means any pack pinned for longer than the
release cadence will eventually meet an evaluator that no longer evaluates it. Issue
[#93](https://github.com/Judgment-Pack/judgment-pack-runtime/issues/93) records the first time this
happened in the field, mid-study.

The unit of replay is therefore a tuple of three facts, recorded **together**, at the moment the
decision — or the freeze, for a study — happens:

| | what to record | where it comes from |
| --- | --- | --- |
| the pack | SHA-256 of the exact bytes evaluated | your repository, lock, or audit record |
| the evaluator release | the version that ran | the JSON envelope's `tool.version`, or `jpack version` |
| the executable | SHA-256 of the binary that ran | the audit record's `tool.digest`, or hash the file you staged |

Side by side, in one place. A pack hash in one file and a binary version in another is the
fact-stated-twice problem from `expectedVersion` in a different costume: nothing checks that the
pair you eventually replay is the pair that ran. The opt-in audit trail (ADR-0018) writes all three
on every evaluation record: the pack's digest, the `tool` that produced the record with its version
and `evaluatorSpecVersion`, and, from 0.24.0, `tool.digest`, the SHA-256 of the
executable that wrote it (ADR-0043). A graph run's composite record names no pack; its node records
do, so a graph replay reads both. The runtime reads the digest from its own executable, so it is the
running program's account of itself: evidence of which build ran rather than proof. On Linux it
names the running file even if its path was replaced; elsewhere it names what was at the
executable's path when the first record was composed, and a file rewritten while it was read can
go unnoticed there. It is absent where the executable could not be read, or was seen to change
while it was read. A project that needs the digest from a source the binary does not control keeps its
own, from wherever the binary is staged and verified, beside the record.

The discipline at replay time:

1. Fetch the recorded release by its tag. Published tags are never moved or reused, and old
   releases stay published, precisely so this step works years later
   ([VERSIONING.md](../VERSIONING.md)).
2. Verify the archive against its `checksums.txt` and the extracted binary against your recorded
   digest, before executing anything.
3. Evaluate the recorded pack bytes with that binary — never with a current one. A current binary
   refusing your old pack is not the replay failing; it is the specification's exactness doing its
   job. Re-declaring the pack to satisfy a newer evaluator is an edit to the artifact you were
   trying to replay, and belongs to a new version of the decision, not to the replay of the old
   one.

Projects that already stage binaries through a verified lock — a file of name, version, and digest
that a boot step checks before executing — have all the mechanics. The habit that tends to be
missing is the pairing: writing the evaluator's identity down next to the hash of the pack it
judged, in the same record.
