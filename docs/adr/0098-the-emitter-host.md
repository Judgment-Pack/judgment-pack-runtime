---
status: proposed
date: 2026-11-16
deciders: maintainer
---

# The registry emitter host, `jpack emit`

## Context and problem statement

The specification accepted RFC 0036 with RFCs 0034, 0035, 0037 and 0038. An **emitter** turns
resolved registries (ADR-0093) or facts contracts (ADR-0095) into files a repository commits: a
pure function from one request document on standard input to one response document on standard
output. The **host** does all the file-system work. It forms the request, runs the emitter, checks
the response, and writes or compares the files the emitter's manifest owns. The registry
companion (`spec/registry.md` §11, proposed) restates RFC 0036 §§1–4 as normative text, and its
§12 names the host's codes. Where the two differ, the companion governs.

This record covers the host only (RFC 0036 §§1–4). The conformance harness (§5,
`jpack emit conformance`) and the reference emitters (§6) are later work, and they reuse the host's
run step.

Material-decision impact: public-surface.

## Decision drivers

- A generator should not be able to write outside the paths it declares, through a path, a link or
  a name that two file systems read differently.
- Nothing may change on disk until every response of the run has been checked whole.
- Check mode must give the same answer in CI as on a laptop, and its report must be safe to post:
  paths, codes and digests, never contents or an env value.
- The emitter is code the repository chose to run. The host bounds it (time, output, environment)
  but is not a sandbox, and it does not claim to be one.
- The harness must be able to reuse the run step unchanged.

## Considered options

1. **A top-level `jpack emit` over a package `internal/emit`, reusing the registry reader.** The
   manifest, request, response, run and file steps are separate functions. `emit.Run` is the run
   step the harness calls.
2. **`jpack registry emit`.** This would put the host under the registry command it reads from.
   But RFC 0036 names the command `jpack emit`, and the harness is `jpack emit conformance`.
3. **Run emitters in a container or sandbox.** This is out of scope for RFC 0036, and it would
   make the host depend on a runtime that most CI images do not have.

## Decision outcome

Chosen: option 1.

**Manifest (§1).** `emit.ReadManifest` admits the manifest as the registry admits its documents:
I-JSON, depth 32, number rules and an exact selector, `emitterManifestVersion` `"1.0.0"`. It then
checks every member at its pointer (`EMIT-MANIFEST`):

- `reads` holds SemVer ranges (`^`, `~`, `=`, `>=a <b`).
- `options` is compiled with the registry's schema subset.
- `env` names follow the grammar, are not reserved and are not repeated.
- `command` and `owns` are relative, with no `..` segment.
- An owns glob uses `*` within one segment and `**` as a whole segment. A bare `**` is refused, and
  so is a glob with a `.git` segment in any case (see below).

`--other-manifest` names the repository's other manifests. Their owns globs must not overlap this
one's (`EMIT-OWNS-OVERLAP`). Overlap is decided exactly: two globs overlap when one path can match
both.

**Request (§2).** The registry is validated with ADR-0093's reader. Every pin of a kind the manifest
reads is checked against its range (`EMIT-KIND-RANGE`) and its kind id (`EMIT-KIND-ID`). Every
entry and pin of a kind it does not read is removed before the request is formed. The options are
validated against the manifest's schema (`EMIT-OPTIONS`). The request is
`{"emitRequestVersion", "emitter", "options", "resolved"}`, or `factsContracts` in place of
`resolved`, as RFC 8785 canonical JSON. It holds one environment for `"each"`, or all of them in
order for `"all"`. The host picks the highest request version it writes that the manifest lists
(`EMIT-REQUEST-VERSION`). Request `1.0.0` carries `resolvedFormatVersion` and
`factsContractVersion` `1.0.0`.

**Response (§3).** `emit.ParseResponse` admits the response, then checks the following. Each
failure is `EMIT-RESPONSE`, at its pointer.

- The members are exactly `emitResponseVersion`, `files` and `diagnostics`. The response version's
  major and minor do not exceed the request's.
- Each file has a path and exactly one of `text` or `base64`. Text ends in a line feed and holds no
  carriage return. Base64 is canonical and unbroken.
- The mode is `0644` or `0755`.
- Each path is relative and safe. It has no `.`, `..`, empty or `~` segment, no trailing `/`, no
  NUL, backslash or control character, and no drive letter. No segment is `.git`, in any case. It
  is owned.
- `files` is sorted by path, and no two paths are equal once case is folded and normalized. No path
  is a directory of another (`a` and `a/b`), since writing one would replace or block the other.
- Each emitter diagnostic has an `x-` code, a `warning` or `error` severity, and a §7.3 location.
  An `error` fails the run.

A path two runs of one host both give, or one that is a directory of another run's, is refused as
well, because the runs answer into one set of files.

**Bounds**, as named constants in `internal/emit/codes.go`:

| Bound | Constant | Value | From |
|---|---|---|---|
| A response | `MaxResponseBytes` | 64 MiB | §11.3 |
| Files in a response | `MaxFiles` | 10 000 | §11.3 |
| JSON values in a response | 4 × `MaxFiles` + 80 000 | 120 000 | this runtime's |
| A file | `MaxFileBytes` | 16 MiB, decoded | §11.3 |
| A path | `MaxPathBytes` | 512 bytes | §11.3 |
| Standard error kept | `MaxStderrBytes` | 64 KiB | §11.4 |
| Wall-clock limit | `DefaultTimeout` | 120 s (`--timeout`) | §11.4 |
| A manifest | `MaxManifestBytes` | 256 KiB | this runtime's |
| An options document | `MaxOptionsBytes` | 1 MiB | this runtime's |
| Diagnostics reported | `MaxReportedDiagnostics` | 1 000 per list | this runtime's |

Standard output past 64 MiB ends the run at once (`EMIT-RESPONSE`). It does not wait for the time
limit. A response is decoded under the bound on JSON values, so a `files` array past `MaxFiles` is
refused while it is decoded, not after it is held whole. The bound gives each file four values (the
object, its path, its contents and its mode) and leaves a fixed 80 000 for the rest, so about 8 000
diagnostics fit beside 10 000 files.

**Run (§4).** `emit.Run(ctx, manifest, requestVersion, request, RunOptions)` returns the validated
response or the diagnostics that refuse it. It works as follows.

- `command[0]` is resolved first. A name is looked up on the host's `PATH`, in absolute directories
  only, and never in the working directory. A relative path is resolved against the repository
  root, and with its links resolved it must stay inside the root.
- The emitter's environment is exactly `PATH`, `HOME` (a fresh empty directory, removed afterwards),
  `LANG=C.UTF-8` and the manifest's `env` names that are set. An unset name is a warning,
  `EMIT-ENV-UNSET`, which carries the name and never a value.
- The working directory is the repository root, and the request goes on standard input.
- The emitter runs in a process group of its own. At the limit the whole group is killed
  (`EMIT-PROCESS`). Windows has no process groups, so there only the emitter itself is killed.
- Standard error is counted, and its first 64 KiB are kept. They are passed through only with
  `--emitter-stderr`.
- `RunOptions.Override` sets variables after the environment is formed, for the harness's
  determinism check. The host never sets it.

**Write mode.** Nothing is written until every run's response is valid. Then the host does the
following.

1. It walks the owned paths without following a link. A link that an owns glob matches, or below
   which one can match, refuses the run (`EMIT-LINK`), as does one met at or above a response path.
   The walk passes over anything named `.git`, in any case, so git's directory, or the `.git` file
   of a worktree or submodule, is never owned and never deleted as stale.
2. The directories are created, and every file is staged as a temporary file (`.jpack-emit-*`) in
   its directory. A file whose directory is held by a stale file is staged beside that file. A
   failure here leaves every owned file as it was.
3. The stale owned files are deleted, and the directories they held are created.
4. Every staged file is renamed into place.

The report lists what was written and deleted. A failure on the way is `EMIT-WRITE`, at the path;
the staged files are removed, and what was already deleted or renamed is reported. The run is not
a transaction across files.

**Check mode** writes nothing. It reports each path as `missing`, `changed`, `mode` or `extra`. Bytes
are compared exactly, reading at most the response's length. A mode is the owner's executable bit,
as git records it, and it is not compared on Windows, which has none.

**Exit codes.**

| Exit | Meaning |
|---|---|
| 0 | Written, or identical. |
| 1 | `--check` found a difference. |
| 2 | Not written or not checked: a refusal of any kind, an emitter failure, or an unreadable input. |
| 3 | An unusable command line: a missing or conflicting flag, a remote path, a non-positive timeout, a missing root, or an input the manifest's `input` does not take. |

The report is JSON on standard output, `emitReportVersion` `"1.0.0"`. It is this runtime's own
format, holding paths, codes, digests and counts. When the registry is invalid it also carries the
registry's own validation report.

**Codes.** The companion's codes are used under its names: `EMIT-REQUEST-VERSION`,
`EMIT-KIND-RANGE`, `EMIT-PROCESS`, `EMIT-RESPONSE` and `EMIT-DEPRECATED`. The companion's open items
leave host refusals uncoded until "the first host's rows". This runtime adds these, all provisional
(VERSIONING.md):

- Errors: `EMIT-MANIFEST`, `EMIT-OWNS-OVERLAP`, `EMIT-KIND-ID`, `EMIT-OPTIONS`, `EMIT-REQUEST`,
  `EMIT-LINK`, `EMIT-PATH-UNREADABLE` and `EMIT-WRITE`.
- Warnings: `EMIT-ENV-UNSET` and `EMIT-REPORT-TRUNCATED`.

`EMIT-STRAY-WRITE` belongs to the harness.

**Where the companion and RFC 0036 differ**, the companion governs:

1. A time-out is `EMIT-PROCESS`. The RFC names only a non-zero exit.
2. An emitter diagnostic's location has §7.3's form. RFC 0036 §3's example location,
   `{"kind", "id"}`, is not of that form, so it is refused.
3. The determinism list leaves out "randomness". The host does not check determinism; the harness
   will.
4. The response's three members are all required. The companion lists `diagnostics` without
   "optional", so a response that leaves it out is refused.

**Readings this runtime makes** where the text is silent:

- `**` matches any number of segments, zero included, so `gen/**` also owns a file named `gen`.
  This differs from a gitignore pattern.
- `*` matches within one segment, a leading `.` included.
- A caret range on `0.x` keeps the leftmost non-zero part, as npm and Cargo do. A pre-release kind
  version is in no range.
- A pin of a read kind name with another kind id is refused (`EMIT-KIND-ID`), not dropped
  silently.
- Paths are also refused when they collide once normalized (NFD), not only once case is folded. This
  is canonical caseless matching.
- A path may not begin with a drive letter (`C:`).
- Git's directory is never owned. A bare `**` owns the whole repository, so stale deletion would
  remove every file the emitter does not answer with; a manifest that gives it is refused. An owns
  glob or response path with a `.git` segment is refused, in any case, since a case-insensitive file
  system reads `.GIT` as `.git`. A glob such as `*/**` can still match a path below `.git`, so the
  walk of the owned paths never enters it either. Decided here; the RFC is silent.
- No argument of `command` may be absolute or hold a `..` segment.
- Emitter codes follow `^x-[a-z0-9]+(-[a-z0-9]+)*$`, at most 64 bytes.
- An unreadable input exits 2, where the registry commands exit 4, because RFC 0036 §4 fixes the
  host's exit codes.

### Consequences

- Good: a generator in any language gets the same path safety, stale-file deletion, check mode and
  value-free report, and CI can gate on `jpack emit --check`.
- Good: the harness reuses `emit.Run` and `emit.ParseResponse` as they are.
- Bad: the host is not a sandbox. The emitter runs with the user's rights and can write anywhere
  itself; only the harness's stray-write check notices that. The link checks are made before the
  writes, so a concurrent change to the tree between check and rename is not guarded against.
- Bad: write mode is not atomic across files. A failure part way leaves the files renamed so far in
  place, and the report says which.
- Neutral: empty directories left after stale files are deleted are not removed. The walk of the
  owned paths is not bounded beyond what the globs prune.

## More information

- Spec RFC 0036 (Accepted 2026-11-16) and the registry companion `spec/registry.md` §§11–12
  (proposed).
- ADR-0093 (the registry reader), ADR-0095 (facts contracts).
- Open questions for the spec:
  1. Should `**` match zero segments at the end of a glob?
  2. Is `diagnostics` optional?
  3. How strict should the normalization rule be?
  4. What do modes mean on Windows?
- Decided here, for the companion to confirm: owns globs that name `.git`, and a bare `**`, are
  refused, and the walk never enters `.git` (see the readings above).
