---
status: accepted
date: 2026-11-16
deciders: maintainer
---

# A start-up module of its own, which reads one resolved registry and the secrets the platform delivered, never a store

## Context and problem statement

The specification accepted RFCs 0034 to 0038 on 2026-11-16. They name a Go start-up package,
`github.com/Judgment-Pack/judgment-pack-runtime/startup`, as a nested module of this runtime tagged
`startup/vX.Y.Z` (RFC 0034 §8, Related artifacts). It is product code, not part of the registry
companion. The RFCs give it these duties:

- It reads resolved registries (RFC 0034 §8). As a consumer of resolved documents, it declares the
  `resolvedFormatVersion` selectors it accepts (§8, Selectors across 1.x).
- It reads secret values at run time, from the environment or the mounted files the platform
  provides. It holds them in a type whose printed form is `[secret]` (RFC 0034, Security and
  privacy; RFC 0035, Evidence).
- RFC 0036 §6's Go typed loader reads one resolved document through it, and gives members at
  secret positions its secret type.

Nothing fetches a secret (RFC 0035 §5), and how a secret reaches a process is the deployment's
concern, not the reference's (RFC 0035, Alternatives).

The maintainer decided:
- the module path, a nested module, and its own tags;
- that 1.0 is a secret reader interface only, with two readers: the environment and mounted files.
  A store reader comes later, in a sub-package;
- that both readers trim exactly one trailing `\n` or `\r\n`, and keep every other byte;
- that the file reader refuses a world-readable file;
- that values are held only in a type that prints `[secret]` under every fmt verb, `%#v` included,
  and under encoding/json, encoding/text and log/slog, with the bytes reached only by an explicit
  method;
- that the module maps a reference to where the platform mounted or exported it, and never calls a
  store.

Material-decision impact: public-surface, security.

## Decision drivers

- No value is held outside the secret type, and nothing in the repository fetches one.
- Fail closed: a document or value that cannot be shown to be the one delivered is refused, never
  defaulted.
- A stable surface that a later store reader extends without a break.
- No dependency outside the standard library, so an adopter takes on none.
- What `jpack registry resolve` writes is read unchanged.

## Considered options

- A nested module: a reader interface, the two readers, and a table of bindings keyed by reference.
- A package in the runtime module, beside `sdk`.
- Readers that derive a variable or file name from the reference by a naming rule.
- A secret store client in the module.

## Decision outcome

Chosen option: "a nested module". A package in the runtime module would share its 0.x promise and
its dependencies. A naming rule would put delivery in the reference, which RFC 0035 rejects. A store
client is outside the maintainer's decision.

**The module.** It is `startup/`, with its own `go.mod`, `go 1.24.0` (the runtime's floor), and no
`require`. Its tags, `startup/vX.Y.Z`, do not start the release workflow, which runs on `v*`.

**Reading a resolved registry.** `ReadResolved(data, environment)` and
`ReadResolvedFile(path, environment)` refuse, in this order:

1. A document over 8 MiB or nesting deeper than 32 (RFC 0034 §1; the root counts as one).
   `ErrLimit`.
2. A document that is not exactly one UTF-8 JSON value, or has two members of one name in any
   object. `ErrDocument`. encoding/json keeps the last of two members, and another reader may keep
   the first, so the same bytes could give two selectors or two environments.
3. A `resolvedFormatVersion` that is missing or not exactly one of `ResolvedFormatVersions()`, which
   is `1.0.0`. `ErrSelector`. The selector is read before any other member, so a document of a later
   format is refused for its selector, not for a member it adds.
4. A member that is missing, unknown or of the wrong type, as RFC 0034 §5 shows the document:
   - five members at the root;
   - `registry`: `id`, `version` and `digest`;
   - each kind pin: `kind`, `id`, `version` and `digest`;
   - each entry: `kind`, `id`, `owner` and `value`.

   Also refused here: entries that are not in strictly increasing (kind, id) order, and pins not in
   strictly increasing kind order, by code point. `ErrDocument`. Members are read into maps, not
   structs, because encoding/json matches a struct field's name without regard to case.
5. An environment other than the one named, or none named. `ErrEnvironment`.

An entry's `value` is kept as raw JSON, with every reference as written. The reader does not re-run
validation:
- not the number rules of §1;
- not escapes inside values;
- not kind schemas, references or digests.

`jpack registry validate` and `resolve` run those in CI. The start-up reader checks what decides
which document a service runs with: its selector, its environment and its shape.

**Reading secrets.**
- `Reader` is `ReadSecret(ctx, reference) (Secret, error)`. The context is for the later store
  reader. The environment and file readers only check it.
- `Bindings` maps a reference, matched exactly and never parsed, to an environment variable (`Env`)
  or a file (`Files`). It reads through `ReadEnv` or `ReadFile`. A reference in neither map is
  refused, and so is one in both.

These readings go beyond the decisions:
- A value that is empty after the trim is refused (`ErrEmpty`). A failed injection often shows as
  an empty variable.
- A value over 64 KiB is refused (`ErrLimit`). That is the largest payload of the store RFC 0035
  names.
- `ReadFile` follows symbolic links, as mounted secret volumes use them, and checks the target.
  - It refuses a path that is not a regular file. It checks before the open, because a named pipe
    would block the open, and again on the opened file.
  - Only the others' read bit (`0o004`) is refused. A group-readable file is read.
  - The others' read bit is refused by default. `FileRules{AllowOtherRead: true}`, through
    `ReadFileWith` or `Bindings.FileRules`, accepts it. Cloud Run (`0444`) and Kubernetes (`0644`)
    mount secret files world-readable unless a mode is set, so an operator either sets `0440` or
    `0400` (preferred) or opts in. The opt-in relaxes this one check and nothing else.
- Outside Unix the permission bits Go reports are synthesized, so `ReadFile` refuses every file
  (`ErrPermissionsUnknown`).

An error names the variable or the path. It never carries a value, and never carries a reference,
as RFC 0035 §5 asks of diagnostics.

**The secret type.**
- `Secret` implements `fmt.Formatter`, `fmt.Stringer`, `json.Marshaler`,
  `encoding.TextMarshaler` and `slog.LogValuer`, each giving `[secret]`.
- It refuses `UnmarshalJSON` and `UnmarshalText`, since a secret position holds a reference.
- `Reveal` returns a copy of the bytes, and `NewSecret` copies its input.

fmt prints a value by reflection, without its methods, when the value is in an unexported struct
field. Under a verb that does not fit, it then follows a pointer to a struct. This change's test
found that path: a `[]byte` behind a pointer was printed in decimal under `%s`. So the bytes are
captured by a function behind the pointer, and reflection cannot read a function's captured
variables. fmt answers `%T` and `%p` itself, with the type's name and an address. `encoding/gob`
refuses the type, which has no exported fields.

**CI.** `go test ./...` at the root does not enter a nested module. `dev.json` gains
`startup-fmt`, `startup-vet` and `startup-test`, and `jpdev.json` maps `startup/**` to them.
`startup-test` holds the module to 90% statement coverage. The `test` matrix runs the module's
format check, vet and tests on every leg.

**Not built:**
- a store reader;
- the Go typed loader (RFC 0036 §6), an emitter that `jpack emit` would run;
- a tag. The first `startup/v1.0.0` is a maintainer action.

### Consequences

- Good: a service reads its registry and its secrets with no new dependency, and no printing or
  encoding path shows a value.
- Bad:
  - files cannot be read on Windows;
  - a platform whose secret mounts are world-readable by default (Cloud Run `0444`, Kubernetes
    `0644`) needs a mode set, or the explicit `AllowOtherRead` opt-in;
  - an empty secret cannot be delivered.
- Bad: `jpack registry resolve` holds each resolved document to the size bound, but not yet to the
  depth bound. A valid registry whose overlay sets a nested value at a deep pointer resolves to a
  document deeper than 32, which this module refuses. That refusal fails closed, and it is to be
  fixed in the registry's step 8.
- Neutral: the bindings are the adopter's to write, or a deployment emitter's.
- Revisit when:
  - the registry companion (`spec/registry.md`) is released, under a new selector if its resolved
    format differs;
  - a store reader is added;
  - the resolver gains the depth check.

## More information

Spec RFCs 0034 (§1, §5, §8), 0035 (§5) and 0036 (§6), accepted 2026-11-16. `startup/`,
[startup/README.md](../../startup/README.md), and the start-up module section of
[VERSIONING.md](../../VERSIONING.md#the-start-up-module).
