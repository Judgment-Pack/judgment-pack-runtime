---
status: accepted
date: 2026-11-12
deciders: maintainer
---

# Return a canonical decision record for one evaluation from the Go API, written nowhere

## Context and problem statement

A Go program that evaluates a pack in-process (ADR-0073) and wants to keep what was decided has to
assemble its own record of the evaluation from `Pack.Identity`, `Evaluation.CanonicalDisposition`
and a digest of the facts it computes itself (issue #387). Every such assembler chooses its own
member names, its own form for the facts (the exact bytes, or a re-encoding through
`encoding/json`, which writes `1.50` as `1.5` and escapes `<`),
and its own answer for an omitted evidence document. Two programs that evaluated the same pack on
the same facts then keep records that do not agree byte for byte, and neither can tell from the
other's record whether they decided the same thing.

The audit trail (ADR-0018) is this runtime's record of an evaluation, but it is a project feature:
it is appended to a file in the project's tree, carries the run, the time, the chain and the
signature, and ADR-0073 leaves it out of the Go API, which reads and writes nothing.

Material-decision impact: public-surface.

## Decision drivers

- Deterministic: the same pack bytes, documents and binding under the same runtime version give the
  same record bytes and the same digest. The record holds no clock, no random value and nothing
  read from outside its inputs.
- Writes nothing: the Go API keeps its boundary of no files, environment or network, which the
  import-closure test holds.
- Every existing byte stays: `Evaluate`, `Evaluation.JSON`, `CanonicalDisposition`, the view's two
  encodings, the trail record and `internal/audit` are unchanged.
- No second evaluator: the conformance enumeration counts one place in package `sdk` that builds
  the shared evaluator, and the record must not add another.
- Says no more than it holds: it states what it does not establish, and nothing in its form
  suggests a trail entry or a signature.
- A caller's own commitments can ride on it, small, closed and refused when malformed, never
  dropped in silence.

## Considered options

- **A.** A new method, `Pack.EvaluateWithRecord`, that calls `Evaluate` and returns, beside the
  evaluation, a `record.DecisionRecord` of a new package `sdk/record`, with the facts and evidence
  digested over the canonical JSON text the pack view already writes (ADR-0074), and a bounded map
  of names to digests as the caller's binding.
- **B.** Export the trail's record through the Go API, unchained and unsigned.
- **C.** Write nothing new, and document how a caller assembles a record.
- **D.** As A, but digest the exact bytes the caller passed for the facts and evidence.
- **E.** As A, but digest the documents in RFC 8785 form, numbers as IEEE 754 doubles.
- **F.** As A, but take one ADR-0050 binding (`kind` and `digest`) instead of a map.

## Decision outcome

Chosen option: **A**, because it gives every in-process caller one form whose bytes two callers can
compare, without writing anything, without a second evaluator and without moving a byte any
existing surface writes. B would carry the trail's run, time and chain members empty or invented,
and tie the trail's form to a public Go type: the trail is not changed here. C leaves the
disagreement in place. D gives two digests for one document spelled two ways, so two callers who
read their facts from different encoders never agree. E writes `12345678901234567890` and
`12345678901234567891` as one number, so two different documents could share a digest. F cannot
hold two commitments, which a caller with more than one input of its own needs; a map whose names
follow ADR-0050's kind grammar holds an ADR-0050 binding as one entry.

**The call.** `Pack.EvaluateWithRecord(ctx, facts, evidence, binding)` returns
`(*Evaluation, record.DecisionRecord, error)`. It checks the context on entry, as `Evaluate` does,
then the binding, before anything is evaluated; then calls `Evaluate`; then decodes the facts and
evidence documents again with the carrier decoder under the default limits, which are the decoder
and the limits the evaluation used, so a document `Evaluate` accepted decodes again; then builds
the record. The `Evaluation` is the one `Evaluate` returns, and every error but the binding's is
one `Evaluate` returns for the same inputs or a fault of this runtime wrapping `ErrInternal`
(`JPS-RECORD-INTERNAL`).

**The record.** One canonical JSON text, with its own digest, the SHA-256 of that text in the
`sha256:` form. Its members, in the order the canonical form writes them:

| member | value |
|---|---|
| `binding` | the caller's binding, an object of names to digests, as given; omitted when none was given |
| `disposition` | the §8.3 portable disposition, the bytes `CanonicalDisposition` returns |
| `evaluator` | `runtimeVersion`, the module version `Describe` reports, omitted when the build information gives none; and `specVersion`, the JPS Core evaluator contract applied |
| `evidenceDigest` | the digest of the evidence document as evaluated |
| `factsDigest` | the digest of the facts document as evaluated |
| `pack` | `digest`, `id`, `specVersion` and `version`, as `Pack.Identity` returns them |
| `recordVersion` | `"1.0.0"` |

The canonical form is the one `view.PackView.JSON` writes (ADR-0074): object members ordered by the
UTF-16 code units of their names, RFC 8785's string escapes, no white space, no trailing newline,
and every number written as the literal token the document holds. The writer moves, unchanged, to
`sdk/internal/canonjson`, so the view and the record share one writer and the view's bytes stay as
they were.

**The facts and evidence digests.** Each is the SHA-256 of the canonical text of the document as
the carrier decoder read it for the evaluation, not of the bytes the caller passed. Two documents
that differ only in white space, member order or how a string is escaped have one digest; two that
write one number differently, `1.5` and `1.50`, have two. An omitted evidence document is the empty
object JPS Core §8.2 evaluates it as, so an omitted document and a supplied `{}` give one record;
whether a document was supplied is not carried, because the evaluation does not depend on it.

**The binding.** `record.Binding` maps a name to a digest. A name is 1 to 128 characters of `a-z`,
`0-9`, `.`, `_`, `/` and `-`, the grammar of ADR-0050's kind; a digest is `sha256:` and 64
lower-case hexadecimal digits; a binding holds at most 16 names. A nil or empty binding is no
binding, and the member is omitted. A binding not of that shape is refused with an error wrapping
`record.ErrBinding`, in a fixed sentence that names the rule broken and repeats no name or digest.
It is not a `*Refusal`: the binding is not an input of the evaluator, and no JPS Core class names
it. The record keeps its own copy. The runtime never reads meaning into a name or a digest, never
compares a digest with anything and never recomputes one, and neither the evaluation nor the
disposition depends on it.

**The record version.** `recordVersion` is the record form's own version, `"1.0.0"`, and not the
trail record's. Its major part moves when a member is removed or renamed or its value is taken over
other bytes or written in another form; its minor part when a member is added; its patch part when
the bytes change in any other way. It does not move when the runtime learns to record evaluations
it refused before, or when the runtime version the record carries changes.

**The chosen outcome's annotations.** `Evaluation.OutcomeAnnotations` returns the annotations of
the outcome a disposition chose, as `Pack.View` carries them on that outcome (Core 0.5.0), read
from a view built once per `Pack`. It is beside the record, not in it: the record's bytes, the
disposition and `Evaluation.JSON` do not carry the annotations. Carrying them in the disposition or
the trail, and the rules that contributed, stays with issue #355.

**What a record establishes.** That this runtime, by the version it reports, given pack bytes with
this digest and a facts and an evidence document with these canonical texts' digests, decided this
disposition under this evaluator contract, and that the caller stated this binding. Anyone holding
the same pack, documents and binding can evaluate them under the same runtime version, write the
same bytes and compare the digests.

**What a record does not establish.**

- It is not a trail entry. It is appended to no trail, carries no run, time, sequence or link to an
  earlier record, and nothing `jpack audit` reads covers it. A timestamp, where a caller wants one,
  is the caller's and outside the record.
- It is unsigned. Its digest names its bytes and says nothing about who wrote them: anyone holding
  the inputs and this runtime can write the same bytes, and anyone can write other bytes. Signing
  is issue #391.
- The binding is the caller's assertion. It is held to its shape and otherwise carried as given.
- The facts and evidence appear by digest only. The record does not hold them and cannot reproduce
  them; a reader who needs them keeps them.
- The runtime version is the program's build information's account of the module, not a check of
  the code that ran; ADR-0043's executable digest, which the trail records, is not taken here.

### Consequences

- Good, because every in-process caller keeps one form, and two records of one evaluation agree
  byte for byte.
- Good, because nothing is written, no existing byte moves, and the conformance claim is unchanged:
  the method reaches the shared evaluator only through `Evaluate`.
- Bad, because a new public package and a versioned byte form are kept from now on, experimental
  while the module is pre-1.0.
- Bad, because the record names the facts by their canonical text while the trail keeps the exact
  bytes; matching a record with a trail record of the same evaluation needs the facts
  re-canonicalised in this form.
- Bad, because the binding's grammar now has two homes, ADR-0050's single object and this map, and
  a change to one must be weighed for the other.
- Bad, because the facts and evidence are decoded a second time after the evaluation.
- Revisit when signing (#391), batch evaluation (#357) or annotations on the disposition (#355)
  reach the record, or when a reader needs the record kept in the trail.

## More information

- Issue #387; ADR-0073 (the Go API), ADR-0074 (the view and its canonical form), ADR-0018 (the
  trail), ADR-0050 (a caller's binding on the trail record).
- Code: `sdk/record.go`, `sdk/record/record.go`, `sdk/internal/canonjson`, `sdk/view.go`
  (`OutcomeAnnotations`).
