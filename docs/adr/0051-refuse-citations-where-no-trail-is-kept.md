---
status: proposed
date: 2026-10-12
deciders: maintainer
---

# Refuse citations where no trail is kept, as a binding is refused

## Context and problem statement

[ADR-0033](0033-a-record-cites-the-receipts-it-relied-on.md) lets a caller name the gateway receipts
a decision relied on: `experimental evaluate --cites`, `experimental graph evaluate --cites` and the
MCP `experimental_evaluate` tool's `cites` argument. Its "where it lands" determination puts them on
every record the run leaves. A project whose configuration declares no audit directory leaves no
record (ADR-0018). There, the citations were held to their shape, accepted and recorded nowhere: the
run evaluated and exited 0, and its payload did not say they were dropped. The caller could believe
the decision record cites the receipts when no record exists (#266).

[ADR-0050](0050-record-a-callers-binding-digest.md) refuses a binding in that case, after the
configuration is read and before anything is evaluated. Its reason is the one it gives for refusing
a binding on a rehearsal: a binding accepted and then not recorded would let the caller believe it
was committed. It noted that citations were still accepted and recorded nowhere.

## Decision drivers

- A citation is a commitment on the record, as a binding is: the caller's assertion, which the
  gateway's verifier resolves from the record (ADR-0033). Accepted and then not recorded, it lets
  the caller believe it was recorded.
- One rule for both commitments, in the binding's place and form (ADR-0050).
- A project that keeps a trail is unchanged, and its records are byte for byte what they were.
- A rehearsal records nothing and says so in its payload (ADR-0028, ADR-0041). Its rule for
  citations is not changed here.

## Considered options

- **A. Refuse citations where no trail is kept, as a binding is refused** (chosen).
- **B. Accept them and say in the payload that nothing recorded them.** Rejected: a caller that reads
  the exit code alone still believes it, and a binding in the same case is refused, so one case
  would have two rules.
- **C. Keep ADR-0033's determination.** Rejected: it is the case #266 reports.

## Decision outcome

Chosen option: **A**, because a commitment the runtime cannot record is then never accepted.

**The refusal.** Citations given in a project whose loaded configuration declares no audit
directory, or where no configuration is present, are refused under `JPS-INVOCATION-CITES`, the code
a citations document not of its shape is refused with. On the CLI, on both evaluate verbs, the exit
is 3 and the sentence is "--cites is not taken here: the project keeps no trail, so citations would
be accepted and not recorded." On MCP it is a tool error: "The "cites" argument is not taken here:
the project keeps no trail, so citations would be accepted and not recorded." Neither names a value
of the citations, and nothing is written.

**Where.** After the configuration is read and before anything is evaluated, where ADR-0050 refuses
a binding. On `experimental evaluate` and the MCP tool, that is after the citations' and the
binding's shapes are held, the project is loaded and a pack named by id is resolved. It is before a
pack named by path, the facts and the evidence are read, the reviewed set is consulted, or the
evaluator runs. Given a binding too, the citations are refused first, in the order the two shapes
are checked. `experimental graph evaluate` reads its graph document before its configuration, as it
always has. There the refusal comes once the configuration is read, before the inputs document or
any node's pack is read.

**What it does not change.** An empty array cites nothing and is not refused. A rehearsal with
citations keeps its rule: the citations are held to their shape and recorded nowhere, in any
project, and the payload says `"rehearsal": true`. In a project that keeps a trail, citations land
as ADR-0033 says, and the record is byte for byte what it was. The citation's shape, `audit verify`
and the MCP tool's schema are unchanged, except that the `cites` description states the refusal.

**What this supersedes.** ADR-0033's "where it lands" determination, in this one respect: in a
project that keeps no trail, citations no longer land on no record; they are refused. The rest of
ADR-0033 stands.

**What it establishes.** Outside a rehearsal, a run given citations that completes has them on its
records: where no trail is kept the run is refused, and where one is kept a record that cannot be
written refuses the run (ADR-0018).

**What it does not establish.** That a cited receipt exists or verifies, which is still the gateway
verifier's finding (ADR-0033). Anything about a rehearsal, whose payload says it decided nothing and
which records nothing. Nor does the trail now hold attempts: a refused run records nothing
(ADR-0048).

### Consequences

- Good, because a caller can no longer read a completed run as having recorded citations that no
  record holds.
- Good, because the binding and the citations, a caller's two commitments, follow one rule.
- Bad, because a caller that passed citations to a project without a trail, and was answered
  before, is now refused, and must drop the citations or declare an audit directory.
- Revisit when a caller needs citations on a run that keeps no trail, for something other than the
  record.

## More information

- #266 (the request), #263 and ADR-0050 (the binding's refusal, the precedent), ADR-0033 (the
  citations), ADR-0018 (the trail), ADR-0028 and ADR-0041 (rehearsals), ADR-0048 (refusals stay
  outside the trail).
- The `--cites` flags in `internal/cli` (`app.go`, `graph.go`) and the `cites` argument in
  `internal/mcp` (`tools.go`).
