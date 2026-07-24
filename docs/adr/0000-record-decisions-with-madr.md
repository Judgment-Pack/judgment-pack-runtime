---
status: accepted
date: 2026-07-24
deciders: Brian Jin
---

# Record runtime decisions with MADR-format ADRs

## Context and problem statement

The runtime makes cross-cutting implementation decisions — repository layout, adapter strategy,
what to defer — that no single commit or pull request represents, because they are stances rather
than diffs. The commit and PR bodies here already carry excellent rationale, but a stance like "no
`packages/` umbrella" is owned by no one change and cannot be found by reading `git log`.

The specification project already has a process for design decisions: RFCs, in
`judgment-pack-spec` under `rfcs/` (`rfcs/0000-rfc-process`). But an RFC is a public,
comment-before-commit proposal aimed at reviewers who need to weigh in — appropriate for a
normative standard, and disproportionate for the implementation choices of one nonnormative
consumer that are, in practice, already made.

## Decision drivers

- Cross-cutting stances need a greppable, durable home separate from `git log`.
- The runtime has no external review body; comment-before-commit ceremony would be theatre.
- The record should stay lightweight enough that writing one is not a deterrent.

## Considered options

- Markdown ADRs (MADR) under `docs/adr/`.
- Adopt the specification's RFC process inside the runtime as well.
- Keep relying on commit and pull-request bodies plus [architecture.md](../architecture.md).

## Decision outcome

Chosen option: MADR-format ADRs under `docs/adr/`, because they fit decisions already made and keep
the record cheap. The repository boundary decides the mechanism:

- **Runtime → ADRs.** Implementation decisions, recorded after the fact.
- **Specification → RFCs.** Normative, public, cross-implementation proposals open for comment.

A runtime decision graduates into a spec RFC only if it ever needs agreement across independent
implementations.

[architecture.md](../architecture.md) remains the description of the system as it is; ADRs hold the
history of why and when. architecture.md links to an ADR rather than restating it.

### Consequences

- Good, because a future contributor finds "why is there no `packages/`?" in one file, not a commit.
- Good, because it matches a repository that already values written rationale (CHANGELOG, VERSIONING, NOTICE, releasing).
- Bad, because it is a second place to keep honest; an accepted ADR that no longer holds must be superseded, not silently ignored.
- Revisit when the runtime grows a contributor community that needs comment-before-commit, at which point some decisions may warrant a runtime RFC process.

## More information

Status lifecycle: `proposed` → `accepted` → (`deprecated` | `superseded by NNNN`). Accepted ADRs are
immutable; a change is a new ADR. See [README.md](README.md) for how to add one.
