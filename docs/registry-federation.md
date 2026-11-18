# Registry federation: assembly, the index check, locks and signatures (proposed)

**Status: proposed.** Registry format 1.1.0 is a proposed revision of the registry companion,
not released, and nothing here is promised under `VERSIONING.md`. The commands and their output
may change with the text until it is released. Format 1.0.0 and every 1.0.0 command are
unchanged.

Format 1.1.0 lets one registry be assembled from several sources: a standard, which governs kinds
and states bounds, and repositories, which hold their own entries within those bounds. A
federation manifest names each source, the repository identity and commit its bytes come from, the
environments it serves, and the paths of its registry, its bounds and its kind declarations. This
runtime builds every class of the format: the assembler, the index checker, the lock writer, the
lock checker and the lock signature verifier (sections 1 to 12), with the commands that sign a
lock and a source statement.

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
  `LOCK-INDEX-MISSING`: never a skipped check. Likewise, since a lock lists every emitter manifest
  its writer was given (§7), an emitter given that the lock does not list, or one given for a lock
  that names no emitters, is `LOCK-EMITTER-MISMATCH` at `/emitters` (§9, step 8), never ignored.
- `coverage` says whether the lock covers a resolved registry (of the `--environment` given, and
  the one the lock names for it) or a generated file (its path, its mode as on disk and its
  digest, of `--file-source` in an assembled lock). Its report names no registry.
- A check and coverage establish that bytes are the ones a lock names, not who wrote the lock:
  that is verification (below).

## Signatures and verification

```
jpack registry lock sign --lock registry.lock.json --registry registry.json \
  --kind kinds/database.json --kind kinds/db-role.json \
  --registry-id <registry id> --require-index false \
  --key <seed file> --public-key standard.pub

jpack registry source sign --federation federation.json \
  --source billing=<commit>=checkouts/billing \
  --key <seed file> --public-key billing.pub --output billing.sig.json

jpack registry lock verify --lock registry.lock.json \
  --signature registry.lock.json.sig.json --keys expected-keys.json \
  --registry-id <registry id> --require-index false \
  --record-dir <records directory> --floor 1
```

- A signature document has five string members, written in code-point order as RFC 8785 bytes
  and one line feed, and signs the text's message for one digest: a lock's digest, or a source
  statement's (its repository, its commit, and each document's path and digest). A signature says
  the key's holder signed that digest; it does not say the registry is right, reviewed or
  current, or when it was signed.
- `lock sign` first checks the lock as `lock check` does, with the same flags, and signs only a
  lock that checks; otherwise it writes the check's report and exits 1. The document goes to
  `<lock>.sig.json`, or to `--output`. `source sign` builds the statement of its one `--source`
  from that source's own documents at its commit, each admitted as the assembler admits it; no
  other source is read, so nothing here checks the assembly. Its document goes to `--output`.
  Neither command writes a document over anything.
- The seed is read from the file `--key` names, and from nothing else: no flag or environment
  variable carries a key. It is held to the rules a trail's signing key is held to: named by its
  real path with no symbolic link in it, one regular file with one name, on unix owned by the
  user the runtime runs as and readable by nobody else, in directories nobody else can write
  unless their sticky bit is set. On Windows a key's privacy cannot be checked, so no key signs
  there. `--public-key` names the public key the seed must have, and a seed with another is
  refused before anything is signed. Output and errors name a key by its identifier alone (the
  first 32 hexadecimal characters of the SHA-256 of the public key), never the key or the seed.
- `verify` follows the text's order: the expected inputs and the serial record; the lock
  signature, checked against the expected key before the lock is parsed; the lock; its serial;
  the index; each source signature, against the key the signer set names for its source and the
  statement rebuilt from the index (never the lock's own statement member); then, with
  `--registry`, the rest of the lock check, and coverage with `--resolved` or `--file`. Exit 0
  when the lock is accepted and recorded, 1 when it is not.
- `--keys` names the expected signer set, a JSON object `{"key": ..., "repositories": {...}}`:
  the lock's (the standard owner's) key and, for an assembly, each repository identity's key,
  64 lowercase hexadecimal characters each. It is given every time and never taken from a
  document. Every key is held to the trust rule (canonical, not of small order), and a signature
  is checked with Ed25519 as RFC 8032 states it, an S of the group order or more refused.
- Signature documents are counted in the order given, the `--signature` documents first. Only
  the first lock signature is read; every document beyond the lock's sources count plus one is
  `LOCK-SIG-EXTRA` and is never opened, so a path there that names nothing, a directory or a pipe
  is not an input error. Each document is read when the verification reaches it, so the source
  signatures only once the lock itself is admitted; one read there that cannot be is an input error
  (exit 4).

### The serial record

- `--record-dir` has no default. It holds one record per registry id, a file named by the
  SHA-256 of the id: `{"digest","formatVersion":"1.0.0","registryId","serial"}` as RFC 8785
  bytes and one line feed. With no record for the registry id, `--floor` must be given
  (`LOCK-EXPECTED` otherwise): a floor of 1 accepts whatever lock comes first, trust on first use,
  chosen by giving it.
- Against a record, a lower serial is `LOCK-SERIAL-ROLLBACK`, and the same serial with another
  digest `LOCK-SERIAL-CONFLICT`; a floor above the record's serial takes its place. A lock that
  every step accepts is recorded; a refused lock changes no record, and the record of a lock
  already recorded is not written again.
- Recording is a compare and set. The verifier creates a lock file beside the record
  (`<name>.lock`, exclusively) and waits at most five seconds for one another verifier holds;
  then it reads the record again and applies the serial rule again, writes a temporary file and
  syncs it, renames it over the record, syncs the directory, reads the record back and compares
  it, and removes its lock file. Only then is the lock accepted.
- A lock file that is still there after the wait is never removed by the runtime, since it cannot
  tell a crashed verifier from a slow one: the verification is `LOCK-SERIAL-STATE`, and standard
  error names the file. Remove it once no verifier is running.
- `LOCK-SERIAL-STATE` is also a record that cannot be read, is not of its form or is of another
  registry id, and a write or read back that fails; the lock is not accepted, and the reason is
  written to standard error. The directory is created with mode 0700 and the record with 0600; a
  directory or record its group or other users can write, a record that is a symbolic link or not
  a regular file, and a directory that is a symbolic link, are refused the same way.
- On Windows, a record's and its directory's permissions are not checked (their ACLs are not
  read), and the directory is not synced, which Windows does not offer: the temporary file's bytes
  are flushed before the rename, and the rename replaces the record in one step, but without
  write-through, so a power loss just after it may leave the record as it was before. A floor no
  lower than the last serial accepted covers that.

## Bounds

Every input is read within a fixed bound, and a document over its bound is not parsed: a source's
document, the manifest or the index is refused as `REG-DOC-SIZE`, and a known index or lock is
an input error (see Readings). The bounds are the manifest and a bounds document 256 KiB each, a
registry and an index 8 MiB, a kind declaration 256 KiB, a lock 1 MiB, a lock or source signature
4 KiB, a serial record 64 KiB, an emitter manifest 256 KiB and a generated file 16 MiB. A manifest names at
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
- a file's source is not compared for a single registry's lock;
- an index item that is not of its form is `LOCK-SOURCES-MISMATCH` at the lock's item;
- `verify` reads the bytes of every signature document it is given, each within 4 KiB, and parses
  none beyond the count;
- a record that is of another registry id than the one its name is the hash of is
  `LOCK-SERIAL-STATE`.

The pinned rows are in `internal/federation/testdata/1.1.0`, with their provenance. Every row of
every class runs in that package and through the commands, a verifier row with its record in a
directory of its own.
