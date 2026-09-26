---
status: accepted
date: 2026-09-26
deciders: repository maintainer
---

# Admit test matrices and rehearse exact pack snapshots through MCP

## Context and problem statement

Clients designing test suites need machine-readable admission findings before
saving a proposal. Job releases and draft tests must run the exact supplied pack
and matrix without registering temporary files or appending decision audit records.
A second client-side matrix implementation would drift from project-file admission.

## Decision

Expose three explicitly experimental MCP tools:

- `experimental_get_test_matrix_contract` describes the runtime-owned matrix
  carrier, field names, version membership, expectations and limits.
- `experimental_validate_test_matrix` reports root and indexed row findings,
  reusing matrix decoding and exact disposition decoding. It does not evaluate
  policy, infer expected answers, establish reachability or save proposals.
- `experimental_test_cases` rehearses exact supplied pack and matrix JSON text.
  It shares the loaded-pack execution path, canonical comparison, target budgets
  and advisory coverage used by `experimental_test_packs`.

The runtime remains the authority for the test carrier and evaluation. Clients
own bounded correction loops, human review, persistence and orchestration. The
snapshot tool reads no project files, consults no reviewed set and appends no
audit record. This is a rehearsal, not an operational decision or authorization.

Both supplied documents are bounded before evaluation. The report is bounded
before returning over MCP. Expected dispositions retain their full JPS form;
unknown members are refused rather than silently normalized. Application source
mappings and UI metadata stay outside the matrix carrier.

## Consequences

Clients can explain invalid proposals using row-specific findings and test the
same bytes they are about to release. The cost is three additional experimental
public surfaces, with no stability promise. `CONFORMANCE.md` enumerates the new
snapshot entry point without changing the evaluator's semantics or extending a
claim to the supplied pack, evidence or business policy.

Alternatives rejected: accepting prose/code fences as saved tests, maintaining a
permissive client-only decoder, or writing temporary project files for rehearsal.
Revisit the transport shape if matrix report limits or a future normative format
require a separately versioned API.

## Verification and review

MCP tests cover exact member admission, row findings, shared snapshot comparison,
empty suites and snapshot isolation. The full runtime suite, static analysis and
bundled JPS conformance suite pass. This public-surface and documented-claim change
ordinarily requires the recorded cross-vendor review specified in this directory's
README. For runtime PR #164, the maintainer explicitly authorized an exception
to that requirement on 2026-09-26 after reviewing the tested PR. No cross-vendor
review was performed or claimed. This exception applies only to this change;
the repository review policy remains in force for future material decisions.
