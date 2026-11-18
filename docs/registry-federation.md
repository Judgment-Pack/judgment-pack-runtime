# Registry federation: assembly, the index check and locks (proposed)

**Status: proposed.** Registry format 1.1.0 is a proposed revision of the registry companion,
not released, and nothing here is promised under `VERSIONING.md`. The commands and their output
may change with the text until it is released. Format 1.0.0 and every 1.0.0 command are
unchanged.

Format 1.1.0 lets one registry be assembled from several sources: a standard, which governs kinds
and states bounds, and repositories, which hold their own entries within those bounds. A
federation manifest names each source, the repository identity and commit its bytes come from, the
environments it serves, and the paths of its registry, its bounds and its kind declarations. This
runtime builds the assembler, the index checker, the lock writer and the lock checker of the
format (sections 1 to 9 and 12). The lock signature verifier (the lock signature, the serial
record and verification, sections 10 and 11) is not built yet: nothing raises its
`LOCK-SERIAL-*` codes, and no command here says who signed a lock.

## Commands

```
jpack registry assemble --federation federation.json \
  --source standard=<commit>=checkouts/platform \
  --source billing=<commit>=checkouts/billing \
  --no-known --registry-out assembled.json --index-out index.json

jpack registry check-index --index index.json --federation federation.json \
  --source standard=<commit>=checkouts/platform \
  --source billing=<commit>=checkouts/billing \
  --no-known
```

- `--source NAME=COMMIT=DIR` gives one source: its name in the manifest, the full commit its
  bytes are read at, and a local directory that holds them at that commit. Nothing is fetched,
  and the runtime does not check that the directory is that commit: whoever runs the command
  vouches for it.
- Each path the manifest names is read under its source's directory. A symbolic link or junction
  in any component of the path (even one that stays inside the directory), a component whose
  name on disk is spelled otherwise (as a case-insensitive filesystem would resolve it), or a
  path that leaves the directory, is a read error; a file that is absent is not given, which the
  assembly reports as `FED-SOURCE-MISSING`.
- The known list (the assembled indexes and locks a source's registry must not be one of) has
  no default: give `--known-index FILE` and `--known-lock FILE` for each, or `--no-known` to state
  that it is empty. A known index or lock that cannot be admitted (over its bound, not JSON, or a
  repeated member name, for example) is an input error: the command exits 4 with no report and
  names the flag and the file, never what the file holds.
- The report is the format's 1.1.0 report, written to standard output as RFC 8785 canonical
  JSON and one newline. A location may name the `source` a diagnostic is in.
- `assemble` writes the assembled registry and the index only when the assembly has no error.
  `check-index` assembles again from the sources and compares the index that assembly would
  write with the one given, JCS for JCS; any difference is `FED-INDEX-MISMATCH`. A verifier who
  cannot read the repositories holds only the index's word for where the bytes came from.

Exit codes: 0 when the report has no error (warnings allowed), 1 when it has one, 3 for a command
line that cannot be used, and 4 for an input that cannot be read or an output that cannot be
written.

## Locks

```
jpack registry lock write --registry registry.json \
  --kind kinds/database.json --kind kinds/db-role.json \
  --previous registry.lock.json \
  --emitter emitters/db-roles.json=. --emitter-check emitters/db-roles.json=0

jpack registry lock write --federation federation.json \
  --source standard=<commit>=checkouts/platform \
  --source billing=<commit>=checkouts/billing --no-known \
  --source-signature standard=standard.sig.json --source-signature billing=billing.sig.json \
  --key <standard owner's key> --repository-key <billing's repository identity>=<its key> \
  --first-serial 1 --out assembled.lock.json

jpack registry lock check --lock registry.lock.json --registry registry.json \
  --kind kinds/database.json --kind kinds/db-role.json \
  --registry-id <registry id> --require-index false

jpack registry lock coverage --lock registry.lock.json \
  --registry-id <registry id> --require-index false --environment prod \
  --resolved resolved-prod.json --file gen/grant.sh=gen/grant.sh
```

- A lock names its registry by digest, its kinds as pinned, the digest of each environment's
  resolved registry, and, for an assembly, the index's digest and each source's statement and
  accepted source signature, with the files each emitter writes. It is written as its RFC 8785
  bytes; a lock re-formatted by hand is an edited lock (`LOCK-NOT-CANONICAL`).
- `write` takes the serial one above the `--previous` lock's, which must be of the same registry
  id, or the `--first-serial` given out of band. Neither or both is a usage error: there is no
  default first serial. A previous lock at serial 9007199254740991 leaves no next serial
  (`LOCK-FORM`). A refused write prints `{"refused": [codes]}`, exits 1 and writes no lock; the
  lock goes to `--out`, by default `<name>.lock.json` beside a `<name>.json` input.
- For an assembly, `write` verifies each source signature before naming it: by `--key`, the
  standard owner's key, for the standard source, and by `--repository-key` for each repository,
  over the statement rebuilt from the index. A signature that does not verify refuses the lock
  with that signature's code. Keys are 64 lowercase hexadecimal characters, obtained out of band.
- An emitter is a checkout: `--emitter PATH=DIR` (`--source-emitter SOURCE=PATH=DIR` for an
  assembly) names the manifest's path and the directory holding it and the files its `owns`
  matches, which are found as the emit host's check mode finds them and read inside the
  directory; a symbolic link there is a read error. `write` is given the exit code of the host's
  check of each emitter (`--emitter-check`) and runs nothing; any code but 0 is
  `LOCK-EMITTER-MISMATCH`. On a file system that records no executable bit (Windows), emitters
  and `coverage --file` are refused, since a file's mode cannot be read.
- `check` holds the lock to the registry (validated), its kinds and resolved registries, the index
  and its sources for a lock that names one, and the emitters for a lock that names them. The
  expected registry id and index requirement (`--registry-id`, `--require-index`) have no default
  and are never read from the lock: one not given is `LOCK-EXPECTED`. An index given for a lock
  that names none is `LOCK-INDEX-UNLOCKED`, and one not given for a lock that names one is
  `LOCK-INDEX-MISSING`: never a skipped check.
- `coverage` says whether the lock covers a resolved registry (of the `--environment` given, and
  the one the lock names for it) or a generated file (its path, its mode as on disk and its
  digest, of `--file-source` in an assembled lock). Its report names no registry.
- A check and coverage establish that bytes are the ones a lock names, not who wrote the lock:
  that is verification, not built yet.

## Bounds

Every input is read within a fixed bound, and a document over its bound is not parsed: a source's
document, the manifest or the index is refused as `REG-DOC-SIZE`, and a known index or lock is
an input error (see Readings). The bounds are the manifest and a bounds document 256 KiB each, a
registry and an index 8 MiB, a kind declaration 256 KiB, a lock 1 MiB, a source signature 4 KiB,
an emitter manifest 256 KiB and a generated file 16 MiB. A manifest names at
most 256 sources, 4096 paths in all, 64 kind declarations per source and 32 environments; the
known list holds at most 256 indexes and 256 locks. A bound lists at most 1024 values and 16
patterns, and a `maxEntries` is at most 100000. A JSON document whose object repeats a member name
is not admitted (`REG-DOC-DUPLICATE`).

## Readings

Where the text leaves a choice open and no row decides it, this reader reads it as follows:

- a bounds document in YAML is refused, because the YAML import knows only the registry and
  kind selectors;
- a source's own validation report that is truncated and has an error fails the assembly;
- a value-set bound is compared both on membership and on the whole bound;
- a project that two sources list in their secret scopes for different environments is reported
  (`REF-SECRET-SCOPE-SHARED`) against every earlier listing, not only the first;
- a known index or lock that cannot be admitted fails the command as an input error (exit 4,
  no report), rather than naming no registry: read as naming nothing, it would let the source it
  names pass as a repository's own instead of being refused as `FED-SOURCE-ASSEMBLED`;
- a writer is given each source signature by the source's name, and a document given for no
  source is `LOCK-SIG-EXTRA`; a previous lock that is not admitted or not of its form, and a
  first serial out of range, are `LOCK-FORM`;
- an emitter manifest that is admitted but is not a valid emitter manifest is
  `LOCK-EMITTER-MISMATCH`, since no host check of it exits 0;
- emitters given for a lock that names none are not compared, and a file's source is not
  compared for a single registry's lock;
- an index item that is not of its form is `LOCK-SOURCES-MISMATCH` at the lock's item.

The pinned rows are in `internal/federation/testdata/1.1.0`, with their provenance. Every
assembler, index-checker, lock-writer and lock-checker row runs in that package and through the
commands; the lock signature verifier's rows are listed there as pending.
