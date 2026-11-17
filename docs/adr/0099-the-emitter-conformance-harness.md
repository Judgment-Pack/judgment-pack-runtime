---
status: proposed
date: 2026-11-16
deciders: maintainer
---

# The emitter conformance harness, `jpack emit conformance`

## Context and problem statement

RFC 0036 §5 defines a harness that checks an emitter against its author's vectors. The registry
companion (`spec/registry.md` §11.5, proposed) restates it, and it names `EMIT-STRAY-WRITE` for
the harness. ADR-0098 built the host (§§1–4) and kept its run step, `emit.Run`, for the harness to
reuse. This record covers the harness only. The reference emitters (§6) are later work.

Five points are open in the text. Check 4 as written fails an emitter that writes the registry
digest, which §4 allows. Check 5 as written fails a file that holds two whole references of one
project. Check 6 names no bounds for its repository copy. Check 3's hostile vectors are not
listed. And the RFC fixes no vector layout beyond the members it names.

Material-decision impact: public-surface.

## Decision drivers

- A harness run must mean what a `jpack emit` run means: the same request bytes, the same run
  step and the same response checks, not a second implementation of them.
- A passing vector set must keep passing under every later 1.x harness (§5, harness levels).
- The report must be safe to post, as the host's is: no file contents, request values or env
  values.
- A refusal is never a silent pass. A check the harness cannot complete stops the run, and the run
  exits 2.

## Considered options

1. **A package `internal/emit/conformance` over the host's exported steps.** It uses
   `emit.FormRequests`, `emit.Run` and `emit.ParseResponse`, plus three small exports
   (`emit.PathSet`, `emit.RequestVersion` and `emit.ModeOf`) and one read of the registry
   (`registry.Result.SecretStrings`). None of these changes what the host does.
2. **A test-only harness in the host's own tests.** This would not give authors a command, and §5
   names one.
3. **Substitute hostile responses through `emit.RunOptions`.** This would add a host hook that no
   host run needs. Instead, the hostile responses go to `emit.ParseResponse`, the step `emit.Run`
   applies to an emitter's standard output.

## Decision outcome

Chosen: option 1, with option 3's alternative.

**Command.** The command is
`jpack emit conformance --manifest FILE --vectors DIR [--update] [--root DIR] [--timeout 120s]`.
It exits 0 when every row passes, is skipped or is info, and 1 when a check fails. It exits 2 when
the harness could not run: a refused manifest, vectors not in the layout, an unreadable path, or a
bound. An unusable command line exits 3, as it does for every `jpack` command. The report is
RFC 8785 canonical JSON on standard output.

**Vectors.** Each subdirectory of `DIR` is one vector, read in name order. It holds exactly one of
the following inputs:

- `request.json`;
- `registry.json` with `kinds/` (its `*.json`, as `--kinds DIR` reads them) and an optional
  `options.json`.

It also holds exactly one of these expectations:

- `expected/`, which must hold regular files only;
- `expected-failure.json`, `{"code": CODE}`. `CODE` is a listed `EMIT-` error code or an emitter's
  `x-` code.

Anything else in a vector is `EMIT-VECTOR`: an unknown member, a link, or a vector past a bound.
Every vector is read before any emitter runs, so a bad layout refuses the run whole.

The two inputs are formed differently:

- **A registry vector** is formed with `emit.FormRequests`, as `jpack emit` forms it, from the
  registry and its kinds, each read under the host's bounds. An oversize options document is
  `EMIT-OPTIONS`, as the host reports it.
- **A given request** is held to the envelope the host writes for the manifest, then written in
  its canonical form. The envelope is: exactly the four members, the request version the host
  writes, the manifest's emitter, options valid against its schema, the input member as an array,
  and one environment for `resolved` with `each`. What the request carries inside is the author's.

`DIR/harness.json` is exactly `{"harnessLevel": LEVEL}`. Without it the level is `1.0`, and an
unknown level is `EMIT-VECTOR` and exit 2.

**`--update`** rewrites `expected/` of each vector whose run succeeded, from that run. It never
touches an expected-failure vector. It writes nowhere but `VECTOR/expected/`, which must be a
directory or absent, and it removes that directory first. The vector directories are not links.
The checks then run against the new `expected/`.

**Run 1** is the emitter's run in the repository copy, one per request, as `jpack emit` makes it.
Two runs that give one path are `EMIT-RESPONSE`, as the host refuses them.

**Checks of level 1.0.** Each is one row per vector:

1. **Golden.** The files of all runs must equal `expected/`, byte for byte, and mode for mode
   except on Windows. A vector with `expected-failure.json` passes when an error the run reports
   has that code: the host's, or the emitter's.
2. **Determinism.** Runs 2 and 3 each have a fresh `HOME` at a path of another shape. They set
   `TZ` to `Pacific/Kiritimati` and then `America/St_Johns`, `LANG` to `tr_TR.UTF-8` and then
   `de_DE.UTF-8`, and `SOURCE_DATE_EPOCH` to `1` and then `2000000000`. Run 1 has none of these
   values. The manifest's `env` names are held as the host gives them. Each run must give run 1's
   response bytes exactly (the RFC's "identical bytes", read as the raw bytes, not a canonical
   form of them), or run 1's refusal exactly. The row names
   the runs that differ.
3. **Path safety and bounds.** A vector passes when every response passed the host's §3 checks,
   which `emit.Run` applies to the whole response. A response that failed only on the error
   diagnostics the emitter reported is of its form. A vector that expects `EMIT-RESPONSE` passes
   when the host refused its response. The harness also adds one row for each of its hostile
   responses, described below.
4. **Kind filtering.** This is decided below.
5. **References.** This is decided below.
6. **Stray writes.** The copy is compared with what it held once the vector's runs end. Anything
   changed is `EMIT-STRAY-WRITE`: a path removed, or a path changed in kind, bits, size, bytes or
   link target, each reported by its own path. A path created is reported at the nearest directory
   that existed before (`.` for the root), so no name the emitter chose reaches the report. Then
   the copy is made afresh for the next vector.

A row skips with a fixed reason when its check cannot apply:

- the request is refused before the emitter runs (checks 2–6);
- no response was read (check 3);
- the vector gives its request whole (checks 4 and 5);
- the request is in the facts form (check 5);
- the run failed (check 5);
- links cannot be made (check 6).

**Check 4, the decided reading.** The harness adds a synthetic kind that the manifest does not
read: its declaration, its pin, and one entry, owned by the registry's first owner. Each goes in
the place the registry's order rules require. With those added, it forms each request again, the
same way in the facts form. It then restores every `registry.digest` in the changed request to the
original request's value at the same place. That place is `/resolved/N/registry/digest` and, in
the facts form, a contract's `source.registry.digest` if a source carries one. Next it asserts
that the result is byte-identical to the original request. A difference fails the row. The reason
says whether the request holds the synthetic kind's id. Only then does the harness run the emitter
on the restored request and assert that the response is unchanged. The digest is restored because
§4 lets an emitter write the registry digest into a generated file's header, and the synthetic
entry changes that digest. Without restoring it, a conforming emitter would fail. This reading is
filed against companion §11.5 as spec #350. A registry that is not valid once the synthetic kind is added
is `EMIT-HARNESS-BOUND`, and the run exits 2. A vector with no registry skips, with that reason.

**Check 5, the decided reading.** For each string R at a secret position of the request and each
output file, the row fails when the file holds a run of at least 32 consecutive characters of R
(all of R, if R is shorter) that does not lie inside a complete occurrence of any string at a
secret position of the request. The text says "a complete occurrence of R". Read that way, a file
that holds two whole references of one project fails, because they share
`secret:gcp-sm:projects/<project>/secrets/`, 43 characters. The RFC's rationale, a reference cut
short or rewritten, needs no such failure. The run must lie inside one occurrence: two overlapping
occurrences do not together hold a run that neither holds. This reading is to be raised on spec
#350, with check 4's.

Secret positions are found the registry's way: the read kinds' `secrets` patterns, applied to each
environment's resolved entries (`SecretStrings`). With `each`, a file is checked against its own
environment's strings. With `all`, it is checked against every environment's. A run of all of a
string of at most 32 characters is a complete occurrence, so only longer strings can fail, and
only strings of 32 or more can hold a run that could. A reference at a secret position of a valid
registry is ASCII (RFC 0035), so a file's text and its bytes read as Latin-1 hold the same runs.
The scan therefore reads bytes.

The scan has two stages:

- **Finding regions.** A table of every 32-byte window of every string, with a rolling-hash
  filter, finds where a string's runs can be.
- **Checking regions.** A suffix automaton of each string that occurs reads only its regions. A
  first pass over every such string finds the complete occurrences of all of them. A second pass,
  for each string longer than 32, asks whether one of those occurrences holds each maximal run of
  32 or more.

The work is linear in the files' bytes for each string that occurs, and two bounds hold it:

- `MaxReferenceBytes`, 1 MiB of distinct strings of 32 or more;
- `MaxReferenceWork`, 2^32 steps a vector.

Past either bound, the result is `EMIT-HARNESS-BOUND` and exit 2. A row that fails names the file,
the entry and the pointer, never the string.

**Stray writes and their bounds.** The emitter runs in a fresh copy of `--root`, made in a new
temporary directory with its links resolved. Nothing named `.git` is copied, in any case, and no
path under it is compared. Regular files keep their permission bits, links are copied as links,
and any other kind of file is `EMIT-PATH-UNREADABLE`. The copy is bounded at 256 MiB of file bytes
and 100 000 entries (`MaxCopyBytes`, `MaxCopyEntries`). Past either, the result is
`EMIT-HARNESS-BOUND` and exit 2. A system that cannot make a link the repository holds (Windows
without the privilege) runs the emitter in the repository itself, and each check 6 row skips, with
that reason. The copy keeps an honest emitter's mistake out of the repository. It is not a
sandbox: the emitter runs with the user's rights.

**Hostile responses.** Each is passed to `emit.ParseResponse` for a fixed manifest of the
harness's own that owns `gen/**`, with request version `1.0.0` and a fixed request of one
environment, `dev`, one kind, `queue`, and one entry, `queue/orders`, which a diagnostic's location
is resolved against. They do not go through a hook in
`emit.RunOptions`: `ParseResponse` is the step `emit.Run` applies to every emitter's standard
output, so a refusal there is the host's refusal, and no host run needs the hook. This is decided.
Each is a row of its own,
`hostile/NAME`. A row passes only when the host refuses the response with `EMIT-RESPONSE` at the
named pointer.

| Hostile | Pointer |
| --- | --- |
| path-traversal, absolute-path, drive-letter, backslash, git-directory, tilde-segment, trailing-dot, device-name, path-too-long (513 bytes), outside-owns | `/files/0/path` |
| duplicate-path, case-folded-duplicate, directory-of-a-file, unsorted | `/files/1/path` |
| unknown-member | `/files/0/size` |
| text-and-base64 | `/files/0` |
| text-carriage-return, text-no-final-newline, file-too-large (16 MiB + 1) | `/files/0/text` |
| base64-malformed, base64-noncanonical | `/files/0/base64` |
| bad-mode (`0777`) | `/files/0/mode` |
| too-many-files (10 001) | `/files` |
| response-too-large (64 MiB + 1), not-utf8 | `""` |
| response-version-too-new (`1.1.0`) | `/emitResponseVersion` |
| diagnostics-missing | `/diagnostics` |

**Report** (`harnessReportVersion` `"1.0.0"`). The report holds the level applied, the outcome
(`pass`, `fail` or `not-run`) and the emitter's id and version. It has one row for each vector and
check, and one for each hostile. A row holds the vector, the check number and name, the status, and
the reason. It also holds the details its check gives: the expected code, the differing runs, the
host's and the emitter's error diagnostics, the differences, the references and the stray writes.
Each list is bounded at 1000. The report's own diagnostics give why it could not run, plus
`EMIT-DEPRECATED` and `EMIT-ENV-UNSET`, each once.

**Codes.**

- `EMIT-STRAY-WRITE` is the companion's.
- `EMIT-VECTOR` (the layout) and `EMIT-HARNESS-BOUND` (a check that could not be completed) are
  this runtime's, and provisional.
- An `--update` that cannot write is `EMIT-WRITE` on the vector, and the run exits 2.

### Consequences

- Good: a vector run is a `jpack emit` run. The request bytes, run step and response checks are the
  host's own, so a fix to the host is a fix to the harness.
- Good: check 4 tests the property itself, that the emitter never sees an undeclared kind, and it
  does not fail an emitter that writes the registry digest.
- Good: check 5 passes a file that holds whole references of one project, which share 43
  characters, and still fails a reference cut short or rewritten.
- Bad: every vector copies the repository and runs the emitter four times or more. A large
  repository makes a run slow, within the bounds above.
- Neutral: a vector that gives its request whole skips checks 4 and 5, each with its reason. It
  has no registry to add a kind to, and no kinds to find its secret positions by. This is decided.
- Revisit when the companion's §11.5 text or its harness rows land, or when a later level adds
  checks.

## More information

- Spec RFC 0036 §5 (Accepted 2026-11-16) and the registry companion `spec/registry.md` §11.5
  (proposed). ADR-0098 covers the host.
- Code: `internal/emit/conformance`, `internal/cli/emit_conformance.go`, and the golden vectors in
  `internal/emit/testdata/conformance/`.
- Raised on spec #350 (companion §11.5): check 4's reading, and check 5's reading that a run
  inside a complete occurrence of any secret-position string passes.
- Decided here: check 2 compares raw response bytes; a vector that gives its request whole skips
  checks 4 and 5; the hostile responses go through `emit.ParseResponse`.
