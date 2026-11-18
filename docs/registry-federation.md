# Registry federation: assembly and the index check (proposed)

**Status: proposed.** Registry format 1.1.0 is a proposed revision of the registry companion,
not released, and nothing here is promised under `VERSIONING.md`. The commands and their output
may change with the text until it is released. Format 1.0.0 and every 1.0.0 command are
unchanged.

Format 1.1.0 lets one registry be assembled from several sources: a standard, which governs kinds
and states bounds, and repositories, which hold their own entries within those bounds. A
federation manifest names each source, the repository identity and commit its bytes come from, the
environments it serves, and the paths of its registry, its bounds and its kind declarations. This
runtime builds the assembler and the index checker of the format (sections 1 to 6 and 12.1). The
lock writer, the lock checker and the lock signature verifier are not built yet, and no command
raises a `LOCK-*` code.

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
- Each path the manifest names is read under its source's directory. A symbolic link, or a path
  that leaves the directory, is a read error; a file that is absent is not given, which the
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

## Bounds

Every input is read within a fixed bound, and a document over its bound is not parsed: a source's
document, the manifest or the index is refused as `REG-DOC-SIZE`, and a known index or lock is
an input error (see Readings). The bounds are the manifest and a bounds document 256 KiB each, a
registry and an index 8 MiB, a kind declaration 256 KiB, a known lock 1 MiB. A manifest names at
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
  names pass as a repository's own instead of being refused as `FED-SOURCE-ASSEMBLED`.

The pinned rows are in `internal/federation/testdata/1.1.0`, with their provenance. Every
assembler and index-checker row runs in that package and through both commands; the lock classes'
rows are listed there as pending.
