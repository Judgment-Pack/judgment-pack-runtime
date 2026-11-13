---
status: proposed
date: 2026-11-13
deciders: maintainer
---

# Check, over a git repository's own tags, that a published pack or dialect identity keeps its bytes

## Context and problem statement

The specification asks that a published `(id, version)` not change. VERSIONING.md of the Spec says a
published pair SHOULD be immutable and changed pack content should receive a new version; Core §11
says the same of a published pack version; the dialect specification requires a publication
authority to keep `(id, version)` immutable within its catalog, over canonical content, and says the
requirement governs publication, not validation. Nothing in this runtime checks the rule for a
project's own repository, whose release tags are the record of what it published. A pack or dialect
edited in place after its release goes unnoticed until a reader holding the old bytes disagrees
with one holding the new (#404).

## Decision drivers

- Identity is what a document says it is, never where it lives: a moved file is not a change, and
  a changed file under a new version is not one either.
- The repository is not trusted. A clone's configuration can name programs; reading it must run
  none of them, write nothing into it and open no network connection.
- A result that cannot be backed is never reported as clean: a shallow clone cannot say what its
  missing tags published.
- Thousands of tags and large trees must fit in bounded memory, and work must grow with what
  changed, not with tags times files.
- No new dependency: `go.mod` holds no git library.

## Considered options

- **Run `git`'s read-only plumbing**, hardened, and walk trees by object id in Go.
- A Go git library: a new dependency, and the largest one this module would carry.
- Read the object store directly: loose objects, packfiles and their deltas, packed refs and
  reftables, shallow and alternates files, reimplemented and kept in step with git.
- Check each tag out into a scratch directory: writes files, applies the repository's filters and
  attributes, which can name programs, and costs a full tree per tag.
- Compare canonical content rather than bytes, as the dialect rule is stated.

## Decision outcome

Chosen option: "run git's read-only plumbing", because it reads every repository format git reads
with no new dependency, and every way git could start another program or reach the network is one
of a small, fixed set of options that can be turned off.

**The command** is `jpack experimental check-published`. It is experimental, and it is not under
`packs`: it spans packs and dialects, reads a git repository rather than a `jpack.json` project, and
is the first command of this runtime to start another program. It has no MCP tool: the MCP surface
reads documents a client names, and this one starts a program over a repository.

**Identity.** A JSON object with a `specVersion` member is a pack; one with a string
`formatVersion` and a `case` member is a named dialect document; anything else is counted and not
read further. The identity is the kind and the document's own `id` and `version`. A document that
does not decode under the carrier limits every pack read applies, is larger than the 10 MiB hard
limit, is a symbolic link, or declares no non-empty string `id` or `version` is reported as
unreadable at that tag and never skipped. A tree entry is classified as git classifies it, by the
file-type bits of its mode, so a directory whose mode is spelled `040000` is entered as `40000`
is; a spelling git never writes is reported besides, and a submodule, or a mode git reads as one,
that the selection reaches is reported, not passed over.

**Bytes, not canonical content.** The check compares the SHA-256 of exact bytes, the digest locks
already use. A pack has no canonical form in the specification, and the issue asks for bytes. For a
dialect document this is stricter than the dialect rule, which allows a re-serialization of the
same canonical content: such a change is a finding here. A repository that re-serializes its
dialect documents gets a finding per document, and bumps the version or keeps the bytes.

**Order.** Tags are ordered by `git rev-list --topo-order`, oldest first: an ancestor always comes
before its descendants, tags of one commit in name order. Version strings are not consulted, since
a tag's name need not be a version, nor tag dates, which their writer sets. Whether there is a
finding never depends on the order: any two digests under one identity among the selected tags are
a finding in every order. The order chooses the reference, the place a finding names as published,
and the first tag a new manifest entry names. Between tags no ancestry relates, such as the heads of
two maintenance lines, git's walk falls back to commit dates, which committers set, so the order
cannot say which of two digests came first: an identity with a `changed` or `duplicate` finding
enters no manifest a run writes. HEAD comes last and never publishes.

**Findings.** Per identity, the reference is the manifest's pin when there is one, else the first
tag holding the identity; each other digest is one finding, at the first place found: `changed`,
`duplicate` (two digests in the reference tag itself), `differs-from-manifest`, `vanished` (a pin no
selected tag holds; HEAD alone does not answer it), and, under `--strict`, `not-in-manifest`.

**Status and exit.** `valid` exits 0. `invalid` (a finding) and `incomplete` (no finding, and
something selected unreadable, or no tag read and no manifest pin given) exit 1, so a check that
could not read everything, or had nothing published to compare HEAD with, never passes: a clone
fetched without tags, or a `--tags` pattern that matches none, is not a clean result. A usage error
exits 3. A repository that cannot be read (no git, not a repository, a shallow clone without a
manifest, a partial clone, an unreadable HEAD, git failing mid-run, trees that name more than the
walk's bounds) exits 4 with nothing reported.

**The manifest** (`manifestVersion` `"1"`) pins each identity to its digest and first tag in
deterministic bytes. It is a closed input: a reader refuses any member it does not know, so adding
one moves the version. A run writes it by rename, keeping every given entry as it was, so a run
never moves a pin to the bytes it found, and replaces only a file that holds a manifest. A new
identity is written only when its bytes have no finding; one that has a finding is reported as
withheld, and its pin is left to whoever knows which bytes were released. A `not-in-manifest`
finding does not withhold: it says the given manifest lacks the entry, not that the bytes moved,
and under `--strict` it is raised for every new identity, so a run with `--strict
--write-manifest` would otherwise write no new entry at all. No manifest is written from a shallow
clone.

**Git.** Every invocation carries `--no-pager --no-replace-objects --no-optional-locks
--literal-pathspecs`, `-c core.fsmonitor=false`, `-c core.hooksPath=` the null device,
`-c protocol.allow=never`, `-c credential.helper=`, `-c gc.auto=0`, `-c maintenance.auto=false` and
an explicit `--git-dir`; every inherited `GIT_*` variable is removed and `GIT_ALLOW_PROTOCOL=_`,
`GIT_NO_LAZY_FETCH`, `GIT_TERMINAL_PROMPT=0`, `GIT_OPTIONAL_LOCKS=0` and `GIT_NO_REPLACE_OBJECTS`
are set. No tag name and no path in the repository is an argument: tags are named by object id, and
only object ids go over standard input. The subcommands are `rev-parse`, `config --get-regexp`,
`for-each-ref`, `rev-list --stdin` and `cat-file --batch`; none applies filters or attributes, runs
a hook, or pages. Several of the options are defence in depth for exactly that reason, and their
removal is not observable through these subcommands; they are kept so that a later subcommand
cannot quietly widen what runs.

**Fetching.** A partial clone, a repository whose configuration makes a remote a promisor (by
`extensions.partialclone`, a true `remote.<name>.promisor`, or any
`remote.<name>.partialclonefilter`, as git's own promisor-remote code reads it), is refused
outright. Below that refusal the environment stops a fetch, and `-c protocol.allow=never` does
not: a repository's own `protocol.<name>.allow` overrides it. From git 2.45, `GIT_NO_LAZY_FETCH`
fetches no missing object at all. On every version, `GIT_ALLOW_PROTOCOL` overrides every
`protocol.*.allow` setting and allows only a transport named `_`: no built-in transport has the
name, a remote helper given it is looked up on `PATH`, and before git 2.54, the release that let an
alias name hold other characters, no alias a repository defines can have it. A word such as `none`
would not do: git runs the repository's alias `remote-none` for a remote whose `vcs` is `none`.
The two variables overlap on every version from 2.45 to 2.53, and each alone covers the rest.
`safe.directory` is left to git, and so is the user's global and system configuration, the scopes
git reads it from; what in them could start a program is overridden by the options and variables
above.

**Bounds.** One `cat-file --batch` process answers every read. A directory is read once per
distinct (path, tree id), so a tag costs what changed since the tags before it; a directory no
include pattern can reach below is not read. A selected blob is read, hashed and decoded once by
object id. Since the key holds the path, a tree that holds one subtree under two names doubles the
reads at each level, so a run reads at most 1,000,000 directories and 1 GiB of tree objects (an
object read past included; an entry takes at least 24 bytes, so this bounds the entries too), and a
run that would go past either ends with `JPS-RESOURCE-TREE-WALK-LIMIT`, exit 4, nothing reported.
Exit 4 rather than `incomplete`: the walk stops partway through one tag's tree, so no list of what
was not read could be given, and a run that reads part of the tags is not a result. Memory holds a
32-byte SHA-256 of each (path, tree id) key, one digest and identity per distinct blob, and per
identity the first place of each distinct digest; contents are held one object at a time, at most
10 MiB, and a larger object is read past. Lists are capped at 1000 items with exact counts.

### Consequences

- Good, because a repository can gate its CI on the specification's immutability rule for what it
  publishes, with full history or, on a shallow clone, against a manifest.
- Good, because the reading runs nothing a repository's configuration names, and writes nothing.
- Bad, because the command needs a `git` of version 2.15 or later on `PATH`, the first external
  program a `jpack` command starts.
- Bad, because the default `**/*.json` reads test fixtures and examples too, and a fixture that
  reuses an identity is a finding: a repository with such files names its published directories
  with `--include`.
- Bad, because a historical document that does not read keeps a run incomplete until its path is
  excluded or its tag deselected; there is no flag that lets an unreadable file pass.
- Revisit when the specification defines canonical content for packs, when a project needs to
  acknowledge a past change without excluding its path, or when `go.mod` gains a git library.

## More information

- [docs/published-identities.md](../published-identities.md): the command, the manifest, the cost
  and a CI recipe.
- `internal/published` (the check, the git runner, the manifest) and `internal/cli/published.go`.
- ADR-0019 (the reviewed-set lock, whose `sha256:` digest this check uses) and ADR-0083 (pack
  signatures, the other check of a pack's exact bytes).
