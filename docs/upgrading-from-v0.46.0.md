# Moving a CI pin of `jpack` from v0.46.0 to v0.51.0

This page is for a project that pins the `jpack` binary at `v0.46.0` in CI and wants to move the
pin to `v0.51.0`. It lists, release by release, what such a project can notice: new commands and
flags, exit codes, new refusals, formats, the registry line, and deprecations. The release notes
under [`docs/releases/`](releases/) remain the full record; this page only gathers what touches a
pinned CLI.

The short version:

- For an unchanged pack, nothing a pack decides changes in any of these releases. Dispositions,
  evaluation output, decision record bytes and digests are the same for every supported
  `specVersion` (`0.5.0`, `0.3.0`, `0.2.0-draft`, `0.1.0-draft`). Each release checked this
  against the previous release binary.
- Nothing is removed, and nothing is deprecated.
- Almost every change is new surface: the registry commands, `jpack emit`, `jpack terraform
  facts`, the start-up module and an outcome gate on `experimental evaluate`. A pipeline that does
  not use them sees one change, in `experimental check-published` (v0.47.0).
- The runtime does not call the gateway, and what a Runner reads from the runtime does not
  change. Moving this pin needs no gateway or Runner release.

## Steps

1. **Take a baseline with the pinned binary.** From the project's checkout, run v0.46.0:

   ```bash
   jpack experimental revalidate --write baseline.json
   ```

2. **Check the candidate against it.** Run v0.51.0 from the same checkout:

   ```bash
   jpack experimental revalidate --baseline baseline.json
   ```

   A difference is reported per row. [Revalidating before moving a Runtime
   pin](revalidating-a-runtime-pin.md) describes the procedure and what it does and does not show.
3. **If the pipeline runs `experimental check-published`,** read [v0.47.0](#v0470) below. Its exit
   code for one input changed.
4. **Download and verify the v0.51.0 archive** the same way the pipeline verifies v0.46.0's. Then
   move the pin.
5. **To adopt the registry line,** read [The registry line](#the-registry-line) for what is
   promised from v0.51.0. If the project's Go code reads delivered secrets or a resolved registry,
   pin the start-up module at `startup/v1.0.0`.

## Release by release

### v0.47.0

**Changes to existing commands.** `experimental check-published` carries its release review's
fixes:

- **A changed exit code.** A manifest that declares another `manifestVersion` and also adds a
  member now gets the version refusal, exit 2. Under v0.46.0 it got exit 1.
- **A new unreadable case.** A document whose `id` or `version` is longer than 4096 bytes is now
  reported as unreadable.
- **New JSON members.** With `--format json`, a path or tag ref that is not UTF-8 gains a
  `pathEscaped`, `refEscaped` or `firstTagEscaped` member. For UTF-8 names the bytes are the same,
  and `publishedCheckVersion` stays `"1"`.
- **Bounded work.** The work per identity no longer grows with its digests, git output is
  bounded, and no grafts or commit-graph file is read. The two known limits stated in v0.46.0's
  notes are fixed.

**New flags.**

- `experimental evaluate --require-outcome a,b` turns a run or a batch into a gate:
  - exit 0 when every item's disposition is a listed outcome;
  - exit 1 on a finding;
  - exit 2 when the gate could not judge;
  - exit 3 for a command line that cannot be parsed.
- `--gate-summary FILE` writes a JSON summary that carries no fact value.
- Without `--require-outcome`, every exit code and byte of output is unchanged.

**New commands.**

| Command | What it does | Exit codes |
|---|---|---|
| `jpack registry validate` | checks a registry | 0 valid, 1 invalid (the report is still written), 3 invocation error, 4 unreadable input |
| `jpack registry resolve` | writes one environment as RFC 8785 JSON | |
| `jpack registry import` | converts the YAML authoring subset to canonical JSON, and refuses everything outside it | |
| `jpack registry facts` | writes registry entries as batch lines for `evaluate --batch` | 2 when the registry is invalid |
| `jpack registry check-pack` | reports where a pack's fact pointers do not fit a contract | |

### v0.48.0

**Changes to the registry commands.** These follow the registry companion's text:

- **Pointer length.** An entry reference's `#pointer` is held to 512 code points, not 512 bytes.
- **`id` form.** An `id` is read as an RFC 3986 `absolute-URI`. Non-ASCII characters, and
  characters outside RFC 3986, are refused.
- **Pattern size.** A pattern's size is counted as written, so `(ab|ac){1000}` is
  `REG-KIND-PATTERN-LIMIT`.
- **Locations and cycles.** A kind declaration's step-2 diagnostics carry `kind` in their
  location. The schema cycle check covers every `$ref` that names a definition.
- **A message.** `FACTS-DOCUMENT-LIMIT`'s message begins "A facts document". The code and the
  limit are unchanged.

**New commands.**

- **`jpack emit`** runs a registry emitter named by its manifest, under a time limit and a
  minimal environment. `--check` compares instead of writing.
- **`jpack emit conformance`** runs an emitter on its vectors. `--update` rewrites `expected/`.
- **Three reference emitters:**
  - `jpack-emit-go-facts`;
  - `jpack-emit-go-loader`;
  - `jpack-emit-ts-constants`.
- **`jpack terraform facts`** turns the JSON that `terraform show -json` writes into one batch line
  per selected resource. Every value marked sensitive is removed before selection. Its flags:

  ```
  jpack terraform facts (--plan FILE | --state FILE) --type T[,T...] [--mode managed|data]
    [--address PATTERN] [--actions a,b] [--max N] [--max-bytes N]
  ```

  Its exit codes:
  - exit 0 for a batch, an empty one included;
  - exit 2 for a refused file, with empty standard output and the refusal on standard error;
  - exit 3 for a flag that breaks a rule;
  - exit 4 for a file that cannot be read, or output that cannot be written.

**Formats.**

- Emit request and report version `1.0.0`, and harness report version `1.0.0`.
- Terraform plan `format_version` `1.0` to `1.2`, and state `1.0`.

**New Go module.** `startup/` is tagged on its own as `startup/v0.1.0`.

### v0.49.0

**Changes to the commands added in v0.48.0.**

- **Options schemas.** An emitter options schema's `$defs` is admitted at the root only. One
  level below the root it is `EMIT-MANIFEST`; v0.48.0 admitted it by mistake.
- **Deposed objects in `jpack terraform facts`.** Each deposed object now has its own line id,
  `<address> deposed <key>`, and a `deposed` member. Before, it shared its current object's id.
  A malformed deposed key, or two resources with one id, is `TF-DOCUMENT`.
- **Start-up module.** These changes are on the main branch and ship in `startup/v1.0.0`:
  - `ReadFile` refuses a world-writable file;
  - `ReadResolvedFile` refuses a file that is not regular, without blocking;
  - `ErrNotRegular`'s text changed, so match it with `errors.Is`.
- **Windows paths.** A conformance suite or fixture path that passes through a directory reparse
  point, such as a synced folder's placeholder, is refused.
- **Interrupts.** An interrupted `jpack emit` refuses the run before its write.

**New.**

- A warning, `EMIT-STAGING-LEFTOVER`, for a staging file a stopped run left behind.
- The registry line's support and deprecation policy, in VERSIONING.md. It takes effect at
  v0.51.0.

### v0.50.0

**Changes to the emit commands.**

- **Interrupt codes.** An interrupted `jpack emit` or `jpack emit conformance` reports
  `EMIT-INTERRUPTED`. v0.49.0 reported `EMIT-PROCESS` and `EMIT-HARNESS-BOUND` respectively.
- **Outward links.** `jpack emit conformance` no longer refuses a repository that holds an
  untracked link leading out of it, such as a virtual environment's interpreter. It leaves the
  link out and warns `EMIT-LINK-OMITTED`. To decide this, it reads the git index itself; it never
  runs git.
- **The `expected/` bounds.** A few edge cases of the `expected/` bounds are read differently.
  The v0.50.0 notes list them.

**Elsewhere.** The shared JSON decoder is faster and gives the same bytes.

### v0.51.0

**The registry line is named stable** (below).

**Changes to the emit commands.**

- **`jpack emit --check`.** An interrupt after the last emitter is refused with
  `EMIT-INTERRUPTED`, where v0.50.0 exited 0 or 1.
- **Git indexes.** `jpack emit conformance` no longer reads a git index that holds an offset table
  (`IEOT` or `EOIE`). In that case every outward link is refused with `EMIT-HARNESS-BOUND`, as
  before v0.50.0.

**The start-up module.** `startup/v1.0.0` is tagged on the same commit as `v0.51.0`.

## The registry line

From v0.51.0 these are stable under VERSIONING.md, "The registry line":

- `jpack registry` (all of its subcommands), `jpack emit` and `jpack emit conformance`, with their
  flags and exit codes;
- the formats they read and write;
- their codes and bounds;
- the reference emitters' manifests, the kinds they read, and their options;
- the start-up module at `startup/v1.0.0`.

What the promise means in practice:

- Within major version 1 of the line, nothing is removed, no bound is tightened, and no code
  changes meaning.
- Every `REG-*`, `FACTS-*` and `EMIT-*` code is now the registry companion's (spec tag
  `registry-v1.0.0`), so a pipeline may branch on it.
- Security fixes are also backported to the previous `jpack` minor and the previous `startup/v1`
  minor for three months.

**Still provisional, outside the promise:**

- `jpack terraform facts`, its batch lines, the `TF-*` codes and the Terraform format table. No
  spec release carries them yet. A change to any of them is named in the release notes of the
  release that makes it.
- The exact bytes the reference emitters write.
- The wording of messages.

`jpack` itself stays at `0.x`.

## Deprecations

None between v0.46.0 and v0.51.0.
