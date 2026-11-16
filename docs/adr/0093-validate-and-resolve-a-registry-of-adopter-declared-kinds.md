---
status: accepted
date: 2026-11-16
deciders: maintainer
---

# Validate and resolve a registry of adopter-declared kinds, as a top-level command

## Context and problem statement

The specification accepted RFCs 0034 and 0035: a registry of entries of kinds that adopters declare,
each kind with a closed JSON Schema subset, per-environment overlays, entry references, secret
references pinned to a version and scoped to an environment's projects, a fixed validation order
with a value-free report, and resolution to one document per environment. The companion text
(`spec/registry.md`), its schemas and its vectors are not released yet. The runtime needs a reader
now, so emitters, facts contracts and adopters can build against it. It must not claim conformance
to a text that is not released yet.

Material-decision impact: public-surface.

## Decision drivers

- One reader, following the RFCs' order and codes exactly, so the companion's vectors can be
  checked against it when they exist.
- Nothing fetched: a secret reference is checked for form, pin and scope and left as written.
- A report never carries a value from the documents: no secret reference, entry reference or
  member value, only codes, fixed messages and locations.
- Deterministic bytes: the resolved document is RFC 8785 canonical JSON, independent of the order
  kinds are supplied in.
- No existing bytes or behaviour change.

## Considered options

- A top-level `jpack registry` group now.
- `jpack experimental registry` until the companion text is released.
- A library only, with no command.

## Decision outcome

Chosen option: "a top-level `jpack registry` group", because the RFCs are accepted and the
companion is intended for stable maturity at its first release. Adopters' CI steps and emitters
need a command name that will not move. The experimental surface is for designs that may be
withdrawn, which this one is not. A library alone would leave emitter authors and CI steps without
a command.

`internal/registry` implements RFC 0034 §3's eight steps:
1. admission of each document (size bound, strict I-JSON, depth 32, the number rules, the exact
   selector);
2. kind declarations;
3. the registry's shape;
4. pins;
5. entries;
6. overlays;
7. resolved values per environment, against the schema and the references;
8. bounds.

`internal/jcs` gains `EncodeDocument`, full RFC 8785 for any JSON value (null and non-integers
included); `Encode` is unchanged.

Choices the RFCs leave to the reader, recorded here so the companion can settle them:

- **Locations.** A location names its document (`registry`, `kind` or `value`). A kind document is
  named by the sha256 of its raw bytes, because one that fails admission has no readable `kind`.
- **What stops validation in step 3.** A form error at the registry level (a member, the
  environments, owners or scopes) stops before step 4. A malformed entry, overlay or patch is
  dropped and reported, and validation goes on. An order error is reported and does not stop it.
- **Pointer reachability is permissive** through `$ref`, `allOf` and `anyOf`. A pattern is
  unreachable only when no branch could hold it.
- **`secrets` and `references` are optional**, empty by default. The empty pointer pattern (the
  whole value) is refused.
- **A malformed overlay member** is reported as `REG-REGISTRY-FORM` in step 6.
- **A document over its size bound** is a validation finding (`REG-DOC-SIZE`, exit 1), not an
  operational error, and validation goes on with the other documents. The reader stops at the bound
  and digests nothing it did not read. A resolved document over its bound is a step 8 finding for
  its environment, so `resolve` never meets one.
- **Nothing that could be a value is named.** A location names an entry only when its kind and id
  are well formed, and an overlay `set` key that is not a pointer, or that no overlayable pattern
  matches, is located at the `set` member. The report names the registry by its digest only.
- **A failed patch fails its (entry, environment) pair.** Steps 7 and 8 skip the entry in that
  environment, as RFC 0034 §6 runs each check only on what passed the earlier ones.

Commands:

- `validate REGISTRY --kind K...` writes the report (`reportFormatVersion` `1.0.0`). It exits 0 when
  the registry is valid (warnings allowed) and 1 when it is not.
- `resolve REGISTRY --kind K... --environment E` writes the canonical resolved document and one
  newline. An invalid registry writes the report and exits 1.
- An invocation error exits 3 and an unreadable input exits 4, as elsewhere.

### Consequences

- Good, because emitters (RFC 0036) and facts contracts (RFC 0037) can be built on one resolved
  form now.
- Good, because a CI step can gate on `validate`'s exit code without its log showing a secret.
- Good, because the formats carry RFC 0034 §8's promise: within 1.x nothing is removed or changes
  meaning, and additions come only under a new minor selector (VERSIONING.md).
- Bad, because the RFCs define only the `REF-*` codes and `REG-DOC-NUMBER`; every other `REG-*` code
  and the location members are this runtime's own reading. That list is provisional until
  `spec/registry.md` is released, and a different list there is adopted under a new selector.
- Revisit when `spec/registry.md` and its vectors are released: the claim goes into CONFORMANCE.md,
  and YAML authoring (RFC 0034 §7) is read through the same steps.

## More information

Spec RFCs 0034 and 0035 (accepted 2026-11-16). `internal/registry`, `internal/jcs/full.go`,
`internal/cli/registry.go`.

## Amendment: a bounded report (2026-11-16, #427)

A registry document within its 8 MiB bound can fail on every entry. It can also find thousands of
warnings, one `REF-SECRET-UNSCOPED` for each secret position of each entry in each environment.
Without a bound, the report grows with the document and can outgrow it, and the reader's memory
grows with the report.

`validate` and `resolve` report at most the first 1000 diagnostics in the report's own order
(location, then code, then message). The collector keeps the 1000 smallest distinct diagnostics in
a sorted list that it trims as it inserts. A diagnostic that sorts past the 1000th costs no memory.
So a capped report is exactly the first 1000 diagnostics of the uncapped one, in the same order. It
does not depend on the order the checks found them in, or on the order the kinds were supplied in.
When anything was dropped, the report ends with one `REG-REPORT-TRUNCATED`, a warning located at
`{"document":"registry","pointer":""}`. Like every location, it carries no value. The report has no
member for counts, and none is added.

- **Every check still runs.** The report's order is not the order the checks run in: entries are
  ordered by kind and id, `/entries/10` sorts before `/entries/2`, and step 8's registry findings
  sort before step 7's value findings. Stopping at the 1000th diagnostic found would therefore not
  report the first 1000. The work is linear in the input, which the 8 MiB admission bound already
  caps, and `resolve` needs every check's result anyway. So the work does not stop early; only what
  is kept is bounded.
- **Validity is judged on every diagnostic found**, counted by severity whether kept or not. A
  registry with more than 1000 warnings and no error is valid, and `resolve` resolves it. A
  registry whose only error sorts past the 1000th diagnostic is invalid. The exit code follows
  validity, as before. This is why `REG-REPORT-TRUNCATED` is a warning: it says the report is
  partial, not that the registry is invalid.
- **Ties are broken by code, then by message.** So distinct diagnostics never compare equal, and the
  kept 1000 and their order are fully determined. One comparison serves both the bounded list and
  the final sort.
- **Import's bound differs** (ADR-0094). `import` stops reading at its 1001st refusal and reports
  `REG-YAML-TRUNCATED` where it stopped. A YAML refusal is located by line and column, which reading
  reaches in order, so stopping early reports the first refusals. Once a file is refused there is
  nothing left to judge, and an alias-heavy file could hold millions of refusals. Validation reports
  in an order its checks do not follow, and it judges validity and resolves on everything found. So
  it bounds what it keeps instead of stopping.

`REG-REPORT-TRUNCATED` is provisional, like the other `REG-*` codes, until `spec/registry.md` is
released.
