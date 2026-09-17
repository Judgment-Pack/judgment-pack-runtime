---
status: proposed
date: 2026-09-16
deciders: maintainer
---

# Report an expectation no pack can reach under its own code, in the admission tool alone

## Context and problem statement

ADR-0035's `experimental_validate_expectations` reports `valid` for dispositions
that §8's step order and §5's identifier grammar put beyond every conforming pack:
an `unresolved` result retaining `not-applicable`, `no-match` beside any other
reason, and an `outcomeId` outside `^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`. Its own
Authority consequence states all three as an open gap and defers the fix — "a
separately coded reachability finding is the way to close the rest, and it belongs
to the tool rather than to the decoder". Until it lands, an authoring client that
receives `valid` cannot tell "this is not a disposition" from "no pack produces
this", so it opens a repair loop against a candidate that can never satisfy the
expectation: the stall ADR-0035 exists to end, reached by a different route.

## Decision drivers

- A blocked admission must say which side has to move — the expectation or the
  candidate — because the two repairs have nothing in common.
- Only rules that hold for every pack may be applied: the tool sees no pack, so a
  rule it cannot decide from the disposition alone it must not pretend to decide.
- The §8.3 grammar gate has other readers whose derivations depend on admitting
  exactly what §8.3 admits; whatever is added must not narrow it.
- One code per class of refusal, continuing `JPS-EXPECTATION-LIMIT`'s split, so a
  client branches on `code` and never on `status`.
- No new authority, no new dependency: read-only over text already in memory.
- The statement ADR-0035 wrote down must become checkable rather than restated.

## Considered options

- A separately coded per-row finding, `JPS-EXPECTATION-UNREACHABLE`, in the
  admission tool, after the §8.3 grammar gate.
- A third `status` value beside `valid` and `invalid`, such as `unreachable`.
- A new boolean member on the finding, such as `reachable: false`, beside the
  existing `status`.
- The same three rules inside the shared `evaluation.DecodeDisposition`, so every
  reader of an expected disposition inherits them.
- Nothing in code: leave the three classes documented, as ADR-0035 left them.
- Pack-dependent reachability as well: hold `outcomeId` to the outcomes a supplied
  pack declares, and the handoff to that pack's escalation object.
- For the §5 grammar the third class needs: a fourth statement of the pattern,
  local to `internal/mcp`, against exporting one shared compiled pattern and
  moving every existing statement of it — `internal/validation`'s lookup key and
  `internal/project`'s unexported matcher and filename convention — onto it.

## Decision outcome

Chosen option: "a separately coded per-row finding in the admission tool, after
the §8.3 grammar gate", because the three rules are facts about the disposition
alone and so are decidable exactly where the tool already stands, and because a
new code — not a new status and not a new member — is the split this payload
already made once and clients already branch on.

**The finding.** After `evaluation.DecodeDisposition` succeeds and before the row
is marked valid, `unreachableExpectation` names the rule that rules the shape out,
or returns nothing. A named rule makes the row `status: "invalid"` with
`code: "JPS-EXPECTATION-UNREACHABLE"` and that rule as its `message`. Three
classes, tested in §8's own order and then §5:

1. `kind: "unresolved"` retaining reason `not-applicable`. §8 step 1: "If
   applicability is false, produce a terminal `not-applicable` result carrying
   reason `not-applicable` and do not evaluate exceptions or rules." That is the
   only step that records the reason, and it reports the halt under kind
   `not-applicable`. The message says so and says to expect that kind instead.
2. Reason `no-match` beside any other reason, as a set: duplicates are one member,
   so `["no-match","no-match"]` is `no-match` alone and stays valid. §8 step 10 —
   "If no fallback is present, produce `unresolved` with reason `no-match`" — is
   the only step that records it, and every step that records another reason
   returns first: step 5 "produce `unresolved` after all exception effects have
   been inspected, and do not evaluate normal rules", step 8 "Produce `unresolved`
   whenever either reason is present". So `no-match` is only ever the sole reason.
3. `kind: "outcome"` whose `outcomeId` does not match §5's grammar: "Local object
   identifiers are non-empty ASCII strings matching
   `^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`." The message quotes the grammar and names
   the offending id. The specification's own expected-disposition schema already
   refuses such an id — `$defs/disposition` names `$defs/localId` for `outcomeId`
   — so this reports over a §8.3 text what that schema reports over a manifest.

**A fourth statement of §5's grammar, not a reuse.** Class 3's pattern is written
once more, as one compiled `localIdentifier` local to `internal/mcp`, beside the
three statements of it this repository already carries. Neither existing one is
reusable as it stands. `internal/validation`'s copy is a *lookup key*: it maps a
JSON Schema `pattern` string to the diagnostic code `JPS-STRUCTURE-LOCAL-ID`, so
it is compared, never matched, and binding this gate to it would couple an MCP
refusal message to that table's job and silently break this check the day the
table is re-keyed. `internal/project`'s copy is a real compiled matcher but
unexported, in a package the MCP server does not import and should not import for
one regexp — and that package states the grammar a second time itself, inside a
filename convention, for the same reason. Exporting one shared compiled pattern
and moving all four call sites onto it was considered and rejected here: it would
put a change to three subsystems, each with its own refusal wording and diagnostic
code, inside a change whose subject is one tool's finding, and `internal/project`
is a tree this change is otherwise required not to touch. The cost accepted is one
more place to edit if §5's grammar ever moves. It is bounded: the grammar is a
normative constant of §5 rather than an implementation detail that drifts, every
place that enforces it already states it in full, and the specification's own
expected-disposition schema states it once more at `$defs/localId`, which
`$defs/disposition` names for `outcomeId` — so a move would be caught by that
schema and by the conformance corpora, not only by reading four files.

**Not a third status, and not a new member.** The objection is not that a third
value would break this runtime's own readers. `NewExpectationReport` reads
`if finding.Status != "valid" { status = "invalid" }`, so a row carrying
`unreachable` would already aggregate to `invalid`; and a client branching on
`valid` versus anything else would take the refusal branch it already has. The
objection is the contract and the dispatch. ADR-0035 wrote two statuses down —
"the aggregate is `valid` only when every finding is valid" — and
`result.ExpectationFinding` gives a valid and an invalid finding disjoint
members, so "a reader can tell the two apart without consulting the aggregate";
`docs/mcp-clients.md` describes exactly those two entry shapes, the valid one
carrying `canonical` and the invalid one a `code` and a `message`. A third
status would have to declare which of those two member sets it carries, and
would carry no member the code does not carry already. ADR-0035 also established
with `JPS-EXPECTATION-LIMIT` that a refusal *class* is told apart by `code` and
never by `status`; a second mechanism for the same question is one a client has
to choose between. And the readers a third value does break are the ones that
switch on `status` exhaustively rather than on `valid`-versus-else — the shape
the disjoint-member contract invites, because the two arms read different members
— which have no arm for it and fail closed or open by their default. An unknown
`code` cannot do that: it arrives on a row whose `status` already says `invalid`.
A `reachable` boolean would put a second, silently defaulted axis on a finding
whose meaning is already carried by one string, and a client that did not read
the new member would treat an unreachable row as a plain §8.3 defect — the same
failure as doing nothing. The code carries it instead, exactly as
`JPS-EXPECTATION-LIMIT` carries "not admitted, rather than prohibited by Core".
Clients branch on `code`.

**No `canonical`.** An unreachable row carries none. `result.ExpectationFinding`
makes a valid and an invalid finding carry disjoint members — "Neither carries the
other's members, so a reader can tell the two apart without consulting the
aggregate" — and the canonical text is precisely what a client stores and
compares, which is the one thing it must not do with an expectation no pack can
produce.

**Order.** The reachability check runs after the §8.3 grammar gate and only when
that gate passes. A row that is both malformed and unreachable is reported for its
grammar defect and keeps `JPS-EXPECTATION-INVALID`; a row that is legal §8.3 and
unreachable is reported `JPS-EXPECTATION-UNREACHABLE`. A shape that is not a
disposition at all has no reachability to speak of, and the author cannot act on
the second diagnosis before the first. The order is stated in the tool description
and in `docs/mcp-clients.md`, not left to be inferred from a message.

**Not in the decoder.** `evaluation.DecodeDisposition` is unchanged. It is the
§8.3 grammar gate every matrix, graph, coverage and corpus reader calls, and one
of those readers depends on admitting the very shape class 1 refuses.
`internal/project/coverage.go` reads it as a witness: "A step-1 halt is proven by
the not-applicable kind OR by an unresolved carrying the not-applicable reason:
the disposition grammar pins the reason set only for the kind, so the reason
spelling decodes and must mean the same halt here." `internal/project`'s tests pin
that spelling as a distinct table case, `not-applicable-as-reason`, carrying
`{"kind":"unresolved","reasons":["not-applicable"],"handoff":{"state":"none"}}`
under a comment reading "The same halt spelled as an unresolved carrying the
not-applicable reason".

What moving the rule into the decoder would cost is not a passing row turned
failing. A stored row spelled that way already fails its comparison today.
`internal/evaluation/resolve.go` records the `not-applicable` reason on the line
before it disposes under kind `not-applicable`, and nowhere else, so no
evaluation produces that reason under `unresolved`; the row comparator compares
canonical bytes — `if outcome.Actual != outcome.Expected` — and reports "The
canonical disposition bytes differ." That was measured rather than reasoned:
changing the bundled `not-applicable-request-type` corpus case's expected `kind`
to `unresolved` and changing nothing else leaves it decoding and makes
`evaluate-corpus` answer 19/20, with that detail and no other. The costs are
different ones, and two of them decide it. The refusal would move from the
comparison to the decode: a decode refusal naming the reachability rule, raised
by every reader of a stored expectation — including the ones that only derive
and never compare — in place of a mismatch naming the bytes that differ. Where
it is raised depends on the reader. A pack-matrix or corpus row and a graph's
headline expectation are decoded before the row is evaluated, so there the
refusal would come before any evaluation runs; a graph node's expectation is
decoded only after the graph has run, so there it would come beside the
completed trace. That is a compatibility cost on rows that are legal §8.3 today,
and at the same time a worse diagnosis, because a reader that only derives
coverage would refuse a row it was never going to compare.

The third cost is a coverage loss, and it is a migration rather than a
derivation an author cannot recover — which is why it is not the cost that
decides. `internal/project.DecodeWitness` returns `ProbeWitness{}, false` for a
text that does not decode, and its callers take a witness only when that bool is
true, so a row left in the `unresolved` spelling would contribute no probe
witness at all and the applicability-stage coverage it contributes today would go
with it. An author recovers that witness by correcting the stored row rather than
by rewording it: the same disposition under `"kind":"not-applicable"`, carrying
the same sole reason, is admitted by the rules this record decides, and
`internal/project/coverage.go` reads the two spellings as the same step-1 halt —
`halted` is `witness.Kind == "not-applicable"` OR the not-applicable reason, and
the applicability stage is exercised by any witness that decodes — so the
corrected row witnesses exactly what the old one did. `internal/project`'s table
pins that equivalence: `not-applicable` and `not-applicable-as-reason` are two
cases with identical expectations, covered applicability and neither the
exception nor the rule stage reached. So the honest statement of this cost is a
migration on every stored row of that spelling, and the reasons that decide
against the decoder are the compatibility and diagnostic-timing ones above.

The distinction is real rather than convenient: the decoder answers "is this a
§8.3 disposition?", which a stored row must be; the tool answers "can any pack
produce it?", which only a *proposed* row has to be. The three rules are
therefore stated once, in the one place whose question they answer.

**Aggregate.** Unchanged: `NewExpectationReport` makes the aggregate `invalid` when
any row is not valid, and an unreachable row is not valid. `internal/result` gains
no new type, no new member and no new rule; only the `ExpectationReport` doc
comment changes, because its sentence "Status is about admission, never about
reachability" is now half false — pack-independent reachability does enter it, and
pack-dependent reachability still does not.

**Pack-dependent reachability is still not checked**, and the surfaces say so.
Whether an admitted `outcomeId` names a declared outcome of *this* pack, and
whether a handoff agrees with its escalation object, need the pack this tool never
sees. A valid finding therefore stays necessary and not sufficient; what changes is
that the part of "not sufficient" that never needed a pack is now reported rather
than only documented.

### Consequences

- **Compatibility and migration:** the new code is additive on an experimental
  surface. No status, member, type or aggregate rule changes, and a client that
  ignores `code` sees only that a row it was told was valid is now invalid. An
  expectation that is one of the three shapes was reported `valid` before and is
  reported `invalid` with `JPS-EXPECTATION-UNREACHABLE` now; the repair is to the
  expectation, and the message names the rule to repair it by. Nothing stored or
  evaluated changes, because no pack produced any of these shapes: the shared
  decoder, the matrix, graph, coverage, profile and corpus readers, and what
  `packs test` accepts in a stored row are all untouched, which the unchanged
  `internal/evaluation` and `internal/project` trees make checkable rather than
  claimed. Older runtimes emit only the two ADR-0035 codes, so a client that must
  work against both treats an unknown code as an unrepaired refusal.
- **Automation:** a blocked-admission record can now name a pack-independent
  impossibility, which is the one case where a repair loop against the candidate
  is guaranteed to be wasted. A client branching on `code` can stop that loop and
  raise the expectation for correction instead. The tool still supplies no
  corrected expectation and still decides nothing about a pack. Desk's
  `findingSummary` branches only on `JPS-EXPECTATION-LIMIT` today and will present
  an unreachable finding as a §8.3 defect to correct; that is a desk follow-up,
  not a runtime one, and the wording it needs is in the message it already shows.
- **Authority:** the tool gains none. It stays read-only over text in memory,
  reaches no pack, project, evaluator, audit record, source, credential or
  network, and every rule it applies is one the specification states about a
  disposition or an identifier — the §8 step order and the §5 grammar, quoted in
  the message rather than paraphrased. The one authority it explicitly does not
  take is the pack's: it does not hold an `outcomeId` to a pack's declared
  outcomes or a handoff to a pack's escalation object, because it has no pack to
  hold them to. ADR-0035's deferral of "a separately coded reachability finding …
  belongs to the tool rather than to the decoder" is discharged here, in the place
  that record named. The evaluator and the specification formats do not change.
- **Security and privacy:** no new input, no new output channel, no new
  dependency and no persistence. The three rules read members the tool had already
  decoded and bounded: the ADR-0035 limits — 16 KiB, depth 16, 1,024 nodes, 8 KiB
  per string, 1–256 expectations — are applied before any of this runs. The
  identifier grammar is a linear, anchored, backtracking-free pattern over a
  string already bounded at 8 KiB. An offending `outcomeId` is quoted back to the
  caller that sent it and to nobody else, and it is quoted whole rather than
  truncated, exactly as the §8.3 refusals beside it quote an unadmitted kind or
  reason; truncating only the new one would make two refusals in the same
  response disagree about whether the author gets to see what they sent. The
  echo is bounded, but not by one number and not by 32 KiB: the message, and each
  of the two payloads that carry it, are three different sizes with three
  different worst cases, and a client sizes a limit from the largest of them.
  `carrier.Limits`'s `MaxStringBytes` is measured on the *decoded* string and the
  16 KiB input bound on the JSON text, so the largest identifier admitted is
  8,192 single-byte characters, arriving in 8,265 bytes of expectation text.
  Which character is the worst one depends on who writes it next, because
  `toolResult` writes the report twice and the two writers escape different
  characters: `structuredContent` is written by the server's encoder, which is
  configured `SetEscapeHTML(false)`, while `content[0].text` is
  `jsonText(structured)` — `json.Marshal`, with HTML escaping on. The three
  measured figures, each at its own worst identifier and at index 255, the
  largest index a 256-expectation batch can report:
  - **The decoded message, 32,936 bytes**, which carries no index and so has one
    figure. Worst identifier U+007F: one byte decoded, one byte of input text and
    four bytes under `%q`, so 8,192 of them quote to 32,770 bytes inside a fixed
    sentence of 166 — over 32,768, under 33 KiB.
  - **The finding inside `structuredContent`, 41,214 bytes.** Worst identifier
    U+007F again, at five bytes per character, because JSON escapes each of the
    8,192 backslashes `%q` introduced, and the four quotation marks, inside an
    82-byte envelope. HTML escaping is off on this path, so U+003C costs one byte
    here and the finding is 8,446 bytes.
  - **The finding inside `content[0].text`, 49,406 bytes.** Worst identifier
    U+003C — or U+003E, or U+0026 — at six bytes per character: `%q` leaves it
    printable, and `json.Marshal` then writes it as a six-byte escape. Six is
    more than U+007F's five, so the worst identifier flips between the two paths,
    and this figure is the largest of the three: it is the one a client sizing a
    limit has to size it from. U+007F reaches 41,214 bytes here too.

  All three are measured on the bytes the server wrote — at both indexes and under
  both identifiers — by `TestUnreachableFindingIsBoundedOnEachSerializationPath`,
  which locates each finding inside the payload rather than re-serializing one of
  its own, and asserts every figure twice: once derived from the message and the
  escaping rule, so a changed escaping fails it, and once against the literal
  written here, so a reworded sentence fails it. A fourth size is not one of the
  three, and no bound is stated for it, because it is not a finding: the text
  block is itself a JSON string in the response, so each escape inside it is
  escaped once more, and it grows with every other row the batch carries. For a
  one-expectation call the test measures it at 57,848 bytes under both of the
  identifiers above, at seven bytes per character — a figure for those two
  identifiers, not the largest one row can reach: an identifier chosen for that
  extra layer, such as one of backslashes, which `%q` doubles and each JSON
  layer doubles again, makes the block larger.
- **Validation:** native wire tests, in the shape ADR-0035 established. Each new
  fixture in `internal/mcp/testdata/expectations.json` is named for the rule it
  pins, states the `message` fragment the finding must contain, and now states the
  `code` it must be reported under, so a fixture written for §8's step order
  cannot pass by being refused as a grammar defect or the reverse; the fixture
  default is `JPS-EXPECTATION-INVALID`, which is what every pre-existing fixture
  means. Negative controls hold the boundaries the rules must not cross:
  `not-applicable` as the sole reason under kind `not-applicable`, `no-match`
  alone and duplicated, and a hyphenated `outcomeId`, all still valid; a
  grammar-failing `outcomeId` under a non-outcome kind still
  `JPS-EXPECTATION-INVALID`, because there the defect is that `outcomeId` is
  present at all. A malformed-and-unreachable fixture pins the order, and a
  fixture retaining both `not-applicable` and `no-match` pins which of the two
  overlapping classes answers — swapping those two arms changes neither the status
  nor the code, only the repair the author is told to make. A separate wire test
  holds the finding on its own — aggregate `invalid`, `code` and `message`
  present, `canonical` absent — because asserting that inside a batch already
  invalid for two dozen other reasons proves none of it. The
  description-to-behaviour test is extended to hold the tool's advertised text to
  naming the new code, each class it reports and the order sentence whole, to
  compare the advertised code against the code the tool emits, and to compare the
  advertised grammar against the compiled pattern the tool applies. Two further
  tests carry the sizes: one sends the maximally escaped identifier the carrier
  admits and holds the decoded message to an exact figure, derived from the
  message template and written again as the documented literal, with a
  one-character-longer identifier held to the string limit beside it, which is
  what makes that identifier the worst case for `%q`; the other sends both worst
  identifiers, at index 0 and at index 255, and holds each of the three figures
  the Security and privacy consequence states — the decoded message and the
  finding on each of the two serialization paths — against a derivation and
  against the documented literal, measuring the finding where the server wrote
  it rather than re-serializing one of its own. Each guard was mutation-checked:
  each of the three checks deleted, the order reversed in the code and separately
  inverted in the advertised text, the two overlapping arms swapped, the code
  dropped, the no-canonical decision neutralized, the reported index changed, the
  HTML escaping of the text block switched off, and the message template
  lengthened, each against the test paired with it.

Material impact is public-surface (a new finding code on an advertised MCP tool)
and documented-claim (the tool description, `docs/mcp-clients.md` and ADR-0035's
stated gap all change what this runtime says it checks). Cross-vendor review and
maintainer dispositions are required on the introducing PR before merge, under the
repository's existing review regime.

## More information

- [ADR-0035](0035-validate-proposed-expectations-before-admission.md), whose
  Authority consequence states the gap and defers this finding to the tool.
- `internal/mcp/expectations.go` (`unreachableExpectation`, `localIdentifier`,
  `unreachableOutcomeIDRule`), `internal/result/result.go`
  (`ExpectationFinding`, `ExpectationReport`, `NewExpectationReport`).
- `internal/evaluation/resolve.go` and `internal/evaluation/corpus.go` for where a
  stored `unresolved` retaining `not-applicable` is refused today, and
  `internal/project/coverage.go` (`DecodeWitness`, `siteStage.exercisedBy`) for
  the witness a decoder rule would remove from an uncorrected row, and for the
  correction that recovers it.
- JPS Core `0.2.0-draft` §5, §8 steps 1, 5, 8 and 10, and §8.3.
