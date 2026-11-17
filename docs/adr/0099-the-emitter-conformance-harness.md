---
status: accepted
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

- `expected/`, which must hold regular files only. For a run that gives no files, it holds the
  reserved marker `~empty` alone: one zero-byte regular file at its root, read as no files. Git
  cannot track an empty directory, so an empty `expected/` would be lost on a clone and the
  vector refused there. No response path can name the marker, because the host refuses a path
  segment that begins with `~`. A `~empty` that is not empty, not a regular file, not at the
  root of `expected/`, or beside any other file or directory is `EMIT-VECTOR`. A missing
  `expected/` is never read as empty: it is `EMIT-VECTOR`, except under `--update`. Nor is one
  that holds neither a regular file nor the marker, empty or holding only directories: it is
  `EMIT-VECTOR`, except under `--update`, which replaces it with the marker;
- `expected-failure.json`, `{"code": CODE}`. `CODE` is a listed `EMIT-` error code or an emitter's
  `x-` code.

Anything else in a vector is `EMIT-VECTOR`: an unknown member, a link, or a vector past a bound.
The vectors directory and each `kinds/` are listed by batches, keeping only the
entries their bound counts (the vector directories and links, and the `*.json` files), so a
listing past its bound stops there and is never held whole. Every vector is laid out before any emitter runs, by its entries and their sizes alone (its
members and their kinds, the names of `kinds/*.json`, and the files, count and sizes of
`expected/`), so a bad layout refuses the run whole. No vector's bytes are read then. Each vector
is read just before its own run, under the bounds above, and dropped after its checks, so the
harness holds one vector at a time and no bound on the whole set is needed. At that read, the
vector directory and every directory between it and each file are checked again to be
directories and not links, and the file is opened without following one, so a directory swapped
for a link after the layout is `EMIT-PATH-UNREADABLE` at the file, never read through. A vector whose bytes
are refused there (an unreadable file, or an `expected-failure.json` that is not of its form)
refuses the run at that vector, with `EMIT-VECTOR` or `EMIT-PATH-UNREADABLE` and exit 2; the
earlier vectors' rows stay in the report, and `--update` may already have written their
`expected/`. A directory
that holds no vector, empty or holding only files, is `EMIT-VECTOR` too: the hostile rows alone
would otherwise pass a run that never ran the emitter. For the same reason, a set whose every
vector is refused before the emitter runs, so that no request reaches it, is `EMIT-VECTOR` and
exit 2.

The two inputs are formed differently:

- **A registry vector** is formed with `emit.FormRequests`, as `jpack emit` forms it, from the
  registry and its kinds, each read under the host's bounds. An oversize options document is
  `EMIT-OPTIONS`, as the host reports it.
- **A given request** is held to the envelope the host writes for the manifest, then written in
  its canonical form. The envelope is: exactly the five members of the resolved form
  (`declarations` among them, an array) or the four of the facts form, the request version the
  host writes, the manifest's emitter, options valid against its schema, the input member as an
  array, and one environment for `resolved` with `each`. What the request carries inside is the
  author's, a declaration that does not match its pin included (ADR-0101).

`DIR/harness.json` is exactly `{"harnessLevel": LEVEL}`. Without it the level is `1.0`, and an
unknown level is `EMIT-VECTOR` and exit 2.

**`--update`** rewrites `expected/` of each vector whose run succeeded, from that run. It never
touches an expected-failure vector. It writes nowhere but `VECTOR/expected/`, which must be a
directory or absent. The new `expected/` is written whole in a temporary directory in the
vector, `.expected-update-*`, and renamed into place, and the old one is then removed, so a write
that fails partway leaves `expected/` as it was. When the new one cannot be renamed into place and
the old one cannot be put back, the temporary directory is kept, holding the old one, and the error
names it. That directory, or one left by a run stopped between the renames, is refused by the layout
as an unknown member until it is removed, so the old files are never lost. A run that gives no files
gets the marker `expected/~empty` alone, and a run that gives files has no marker. The vector directories
are not links.
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
   values. On Windows, where a program finds its home by `USERPROFILE`, `APPDATA` and
   `LOCALAPPDATA` rather than `HOME`, runs 2 and 3 set each of them to their `HOME` too; run 1
   sets none. Go ignores `TZ` on Windows, so there the time zone is not changed for a Go emitter
   (a known limit, below). The manifest's `env` names are held as the host gives them, these
   included. Each run must give run 1's
   response bytes exactly (the RFC's "identical bytes", read as the raw bytes, not a canonical
   form of them), or run 1's refusal exactly. The row names the runs that differ. A `HOME` the
   harness cannot make is its own fault, not a difference: `EMIT-HARNESS-BOUND`, and the run
   exits 2.
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

**Check 4, the decided reading.** The harness adds a synthetic kind that the manifest does not read:
its declaration, its pin, and one entry, owned by the registry's first owner. Each goes in the place
the registry's order rules require. With those added, it forms each request again, the same way in
the facts form. It then restores every `registry.digest` in the changed request to the original
request's value at the same place. That place is `/resolved/N/registry/digest` and, in the facts
form, a contract's `source.registry.digest` if a source carries one. Next it asserts that the result
is byte-identical to the original request. In the resolved form that includes `declarations`, in
which nothing is restored, so the synthetic kind's declaration must not be there (ADR-0101). A
difference fails the row. The reason says whether the
request holds the synthetic kind's id. Only then does the harness run the emitter again and assert
that the response is unchanged. That run is on the original request's bytes: the restored bytes are
those bytes exactly, or the row has already failed. So the byte comparison is what tests that no
undeclared kind reaches the emitter, and the run repeats run 1. The digest is restored because §4
lets an emitter write the registry digest into a generated file's header, and the synthetic entry
changes that digest. Without restoring it, a conforming emitter would fail. This reading is filed
against companion §11.5 as spec #350. A registry that is not valid once the synthetic kind is added
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
- **Checking regions.** A suffix automaton of each string that occurs, built once for the request
  and kept for its files, reads only that string's regions. A
  first pass over every such string finds the complete occurrences of all of them. A second pass,
  for each string longer than 32, asks whether one of those occurrences holds each maximal run of
  32 or more.

The work is linear in the files' bytes for each string that occurs, and two bounds hold it:

- `MaxReferenceBytes`, 1 MiB of distinct strings of 32 or more;
- `MaxReferenceWork`, 2^32 steps a vector.

Past either bound, the result is `EMIT-HARNESS-BOUND` and exit 2. A row that fails names the file,
the entry and the pointer, never the string.

**Stray writes and their bounds.** The emitter runs in a fresh copy of `--root`, made in a new
temporary directory with its links resolved. Nothing named `.git` is copied, in any case, so the
repository's own is never compared, and a `.git` in the copy after a run, at any depth, is one the
emitter made: a creation. Regular files keep their permission bits, links are copied as links,
and any other kind of file is `EMIT-PATH-UNREADABLE`. A link keeps its target, except that an
absolute target inside `--root` is moved to the same path in the copy, so a write through it
stays in the copy and is a stray write there. A link that leads out of `--root` is
`EMIT-HARNESS-BOUND` naming the link, and exit 2, before any emitter runs; by the amendment
below, one that git does not track and that no path of the run could name is instead left out of
the copy, with the warning `EMIT-LINK-OMITTED`. Where a link leads is
found by following it in `--root`, link by link: each `..` is taken from where the parts before it
led, and a part that does not exist is taken as written. A link that cannot be followed within 40
links is treated as leading out. The run is refused, rather than check 6 skipped for that link,
because skipping would still run the emitter with a way out of the copy: a write through the link
would land in the real directory, possibly the repository itself, while the run passed. The
author removes the link from the working tree, or makes it relative and inside `--root`. On
Windows, a reparse point that Go reads as neither a symbolic link nor a regular file, a junction
for one, is `EMIT-HARNESS-BOUND` naming it, and exit 2, before any emitter runs, by the same walk
and whether or not the copy falls back. By the amendment below, a junction is followed as a link
is, and one that leads out is held to the same rule as a link. Since Go 1.23 a junction is `ModeIrregular`, not
`ModeSymlink`, so the link check cannot see where it leads, and under the fallback the emitter
would run in the repository with a way out of it while check 6 skipped. Reporting check 6 as not
run there instead was weighed and not chosen: the refusal fails closed, and it holds whether or
not the copy can be made. A regular file stays, as Go reads a deduplicated file, which is a
reparse point too. The copy is bounded at 256 MiB of file bytes
and 100 000 entries (`MaxCopyBytes`, `MaxCopyEntries`). The bytes bound counts the bytes copied,
so a file that grows while it is copied cannot pass it. Past either, the result is
`EMIT-HARNESS-BOUND` and exit 2. Every link of `--root` is checked to stay in it before the copy is
made, so a link that leads out is refused whether or not the copy can be made. Only Windows refusing
a link for want of the privilege (`ERROR_PRIVILEGE_NOT_HELD`) runs the emitter in the repository
itself, and each check 6 row skips, with that reason. Any other link the copy cannot make, a target
past the system's length limit once moved into the copy for one, is `EMIT-PATH-UNREADABLE` naming
the link, and exit 2, before any emitter runs. The copy keeps an honest emitter's mistake out of the repository. It is not a
sandbox: the emitter runs with the user's rights. The copy's root is compared too: a change to
its permission bits is a stray write at `.`. After a vector that wrote to it, the copy is made again; when it cannot
be, the run stops with `EMIT-PATH-UNREADABLE` and exit 2, and that vector's failing check 6 row
stays in the report with the earlier rows.

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
| path-traversal, absolute-path, drive-letter, backslash, git-directory, git-short-name (`GIT~1`), tilde-segment, trailing-dot, device-name, path-too-long (513 bytes), outside-owns | `/files/0/path` |
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
| unresolvable-location (an entry the request does not hold) | `/diagnostics/0/location` |

**Report** (`harnessReportVersion` `"1.0.0"`). The report holds the level applied, the outcome
(`pass`, `fail` or `not-run`) and the emitter's id and version. It has one row for each vector and
check, and one for each hostile. A row holds the vector, the check number and name, the status, and
the reason. It also holds the details its check gives: the expected code, the differing runs, the
host's and the emitter's error diagnostics, the differences, the references and the stray writes.
Each list is bounded at 1000. The report's own diagnostics give why it could not run, plus
`EMIT-DEPRECATED` and `EMIT-ENV-UNSET`, each once, and, by the amendment below,
`EMIT-LINK-OMITTED` at each link left out of the copy.

**Codes.**

- `EMIT-STRAY-WRITE` is the companion's.
- `EMIT-VECTOR` (the layout) and `EMIT-HARNESS-BOUND` (a check that could not be completed) are
  this runtime's, and provisional, as is the warning `EMIT-LINK-OMITTED` (a link left out of the
  copy), by the amendment below.
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

### Known limits

These are carried, not fixed. None gives a wrong verdict for an honest emitter on Linux or macOS.

- **Check 2's time zone on Windows.** Go reads the time zone from the system there and ignores
  `TZ`, so runs 2 and 3 do not change it for a Go emitter. An emitter in another language may
  still read `TZ`.
- **Links changed while the copy is made.** Each link is checked against `--root` as it is
  copied, but the kind of each entry comes from the walk's earlier listing. A writer racing the
  repository during the copy could flip a link to a directory while another link is checked
  through it, and back before it is copied, so that the copy holds a link that leads out. It
  needs a concurrent writer to the repository; the emitter, which runs in the copy, cannot cause
  it.
- **A directory link made before its target (Windows).** Go's `os.Symlink` on Windows stats the
  target to choose a file link or a directory link. A link that sorts before the directory it
  points to, relative (`a -> b/`) or absolute and moved into the copy (`cache -> C:\repo\real`),
  is made before that directory exists in the copy, so it becomes a file link, and an honest
  emitter's write through it fails check 1. Making links in a second pass, after every file and
  directory, would close it.
- **Whether Windows resolves `..` in a link target lexically: unverified.** The link check takes
  each `..` from where the parts before it led. If Windows instead resolves `..` lexically, a
  target such as `a -> b\..\..\x`, with `b -> deep\deeper`, passes the check but leads out. It
  needs a run on Windows with the privilege to make links.
- **Synced-folder placeholders (Windows).** A file a sync client has not yet downloaded is a
  reparse point that Go reads as irregular, not as a regular file, so check 6 refuses it and a
  repository holding one is refused with `EMIT-HARNESS-BOUND` at its path. Downloading the file
  first lets the run go ahead. The refusal is deliberate: the harness cannot read such a file
  without making the sync client fetch it.

## More information

- Spec RFC 0036 §5 (Accepted 2026-11-16) and the registry companion `spec/registry.md` §11.5
  (proposed). ADR-0098 covers the host.
- Code: `internal/emit/conformance`, `internal/cli/emit_conformance.go`, and the golden vectors in
  `internal/emit/testdata/conformance/`.
- Raised on spec #350 (companion §11.5): check 4's reading, and check 5's reading that a run
  inside a complete occurrence of any secret-position string passes.
- Decided here: check 2 compares raw response bytes; a vector that gives its request whole skips
  checks 4 and 5; the hostile responses go through `emit.ParseResponse`.

## Amendment: links that lead out of the repository (2026-11-17, #467)

A repository often holds a link that leads out of it in a place the run never touches. A Python
virtual environment inside the repository is the common case: its interpreter is such a link.
Refusing the whole run for it blocked a common layout. Such a link is now left out of the copy
when it is not the repository's own content and no path of the run could name it. Every other
link that leads out is refused, as before.

**The rule.** For each symbolic link of `--root` that leads out of it, found as before, and for
each Windows junction, followed as a link is (`os.Readlink` reads where it leads), the link is
left out of the copy only when all three hold:

- **It is not tracked in the index.** Neither the link's path nor any path below it is in the
  repository's index. What is tracked is read from the index alone, not from ignore rules or the
  last commit, so "not tracked in the index" covers ignored and untracked paths alike. An index
  with zero entries, as after every path is removed from it (`git rm -r --cached .`), tracks
  nothing: every link that leads out is then untracked, and the other two conditions decide.
- **No owns glob could name it.** No owns glob matches the link's path, or may match a path below
  it. This is the host's own `EMIT-LINK` rule, so a link the host would refuse is refused here
  too. Names are also compared loosely (`emit.Manifest.MayOwnAtOrBelow`), by their loose key
  (`emit.NameKey`). Two names count as one when they match after the code points HFS+ ignores are
  dropped, Unicode canonical caseless matching is applied, the result is upper-cased, a backslash
  is read as a separator, and each segment's trailing dots and spaces are dropped, as Windows
  reads names. A glob segment that holds `~` counts as any name, because Windows can read it as a
  short name. So a link at `Gen` or `gen.` is refused for `gen/**` on every system, and so is a
  link at `generated` for `GENERA~1/*.go`. A glob itself never ends a segment in a dot or a
  space, which the host refuses.
- **The vectors path is clear of it.** The harness reads the vectors, and `--update` writes them,
  through the directory given, not through the copy. That directory, cleaned as `filepath.Abs`
  makes it, is followed part by part and link by link from its volume's root. Each time the walk
  stands on `--root` (by `os.SameFile`), it counts the parts below it. A link it follows inside the
  repository is refused. So is a link at, above or below the path where the directory ends in the
  repository, compared loosely as above, so that `venv./v` names a link at `venv`. A segment
  whose loose key is empty on either side names every link from there down. A directory that
  holds the repository names every link,
  and so does one that cannot be followed: a part that cannot be read, a link that cannot be read,
  or more than 40 links.

Every other link that leads out is `EMIT-HARNESS-BOUND` at its path, and exit 2, before any
emitter runs, as before: the first in walk order. A link left out is left out of every copy, and
the report gives the warning `EMIT-LINK-OMITTED` at its path. That warning lists at most 100
paths, in sorted order. Past them, one more diagnostic, at the repository alone, gives the count
of the rest in its message, because the report has no member for counts. A write the emitter
makes at a left-out link's path lands in the copy and is a stray write there. A junction that
stays in the repository, or a reparse point `os.Readlink` cannot read, is still refused, since the
copy does not make one. Where the copy falls back to the repository itself (Windows without the
privilege to make links), a link left out would be there for the emitter, so the run is refused
at the first such link, as before.

**Reading the index.** The harness reads `--root/.git/index` itself, read-only, and never runs
git or any other program (`internal/gitindex`). It reads index versions 2, 3 and 4, with version
4's compressed path names, and checks the trailing SHA-1 checksum before it reads an entry. It
fails closed: every link that leads out is refused, as before this amendment, when any of these
holds.

- `--root` holds no `.git`, or its `.git` is not a directory. A linked worktree's or a
  submodule's `.git` file is one such case: it names an index elsewhere, which the harness does
  not follow. Running from the main working tree's root reads its index.
- The index is missing, is not a regular file, or cannot be read.
- The index is past a bound: 64 MiB, or 1 048 576 entries.
- The checksum does not match, so a file cut short or changed is refused. An index of a
  repository whose object format is not SHA-1 ends in another function's checksum, so it does not
  match either.
- The index is not of a form the reader reads: another version, an entry or path that is
  malformed, a mode that is not a file, a link or a submodule, an extended flag it does not know,
  or entries out of git's order.
- The index holds an extension whose signature does not begin with `A` to `Z`. Git requires a
  reader to understand such an extension, and the reader understands none. Two change the set of
  paths the entries give: a split index (`link`), whose entries are in another file, and a sparse
  index (`sdir`), whose entries may be directories. An optional extension, `A` to `Z`, such as
  `TREE`, `REUC`, `UNTR`, `FSMN`, `EOIE` or `IEOT`, is passed over, as the format allows.

**Comparing paths.** An index path and a link's path are compared byte for byte, as git's slash
paths. The file system may read two names as one, so they are compared loosely too, by the same
loose key as above, and either match counts as tracked. Every link below a submodule (a gitlink
entry) counts as tracked, since its files are in the submodule's own index. So does every link
in or below a directory that holds an index segment with `~` and a digit, which Windows may read
as the short name of any name there. A segment made only of dots and spaces has an empty loose
key: Windows reads it as no name at all, so that `lib/. ./mod.py` is `lib/mod.py` there. A link
whose path holds such a segment counts as tracked, and so does every link in or below a
directory that holds one in an index path; one at the index's root makes every link tracked. The
index must also hold each path at most once at each stage, in git's order. Each of these errs
toward tracked, and so toward refusing.

**Why this is sound.** A response path is one the host refuses unless an owns glob matches it, so a
response can write only at or below an owned path. The vectors are read and written only below the
vectors directory. Both are known before the copy is made, and each comparison errs toward naming a
link. A link tracked in the index is the repository's own content, which an emitter may rely on, so
it is never left out: a run without it would not be a run in the repository. The links refused are
a subset of those refused before, and the copy never holds a link that leads out, so the emitter
has no new way out of it. Where there is no copy, because the harness would fall back to running in
the repository itself, a link left out would still be there, so that fallback refuses the run at
the first such link (see "The rule" above). Omitting every link that leads out was weighed and not
chosen: it would drop tracked content from the copy, would let an emitter pass while the host
refuses the same repository for a link at an owned path, and would let `--update` write outside the
repository through a link the vectors are reached by. Deciding "tracked in the index" from ignore
rules was weighed and not chosen: it would reimplement git's ignore rules, and an untracked path
that no rule ignores would still be the repository's content to that reading.

**Known limits.**

- A repository whose `.git` directory names another working tree (`core.worktree`), or whose
  index git reads from elsewhere (`GIT_INDEX_FILE`), is read as its `.git/index` gives it. The
  harness does not read git's configuration.
- An index git wrote without its checksum (`index.skipHash`, which `feature.manyFiles` turns on)
  ends in zeros, which do not match, so every link that leads out is refused there.
- A fresh repository has no index file until something is added, so every link that leads out is
  refused there too.
- A Windows short name set by hand, without `~`, is not read as an alias.
- An emitter that reads a file of the repository through a link left out finds nothing there. Its
  run then differs from a run in the repository. The warning names the link, so the author can
  remove it, make it relative and inside `--root`, or track what it holds.

`EMIT-LINK-OMITTED` is this runtime's, and provisional (VERSIONING.md).
