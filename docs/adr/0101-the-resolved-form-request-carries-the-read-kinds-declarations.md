---
status: accepted
date: 2026-11-16
deciders: maintainer
---

# The resolved-form request carries the read kinds' declarations

## Context and problem statement

Spec RFC 0036 §2 gives an emitter the pins of the kinds it reads: each kind's name, id, version and
digest, and nothing of the declaration itself. An emitter opens no file (§1), so the reference
emitters of registry companion §11.6 cannot see a kind's schema or its secret positions. Spec issue
[#352](https://github.com/Judgment-Pack/judgment-pack-spec/issues/352) settles this in companion
§11.2 (drafted, with a §17 row). In the resolved form, the request gains a top-level `declarations`
member. It holds each read kind's declaration exactly as admitted, in its RFC 8785 form, sorted by
`kind`, and the SHA-256 of each one is its pin's `digest`. An emitter must refuse a missing or
mismatched declaration. The facts-contracts form does not change, and the change is part of request
`1.0.0`.

This record amends ADR-0098 (the request) and ADR-0099 (a given request's envelope, and check 4).

Material-decision impact: public-surface.

## Decision drivers

- The declaration an emitter reads must be the one validation admitted and the pin names, byte for
  byte. A second read of a kind file could differ from it.
- No kind the manifest does not read may reach the emitter, in `declarations` as in the pins.
  Check 4 must keep testing that.
- A request whose pins and declarations disagree is never written. The host refuses it, and does
  not leave the emitter to find the problem.

## Considered options

- Take the declarations from what validation already holds (`registry.Result.Declarations`), with
  the pins' own filter. Take each digest again over the form the request holds, and check it one to
  one against every environment's pins.
- Read the kind files again in the host. Rejected: a second parse could differ from the admitted
  one, and its digest would not be the one validation checked.
- Put each declaration inside the resolved documents. Rejected: the companion keeps §8's documents
  exactly as `registry resolve` writes them, and `"all"` would then carry each declaration once per
  environment.

## Decision outcome

Chosen option: the first, because the bytes the emitter checks against its pins are then the bytes
validation checked, and no file is read twice.

**The reader.** `Result.Declarations(keep)` returns the declaration of each pinned kind that `keep`
admits, sorted by kind name: the kind, its pin's digest, and the document that step 1 admitted and
digested (`Kind.Document`). The document is not read again. It is shared with the reader and
read-only. `keep` is the filter that `ResolvedFor` applies to the pins. A registry that is not valid
returns `ErrInvalid`.

**The request.** In the resolved form, `FormRequests` writes `declarations`, `emitRequestVersion`,
`emitter`, `options` and `resolved`, in RFC 8785's member order, for `"each"` and `"all"` alike.
Every request of a run holds the same declarations, and `"all"` holds each one once. The host takes
each declaration's digest again, over its RFC 8785 form, because RFC 8785 nests: a value's bytes
inside the request are its own canonical form. Before any request is written, each environment's
pins and the declarations must be of the same kinds, one to one, and each pin's digest must be its
declaration's. Otherwise the run is refused with `EMIT-REQUEST`, located at the registry and the
environment, before any emitter starts. A valid registry cannot fail this check. It stays a
refusal so that it is never a silent pass. The facts-contracts form has no `declarations`.

**The version.** The change is part of request `1.0.0`, with no new selector. VERSIONING.md says
that any change to a request's bytes for the same inputs moves `emitRequestVersion`. That rule takes
effect from the first tagged release that has `jpack emit`. Request `1.0.0` has not been in one: the
host is under `## Unreleased`, and the companion is proposed.

**The harness.** A given request in the resolved form is held to five members, with `declarations`
an array. In the facts form it is held to four. The declarations' content is the author's, so a
vector can give one that does not match its pin, for an emitter to refuse. Check 4 compares the
whole request. With the synthetic kind added, `declarations` must stay byte-identical, and nothing
in it is restored. The golden vectors' `request.json` files were written again by
`jpack emit conformance --update`.

**What does not change.** An emitter diagnostic's location is still resolved as ADR-0098 says.
Without an entry, a location resolves only at pointer `""`, so a pointer into a declaration is
refused, even though the request now holds the declaration. `cmd/jpack-emit-go-facts` reads only
the facts form, which is unchanged. The tests' emitter reads the request loosely.

### Consequences

- Good: an emitter sees each read kind's schema and secret positions, bound to its pin by digest.
- Good: check 4 holds the declarations to the same property as the pins, with no new check.
- Bad: a request grows by its read kinds' declarations, each at most 256 KiB (`MaxKindBytes`). The
  host already holds them. A request is at most 64 MiB of RFC 8785 bytes (`MaxRequestBytes`, the
  response's bound and the harness's for a given request), and the host refuses a larger one, any
  request of `"each"` or the one of `"all"`, with `EMIT-REQUEST` before any emitter starts.
- Neutral: check 3's hostile request has no declarations. The host's response step answers it, and
  no emitter is ever sent it.
- Revisit when the companion's §11.2 text changes in review, or when a location into a declaration
  should resolve.

## More information

- Spec issue [#352](https://github.com/Judgment-Pack/judgment-pack-spec/issues/352), registry
  companion §11.2 and §17 (drafted). ADR-0098 covers the host, and ADR-0099 the harness.
- Code: `internal/registry/emit_support.go` (`Declarations`), `internal/emit/request.go`
  (`readDeclarations`, `declaresPins`), and `internal/emit/conformance/vectors.go` (`givenRequest`).
