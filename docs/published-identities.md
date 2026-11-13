# Published identities

`jpack experimental check-published` reads a git repository's own release tags and reports every
pack or dialect document whose bytes changed under an identity a tag already published
([ADR-0090](adr/0090-check-published-identities-over-a-repositorys-tags.md)). The specification
asks that a published `(id, version)` not change: changed content gets a new version. This command
checks that rule for the packs and dialects a project keeps in its own repository. It is a check of
this runtime, not part of the specification, and it evaluates nothing, so the conformance claim in
[CONFORMANCE.md](../CONFORMANCE.md) is unchanged by it.

The command, its output and its manifest are experimental and may change in any minor release
before 1.0 ([VERSIONING.md](../VERSIONING.md)).

## What it checks

An **identity** is a document's kind and the `id` and `version` the document itself declares:

- a **pack** is a JSON object with a `specVersion` member;
- a **dialect document** is a JSON object with a string `formatVersion` and a `case` member (a
  named dialect document);
- any other JSON file is counted as an other file and not read further.

Identity is read from the document, never from its path. A file moved to another path with the same
bytes is no finding, and a changed file that declares a new `version` is no finding. A pack and a
dialect with one `id` and `version` are two identities.

The tags are read in the order of the commit graph (below). For each identity, the **published
bytes** are the bytes the first tag holding it published, or, with `--manifest`, the bytes the
manifest pins. Every other digest found under the identity, at a later tag or at HEAD, is one
finding, at the first place it was found:

| Code | Meaning |
| --- | --- |
| `changed` | A later tag, or HEAD, holds other bytes under an identity an earlier tag published. |
| `duplicate` | The tag that first published an identity holds two documents with other bytes under it. |
| `differs-from-manifest` | A tag or HEAD holds bytes under an identity the manifest pins to another digest. |
| `vanished` | An identity the manifest pins is held by no tag the run reads. HEAD alone does not answer a pin: a deleted or moved tag is a finding. |
| `not-in-manifest` | With `--strict`, a tag published an identity the manifest does not pin. |

Bytes are compared exactly, by SHA-256 (the `sha256:` digest a lock uses), so a change of
whitespace, of member order or of a final line feed is a finding. This is stricter than the
dialect specification's rule for a publication authority, which is stated over canonical content;
ADR-0090 says why the check compares bytes.

HEAD is the commit HEAD names. Changes not yet committed are not read. HEAD never publishes an
identity: a document only HEAD holds is new, not published, and a document whose version is bumped
at HEAD is no finding.

A selected file that cannot be read as a document at some tag is reported as **unreadable** at
that tag, with the reason, and is never skipped: JSON this runtime does not accept, a file larger
than the 10 MiB limit every pack read applies, a symbolic link, or a pack or dialect document whose
`id` or `version` is not a non-empty string. An unreadable file hides no finding.

Tree entries are read as git reads them, by the file type their mode names, never by how the mode
is spelled. A submodule is not entered: a submodule at a path the selection selects or reaches
below is reported as unreadable, and so is an entry whose mode git reads as a submodule; exclude
such a path with `--exclude 'path/**'`. A directory or file whose mode is spelled otherwise than
git writes it (`040000` or `40755` for `40000`, say) is entered or read as git reads it, and is
reported as well, since a reader that went by the spelling would not see it as git does. A tree
git could not parse, one with an entry whose mode is not octal digits among them, is unreadable
whole.

## Selecting tags and paths

```text
jpack experimental check-published [--repo DIR] [--tags GLOB]... [--include GLOB]... [--exclude GLOB]...
                                   [--manifest FILE] [--write-manifest FILE] [--strict]
                                   [--format human|json]
```

- `--repo` names a directory in the repository; the default is the current directory.
- `--tags` selects tags by name, without `refs/tags/` (repeatable). The default is every tag,
  annotated and lightweight. A tag that names no commit (a tag of a tree or a blob) is reported as
  not read, and the result is incomplete.
- `--include` and `--exclude` select paths (repeatable). The default is `**/*.json`. A repository
  that keeps test fixtures, examples or schemas beside its published documents should name the
  directories that hold what it publishes, for example `--include 'packs/**/*.json'
  --include 'dialects/**/*.json'`: a fixture that reuses one identity for two documents is a
  `duplicate` finding like any other.

In a pattern, `*` matches within one slash-separated segment, `**` as a whole segment matches any
number of segments, and `?` and `[...]` are as in Go's `path.Match`. Patterns match paths from the
repository's root.

## Order of the tags

Tags are ordered by the commit graph: in git's topological order, oldest first, so a tag on an
ancestor always comes before a tag on its descendant, and tags of one commit come in name order.
Version strings and tag dates are not consulted: a tag's name need not be a version, and its date
is whatever its writer set.

The order decides only which place a finding names as published and which as found. Whatever the
order, two digests under one identity among the selected tags are a finding. Two tags no ancestry
relates, such as the heads of two maintenance lines, come in the order git's walk gives them.

## Output and exit status

`--format human` writes a short text whose first line is `valid:`, `invalid:` or `incomplete:`.
Every name taken from the repository (a ref, a path, an `id`, a `version`) is written as a JSON
string in printable ASCII, so no character of it can act on a terminal or break a line; a path
that is not UTF-8, which git admits, is written as Go quotes it, each such byte as `\x` and two
hexadecimal digits.

`--format json` writes the standard envelope (`outputVersion`, `tool`, `command`, `status`) with:

| Member | Meaning |
| --- | --- |
| `publishedCheckVersion` | `"1"`, the version of this report's members. |
| `repository` | `head`, the commit HEAD names, and `shallow`. |
| `tags` | `total` in the repository, `read`, `unreadCount` and `unread` (each `ref` and `reason`). |
| `identities` | The number of identities the tags read published. |
| `documents`, `otherFiles` | Distinct blobs read as documents, and as other JSON files. |
| `findingCount`, `findings` | Each finding: `code`, `kind`, `id`, `version`, `published` and, but for `vanished` and `not-in-manifest`, `found`. A place is `source` (`tag`, `head` or `manifest`), `ref`, `path` and `digest`. |
| `unreadableCount`, `unreadable` | Each `ref`, `path`, `digest` when the bytes were read, and `reason`. |
| `manifest` | With `--manifest` or `--write-manifest`: `checked`, `strict`, `newCount` and `new`, `notHeldCount` and `notHeld`, `entries`, and `writtenTo`. |
| `listLimit` | Each list holds at most this many items, 1000; every count is exact. |

In JSON, a path that is not UTF-8 is written with each such byte replaced by U+FFFD, as Go's
encoder writes it.

| Status | Exit | When |
| --- | --- | --- |
| `valid` | 0 | Every identity keeps its published bytes and every selected tag and file was read. |
| `invalid` | 1 | At least one finding. |
| `incomplete` | 1 | No finding, and a selected tag or file could not be read. |
| `error` | 1 | The `--manifest` file is not a manifest this runtime reads (`JPS-PUBLISHED-MANIFEST`). |
| `unsupported` | 2 | The `--manifest` file declares a later `manifestVersion`. |
| `error` | 3 | A usage error: a pattern, `--strict` without `--manifest`, a `--write-manifest` target that is not a manifest. |
| `error` | 4 | The repository could not be read, and nothing is reported (`JPS-PUBLISHED-*`, below), or the `--manifest` file could not be read (`JPS-INPUT-READ`, `JPS-RESOURCE-INPUT-BYTE-LIMIT`) or the manifest written. |

| Code | Meaning |
| --- | --- |
| `JPS-PUBLISHED-GIT` | No `git` program on `PATH`. |
| `JPS-PUBLISHED-REPOSITORY` | `--repo` is not in a git repository git will read, or git failed. |
| `JPS-PUBLISHED-SHALLOW` | A shallow clone without `--manifest`, or with `--write-manifest`. |
| `JPS-PUBLISHED-PARTIAL-CLONE` | A partial clone: a repository with a promisor remote. |
| `JPS-PUBLISHED-HEAD` | HEAD names no readable commit. |
| `JPS-PUBLISHED-READ` | git stopped answering while objects were read. |
| `JPS-PUBLISHED-WRITE` | The manifest could not be written. |

## The manifest

A manifest pins every published identity to the digest of the bytes its first tag published:

```json
{
  "manifestVersion": "1",
  "documents": [
    {"kind": "pack", "id": "expense", "version": "1.0.0", "digest": "sha256:…", "firstTag": "v1.0.0"}
  ]
}
```

`--write-manifest FILE` writes the manifest of the run: the entries of the `--manifest` given, as
they are, and every identity a tag published that it does not pin, at its first tag's bytes. The
entries are sorted by kind, `id` and `version`, one per line, so the same entries always give the
same bytes. A run never moves a pin to the bytes it found: a manifest checked and written back in
one run keeps every entry it had, a changed one included, and the finding is reported. The file is
written whole or not at all, by rename, and only over a file that holds a manifest this runtime
reads; any other file, or a symbolic link, is left as it is and the run is refused before the
repository is read.

`--manifest FILE` checks against a manifest in the same run. An identity whose bytes differ from its
pin, and a pinned identity no tag holds, are findings. An identity the manifest does not pin is
listed as new and is not a finding, unless `--strict` is given. The manifest is read strictly: an
unknown or duplicate member, another `manifestVersion`, a digest not of the form `sha256:` and
sixty-four lowercase hexadecimal digits, or two entries for one identity are refused.

## Shallow and partial clones

A shallow clone lacks history and, usually, tags. Without `--manifest` it is refused
(`JPS-PUBLISHED-SHALLOW`), since no result it gave could be backed. With `--manifest` it is
checked, and every report says `shallow`: HEAD and the tags it holds are compared with the pins, and
a pinned identity no tag it holds answers is listed under `notHeld`, not reported as `vanished`,
because a shallow clone cannot tell a vanished identity from one it did not fetch. No manifest is
written from a shallow clone: it cannot say which tag first published an identity, or with which
bytes.

A partial clone (one with a promisor remote) is refused: reading a blob it lacks would mean
fetching it, and this command fetches nothing.

## How git is run

The command starts `git` (version 2.15 or later) from `PATH` and nothing else, and reads the
repository through git's read-only plumbing: `rev-parse`, `config --get-regexp` (the promisor
check), `for-each-ref`, `rev-list --topo-order --stdin` and one `cat-file --batch`. A repository's
own configuration can name programs to run, so every invocation is made safe against a repository
it does not trust:

- `--no-pager`, `-c core.hooksPath=` set to the null device, `-c core.fsmonitor=false`,
  `-c protocol.allow=never`, `-c credential.helper=`, `-c gc.auto=0`, `-c maintenance.auto=false`,
  `--no-optional-locks`, `--no-replace-objects` and `--literal-pathspecs`, with an explicit
  `--git-dir`;
- every inherited `GIT_*` variable is removed, so none can redirect the read to another object
  store, and `GIT_NO_LAZY_FETCH=1`, `GIT_TERMINAL_PROMPT=0`, `GIT_OPTIONAL_LOCKS=0` and
  `GIT_NO_REPLACE_OBJECTS=1` are set;
- no tag name and no path in the repository is ever one of git's arguments: tags are named to git
  by object id, what goes to git's standard input is object ids alone, and the one path given, the
  repository's directory, is given as an absolute path;
- git's checks of the repository's owner (`safe.directory`) are left as they are.

The tests plant hooks, an fsmonitor, pagers, an `ssh` command, an external diff, a credential
helper and an alternate-refs command in a repository's configuration, and check that none of them
runs; they also check, file by file with digests, `.git` included, that a run writes nothing into
the repository. The only file the command writes is the one `--write-manifest` names.

## Cost

One `git cat-file --batch` process answers every object read. A run reads:

- each tag's commit, once;
- each directory at most once per distinct pair of path and tree id: a directory whose tree is
  unchanged since an earlier tag is not read again, so a tag costs only what changed since the tags
  before it, and a directory no `--include` pattern can reach below is not read at all;
- each selected blob once, by object id, however many tags and paths hold it, hashed and decoded
  once.

Memory holds, per run, the pairs of path and tree id already read, one digest and identity per
distinct selected blob (not its bytes), and, per identity, the first place each of its distinct
digests was found. Object contents are held one object at a time, each at most 10 MiB; a larger
object is read past without being kept. Every list a report carries holds at most 1000 items. Over
this runtime's own history, 55 tags and about 900 distinct documents, a run takes under a second
and about 30 MB of memory.

## In CI

A job with the full history and every tag:

```sh
git fetch --force --tags --unshallow 2>/dev/null || git fetch --force --tags
jpack experimental check-published --include 'packs/**/*.json' --include 'dialects/**/*.json'
```

A job on a shallow clone checks against a manifest the repository keeps, written from a full clone
after each release:

```sh
# from a full clone, after a release tag is made; commit the result
jpack experimental check-published --manifest published.json --write-manifest published.json \
  --include 'packs/**/*.json' --include 'dialects/**/*.json'

# in the shallow job
jpack experimental check-published --manifest published.json --strict \
  --include 'packs/**/*.json' --include 'dialects/**/*.json'
```

A manifest kept in the repository is only as good as the review of its changes: a commit that
changes a document and its pin together passes the shallow job. The full-history job reports that
change as `changed`, which is why it is the one to keep where it can run.
