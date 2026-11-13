---
status: proposed
date: 2026-11-13
deciders: maintainer
---

# Name the facts on a decision record by a digest the caller keys, in a second form, and publish the canonical text

## Context and problem statement

A decision record (ADR-0079) names the facts and evidence documents by `factsDigest` and
`evidenceDigest`, the SHA-256 of each document's canonical text (issue #400). Where the facts are
drawn from a small space, an identifier, an amount, a code, anyone holding the record can write
each candidate document, digest it and compare: the digest is a guessable pseudonym of the facts.
The evidence document is a map from the pack's declared requirements to `present`, `absent` or
`unknown`, so anyone holding the pack can try every evidence document and recover it.

A caller that wants to key these digests, or a second implementation that wants to recompute them,
also has no way to obtain the text they are taken over: the canonical form lives in an internal
package, it is not RFC 8785 for numbers, and it is taken over the document as the carrier decodes
it, not over the bytes passed.

Material-decision impact: public-surface, security.

## Decision drivers

- The plain form's bytes do not change: every record `EvaluateWithRecord` wrote before stays the
  record it writes now, at `"1.0.0"`, and a reader of that form reads it as before.
- The key never reaches this runtime: no parameter, field, error or log line of package `sdk` or
  `sdk/record` holds it.
- A keyed record is never read as a plain one, nor the reverse, by this runtime's reader or by a
  reader written to the 1.0.0 form alone.
- A check that cannot be made is reported as such: a keyed record checked without the key is
  neither a match nor a mismatch.
- The labels a record names are a closed list, and what a record carries is bounded and refused
  when malformed, never dropped in silence.
- The Go API keeps its boundary (ADR-0073): no files, environment or network, and no second
  evaluator.

## Considered options

- **A. The runtime computes the keyed digest from a key the caller passes.** Simple to call, and
  the runtime could check that the digest is of the documents evaluated. Rejected: the key would be
  in this runtime's memory, arguments and, on any mistake, its errors; the driver is that it is
  never there. A caller whose key lives in a key-management service could not use it at all.
- **B. The caller computes the keyed digests over the canonical texts this runtime publishes; the
  record carries them, the algorithm's label and the caller's key identifier, and no plain digest
  (chosen).**
- **C. The keyed digest in `factsDigest`, with another prefix, such as `hmac-sha256:`.** No new
  member. Rejected: a reader that takes `factsDigest` as "the digest of the facts", or tests its
  prefix loosely, reads a keyed value as a plain one, and a 1.0.0 reader that ignores the version
  would accept the record. Distinct members make that impossible: a reader looking for a plain
  digest finds none.
- **D. `keyedDigests` beside `factsDigest` and `evidenceDigest`, at `"1.1.0"`.** Additive, so a
  minor version. Rejected: the plain digests the keyed form exists to replace would stay in the
  record.
- **E. No document digests at all.** Rejected: the record could no longer be checked against the
  documents by anyone, key or not.
- **F. A plain digest with a salt carried in the record.** Rejected: a salt in clear does not stop
  anyone holding the record from trying candidates.
- **G. An open list of labels, the caller naming any algorithm.** Rejected: a reader could not
  tell a label it does not know from a mistake, nor the digest size to admit.

For the algorithm list, beside HMAC-SHA-256:

- **HMAC-SHA-512.** Rejected for now: the protection is the key's secrecy, and a 256-bit output is
  not the weak point. A second label is a second way for two holders of one key to write two
  records of one evaluation that do not agree.
- **KMAC256 or HMAC over SHA-3.** Rejected for now: not in the standard library of every common
  language nor offered by every common key-management service, which a caller computing the
  digest outside this runtime needs.

A reader must refuse a label it does not know, as `record.Read` does: one that passed over it and
computed HMAC-SHA-256 would report a mismatch where it cannot check. A label added later is
therefore a change a reader of the earlier bytes cannot pass over, and takes a new major number by
the version table below.

## Decision outcome

Chosen option: "B", because it keeps the key with the caller and makes the two forms unmistakable
by their members, not only by a version a reader might not test.

The keyed form removes the guessable document digests and nothing more: a reader without the key
cannot test candidate facts against the record's document digests, but the record still carries
the disposition and names the pack by id and digest, so a holder of the pack can evaluate a
candidate and compare dispositions, which rules candidates out and confirms one outright where a
rule singles out a value.

- `record.CanonicalFacts` and `record.CanonicalEvidence` return the canonical text, and
  [docs/decision-records.md](../decision-records.md) states it with vectors a test reads from the
  page.
- `Pack.EvaluateWithKeyedRecord` takes a `record.KeyedDigests`: `Algorithm` (`hmac-sha256`, the one
  label), `KeyID` (1 to 128 characters of `A`–`Z`, `a`–`z`, `0`–`9`, `.`, `_`, `-`, `:` and `/`) and
  the two digests (32 bytes each). They are checked before the evaluation. After it, a keyed digest
  or a binding digest that is the plain SHA-256 of either document, as passed or as canonical text,
  or the digest of the plain record of the same evaluation, is refused, and so is a key identifier
  that holds one of those digests in hexadecimal or base64. These refusals catch one plausible
  mistake and are not a defence: a digest of anything else derived from the facts is not detected.
- The keyed record has `binding`, `disposition`, `evaluator`, `keyedDigests` (`algorithm`,
  `evidence`, `facts`, `keyId`, the digests in lower-case hexadecimal), `pack` and `recordVersion`
  `"2.0.0"`. The major part of the version names the form, by a table: 1 plain, 2 keyed. A change a
  reader of the earlier bytes would misread takes the next major number not yet in the table, never
  the next number of the form it changes, so the forms' numbers never collide; removing
  `factsDigest` and `evidenceDigest` took 2. A reader decides the form by the table and the members
  together, and refuses a record where they disagree.
- `record.Read` admits exactly the two forms' member sets at exactly their two versions, and holds
  the disposition to a §8.3 disposition of the evaluator contract the record names (no draft
  `value` member), that contract to one the reader knows, and the pack's id, version and
  specVersion to their grammars.
  `DecisionRecord.CheckDocuments` reports `match`, `mismatch`, or `not-checkable-without-key` for a
  keyed record checked without the caller's `KeyedDigestFunc`, which is the only place the key is
  used and is the caller's code.

What in each form follows from the facts:

| member | plain | keyed |
|---|---|---|
| `factsDigest`, `evidenceDigest` | present, guessable | absent |
| `keyedDigests.facts`, `.evidence` | absent | present, not computable without the key |
| `keyedDigests.algorithm`, `.keyId` | absent | present, not from the facts unless the caller makes the identifier so; one holding a plain digest is refused |
| `disposition` | present: the decision itself, from the pack's declared vocabulary; its reasons say something of the evidence; a holder of the pack can test candidates against it | the same |
| `binding` | the caller's | the caller's, a plain digest of either document or of the plain record refused |
| `evaluator`, `pack`, `recordVersion` | not from the facts | the same |
| `Digest()` of the record | guessable, through the members above | not predictable from candidate facts without the key |

### Consequences

- Good, because a record can be kept or passed on without carrying a guessable digest of the
  facts or evidence documents (its disposition still says what was decided), and the plain form,
  its bytes and its readers are unchanged.
- Good, because the canonical text is now a stated, tested form, so a second implementation can
  reproduce a plain digest, and a caller can key it, without reading this runtime's source.
- Bad, because this runtime cannot tell whether the keyed digests a caller passes are of the
  documents evaluated: they are the caller's assertion, as the binding is.
- Bad, because records under one key are linkable by their keyed digests, the evidence digest most
  of all, and the disposition still says what the pack decided, in either form.
- Bad, because a caller that binds a plain digest of the facts document (a preparation step's
  digest of its output, when that output is the facts document, say) cannot use the keyed form
  until it binds something else, and one that binds another derivation of the facts re-creates the
  pseudonym undetected.
- Bad, because `record.Read` refuses a record naming an evaluator contract it does not know, so a
  reader of this version refuses a later runtime's record of a newly admitted specification version
  at a record version it knows.
- Revisit when a caller needs a second algorithm, when records are signed, or when the
  specification defines a decision record of its own.

## More information

- [docs/decision-records.md](../decision-records.md): the forms, the canonical text, the vectors,
  the check and the full table of facts-derived members.
- ADR-0079, the decision record; ADR-0073, the Go API boundary; ADR-0038, unpaired surrogates.
- `sdk/record/keyed.go`, `sdk/record/read.go`, `sdk/record/canonical.go`; golden records in
  `sdk/record/testdata/`.
