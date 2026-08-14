# Changelog

All notable changes to tagged releases are documented here.

## Unreleased

- **The declared graph matrix runs over MCP** (ADR-0026): `experimental_test_graphs` runs every
  configured graph's matrix, or one by its configured key in `graph_id`, and returns exactly the
  payload the graph project walk emits. It closes the reopening condition ADR-0021 recorded when
  it deferred the graph twin. An optional `supported_extensions` list applies uniformly to every
  node of every row; omitting it and passing an empty array are the same run. Like the packs
  tool it writes nothing — no audit record (ADR-0018) and no reviewed-set consultation
  (ADR-0019), which are two independently guarded invariants rather than one — and a mismatching
  or skipped run is a successful call carrying its status.
- **A graph matrix report is bounded as it accumulates, not after it exists** (ADR-0026):
  `graph.Options.ReportBudget` charges every component a run retains — each row's report and each
  graph's coverage block — and refuses at the point the total passes the budget, leaving the
  remaining rows unevaluated. A graph
  matrix multiplies where a pack matrix does not — up to 10,000 rows, each re-evaluating up to 64
  nodes and retaining every node's canonical disposition — so a report can reach gigabytes before
  a check on the marshaled response could see it, which is the shape ADR-0025 names as a defect.
  The CLI leaves the budget unset and streams to a terminal an operator can interrupt.
- The conformance claim surface grows from seven to eight; `CONFORMANCE.md` and its mechanical
  enumeration test name the new tool.

## 0.17.0 - 2026-08-09

- **A matrix row can assert the handoff target, because no disposition can carry it** (ADR-0025):
  a `packs test` row may declare `expectedHandoffTarget`, the one further assertion beside
  `expectedDisposition`. It exists because JPS §8.3 keeps a pack's configured escalation target
  **outside** the disposition — this runtime reports it beside one, as `handoffTarget` — so a pack
  edit reaching only `escalation.target.name` leaves `kind`, `outcomeId`, `reasons`,
  `handoff.state`, and `handoff.triggeredBy` byte-identical and every row of a disposition-only
  matrix still passes. That is Study 013's holdout cell h02, written adversarially by that study's
  cross-vendor reviewer and caught in the study only downstream, once the corrupted target had
  already reached a tool argument. The member has three states and they are three different
  statements: an object with `kind` and `name` (both required, neither empty) asserts that exact
  target by string equality; the literal `null` asserts the evaluation reports **no** target; and
  **absent asserts nothing**, so a matrix written before this release — where it was otherwise valid,
  in the sense the two carrier fixes below narrow — is judged by byte-identical behavior and its row
  results carry no new member. It is an **assertion and not a coverage line** —
  a mismatch moves the row, the pack entry, the run, and the exit code exactly as a disposition
  mismatch does, which is why ADR-0014 and ADR-0023's never-gate stance for *derived* probes is
  untouched and unamended. It rides beside `expectedDisposition` only, refused at load on the graph
  surface's `expectedNodes` precedent: a refused evaluation reports no target to compare. Both sides
  are rendered by one writer (`result.HandoffTarget.Canonical`), reported as `expectedHandoffTarget`
  and `actualHandoffTarget` on the row and printed as one line beside the disposition pair; the
  mismatch detail names which side states a destination rather than repeating two authored strings a
  third time. Folding the target into `expectedDisposition` was refused because that section's
  composition is normative and the target's separateness is what makes a disposition portable;
  always asserting it was refused because it breaks every existing matrix and makes the runtime
  compare the pack against itself. What it holds is the target the pack *configures* — no delivery
  is observed, and a green row does not make the destination the right one.

  **`matrixVersion` moves to `"2"`, and this is the compatibility rule.** A matrix is a **closed
  input** under VERSIONING.md — an older reader *rejects* a document carrying a member it does not
  know rather than ignoring it — so adding a member moves the version whatever the addition is,
  exactly as `graphs` moved `configVersion` to `"2"`. A matrix declaring `"1"`, and a matrix
  declaring no version at all, is read as the shape it was written for and — where it was
  **otherwise valid**, in the sense the two carrier fixes below narrow — is **unchanged in every
  byte**; a row in one that asserts a target is refused by name, naming the version it would take.
  Only `"2"` admits `expectedHandoffTarget`. The declared version is settled before anything
  version-specific decodes, so a document from a future version gets a refusal about the version
  rather than about a member. `outputVersion` stays `"2"` — that is the output side, where a
  consumer ignores what it does not know, and the two rules are deliberately decided separately.

  Two defects older than this change were found by its adversarial review and fixed with it. **A
  "closed" matrix shape was accepting case-folded aliases**: `encoding/json` matches member names
  case-insensitively even under `DisallowUnknownFields`, so `{"Facts":…}` decoded into `facts` and a
  document carrying both spellings had one silently overwrite the other. Member names are now
  checked against the carrier-decoded document, where the authored spelling still exists, and an
  alias is refused rather than read — for every row member, not only the new one. **The carrier was
  silently repairing invalid Unicode**: Go replaces an unpaired surrogate escape such as `"\ud800"`
  with U+FFFD without complaint, so two different documents could canonicalize to the same bytes and
  a byte comparison §8.3 requires to be exact quietly stopped being one. RFC 8785 §3.2.2.2 makes
  such a value invalid rather than replaceable, and `carrier.Decode` now terminates on it — for
  every pack, matrix, facts, evidence, configuration, and graph document. An escape and the literal
  it names are still one string; NFC and NFD are still two.

  Both reported renderings are **bounded**, at 256 bytes with a SHA-256 digest tail on ADR-0023's
  shape, and the run carries a **4 MiB aggregate budget** charged as each row's result is composed:
  a pack may configure a target name §2.1 admits at a megabyte and a matrix may declare ten thousand
  rows, which uncapped is a report in the gigabytes built from inputs every carrier limit accepts.
  Crossing the aggregate budget refuses the run and writes nothing, because a report cut short looks
  exactly like a complete one. A capped rendering is a **display value and never an equality key**:
  the comparison is decided on the decoded targets — presence, then each member in full — because
  sixty-four bits of digest deciding whether a suite passes would be a probabilistic answer to a
  question with an exact one. And because a budget on *retained* bytes says nothing about the work
  spent producing them, the pack's target is **rendered once per pack per run** — where the pack is
  loaded, above the row loop, and only when some row asserts one — and handed to every row as a
  value: §8.1 gives a pack one escalation target, so rendering per row makes ten thousand rows
  against a megabyte-long name ten gigabytes of repeated canonicalizing and hashing that no
  retained-bytes budget can see. Caching it instead of hoisting it is not enough, and the attempts
  are recorded because each failed the same way: keyed on the target's *content*, a run mixing two
  capability sets compares an equal megabyte per row, because `supportedExtensions` selects a
  distinct admission and each admission decodes the pack separately; stored on the *admission*, a
  matrix that uses sixty-four one-off capability sets and repeats a sixty-fifth re-renders on every
  remaining row, because `maxAdmissions` bounds what is retained and not what is computed. The
  rendering is a function of the pack's bytes alone, so it belongs where a pack is loaded; the row
  path then holds no cache, no lock, and no counter. The rendering type is **opaque and bound to the pack it
  was minted from** — unexported members, `PackHandoffTarget` as its only constructor and the only
  place a target is rendered, and the SHA-256 of the pack's bytes carried alongside so a row uses a
  rendering only when it belongs to the pack it evaluated. So "the row path renders nothing" is
  enforced rather than documented, and a rendering belonging to another pack degrades to
  `unavailable` instead of putting a destination that pack never declared into the report. The
  binding is a digest rather than a comparison of the decoded targets, deliberately: both operands
  of that comparison come from the *pack*, so it would scan a megabyte-long target once per row —
  unlike the row-versus-pack comparison the verdict makes, which the matrix's own byte limit bounds.
  Thirty-two bytes, whatever the pack weighs, checked ahead of everything else the handle carries so
  a foreign one degrades the report rather than failing the row. A rendering minted elsewhere over
  the *same* bytes reports honestly, since the same bytes declare the same target. **An admitted
  pack is now a snapshot**: `AdmitPack` copies the bytes it is given and answers every later
  question from that copy, because a digest of bytes the caller can still edit binds nothing — edit
  the slice in place into a same-length pack after admitting it, and the stale digest would accept
  the first pack's rendering while the evaluation decoded the second. The pack byte limit is decided
  once, in `AdmitPack`, ahead of both the copy and the digest, so an oversized pack is refused
  rather than scanned by work this change added; every evaluating path replays that one decision,
  and `Admits` keeps the conformance-only semantics it documented long before. A pack's hard byte limit is
  consulted before any of this decodes a pack, so an oversized document is not scanned first — and
  the refusal itself still travels the ordinary row path, so every expectation is canonicalized and
  every payload keeps the shape it had. The actual side of the pair carries a third value, **`unavailable`**
  (`unavailable (evaluation refused)` on the human surface), for a row whose evaluation was refused:
  reporting `null` there would say an evaluation reported no target when no evaluation happened. The
  **graph** surface is deliberately unchanged and its matrices stay blind to a target-only pack edit;
  a graph row stating the member is refused, and the deferral and its remaining gap are recorded.
  The conformance claim is
  unaffected and stated, in full and only, in `CONFORMANCE.md`, which no line of this entry
  restates.

## 0.16.0 - 2026-08-07

- **Replaying a decision: pin the tuple, not the pack**: `docs/building-with-packs.md` gains a
  closing section recording the replay discipline issue #93 surfaced — a pack hash alone does not
  make a decision replayable, because JPS §11's exactness means a later evaluator correctly
  refuses an unedited older pack. The unit of replay is three facts recorded together: the pack's
  SHA-256, the evaluator release that ran, and the executable's digest; old releases stay
  published so replay is a fetch-by-tag, verify-checksums, never-use-latest operation. The
  `JPS-EVALUATION-PACK-SPEC-VERSION` diagnostic now names that path beside the one-edit
  re-declaration it already explained, so the guidance appears at the moment of refusal.

- **Derive candidate test-row inputs from a pack's own literals, and never their expectations**
  (ADR-0024): a new `jpack packs suggest` reads the packs a project declares and emits a
  `candidatesVersion`/`candidates` document — facts documents at the values each pack's own
  conditions imply, with an id, `origin: "generated"`, sometimes an `evidenceAvailability`, and a
  rationale: a sentence saying what the candidate places and which declaration implies it, closed by
  the sentence every candidate ends with — no expectation is stated, write one from the policy text
  or delete this candidate. It carries **neither
  `expectedDisposition` nor `expectedErrorClass`**, and that absence is the design rather than an
  omission: an expectation is the member that says what a pack *should* decide, deriving one from
  the pack would be ADR-0014's circular oracle. Nothing it emits can be scored, through refusals the
  matrix loader already makes and this change neither adds nor can relax: a candidate pasted
  **verbatim** into a `cases` array is refused first for its `rationale`, a member of no row, which
  fails the loader's strict decode before any row is examined — the layer that keeps the generator's
  prose out of anything scoreable — and with the rationale removed it is refused again, by name, for
  declaring neither expectation. A sentinel was refused for the same reason it is tempting: a slot
  invites a fill and the fill is one token, where absence makes a reviewer author the expectation
  from the policy text — `kind`, `outcomeId`, `reasons`, and `handoff` for an outcome disposition,
  which is the shape of the authoring act rather than a check the loader performs; what it enforces
  is the weaker "exactly one of `expectedDisposition` and `expectedErrorClass`". The emitted document is not a
  matrix either, so a `jpack.json` matrix path aimed at a raw candidate file is refused twice over.
  Per fact pointer the values are the compared literal itself, one unit either side of it at the
  precision the pack authored it in (`"5000"` steps by 1, `"70.0"` by 0.1, clamped at six digits and
  said so), the midpoints of adjacent literals — which invent no granularity, because 2 divides 10
  and the midpoint of two terminating decimals is itself one — and one unit outside the outermost
  literals: at most `4n+1` values (`6n+1` under `--include-hugs`, off by default, which adds the
  pair two decimal places finer than each literal's authored precision — under the same six-digit
  floor the step carries, so a literal authored at five digits is hugged one place finer instead of
  two and one at six or more gets no pair, both reported rather than delivered quietly),
  deduplicated by the
  evaluator's own decimal identity so `70` and `70.0` derive one lattice. Because that makes them
  one boundary, the step is read off the **finest** spelling any of the boundary's sites authored
  rather than off whichever rule declared it first: otherwise reordering two rules would change the
  lattice, and a derivation must be a function of what a pack says and not of the order it says it
  in. Each stated member of an `in`, `equals`, or `not-equals` operand gets a
  candidate too, the negative witness is an *absence* rather than an invented non-member, and the
  three tri-states of each declared evidence requirement are a separate axis never crossed with the
  numeric one. Composition is **one factor or axis at a time**: a value or membership candidate
  varies exactly one pointer and holds the rest at a base assignment — `--base <rowId>` makes that an
  already-reviewed matrix row, so a candidate reads as "this reviewed row, with one pointer moved" —
  an evidence candidate varies no pointer at all, and with no base the single absence candidate
  states no facts, because there is nothing to hold the other pointers at. A run's size is therefore
  the sum over pointers and axes and never their product, because volume is what turns review into
  rubber-stamping. A plausible-looking full
  record is never synthesized: that is the generator inventing a policy world. This does not reverse
  ADR-0023's refused option E, and the record states the hinge rather than leaving it implied: E was
  refused as a *demand*, "a probe built on it would demand a row at a value nobody can justify",
  where a generated value is an *offer* that moves no probe, no status, and no exit code and costs
  one delete. The command runs no evaluator, derives no expectation, and gates nothing: nothing is
  written unless `--write <file>` or `--write -` says so, a destination the configuration declares as
  a pack, matrix, graph, or `rows` document is refused **by name** — the exclusive open cannot make
  that refusal, because it would happily create a declared matrix that does not exist yet, and the
  check resolves both ends through their symlinks, the destination *and every declared path*, so
  neither an aliased root nor a configuration that names its own documents through an alias is a
  second spelling that walks past it — and past `--max` (500) the run
  refuses rather than truncating, since a truncated candidate set looks exactly like a complete one.
  The cap is charged as candidates are composed rather than read off the finished document: a cap
  checked at the end would bound what is returned and nothing about the work done to reach it.
  A non-positive `--max` is refused by name rather than read as the default, because a caller who
  asked for at most zero candidates and silently received five hundred was not answered. There is a
  16 MiB bound on the emitted document beside the count, on `MaxMatrixBytes`' own footing and for a
  reason the count cannot cover: every candidate carries a whole facts document, so a run's size is
  the candidate count times its base row, and a base row is bounded only by `MaxMatrixBytes`. It
  bounds the *written* form rather than a compact measure of the same values, because indentation
  multiplies with nesting depth: a base row a hundred containers deep wrapping very many
  one-character tokens is legal on every carrier bound, under a megabyte composed, and hundreds of
  megabytes written. It is charged as each candidate is composed rather than measured at the end, so
  at most one candidate's encoding is held while it is measured and the document's own bytes are
  never accumulated; crossing it refuses whole — naming a **bound** rather than a measurement, since
  each candidate is charged its written encoding plus a fixed envelope, alongside the budget and what
  each remedy actually does — rather than writing a partial document. A pack using draft RFC 0008 collection quantifiers has that dimension reported
  as skipped, never silently omitted. Two runs over an unchanged pack write identical bytes.
  Alongside it, `packs test` reports a per-pack `origins` count of the rows by the origin each
  declares, in both formats: `origin` was already a member of the case carrier and already loaded
  silently while meaning nothing, and it now means provenance and is **counted, never gated** — a
  gate would be defeated by deleting one member, and would teach the deletion that destroys the only
  signal measuring how much of a suite a generator supplied. The count is an added member, so
  `outputVersion` stays `"2"`. `evaluation.DecimalString` is a new export because `DecimalKey` cannot
  render — it returns big.Rat's canonical form, `"81/2"` for 40.5, which §7.4 declines to compare —
  and it refuses rather than rounds any value whose decimal expansion does not terminate;
  `DecimalValue` and `PointerTokens` are exported beside it so this package holds no second decimal
  admission and no second implementation of the `~1`/`~0` escapes. The walk ADR-0023 introduced is
  split into an enumeration half and a leaf half so the generator's membership sites reuse it without
  widening it, held to a pure refactor by a golden over every fixture: widening a shared walk would
  widen a demand. The record is honest about what it does not fix — this makes coverage cheaper to
  reach, and cheaper coverage under unchanged review discipline is worse-justified coverage — and
  registers the falsifier that would show it: an acceptance rate near 100% for generated candidates
  refutes the premise. Generated rows get no special credit in the coverage report, because that is
  the metric that would reward the rubber-stamp. CLI-only in this cut: an MCP tool handing an agent
  candidate rows and an expectation-shaped hole in one turn is the rubber-stamping vector, and
  ADR-0021 set the precedent of a CLI surface lived with first.

- **Derive a boundary probe for every ordered comparison** (ADR-0023): the coverage report beside
  `packs test`'s rows gains a second probe family, `boundary:<pointer>:<literal>` — one probe per
  distinct fact pointer and decimal value a pack's own conditions compare with `greater-than`,
  `greater-than-or-equal`, `less-than`, or `less-than-or-equal`, covered by a row whose facts place
  that pointer's value exactly at the literal. It catches the defect nothing else here can see: a
  rule described as "5000 or more" that compares `greater-than "5000"` is two individually valid
  members disagreeing at exactly one input, and a suite with rows at 4999 and 5001 is green while
  saying nothing about which reading the pack carries. Identity is per pointer and value, not per
  site and not per operator — sites sharing them ask one question of a row's facts, so a threshold
  compared in three places with two operators is one probe, and `70` and `70.0` are one boundary
  because equality is the evaluator's own decimal comparison. A JSON number at the pointer witnesses
  nothing, because §7.4 cannot compare it at all. The witness is expectation-gated as well as
  facts-located: a row expecting a §8.4 error class, a row whose expectation does not decode, and a
  row whose facts do not decode each state nothing about the boundary. The expectation also decides,
  from §8's evaluation order, whether that row's evaluation could have reached the comparison at
  all, and §8 reads a pack's conditions at three points with a different halt in front of each:
  `applicability` (step 1) is exercised by any row whose expectation decodes; an exception's `when`
  (step 3) by every row except one expecting `not-applicable`, because step 2 records missing
  evidence without returning and §8 halts on it only at step 5, after every exception effect has
  been inspected; and a normal rule's `when` (steps 6-7) only by a row whose expectation does not
  prove that step-5 halt — a retained `missing-required-evidence` proves it whatever else the reason
  set contains, and so does a retained `exception-escalation`. A merged probe is covered only when
  every stage its sites sit at has an eligible row at the literal, so a rule-sited threshold cannot
  read covered because some row settled an applicability-sited copy of it. Each rendered pointer,
  literal, and declaration id is capped at 128 bytes with a SHA-256 tail beyond that, so the
  report's size follows the pack's thresholds and not the megabyte the carrier allows each of those
  strings — without the cap, a pack inside every carrier limit could push the report past the MCP
  surface's response bound and turn a call that used to succeed into a refusal. The walk is
  structure-keyed — `applicability`, `rules[].when`, `exceptions[].when`, descending only through
  `all`, `any`, and `not` — so a condition-shaped object carried inside a `value` literal or an
  `extensions` slot derives no probe: an over-reported probe is a demand for a row nothing could
  satisfy. The graph surface derives none of this family, because an edge may inject the very fact a
  node compares. Coverage still informs and never gates: a missing boundary moves no status, no
  summary, and no exit code, and the probes are new values in the existing `coverage` array, so
  `outputVersion` stays `"2"`. One human line changes — the count sentence now reads "*n/m* derived
  probes are witnessed by a row", true of both families. This record also narrows the scope of the
  affirmative half of ADR-0014's witness determination — "witnessed by what a row expects" now
  states the disposition family's rule rather than a universal — and keeps its prohibitive half,
  "never by what it produced", verbatim for both: a row's facts are an authored input in the row
  document, not a product of the evaluator. The `test_pack` prompt's ordered-comparison *type* probe
  is deliberately still not derived; it is a named follow-up. The conformance claim is unaffected
  and stated, in full and only, in `CONFORMANCE.md`.

## 0.15.0 - 2026-08-05

- **Lint every consulted pointer against a producer declaration** (ADR-0022): a new `jpack packs
  lint` holds the packs a project declares to a producer declaration — every fact pointer a pack's
  conditions consult must be covered by a producer (at the pointer or an ancestor, the declared
  subtree contract; or at a descendant, which structurally makes the ancestor resolvable), every
  declared evidence requirement must have a supplier, and nothing may be supplied that nothing
  declares — per pack in hints mode, project-wide for a manifest's evidence list, because
  evidence ids are pack-local names. Without `--producers`, the configuration's own facts and
  evidence hints are the declaration, read in the direction `packs validate` never checks:
  validate holds every hint to the document, and lint holds the document to a producer. With
  `--producers`, an explicit manifest (`{"producersVersion":"1","facts":[...],"evidence":[...]}`,
  a closed input decoded through the strict carrier with its value domains checked, `-` for
  stdin) is the declaration instead, for an application whose producer set is wider than its
  hints. The defect this catches never errors at run time — a consulted pointer no source feeds
  is merely unknowable, every rule touching it escalates, and the system looks conservative
  rather than broken — which is why it is a build gate and why its status follows `packs test`'s
  discipline: a failed check exits 1, an unreadable declared document fails rather than skips,
  and a run in which nothing was checkable is reported skipped, never passed, and exits 1 too. A
  pack using the known draft-RFC collection quantifiers reports its fact half as a named
  `quantifier-scope` skip rather than either verdict, on ADR-0020's recorded narrowness — and
  the detection is operator-keyed so a data literal cannot silently suppress the gate. The
  payload is its own type with a skipped-carrying summary and `outputVersion` stays `"2"`. The
  conformance claim is unaffected and stated, in full and only, in `CONFORMANCE.md`, which no
  line of this entry restates.

## 0.14.0 - 2026-08-05

- **Run a project's instance matrix over MCP** (ADR-0021): a new `experimental_test_packs` tool
  runs every declared pack's instance matrix — or the one an optional `pack_id` names — through
  the same evaluator and the same comparison `jpack packs test` uses, and returns the payload that
  command already emits: each row's agreement or divergence, by the RFC 8785 canonical disposition
  compared byte for byte or the §8.4 error class and phase the row expects, with the derived
  coverage report of ADR-0014 beside each pack's rows, informing and never gating. The method for
  building a matrix has been served over MCP since ADR-0008 and the means to run one had not: a
  client with no shell in front of it was taught a regression discipline it could not execute, and
  the substitute — replaying rows through `experimental_evaluate` and comparing client-side —
  re-implements the comparison and drops the coverage report, which under-reports silently. The
  posture is unchanged: the tool reads the project tree the server was launched in, takes no path
  over the wire, holds no credential, opens no connection, and writes nothing at all — a matrix
  row is a rehearsal and not a decision, so it appends no audit record (ADR-0018) and consults no
  reviewed set (ADR-0019), the same split both records drew. A mismatching or skipped run is a
  successful call carrying its payload; tool errors are kept for what stopped the run from
  happening — a bad argument, an unknown decision id, a configuration that is there and will not
  load, or no configuration at all — while a pack or matrix unreadable inside a run is that pack's
  own in-band mismatch, exactly as the CLI reports it. A marshaled report over 16 MiB is refused
  with its size and the CLI command that streams the same report, never truncated: a truncated
  suite report would under-report silently. Both surfaces' payloads gain one member,
  `evaluatorSpecVersion`, so the applied contract version stays in band even for a run whose every
  pack was skipped. `outputVersion` stays `"2"`, and CONFORMANCE.md's claim-scope
  enumeration now names seven surfaces rather than six, held mechanically by the test that counts
  evaluator constructor sites; the conformance claim itself is unaffected and stated, in full and
  only, in `CONFORMANCE.md`, which no line of this entry restates.
- **Report the fact pointers a pack's conditions read** (ADR-0020): every `packs list --format
  json` row and every MCP `list_packs` row now carries `consultedFactPaths` — the pointers the
  pack's conditions read, sorted and deduplicated, from the same whole-document walk `packs
  validate` holds a configuration's fact-hint keys to, which recognizes a condition by carrying
  `op` and a string `path` rather than by the operator names this runtime happens to know. The
  list was computed and deliberately withheld, and the consequence was that a consumer needing it
  re-walked the document with a walk keyed to the operators it knew — which under-reports the
  moment a pack uses another fact-reading shape, invisibly, because a pointer never listed is
  never mentioned to the person who needed it. An application can now name the candidate pointers
  an unresolved disposition may be waiting on, and can check at build time that every consulted
  pointer has a producer. The empty string is the root pointer and is reported like any other;
  the list over-approximates by design — a condition-shaped object carried as data is listed too
  — so its values are candidate pointers from an untrusted document, never proof of a read. `outputVersion` stays `"2"`: an added member is backward-compatible under
  VERSIONING.md's machine-output rule, so the bump issue #73 asked for was declined in the open —
  it breaks every consumer that pins the version and buys nothing. Human `packs list` output is
  unchanged, and the row still reports what the document says rather than a verdict on it: a
  document that could not be read carries `[]` beside its `detail`, exactly as
  `evidenceRequirements` does. The conformance claim is unaffected and stated, in full and only,
  in `CONFORMANCE.md`, which no line of this entry restates.

## 0.13.0 - 2026-08-02

- **Pin the reviewed set, and refuse to decide under law that left it** (ADR-0019): `jpack packs
  lock` writes `jpack.lock.json` beside the configuration — the digest of the configuration's exact
  bytes and of every pack and graph it declares, generated, sorted, and byte-deterministic — and
  `jpack packs verify` names every difference from it: `config-drift`, `document-drift`,
  `document-missing`, `lock-entry-missing`, `locked-but-undeclared`, exit `1` on any. The file's
  *presence* is the whole of the opt-in: it is a sibling found by convention rather than a member
  declared in `jpack.json` (a configuration that named its own lock could rename it), so no
  `configVersion` moves, the configuration schema is untouched, and a project with no lock file
  behaves exactly as before. With one, the three deciding surfaces — `experimental evaluate`,
  `experimental graph evaluate`, and the MCP `experimental_evaluate` tool — hold the law that one
  evaluation applies to the reviewed set before evaluating — against the exact bytes about to be
  evaluated, never a second read of the path they came from — and refuse a mismatch fail-closed with
  the provisional code `JPS-LOCK-VERIFY` at exit `1`, naming both honest ways forward: declare the
  amendment with `packs lock`, or restore the reviewed bytes. `packs lock` refuses to write over a
  document the configuration declares, or to emit a lock past the byte limit its own readers apply;
  a matrix or a graph's rows are deliberately not pinned, because the surfaces that read them
  neither record nor decide. A run consults the reviewed set once and every check it makes is
  against that one revision; the lock is held to the shape its generator writes when it is read, and
  an entry recorded at a path the configuration does not declare is its own `path-mismatch`
  finding. `packs test`, `experimental graph
  test`, and `experimental evaluate-corpus` consult it never — the author's loop is free and only
  decisions are classified, the same split the audit trail draws. A pack named by path, or passed
  as text over MCP, is a draft: evaluated, never refused for being unlocked. Where an audit trail is
  configured, each record gains an optional `reviewed` member — `true` when every document applied
  was declared and its exact evaluated bytes matched the reviewed set, `false` for a draft, absent
  when the project keeps no lock;
  `recordVersion` stays `"1"`, because an added member is backward-compatible under the same rule
  VERSIONING.md applies to `outputVersion`. What this is *not*, stated in the record and in the
  documents: it is not a wall. Anything that can edit a pack can re-run `packs lock`, and this
  runtime cannot tell that from an author amending policy on purpose. It makes the amendment
  explicit and recorded. The conformance claim is unaffected and stated, in full and only, in
  `CONFORMANCE.md`, which no line of this entry restates.

- **Say which side to change when a `configVersion` is from a newer toolchain** (#69): a
  configuration declaring a plain integer above the newest version this runtime reads now earns a
  steer beside the accepted list — upgrade the runtime, and do not edit the declaration down,
  because that discards what the configuration declares. An unparseable declaration says nothing
  about which side is behind and keeps the unsteered message. Diagnostic text only; no surface,
  payload, or exit class changes. It was observed live: an agent refused on a `configVersion "3"`
  project by an older sandbox edited the declaration down, evaluated, and reverted — the incident
  ADR-0019 above is the structural answer to. The conformance claim is unaffected and stated, in
  full and only, in `CONFORMANCE.md`, which no line of this entry restates.

## 0.12.0 - 2026-08-02

- **Record evaluations when the project asks for it, and only then** (ADR-0018): `configVersion "3"`
  adds one root member to `jpack.json` — `"audit": {"dir": "<relative-dir>"}` — and declaring it is
  the whole of the opt-in. Each completed evaluation of `experimental evaluate`, `experimental graph
  evaluate` (one record per node plus one for the composite headline), and the MCP
  `experimental_evaluate` tool then appends one JSON line to `<dir>/evaluations.jsonl`: the
  timestamp, the surface, the pack's `id`, `version`, `specVersion` and the SHA-256 of its exact
  bytes, the facts and evidence documents as they reached the engine with the `evidenceSupplied`
  flag that keeps an omitted document distinct from an empty one, and the disposition as the RFC
  8785 canonical bytes the run itself produced. `packs test`, `experimental graph test`, and
  `experimental evaluate-corpus` reach the same evaluator and record nothing — a matrix row is a
  check on a pack, not a decision anyone took — and a refused evaluation records nothing either,
  because it produces no disposition at all. A graph run is one run for that purpose: its node
  records are held and written together with the composite, so a run refused at its third node
  leaves nothing for the first two. A record that cannot be written refuses the run with exit `4`
  and the provisional code `JPS-AUDIT-WRITE`, reporting no disposition; the evaluation is never
  influenced, having already completed. The append goes through the directory handle
  `internal/fssecure` already holds open on the configuration's own directory, under exactly the
  refusals a read is held to plus two of the write's own — an existing trail is opened without
  `O_CREATE` and an absent one exclusively, so a symlink swapped in behind the check loses the race
  instead of being followed into existence, and a trail file with more than one link is refused
  where the platform reports the count — and no surface is handed a pathname. `packs validate` gains
  one check on the configuration itself, `audit-dir-inside-root`, which resolves the declared
  directory's final component because everything written beneath it resolves through that component,
  so a directory that leaves the project — a symlink out of it included — fails the gate instead of
  failing every later evaluation. Every record carries a run id, one per invocation, and a graph
  run's composite line is that run's commit marker: a flat append cannot be atomic against an I/O
  failure partway through a write, so a reader tells a complete run from an abandoned one by that
  rule rather than by trusting the writer. Records name the build that produced them, the bundled
  artifacts evaluated against, the graph's `formatVersion` and document digest, and the draft-RFC
  label whenever the payload carries one; inputs are recorded as JSON values, compacted by the line
  encoder rather than kept as source bytes. A reviewed record also names the revision that made its
  claim true — `reviewedSet` carries the lock's own digest, its `lockVersion`, and the configuration
  digest compared — because the lock is replaced in place and the Boolean alone is a claim nothing
  outside the run can re-derive. On unix the trail file is kept owner-only; on Windows a
  Go file mode sets the read-only attribute and does not restrict the DACL, so confidentiality there
  is the containing directory's ACL. Two consequences worth reading before upgrading a project: declaring `audit` requires
  `configVersion "3"`, which an older runtime refuses as unsupported while naming what it accepts;
  and the three evaluating surfaces now resolve the project configuration on every run, so a
  configuration that *is there* and cannot be read now refuses an evaluation whose pack was named by
  path or passed as text, where it previously succeeded without the configuration being read at all.
  A project that declares no `audit` member, or none at all, is written to by nothing and behaves
  exactly as before. The conformance claim is unaffected and stated, in full and only, in
  `CONFORMANCE.md`, which no line of this entry restates.

## 0.11.0 - 2026-07-30

- **Say what a facts document is, where the mistake happens**: the `experimental_evaluate`
  tool's `facts` description and the CLI `--facts` help now state that the facts document is
  the nested document fact pointers descend into — `{"request":{"type":...}}` for the pointer
  `/request/type` — and never a flat object with pointer-named members, a shape that mirrors
  how `jpack.json` fact hints are keyed and does not resolve the pointers a pack spells
  unescaped. Help text only. A preflight refusal of the all-pointer-named shape was drafted
  and withdrawn under review: a pack can declare the escaped pointer `/~1a~1b`, which reads
  exactly a member literally named `/a/b`, so the shape is a legal facts document and §8.2
  permits no refusal of it. The conformance claim is unaffected and stated, in full and only,
  in `CONFORMANCE.md`, which no line of this entry restates.

- **`author_graph` grows the testing and declaration steps** (ADR-0008's surface; method text
  only): a new step walks writing the graph matrix and running `experimental graph test` —
  `expectedNodes` is what pins an upstream node, the coverage report informs and never gates,
  expectations come from the source statement and never from a run, and a probe that cannot be
  constructed is a question for the policy text, not a row to force — and the hand-over step now
  proposes the `jpack.json` graphs entry (configVersion `"2"`, ADR-0017) beside the document,
  with the human committing the configuration edit exactly as they commit the graph. No
  evaluation surface changes. The conformance claim is unaffected and stated, in full and only,
  in `CONFORMANCE.md`, which no line of this entry restates.

## 0.10.0 - 2026-07-30

- **Declare graphs in `jpack.json`, and walk them like matrices** (ADR-0017): `configVersion`
  becomes `"2"` — this runtime reads both `"1"` and `"2"`, and a `"1"` configuration keeps
  loading everywhere unchanged — and a `"2"` configuration may declare `graphs`: the project's
  own name for each graph, its document path, and optionally its rows document. `experimental
  graph test` and `experimental graph validate` with no argument then walk the declared graphs
  (`--id` selects one), each entry exactly the path-named form's run, rows and coverage
  included; the path-named forms are unchanged. A test walk in which no row ran reports
  `skipped` and exits 1 — a green gate over nothing tested is not a pass — while the validate
  walk reports zero declared graphs as `skipped` at exit 0. Declaring a graph is the one thing
  that costs `configVersion "2"`, which an older runtime refuses as unsupported while naming
  what it accepts; the schema's own `if/then` states that gate once, and `packs schema` now
  reports `supportedConfigVersions` beside the newest version. This partially supersedes
  ADR-0015's determination that graphs stay out of `jpack.json` (its verb-and-payload
  separation stands: nothing graph-shaped enters `packs test`), and reverses the 0.9.0 entry's
  sentence that the stable `jpack.json` shape was untouched by the experimental surface —
  reversed here, not edited there. The `graphs` member and the walk are part of the
  experimental surface and vanish with it. The conformance claim is unaffected and stated, in
  full and only, in `CONFORMANCE.md`, which no line of this entry restates.

- **Report derived coverage in `experimental graph test`** (ADR-0016): the payload carries a
  `coverage` array beside its rows — per node, ADR-0014's own pack derivation renamed
  `node:<nodeId>:<packProbe>`, witnessed by a row's `expectedNodes` entry and, for the declared
  result node, by the row's headline expectation (the composite headline is that node's
  disposition echoed, so it is one probe family, not two); per edge, `edge:<index>:resolved` and
  `edge:<index>:unresolved`, each derived only while the upstream's declarations can reach that
  branch and witnessed by the upstream node's expected disposition kind. One graph-specific
  narrowing: an edge-fed required requirement can never be absent under the default
  `onUnresolved` — a caller entry for an edge-fed requirement is refused, so the state set is
  closed — and its `missing-required-evidence` probe is then not derived rather than pretended
  to. Coverage informs and never gates: no status or exit code moves, witnesses pass the same
  strict disposition gate every comparison shares, and what ran never witnesses — only what the
  rows expect. `coverage` is an additive member, so `outputVersion` is unchanged. The shipped
  fixture's rows now pin the screening node their unresolved row is named for — a fixture
  defect this report found. The conformance claim is unaffected and stated, in full and only,
  in `CONFORMANCE.md`, which no line of this entry restates.

## 0.9.0 - 2026-07-30

- **Add `experimental graph test`**: run a graph matrix — rows carrying one inputs document and
  exactly one expectation each — and judge every row exactly as a pack matrix row is judged:
  the RFC 8785 canonical §8.3 byte comparison, applied to the composite headline and to every
  node a row names in `expectedNodes` (an unnamed node is unchecked, the row author's stated
  choice), or the §8.4 class and phase a refusal row expects. A row whose expectation is not a
  legal disposition is the row's own mismatch, never compared loosely — the same strict gate
  every comparison shares. Exit 0 when every row matched, 1 otherwise; graphs and rows stay
  path-named, and the stable `jpack.json` shape is deliberately untouched by this experimental
  surface. The conformance claim is unaffected and stated, in full and only, in
  `CONFORMANCE.md`, which no line of this entry restates.

- **Add two method prompts, `present_pack` and `author_graph`** (ADR-0008's surface, grown to
  six): `present_pack` guides presenting one pack to an audience grounded in the document
  alone — every statement traceable to a member, the representation labeled as one reading,
  omissions stated, and no outcome ever simulated in place of an evaluation; `author_graph`
  guides composing existing packs into an experimental graph document — declare only
  relationships the source states, check both ends of every verbatim edge (a permission is
  not a performed act), record verbatim what cannot be declared, and hand the proposal to a
  human, the packs themselves never edited. `author_pack` now names `specVersion`
  `0.2.0-draft` — the one version the experimental evaluator admits — where it previously
  named `0.1.0-draft` and steered its own step 7 into a §11 refusal. Method text only; no
  evaluation surface changes, and the conformance claim is unaffected and stated, in full and
  only, in `CONFORMANCE.md`, which no line of this entry restates.

- **Report derived matrix coverage in `packs test`** (ADR-0014): each entry whose matrix loaded,
  whose pack the evaluator admits, and whose declarations derive any probe carries a
  `coverage` array — the probe classes the pack's own
  declarations derive (one per producible
  declared outcome — one a rule, exception, or fallback names — then `not-applicable`, `missing-required-evidence`, `unknown`, `conflict`,
  `exception-escalation`, and `no-match`, each only where the pack makes it reachable), and
  which of them some row's expected disposition witnesses. Coverage informs and never gates:
  no status or exit code moves, a probe's detail states "no row expects this" and nothing more,
  and probes no expectation can witness (a forced outcome, the ordered-comparison type probe)
  are not derived rather than pretended to. `coverage` is an additive member, so
  `outputVersion` stays `"2"`; the §8 reason vocabulary is now exported from the evaluator
  package so the derivation reads the strings the resolver writes. The conformance claim is
  unaffected and stated, in full and only, in `CONFORMANCE.md`, which no line of this entry
  restates.

- **Record the applicability stage in the trace**, so no disposition an authored applicability
  decides arrives over an empty one. §8 step 1 is the single stage that can decide an entire
  evaluation, and both of its terminal branches returned before any exception or rule was
  reached: a pack whose applicability evaluated `false` or `unknown` produced a
  `not-applicable` or `unresolved` disposition with an empty `trace[]` — a record naming no
  condition a reader could place the disposition against, and the same record whatever the
  pack had asked. An authored applicability is now recorded whatever it evaluates to, as an
  entry whose `stage` is `applicability`; an omitted one is the literal `true` with no
  authored condition to report and still records nothing — so a pack without one that stops
  at the §8 step-2 evidence gate still returns an empty trace, with the retained reasons as
  the account. The entry carries no `id`, applicability being one unnamed condition on the
  pack rather than an authored declaration with one, so `id` is now `omitempty` and the human
  renderers print such an entry as its stage alone — both of them through one function,
  rather than the two copies of that formatting that had to agree by inspection. The
  `explain_disposition` prompt's trace walk names the new stage and what a terminal
  applicability means, so a narration reports the pack declining the question or failing to
  reach it instead of a rule that did not fire. `stage` gains a value within an unchanged
  member and `outputVersion` stays `"2"`, on the 0.7.0 precedent that a value change within
  unchanged members is not a protocol break; a consumer matching `stage` exhaustively must
  accept `applicability` beside `exception` and `rule`, exactly as a consumer matching
  `tool.name` had to accept `jpack`. The trace remains informative — ADR-0014's refusal of
  trace-based coverage turns on that, and nothing here pins a trace minimum or invites
  coverage to read one — and the conformance claim is unaffected and stated, in full and
  only, in `CONFORMANCE.md`.

## 0.8.0 - 2026-07-30

- **Add an experimental graph surface** (ADR-0015), prototyping the composition the
  specification's RFC 0002 (Draft) proposes: `experimental graph validate | evaluate | explain`
  read a closed-schema graph document — `formatVersion` `"1"`, distinct from the document's own
  identity `version`, echoed in payloads as `formatVersion` beside `graphId` and `graphVersion`
  — and `experimental graph schema` prints the schema those documents are held to. A graph
  composes packs the project's `jpack.json` declares into a directed acyclic
  structure — a node references one configured decision id, and an edge feeds an upstream
  node's outcome downstream as a fact written at an RFC 6901 pointer, as tri-state evidence
  availability (`present` for an outcome; the edge's declared `onUnresolved`, default
  `unknown`, otherwise), or both. Duplicate and overlapping fact targets, evidence targets fed
  twice, cycles, and over-deep pointers are validation errors; a feed colliding with a
  caller-supplied input is a refusal, never a merge. Evaluation runs every node in
  deterministic topological order, each as one ordinary experimental evaluation by the
  unchanged engine; the composite payload is an envelope of per-node §8.3 dispositions with
  the declared result node's echoed as its headline and every requested handoff aggregated
  beside it, and a node the engine refuses refuses the whole run with the node named and its
  §8.4 class intact. No JPS version defines a graph, a composition, or a composite result:
  every payload is labeled a non-normative runtime convention, the surface may change or be
  removed without compatibility promise, and the conformance claim is unaffected and stated,
  in full and only, in `CONFORMANCE.md`, which no line of this entry restates.

## 0.7.0 - 2026-07-29

- **Rename the command binary to `jpack`**, dropping the `judgment-pack` executable and the
  short-alias arrangement: release archives now carry one binary, `jpack`, built from
  `cmd/jpack`. The long name was the friction a two-binary alias only papered over, so while
  the project has no downstream users the alias becomes the name. The project, repository,
  archive names (`judgment-pack_<version>_<os>_<arch>`), GHCR image
  (`ghcr.io/judgment-pack/judgment-pack`), and MCP registry identity
  (`io.github.Judgment-Pack/judgment-pack`) are unchanged; the executable inside them is
  `jpack`, the OCI entrypoint is `/jpack`, `--version` prints `jpack <version>`, and the MCP
  `serverInfo.name` and every payload's `tool.name` now carry the value `jpack`. That is a
  value change within unchanged members, so `outputVersion` stays `"2"`; a consumer matching
  `tool.name == "judgment-pack"` must match `jpack` instead. The conformance claim is
  unaffected and stated, in full and only, in `CONFORMANCE.md`, which no line of this entry
  restates.
- **Remove the diagram surface entirely** — the `packs diagram` CLI command, the
  `get_pack_diagram` MCP tool (added 0.6.0–0.6.2), and the internal renderer. Mermaid is a
  format, not universal: clients render it unevenly, and a fixed picture is one reading of a
  pack among many. Representation belongs to the interpretation layer — a client's model
  presenting the document to its audience, grounded in `get_pack` — and, later, to an MCP
  Apps surface the server carries. Removed by the maintainer's decision while the project has
  no downstream users; the conformance claim is unaffected and stated, in full and only, in
  `CONFORMANCE.md`, which no line of this entry restates.
- **Harden the authoring method prompts against test-satisfying pack edits**, from live agent
  behavior: `author_pack` now states that `evidenceRequirementRefs` is a citation the evaluator
  never reads, and `test_pack` now names the arbiter for every matrix divergence — the policy
  text — with the rule that the pack is never weakened to make an author's own expectation
  pass, and that `missing-required-evidence` usually indicts the row's evidenceAvailability.
  Method text only; no evaluation surface changes, and the conformance claim is stated, in
  full and only, in `CONFORMANCE.md`, which no line of this entry restates.

## 0.6.2 - 2026-07-29

- **Add the `get_pack_diagram` MCP tool**: one declared pack as the deterministic Mermaid
  flowchart the CLI's `packs diagram` emits — same document, same bytes, on the same strict
  carrier decode every other surface uses. Motivated by live agent behavior: with only
  `get_pack` available, a model asked for a diagram fetches the document and draws its own
  paraphrase; with the rendering one tool call away, the deterministic picture is the path of
  least resistance, and the tool's description says plainly to use it verbatim. The
  conformance claim is stated, in full and only, in `CONFORMANCE.md`, which no line of this
  entry restates.

## 0.6.1 - 2026-07-29

- **`packs diagram` lays the flowchart out in the resolution model's reading order**:
  applicability, evidence, exceptions, rules, outcomes, then the terminal states, ranked
  top-to-bottom with invisible edges — the reader follows the pack's own evaluation flow
  instead of a renderer's packing. Rendering only; no evaluation surface changes, and the
  conformance claim is stated, in full and only, in `CONFORMANCE.md`, which no line of this
  entry restates.

## 0.6.0 - 2026-07-29

- **Add `packs diagram`**: render one declared pack as a deterministic Mermaid flowchart —
  applicability, evidence requirements, rules, exceptions, outcomes, fallback, and escalation,
  in document order, the same bytes on every run. Member nodes quote the document;
  resolution-state nodes (not-applicable, unresolved, no rule fired) are synthesized and
  labeled as what they are. A reading aid derived from the pack, never a second statement of
  it: it adds no member, decides nothing, and diagrams a document exactly as written whether
  or not it validates. GitHub and VS Code render the output in fenced blocks labeled
  `mermaid`. Evidence draws `reads` edges for what conditions actually test
  (`evidence-present`) and `cites` edges for `evidenceRequirementRefs` citations; escalating
  unknowns route to an unresolved node — only `escalation.triggers` decides a handoff;
  colliding sanitized ids stay distinct vertices; labels neutralize the renderer's hazard
  bytes (backticks, `%%` directives, entity-lookalike `#`, empty labels); the frontmatter
  title is a quoted YAML scalar; and the document is decoded once, by the same strict carrier
  every other surface uses, with resource limits refused as operational (exit 4), not
  invalid.
- **Add the `explain_disposition` method prompt** (MCP `prompts` capability, ADR-0008), the
  fourth beside `author_pack`, `test_pack`, and `fix_pack`: non-normative guidance for
  narrating an evaluation payload strictly from the record it carries — ground every
  sentence in a payload or pack member, walk the `trace[]` in order, show how the
  disposition follows from it, and hold the line: the narrative must not soften, overrule,
  or extend the disposition, and the payload asserts nothing about the wisdom of acting on
  it (JPS §3.5). The evaluator's conformance claim is stated, in full and only, in
  `CONFORMANCE.md`, which no line of this entry restates.

## 0.5.0 - 2026-07-28

- **Distribute the released binary as an OCI image and publish it to the MCP registry**
  (ADR-0013). Post-gate release jobs build a `scratch` image from the published release
  archives — never a rebuild — push it to `ghcr.io/judgment-pack/judgment-pack`
  (linux amd64/arm64) under the immutable version tag, execute it anonymously on native
  runners of both architectures, and only after that passing run attest the digest,
  fast-forward `latest` (non-prereleases only), and publish the server to the official
  MCP registry as `io.github.Judgment-Pack/judgment-pack` via GitHub OIDC. Released
  image version tags are refused rather than overwritten, and an image whose smoke test
  fails is left unpromoted, unattested, and unpublished. `docs/mcp-clients.md` documents
  the container and registry install paths. The image carries the same `CONFORMANCE.md`,
  `LICENSE`, `NOTICE`, and `THIRD_PARTY_NOTICES` the archives do; the conformance claim
  is stated, in full and only, in `CONFORMANCE.md`, which no line of this entry restates.
  Nothing about the runtime itself changes: local execution, keyless, network-free, no
  HTTP surface (ADR-0004).
- The pre-gate release archive smoke matrix gains native `linux/arm64`
  (`ubuntu-24.04-arm`); previously that platform was published without ever being
  executed.
- CI validates the rendered MCP registry `server.json` template against the vendored
  registry schema — and against the server-side rules the registry enforces beyond it,
  learned live during the release candidates: an OCI package carries its version in the
  `identifier` tag and must not have a `version` member.

## 0.4.0 - 2026-07-28

- **Add the `jpack.json` project convention** (ADR-0012), a **non-normative convention of this
  runtime** and not part of the Judgment Pack Specification. It gives a project one name per decision
  that works identically from a shell, a CI step, and an agent's tool call. It is optional end to end:
  every command still takes a pack by path, every MCP tool still takes one as text, and a project
  without the file loses no capability. The configuration is selected by `--config`, then
  `JPACK_CONFIG`, then `./jpack.json`, and its schema is closed — every member it does not name is
  rejected, so a misspelled key is an error rather than an intention silently dropped. The published
  schema pins `configVersion` to the one value this runtime accepts, so a configuration held to those
  exact bytes in an editor or a CI step is held to what the runtime will actually read.
  - **New top-level `packs` namespace.** `packs list` reports the resolved inventory (the project's
    decision id, the pack document's own id and version, path, description, and `expectedVersion`
    status). `packs validate [--id X]` checks the configuration and then six named steps per pack —
    path containment, document validation, the `expectedVersion` pin, the optional filename
    cross-check, the hint keys, and matrix well-formedness — each reported as `passed`, `failed`, or
    `skipped`, so a check that passed is distinguishable from one the configuration never asked for.
    `packs test [--id X]` runs each pack's instance matrix through the **experimental** evaluator,
    which its help says plainly. `packs schema` prints the embedded configuration schema, on the same shape and with
    the same `--write` guards `spec schema` has. `packs validate` and `packs test` each exit `1` on any
    failure, so the CI gate is one line: `judgment-pack packs validate && judgment-pack packs test`.
  - **A project's matrix rows are the specification corpus's own case-carrier shape**, and they are run
    by the same code: `id`, `facts`, `evidenceAvailability`, `supportedExtensions`, and exactly one of
    `expectedDisposition` and `expectedErrorClass` (with an optional `expectedErrorPhase`). A row with a
    disposition passes on RFC 8785 canonical byte equality; a row with a class passes when the
    evaluation is refused with that class and phase. A builder's row is a corpus row once it names
    its pack fixture — the one member a corpus row adds, because a project matrix names its pack in
    the configuration — and a pack that declares no matrix is reported **skipped** rather than passed.
    A run in which no row ran at all is reported **skipped** and exits `1`: a green gate over zero rows
    would say a project was tested when nothing was.
  - **`configVersion` is a single integer as a string**, on the `outputVersion` precedent rather than
    semantic versioning; `"1"` is the only accepted value, and anything else is refused as `unsupported`
    with a message naming what this runtime does accept. **There is no templating, no target or
    environment blocks, and no selection**: a templated pack was never the pack anyone reviewed,
    environments are one file per environment by convention, and choosing which decision to ask stays
    with the application. Approval is your pull request; there is no approval state in the file.
  - **Identity is stated once and referenced three times.** The pack document's `id` and `version`
    members are what a pack is. `expectedVersion` in the configuration, the optional
    `<decision-id>-<semver>.pack.json` filename, and the new `packId`/`packVersion` payload members are
    each a *validated reference* to that statement: any of them may disagree with the document, every
    disagreement is an error, and none of them can win one. The filename convention is never required —
    a file named anything else has that check `skipped` — and it is binding when followed, on both the
    decision id and the version.
  - **Additive payload members, no protocol-version change.** Every evaluation payload, and every
    evaluation-corpus row, now carries `packId` and `packVersion`, read off the document that was
    actually evaluated. Two `VERSIONING.md` rules, read together, keep **`outputVersion` at `"2"`**:
    its compatibility-dimensions clause makes a break in machine-output compatibility the thing that
    increments the protocol version, and its MINOR bullet classes an added output field as a
    backward-compatible change. A consumer that never reads these members reads the payload it read
    before. A row or payload that produced no disposition carries neither member: there was no
    evaluation to read an identity off.
  - **`experimental evaluate --pack-id X`** resolves one decision id through the configuration
    (honoring `--config` and `JPACK_CONFIG`). It is mutually exclusive with the pack argument;
    supplying both, or neither, is an invocation error rather than a precedence rule. It is documented
    in the README command block and in the builder's guide, and it reads through the same rooted
    reader every other project read uses. It yields the pack's *bytes*, never a pathname for the
    generic reader to open again: a returned pathname would have to be reopened as a second
    operation on a filesystem that may have changed in between, which is precisely the interval the
    handle-bound reader below exists to remove.
  - **New MCP tools `list_packs` and `get_pack`**, and an optional `pack_id` argument on
    `experimental_evaluate` (mutually exclusive with `pack`, with the same presence and null-rejection
    discipline the existing arguments have). The server reads `JPACK_CONFIG` or the directory it was
    launched in and takes no path over the wire (ADR-0006); with no configuration, `list_packs` answers
    **empty with an explanation of where the runtime looked** rather than failing. `get_pack` serves a
    document that was read and did not decode with a status of `undecodable` and a `detail` saying why,
    rather than calling it valid with empty identity members — the same thing `list_packs` says about
    the same file. Every tool's `inputSchema` stays a flat object: the pack/`pack_id` exclusion is
    stated in both property descriptions and enforced by the handler rather than advertised as a
    composed schema keyword a bridge may drop. The server stays read-only, keyless, and offline.
  - **Hints are non-normative agent guidance and the runtime never resolves one.** A `facts` hint is
    keyed by the RFC 6901 pointer a pack reads, an `evidence` hint by a declared evidence-requirement
    id, and each says in the project's own words where the value is held. This runtime holds no
    credential, opens no network connection, and never reads a source a hint names (ADR-0004,
    ADR-0006). The gathering is the agent's, and an unsourceable fact is reported `unknown` rather than
    guessed, so the pack escalates instead of deciding on an invention. Because nothing else ever
    resolves a hint key, `packs validate` checks it against the pack document: an `evidence` key must
    be a declared evidence-requirement id, and a `facts` pointer must be one some condition reads or an
    ancestor of one. A misspelled key is a failed check rather than an instruction an agent follows.
  - **Every file access is bound to a handle held open on the configuration's own directory** through
    the new `fssecure.Relative` and `fssecure.Root`. A path that escapes is refused **twice**: when the
    configuration is validated, before anything is read, and again at read time, where resolution
    against the handle catches an escape through a symlinked component that a lexical check cannot
    see. The read-time half is a handle rather than a pathname so that **containment holds through the
    open and not merely up to it** — a pathname checked and then opened leaves an interval in which an
    intermediate directory component can be swapped for a symlink pointing out of the root, and the
    open follows it, with a post-hoc check on the opened file unable to notice. `internal/fssecure`
    opens the project directory once, at load, and resolves every later read relative to that handle
    through Go's `os.Root`. A final component that is a symlink is still refused whatever it points at,
    and only a regular file is read. Every surface that reaches a pack through the configuration takes
    this one reader — none is handed a pathname to open itself — and `packs validate` asks its
    containment question of the same handle, so a symlinked-component escape fails the check named for
    containment rather than the one after it. The project's root is the handle's own directory rather
    than a second derivation from the configuration path, so the root and the configuration bytes
    cannot describe two different directories.
  - **The minimum Go version is now 1.24** (from 1.21). `os.Root` is the standard library's
    handle-bound opener and is the containment primitive above; hand-rolling one per platform to keep
    an older floor would be reimplementing a security primitive for no gain. The release workflow
    continues to pin its own supported toolchain independently.
  - **A configuration must declare at least one pack.** The schema's `packs` object carries
    `minProperties: 1`, and the test runner independently demotes any run that executed zero rows to
    `skipped` regardless of how many packs were selected. Either alone would leave a hole: without the
    schema constraint an empty project is a valid configuration, and without the runner's demotion any
    other route to an empty selection reports a clean run over nothing.
  - New documentation: [`docs/building-with-packs.md`](docs/building-with-packs.md), the builder's
    guide — the packs-as-code lifecycle, the three-owner model (the application selects, the agent
    gathers and never invents, the pack judges, with `not-applicable` as the misrouting net), hints in
    practice, `expectedVersion` discipline, the filename convention, and the
    data-sufficiency-as-another-pack pattern. A README section and an
    [`docs/mcp-clients.md`](docs/mcp-clients.md) section cover the same ground briefly. Every new help
    text, tool description, and label is **reference-only**: `packs test` says where the evaluator's
    conformance claim is stated and states no part of it, and the claim-surface inventory now walks the
    new CLI help texts and the new MCP tool descriptions and asserts them by name.

- **Take the §3.4.1 conformance decision, and add the root [`CONFORMANCE.md`](CONFORMANCE.md) it is
  stated in** (ADR-0011). This entry is **reference-only**, deliberately: §3.4.1 fixes the entire form
  such a statement may take, so the class, the version scope, the corpus, the results, the evidence and
  its limits, the activation point, and everything not asserted are in that file and in no other
  sentence in this repository — a partial restatement here would be the partial form §3.4.1 forbids.
  Read `CONFORMANCE.md` for all of it; what follows is what changed in this runtime to make the
  decision takeable, and each item is a change to behavior, output, or documentation rather than a
  restatement of the file. Activation is not release-conditional — §3.4.1 requires no tag — so a
  development build from this history behaves exactly like a released one, and `CONFORMANCE.md` states
  where activation begins and how long it persists.
  - **Breaking: the evaluator now evaluates only a pack declaring `specVersion` `0.2.0-draft`.** §11
    makes the declared value exact — an unedited `0.1.0-draft` pack "must be re-declared before an
    implementation claiming this draft evaluates it" — so a pack declaring any other version is refused
    as `pack-not-conformant` in the `preflight` phase with the new
    `JPS-EVALUATION-PACK-SPEC-VERSION` code, on `experimental evaluate`, `experimental evaluate-corpus`,
    and `experimental_evaluate` alike, with and without `--rfc0008-quantifiers`. There is no legacy path.
    **Migration is one edit per pack:** change the `specVersion` string to `0.2.0-draft` and nothing
    else, because §11 says that version changes no part of the document format — every member, every
    cross-field rule, and every document-conformance verdict of §§3.1–3.3 is unchanged. Document
    validation is untouched: `spec validate` still validates a `0.1.0-draft` pack against its own
    published schema, and document conformance needs no evaluator (§3.4). This supersedes ADR-0010's
    rejection of that behavior, which was sound only while nothing was claimed.
  - **The §10 limits the class requires are supplied first**, which is the precondition ADR-0010
    deferred and named. `resource-exhaustion` is now reachable on the ordinary Core path and not only
    under `--rfc0008-quantifiers`: an **evaluation-work limit of 20,971,520 units** per evaluation
    (`DefaultCoreWorkLimit`, configurable per evaluation) is charged over every condition node, §8
    iteration, pointer resolution, and compared byte, using the accounting model the draft-RFC prototype
    already documented — so one model now serves both paths, plus one unit per authored evidence
    requirement, exception, and rule, charged before §8 step 1. Each condition tree's charge completes
    before any predicate in it runs, so an exhausted limit is an explicit `resource-exhaustion` error in
    the `evaluation` phase with no disposition and no partial state, never a truncated result. The
    number is **derived in code** as twice the carrier's 10 MiB per-document byte cap, so it cannot
    drift from it. That ratio is an arithmetic fact about two numbers and **gives no whole-evaluation
    guarantee**: three documents may be admitted, and a unit is charged per use rather than once per
    admitted byte. Every row of the bundled corpus charges under 1,000 units, which measures those rows
    and bounds nothing else.
  - The **collection-size limit is the carrier's 250,000-node cap**, documented as the §10 limit it is
    rather than duplicated as a second mechanism: every input is admitted under that cap, so no admitted
    document holds a larger collection and Core constructs none of its own. Reaching it is
    `malformed-input` in the `preflight` phase, which is §10's own phase split and stricter than an
    evaluation-phase check of the same bound.
  - **The `experimental` namespace stays and no command is renamed**; what changes is what it says.
    "Experimental" now means only what it always meant about stability — this surface may change or be
    removed without compatibility promise — and every surface that denied a claim is now **reference-only**:
    the CLI help, the human output header, the corpus label, the MCP tool descriptions, the `test_pack`
    prompt, the draft-RFC prototype note, the README, and the docs each say that the conformance claim is
    stated, in full and only, in `CONFORMANCE.md`, and no sentence outside that file states any part of
    it. That is §3.4.1's requirement and not a style choice: it fixes the entire form a claim may take, so
    a surface naming the class and the version while omitting the corpus version, the results, and the
    every-row statement would be making a partial claim §3.4.1 forbids. The rule is **enforced over the
    maintained prose too**, not only over the runtime's output: the inventory test walks this CHANGELOG's
    Unreleased section, the README, every document under `docs/`, the decision record, and the *rendered*
    MCP prompts, and it fails on an assembled claim — one statement holding the class, the exact version,
    and the corpus version or its results — as well as on a claiming or denying phrase. Two dated records
    (ADR-0007, ADR-0010) and released sections of this file are outside the walk, since neither is a
    statement about the current posture and rewriting decision history to match one would be the worse
    defect.
  - **Breaking, with `outputVersion` incremented from `"1"` to `"2"` on every payload.** The in-band
    `conformanceClaim` member — which carried `"none"` — is **removed** and replaced by
    `conformanceClaimReference`, whose value is the fixed string `"CONFORMANCE.md"`: a locator, not a
    claim. Renaming a member breaks a consumer that read the old name, and
    [`VERSIONING.md`](VERSIONING.md) requires a change that breaks machine-output compatibility to
    increment the protocol version deliberately, so it is incremented — for every command's payload, not
    only the evaluation ones, because one protocol version describes one output contract.
    **Migration:** read `conformanceClaimReference` where `conformanceClaim` was read and expect the fixed
    value `"CONFORMANCE.md"`; a consumer that asserted `conformanceClaim == "none"` should instead treat
    the reference as opaque and read the claim from that file. Branch on `evaluatorSpecVersion` (still
    `"0.2.0-draft"`) for the contract version, and on `specVersion` for the pack's own — the two must now
    agree for a pack to be evaluated at all. A consumer that pins `outputVersion` must accept `"2"`; every
    other member of every payload is unchanged.
- Bundle JPS `0.2.0-draft` alongside `0.1.0-draft`, imported from the specification tag
  `v0.2.0-draft` with the maintainer tool. The new bundle adds the evaluation corpus of §3.4.1 — its
  manifest, that manifest's schema, and the four pack fixtures its twenty rows name — which the
  import tool now carries for any specification version that publishes one. `spec validate` accepts a
  pack declaring either version and still selects the schema by the version the document itself
  declares; `spec schema`, `spec test-conformance --spec-version`, `version`, `get_schema`, and
  `describe_runtime` all reach both. **No default changes**: every surface that selects a version for
  a caller who named none still selects `0.1.0-draft`. The release gate now verifies the lock of
  every bundled version rather than only the default one, and the one `artifactProvenance` string in
  the runtime description now describes every bundle, so a development snapshot in a
  non-default version can no longer hide behind the default version's provenance.
- Align the EXPERIMENTAL evaluator to the evaluator conformance class of Core `0.2.0-draft`
  (ADR-0010), under the `experimental` namespace and claiming nothing *as that change landed* — JPS
  §3.4.1 defines the one form an evaluator-conformance claim may take, and taking that decision was
  left to a separate ADR, which the entry above is. The contract is applied to a
  pack of either bundled version whatever that pack declares, so every evaluation payload and every
  `evaluationError` now names the contract's own version in a new `evaluatorSpecVersion` member beside
  the pack's `specVersion`: §11 says these semantics existed for no consumer under `0.1.0-draft`, so a
  payload carrying only the pack's version would read as a `0.1.0-draft` disposition, which does not
  exist. Three normative sections are implemented:
  - **§8.2 input preflight.** Inputs are admitted in Core's order — pack, facts, evidence,
    required-extension set — and the preflight completes before §8 step 1, so no result can outrace
    an input error: a pack whose applicability is false, presented with an evidence document carrying
    an undeclared key, is now the input error rather than the `not-applicable` disposition. An omitted
    evidence document is the implicit empty object, the only form its absence takes.
  - **§8.3 portable disposition.** Under `--format json` without `--pretty`, the `disposition` member
    is now written in its RFC 8785 canonical form, produced by a new stdlib-only `internal/jcs` encoder
    over the strings, arrays, and objects a disposition is made of. `--pretty` indents the whole
    payload and reaches inside that member too, so under it the member order and both sets stay
    canonical and the exact bytes do not appear; §8.3 requires canonicalization where a byte comparison
    is required, so a comparison recanonicalizes either side it did not produce. `handoff` gains
    `triggeredBy`, the retained reasons that triggered a request, present exactly when the state is
    `requested`. Canonicalization is also the one place that refuses a disposition §8.3 forbids, so no
    value assembled outside the engine can violate any invariant that section states about the
    disposition alone: the `kind`, `handoff.state`, and reason vocabularies, its three presence rules,
    the exact reason set of a `not-applicable` result, and `triggeredBy` being a subset of `reasons`.
    **Breaking for a consumer of the experimental payload:** the disposition no longer echoes the
    pack's escalation target, which §8.3 keeps out of it; the target moved to a sibling
    `handoffTarget` member. Human output is unchanged prose, now naming the triggering reasons.
  - **§8.4 error contract.** Every refused evaluation reports exactly one of the four Core classes in
    band, as a new `evaluationError` member carrying `class` and `phase`, with no disposition at all;
    the existing `JPS-*` codes stay beside the class as the finer detail. The classes are evaluated in
    Core's fixed order, which is the preflight order, so an unsupported required extension is now
    reported as `unsupported-required-extension` rather than folded into
    `JPS-EVALUATION-PACK-NOT-CONFORMANT`, and a malformed input outranks it on the same inputs.
    Reaching a limit while admitting an input is `malformed-input`, and that now includes the input
    byte limit on every surface: the CLI reports an oversized input inside the preflight instead of
    refusing it at the read, so it carries a class and takes its place in the fixed order — an
    oversized facts document no longer outranks a non-conformant pack. `resource-exhaustion` is the one
    class of the evaluation phase, and as this change left it, it was reached only by the
    `--rfc0008-quantifiers` work budget — no evaluation-work charge was levied on the Core path at all.
    The conformance-claim entry above closes that gap in the same release, so the `resource-exhaustion`
    a consumer sees is the Core path's too. Both surfaces report the same envelope: an `experimental_evaluate` refusal
    is an in-band MCP tool error whose `structuredContent` is the `evaluationError` payload the CLI
    writes, so the class, the phase, and `evaluatorSpecVersion` are machine-readable there too rather
    than sentences a client has to classify itself.
  - **§8.2 on the MCP surface.** `experimental_evaluate` now decodes each document argument's presence
    separately from its value, so the two meanings §8.2 gives an absent and an empty evidence document
    stay apart on the wire: omitting the key is the implicit empty object and evaluates, while a key
    present with an empty string is a supplied document and is `malformed-input`. A present-but-empty
    `pack` or `facts` likewise enters the preflight — `pack-not-conformant` and `malformed-input` —
    instead of being reported as an unclassified missing argument, an absent required key stays an
    invocation failure with no class, and an explicit `null` is rejected as the argument-type error the
    declared string schema makes it.
- Add `judgment-pack experimental evaluate-corpus`, which runs the bundled evaluation corpus for one
  exact specification version and reports every row: the disposition compared as §8.3 defines
  disposition equality — both sides through the same canonicalizer, so a set's stored order is not a
  difference — or the expected §8.4 class and phase. Its output is labelled as corpus results in both
  formats and a mismatch exits 1. **A run is not a claim**: §3.4.1 makes corpus results the required
  and non-exhaustive evidence a statement of that class rests on, and this repository states one in full
  and only in `CONFORMANCE.md` (see the entry above). Run the verb for the current result rather than
  reading one out of a changelog, since a mismatching row would decide nothing by itself (§3.4). The
  verb is CLI only; the MCP surface does not expose it.

## 0.3.0 - 2026-07-28

- Add a DRAFT-RFC PROTOTYPE of the specification's RFC 0008 (Draft), bounded collection quantifiers,
  behind the new `judgment-pack experimental evaluate --rfc0008-quantifiers` flag (ADR-0009, under
  ADR-0007's experimental umbrella). The flag admits three condition operators — `exists`, `every`,
  and `uniform` — with the RFC's element re-rooting, its pinned empty-array values, its
  aggregate-depth bound of two, and a candidate work-accounting model the RFC itself leaves open.
  Every successful evaluation payload produced this way carries a new output member,
  `draftPrototype`, naming the operators used and stating that a pack using one is NOT valid under
  any published JPS version; a refusal carries no such member, because a grammar or work-limit
  failure is reported through the ordinary operational-error envelope. Four diagnostic codes are
  minted, `codeStability: "provisional"` like every other: `JPS-EVALUATION-RFC0008-GRAMMAR`,
  `JPS-EVALUATION-RFC0008-DEPTH`, `JPS-EVALUATION-RFC0008-SHAPE`, and
  `JPS-RESOURCE-EVALUATION-WORK-LIMIT`. **No conformance claim and no validation behavior changes**:
  `spec validate` is untouched and still rejects every pack using an operator, the evaluator without
  the flag refuses one for the same reason, the MCP surface does not expose the flag, and everything
  the draft grammar does not add is still held to full document conformance through the pack's Core
  projection. The operators belong to an open proposal that may never be accepted.
- Make §7.4 equality total in the EXPERIMENTAL evaluator. JSON numbers are compared as normalized
  tokens — sign, significant digits, adjusted exponent — rather than through arbitrary-precision
  arithmetic, so `1e3`, `1000`, and `1.0e3` are one value, `-0` equals `0`, and a pair no arithmetic
  type can hold (`1e999999999` against `2e999999999`) compares `false` instead of degrading to
  `unknown`. `equals`, `not-equals`, and `in` therefore always decide, and `uniform` needs no arm
  beyond RFC 0008's five clauses: an evaluator's inability to represent an admitted value is a
  resource condition, not a semantics two implementations would have to agree on. Ordered comparison
  is unchanged — still defined only over §2.2 decimal strings — and `spec validate`, the conformance
  corpus, and the exit classes are untouched.
- Add the MCP `prompts` capability (ADR-0008): three non-normative authoring-method prompts —
  `author_pack`, `test_pack`, `fix_pack` — served as static, versioned text that the client's model
  executes with the client's key. The server still calls no model, holds no key, and opens no
  network connection; serving a prompt is read-only, like `get_schema`. Method content distills the
  expressiveness studies: the resolution-model shapes that avoid conflicts, the `onUnknown`
  discipline, the decimal-string rule for ordered comparisons, the prepared-facts ledger, and the
  instance-matrix logic probe. Following a prompt does not make a pack conformant, and no prompt
  interprets any policy.

## 0.2.0 - 2026-07-27

- Add an EXPERIMENTAL evaluator (ADR-0007): `judgment-pack experimental evaluate` and the
  `experimental_evaluate` MCP tool apply the JPS Core §§7–8 experiment, as pinned by the
  specification's RFC 0006 (Draft), to one conformant pack, one facts document, and an optional
  tri-state evidence-availability input, returning a disposition (kind, reasons, handoff) and a
  trace. It claims NO evaluator conformance — JPS `0.1.0-draft` forbids such claims — authorizes
  nothing, evaluates only fully conformant packs, and may change or be removed without
  compatibility promise. The nine walked instances committed in RFC 0006's appendix run as this
  surface's acceptance tests. Validation behavior, claims, and exit classes are unchanged.

## 0.1.0 - 2026-07-25

- Add a `NOTICE` file identifying Brian Jin as the copyright holder and carrying
  the attribution required for the embedded Judgment Pack Specification artifacts, and ship it in
  the release archives.
- Record the embedded specification bundle in `THIRD_PARTY_NOTICES`. The bundle is Apache-2.0
  material from a separate project and appears in neither `go.mod` nor `go.sum`, so the previous
  Go-modules-only framing omitted it.
- Re-vendor the embedded specification bundle from `Judgment-Pack/judgment-pack-spec`, replacing the
  pre-neutralization pin. The two schema `$id` values now carry the permanent
  `https://judgmentpack.org/schema/` identifiers. Machine-visible: `spec schema` reports a new
  `schemaId`, `sha256`, and `bytes`, and the bundle digest changes. No validation behavior changes
  and all 47 conformance cases still pass — the corpus is byte-identical to the previous pin.
- Pin the bundle to an exact commit rather than a tag. The specification's release tooling requires
  a tag string to equal `specVersion`, so the permanent identifiers on its `main` branch cannot be
  republished under a second `0.1.0-draft` tag. The release gate already treats a full-length commit
  digest as an immutable reference; the pin moves back to a tag once a specification version
  carrying those identifiers is published.
- Pin the machine contract in tests: `outputVersion`, `tool.name`, the six exit classes, and the
  four envelope members every JSON payload carries. These were previously enforced only by
  convention, so a rename or an accidental version bump passed silently.
- Add a `judgment-pack mcp` subcommand: a hand-rolled, dependency-free Model Context Protocol server
  over stdio that exposes the offline validation, conformance, and description operations as MCP
  tools. It holds no credential, opens no network connection, and evaluates nothing. See ADR-0003.
- Make validator diagnostics self-sufficient. Structural and carrier messages now name the offending
  value and the fix — the allowed enum values, the identifier grammar with an example, the required
  and actual collection counts, the valid condition `op` values, the exact effect/operand mismatch for
  an exception, and (for carrier parse errors) the line, column, and byte offset with the decoder's
  reason. Messages and provisional codes only; all 47 conformance cases still pass and the
  machine-output contract is unchanged.
- Unify diagnostic code prefixes under `JPS-`. The CLI-invocation codes previously prefixed `JPR-`
  are now `JPS-INVOCATION-*`; the process exit class, not the prefix, carries the error category.
  Codes remain provisional. See ADR-0005.
- Populate the `extensions` summary in `spec validate` JSON output even when a document is invalid at
  the semantic layer, so a declared required extension is no longer dropped from the summary.
- Surface the embedded valid conformance fixtures read-only: `list_examples` / `get_example` MCP
  tools and a `spec examples [name] [--write …]` CLI command. They give a filesystem-less client a
  conformant starting point for Create, return the exact digest-locked bytes the bundle embeds, and
  label every payload `kind: version-pinned-conformance-fixture` so a fixture is never mistaken for
  an authored template. Read-only, offline, keyless, dependency-free. See ADR-0006 and the new
  `docs/authoring-lifecycle.md`.

## 0.0.1 - 2026-07-23

- Establish `judgment-pack-runtime` as the vendor-neutral reference runtime for the Judgment Pack
  Specification (JPS) under the `Judgment-Pack` organization. This project originated as the
  `protoss-cli` reference validator and was renamed and relocated to a vendor-neutral home; it is a
  reference implementation, not the only valid one.
- Provide the offline `judgment-pack spec` command namespace (`validate`, `test-conformance`,
  `schema`) plus `version`, built as the `judgment-pack` binary with a `jpack` short alias.
- Implement strict carrier, structural, semantic, and extension-capability validation. The runtime
  validates documents only; it does not evaluate rules, choose an outcome, or authorize an action,
  matching the JPS `0.1.0-draft` scope (no evaluator conformance class).
- Embed and integrity-check JPS `v0.1.0-draft`, pinned byte-for-byte to its immutable upstream
  release tag.
- Provide bundled and local conformance-suite execution, versioned JSON output, and stable process
  exit classes.
- Provide cross-platform release archives, SHA-256 checksums, and build-provenance attestations.

### Known follow-ups

- The embedded specification bundle is pinned to the pre-neutralization upstream tag
  (`v0.1.0-draft`), whose schema `$id` values use temporary repository-hosted URLs. (Resolved in
  Unreleased by re-vendoring from `Judgment-Pack/judgment-pack-spec` at an exact commit. The
  original entry attributed the delay to the specification project not having published a neutral
  tag; the permanent identifiers were in fact already on its `main` branch.)
- No `GOVERNANCE`/`MAINTAINERS`/`CODEOWNERS` files exist yet. (The `NOTICE` file is added in
  Unreleased. `LICENSE` is deliberately left byte-identical to the canonical Apache-2.0 text: its
  appendix is a template for marking your own files, not a field to fill, and editing it costs a
  clean Apache-2.0 match on automated license scanners. The copyright holder is named in `NOTICE`.)
